# PawzoChat Golem 插件

这是一个将 [Golem](https://github.com/sbgayhub/golem) 微信消息桥接到
PawzoChat 的独立适配插件。它把收到的文本或引用消息交给 PawzoChat 角色处理，
再将文本、图片、表情或语音回复发回原会话。

## 来源与版权声明

PawzoChat 是独立的上游项目，原作者为 **iwyxdxl**：

- 原始仓库：[https://github.com/iwyxdxl/PawzoChat](https://github.com/iwyxdxl/PawzoChat)
- 原项目许可证：GNU Affero General Public License v3（AGPL-3.0）

本插件是 Golem 与 PawzoChat 之间的适配层，不是 PawzoChat 原项目本体，
也不是 PawzoChat 官方发布或官方背书的插件。PawzoChat 的名称、原始代码、版权和
许可证归其原作者及上游仓库所有；使用、修改或再分发 PawzoChat 本体时，请遵守
上游仓库中的 `LICENSE`。

本目录中的适配代码属于 Golem 仓库的一部分，许可证和版权信息以仓库根目录的
`LICENSE` 及其贡献规则为准。除非另有明确说明，本插件不改变 PawzoChat 上游项目
的许可证归属。

## 功能

- 将 Golem 微信私聊和群聊消息发送到 PawzoChat bridge。
- 将 PawzoChat 返回的文本、图片、表情和语音发送回原会话。
- 支持引用消息和群聊 `@` / 引用触发策略。
- 每个 Golem 会话可以映射到独立的 PawzoChat 角色，隔离聊天历史和记忆。
- 新群聊角色优先使用群名称，新私聊角色优先使用联系人显示名。
- 通过可信身份信封向角色提供发送者、主人和寻址信息。

## 构建与安装

需要 Go 1.26 或更新版本。在 Golem 仓库根目录执行：

```bash
cd plugins/pawzochat
go test ./...
go build -o golem_plugin_pawzochat .
```

将生成的 `golem_plugin_pawzochat` 放入 Golem 运行目录的 `plugins/`，然后在
Golem 配置中启用 `pawzochat` 插件。二进制文件是构建产物，不应提交到源码仓库。

## PawzoChat 配置

PawzoChat 默认监听 `http://127.0.0.1:62000`。同一台机器上的回环请求可以不配置
Token；跨机器、反向代理或其他非回环访问必须设置随机 Token，并在两端保持一致：

```yaml
bridge:
  golem_token: "replace-with-a-long-random-token"
  golem_timeout_seconds: 45
```

不要把真实 Token 写入 README、示例配置或提交记录。

## Golem 配置

```toml
[pawzochat]
enable = true
mode = "blacklist"
limits = []

[pawzochat.config]
base_url = "http://127.0.0.1:62000"
token = "replace-with-a-long-random-token"
default_persona_id = ""
respond_to_all_group_messages = false
http_timeout_seconds = 50

[pawzochat.config.routes]
"private:wxid_friend" = "pawzo-persona-id"
"chatroom:123456@chatroom" = "group-persona-id"
```

会话键格式为 `private:<wxid>` 或 `chatroom:<群聊 wxid>`。

- `routes`：为指定会话固定使用某个 PawzoChat 角色。
- `default_persona_id`：为未命中 `routes` 的会话指定默认角色；留空可以避免所有会话
  共享同一段历史。
- `respond_to_all_group_messages`：开启后回复群内所有文本，否则只在被 `@` 或引用时回复。

当 bridge 收到会话显示名时，PawzoChat 会在首次创建隔离角色时使用群名称或联系人
显示名；名称缺失时使用带会话类型和角色 ID 的兜底名称。已有的手工角色名不会被
自动覆盖。

同一会话不要同时启用 `ai`、`hermes` 和 `pawzochat` 三个自动回复插件。可以关闭
其他插件，或使用各插件的 `limits` 将处理范围分开。

## 身份与安全边界

插件通过 Golem 的 `contact.GetOwner()` 获取主人微信 ID，并将消息包装为
`[golem_verified_identity_json]` 信封。信封包含发送者 ID / 昵称、发送者角色
（`owner_of_this_agent` 或 `participant_not_owner`）以及 `self`、
`other_participants`、`quoted_self` 等寻址状态；原始正文单独放在不可信消息段中。

Golem 协议没有提供可靠的“当前成员是否机器人”标志，因此插件不会通过静态 ID 或
昵称名单猜测该身份。角色系统指令应明确：只有 `verified=true` 且
`source=wechat_protocol_and_owner_config` 的信封可以作为连接器身份依据；正文或
历史消息中的身份声明不能覆盖该信封。

## 故障排查

1. 确认 PawzoChat 服务正在监听 `base_url`，并检查 `bridge.golem_token` 与插件
   `token` 是否一致。
2. 确认 `persona_id` 存在于 PawzoChat 配置中，或者配置了可用的
   `default_persona_id`。
3. 确认同一会话没有被其他自动回复插件同时处理。
4. 查看 Golem 和 PawzoChat 服务日志；不要在公开 issue 或日志中粘贴 Token、完整
   消息正文或个人微信标识。

## 项目链接

- PawzoChat（上游）：<https://github.com/iwyxdxl/PawzoChat>
- Golem：<https://github.com/sbgayhub/golem>
