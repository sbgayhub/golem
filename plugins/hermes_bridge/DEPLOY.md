# hermes_bridge + wechat_golem 部署指南

把微信接到 [Hermes](https://github.com/) agent 网关。两个部件：

| 部件 | 跑在哪 | 是什么 |
|---|---|---|
| `hermes_bridge` | Golem host 所在机器 | Golem 插件，提供 HTTP/SSE 桥 + 本机管理台 |
| `wechat_golem` | Hermes gateway 所在机器 | Hermes 官方平台适配器（Python） |

两者**可同机也可分机**。同机时桥地址就是 `http://127.0.0.1:8643`；分机时填桥所在机器对
Hermes 可达的地址。本文不假设任何特定网络拓扑。

可选第三个部件 `hermes_ops`（Hermes 侧只读运维服务），让桥的管理台能看 gateway 状态、
日志尾、表情库与群友档案。不装则管理台的「Hermes」页不可用，其余功能不受影响。

---

## 一、前置依赖

**Golem 侧（桥）**

- Go 工具链与 `task`（构建插件用）
- **ffmpeg / ffprobe**：语音转码、视频封面与时长。缺了语音和视频发不出去。
  装好后要么进 `PATH`，要么在配置里写 `ffmpeg_path` / `ffprobe_path`。
- 可选 `silk_v3_encoder`：语音优先编码成微信原生的腾讯变体 SILK（音质与兼容性更好）。
  没有则自动降级 ffmpeg AMR。需自行编译，路径填 `silk_encoder_path`。

**Hermes 侧（适配器）**

- Python 3.9+ 与 `aiohttp`（`pip install aiohttp`）
- 一个已能跑的 Hermes profile

自检：桥装好后在微信发 `/hermes status`，末行会报 `媒体工具: ffmpeg✓ ffprobe✓ silk✗`。
也可 `curl http://<桥地址>/health`，返回的 `media_tools` 有每个工具的解析结果或错误原因。

---

## 二、Golem 侧：构建与配置

```bash
cd plugins
task build:hermes_bridge
# → host/plugins/golem_plugin_hermes_bridge.exe
```

重载或重启 host 使新 exe 生效。

配置写在 host 的 `plugins/config.toml`（键名与 `main.go` 的 `Config` 一致）：

```toml
[hermes_bridge.config]
# ---- 业务口：给 Hermes 适配器连 ----
listen = "0.0.0.0:8643"        # 同机部署可收紧为 127.0.0.1:8643
token  = "长随机串"             # 必填，须与 Hermes 侧 WECHAT_GOLEM_TOKEN 一致；留空则拒绝所有请求
max_text_len = 2000
send_rate_per_min = 20
max_body_bytes = 83886080      # 80MB，含 base64 媒体

# ---- 本机管理台：与业务口分离 ----
admin_listen = "127.0.0.1:8644"  # 空=关闭；要远程访问请走 Tailscale/SSH 隧道，别直接绑 0.0.0.0
admin_token  = "另一条长随机串"    # 与业务 token 不同；留空则 /admin/* 全部拒绝

# ---- 外部程序：留空=在 PATH 中查找 ----
ffmpeg_path  = ""              # 如 "D:/tools/ffmpeg/bin/ffmpeg.exe" 或 "/usr/local/bin/ffmpeg"
ffprobe_path = ""
silk_encoder_path = ""         # 可选；留空则语音降级 AMR
silk_max_bytes  = 0            # 单条语音 SILK 上限，0=不限
silk_sample_rate = 24000       # 0 视为 24000

# ---- 群触发门闩 ----
trigger_names = []             # 如 ["小赫"]，群聊正文含这些名字也触发
bubble_rate = 0.1              # 未点名冒泡概率，0=关
bubble_cooldown_minutes = 10
debounce_seconds = 3           # 触发后合并窗口
max_context_messages = 40
group_push_all = false         # true=白名单群每条都推（关闭门闩，回滚用）

# ---- 斗图门闩 ----
emoji_burst_count = 3          # 窗口内第 N 条表情推一批，0=关
emoji_burst_window_seconds = 30
emoji_burst_cooldown_minutes = 5

# ---- 控制捷径词表：留空=用内置默认集 ----
# 改这些必须同步改 Hermes 侧同名 env，否则桥的群门闩会先把消息吞掉，
# 适配器根本收不到（症状：改了 env「完全没反应」）。适配器连上时会比对并告警。
interrupt_tokens     = []      # 默认 ["打断"]
session_reset_tokens = []      # 默认 ["新开会话", "新对话"]
archive_tokens       = []      # 默认 ["归档","归档群友","记群友","记成员","归档成员"]
approval_tokens      = []      # 默认 yes/no/是/否/同意/拒绝… 共 16 个

# ---- Hermes 只读运维页（可选，需装 hermes_ops）----
# hermes_ops_url   = "http://<hermes-host>:8650"   # 同机可用 127.0.0.1
# hermes_ops_token = "与 HERMES_OPS_TOKEN 一致"

# ---- 诊断（一般不用开）----
# record_xml_dump_dir = ""     # 出站记录卡片 XML 落盘目录（绝对路径）；空=不落盘

# targets（会话白名单）不必手写，用 /hermes enable 或管理台加
```

会话白名单：微信里发 `/hermes enable [名称]`，或打开管理台 `http://<admin_listen>/ui/`。
**主人私聊始终放行**，不需要 enable。

### 桥的微信命令（仅主人）

| 命令 | 说明 |
|---|---|
| `/hermes status` | token 掩码、SSE 订阅数、门闩、捷径词、媒体工具、白名单数量、管理台地址 |
| `/hermes enable [名称]` | 当前会话入白名单 |
| `/hermes disable` | 移出白名单 |
| `/hermes image\|video\|emoji <url>` | 诊断：下载并直发 |
| `/hermes help` | 帮助 |

> Host 会吞掉**所有未注册**的 `/` 命令并回「未知命令」，不会转发给 Hermes。
> 所以 Hermes 的审批**不能**用 `/approve`、`/deny`，只能回纯文本 `yes`/`no`。

---

## 三、Hermes 侧：安装适配器

**只装一份，路径必须带 `platforms/`**，与 `config.yaml` 的 `plugins.enabled` 对齐。
下面以 profile 名 `wechat` 为例，`$HERMES_HOME` 即 `~/.hermes/profiles/wechat`：

```bash
KEEP="$HERMES_HOME/plugins/platforms/wechat_golem"
mkdir -p "$KEEP"

# 从仓库拷入（仓库路径：plugins/hermes_bridge/wechat_golem/）
cp plugins/hermes_bridge/wechat_golem/PLUGIN.yaml \
   plugins/hermes_bridge/wechat_golem/adapter.py "$KEEP/"

# loader 要包名：__init__.py 必须与 adapter.py 同内容
cp "$KEEP/adapter.py" "$KEEP/__init__.py"
rm -rf "$KEEP/__pycache__"
```

不要同时存在下面这些副本，否则会改错文件、出现「修了不生效」：

```text
❌ ~/.hermes/plugins/wechat_golem/
❌ $HERMES_HOME/plugins/wechat_golem/          # 少了 platforms/
❌ ~/.hermes/plugins/platforms/wechat_golem/   # 全局第二份
```

环境变量（`$HERMES_HOME/.env`）：

```bash
# 必填
WECHAT_GOLEM_TOKEN=与桥 token 一致
WECHAT_GOLEM_BASE_URL=http://127.0.0.1:8643   # 分机部署填桥所在机器地址

# 常用
WECHAT_GOLEM_HOME_CHANNEL=主人wxid             # cron 默认投递会话
WECHAT_GOLEM_ALLOW_ALL_USERS=true
WECHAT_GOLEM_ALLOWED_USERS=主人wxid
HERMES_EXEC_ASK=1                              # 危险命令走审批
```

完整可选变量见 `wechat_golem/PLUGIN.yaml`（安装向导会逐项提示）与本文附录。

`config.yaml` 要点：

```yaml
plugins:
  enabled:
    - platforms/wechat_golem

# 群会话隔离：必须显式钉住，别靠默认值
group_sessions_per_user: false   # 一个群=一条共享 session；默认 true 会每成员一条 → 上下文串台
session_reset:
  mode: none                     # 不自动轮换会话

approvals:
  mode: manual                   # smart 会静默放行部分危险命令；调试审批链路用 manual
  timeout: 60                    # 到期 auto-deny，不会无限挂起
```

启用并重启：

```bash
hermes -p wechat plugins enable platforms/wechat_golem   # tool override 选 n
hermes -p wechat gateway restart
```

**必验**：重启后确认工具真注册上了（语法检查抓不到运行期 `NameError`）：

```bash
grep -a 'tool registered: wechat_\|registration crashed' "$HERMES_HOME/logs/agent.log" | tail
# 期望：tool registered: … verify=ok=True，且无 registration crashed
```

再确认适配器连上了桥：

```bash
curl -sS http://<桥地址>/health
# subscribers 应 ≥ 1；tokens 是桥的生效捷径词表
```

### 持久数据落在哪

三个目录默认都在 profile 内，**迁移和备份要一并搬**：

| 内容 | 默认路径 | 覆盖变量 |
|---|---|---|
| 表情收藏库 | `$HERMES_HOME/wechat_stickers` | `WECHAT_GOLEM_STICKER_DIR` |
| 群成员档案 | `$HERMES_HOME/wechat_member_profiles` | `WECHAT_GOLEM_MEMBER_PROFILE_DIR` |
| 入站媒体缓存（24h 自动清） | `$HERMES_HOME/wechat_inbound_media` | `WECHAT_GOLEM_MEDIA_DIR` |

同机跑多个 profile 时保持默认即可天然隔离。想让多个 profile 共享表情库，才把
`WECHAT_GOLEM_STICKER_DIR` 显式指到公共路径 —— 但注意跨进程写 `index.json` 无锁，
并发收藏可能丢更新。

---

## 四、可选：hermes_ops 只读运维服务

装在 Hermes 所在机器，让桥的管理台能看 gateway 状态与日志。完整文档见
`hermes_ops/README.md`，这里只列部署要点。

```bash
mkdir -p ~/.hermes/ops
cp plugins/hermes_bridge/hermes_ops/hermes_ops.py ~/.hermes/ops/

export HERMES_PROFILE=wechat          # 派生 HERMES_HOME 默认值与 gateway 单元名
export HERMES_OPS_TOKEN='长随机串'     # 非回环监听时必填，否则拒绝启动
python3 ~/.hermes/ops/hermes_ops.py
```

用 systemd 常驻：`cp hermes_ops/hermes-ops.service.example ~/.config/systemd/user/hermes-ops.service`
后改 Environment，然后 `systemctl --user daemon-reload && systemctl --user enable --now hermes-ops.service`。
**脚本放 `~/.hermes/ops/`，别拷进 systemd 单元目录。**

非 systemd 环境（容器 / macOS / WSL1 / supervisor）直接跑脚本即可：ops 会自动探测到没有
systemd 并跳过服务状态检查，不会让 `/overview` 恒亮红灯。

安全默认值：

- 默认只绑 `127.0.0.1:8650`。
- 无 token 时**只接受本机请求**；非回环监听且无 token 会**拒绝启动**
  （否则同网段任何人可读日志/会话/群友档案，并可写删档案）。
- 桥在另一台机器时，推荐保持回环 + SSH 隧道：`ssh -L 8650:127.0.0.1:8650 <hermes-host>`；
  或设好 token 后再放开 `HERMES_OPS_LISTEN`。

三个 token 互不相同：

| Token | 用途 | 配在哪 |
|---|---|---|
| `token` / `WECHAT_GOLEM_TOKEN` | 适配器 ↔ 桥业务口 | 桥 toml + Hermes `.env` |
| `admin_token` | 浏览器 ↔ 桥管理台 | 桥 toml + UI 登录框 |
| `hermes_ops_token` / `HERMES_OPS_TOKEN` | 桥 ↔ ops | 桥 toml + ops 环境变量 |

---

## 五、控制捷径（两侧词表必须一致）

微信里发这些整句可以旁路群门闩：

| 捷径 | 默认词 | 谁能用 | 作用 |
|---|---|---|---|
| 打断 | `打断` | 任何人 | 停当前 agent 任务，并作废该会话未推送的去抖批次 |
| 新开会话 | `新开会话` / `新对话` | 仅主人 | 就地重置该会话的 gateway 历史；长期记忆与群友档案不受影响 |
| 归档群友 | `归档` / `归档群友` / `记群友` / … | 仅主人 | 把当前上下文里的群友喜好写入档案 |
| 审批 | `yes` / `no` / `是` / `否` / `同意` / … | 仅主人 | 回应审批卡（**不要**加 `/`） |

这四组词**桥和适配器各存一份**，必须同步：

| 语义 | 桥（`config.toml`） | 适配器（`.env`） |
|---|---|---|
| 打断 | `interrupt_tokens` | `WECHAT_GOLEM_INTERRUPT_TOKENS` |
| 新开会话 | `session_reset_tokens` | `WECHAT_GOLEM_RESET_TOKENS` |
| 归档 | `archive_tokens` | `WECHAT_GOLEM_ARCHIVE_TOKENS` |
| 审批 | `approval_tokens` | （无 env，仅桥侧可配） |

**为什么必须同步**：桥承担群门闩。只改适配器一侧时，用户在群里发新词 → 桥不认识 →
不透传、不取消 pending → 消息被门闩吞掉 → 适配器那半边根本没机会执行。表现为
「这个 env 完全没用」，且没有任何错误。

适配器连上桥时会拉 `/health` 比对两侧词表，不一致就在 `gateway.log` 打 warning：

```
[wechat_golem] 捷径词表与桥不一致 kind=interrupt 桥=['打断'] 适配器=['停']（桥的群门闩会先吞掉桥不认的词；请对齐 …）
```

也可随时 `curl http://<桥地址>/health` 看 `tokens` 字段，或在管理台的配置页查看与热改。

---

## 六、排障速查

**适配器连不上桥（`/health` 的 subscribers=0）**

- 桥的 `token` 与 `WECHAT_GOLEM_TOKEN` 是否一致；桥 `token` 留空时会对所有请求回 503
- `WECHAT_GOLEM_BASE_URL` 是否指向桥**实际监听**的地址；分机部署时桥不能只绑 `127.0.0.1`
- 端口被占时桥只在 host 日志里 `Error`，插件仍显示「加载成功」——查 host 日志确认

**语音/视频发不出去**

- `/hermes status` 看 `媒体工具:` 那行。`ffmpeg✗` 或 `ffprobe✗` 就是没装或没进 `PATH`
- 装在非标准目录时配 `ffmpeg_path` / `ffprobe_path`（写到可执行文件本身，不是目录）
- 语音想要更好音质再配 `silk_encoder_path`；不配只是降级 AMR，不影响能发

**Hermes 侧改了代码不生效**

- 检查是否装了多份适配器（见第三节的 ❌ 列表）
- `__init__.py` 是否与 `adapter.py` 同内容；删掉 `__pycache__`
- 必须冷重启 gateway 才会重跑 `register(ctx)` 重新注册工具

**工具整片消失（agent 说没有 wechat_* 工具）**

注册被 `try/except` 吞掉的 traceback 不在 `gateway.log`，去这两个文件找：

```bash
grep -a 'registration crashed' "$HERMES_HOME/logs/errors.log" "$HERMES_HOME/logs/agent.log"
```

**日志在哪**

应用日志落文件，不在 journald：`$HERMES_HOME/logs/` 下的 `gateway.log` / `agent.log` /
`errors.log`。**grep 必须加 `-a`**：日志含非 UTF-8 字节，否则 grep 判定为二进制文件，
只回一句 `binary file matches` 并吞掉所有匹配行。

插件启动期的 `print(...)` 走 stdout → journald，用 systemd 跑时：
`journalctl --user -u hermes-gateway-<profile>.service -n 150 --no-pager`（漏了 `--user` 会恒空）。

**日志占地方 / 要不要清**

Hermes 自带轮转（`agent.log`、`gateway.log` 5MB，`errors.log` 2MB，滚 `.1`/`.2`/`.3`），
典型总占用几十 MB，**不必清、也不要再配 logrotate**——两套轮转管同一批文件会打架。
例外是 `gateway-exit-diag.log` / `gateway-shutdown-diag.log`，无轮转、只追加，
要清就 `truncate -s 0`（**别用 `rm`**：进程持着 fd，删了空间不释放且读不到新日志）。
详见 `hermes_ops/README.md` §6.5。

**排查日志里的告警时先看时间戳**

日志是追加的，历史告警会一直留在文件里。`grep` 出旧行不代表问题还在——先确认时间戳
是不是本次重启之后的，再看措辞是否与当前代码一致。换了适配器代码后，`adapter.py` 与
`__init__.py` **必须同时更新**：loader 加载的是 `__init__.py`，只换前者会继续跑旧代码，
表现为「文件明明是新的，日志还在报老问题」。

**「新开会话」报成功但没生效**

适配器依赖 gateway 的 `SessionStore` 私有成员，Hermes 升级改名后会失效。现在这种情况会
明确返回失败并说明原因，不再谎报「已重置」；成功时回执带上重置了几个 session。
启动时也会自检并在日志里报 `Hermes 内部结构自检未通过`（同一次启动只报一条）。

**群里上下文串台 / 一个群一堆 session**

`config.yaml` 顶层设 `group_sessions_per_user: false`（见第三节）。设为默认 `true` 时每个
群成员各一条 session，会并发多活会话。

**agent 隔几分钟就忘事**

不是 session 问题，是 `session_reset.mode: none` 下自动压缩把细节摘掉了。让它落持久记忆，
或查真源（如 cron 定义在库里，别靠模型回忆），而不是调大压缩阈值。

---

## 七、附录：环境变量与配置项全表

### 适配器（Hermes 侧 `.env`）

| 变量 | 默认 | 说明 |
|---|---|---|
| `WECHAT_GOLEM_TOKEN` | — | **必填**，与桥 `token` 一致 |
| `WECHAT_GOLEM_BASE_URL` | `http://127.0.0.1:8643` | **必填**，桥业务口地址 |
| `WECHAT_GOLEM_HOME_CHANNEL` | 空 | cron 默认投递会话 wxid |
| `WECHAT_GOLEM_HOME_CHANNEL_NAME` | 同 chat_id | home channel 显示名 |
| `WECHAT_GOLEM_ALLOWED_USERS` | 空 | 逗号分隔的允许用户 wxid |
| `WECHAT_GOLEM_ALLOW_ALL_USERS` | 空 | true=白名单会话内全员可聊 |
| `WECHAT_GOLEM_STICKER_DIR` | `$HERMES_HOME/wechat_stickers` | 表情收藏库目录 |
| `WECHAT_GOLEM_MEMBER_PROFILE_DIR` | `$HERMES_HOME/wechat_member_profiles` | 群成员档案目录 |
| `WECHAT_GOLEM_MEDIA_DIR` | `$HERMES_HOME/wechat_inbound_media` | 入站媒体缓存（24h TTL） |
| `WECHAT_GOLEM_INTERRUPT_TOKENS` | `打断` | 打断词；须与桥同步 |
| `WECHAT_GOLEM_RESET_TOKENS` | `新开会话,新对话` | 新开会话词；须与桥同步 |
| `WECHAT_GOLEM_ARCHIVE_TOKENS` | 5 个默认词 | 归档词；须与桥同步 |
| `WECHAT_GOLEM_SILENCE_TOKENS` | 空 | 追加沉默词；纯出站，可自由改 |
| `WECHAT_GOLEM_DEBOUNCE_MS` | `0` | 私聊去抖毫秒 |
| `WECHAT_GOLEM_GROUP_DEBOUNCE_MS` | `0` | 群去抖毫秒（桥已去抖） |
| `HERMES_HOME` | `~/.hermes` | Hermes profile 根（平台变量） |
| `HERMES_EXEC_ASK` | — | `1` 时危险命令走审批（Hermes 核心读） |

### hermes_ops（Hermes 侧环境变量）

| 变量 | 默认 | 说明 |
|---|---|---|
| `HERMES_PROFILE` | `wechat` | 派生下面三项的默认值 |
| `HERMES_OPS_LISTEN` | `127.0.0.1:8650` | 监听地址；非回环时必须设 token |
| `HERMES_OPS_TOKEN` | 空 | Bearer；空则只接受本机请求 |
| `HERMES_HOME` | `~/.hermes/profiles/$HERMES_PROFILE` | profile 根 |
| `HERMES_GATEWAY_UNIT` | `hermes-gateway-$HERMES_PROFILE.service` | systemd 单元名 |
| `HERMES_OPS_SERVICE_MANAGER` | `auto` | `auto`\|`systemd-user`\|`systemd-system`\|`none` |
| `HERMES_OPS_LOG_DIR` | `$HERMES_HOME/logs` | 日志目录 |
| `HERMES_OPS_STATE_DB` | 自动探测 | 显式指定 `state.db` 路径 |
| `WECHAT_GOLEM_STICKER_DIR` | `$HERMES_HOME/wechat_stickers` | 与适配器保持一致 |
| `WECHAT_GOLEM_MEMBER_PROFILE_DIR` | `$HERMES_HOME/wechat_member_profiles` | 与适配器保持一致 |

### 桥配置项

见第二节的 `config.toml` 示例；每个字段在 `main.go` 的 `Config` 结构上都有注释，
`plugins/config.toml` 首次生成时会带上这些注释。

---

产品与协议细节：`readme.md`（桥）、`wechat_golem/README.md`（适配器）、
`hermes_ops/README.md`（运维服务）。
