package plugin

import (
	"time"
)

// Plugin 插件基础接口
type Plugin interface {
	GetMetadata() *Metadata // 获取插件元数据
}

// Lifecycle 生命周期接口
type Lifecycle interface {
	OnLoad() error    // 插件加载时调用
	OnUnload() error  // 插件卸载时调用
	OnEnable() error  // 插件启用时调用
	OnDisable() error // 插件禁用时调用
}

// SessionAbility 会话劫持能力
type SessionAbility interface {
	Hold(p Plugin, id string, duration time.Duration) // 劫持会话
	Release(id string)                                // 释放会话
}

// CallerAbility 插件调用能力（宿主侧 wrapper 实现）
type CallerAbility interface {
	CallPlugin(capability string, args map[string]string) (string, []byte, error)
}

// EventPlugin 事件插件接口
type EventPlugin interface {
	GetSubscriptions() []string         // 获取订阅的事件主题
	OnEvent(event *Event) (bool, error) // 事件触发时调用
}

// CalledPlugin 调用插件接口
type CalledPlugin interface {
	GetCapabilities() []string                                                // 获取插件能力
	OnCall(capability string, args map[string]string) (string, []byte, error) // 调用插件能力时调用
}

// CommandSchemaProvider 命令 schema 提供接口
type CommandSchemaProvider interface {
	GetCommandSchemas() []*CommandSchema // 获取命令 schema
}

// CommandPlugin 命令插件接口
type CommandPlugin interface {
	GetCommands() []string                      // 获取命令列表
	OnCommand(command *Command) (string, error) // 命令执行时调用
}

// Ability 插件能力接口，插件实现禁止使用
type Ability interface {
	GetAbilities() []string                   // 获取插件声明的能力列表
	InjectAbilities(abilities []string) error // 向插件注入其所需的能力列表
}
