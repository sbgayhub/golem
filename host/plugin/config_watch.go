package plugin

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/pelletier/go-toml/v2"
	sdk "github.com/sbgayhub/golem/sdk/plugin"
	"google.golang.org/protobuf/proto"
)

const pluginConfigReloadDelay = 300 * time.Millisecond

var configWatcher *fsnotify.Watcher

type configInjection struct {
	name string
	p    *sdk.Plugin
	cfg  *Config
}

func startConfigWatcher() error {
	if configWatcher != nil {
		return nil
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("创建插件配置监听器失败: %w", err)
	}
	if err := watcher.Add(filepath.Dir(configPath)); err != nil {
		_ = watcher.Close()
		return fmt.Errorf("监听插件配置目录失败: %w", err)
	}

	configWatcher = watcher
	go watchConfigFile(watcher)
	return nil
}

func stopConfigWatcher() {
	if configWatcher == nil {
		return
	}
	if err := configWatcher.Close(); err != nil {
		slog.Warn("关闭插件配置监听器失败", "err", err)
	}
	configWatcher = nil
}

func watchConfigFile(watcher *fsnotify.Watcher) {
	timer := time.NewTimer(pluginConfigReloadDelay)
	stopTimer(timer)
	pending := false

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				stopTimer(timer)
				return
			}
			if !isPluginConfigEvent(event) {
				continue
			}
			pending = true
			resetTimer(timer, pluginConfigReloadDelay)

		case err, ok := <-watcher.Errors:
			if !ok {
				stopTimer(timer)
				return
			}
			if err != nil {
				slog.Warn("监听插件配置文件失败", "path", configPath, "err", err)
			}

		case <-timer.C:
			if !pending {
				continue
			}
			pending = false
			if err := reloadPluginConfigFromFile(); err != nil {
				slog.Warn("插件配置文件重载失败", "path", configPath, "err", err)
			}
		}
	}
}

func isPluginConfigEvent(event fsnotify.Event) bool {
	if filepath.Clean(event.Name) != filepath.Clean(configPath) {
		return false
	}
	return event.Has(fsnotify.Write) ||
		event.Has(fsnotify.Create) ||
		event.Has(fsnotify.Rename) ||
		event.Has(fsnotify.Remove)
}

func reloadPluginConfigFromFile() error {
	next, err := readPluginConfigFile()
	if err != nil {
		return err
	}

	applyPluginConfigs(next)
	slog.Info("插件配置文件已重载", "path", configPath)
	return nil
}

func applyPluginConfigs(next map[string]*Config) {
	injections := replacePluginConfigs(next)
	for _, item := range injections {
		if err := injectRuntimePluginConfig(item.p, item.cfg); err != nil {
			slog.Warn("插件配置注入失败", "plugin", item.name, "err", err)
		}
	}
}

func replacePluginConfigs(next map[string]*Config) []configInjection {
	mu.Lock()
	defer mu.Unlock()

	configs = next
	injections := make([]configInjection, 0, len(plugins))

	for i, w := range plugins {
		if slices.Contains(w.types, "builtin") {
			continue
		}

		cfg := configs[w.Name]
		if cfg == nil {
			cfg = &Config{Enable: true, Mode: "blacklist"}
			configs[w.Name] = cfg
		}

		metadata := proto.CloneOf(w.Metadata)
		w.Metadata.ProtoMessage()
		applyMetadataConfig(metadata, cfg)

		updated := *w
		updated.Metadata = metadata
		updated.Config = cfg
		plugins[i] = &updated

		if cfg.Config != nil {
			injections = append(injections, configInjection{
				name: w.Name,
				p:    w.plugin,
				cfg:  cfg,
			})
		}
	}

	sortPlugins()
	rebuildCommandIndex()
	rebuildCapabilityIndex()
	return injections
}

func injectRuntimePluginConfig(p *sdk.Plugin, cfg *Config) error {
	pc, ok := (*p).(IPluginConfig)
	if !ok {
		return nil
	}

	data, err := toml.Marshal(cfg.Config)
	if err != nil {
		return fmt.Errorf("序列化插件配置失败: %w", err)
	}
	if err := pc.SetConfig(data); err != nil {
		return fmt.Errorf("注入插件配置失败: %w", err)
	}
	return nil
}

func resetTimer(timer *time.Timer, delay time.Duration) {
	stopTimer(timer)
	timer.Reset(delay)
}

func stopTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}
