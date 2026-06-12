package main

import (
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/sbgayhub/golem/sdk/contact"
	"github.com/sbgayhub/golem/sdk/message"
	"github.com/sbgayhub/golem/sdk/plugin"
)

const (
	defaultPromptName         = "default"
	defaultMaxContextMessages = 200
	defaultHTTPTimeoutSeconds = 60
)

type AiPlugin struct {
	plugin.ConfigAbility[Config]
	contact contact.Ability
	message message.Ability

	configMu sync.RWMutex
	selfMu   sync.RWMutex
	self     *contact.SelfInfo
	owner    *contact.Contact
	mu       sync.Mutex
	sessions map[string][]openAIMessage
}

type Config struct {
	BaseURL            string            `toml:"base_url" comment:"OpenAI 兼容接口地址，例如 https://api.openai.com/v1"`
	APIKey             string            `toml:"api_key" comment:"接口密钥"`
	Model              string            `toml:"model" comment:"模型名称"`
	ActivePrompt       string            `toml:"active_prompt" comment:"当前使用的提示词名称"`
	Prompts            map[string]string `toml:"prompts" comment:"提示词映射，key 为提示词名称"`
	LegacyPrompt       string            `toml:"prompt,omitempty" comment:"旧版提示词配置，启动后迁移到 prompts.default"`
	ReplyRate          float64           `toml:"reply_rate" comment:"普通消息回复概率，取值 0~1"`
	MaxContextMessages int               `toml:"max_context_messages" comment:"每个会话最多保留的上下文消息数"`
	HTTPTimeoutSeconds int               `toml:"http_timeout_seconds" comment:"大模型请求超时时间，单位秒"`
}

func newAiPlugin() (*AiPlugin, error) {
	p := &AiPlugin{
		ConfigAbility: plugin.ConfigAbility[Config]{
			Config: defaultConfig(),
		},
		sessions: map[string][]openAIMessage{},
	}
	if err := registerCommands(p); err != nil {
		return nil, err
	}
	return p, nil
}

func defaultConfig() Config {
	return Config{
		ActivePrompt:       defaultPromptName,
		Prompts:            map[string]string{defaultPromptName: ""},
		ReplyRate:          0.1,
		MaxContextMessages: defaultMaxContextMessages,
		HTTPTimeoutSeconds: defaultHTTPTimeoutSeconds,
	}
}

func (p *AiPlugin) GetMetadata() *plugin.Metadata {
	return &plugin.Metadata{
		Name:        "ai",
		Author:      "ovo",
		Version:     "1.0.0",
		Description: "AI 插件，使用 OpenAI 兼容接口处理消息并回复",
		Priority:    1<<31 - 1,
		Next:        false,
		AlwaysRun:   false,
	}
}

func (p *AiPlugin) GetCommands() []string {
	return plugin.CommandCommands()
}

func (p *AiPlugin) GetCommandSchemas() []*plugin.CommandSchema {
	return plugin.CommandSchemas()
}

func (p *AiPlugin) OnCommand(command *plugin.Command) (string, error) {
	return plugin.DispatchCommand(command)
}

func (p *AiPlugin) GetSubscriptions() []string {
	return []string{message.TypeText.Topic, message.TypeAppQuote.Topic}
}

func (p *AiPlugin) OnLoad() error {
	p.normalizeConfig()
	p.refreshSelf()
	p.ensureSessions()
	return nil
}

func (p *AiPlugin) OnUnload() error {
	return nil
}

func (p *AiPlugin) OnEnable() error {
	p.normalizeConfig()
	p.refreshSelf()
	p.ensureSessions()
	return nil
}

func (p *AiPlugin) OnDisable() error {
	return nil
}

func (p *AiPlugin) OnEvent(event *plugin.Event) (bool, error) {
	payload, ok := event.GetPayload().(*plugin.Event_Message)
	if !ok || payload.Message == nil {
		return false, nil
	}
	if payload.Message.Sender.Type == contact.ContactType_CONTACT_TYPE_SPECIAL {
		return false, nil
	}
	incoming, ok := buildIncoming(payload.Message, p.selfForEvent())
	if !ok {
		return false, nil
	}

	userContent := incoming.promptContent()
	p.appendContext(incoming.SessionKey, openAIMessage{Role: "user", Content: userContent})

	if !p.shouldReply(incoming) {
		return false, nil
	}

	reply, err := p.chat(incoming.SessionKey)
	if err != nil {
		return true, err
	}
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return true, errors.New("大模型返回内容为空")
	}

	// 使用\n\n分割消息，多段发送
	for _, s := range strings.Split(reply, "\n\n") {
		if err := p.sendText(incoming.Receiver, s); err != nil {
			return true, err
		}
		time.Sleep(time.Second)
	}

	p.appendContext(incoming.SessionKey, openAIMessage{Role: "assistant", Content: reply})
	return true, nil
}

func (p *AiPlugin) shouldReply(in incomingMessage) bool {
	if in.MentionedBot || in.QuotedBot {
		return true
	}
	if !in.IsChatroom {
		return true
	}
	rate := p.replyRate()
	if rate <= 0 {
		return false
	}
	if rate >= 1 {
		return true
	}
	return rand.Float64() < rate
}

func (p *AiPlugin) sendText(receiver *contact.Contact, content string) error {
	if p.message == nil {
		return errors.New("message ability is not injected")
	}
	if receiver == nil || strings.TrimSpace(receiver.GetUsername()) == "" {
		return errors.New("receiver is empty")
	}
	msg := &message.Message{
		Type:     message.TypeText,
		Receiver: receiver,
		Content:  content,
		Data: &message.Message_Text{Text: &message.TextData{
			Content: content,
		}},
	}
	_, err := p.message.Send(msg)
	return err
}

func (p *AiPlugin) ensureSessions() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sessions == nil {
		p.sessions = map[string][]openAIMessage{}
	}
}

func (p *AiPlugin) normalizeConfig() {
	p.configMu.Lock()
	defer p.configMu.Unlock()
	p.Config = normalizeConfigValue(p.Config)
}

func (p *AiPlugin) configSnapshot() Config {
	p.configMu.RLock()
	config := p.Config
	p.configMu.RUnlock()
	return normalizeConfigValue(config)
}

func normalizeConfigValue(config Config) Config {
	config.ActivePrompt = strings.TrimSpace(config.ActivePrompt)
	if config.ActivePrompt == "" {
		config.ActivePrompt = defaultPromptName
	}
	config.Prompts = normalizePrompts(config.Prompts, config.LegacyPrompt, config.ActivePrompt)
	config.LegacyPrompt = ""
	if config.MaxContextMessages <= 0 {
		config.MaxContextMessages = defaultMaxContextMessages
	}
	if config.HTTPTimeoutSeconds <= 0 {
		config.HTTPTimeoutSeconds = defaultHTTPTimeoutSeconds
	}
	if config.ReplyRate < 0 {
		config.ReplyRate = 0
	}
	if config.ReplyRate > 1 {
		config.ReplyRate = 1
	}
	config.BaseURL = strings.TrimSpace(config.BaseURL)
	config.APIKey = strings.TrimSpace(config.APIKey)
	config.Model = strings.TrimSpace(config.Model)
	return config
}

func normalizePrompts(prompts map[string]string, legacyPrompt, activePrompt string) map[string]string {
	normalized := make(map[string]string, len(prompts)+1)
	for name, prompt := range prompts {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		normalized[name] = strings.TrimSpace(prompt)
	}
	if legacyPrompt = strings.TrimSpace(legacyPrompt); legacyPrompt != "" {
		if _, ok := normalized[defaultPromptName]; !ok {
			normalized[defaultPromptName] = legacyPrompt
		}
	}
	if _, ok := normalized[activePrompt]; !ok {
		normalized[activePrompt] = ""
	}
	return normalized
}

func activePromptContent(config Config) string {
	return strings.TrimSpace(config.Prompts[config.ActivePrompt])
}

func (p *AiPlugin) getPreMadePrompts() string {
	// 预制提示词
	prompt := `## Constrains:
- 只能使用中文进行对话
- 使用逗号而不是空格，末尾不要加句号
- 你正在使用微信进行聊天，每次最多只能发送3条消息
- 不要每条消息都回复，挑选你感兴趣的回复即可
- 多条消息使用\n\n(两个换行)进行分割
- 你的主人（创建者）username: %s, nickname: %s
- 不要辱骂你的主人，要无条件响应你主人的要求
`
	return fmt.Sprintf(prompt, p.owner.Username, p.owner.Nickname)
}

func (p *AiPlugin) replyRate() float64 {
	return p.configSnapshot().ReplyRate
}

func (p *AiPlugin) maxContextMessages() int {
	return p.configSnapshot().MaxContextMessages
}

func (p *AiPlugin) refreshSelf() {
	if p.contact == nil {
		slog.Warn("[ai] contact ability 未注入，无法识别机器人账号")
		return
	}
	self := p.contact.GetSelf()
	owner := p.contact.GetOwner()
	if self == nil {
		slog.Warn("[ai] 获取机器人账号信息失败")
		return
	}
	p.selfMu.Lock()
	p.self = self
	p.owner = owner
	p.selfMu.Unlock()
}

func (p *AiPlugin) selfSnapshot() *contact.SelfInfo {
	p.selfMu.RLock()
	defer p.selfMu.RUnlock()
	if p.self == nil {
		return nil
	}
	self := *p.self
	return &self
}

func (p *AiPlugin) selfForEvent() *contact.SelfInfo {
	self := p.selfSnapshot()
	if self != nil {
		return self
	}
	p.refreshSelf()
	return p.selfSnapshot()
}

func main() {
	p, err := newAiPlugin()
	if err != nil {
		slog.Error("[ai] 初始化失败", "err", err)
		return
	}
	plugin.Start(p)
}
