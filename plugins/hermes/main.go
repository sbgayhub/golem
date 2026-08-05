// hermes 插件：已弃用。请改用 hermes_bridge + Hermes wechat_golem 平台适配器。
//
// 旧架构（仅供阅读/回滚）：
// 入站：订阅微信消息进滚动上下文，被 @/引用/点名/冒泡时触发一次 agent run，
// 通过 Hermes API Server（OpenAI Chat Completions 兼容）把会话上下文推给 agent。
// 出站：内嵌 MCP server（streamable HTTP），Hermes 通过 wechat_* 工具回复微信。
//
// OnLoad 已直接 return，不再启动 MCP / worker。
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

// Target 白名单会话：既是允许交互（消息进上下文、可触发）的范围，也是允许发送的目标
type Target struct {
	ID        string `toml:"id" comment:"会话 wxid（群聊形如 xxx@chatroom，私聊为对方 wxid）"`
	Name      string `toml:"name" comment:"会话名称，帮助 agent 辨识目标"`
	Proactive bool   `toml:"proactive" comment:"Hermes 自主任务（无微信触发）时是否允许发送到该会话"`
}

// Config 插件配置
type Config struct {
	BaseURL            string   `toml:"base_url" comment:"Hermes API Server 地址（含 /v1），如 http://192.168.x.x:8642/v1"`
	APIKey             string   `toml:"api_key" comment:"Hermes 的 API_SERVER_KEY"`
	Model              string   `toml:"model" comment:"模型名，默认 profile 为 hermes-agent，自建 profile 为其名称"`
	HTTPTimeoutSeconds int      `toml:"http_timeout_seconds" comment:"等待 agent 完成一次 run 的超时秒数（agent 会跑工具，建议 180~600）"`
	MCPListen          string   `toml:"mcp_listen" comment:"内嵌 MCP server 监听地址，建议填仅虚拟机可达的宿主机网卡 IP"`
	MCPToken           string   `toml:"mcp_token" comment:"MCP Bearer 认证 token，留空时拒绝所有调用"`
	TriggerNames       []string `toml:"trigger_names" comment:"群聊中消息包含这些名字也触发（@ 和引用始终触发）"`
	BubbleRate         float64  `toml:"bubble_rate" comment:"未点名群消息的冒泡触发概率 0~1，0 关闭"`
	BubbleCooldownMin  int      `toml:"bubble_cooldown_minutes" comment:"同会话两次冒泡的最小间隔分钟数"`
	DebounceSeconds    int      `toml:"debounce_seconds" comment:"触发后等待合并后续消息的秒数"`
	MaxContextMessages int      `toml:"max_context_messages" comment:"每会话滚动上下文最多保留的消息条数"`
	FallbackReply      bool     `toml:"fallback_reply" comment:"agent 未调用发送工具时，把它的文本响应兜底发回触发会话"`
	SendRatePerMin     int      `toml:"send_rate_per_min" comment:"MCP 发送类工具的全局限流（条/分钟）"`
	MaxTextLen         int      `toml:"max_text_len" comment:"单条发送文本的最大字符数"`
	ExtraPrompt        string   `toml:"extra_prompt" comment:"追加到场景说明末尾的自定义提示词（人格请配在 Hermes profile 侧）"`
	Targets            []Target `toml:"targets" comment:"会话白名单（主人私聊始终允许，可用 hermes启用/hermes停用 管理）"`
}

// chatMessage OpenAI Chat Completions 格式的消息
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// runContext 一次 agent run 的触发上下文，是 MCP 工具权限判定的唯一依据
type runContext struct {
	SessionKey string           // 触发会话 key（chatroom:xxx / private:xxx）
	TargetID   string           // 触发会话 wxid
	TargetName string           // 触发会话显示名
	SenderID   string           // 触发者 wxid（协议层身份，非消息内容）
	SenderName string           // 触发者昵称
	IsOwner    bool             // 触发者是否为主人（与 host owner 硬比对的结果）
	IsChatroom bool             // 是否群聊
	Receiver   *contact.Contact // 触发会话联系人（兜底回复用）
	Sent       atomic.Int32     // 本次 run 经 MCP 工具发出的消息条数
	// Token 本次 run 的一次性令牌：processRun 生成、只写进本次 system prompt，
	// 工具调用带回 run_token 与之比对，对上才认领 rc。防止插件外发起的
	// run（Hermes cron/CLI/自主任务）撞车时借用本 run 的权限档。
	Token string
}

// HermesPlugin 插件主结构
type HermesPlugin struct {
	plugin.ConfigAbility[Config]
	message  message.Ability
	contact  contact.Ability
	chatroom chatroom.Ability
	cdn      cdn.Ability
	caller   plugin.CallerAbility

	cfgMu sync.RWMutex

	selfMu sync.RWMutex
	self   *contact.SelfInfo
	owner  *contact.Contact

	sessMu   sync.Mutex
	sessions map[string][]chatMessage

	runMu     sync.RWMutex
	activeRun *runContext

	queueCh chan *runContext
	pendMu  sync.Mutex
	pending map[string]bool

	bubbleMu   sync.Mutex
	lastBubble map[string]time.Time

	rateMu    sync.Mutex
	sendTimes []time.Time

	srvMu   sync.Mutex
	httpSrv *http.Server

	client      *http.Client // 调用 Hermes API Server（超时由每次请求的 ctx 控制）
	dlClient    *http.Client // 下载图片（独立短超时）
	mediaClient *http.Client // 下载语音/视频（较长超时）
	workerOnce  sync.Once
	stopCh      chan struct{}
}

// configSnapshot 线程安全地获取配置快照（Targets 为共享底层数组，修改需整体替换）
func (p *HermesPlugin) configSnapshot() Config {
	p.cfgMu.RLock()
	defer p.cfgMu.RUnlock()
	return p.Config
}

// main 插件入口。配置默认值只在此处给。
func main() {
	p := &HermesPlugin{
		ConfigAbility: plugin.ConfigAbility[Config]{
			Config: Config{
				BaseURL:            "http://127.0.0.1:8642/v1",
				APIKey:             "",
				Model:              "hermes-agent",
				HTTPTimeoutSeconds: 300,
				MCPListen:          "0.0.0.0:8643",
				MCPToken:           "",
				TriggerNames:       []string{},
				BubbleRate:         0,
				BubbleCooldownMin:  10,
				DebounceSeconds:    3,
				MaxContextMessages: 40,
				FallbackReply:      true,
				SendRatePerMin:     10,
				MaxTextLen:         2000,
				ExtraPrompt:        "",
				Targets:            []Target{},
			},
		},
		sessions:    map[string][]chatMessage{},
		pending:     map[string]bool{},
		lastBubble:  map[string]time.Time{},
		queueCh:     make(chan *runContext, 16),
		client:      &http.Client{},
		dlClient:    &http.Client{Timeout: 20 * time.Second},
		mediaClient: &http.Client{Timeout: 120 * time.Second},
		stopCh:      make(chan struct{}),
	}
	slog.Info("[hermes] 插件启动中...")
	plugin.Start(p)
}
