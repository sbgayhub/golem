# wechat_golem — Hermes 平台适配器

把微信（经 Golem `hermes_bridge`）接到 Hermes 官方 Gateway 平台适配器范式。

- 产品级桥说明：`plugins/hermes_bridge/readme.md`
- **部署与踩坑（现行）**：`t-doc/hermes-bridge-notes.md`
- 旧 MCP 方案历史：`t-doc/hermes-plugin-notes.md`（已弃用，勿按该文部署）

## 架构

```
微信 ↔ Golem host (Windows)
         └ hermes_bridge 插件
              ├ GET  /events   SSE 入站
              ├ POST /send     出站文本（mentions=wxid 真 @）
              ├ POST /send_image|video|voice|emoji
              ├ POST /send_app | /send_record | /send_quote
              ├ GET  /self | /group_info | /group_members
              ├ POST /group_member_detail
              └ GET  /health
                    ↕ LAN
Ubuntu VM: Hermes gateway + $HERMES_HOME/plugins/platforms/wechat_golem
```

**真 @**：可靠路径是最终回复正文写 `@显示名` / `@wxid` / `[[mentions:wxid]]`，适配器解析后 POST 桥 `mentions`（`metadata.mentions` 可选但文本路径通常带不上）。模型应先 `wechat_group_members` 查 wxid（对用户勿念 wxid）。**查询/发送 tool**：`wechat_self_info` / `wechat_group_info` / `wechat_group_members` / `wechat_group_member_detail` / `wechat_send_emoji` / `wechat_send_music` / `wechat_send_record` / `wechat_send_quote` / `wechat_send_voice`（named schema + session-map 兜底 `chat_id`）。斗图必须 `wechat_send_emoji`（TypeEmoji），勿用发图冒充。长列表/嵌图用 `wechat_send_record`（聊天记录卡片 type=19；图片 `type=image`+`url`/`media_ref`，勿 data_b64）。引用气泡用 `wechat_send_quote`（type=57；`svrid`=入站 `msg_id`）。agent 验收勿 curl 桥。

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
mkdir -p ~/.hermes/profiles/wechat/plugins/platforms/wechat_golem
cp PLUGIN.yaml adapter.py \
  ~/.hermes/profiles/wechat/plugins/platforms/wechat_golem/
# 若 loader 要包名，可再：cp adapter.py .../__init__.py

# ❌ 不要装这些（会与正确路径并存，改错文件导致「修了不生效」）
# ~/.hermes/plugins/wechat_golem/
# ~/.hermes/profiles/wechat/plugins/wechat_golem/
# ~/.hermes/plugins/platforms/wechat_golem/   # 全局第二份，易重复
```

```bash
# ~/.hermes/profiles/wechat/.env
WECHAT_GOLEM_TOKEN=与桥一致
WECHAT_GOLEM_BASE_URL=http://192.168.47.1:8643
WECHAT_GOLEM_HOME_CHANNEL=主人wxid   # 或群 chatroom
WECHAT_GOLEM_ALLOW_ALL_USERS=true
WECHAT_GOLEM_ALLOWED_USERS=主人wxid
HERMES_EXEC_ASK=1
```

`config.yaml` 要点：

```yaml
plugins:
  enabled:
    - platforms/wechat_golem
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
- 细节与验收：`t-doc/hermes-bridge-notes.md` §十。

| 环境变量 | 默认 | 说明 |
|---|---|---|
| `WECHAT_GOLEM_DEBOUNCE_MS` | `0` | 私聊去抖；一般保持 0 |
| `WECHAT_GOLEM_GROUP_DEBOUNCE_MS` | `0` | 群去抖；桥侧已合并 |
| `WECHAT_GOLEM_INTERRUPT_TOKENS` | `打断` | 整句打断词（逗号分隔） |
| `WECHAT_GOLEM_RESET_TOKENS` | `新开会话,新对话` | 整句新开会话词（仅主人；CLI prune --chat-id 清历史后桥回执，不投 agent） |
| `WECHAT_GOLEM_STICKER_DIR` | `~/.hermes/wechat_stickers` | 表情收藏库目录（`<md5>.<ext>` + `index.json`：`moods` 情绪 / `tags` 题材标记 / desc…） |
| `WECHAT_GOLEM_MEMBER_PROFILE_DIR` | `$HERMES_HOME/wechat_member_profiles` | 群成员偏好档案目录（每成员一个 `<wxid>.json`） |

**表情库约定**（实现见 `adapter.py`，踩坑与工具表见 `t-doc/hermes-bridge-notes.md`）：

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
