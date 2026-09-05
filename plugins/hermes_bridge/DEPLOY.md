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

- Python 3.9+
- `aiohttp`（适配器与桥之间全部 HTTP/SSE 都走它）。必须在**运行 gateway 的那个 venv** 里——
  适配器是被 import 进 gateway 进程的，不是独立进程；装到系统 `python3`、conda 或别的
  virtualenv 都不算。
  它是 Hermes 自己声明的**可选**依赖：源码 `pyproject.toml` 的 `messaging` extra 里
  `aiohttp==<钉死版本>`（版本为修一串 CVE 而 pin）。有的安装会在 setup 时就装上（实测过一台是
  3.14.1，与其余包同批落盘），新版则由 `tools/lazy_deps.py` 在**用到时**才惰性安装（受
  `security.allow_lazy_installs` 控制，默认开）。**本适配器不参与那套惰性安装**，缺了只会
  静默从 channels 消失，所以装完务必按第六节验一次。
  真的缺时**别裸 `pip install aiohttp`**（会绕过上面那个 pin）：在 Hermes 源码目录
  `uv sync --extra messaging`，或重跑官方安装器 / `hermes update`；也不要 `--break-system-packages`。
- 一个已能跑的 Hermes profile
- 网关已作为 **systemd 用户级服务**常驻：`hermes -p <profile> gateway install`（它自己会开 linger，
  不必手动 `loginctl enable-linger`；覆盖升级旧单元后值得核一次
  `loginctl show-user <user> --property=Linger` 是否仍为 `yes`）。
  查状态 `hermes -p <profile> gateway status` 或 `systemctl --user is-enabled hermes-gateway-<profile>.service`；
  前台调试是 `gateway run`。**没有 `gateway logs` 子命令**，日志位置见第六节。

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
cp plugins/hermes_bridge/wechat_golem/plugin.yaml \
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

**manifest 文件名必须全小写 `plugin.yaml`**（bundled 插件同此约定）。写成 `PLUGIN.yaml` 的症状是
「目录明明在，Hermes 当没看见」：`plugins enable` 不认、工具一个不注册，且没有报错。从 Windows
拷过来时尤其容易踩——NTFS 大小写不敏感，本地两种写法是同一个文件，只有落到 Linux 才暴露。

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

完整可选变量见 `wechat_golem/plugin.yaml`（安装向导会逐项提示）与本文附录。

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
systemctl --user restart hermes-gateway-wechat.service    # 冷重启，重跑 register(ctx)
# 也可 hermes -p wechat gateway restart（同样是真重启；但它检测不到 user systemd 时会退回前台跑）
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

**插件 enabled 了，但 `channels` 里没有 wechat_golem**

`plugins ls` 显示 enabled 只说明配置里开了，不代表平台注册成功。适配器的
`check_requirements()`（`adapter.py`）返回 False 时平台就不进 channels，而**两条原因都静默**：

1. **缺 `aiohttp`**：适配器对它是 `try/except ImportError` 软导入，所以模块 import 得去、
   插件照样显示 enabled，只是平台被 `check_fn` 挡在外面。全新机器上这确实可能发生——aiohttp
   只是 Hermes 的可选依赖（`messaging` extra），新版由 `tools/lazy_deps.py` 在用到时才装，
   而本适配器不参与那套惰性安装。
2. **`WECHAT_GOLEM_TOKEN` / `WECHAT_GOLEM_BASE_URL` 没进 gateway 进程环境**：适配器只读
   `os.getenv`，缺一样就 return False。必须写在 **profile 的** `$HERMES_HOME/.env`；放
   `~/.hermes/.env`（默认 profile）或只在自己 shell 里 `export`（systemd 服务读不到）都是同一个症状。

排查顺序是先钉住解释器，再验这两样。

**第一步：gateway 跑的是哪个 python**

适配器不是独立进程，是被 import 进 gateway 进程的，所以「适配器用哪个 python」＝「gateway
进程是哪个 python 起的」。**以运行中的进程为准**，其他都是推断：

```bash
P=$(systemctl --user show -p MainPID --value hermes-gateway-<profile>.service); echo "PID=$P"
tr '\0' '\n' < /proc/$P/cmdline     # argv[0] 就是答案：真正被调用的解释器
tr '\0' '\n' < /proc/$P/environ | grep -E '^(VIRTUAL_ENV|PYTHONPATH|PYTHONHOME)='
readlink -f /proc/$P/exe            # 仅供参考：解析符号链接后的真实二进制
```

⚠️ **别拿 `readlink -f /proc/$P/exe` 的结果去装包或验 import**：venv 里的 `bin/python` 只是个
指向真实解释器的符号链接，解析后可能是 `/usr/bin/python3.x`，也可能是 uv / pyenv 管的独立解释器
（实测过一台是 `/usr/local/share/uv/python/cpython-3.11.15-linux-x86_64-gnu/bin/python3.11`）。
拿它 `import aiohttp` 看不到 venv 的 site-packages，会误判成「没装」。要用 `$VIRTUAL_ENV/bin/python`
或 argv[0] 那个路径。

服务没在跑（`PID=0`）时才退回推断：

```bash
systemctl --user cat hermes-gateway-<profile>.service | grep -i execstart
head -1 "$(command -v hermes)"      # hermes 自己的 shebang
```

**第二步：用那个解释器验 aiohttp，用 `/proc` 验 env**

```bash
V=$(tr '\0' '\n' < /proc/$P/environ | sed -n 's/^VIRTUAL_ENV=//p')
PY=${V:+$V/bin/python}; PY=${PY:-$(tr '\0' '\n' < /proc/$P/cmdline | head -1)}; echo "PY=$PY"
"$PY" -c 'import sys, aiohttp; print(sys.prefix); print(aiohttp.__version__, aiohttp.__file__)'
tr '\0' '\n' < /proc/$P/environ | grep WECHAT_GOLEM   # 两个必填变量真进了进程吗

# 只有上面报 ModuleNotFoundError 才需要装 —— 先明白它为什么会缺：aiohttp 是 Hermes
# pyproject.toml 里 `messaging` extra 的依赖（版本为修 CVE 钉死），新版 Hermes 由
# tools/lazy_deps.py 在用到时才惰性装；本适配器不参与那套机制，所以不会自动补。
# 正解是按 Hermes 自己的清单补，别裸装（裸装会绕过那个 pin）：
cd <Hermes 源码目录> && uv sync --extra messaging     # 或重跑官方安装器 / hermes update
# 实在只能单装：照 pyproject.toml 里钉的版本，别让它自己挑新版
uv pip install --python "$PY" 'aiohttp==<pyproject 里的 pin>'
# 另注：`security.allow_lazy_installs: false` 或 sealed/hosted 安装下 Hermes 自己也不会
# 惰性补装，这时只能显式装。
```

补齐任何一样之后都要**冷重启**（`systemctl --user restart hermes-gateway-<profile>.service`），
`register(ctx)` 才会重跑、平台才会重新注册。

交叉验证（可选）：适配器安装目录的 `__pycache__/` 能读出**版本号**。loader 导入的是
`__init__.py`，所以 `__init__.cpython-3XX.pyc` 的 `3XX` 就是 gateway 解释器的版本；而
`adapter.cpython-3YY.pyc` 是谁跑过 `py_compile adapter.py` 留下的。两个版本号不一致，说明
语法检查用的解释器和真正加载的不是一个（检查意义打折）。具体生成哪些 pyc 取决于加载方式，
所以这只是交叉验证，**以 `/proc` 为准**。

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

Hermes 自带轮转（滚为 `.1`/`.2`/`.3`），实测全目录总占用几十 MB，**不必清、也不要再配
logrotate**——两套轮转管同一批文件会打架。单文件上限约 `agent.log`/`gateway.log` 5MB、
`errors.log` 2MB，但**这两个数值是从备份文件大小反推的、未核 Hermes 源码**（轮转在生效
是确定的，备份文件即证据）。例外是 `gateway-exit-diag.log` / `gateway-shutdown-diag.log`，
无轮转、只追加，要清就 `truncate -s 0`（**别用 `rm`**：进程持着 fd，删了空间不释放且
读不到新日志）。详见 `hermes_ops/README.md` §6.5。

`grep` 查历史记得带 `*`（`gateway.log*`），否则只搜到最后一次滚动至今的内容。

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

**消息发到了别的群（出站串台）**

典型现场：在 A 群让它发图/发表情，趁它还没发完你切到 B 群说了句话，东西发进了 B 群。
这不是模型看错上下文（一个群一条 session，它看不到别的群），是**出站目标解析**的问题：
`wechat_send_*` 的 `chat_id` 曾经「可省略」，模型不填时适配器一路兜底到进程级的
「最近一次入站会话」——而那个变量被任何会话的新消息覆盖。

现在的行为（桥 v0.15 + 适配器 1.3.0）：`chat_id` 在 tool schema 里必填，且出站目标按
**本轮 run 绑定 > session 登记表 > 唯一在途会话** 定位；模型给的 `chat_id` 与本轮会话
不一致时自动纠回本轮会话并打 warning。确需发到别的会话（「往 XX 群发个通知」）要显式传
`allow_cross_chat=true`。适配器还会把本轮 `session_key` 带给桥，桥再校验一次归属，
不匹配直接拒发。排查看这几行：

```bash
grep -aE "出站目标|拒发：目标与声明会话" ~/.hermes/profiles/wechat/logs/gateway.log | tail -20
```

`source=ctx|session_map|inflight` 是可信定位；`source=recent` 说明退到了短窗兜底
（默认 15s，`WECHAT_GOLEM_CHAT_FALLBACK_TTL_S` 可调），多会话同时在途时不会兜底，
而是让 tool 报错要求模型补 `chat_id`。

**出站 tool 拒调 / 报 schema 缺 chat_id**

上一条那次改动把出站 6 个 tool 的 `chat_id` 放进了 schema `required`。历史上这几个 tool
刻意留空 `required`，就是怕 Hermes 某些 dispatch 形态吞掉参数后模型直接拒调。若真出现，
把 `adapter.py` 里那 6 处 `"required": ["chat_id"]` 改回 `[]` 即可回退 —— handler 侧的
受闸兜底仍在，不会因此发不出消息（代价只是模型又倾向不填，退回靠三层定位，串台防护不变）。

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
| `WECHAT_GOLEM_CHAT_FALLBACK_TTL_S` | `15` | 出站 `chat_id` 退到「最近入站会话」的窗口秒数；多会话在途时不兜底 |
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
