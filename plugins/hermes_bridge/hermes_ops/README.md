# hermes_ops

Ubuntu VM 上的 **只读为主** 运维 HTTP 小服务，给 Golem `hermes_bridge` 管理台「Hermes」页用。

桥（Windows）通过 `hermes_ops_url` 反代，浏览器只打本机 `http://127.0.0.1:8644/ui/`，不直连 VM。

源码目录：`plugins/hermes_bridge/hermes_ops/`（随仓库；部署时拷到 VM `~/.hermes/ops/`）。

---

## 一、架构与 token

```text
浏览器 ──admin_token──► Windows :8644 管理台
                            │
                            │ hermes_ops_token
                            ▼
                       VM :8650 hermes_ops  ──只读──►  journal / 日志文件 / state.db
```

| Token | 用途 | 配置位置 |
|-------|------|----------|
| 业务 `token` / `WECHAT_GOLEM_TOKEN` | 适配器 ↔ 桥 `:8643` | 桥 toml + VM `.env` |
| `admin_token` | 浏览器 ↔ 管理台 `:8644` | 桥 toml + UI 登录框 |
| `HERMES_OPS_TOKEN` / `hermes_ops_token` | 桥 ↔ ops `:8650` | VM 环境变量 + 桥 toml |

**三者分开。** 不要和业务 token 共用。

| 端口 | 默认绑定 | 谁连 |
|------|----------|------|
| 8643 | Windows `0.0.0.0` | VM → 宿主机（业务 SSE/发送） |
| 8644 | Windows `127.0.0.1` | 仅本机浏览器（管理台） |
| 8650 | VM `0.0.0.0`（可改） | 宿主机 → VM（ops） |

常用拓扑（VMware）：VM 访问桥用宿主机网关 `http://192.168.47.1:8643`；桥访问 ops 用 **VM 自己的 IP** `http://<VM_IP>:8650`（不是 47.1）。

---

## 二、HTTP 接口

均需 `Authorization: Bearer <HERMES_OPS_TOKEN>`（未设 token 时不鉴权，仅建议本机调试）。

| 路径 | 说明 |
|------|------|
| `GET /health` | 探活 |
| `GET /overview` | systemd 单元 + 工具注册粗检 + 红灯 `alerts` |
| `GET /tools/check` | 扫 `agent.log`/`errors.log` 尾：`tool registered` / `registration crashed` |
| `GET /sessions?n=40` | 只读 `state.db`（若有）或回落 `hermes -p wechat sessions list` |
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

| 变量 | 默认 | 说明 |
|------|------|------|
| `HERMES_HOME` | `~/.hermes/profiles/wechat` | profile 根 |
| `HERMES_OPS_LISTEN` | `0.0.0.0:8650` | 监听；更紧可 `127.0.0.1:8650` + SSH `-L` |
| `HERMES_OPS_TOKEN` | 空 | Bearer；**生产必填** |
| `HERMES_GATEWAY_UNIT` | `hermes-gateway-wechat.service` | `systemctl --user` 单元名 |
| `WECHAT_GOLEM_STICKER_DIR` | `~/.hermes/wechat_stickers` | 与适配器表情库一致 |
| `WECHAT_GOLEM_MEMBER_PROFILE_DIR` | `$HERMES_HOME/wechat_member_profiles` | 群友档案目录 |

---

## 四、安装

### 4.1 拷贝

```bash
mkdir -p ~/.hermes/ops
# 从 Windows 仓库 plugins/hermes_bridge/hermes_ops/ 拷入：
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
export HERMES_HOME=$HOME/.hermes/profiles/wechat
export HERMES_OPS_TOKEN='与桥 hermes_ops_token 相同的长随机串'
export HERMES_OPS_LISTEN=0.0.0.0:8650
export HERMES_GATEWAY_UNIT=hermes-gateway-wechat.service

python3 ~/.hermes/ops/hermes_ops.py
```

另开 SSH 窗口：

```bash
curl -sS -H "Authorization: Bearer $HERMES_OPS_TOKEN" http://127.0.0.1:8650/health
curl -sS -H "Authorization: Bearer $HERMES_OPS_TOKEN" http://127.0.0.1:8650/overview
curl -sS -H "Authorization: Bearer $HERMES_OPS_TOKEN" http://127.0.0.1:8650/tools/check
```

查 VM IP（给 Windows 桥填，**不是** 192.168.47.1）：

```bash
hostname -I
# 或: ip -4 addr
```

Windows 上测：

```powershell
curl.exe -sS -H "Authorization: Bearer <OPS_TOKEN>" http://<VM_IP>:8650/health
```

### 4.3 防火墙（若启用 ufw）

```bash
sudo ufw allow 8650/tcp comment hermes_ops
sudo ufw status
```

### 4.4 Windows 桥配置

`plugins/config.toml`（运行目录那份）：

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
# VM 本机
systemctl --user is-active hermes-ops.service
curl -sS -H "Authorization: Bearer $HERMES_OPS_TOKEN" http://127.0.0.1:8650/health
curl -sS -H "Authorization: Bearer $HERMES_OPS_TOKEN" http://127.0.0.1:8650/overview

# Windows → VM
curl.exe -sS -H "Authorization: Bearer <OPS>" http://<VM_IP>:8650/health

# 桥反代（本机管理台已登录等价）
# UI Hermes 页刷新；或：
curl.exe -sS -H "Authorization: Bearer <ADMIN>" http://127.0.0.1:8644/admin/hermes/meta
curl.exe -sS -H "Authorization: Bearer <ADMIN>" http://127.0.0.1:8644/admin/hermes/health
```

### 6.2 overview 红灯含义

| alerts / 字段 | 含义 | 下一步 |
|---------------|------|--------|
| gateway 未 active | 单元挂了或没起 | `systemctl --user status hermes-gateway-wechat.service` |
| 未见 tool registered | 日志尾还没有注册成功行 | 冷启 gateway；`grep -a 'tool registered' …/logs/agent.log` |
| registration crash 样本 | 工具注册异常被吞 | `errors.log` / `agent.log` 找 traceback（**不要**只看 gateway.log） |
| 日志目录不存在 | `HERMES_HOME` 错 | 检查 profile 路径 |

### 6.3 常见失败

| 现象 | 处理 |
|------|------|
| `address already in use` | 手动 python 仍占 8650；先停再 start 单元 |
| `status=203/EXEC` | `ExecStart` 的 python 路径不对 |
| 401 | ops token 与桥 `hermes_ops_token` 不一致 |
| Windows connection refused | IP 错、ufw、ops 没 listen `0.0.0.0`、单元 dead |
| UI「未配置 hermes_ops_url」 | 桥 toml 未写或未重载插件 |
| `systemctl`/`journalctl` 空白 | 漏了 `--user` |
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

---

## 七、安全

- 路径白名单；无自由 shell argv  
- Token 与业务、admin 分离  
- 默认 LAN 可扫到 8650：靠 token；更紧则 `127.0.0.1` +  
  `ssh -L 8650:127.0.0.1:8650 user@VM`，桥填 `http://127.0.0.1:8650`  
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

产品说明：`plugins/hermes_bridge/readme.md`；部署总册：`t-doc/hermes-bridge-notes.md`。
