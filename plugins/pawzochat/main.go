package main

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/sbgayhub/golem/sdk/contact"
	"github.com/sbgayhub/golem/sdk/message"
	"github.com/sbgayhub/golem/sdk/plugin"
)

const defaultHTTPTimeoutSeconds = 50

type Config struct {
	BaseURL                   string            `toml:"base_url" comment:"PawzoChat 地址，例如 http://127.0.0.1:62000"`
	Token                     string            `toml:"token" comment:"与 PawzoChat bridge.golem_token 一致的 Bearer Token"`
	DefaultPersonaID          string            `toml:"default_persona_id,omitempty" comment:"未命中 routes 时使用的角色；留空可避免跨会话共用上下文"`
	Routes                    map[string]string `toml:"routes" comment:"golem 会话标识到 PawzoChat persona_id 的映射"`
	RespondToAllGroupMessages bool              `toml:"respond_to_all_group_messages" comment:"群聊中是否回复所有消息；关闭时仅回复 @ 或引用"`
	HTTPTimeoutSeconds        int               `toml:"http_timeout_seconds" comment:"等待 PawzoChat 回复的超时秒数，建议小于 60"`
}

type PawzoChatPlugin struct {
	plugin.ConfigAbility[Config]
	contact contact.Ability
	message message.Ability

	configMu          sync.RWMutex
	identityMu        sync.RWMutex
	identityRefresh   sync.Mutex
	self              *contact.SelfInfo
	ownerID           string
	ownerName         string
	identityUpdatedAt time.Time
}

func defaultConfig() Config {
	return Config{
		BaseURL:            "http://127.0.0.1:62000",
		Routes:             map[string]string{},
		HTTPTimeoutSeconds: defaultHTTPTimeoutSeconds,
	}
}

func newPawzoChatPlugin() *PawzoChatPlugin {
	return &PawzoChatPlugin{
		ConfigAbility: plugin.ConfigAbility[Config]{Config: defaultConfig()},
	}
}

func (p *PawzoChatPlugin) normalizeConfig() {
	p.configMu.Lock()
	defer p.configMu.Unlock()
	p.Config = normalizeConfigValue(p.Config)
}

func normalizeConfigValue(config Config) Config {
	config.BaseURL = strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if config.BaseURL == "" {
		config.BaseURL = defaultConfig().BaseURL
	}
	config.Token = strings.TrimSpace(config.Token)
	config.DefaultPersonaID = strings.TrimSpace(config.DefaultPersonaID)
	if config.HTTPTimeoutSeconds <= 0 {
		config.HTTPTimeoutSeconds = defaultHTTPTimeoutSeconds
	}
	routes := make(map[string]string, len(config.Routes))
	for session, personaID := range config.Routes {
		session = strings.TrimSpace(session)
		personaID = strings.TrimSpace(personaID)
		if session != "" && personaID != "" {
			routes[session] = personaID
		}
	}
	config.Routes = routes
	return config
}

func (p *PawzoChatPlugin) configSnapshot() Config {
	p.configMu.RLock()
	defer p.configMu.RUnlock()
	config := p.Config
	config.Routes = make(map[string]string, len(p.Config.Routes))
	for key, value := range p.Config.Routes {
		config.Routes[key] = value
	}
	return config
}

func (p *PawzoChatPlugin) personaForSession(config Config, sessionKey string) string {
	if personaID := strings.TrimSpace(config.Routes[sessionKey]); personaID != "" {
		return personaID
	}
	return strings.TrimSpace(config.DefaultPersonaID)
}

func main() {
	plugin.Start(newPawzoChatPlugin())
	slog.Info("[pawzochat] plugin stopped")
}
