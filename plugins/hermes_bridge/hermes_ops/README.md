# hermes_ops

跑在 **Hermes gateway 所在机器**的 **只读为主** 运维 HTTP 小服务，给 Golem
`hermes_bridge` 管理台「Hermes」页用。

桥通过 `hermes_ops_url` 反代，浏览器只打桥的本机管理台，不直连 ops。

源码目录：`plugins/hermes_bridge/hermes_ops/`（随仓库；部署时拷到 `~/.hermes/ops/`）。

---

## 一、架构与 token

```text
浏览器 ──admin_token──► 桥 :8644 管理台（默认仅本机）
                            │
                            │ hermes_ops_token
                            ▼
                     ops :8650（默认仅回环）──只读──► 日志文件 / state.db / 服务状态
```

| Token | 用途 | 配置位置 |
|-------|------|----------|
| 业务 `token` / `WECHAT_GOLEM_TOKEN` | 适配器 ↔ 桥 `:8643` | 桥 toml + Hermes `.env` |
| `admin_token` | 浏览器 ↔ 管理台 `:8644` | 桥 toml + UI 登录框 |
| `HERMES_OPS_TOKEN` / `hermes_ops_token` | 桥 ↔ ops `:8650` | ops 环境变量 + 桥 toml |

**三者分开。** 不要和业务 token 共用。

| 端口 | 默认绑定 | 谁连 |
|------|----------|------|
| 8643 | 桥 `0.0.0.0`（同机可收紧回环） | Hermes 适配器 → 桥（业务 SSE/发送） |
| 8644 | 桥 `127.0.0.1` | 仅本机浏览器（管理台） |
| 8650 | ops `127.0.0.1` | 桥 → ops |

**同机部署**：三个口都走 loopback，`hermes_ops_url = "http://127.0.0.1:8650"`，无需放开任何监听。

**分机部署**：桥要访问另一台机器上的 ops，两个选择——
① 推荐保持 ops 绑回环 + SSH 隧道 `ssh -L 8650:127.0.0.1:8650 <hermes-host>`，桥仍填 `127.0.0.1:8650`；
② 或设好 `HERMES_OPS_TOKEN` 后放开 `HERMES_OPS_LISTEN=0.0.0.0:8650`，桥填 ops 所在机器的 IP。
**无 token 时 ops 只接受本机请求，且非回环监听会直接拒绝启动**（详见第三节）。

---

## 二、HTTP 接口

均需 `Authorization: Bearer <HERMES_OPS_TOKEN>`。未设 token 时**只接受来自本机的请求**
（`127.0.0.1` / `::1`），其余来源一律 401。

| 路径 | 说明 |
|------|------|
| `GET /health` | 探活；报 profile、各目录、`service_manager`、`auth` 模式 |
| `GET /overview` | 服务状态 + 工具注册粗检 + 红灯 `alerts`（非 systemd 环境不计入红灯） |
| `GET /tools/check` | 扫 `agent.log`/`errors.log` 尾：`tool registered` / `registration crashed` |
| `GET /sessions?n=40` | 只读 `state.db`（若有）或回落 `hermes -p <profile> sessions list` |
| `GET /logs?file=agent\|gateway\|errors&n=80&grep=` | 日志尾；`grep` 为简单子串（大小写不敏感） |
| `GET /stickers/facets` | 情绪/题材计数（UI 先选再加载） |
| `GET /stickers?n=100&mood=&tag=&q=` | 表情库列表（建议带 mood/tag/q，勿一次全库） |
| `GET /stickers/<md5>` | 单条表情元数据 |
| `GET /stickers/<md5>/file` | 表情原文件（image/*，≤2MB；管理台缩略图） |
| `GET /member_profiles?q=` | 群友档案列表 |
| `GET /member_profiles/<wxid>` | 单份档案 JSON |
| `PUT /member_profiles/<wxid>` | **轻写**：合并/覆盖档案字段（body JSON） |
| `DELETE /member_profiles/<wxid>` | 删除档案（幂等） |

**故意没有：** gateway restart、sessions prune、任意 shell、改 `config.yaml`。写操作请 SSH，避免 UI 重启风暴。

---

## 三、环境变量

一般只需设 `HERMES_PROFILE` 与 `HERMES_OPS_TOKEN`，其余都能派生出来。

| 变量 | 默认 | 说明 |
|------|------|------|
| `HERMES_PROFILE` | `wechat` | profile 名；派生下面 `HERMES_HOME` 与单元名的默认值，以及 CLI `-p` 参数 |
| `HERMES_OPS_TOKEN` | 空 | Bearer。空则只接受本机请求；**非回环监听且为空时拒绝启动** |
| `HERMES_OPS_LISTEN` | `127.0.0.1:8650` | 监听地址。放开前务必先设 token |
| `HERMES_HOME` | `~/.hermes/profiles/$HERMES_PROFILE` | profile 根 |
| `HERMES_GATEWAY_UNIT` | `hermes-gateway-$HERMES_PROFILE.service` | systemd 单元名 |
| `HERMES_OPS_SERVICE_MANAGER` | `auto` | `auto`\|`systemd-user`\|`systemd-system`\|`none`；`auto` 探测 `systemctl` + `/run/systemd/system` |
| `HERMES_OPS_LOG_DIR` | `$HERMES_HOME/logs` | 日志目录（容器把日志重定向到 stdout 时可指到别处） |
| `HERMES_OPS_STATE_DB` | 自动探测三个候选 | 显式指定 `state.db` 路径 |
| `WECHAT_GOLEM_STICKER_DIR` | `$HERMES_HOME/wechat_stickers` | 须与适配器一致 |
| `WECHAT_GOLEM_MEMBER_PROFILE_DIR` | `$HERMES_HOME/wechat_member_profiles` | 须与适配器一致 |

两个目录变量与适配器共用，**两侧必须设成同一路径**，否则 ops 看到的是空库。
`GET /health` 会回显 `sticker_dir` / `member_dir`，可据此自查。

### 非 systemd 环境

容器、macOS、WSL1、supervisor、pm2、以及用**系统级**（非 `--user`）systemd 的场合：
`auto` 探测不到 user-level systemd 时会退成 `none`，此时 `/overview` 跳过服务状态检查、
**不因此报红灯**，其余接口（日志、sessions、表情、档案）照常工作。

如确实在用 systemd 但被误判，显式设 `HERMES_OPS_SERVICE_MANAGER=systemd-user`
或 `systemd-system`。启动日志会打印探测结果（`service_manager=...`）。

---

## 四、安装

### 4.1 拷贝

```bash
mkdir -p ~/.hermes/ops
# 从仓库 plugins/hermes_bridge/hermes_ops/ 拷入：
#   hermes_ops.py
#   hermes-ops.service.example（可选）
cp /path/to/hermes_ops.py ~/.hermes/ops/
```

> **路径别搞混（实踩）**  
> - **脚本**必须在：`~/.hermes/ops/hermes_ops.py`（与 unit 里 `ExecStart=… %h/.hermes/ops/hermes_ops.py` 一致）  
> - **unit 文件**才在：`~/.config/systemd/user/hermes-ops.service`  
> 若把 `hermes_ops.py` 误拷进 `~/.config/systemd/user/`，systemd 仍跑旧的 `~/.hermes/ops/` 副本 → 界面一直 404、你以为「已经更新了」。  
> 更新后验：`wc -c -l ~/.hermes/ops/hermes_ops.py` + `curl …/health` 看 `version`。

### 4.2 先手动跑通（必做一次）

```bash
export HERMES_PROFILE=wechat        # 派生 HERMES_HOME 与 gateway 单元名
export HERMES_OPS_TOKEN='与桥 hermes_ops_token 相同的长随机串'
# 监听默认 127.0.0.1:8650。桥在另一台机器且不走 SSH 隧道时才放开：
# export HERMES_OPS_LISTEN=0.0.0.0:8650

python3 ~/.hermes/ops/hermes_ops.py
```

启动日志会打印生效配置，先核一遍再继续：

```text
[hermes_ops] listen=127.0.0.1:8650 profile=wechat HERMES_HOME=/home/u/.hermes/profiles/wechat
             unit=hermes-gateway-wechat.service service_manager=systemd-user
             stickers=… members=…
```

另开一个窗口验：

```bash
curl -sS -H "Authorization: Bearer $HERMES_OPS_TOKEN" http://127.0.0.1:8650/health
curl -sS -H "Authorization: Bearer $HERMES_OPS_TOKEN" http://127.0.0.1:8650/overview
curl -sS -H "Authorization: Bearer $HERMES_OPS_TOKEN" http://127.0.0.1:8650/tools/check
```

### 4.3 让桥能访问到 ops

**同机**：桥填 `hermes_ops_url = "http://127.0.0.1:8650"`，到这步就完了。

**分机**，二选一：

① SSH 隧道（推荐，ops 保持只绑回环）——在桥所在机器上：

```bash
ssh -N -L 8650:127.0.0.1:8650 <user>@<hermes-host>
# 桥仍填 http://127.0.0.1:8650
```

② 放开监听（务必先设好 `HERMES_OPS_TOKEN`，否则 ops 拒绝启动）：

```bash
# Hermes 侧
export HERMES_OPS_LISTEN=0.0.0.0:8650
hostname -I          # 取本机 IP，填进桥的 hermes_ops_url
sudo ufw allow 8650/tcp comment hermes_ops   # 若启用了 ufw
```

从桥所在机器验通：

```bash
curl -sS -H "Authorization: Bearer <OPS_TOKEN>" http://<hermes-host>:8650/health
```

### 4.4 桥侧配置

`plugins/config.toml`（host 运行目录那份）：

```toml
[hermes_bridge.config]
admin_listen = "127.0.0.1:8644"
admin_token  = "管理台 token"

hermes_ops_url   = "http://<VM_IP>:8650"
hermes_ops_token = "与 HERMES_OPS_TOKEN 相同"
```

重载/重启 host 使桥读到配置。浏览器：`http://127.0.0.1:8644/ui/` → 填 **admin_token** → **Hermes** 页刷新。

---

## 五、systemd 用户服务（稳定后）

与 `hermes-gateway-wechat.service` 同级，**独立** user 单元；必须带 `--user`。

### 5.1 写 unit

```bash
mkdir -p ~/.config/systemd/user
nano ~/.config/systemd/user/hermes-ops.service
```

```ini
[Unit]
Description=Hermes ops read-only API (wechat profile)
After=network.target

[Service]
Type=simple
WorkingDirectory=%h/.hermes/ops
Environment=HERMES_HOME=%h/.hermes/profiles/wechat
Environment=HERMES_OPS_LISTEN=0.0.0.0:8650
Environment=HERMES_OPS_TOKEN=与桥一致的长随机串
Environment=HERMES_GATEWAY_UNIT=hermes-gateway-wechat.service
# 若表情库/档案路径有覆盖，一并 Environment= 写上
ExecStart=/usr/bin/python3 %h/.hermes/ops/hermes_ops.py
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
```

`which python3` 若不是 `/usr/bin/python3`，改 `ExecStart`。

也可用仓库示例：

```bash
cp hermes-ops.service.example ~/.config/systemd/user/hermes-ops.service
# 再编辑 token / 路径
```

### 5.2 停掉手动进程再启动

```bash
# 若还在前台跑 python，Ctrl+C；或：
# pkill -f 'hermes_ops.py'
ss -lntp | grep 8650    # 应暂时无占用

systemctl --user daemon-reload
systemctl --user enable --now hermes-ops.service
systemctl --user status hermes-ops.service --no-pager
```

期望：`Active: active (running)`。

### 5.3 常用命令

```bash
systemctl --user status hermes-ops.service
systemctl --user is-active hermes-ops.service    # active / inactive
systemctl --user restart hermes-ops.service      # 更新 .py 后
systemctl --user stop hermes-ops.service
systemctl --user start hermes-ops.service
systemctl --user disable hermes-ops.service      # 取消自启

# 改 unit 环境变量后：
systemctl --user daemon-reload
systemctl --user restart hermes-ops.service
```

### 5.4 日志（journal）

```bash
journalctl --user -u hermes-ops.service -n 80 --no-pager
journalctl --user -u hermes-ops.service -f
journalctl --user -u hermes-ops.service --since "10 min ago"
```

启动成功时 stderr 类似：

```text
[hermes_ops] listen=0.0.0.0:8650 HERMES_HOME=... unit=hermes-gateway-wechat.service
```

**不要**用 `journalctl -u hermes-ops`（无 `--user`）——会空，和 gateway 同一坑。

### 5.5 无人登录也常驻（linger）

用户级服务挂在用户会话上。若 SSH 全断后服务消失：

```bash
loginctl enable-linger $USER
loginctl show-user $USER | grep Linger
# Linger=yes
```

若 gateway 用户服务早已在无人 SSH 时存活，linger 多半已开。

### 5.6 和 gateway 的关系

| 单元 | 作用 |
|------|------|
| `hermes-gateway-wechat.service` | Hermes gateway + 适配器 |
| `hermes-ops.service` | 只读运维 API |

互不依赖重启；改适配器只 restart **gateway**；改 ops 脚本只 restart **ops**。

---

## 六、状态与排障

### 6.1 健康检查顺序

```bash
# Hermes 侧本机
systemctl --user is-active hermes-ops.service    # 用 systemd 时
curl -sS -H "Authorization: Bearer $HERMES_OPS_TOKEN" http://127.0.0.1:8650/health
curl -sS -H "Authorization: Bearer $HERMES_OPS_TOKEN" http://127.0.0.1:8650/overview

# 桥所在机器 → ops（分机且放开了监听时）
curl -sS -H "Authorization: Bearer <OPS>" http://<hermes-host>:8650/health

# 桥反代（等价于本机管理台已登录）
curl -sS -H "Authorization: Bearer <ADMIN>" http://127.0.0.1:8644/admin/hermes/meta
curl -sS -H "Authorization: Bearer <ADMIN>" http://127.0.0.1:8644/admin/hermes/health
```

### 6.2 overview 红灯含义

| alerts / 字段 | 含义 | 下一步 |
|---------------|------|--------|
| gateway 未 active | 单元挂了或没起 | `systemctl --user status hermes-gateway-<profile>.service` |
| 未见 tool registered | 日志尾还没有注册成功行 | 冷启 gateway；`grep -a 'tool registered' …/logs/agent.log` |
| registration crash 样本 | 工具注册异常被吞 | `errors.log` / `agent.log` 找 traceback（**不要**只看 gateway.log） |
| 日志目录不存在 | `HERMES_HOME` 或 `HERMES_OPS_LOG_DIR` 错 | 核 `/health` 回显的路径 |

`systemd.supported=false` 不是红灯：表示本机没用 systemd，服务状态检查已跳过。

### 6.3 常见失败

| 现象 | 处理 |
|------|------|
| 启动即退出、报「拒绝启动」 | 非回环监听但 `HERMES_OPS_TOKEN` 为空；设 token 或改回 `127.0.0.1:8650` |
| `address already in use` | 手动 python 仍占 8650；先停再 start 单元 |
| `status=203/EXEC` | `ExecStart` 的 python 路径不对 |
| 401（带了 token） | ops token 与桥 `hermes_ops_token` 不一致 |
| 401（没带 token） | ops 无 token 时只接受本机请求；跨机访问必须设 token 或走 SSH 隧道 |
| 跨机 connection refused | IP 错、防火墙、ops 仍绑 `127.0.0.1`、或服务没起 |
| UI「未配置 hermes_ops_url」 | 桥 toml 未写或未重载插件 |
| `systemctl`/`journalctl` 空白 | 漏了 `--user` |
| 表情库空但档案正常 | 两侧 `WECHAT_GOLEM_STICKER_DIR` 不一致（或 ops 与 gateway 跑在不同用户下）；比对 `/health` 回显 |
| sessions 空 / 无 db | 见返回 `note`；CLI 回落或确认 `HERMES_HOME` |
| stickers 空 | 检查 `WECHAT_GOLEM_STICKER_DIR` / 默认 `~/.hermes/wechat_stickers/index.json` |

### 6.4 相关 Hermes 日志（gateway 本身，非 ops）

```bash
# 应用日志（grep 必须 -a）
tail -f ~/.hermes/profiles/wechat/logs/gateway.log
grep -a 'tool registered\|registration crashed' \
  ~/.hermes/profiles/wechat/logs/agent.log \
  ~/.hermes/profiles/wechat/logs/errors.log | tail -20

# gateway 单元
systemctl --user status hermes-gateway-wechat.service
journalctl --user -u hermes-gateway-wechat.service -n 100 --no-pager
```

ops 的 `/logs` 与 `/tools/check` 读的就是上述文件尾，避免每次 SSH 手敲。
单次最多回看尾部 2MB（`TAIL_WINDOW_BYTES`），响应里的 `file_bytes` 与
`window_truncated` 说明文件实际多大、有没有被窗口截断。

### 6.5 日志体积与轮转

**Hermes 自带轮转，不要再配 logrotate。** 实测 `agent.log` / `gateway.log` 单文件
上限 5MB、`errors.log` 2MB，滚为 `.1` / `.2` / `.3` 并按 backupCount 顶掉最旧的，
典型总占用几十 MB。给同一批文件再加 logrotate 会和 Python 的
`RotatingFileHandler` 打架：`copytruncate` 清零后 handler 里缓存的写入位置仍是旧值，
会在错误时机触发滚动，两套 backup 命名也会互相覆盖。

例外是 `gateway-exit-diag.log` / `gateway-shutdown-diag.log`：每次退出/关停追加、
**没有轮转**。平时几百 KB 无妨，重启频繁时会持续增长，可按需清零：

```bash
cd ~/.hermes/profiles/wechat/logs
truncate -s 0 gateway-exit-diag.log gateway-shutdown-diag.log
```

清日志一律用 `truncate -s 0`，**不要 `rm` 或 `mv`**：写入进程持着 fd，删除或换掉
inode 后它会继续往已删除的文件写——磁盘空间不释放（`du` 变小而 `df` 不变），
而且新日志再也读不到，只能重启才恢复。

---

## 七、安全

- 路径白名单；无自由 shell argv
- Token 与业务、admin 分离
- **默认只绑 `127.0.0.1`**；无 token 时只接受本机请求，非回环监听且无 token 直接拒绝启动
- 跨机访问优先 SSH 隧道 `ssh -L 8650:127.0.0.1:8650 <user>@<hermes-host>`，桥填
  `http://127.0.0.1:8650`；要直连则先设 `HERMES_OPS_TOKEN` 再放开 `HERMES_OPS_LISTEN`
- `PUT/DELETE member_profiles` 为有意轻写；表情字节与 gateway 控制面仍不开放

---

## 八、桥侧反代路径对照

管理台（需 `admin_token`）→ 桥 → ops：

| 管理台 | ops |
|--------|-----|
| `GET /admin/hermes/meta` | （桥本地，是否配置了 url） |
| `GET /admin/hermes/health` | `/health` |
| `GET /admin/hermes/overview` | `/overview` |
| `GET /admin/hermes/tools/check` | `/tools/check` |
| `GET /admin/hermes/sessions` | `/sessions` |
| `GET /admin/hermes/logs?…` | `/logs?…` |
| `GET /admin/hermes/stickers…` | `/stickers…` |
| `GET|PUT|DELETE /admin/hermes/member_profiles…` | 同名 |

产品说明：`plugins/hermes_bridge/readme.md`；部署总册：`plugins/hermes_bridge/DEPLOY.md`。
