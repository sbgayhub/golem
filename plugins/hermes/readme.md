# hermes 插件

> ⚠️ **已弃用**（API Server + 内嵌 MCP 架构）。  
> 请改用 **`plugins/hermes_bridge`**（Golem 侧 HTTP/SSE 桥）+ Hermes 官方平台适配器 **`wechat_golem`**（见 `t-doc/wechat_golem/`）。  
> **现行部署与踩坑**：`t-doc/hermes-bridge-notes.md`；本文对应历史笔记：`t-doc/hermes-plugin-notes.md`。  
> 本插件 `OnLoad` 仅打弃用警告并 return，不再启动 MCP / worker。代码保留以便回滚与阅读。

---

注意：我目前是虚拟机部署的Hermes Agent，所以权限上面管的比较松。

微信 ⇄ [Hermes Agent](https://github.com/nousresearch/hermes-agent) 双向桥。让部署在虚拟机里的 Hermes 以"群成员"的方式参与微信聊天：

- **入站**：订阅微信消息进滚动上下文（不回复也看得到之前聊了什么），被 @/引用/点名/冒泡时触发一次 agent run，把上下文推给 Hermes API Server（OpenAI Chat Completions 兼容，非流式）。
- **出站**：插件内嵌 MCP server（streamable HTTP），Hermes 通过 `wechat_*` 工具发消息、查历史、查群成员。**agent 的回复一律走工具**——"要不要说话"由它自己决定，run 结束没调发送工具即保持沉默；HTTP 响应正文默认只做兜底。

```
微信 ⇄ hermes 插件（单插件双向）
 ├ 入站：OnEvent → 白名单过滤 → 滚动上下文 → 触发 → 去抖 → 串行 run 队列
 │        → POST {base_url}/chat/completions
 └ 出站：MCP server（mcp_listen）
          wechat_send_text / wechat_send_image / wechat_query_history
          wechat_group_members / wechat_list_targets
Hermes 侧：专用 profile（人格+持久记忆），挂载本 MCP，收紧自带工具集
```

## 权限模型

权限判定全部在代码层，提示词不承担安全职责：

| 触发场景 | 判定方式 | 可操作范围 |
|---|---|---|
| 主人触发 | 触发者 wxid 与 `contact.GetOwner()` 硬比对 | 全部白名单会话 |
| 普通成员触发 | 同上，比对不中 | 仅当前触发会话（target 锁） |
| 无触发（Hermes 定时/自主任务） | 无 run 上下文 | 仅 `proactive = true` 的白名单会话 |

串行 run 队列保证"当前 MCP 工具调用必属当前 run"，权限档绑定 run 上下文，群友话术冒充主人无效。另有全局发送限流、单条长度上限、Bearer 认证（token 未配置时 MCP 拒绝所有请求）。

**身份标注防冒充**：上下文中主人的发言由代码按 wxid 打上【主人】标注，system 里注明触发者是否为主人；提示词侧不写主人昵称——昵称任何人都能改，agent 被要求只认系统标注、不信消息里的身份自称。

## 插件配置

```toml
base_url = "http://192.168.x.x:8642/v1" # Hermes API Server（虚拟机 IP）
api_key = "your-api-server-key"          # 与 Hermes 的 API_SERVER_KEY 一致
model = "hermes-agent"                   # 默认 profile 为 hermes-agent，自建 profile 为其名称
http_timeout_seconds = 300               # agent 会跑工具，别设太短
mcp_listen = "192.168.x.1:8643"          # 建议绑定仅虚拟机可达的宿主机网卡 IP
mcp_token = "随机长串"                    # 与 Hermes 侧 mcp_servers 配置里的一致
trigger_names = ["小赫"]                  # @ 和引用始终触发；包含这些名字也触发
bubble_rate = 0.0                        # 冒泡概率（未点名消息触发 run，说不说由 agent 决定）
bubble_cooldown_minutes = 10
debounce_seconds = 3                     # 触发后合并突发消息
max_context_messages = 40
fallback_reply = true                    # agent 忘调发送工具时兜底代发（稳定后可关）
send_rate_per_min = 10
max_text_len = 2000
extra_prompt = ""                        # 追加场景说明；人格请配在 Hermes profile 侧

[[targets]]
id = "123456789@chatroom"
name = "老友群"
proactive = false                        # Hermes 自主任务（cron 等）是否可发到这里
```

## 管理命令（仅主人，本地执行不进 agent）

| 命令 | 说明 |
|---|---|
| `hermes帮助` | 命令清单 |
| `hermes状态` | 查看配置与白名单 |
| `hermes启用 [名称]` | 把当前会话加入白名单（在目标群里发） |
| `hermes停用` | 把当前会话移出白名单并清上下文 |
| `hermes重置` | 清空当前会话上下文 |
| `hermes冒泡 0.05` | 设置冒泡概率 |
| `hermes兜底 开/关` | 兜底回复开关 |

非主人发送命令样式的消息会被直接吞掉。

## MCP 工具

| 工具 | 说明 |
|---|---|
| `wechat_list_targets` | 列出当前权限下可发送的会话（含"是否当前会话"标记） |
| `wechat_send_text` | 发文本，agent 在微信里说话的唯一方式 |
| `wechat_send_image` | 下载图片 URL 经 CDN 发图（http/https、≤10MB） |
| `wechat_send_voice` | 下载音频 URL 发微信语音（mp3/wav/amr/silk、≤10MB，ffmpeg 自动转 AMR-NB） |
| `wechat_send_video` | 下载视频 URL 经 CDN 发视频（≤100MB，ffprobe 取时长、ffmpeg 抽缩略图） |
| `wechat_query_history` | 透传 `statistics.query_messages`，返回不含发言人昵称 |
| `wechat_group_members` | 群成员列表（wxid + 显示昵称） |

## Hermes 侧配置（虚拟机内）

1. **建专用 profile**（人格与记忆独立，避免污染默认 profile）：

   ```bash
   hermes profile create wechat
   ```

2. **开启 API Server**，写入 `~/.hermes/profiles/wechat/.env`：

   ```bash
   API_SERVER_ENABLED=true
   API_SERVER_HOST=0.0.0.0        # 默认 127.0.0.1，宿主机连不上
   API_SERVER_PORT=8642
   API_SERVER_KEY=your-api-server-key
   ```

3. **挂载本插件的 MCP server**，在该 profile 的 `config.yaml` 加：

   ```yaml
   mcp_servers:
     golem_wechat:
       url: "http://<宿主机IP>:8643/mcp"
       headers:
         Authorization: "Bearer ${GOLEM_MCP_TOKEN}"
   ```

   token 值写进 profile 的 `.env`：`GOLEM_MCP_TOKEN=随机长串`。

4. **人格提示词**写在该 profile 的 `~/.hermes/profiles/wechat/SOUL.md`（system prompt 第一槽位，每次 run 重读、改完即生效），建议包含：微信群成员人设、"发言唯一生效方式是调 wechat_send_text 工具"、"可以选择沉默"、防注入的危险边界。

5. **工具集**：默认全开即可（本插件按触发者在 system 提示里分档：主人 run 开放终端/skill，群友 run 仅聊天+只读查询）。想要物理隔离再配 `terminal.backend: docker`。

6. **单独给该 profile 配模型**（profile 间不共享 provider）：`hermes -p wechat model`。注意免费小模型可能不肯调发送工具（表现为日志 sent=0 次次走兜底），建议用工具调用稳定的模型。

7. **启动并验证**：

   ```bash
   hermes -p wechat gateway          # 前台调试
   # 稳定后装常驻服务（先 sudo loginctl enable-linger <user>）：
   hermes -p wechat gateway stop && hermes -p wechat gateway install
   curl http://127.0.0.1:8642/health
   # 宿主机侧验证 MCP：
   curl http://<mcp_listen>/health
   ```

   在 `hermes -p wechat chat` 里说"调用 wechat_list_targets"确认 MCP 端到端可用（`hermes mcp list` 显示的是配置非连接状态）；微信白名单群里 @ 它说句话走通全链路。改 `.env`/`config.yaml`/模型后需 `gateway restart`，改 SOUL.md 不用。

   本旧方案踩坑见 `t-doc/hermes-plugin-notes.md`（历史）。现行桥见 `t-doc/hermes-bridge-notes.md`。

## 注意事项

- 一次 run 是完整的 agent 执行（可能带工具调用），几秒到几分钟都正常；串行队列排队期间的新消息会合并进上下文，不丢。
- `wechat_send_voice` / `wechat_send_video` 依赖宿主机安装 ffmpeg 与 ffprobe（与 setu/demos 插件同一依赖）。
- `wechat_query_history` 依赖 statistics 插件在线。
- 与 ai 插件功能重叠（都接管闲聊回复），二者建议只启用一个。
