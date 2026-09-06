# hermes_bridge

Golem 侧微信桥，对接 Hermes 官方平台适配器 `wechat_golem`（源码见同目录 `wechat_golem/`）。

旧 `plugins/hermes`（API Server + 内嵌 MCP）已弃用，请用本方案。  
完整部署步骤与排障：**[DEPLOY.md](DEPLOY.md)**。

## 架构

```
微信 ↔ Golem host
         └ hermes_bridge
              ├ 业务口 listen（默认 0.0.0.0:8643，给 Hermes 适配器）
              │    GET  /health  /events  /media  /status  /self …
              │    POST /send  /send_image|video|voice|emoji  /send_app|record|quote  /revoke …
              └ 管理台 admin_listen（默认 127.0.0.1:8644，仅本机）
                   GET  /ui/              单页管理台（embed）
                   GET  /admin/meta       无鉴权发现入口（供以后 Golem 总控跳转）
                   GET  /admin/overview   总览 JSON（Bearer admin_token）
                   …/targets …/config …/contacts/search …/inbound …/sessions …/diagnose …/hermes/*
                    ↕ 同机 loopback 或跨机 LAN（仅业务口）
Hermes gateway + $HERMES_HOME/plugins/platforms/wechat_golem
```

桥与 Hermes **可同机也可分机**：同机时 `WECHAT_GOLEM_BASE_URL=http://127.0.0.1:8643` 即可，
且 `listen` 可收紧为 `127.0.0.1:8643`；分机时填桥所在机器对 Hermes 可达的地址。

- **入站**：白名单会话（主人私聊始终放行）→ SSE `event: message`；无适配器订阅时直接丢弃。
- **入站图片/表情（懒下载）**：OnEvent 只登记 `media_ref`（内存表，TTL 2h/上限 128，见 `mediaref.go`），SSE 事件带 `media_ref=media_N`，**不预下载、不内嵌 base64**。agent 需要看图时适配器调 `GET /media?ref=` 取回，桥此刻才下载：图片按 中图→原图→缩略图 走 `cdn.DownloadImage(fileID, aesKey)`（fileID 从 Raw 的 `content.value` 自解 XML 拿 `cdnmidimgurl` 等；host 的 `Media.Url` 恒空、md5 不能当 file_id；本后端标签是 `imgmsg` 非 `img`），全失败用 Raw 里 sync 自带的 `image_buffer.data`（ImgBuf 缩略图）兜底；表情走 `Media.Url` 真 HTTP 直链。首次取回后桥缓存字节，重复取零开销。语音/视频暂无可用下载参数，不登记。
- **入站表情结构化**（v0.3.1+）：host 把表情消息 Content 填成裸 md5，桥统一改写为 `[表情]`，并在事件/群批次信封带 `emoji_md5`（全局指纹，收藏判重用）与 `emoji_desc`（发送者侧描述，不可信）。配合 `/media` 取字节，Hermes 侧 `wechat_golem` 维护表情收藏库（**`moods` 情绪 / `tags` 题材标记** 分列；工具 save/list/send/delete；自主应景用 mood，点名标记用 tag——详见 `wechat_golem/README.md` §表情库约定）。
- **群门闩**：闲聊只记本地滚动上下文；`@` / 引用机器人 / `trigger_names` / 冒泡 才去抖合并后一批推送；已推送消息标水位，不重复推。去抖为 trailing：同会话只一个 timer，再次触发会重置满额倒计时（无最长窗口封顶）。
- **斗图门闩**（v0.3.2+）：滑动窗口内第 N 条群表情（默认 30s 内第 3 条）也触发一批推送，`trigger_reason=emoji_burst`、addressing 保持 none（同冒泡语义，只解释送达原因）；同会话默认 5 分钟最多一次，`emoji_burst_count = 0` 关闭。这是 agent 参与斗图与自动收藏的主要入口。
- **群聊身份信封**：每条批次消息都附桥生成的 `verified`、发送者、`sender_role`、`addressing`、`trigger_reason`；`trigger_names` 命中时为 `addressing=self` / `trigger_reason=trigger_name`。真 @/引用别人保持 `other_participants`，即使因冒泡送达也不得被当成发给本机器人；详见部署笔记 §五。
- **控制捷径**（立即 SSE、不去抖、不包群上下文）：审批 `yes/no/...`（**仅主人**；适配器还会核对确有待审批项，群内无待审批则忽略、私聊转普通消息，防止闲聊「同意/no」误唤醒 agent）；整句 **`打断`**（不限主人）。打断时还**作废**该会话当前未推送的去抖批次（停 timer + 标水位），避免 ⚡ 后又被尸体批次叫醒。整句 **`新开会话`/`新对话`**（仅主人，v0.3.3+）：同样作废未推批后透传 `trigger_reason=session_reset`，适配器进程内 `reset_session` 清空该会话 gateway 历史并回执——聊天里就地重置，**长期记忆与群成员档案不受影响**。整句 **`归档`/`归档群友`/`记群友`**（仅主人）：旁路门闩透传 `member_archive`，适配器扩成批量 `wechat_member_profile_upsert` 指令（**不清 session**；见下方「群成员偏好档案」）。
- **撤回**（v0.14+）：整句 **`撤回`/`撤回吧`/`撤回上一条`**（**仅主人**）是唯一**不透传**的捷径——桥直接撤掉自己在该会话最近发的一条，不推 SSE、不惊动 agent（微信撤回窗口只约 2 分钟，绕一圈常赶不上），只在撤不掉时回一句原因。agent 主动撤走 tool `wechat_revoke` → `POST /revoke`（可 `count` 撤多条、`message_id` 指定某条）。所有出站消息都记账（每会话最近 24 条 / TTL 10 分钟，见 `outbox.go`），各 `send*` 接口因此也回 `message_id`。超过 `revoke_window_seconds`（默认 120）的直接拒绝并说明原因：host 的 `Revoke` 无论微信是否真撤都回成功，不拦就是「假成功」。词表 `revoke_tokens` **桥侧独有，适配器无对应 env**。
- **私聊**：桥逐条 SSE；**适配器**侧同会话单飞 + pending（防 ⚡ Interrupt，见 `wechat_golem/README.md` §入站交付）。
- **出站 AppMsg 卡片**（音乐等）：适配器拼好 `<appmsg>` XML + `sub_type` 后 POST `/send_app`，桥走 `message.Send`(TypeAppMusic) 经 host `SendApp` 发送；复用媒体防叠发策略（超时不重开 Send）。业务（搜歌、选 AppID 来源显示）全在 Hermes 侧，桥只补数据通道。
- **出站聊天记录卡片**（对齐 `meme list` / `/pm list`，可嵌图）：POST `/send_record` 传 `items`（文本 `{name,content}` 与图片 `{type:image,url|media_ref}` 可混排；或 `lines`/`records` 纯文本），桥拼 AppMsg `type=19`（图片 datatype=2）后 `sendAppMessage`；tool `wechat_send_record`。图片勿传 data_b64。
- **出站引用回复**（AppMsg type=57，一期仅文本 refer type=1）：POST `/send_quote` 传 `reply`/`svrid`/`fromusr`/`quote_content`（`displayname`/`chatusr`/`createtime` 可选）；桥拼顶层 `<appmsg>`（勿包 `<msg>`）后 `sendAppMessage(57)`。host `Send` 只对 Application/ChatRecord/Music 走 `SendApp`，**出站不用 `TypeAppQuote`**（会 default 丢弃、NewId=0）；SubType 仍为 57。入站 SSE/`msg_id` 供 agent 填 `svrid`；tool `wechat_send_quote`。图片引用二期。
- **入站引用展示**：对方发引用气泡时，本条 `text`=回复正文；`quote_text`/群信封 `quote.summary`=被引用人读摘要（引图为 `[图片]`，**不**把 img XML 给 agent）；`msg_id` 始终是**本条** new_id（出站引用对方本条用它，不是嵌套 `quote_svrid`）。
- **出站**：适配器 HTTP 回发；目标须在白名单（主人私聊除外）。
- **出站目标归属校验**（桥 v0.15+）：各 `send*` / `/revoke` 接受可选 `session_key`（形如 `chatroom:xxx@chatroom` / `private:wxid`），带了就必须与 `chat_id` 指向同一会话，否则 403 拒发（`guardOutbound`）。适配器按当前 agent run 填这个字段，于是「A 群的活儿发去 B 群」在桥这一层还能被挡住——它是最后一道防线，第一道在适配器侧（见 `wechat_golem/README.md` §出站目标）。不带该字段时行为不变（只查白名单），cron 主动推送与人工 curl 不受影响。
- **真 @**：两条件同时要满足——① `mentions` 为真实 **wxid**（`TextData.Reminds`）；② 正文含 **`@显示名` + 特殊空格 U+2005**（不是普通空格）。仅 mentions、正文裸「收到」→ 客户端常无系统 @。桥在有 mentions 时会**自动补** `@名\u2005`（从 ListMembers 取展示名）。适配器可从正文 `@` / `[[mentions:wxid]]` 解析 mentions；最终仍靠桥补齐正文形态。
- **查询**：`/self` `/group_info` `/group_members` `/group_member_detail`（ListMembers 缓存；**不**调 host 的 GetMembersDetail）。公告/管理员：GetInfo 通常无，响应当 note。
- **不内嵌** MCP / agent run；会话与工具全在 Hermes gateway。

## 配置

```toml
[hermes_bridge.config]
listen = "0.0.0.0:8643"         # 与 Hermes 同机时可收紧为 127.0.0.1:8643
token = "与 WECHAT_GOLEM_TOKEN 一致的长随机串"
max_text_len = 2000
send_rate_per_min = 20
max_body_bytes = 83886080   # 80MB，含 base64 媒体
# targets 用 /hermes enable 或本机管理台管理，不必手写

# 本机管理台（与业务口分离；默认只绑 127.0.0.1）
admin_listen = "127.0.0.1:8644"  # 空=关闭管理台；勿轻易改成 0.0.0.0
admin_token = "另一条长随机串"    # 与业务 token 不同；空则 /admin/* 拒绝

# 外部程序（留空=在 PATH 中查找）。装了 ffmpeg 但没进 PATH 是最常见的部署故障；
# /hermes status 与 GET /health 都会报三者的可用性。
ffmpeg_path  = ""               # 如 "D:/tools/ffmpeg/bin/ffmpeg.exe" / "/usr/local/bin/ffmpeg"
ffprobe_path = ""
silk_encoder_path = ""          # 可选；配了语音优先编 SILK，留空则降级 ffmpeg AMR
silk_max_bytes = 0              # 单条语音 SILK 上限字节，0=不限
silk_sample_rate = 24000        # 0 视为 24000

# 控制捷径词表（留空=用内置默认集）。
# 改这些必须同步 Hermes 侧 WECHAT_GOLEM_INTERRUPT_TOKENS / _RESET_TOKENS / _ARCHIVE_TOKENS，
# 否则桥的群门闩会先吞掉消息、适配器收不到（适配器连上时会比对并告警）。
interrupt_tokens     = []       # 默认 ["打断"]
session_reset_tokens = []       # 默认 ["新开会话", "新对话"]
archive_tokens       = []       # 默认 ["归档","归档群友","记群友","记成员","归档成员"]
approval_tokens      = []       # 默认 yes/no/是/否/同意/拒绝… 共 16 个

# 撤回（仅主人；桥内闭环，**不**需要同步适配器 env）
revoke_tokens         = []      # 默认 ["撤回","撤回吧","撤回上一条"]
revoke_window_seconds = 120     # 微信撤回时限；超窗直接拒绝并说明（0=用默认，负数=不检查）

# Hermes 只读 ops（hermes_ops；管理台「Hermes」页）
# hermes_ops_url   = "http://<hermes-host>:8650"   # 同机可用 127.0.0.1
# hermes_ops_token = "与 HERMES_OPS_TOKEN 一致"

# 群触发（对齐旧 hermes）
trigger_names = []              # 如 ["小赫"]，包含则触发
bubble_rate = 0.1               # 未点名冒泡概率，0 关闭
bubble_cooldown_minutes = 10
debounce_seconds = 3            # 触发后合并窗口；同会话只一个 timer
max_context_messages = 40
group_push_all = false          # true = 白名单群每条都推（回滚，关闭门闩）

# 斗图门闩（表情连发触发）
emoji_burst_count = 3           # 窗口内第 N 条表情触发一批推送，0 关闭
emoji_burst_window_seconds = 30
emoji_burst_cooldown_minutes = 5

# 诊断（一般不开）
# record_xml_dump_dir = ""      # 出站记录卡片 XML 落盘目录（绝对路径）；空=不落盘
```

> `GET /health` 无鉴权，返回 `subscribers`、桥的生效捷径词表 `tokens`、
> 外部工具状态 `media_tools`，用于部署自检与两侧词表比对。

## 本机管理台（v0.5+）

- 地址：`http://127.0.0.1:8644/ui/`（随 `admin_listen`）
- 登录：页面填 `admin_token`（存浏览器 localStorage；请求头 `Authorization: Bearer …` 或 `X-Admin-Token`）
- 能力：
  - 总览（SSE 订阅数/红灯项）
  - 白名单启停（可搜联系人）
  - 门闩热更新（即时生效并 `saveConfig`）
  - **入站旁路**（`/admin/inbound/recent` + SSE stream：pushed/dropped/context_only/scheduled/cancelled）
  - **本地 session 态**（`/admin/sessions`：去抖 pending、未推缓冲、冒泡/斗图冷却；非 Hermes gateway session）
  - **诊断试发**（`POST /admin/diagnose`：image/video/emoji，等价 `/hermes image|…`）
  - **Hermes 只读**（需 `hermes_ops_url`：gateway/工具/sessions/日志；**表情库**与**群友档案**浏览；档案可轻写；源码 `hermes_ops/`，运维见其 README）
- 发现钩子：`GET /admin/meta`（无鉴权，无敏感字段）→ `{name,version,ui,admin_listen,auth}`，以后 Golem 总控可外链跳转
- **UI 开发**：页面由 `embed.go` 编译期嵌入（`//go:embed ui/*`）——改 `ui/` 下源码后**必须重编译**才生效：
  `cd plugins && task build:hermes_bridge` 再重载 host（浏览器里是 exe 内嵌版，不是磁盘源码）。验证用真实 `admin_token`
  （运行目录 `config.toml`）：浏览器开 `http://127.0.0.1:8644/ui/`，或 `curl -H "Authorization: Bearer $TOKEN" …/admin/overview`。
  改前端前先读 `admin*.go` 与 `hermes_ops/hermes_ops.py` 确认 API 契约；dev mock `_mock.py` 已废弃删除。
  v0.12 视觉为绿色小清新（浅底、鼠尾草绿强调）；总览按链路/容量/身份分组；`Ctrl/⌘ K` 打开命令面板，字母键可跳页（输入框内不触发）。
- **不要**把 `admin_listen` 暴露到与业务口相同的 `0.0.0.0`；远程请用 Tailscale 等，且 token 与 bot token 分离

> 已有 `plugins/config.toml` 时：host `SetConfig` 用 `toml.Unmarshal` 注入，**缺字段会保留** `main()` 默认值  
>（门闩开、`bubble_rate=0.1`、`debounce_seconds=3`、`max_context_messages=40`、斗图门闩 30s/3条/冷却5min、`admin_listen=127.0.0.1:8644`）。  
> 仅当你在 toml 里显式写了对应键才会覆盖。回滚门闩：写 `group_push_all = true`；关斗图门闩：写 `emoji_burst_count = 0`。

## 管理命令（仅主人，host 已拦）

| 命令 | 说明 |
|---|---|
| `/hermes status` | token 掩码、SSE、门闩、白名单数量、管理台地址 |
| `/hermes enable [名称]` | 当前会话加入白名单（也可用管理台远程加） |
| `/hermes disable` | 移出白名单 |
| `/hermes image <url>` | 诊断：宿主机下载并直发图片 |
| `/hermes video <url>` | 诊断：宿主机下载并直发视频 |
| `/hermes emoji <url>` | 诊断：宿主机下载并直发表情（TypeEmoji，过大自动压缩：GIF 保动画、静图 PNG 优先 / JPEG 兜底） |

> 音乐卡片没有 `/hermes` 诊断命令（业务下沉在 Hermes 侧）。
>
> - Agent 侧调 Hermes 侧 `wechat_send_music`（见 `wechat_golem/adapter.py`）即可；其内部拼 AppMsg XML（与 `plugins/music` 一致：`<appmsg appid sdkver=0><title/><des/><action>view</action><type>3</type><dataurl/><songalbumurl/><songlyric/></appmsg>`）并 POST 桥 `/send_app`（`sub_type=76`，可选 `caption`）。
> - `/send_app` 是通用 AppMsg 通道；聊天记录更推荐结构化 `/send_record`（桥拼 type=19 XML，对齐 meme list / /pm list）；链接等仍可走 `/send_app` 自带 XML。
> - Agent 侧：`wechat_send_record`（items/lines/records）→ 桥 `/send_record`；`wechat_send_music` → `/send_app`。
> - 业务（搜歌 API、选 AppID 让来源显示更随机、何时发列表卡片）全部留在 Hermes 侧 agent；桥不内置音乐搜索、不内置 AppID 表（与表情库同理：桥只补数据通道，业务归 Hermes）。
| `/hermes help` | 本说明（含管理台入口） |

Host 会拦截**所有**未注册的 `/` 命令（`未知命令：/xxx`），消息不会进本桥。  
Hermes 危险命令审批请用微信纯文本 **`yes` / `no`**，不要发 `/approve`。  
停当前 agent 任务：微信纯文本整句 **`打断`**（群/私聊均可；群须在白名单）。不要发 `@机器人 打断` 指望当令牌——须单独一条整句 `打断`。  
撤回机器人刚发的消息：主人纯文本整句 **`撤回`**（桥直接撤，不经 agent；微信只给约 2 分钟）。`/hermes status` 会报当前会话还剩几条可撤——「可撤 0 条」即解释「喊了没反应」。

## 构建

```bash
cd plugins
task build:hermes_bridge
# 产出 host/plugins/golem_plugin_hermes_bridge.exe
```

## Hermes 侧（摘要）

完整步骤见 **[DEPLOY.md](DEPLOY.md)**。要点：

1. **只装一份**适配器，路径必须带 `platforms/`（`$HERMES_HOME/plugins/platforms/wechat_golem/`），
   从本仓库 `wechat_golem/` 拷 `plugin.yaml`（**必须全小写**，Hermes 只扫小写）+ `adapter.py`，
   再把 `adapter.py` 复制成 `__init__.py`。
   不要同时装 `~/.hermes/plugins/wechat_golem/` 等顶层副本（会改错文件、「修了不生效」）。

2. `.env` 至少要有 `WECHAT_GOLEM_TOKEN`（与桥 `token` 一致）与 `WECHAT_GOLEM_BASE_URL`
   （桥业务口地址；同机即 `http://127.0.0.1:8643`）。其余可选变量见 `wechat_golem/plugin.yaml`。

3. `config.yaml`：`plugins.enabled` 含 `platforms/wechat_golem`；
   顶层 `group_sessions_per_user: false`（否则群里每人一条 session、上下文串台）；
   审批调试期用 `approvals.mode: manual`。

4. `hermes -p <profile> plugins enable platforms/wechat_golem`（tool override 选 **n**）→
   `hermes -p <profile> gateway restart` → 验 `curl http://<桥地址>/health` 的 `subscribers ≥ 1`。

> **捷径词表两侧必须一致**：`interrupt_tokens` / `session_reset_tokens` / `archive_tokens`
> （桥 `config.toml`）与 `WECHAT_GOLEM_INTERRUPT_TOKENS` / `_RESET_TOKENS` / `_ARCHIVE_TOKENS`
> （Hermes `.env`）。只改一侧时桥的群门闩会先吞掉消息，适配器根本收不到。
> 适配器连上时会比对并在 `gateway.log` 告警；`GET /health` 的 `tokens` 字段是桥的生效词表。

媒体：适配器支持 `url` 或本地文件（桥侧 `url` / `data_b64`）；Golem 在桥所在机器下载/发送。  
图片/视频/语音/表情均走 `message.Send`（表情 `TypeEmoji`，超限自动压缩：GIF 保动画重编码、静图 PNG 优先 / JPEG 降质兜底；视频填 `Duration` + ffmpeg 抽的 `Thumb`；语音必要时 ffmpeg→AMR）。  
不走 `cdn.Upload*`（历史实测 CDN 偶发 RST，message 路径更稳）。**桥所在机器需安装 ffmpeg/ffprobe**（缺失时语音/视频会明确报错；路径可用 `ffmpeg_path` / `ffprobe_path` 指定）。  
**斗图请走 `/send_emoji` 或 `wechat_send_emoji`**，不要用 `/send_image`（那是普通图片消息）。  
`/send_emoji` 支持 `raw: true`（v0.3.1+）：体积 ≤500KB 时原样发送，保住动图与原 md5——重发收藏的微信表情必须用。  
**大表情实测**（v0.3.3+）：>500KB 即使 raw 也强制压缩（GIF 保动画：合成帧→缩边→抽帧重编码；静图 PNG/JPEG）。原样上传 2MB 表情微信回 OK 但无 NewId、不上屏（自定义表情上限约 1MB，「原 md5 引用发送」在本后端不成立）；桥现将 NewId=0 视为真失败返回错误，不再假成功。任意网图保持默认压缩（超边长不压会不显示）。
**压缩画质**（v0.3.4+）：GIF 重编码帧用面积平均缩放（近邻会把源图自带抖动纹理混叠成横竖网点）、按帧实际颜色建调色板（≤256 色精确映射，超出中位切分量化），全程**不做 Floyd-Steinberg 抖动**（表情尺寸小，抖动即可见噪点且拖累 LZW 压缩率）；旧版误用增量帧局部调色板+强制抖动导致满屏噪点。静图同样面积平均缩放并优先 PNG 无损（保透明），超限再退 JPEG。

媒体发送策略（防叠发）：正式超时图片 45s / 视频 120s；超时后再 grace 30s 只等同一 in-flight 回执，**超时不重开 Send**（真失败最多再试 1 次）。  
因此大图/慢网下微信侧最多 1 份；可能出现「聊天已有媒体，但诊断回 outcome=2 超时未确认」。HTTP 出站失败且带 URL 时仍可能降级发文本链接。

## 群成员偏好档案（适配器）

跨 session 记群友喜好/性格：工具 `wechat_member_profile_*`，落盘 `$HERMES_HOME/wechat_member_profiles/`。
主人整句「归档」批量写入（桥旁路门闩 + 适配器扩指令）；「新开会话」不清档案。
详见 `wechat_golem/README.md` 与 [DEPLOY.md](DEPLOY.md)。
