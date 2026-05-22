package plugin

import (
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/sbgayhub/golem/sdk/plugin"
)

var (
	mu           sync.Mutex
	plugins      []*wrapper          // 插件集合
	commandIndex map[string]*wrapper // 命令索引
)

// 插件包装
type wrapper struct {
	*plugin.Metadata          // 插件元数据
	*Config                   // 插件配置
	abilities        []string // 插件使用的能力集合
	subscriptions    []string // 插件订阅的事件主题集合
	capabilities     []string // 插件提供的能力集合
	commands         []string // 插件提供的命令集合
	commandSchemas   []*plugin.CommandSchema
	types            []string // 插件类型

	plugin        *plugin.Plugin        // 插件
	eventPlugin   *plugin.EventPlugin   // 事件监听插件
	calledPlugin  *plugin.CalledPlugin  // 方法调用插件
	commandPlugin *plugin.CommandPlugin // 命令执行插件
}

func LoadPlugins() error {
	mu.Lock()
	defer mu.Unlock()

	commandIndex = map[string]*wrapper{}
	LoadPlugin("")

	return nil
}

func LoadPlugin(name string) error {

	//// 获取插件目录下的所有可执行文件
	//paths, err := goplugin.Discover("*.exe", "../plugins")
	//if err != nil {
	//	return err
	//}
	//
	//// 便利可执行文件路径，加载插件
	//for _, path := range paths {
	//	metadata, p, err := plugin.Get(path)
	//	if err != nil {
	//		return err
	//	}
	//	// 注入能力
	//	if ability, ok := (*p).(plugin.Ability); ok {
	//		if err := ability.InjectAbilities(ability.GetAbilities()); err != nil {
	//			return err
	//		}
	//	}
	//	// 添加插件
	//	m.plugins = append(m.plugins, wrapper{
	//		Metadata:       metadata,
	//		plugin:         p,
	//		subscripptions: (*p).(plugin.EventPlugin).GetSubscriptions(),
	//		//capability:     (*p).(plugin.CalledPlugin).GetCapabilities(),
	//		//commands:       (*p).(plugin.CommandPlugin).GetCommands(),
	//	})
	//
	//	slog.Debug("插件加载成功", "name", metadata.Name, "priority", metadata.Priority, "version", metadata.Version)
	//}

	metadata, p, err := plugin.Get("D:\\Project-Go\\new_golem\\plugins\\example\\golem_plugin_example.exe")
	if err != nil {
		return err
	}

	if ability, ok := (*p).(plugin.Ability); ok {
		ability.InjectAbilities(ability.GetAbilities())
	}

	// 关联配置
	cfg, ok := configs[metadata.Name]
	if !ok {
		cfg = &Config{Enable: true, Mode: "blacklist"}
		configs[metadata.Name] = cfg
	}

	// 配置注入
	if pc, ok := (*p).(IPluginConfig); ok {
		if cfg.Config == nil {
			if data, err := pc.GetDefaultConfig(); err != nil {
				slog.Warn("获取插件默认配置失败", "name", metadata.Name, "err", err)
			} else if len(data) > 0 {
				var m any
				if err := json.Unmarshal(data, &m); err == nil {
					cfg.Config = m
					_ = saveConfig()
				}
			}
		} else {
			data, err := json.Marshal(cfg.Config)
			if err != nil {
				slog.Warn("序列化插件配置失败", "name", metadata.Name, "err", err)
			} else if err := pc.SetConfig(data); err != nil {
				slog.Warn("注入插件配置失败", "name", metadata.Name, "err", err)
			}
		}
	}

	// 如果 config 中有值，覆盖 metadata
	if cfg.Priority != nil {
		metadata.Priority = *cfg.Priority
	}
	if cfg.Next != nil {
		metadata.Next = *cfg.Next
	}

	w := wrapper{
		Metadata: metadata,
		Config:   cfg,
		types:    []string{},
		plugin:   p,
	}
	if ep, ok := (*p).(plugin.EventPlugin); ok && ep.GetSubscriptions() != nil {
		w.subscriptions = ep.GetSubscriptions()
		w.eventPlugin = &ep
		w.types = append(w.types, "event")
	}
	if cp, ok := (*p).(plugin.CalledPlugin); ok && cp.GetCapabilities() != nil {
		w.capabilities = cp.GetCapabilities()
		w.calledPlugin = &cp
		w.types = append(w.types, "called")
	}
	if cp, ok := (*p).(plugin.CommandPlugin); ok && cp.GetCommands() != nil {
		w.commands = cp.GetCommands()
		w.commandPlugin = &cp
		w.types = append(w.types, "command")
		if sp, ok := (*p).(plugin.CommandSchemaProvider); ok {
			w.commandSchemas = sp.GetCommandSchemas()
		}
	}
	if ab, ok := (*p).(plugin.Ability); ok {
		w.abilities = ab.GetAbilities()
	}

	plugins = append(plugins, &w)
	registerCommandIndex(&w)

	slog.Info("插件加载成功", "name", metadata.Name, "priority", metadata.Priority, "version", metadata.Version)
	return nil
}

func registerCommandIndex(w *wrapper) {
	for _, command := range w.commands {
		if exist := commandIndex[command]; exist != nil {
			if exist.Metadata.Priority >= w.Metadata.Priority {
				slog.Warn("命令已被其他插件注册，忽略当前插件", "command", command, "current", w.Name, "exist", exist.Name)
				continue
			}
			slog.Warn("命令注册被更高优先级插件覆盖", "command", command, "current", w.Name, "exist", exist.Name)
		}
		commandIndex[command] = w
	}
}
