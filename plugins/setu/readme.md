# Setu 插件

色图 / 搜图插件：关键词触发随机图（或视频），支持按关键词搜索图片。图片与视频均走 CDN 流式上传，避开 `message.Send` 的 gRPC 4MB 限制。

## 功能特性

- **关键词触发**：漂亮妹妹、黑丝 / 白丝、看看腿、帅哥等一键出图
- **概率视频**：黑丝 / 白丝可按配置概率出视频，失败自动降级图片
- **关键词搜图**：`来点<关键词>` 调百度表情搜图 API，随机返回一张
- **CDN 发送**：图片 `cdn.UploadImage`、视频 `cdn.UploadVideo`（含时长与缩略图）
- **诊断命令**：`setu测cdn` 用于验证 CDN 发图链路是否正常

## 使用方式

在群聊或私聊中发送：

| 命令 | 说明 |
|------|------|
| `setu帮助` / `色图帮助` | 查看用法 |
| `plmm` / `漂亮妹妹` / `来点美女` | 随机美女图 |
| `来点黑丝` | 随机黑丝（有概率为视频） |
| `来点白丝` | 随机白丝（有概率为视频） |
| `看看腿` | 黑丝 / 白丝二选一 |
| `来点帅哥` | 随机帅哥图 |
| `来点<关键词>` | 按关键词搜图，如 `来点柯基` |
| `setu测cdn` | 用默认猫图测 CDN 发图 |
| `setu测cdn <url>` | 用指定图片 URL 测 CDN 发图 |

> `来点` 后至少再跟一个字符才会进入搜图；`来点黑丝` / `来点白丝` / `来点帅哥` 等精确词优先走专用接口，不会当普通搜图。

## 配置

默认值在 `main()` 的 `Config{...}` 中给出，宿主首次启动会写入配置文件。可按需改 API 地址与视频概率：

```toml
[setu]
img_url = "https://api.52vmy.cn/api/img/tu/girl?type=text"   # 美女图片 API（返回图片 URL 文本）
boy_url = "https://api.52vmy.cn/api/img/tu/boy?type=text"    # 帅哥图片 API
heisi_url = "http://api.yujn.cn/api/heisi.php?"              # 黑丝图片
baisi_url = "http://api.yujn.cn/api/baisi.php?"              # 白丝图片
heisi_video_url = "http://api.yujn.cn/api/heisis.php?type=video"
baisi_video_url = "http://api.yujn.cn/api/baisis.php?type=video"
# 搜图默认用 apihz.cn 的百度表情搜图；id/key 请换成自己的
search_url = "https://cn.apihz.cn/api/img/apihzbqbbaidu.php?id=88888888&key=88888888&limit=10&page=1&words="
video_rate = 50   # 黑丝/白丝触发视频的概率 0–100
```

| 字段 | 说明 |
|------|------|
| `img_url` / `boy_url` | GET 后响应体即为图片直链（`type=text` 类接口） |
| `heisi_url` / `baisi_url` | 黑丝 / 白丝图片接口 |
| `heisi_video_url` / `baisi_video_url` | 黑丝 / 白丝视频接口 |
| `search_url` | 搜图 API 前缀，插件会把关键词 URL 编码后拼在后面 |
| `video_rate` | 黑丝 / 白丝走视频的概率；视频失败会降级发图 |

## 工作流程

```
文本消息
  → 精确关键词（帮助 / plmm / 黑丝 / 白丝 / 看看腿 / 帅哥 / setu测cdn）
  → 或「来点」+ 关键词搜图
  → HTTP 拉 URL 或 JSON 结果
  → 下载媒体
  → cdn.UploadImage / UploadVideo 发送
  → 失败时图片可降级发链接文本
```

### 视频发送

1. 下载到临时 `mp4`
2. `ffprobe` 取时长（失败默认 10 秒）
3. `ffmpeg` 抽第 1 秒帧作缩略图
4. `cdn.UploadVideo(receiver, thumb, video, duration)`

**依赖本机已安装 `ffmpeg` / `ffprobe`**，否则视频能力不可用（会降级或失败）。

## 文件结构

```
plugins/setu/
├── main.go      # 入口、默认配置、HTTP 客户端
├── plugin.go    # 元数据、生命周期、OnEvent 分发
├── config.go    # Config 结构
├── handler.go   # 各关键词处理、搜图解析
├── media.go     # 下载、发图/发视频、ffprobe/ffmpeg
└── go.mod
```

## 构建

```bash
# 在 plugins/ 下
task build:setu
# 产物：host/plugins/golem_plugin_setu.exe
```

或：

```bash
cd plugins/setu
go build -ldflags "-s -w" -o golem_plugin_setu.exe .
```

加载：`/pm reload setu`

## 开发信息

| 项 | 值 |
|----|-----|
| 名称 | `setu` |
| 版本 | 1.0.1 |
| 作者 | Golem Team |
| Priority | -100 |
| 订阅 | `message.text` |
| 能力 | 无（纯事件驱动） |
| SDK | message / contact / cdn / ConfigAbility |

## 注意事项

1. 第三方图床 / 搜图 API 可用性不受控，挂了会提示失败
2. 搜图接口默认 `id`/`key` 为占位，生产环境请换成自己的 [apihz](https://www.apihz.cn/api/apihzbqbbaidu.html) 密钥
3. 发视频需本机 `ffmpeg`/`ffprobe` 在 PATH 中
4. 内容尺度与版权请自行合规使用；群聊慎开
5. `setu测cdn` 用于对照 hermes 等其它发图路径，不降级吞错，失败会明文回执

## 常见问题

**Q: 发图失败，只收到链接？**  
A: CDN 上传失败时图片会降级发 URL。可用 `setu测cdn` 单独测 CDN；检查网络与 Host 微信登录态。

**Q: 黑丝/白丝从不出现视频？**  
A: 看 `video_rate`；再确认 `ffmpeg` 是否可用、视频 API 是否返回有效地址。

**Q: `来点xx` 没反应？**  
A: 关键词为空不会处理；与精确命令冲突时以精确命令为准；搜图 JSON `code != 200` 或 `res` 为空会提示失败。
