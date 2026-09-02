// hermes_bridge：微信 ⇄ Hermes 官方平台适配器之间的 Golem 侧桥。
//
// 入站：订阅微信消息，经会话白名单后：
//   - 群聊：本地滚动上下文；仅 @/引用 bot / trigger_names / bubble 才去抖后一批 SSE；
//   - 私聊：逐条立即 SSE（连发排队见适配器项 2）；
//   - 审批捷径 yes/no、打断捷径「打断」始终立即透传（打断另作废该会话未推去抖批）。
//
// 出站：适配器 HTTP POST /send /send_image /send_video /send_voice /send_emoji 等，
// 本插件代为发到微信（图片/视频/语音/表情走 message.Send；表情 TypeEmoji 且过大自动压到约 500KB；视频带 Thumb+Duration；不做 MCP/agent run）。
// 真 @：POST /send 的 mentions=真实 wxid（TextData.Reminds），且正文须含「@显示名 + U+2005」；桥会在缺正文@时自动补全。
// 查询：GET /self、/group_info、/group_members、POST /group_member_detail（供 agent 先查 wxid 再 send）。
//
// 不再内嵌 MCP，不再触发 Hermes API Server——那是平台适配器 + gateway 的职责。
package main

import (
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sbgayhub/golem/sdk/cdn"
	"github.com/sbgayhub/golem/sdk/chatroom"
	"github.com/sbgayhub/golem/sdk/contact"
	"github.com/sbgayhub/golem/sdk/message"
	"github.com/sbgayhub/golem/sdk/plugin"
)

// Target 会话路由白名单项
type Target struct {
	ID   string `toml:"id" comment:"会话 wxid（群聊形如 xxx@chatroom，私聊为对方 wxid）"`
	Name string `toml:"name" comment:"会话名称，便于识别"`
}

// Config 插件配置。默认值只在 main 的 Config{...} 里给。
type Config struct {
	// Listen 桥 HTTP 监听地址，建议绑定宿主机对 VM 可达的网卡
	Listen string `toml:"listen" comment:"桥 HTTP 监听地址，如 0.0.0.0:8643"`
	// Token Bearer 认证；留空时拒绝所有请求
	Token string `toml:"token" comment:"桥 Bearer token，与 Hermes 侧 WECHAT_GOLEM_TOKEN 一致"`
	// MaxTextLen 单条发送文本最大字符数
	MaxTextLen int `toml:"max_text_len" comment:"单条发送文本最大字符数"`
	// SendRatePerMin 出站全局限流（条/分钟，含文本与媒体）
	SendRatePerMin int `toml:"send_rate_per_min" comment:"出站全局限流（条/分钟）"`
	// MaxBodyBytes 单次请求 body 上限（含 base64 媒体）
	MaxBodyBytes int64 `toml:"max_body_bytes" comment:"单次请求 body 上限字节数，默认 80MB"`
	// Targets 会话级路由白名单；主人私聊始终放行
	Targets []Target `toml:"targets" comment:"会话路由白名单（主人私聊始终允许）"`

	// TriggerNames 群聊中消息包含这些名字也触发（@ 与引用 bot 始终触发）
	TriggerNames []string `toml:"trigger_names" comment:"群聊中包含这些名字也触发 agent（@/引用始终触发）"`
	// BubbleRate 未点名群消息的冒泡触发概率 0~1
	BubbleRate float64 `toml:"bubble_rate" comment:"未点名群消息冒泡触发概率 0~1，0 关闭"`
	// BubbleCooldownMin 同会话两次冒泡最小间隔（分钟）
	BubbleCooldownMin int `toml:"bubble_cooldown_minutes" comment:"同会话两次冒泡最小间隔分钟数"`
	// DebounceSeconds 群触发后合并后续消息再推 SSE 的秒数
	DebounceSeconds int `toml:"debounce_seconds" comment:"群触发后去抖合并秒数，同会话只保留一个 timer"`
	// MaxContextMessages 每会话本地滚动上下文最多条数
	MaxContextMessages int `toml:"max_context_messages" comment:"每会话本地滚动上下文最多保留条数"`
	// GroupPushAll true=白名单群每条都推（回滚旧行为）；默认 false=须门闩。
	// 用「默认 false 才开门闩」，避免旧 config.toml 缺字段时 bool 零值把门闩关掉。
	GroupPushAll bool `toml:"group_push_all" comment:"true 时白名单群每条都推 SSE，关闭门闩（回滚用）"`

	// EmojiBurstCount 斗图门闩：群聊滑动窗口内第 N 条表情触发一次批次推送（0 关闭）。
	// 让 agent 看到表情连发（斗图/自动收藏的入口）；addressing 仍为 none，只解释送达原因。
	EmojiBurstCount int `toml:"emoji_burst_count" comment:"群聊表情连发触发阈值：窗口内第 N 条表情推一批，0 关闭"`
	// EmojiBurstWindowSec 表情连发计数窗口（秒）
	EmojiBurstWindowSec int `toml:"emoji_burst_window_seconds" comment:"表情连发计数窗口秒数"`
	// EmojiBurstCooldownMin 同会话两次表情连发触发最小间隔（分钟）
	EmojiBurstCooldownMin int `toml:"emoji_burst_cooldown_minutes" comment:"同会话两次表情连发触发最小间隔分钟数"`

	// ---- 控制捷径词表 ----
	// 这四组词由桥判定后旁路群门闩立即透传；适配器侧有同名 env
	// （WECHAT_GOLEM_INTERRUPT_TOKENS / _RESET_TOKENS / _ARCHIVE_TOKENS）。
	// 两侧必须一致：桥不认的词会被群门闩吞掉，适配器根本收不到，
	// 症状是「改了 env 完全没反应」。桥的生效词表由 GET /health 暴露，
	// 适配器连上后会比对并告警。留空 = 用内置默认集（见 session.go）。
	//
	// InterruptTokens 打断捷径整句词（不限主人）
	InterruptTokens []string `toml:"interrupt_tokens" comment:"打断捷径整句词；空=默认[\"打断\"]；须与适配器 WECHAT_GOLEM_INTERRUPT_TOKENS 一致"`
	// SessionResetTokens 新开会话捷径整句词（仅主人）
	SessionResetTokens []string `toml:"session_reset_tokens" comment:"新开会话整句词；空=默认[\"新开会话\",\"新对话\"]；须与适配器 WECHAT_GOLEM_RESET_TOKENS 一致"`
	// ArchiveTokens 归档群友捷径整句词（仅主人）
	ArchiveTokens []string `toml:"archive_tokens" comment:"归档捷径整句词；空=默认5词；须与适配器 WECHAT_GOLEM_ARCHIVE_TOKENS 一致"`
	// ApprovalTokens 审批捷径整句词（仅主人）；两词组合 all/approve/deny + session/always/once/all 始终生效
	ApprovalTokens []string `toml:"approval_tokens" comment:"审批捷径整句词；空=默认16词（yes/no/是/否/同意…）"`
	// RevokeTokens 撤回捷径整句词（仅主人）。桥内闭环：命中即撤自己最近一条，
	// 不推 SSE、不惊动 agent，故**无需**与适配器 env 同步（适配器侧没有对应词表）。
	RevokeTokens []string `toml:"revoke_tokens" comment:"撤回捷径整句词（仅主人，桥内直接撤，不推 SSE）；空=默认[\"撤回\",\"撤回吧\",\"撤回上一条\"]"`
	// RevokeWindowSec 撤回时间窗（秒）。微信规则为 120s；超窗时桥直接拒绝并说明，
	// 因为 host 的 Revoke 无论微信是否真撤都回成功，发出去只会得到假成功。
	// 负数=不卡窗口（自担风险），0=用默认 120。
	RevokeWindowSec int `toml:"revoke_window_seconds" comment:"撤回时间窗秒数，0=默认120（微信规则）；负数=不检查"`

	// SilkEncoderPath silk_v3_encoder 可执行文件路径，用于语音转腾讯变体 SILK（优先）。
	// 若未配置或执行失败则降级为纯 ffmpeg AMR。Windows 填 …\silk_v3_encoder.exe，
	// Linux/macOS 填自行编译出的可执行文件路径。
	SilkEncoderPath string `toml:"silk_encoder_path" comment:"silk_v3_encoder 可执行文件路径（语音优先 SILK；Windows 为 .exe）"`

	// FFmpegPath ffmpeg 可执行文件路径；留空则在 PATH 中查找。
	// 语音转码、表情/视频处理、视频抽封面都要它；装了但没进 PATH 是最常见的部署故障。
	FFmpegPath string `toml:"ffmpeg_path" comment:"ffmpeg 可执行文件路径；留空=在 PATH 中查找"`
	// FFprobePath ffprobe 可执行文件路径；留空则在 PATH 中查找。
	// 取音频/视频时长用；与 ffmpeg 可能不在同一目录，故单独配置。
	FFprobePath string `toml:"ffprobe_path" comment:"ffprobe 可执行文件路径；留空=在 PATH 中查找"`

	// SilkMaxBytes 单条语音 SILK 编码后最大字节数，超预算先降码率再裁时长。
	// 微信语音通道约 1MB 上限，但小预算有利于稳定编码；0 不限制。
	SilkMaxBytes int `toml:"silk_max_bytes" comment:"单条语音 SILK 最大字节数，0=不限制"`

	// SilkSampleRate SILK 编码采样率，24000 效果较好；设 0 使用默认 24000。
	SilkSampleRate int `toml:"silk_sample_rate" comment:"SILK 编码采样率，默认 24000"`

	// RecordImageVia 仅影响「url 嵌记录」实验路径（默认空=拒绝 url 嵌图，只允许 media_ref）。
	//   "" / off     = 产品默认：url 直接报错，请 media_ref 或普通发图
	//   "send"       = 实验：对目标会话 message.Send 图拿 CDN（手机可点开，会多临时图）
	//   "cdn"        = 实验：filehelper 静默 Upload（不叠图，手机常「过期」）
	// RecordImageRevoke：仅 via=send 时，发完记录是否撤回临时图（默认 false 不撤）。
	RecordImageVia    string `toml:"record_image_via" comment:"url嵌记录实验：空=关（仅media_ref）；send=会话Send；cdn=filehelper Upload"`
	RecordImageRevoke *bool  `toml:"record_image_revoke" comment:"via=send 时是否撤回临时图；默认 false"`

	// RecordXMLDumpDir 诊断用：把出站聊天记录卡片 XML 落盘到该目录，便于与真机 dump 对比。
	// 空 = 不落盘（默认）。勿填相对路径：host 的工作目录随启动方式变化。
	RecordXMLDumpDir string `toml:"record_xml_dump_dir" comment:"诊断：出站记录卡片 XML 落盘目录（绝对路径）；空=不落盘"`

	// AdminListen 管理台 HTTP 监听；默认仅本机，与业务桥 Listen 分离。
	// 空字符串 = 不启管理台。远程管理请走 Tailscale 等，勿轻易改成 0.0.0.0。
	AdminListen string `toml:"admin_listen" comment:"管理台监听，默认 127.0.0.1:8644；空=关闭；勿与业务口混用 0.0.0.0"`
	// AdminToken 管理 API / UI 登录用 Bearer；须与业务 Token 不同。空则管理 API 拒绝读写。
	AdminToken string `toml:"admin_token" comment:"管理台 Bearer token，勿与业务 token 相同"`

	// HermesOpsURL Hermes 所在主机上 hermes_ops 的基址（只读运维代理）。空=管理台 Hermes 页不可用。
	// 例：http://<hermes-host>:8650 （与业务桥 8643 不同口；填 Hermes 侧地址，不是桥自己的地址）
	HermesOpsURL string `toml:"hermes_ops_url" comment:"hermes_ops 基址，如 http://<hermes-host>:8650；空=关闭 Hermes 只读页"`
	// HermesOpsToken 调用 ops 时的 Bearer；可与 admin_token 不同；空则代理不带鉴权头
	HermesOpsToken string `toml:"hermes_ops_token" comment:"调用 hermes_ops 的 Bearer，建议与 ops 侧一致"`
}

// BridgePlugin 插件主结构
type BridgePlugin struct {
	plugin.ConfigAbility[Config]
	message  message.Ability
	contact  contact.Ability
	chatroom chatroom.Ability
	cdn      cdn.Ability

	cfgMu sync.RWMutex

	selfMu sync.RWMutex
	self   *contact.SelfInfo
	owner  *contact.Contact

	hub        *sseHub
	adminTrace *adminTraceHub

	rateMu    sync.Mutex
	sendTimes []time.Time

	srvMu    sync.Mutex
	httpSrv  *http.Server
	adminSrv *http.Server

	dlClient    *http.Client
	mediaClient *http.Client

	// 群聊本地上下文与去抖
	sessMu     sync.Mutex
	sessions   map[string]*sessionState
	bubbleMu   sync.Mutex
	lastBubble map[string]time.Time
	stopped    atomic.Bool

	// 斗图门闩：会话 → 窗口内表情时间戳 / 上次触发时间（见 session.go recordEmojiAndCheckBurst）
	burstMu   sync.Mutex
	emojiSeen map[string][]time.Time
	lastBurst map[string]time.Time

	// 入站媒体懒下载引用表：OnEvent 只登记 ref，agent 经 /media 按需取（见 mediaref.go）
	mediaMu   sync.Mutex
	mediaRefs map[string]*inboundMedia
	mediaSeq  atomic.Uint64

	// via=send 嵌图时产生的临时图片 NewId，send_record 成功后撤回
	recordRevokeMu    sync.Mutex
	recordRevokeQueue []recordRevokeJob

	// 出站消息记账：会话 → 最近发出的 NewMsgID，供「撤回」定位（见 outbox.go）
	outMu  sync.Mutex
	outbox map[string][]outboxEntry
}

func (p *BridgePlugin) configSnapshot() Config {
	p.cfgMu.RLock()
	defer p.cfgMu.RUnlock()
	return p.Config
}

func main() {
	p := &BridgePlugin{
		ConfigAbility: plugin.ConfigAbility[Config]{
			Config: Config{
				Listen:             "0.0.0.0:8643",
				Token:              "",
				MaxTextLen:         2000,
				SendRatePerMin:     20,
				MaxBodyBytes:       80 << 20,
				Targets:            []Target{},
				TriggerNames:       []string{},
				BubbleRate:         0.1,
				BubbleCooldownMin:  10,
				DebounceSeconds:    3,
				MaxContextMessages: 40,
				GroupPushAll:       false,
				// 斗图门闩默认开：30s 内第 3 条表情推一批，同会话 5 分钟最多触发一次
				EmojiBurstCount:       3,
				EmojiBurstWindowSec:   30,
				EmojiBurstCooldownMin: 5,
				// 捷径词表留空 = 用 session.go 的内置默认集；显式配置后须与适配器 env 同步
				InterruptTokens:    []string{},
				SessionResetTokens: []string{},
				ArchiveTokens:      []string{},
				ApprovalTokens:     []string{},
				// 撤回捷径：桥内闭环，无适配器侧 env 需要同步
				RevokeTokens:    []string{},
				RevokeWindowSec: defaultRevokeWindowSec,
				// 外部程序留空 = 在 PATH 中查找
				FFmpegPath:       "",
				FFprobePath:      "",
				RecordXMLDumpDir: "",
				// 管理台默认只绑本机，与业务 0.0.0.0:8643 分离
				AdminListen:    "127.0.0.1:8644",
				AdminToken:     "",
				HermesOpsURL:   "",
				HermesOpsToken: "",
			},
		},
		hub:         newSSEHub(),
		adminTrace:  newAdminTraceHub(),
		sessions:    map[string]*sessionState{},
		lastBubble:  map[string]time.Time{},
		emojiSeen:   map[string][]time.Time{},
		lastBurst:   map[string]time.Time{},
		mediaRefs:   map[string]*inboundMedia{},
		outbox:      map[string][]outboxEntry{},
		dlClient:    &http.Client{Timeout: 20 * time.Second},
		mediaClient: &http.Client{Timeout: 120 * time.Second},
	}
	if err := registerCommands(p); err != nil {
		slog.Error("[hermes_bridge] 注册命令失败", "err", err)
		return
	}
	slog.Info("[hermes_bridge] 插件启动中...")
	plugin.Start(p)
}
