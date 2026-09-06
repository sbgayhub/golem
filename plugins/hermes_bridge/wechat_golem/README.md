# wechat_golem — Hermes 平台适配器

把微信（经 Golem `hermes_bridge`）接到 Hermes 官方 Gateway 平台适配器范式。

- 产品级桥说明：`plugins/hermes_bridge/readme.md`
- **部署与排障**：`../DEPLOY.md`

## 架构

```
微信 ↔ Golem host
         └ hermes_bridge 插件
              ├ GET  /events   SSE 入站
              ├ POST /send     出站文本（mentions=wxid 真 @）
              ├ POST /send_image|video|voice|emoji
              ├ POST /send_app | /send_record | /send_quote
              ├ GET  /self | /group_info | /group_members
              ├ POST /group_member_detail
              └ GET  /health   探活 + 生效捷径词表 + 外部工具状态
                    ↕ 同机 loopback 或跨机 LAN
Hermes gateway + $HERMES_HOME/plugins/platforms/wechat_golem
```

**真 @**：可靠路径是最终回复正文写 `@显示名` / `@wxid` / `[[mentions:wxid]]`，适配器解析后 POST 桥 `mentions`（`metadata.mentions` 可选但文本路径通常带不上）。模型应先 `wechat_group_members` 查 wxid（对用户勿念 wxid）。**查询/发送 tool**：`wechat_self_info` / `wechat_group_info` / `wechat_group_members` / `wechat_group_member_detail` / `wechat_send_emoji` / `wechat_send_music` / `wechat_send_record` / `wechat_send_quote` / `wechat_send_voice` / `wechat_revoke`（named schema + session-map 兜底 `chat_id`）。斗图必须 `wechat_send_emoji`（TypeEmoji），勿用发图冒充。长列表/嵌图用 `wechat_send_record`（聊天记录卡片 type=19；图片 `type=image`+`url`/`media_ref`，勿 data_b64）。引用气泡用 `wechat_send_quote`（type=57；`svrid`=入站 `msg_id`）。agent 验收勿 curl 桥。

## 安装

### 1. Golem 侧

```bash
# 构建
cd plugins && task build:hermes_bridge

# plugins/config.toml 启用 hermes_bridge，填 token / listen
# 旧 hermes 插件可保留但已 OnLoad 直接弃用 return
```

插件配置示例：

```toml
[hermes_bridge.config]
listen = "0.0.0.0:8643"
token = "随机长串"
max_text_len = 2000
send_rate_per_min = 20
max_body_bytes = 83886080

# 白名单用微信命令管理：
# /hermes enable
# /hermes status
```

### 2. Hermes 侧（VM）

**只装一份，路径必须带 `platforms/`**（与 `config.yaml` 里
`plugins.enabled: [platforms/wechat_golem]` 对齐）。

profile `wechat` 时 `HERMES_HOME=~/.hermes/profiles/wechat`：

```bash
# ✅ 正确（唯一应保留的副本）
mkdir -p "$HERMES_HOME/plugins/platforms/wechat_golem"
cp plugin.yaml adapter.py "$HERMES_HOME/plugins/platforms/wechat_golem/"   # manifest 必须全小写
# loader 要包名：__init__.py 必须与 adapter.py 同内容
cp "$HERMES_HOME/plugins/platforms/wechat_golem/adapter.py" \
   "$HERMES_HOME/plugins/platforms/wechat_golem/__init__.py"

# ❌ 不要装这些（会与正确路径并存，改错文件导致「修了不生效」）
# ~/.hermes/plugins/wechat_golem/
# $HERMES_HOME/plugins/wechat_golem/          # 少了 platforms/
# ~/.hermes/plugins/platforms/wechat_golem/   # 全局第二份，易重复
```

```bash
# $HERMES_HOME/.env
WECHAT_GOLEM_TOKEN=与桥一致
WECHAT_GOLEM_BASE_URL=http://127.0.0.1:8643   # 分机部署填桥所在机器地址
WECHAT_GOLEM_HOME_CHANNEL=主人wxid   # 或群 chatroom
WECHAT_GOLEM_ALLOW_ALL_USERS=true
WECHAT_GOLEM_ALLOWED_USERS=主人wxid
HERMES_EXEC_ASK=1
```

持久数据目录（默认都在 profile 内，迁移/备份要一并搬；完整清单见 `plugin.yaml`）：
`WECHAT_GOLEM_STICKER_DIR`（表情库）、`WECHAT_GOLEM_MEMBER_PROFILE_DIR`（群友档案）、
`WECHAT_GOLEM_MEDIA_DIR`（入站媒体缓存）。

`config.yaml` 要点：

```yaml
plugins:
  enabled:
    - platforms/wechat_golem
group_sessions_per_user: false   # 顶层；否则群里每成员一条 session、上下文串台
session_reset:
  mode: none
approvals:
  mode: manual   # smart 会静默放行危险命令，测审批时用 manual
```

```bash
hermes -p wechat plugins enable platforms/wechat_golem   # tool override 选 n
hermes -p wechat gateway restart
# 稳定后
hermes -p wechat gateway install   # systemd user 服务
```

## 命令（Golem）

| 命令 | 说明 |
|---|---|
| `/hermes status` | 桥状态、SSE 订阅数、白名单 |
| `/hermes enable [名称]` | 当前会话进路由白名单 |
| `/hermes disable` | 移出白名单 |
| `/hermes image <url>` | 诊断直发图片 |
| `/hermes video <url>` | 诊断直发视频 |
| `/hermes help` | 帮助 |

Host 层限制：仅主人可跑 `/` 命令；**未注册的 `/xxx` 会回「未知命令」且不会进 Hermes**。

## 撤回

- agent 侧：`wechat_revoke`（无参=撤本会话最近一条；`count` 撤多条；`message_id` 指定某条）→ 桥 `POST /revoke`
- 主人侧：微信整句 **`撤回`** 由**桥**直接处理，**不推 SSE**——agent 的历史里会留着那条已被撤掉的消息，platform_hint 已提醒它别据此以为消息还在
- 微信限时约 2 分钟；桥自己卡窗口（`revoke_window_seconds`）并把原因写进 `error`，可原样转告用户
- 只能撤机器人自己发的（桥出站记账里的）；撤成功微信自带提示，别再发「已撤回」

## 审批

- Gateway：`HERMES_EXEC_ASK=1` + `approvals.mode: manual`
- 危险命令阻塞时，`send_exec_approval` 发中文文本卡
- 主人回复 **`yes` / `no` / `session` / `always`**（**不要加 `/`**）
  - Golem 会拦截 `/approve`、`/deny`（未知命令）
- hardline 永不放行

## 入站交付（单飞，项 2 已验收）

Hermes `handle_message` 是 fire-and-forget。适配器对同会话串行 **整轮 agent**
（busy 判定按 chat_id 收窄到同会话：task / guard / blocking approval /
running_agents 的 key 均含 chat_id，命中才算忙；chat_id 空时退回整 adapter 兜底，
一个群的长任务/审批不再阻塞其他会话）：

- **私聊默认不去抖**（`DEBOUNCE_MS=0`）。
- **单飞 + pending**：busy 时后续只入队；**idle 后**合并一批再送（防 ⚡）；投递前也先查同会话 busy（防 cron 等跨入口撞上 busy=interrupt）。
- 合并批把前面消息的 `media_ref`/`msg_id`/引用内嵌进对应正文（防「先发图再补话」丢图）；`metadata.merged_media_refs` 带全量列表。
- 审批 `yes/no/…`：**仅主人**且确有待审批项才旁路立即；否则群内忽略、私聊转普通消息。
- 投递失败（handle_message 抛异常）整批回队、退避重试（5s 起、上限 60s），不丢消息。
- 出站：字面 `\n` → 真换行。
- 细节与验收：`../DEPLOY.md`。

| 环境变量 | 默认 | 说明 |
|---|---|---|
| `WECHAT_GOLEM_DEBOUNCE_MS` | `0` | 私聊去抖；一般保持 0 |
| `WECHAT_GOLEM_GROUP_DEBOUNCE_MS` | `0` | 群去抖；桥侧已合并 |
| `WECHAT_GOLEM_INTERRUPT_TOKENS` | `打断` | 整句打断词（逗号分隔） |
| `WECHAT_GOLEM_RESET_TOKENS` | `新开会话,新对话` | 整句新开会话词（仅主人；CLI prune --chat-id 清历史后桥回执，不投 agent） |
| `WECHAT_GOLEM_STICKER_DIR` | `~/.hermes/wechat_stickers` | 表情收藏库目录（`<md5>.<ext>` + `index.json`：`moods` 情绪 / `tags` 题材标记 / desc…） |
| `WECHAT_GOLEM_MEMBER_PROFILE_DIR` | `$HERMES_HOME/wechat_member_profiles` | 群成员偏好档案目录（每成员一个 `<wxid>.json`） |
| `WECHAT_GOLEM_CHAT_FALLBACK_TTL_S` | `15` | 出站 `chat_id` 退到「最近入站会话」的窗口秒数（见下节） |

## 出站媒体（MEDIA: 标记）

**模型侧只需一个 `MEDIA:`**：一行一个、写几个发几个，类型自动判，不用记桥接口。

| 形态 | 谁抽 | 结果 |
|---|---|---|
| `MEDIA:<https 直链>` | 适配器 `send()` | 按类型 → 桥 `/send_image`、`/send_video`、`/send_voice` |
| `MEDIA:/绝对路径` | 官方核心 `extract_media` | 图片 → 本适配器 `send_image_file`；`.mp4` → `send_video` |
| `![alt](url)` | 官方核心 `extract_images` | `send_multiple_images` → 本适配器 `send_image`（多张逐张） |
| `VIDEO:<url>` | 适配器 `send()` | 仅当 URL 看不出类型时才需要，等价于「这是视频」的提示 |

类型两级判定：**扩展名**（剥 `?query#frag` 后取后缀；表照抄官方 `_IMAGE_EXTS`/`_VIDEO_EXTS`/`_AUDIO_EXTS`）→ **magic bytes**（无扩展名时下载后嗅 PNG/JPEG/GIF/WebP/`ftyp`/EBML/OggS…，字节复用走 `data_b64`，不拉第二遍）。嗅不出按图片发。

**URL 分支为什么必须自己写**：官方 `extract_media` 的路径锚点只有 `~/`、`/`、`X:\`，`MEDIA:<url>` 官方核心抽不到（实测匹配为空）；视频 URL 官方更是零通道（`extract_images` 只抽图片、`send_video` 只吃本地路径）。而官方 image_gen 文档明确教模型「拿到 URL 后 emit `MEDIA:<url>`」——这一半归适配器补。

行为要点：

- **多个标记全发**（`finditer`）。旧版 `.search()` 只取第一个，其余留在剩余正文里当 caption 发出去，就是「让它发多张图、微信只收到一段文字」的根因。
- **先文字、再逐个媒体**，与官方 `_send_final_text` → `_deliver_attachments` 同序；不再把剩余正文塞 `caption`（多图时说明会黏在第一张后面）。
- 正则鲁棒性对齐官方：容忍 `**MEDIA:…**` 强调包裹、CJK 全角标点终结（`（）：，。`）、`MEDIA:`/`VIDEO:` 互为边界防 `MEDIA:aMEDIA:b` 粘连。
- 文字已发出后媒体失败**不回 failure**（否则 Hermes 重试整条 `send`，文字发第二遍），只记 warning；桥侧带 `url` 时还会降级补一条链接文本。
- 微信没有文件通道：`MEDIA:/x.pdf` 会被官方核心路由到未实现的 `send_document`，基类回一句发送失败——文档走 `wechat_send_record` 或给链接。
- 日志：`grep -a "outbound media tags" gateway.log` 看抽到几个、类型是什么；`媒体类型嗅探` 是走了 magic bytes 那一级。

**模型以前为什么爱调表情包工具**：插件平台的 `platform_hint` 是**替代**官方 hint 而非追加——`agent/system_prompt.py::_platform_hint()` 先查 `PLATFORM_HINTS.get(platform_key)`，命中就用官方的，**没命中才**去 registry 取插件的。`wechat_golem` 不在官方表里，官方那句 “You can send files natively: write MEDIA:/absolute/path/to/file” 我们一个字都拿不到，而旧 hint 里唯一提到 `MEDIA:` 的地方是一句禁令。于是模型只剩「找工具」，而工具表里唯一能发图的就是 `wechat_send_emoji`。现在 `platform_hint` 有独立的【发媒体】块，`wechat_send_emoji` / `wechat_sticker_send` 描述开头也写明「只发表情包；普通图片写 MEDIA:」。

> 调措辞不必每次重拷 adapter.py：`config.yaml` 支持 `platform_hints.<platform>` 覆盖（`replace` / `append`，裸字符串=append），可先 append 试效果、定稿再落回代码（读 `_resolve_platform_hint` 实现所得，未实测）。

## 出站目标（发给谁）

`wechat_send_*` / `wechat_sticker_send` / `wechat_revoke` 的 `chat_id` **必填**，取消息前缀里
的 `chat_id:` 行。历史上它是「可省略、从当前 session 推断」，模型于是普遍不填，适配器一路
兜底到进程级的「最近一次入站会话」——那个变量被任何会话的新消息覆盖，于是：在 A 群让它发
表情，你转头在 B 群说句话，表情就发进了 B 群。

现在按可信度分三层定位本轮会话（`resolve_outbound_target`）：

1. `contextvar`——投递事件前绑定，随 asyncio task 继承，最准
2. **session 登记表**——`session_key` / opaque `session_id` → `chat_id`；只认精确或带 `:`
   边界的匹配（裸后缀会让 `98765@chatroom` 命中 `12398765@chatroom`）
3. **唯一在途会话**——`_INFLIGHT` 账本，给跑在线程池里、拿不到 contextvar 的 handler 兜底

模型给的 `chat_id` 与本轮会话不一致时**纠回本轮会话**并打 warning；确需发到别处要显式传
`allow_cross_chat=true`。三层都空时才退到「最近入站」，且要求窗口内（默认 15s）且没有第二个
会话在途，否则 tool 直接报错让模型补 `chat_id`——猜错会话比报错贵得多。

出站请求还会带上本轮 `session_key`，桥侧再校验一次归属，不匹配拒发（见 `../readme.md`）。
日志：`grep -a "出站目标" gateway.log`，`source=ctx|session_map|inflight` 是可信定位，
`recent` 是短窗兜底。

> **回退开关**：`chat_id` 进 schema `required` 是 2026-09-05 新加的。历史上刻意留空
> `required`，怕 Hermes 某些 dispatch 形态吞参后模型直接拒调。若出现拒调或 schema 报错，
> 把 `adapter.py` 里那 6 处 `"required": ["chat_id"]` 改回 `[]` 即可 —— handler 侧的受闸
> 兜底仍在，发送不会因此失败（代价只是模型又倾向不填，退回靠三层定位）。

**表情库约定**（实现见 `adapter.py`）：

- 工具：`wechat_sticker_save` / `list` / `send` / `delete`
- **自主应景** → `send(mood=…)` 对齐 `moods`；**主人点名标签** → `send(tag=…)` 对齐 `tags`
- 收藏时应带 `moods`（提示约束，非强制识图）；`tags` 留给题材/自定义标记
- 成功发表情后勿旁白；只需表情时最终 `NO_REPLY`

**群成员偏好档案**（为何不塞官方 `USER.md`）：

- Hermes 官方持久记忆只有两份：`MEMORY.md`（agent 笔记，~2200 字）与 `USER.md`（**主人**档案，~1375 字），会话开始冻结注入；装不下「每个群友一份」。
- 适配器另建按 wxid 落盘的档案：`wechat_member_profile_get` / `upsert` / `list` / `delete`
- 入站自动把本批发言人的已知喜好/性格注入消息前缀；**新开会话 / 压缩不清**（与官方 memory 一样独立于 session）
- 只记长期有用的（明确偏好、稳定性格）；跳过一次性吐槽
- **学到就写**：对方明确偏好 / 性格反复出现时模型应**当轮** `upsert`，主人一般不用额外操作
- **归档捷径**（主人整句，桥+适配器）：`归档` / `归档群友` / `记群友` / `记成员` / `归档成员`  
  → 立即进 agent，扩成批量 upsert 指令；词表可用 `WECHAT_GOLEM_ARCHIVE_TOKENS` 覆盖（须与桥默认同步）  
  推荐：会话变长或要清会话前，先发 `归档`，等它回「已归档 N 人」，再发 `新开会话`

## 与旧 hermes 插件差异

| | 旧 hermes | 新 hermes_bridge + wechat_golem |
|---|---|---|
| 入站 | 插件 POST OpenAI API Server | SSE → 官方 MessageEvent |
| 出站 | 内嵌 MCP tools | 适配器 HTTP POST 桥 |
| 权限档 | 提示词 privilegeClause | Hermes 原生 approval |
| run 队列 | 插件串行 | Gateway session 管理 |
| cron | run_token / proactive | home_channel + standalone_sender（仅文本） |
