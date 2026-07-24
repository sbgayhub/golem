# Demos 插件

娱乐合集插件：把大量「关键词 → 第三方 API → 文本/图片/视频/语音」能力收拢到一个插件里。  
既可聊天触发，也可通过 `demos.run` 能力被 cron 等插件调用。

## 功能特性

- **关键词表驱动**：精确匹配或「关键词 + 空格 + 参数」，长关键词优先
- **多媒体出口**：文本、图片（CDN）、原生视频（CDN，可回退链接卡片）、语音
- **可调用能力**：`demos.run`，`text` 写法与用户消息一致，方便定时任务
- **容错**：handler 异常时友好提示，不把错误堆栈甩给用户
- **零业务配置可跑**：默认即可用；百科等可选能力需自行配 URL

## 快速开始

群聊 / 私聊发送：

```
demos
demos帮助
```

会回复完整功能清单。常用例子：

```
撸猫
一言
小姐姐视频
百度百科 人工智能
火子搜歌 晴天
火子点歌 123456
婚宴请柬 张三,李四,王五
```

## 命令一览

### 图片

| 命令 | 说明 |
|------|------|
| `撸猫` | 随机猫图 |
| `旺财` | 随机狗图 |
| `看星空` | NASA 天文图 + 讲解 |
| `名画赏析` | 随机名画 + 赏析 |
| `小黑子表情` | 随机相关图 |
| `随机二次元` | 随机二次元图 |
| `acg美图` | ACG 美图 |

### 视频

| 命令 | 说明 |
|------|------|
| `小姐姐视频` / `--小姐姐视频` | 两个不同源的随机小姐姐视频 |
| `热点视频` / `娱乐视频` / `靓仔视频` | 随机短视频 |
| `懒羊羊k歌` / `怼脸自拍视频` / `看穿搭` / `丝滑舞蹈视频` / `快手随机翻唱` | 各类随机短视频 |

视频默认优先 **CDN 原生上传**（`video_native=true`）；失败自动回退为「视频链接」卡片。

### 文字 / 语音

| 命令 | 说明 |
|------|------|
| `温馨提示` | 问候 + 生活小贴士 |
| `一句` / `一言` / `今日句子` | 随机句子 |
| `吟诗` | 随机诗词 |
| `讲笑话` / `脑筋急转弯` / `来段绕口令` / `谚语` | 趣味文本 |
| `抽签` / `答案之书` / `吃什么` | 随机决策 |
| `保安日记` | 随机保安日记 |
| `king台词` / `l台词` | 王者 / LOL 台词 + 英雄图 |
| `随机坤坤` | 随机语音 |

### 搜索

| 命令 | 说明 |
|------|------|
| `百度百科 <词条>` | 查百科（需配置 `bdbk_url`） |
| `搜短剧 <名称>` | 搜短剧观看链接（条数受 `max_list` 限制） |

### 生成

| 命令 | 说明 |
|------|------|
| `婚宴请柬 新郎,新娘,邀请人` | 生成请柬图（英文逗号分隔三参数） |
| `狗屁不通文章生成 <主题>` | 生成水文 |

### 音乐

| 命令 | 说明 |
|------|------|
| `随机唱` | 随机翻唱（歌词 + 封面 + 语音） |
| `火子搜歌 <关键词>` | 酷我搜歌，返回点歌号列表 |
| `火子点歌 <点歌号>` | 按点歌号播放 |

## 能力：demos.run

供 `cron` 等插件通过 `OnCall` 调用，入参与用户发消息一致。

**入参** `map[string]string`：

| key | 必需 | 说明 |
|-----|------|------|
| `receiver` | 是 | 接收者 username（好友 wxid 或 `xxx@chatroom`） |
| `text` | 是 | 与用户消息相同，如 `温馨提示`、`百度百科 中国` |

**返回**：成功 `mime="none"`（handler 已自行发送，调用方勿再发）；失败返回错误（未匹配、联系人不存在等）。

### 定时示例（配合 cron）

```bash
# 每天 9 点向某群发温馨提示
/cron add -c "0 9 * * *" -p demos.run -t "123456@chatroom" -a "text=温馨提示"

# 每小时撸猫
/cron add -c "0 * * * *" -p demos.run -t "123456@chatroom" -a "text=撸猫"
```

## 配置

```toml
[demos]
video_native = true   # true：优先 CDN 原生视频；失败回退链接卡片
max_list = 3          # 列表类结果最多展示条数（如搜短剧）
bdbk_url = ""         # 百度百科 API 前缀（插件会把关键词 URL 编码后拼接）；空则百科不可用
silk_encoder_path = "" # silk_v3_encoder.exe 绝对路径；空则语音转码回退 AMR（PC 微信放不了）
silk_sample_rate = 24000 # SILK 编码采样率(Hz)
silk_max_bytes = 28000 # 单条语音字节预算；超预算自动降码率/裁时长，0 关闭
```

| 字段 | 默认 | 说明 |
|------|------|------|
| `video_native` | `true` | 原生视频需本机 `ffmpeg`/`ffprobe` |
| `max_list` | `3` | 列表截断 |
| `bdbk_url` | `""` | 百科接口前缀，需自行配置 |
| `silk_encoder_path` | `""` | silk_v3_encoder.exe 路径；配置后语音转 SILK（微信全端可播），否则回退 AMR |
| `silk_sample_rate` | `24000` | ffmpeg 转 PCM 与 silk 编码（`-Fs_API`）共用的采样率 |
| `silk_max_bytes` | `28000` | 上传通道单条语音超约 28KB 会被截断（播放戛然而止）；超预算先降码率（下限 8kbps）再裁时长 |

配置默认值只在 `main()` 的 `Config{...}` 里给，不要在代码里再写 ensureDefaults。

## 工作原理

```
OnEvent(文本) / OnCall(demos.run)
  → 解析 receiver + text
  → 按关键词长度降序匹配（精确 或 「key + 空格 + 参数」）
  → handler：HTTP 拉 API → 解析 → sendText / sendImage / sendVideoOrCard / sendVoice
```

图片走 `cdn.UploadImage`；原生视频下载后 `ffprobe` 时长 + `ffmpeg` 缩略图，再 `cdn.UploadVideo`。

## 文件结构

```
plugins/demos/
├── main.go            # 入口、默认配置、关键词 → handler 注册
├── plugin.go          # 元数据、OnLoad/OnUnload、OnEvent
├── config.go          # Config
├── capability.go      # demos.run、dispatch
├── handler_image.go   # 图片类
├── handler_video.go   # 视频类
├── handler_text.go    # 文本 / 语音类
├── handler_search.go  # 搜索 / 生成 / 音乐 / 帮助
├── media.go           # HTTP、CDN 发媒体、ffmpeg 工具
└── go.mod
```

## 构建

```bash
task build:demos
# 产物：host/plugins/golem_plugin_demos.exe
```

加载：`/pm reload demos`

## 开发信息

| 项 | 值 |
|----|-----|
| 名称 | `demos` |
| 版本 | 1.1.0 |
| 作者 | Golem Team |
| Priority | 0 |
| Next | false（命中即终止事件链） |
| 订阅 | `message.text` |
| 能力 | `demos.run` |
| SDK | message / contact / cdn / ConfigAbility |

## 注意事项

1. 几乎全部功能依赖第三方 HTTP API，挂了会「翻车」提示或业务失败文案
2. 原生视频 / 语音抽时长需要本机 `ffmpeg`/`ffprobe`
3. 语音走 SILK 时必须是**腾讯变体**（`0x02` + `#!SILK_V3` 头）：编码命令带 `-tencent`，标准 SILK 微信全端都判损坏；AMR 只有安卓手机微信能播，PC 端无解码器
4. `百度百科` 未配 `bdbk_url` 时会提示联系管理员
5. 关键词表较长，注意与其它插件触发词冲突；本插件 `Next=false`，命中后不再传给后续插件
6. 内容来源与尺度请自行合规；部分关键词仅供娱乐

## 常见问题

**Q: 发视频变成链接卡片？**  
A: `video_native=false`，或原生上传失败（无 ffmpeg、下载失败、CDN 错误）。日志里有 `原生视频发送失败，使用链接卡片`。

**Q: cron 调 demos 没反应？**  
A: 确认能力名是 `demos.run`，且 args 含 `receiver` + `text`；`text` 必须能匹配已注册关键词。

**Q: 新加一个关键词怎么写？**  
A: 在 `main.go` 的 `handlers` 注册；实现 `handlerFunc`；需要参数时用 `key + " "` 前缀匹配（见 `dispatch`）。
