package plugin

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/pelletier/go-toml/v2"
	"github.com/sbgayhub/golem/sdk/plugin"
)

const pluginExecutablePrefix = "golem_plugin_"

var (
	mu              sync.Mutex
	plugins         []*wrapper
	commandIndex    = map[string]*wrapper{} // 命令索引
	capabilityIndex = map[string]*wrapper{} // 能力索引
)

type pluginExecutableFile struct {
	name string
	path string
}

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
	executables, err := discoverPluginExecutables()
	if err != nil {
		return err
	}

	for _, executable := range executables {
		if err := loadPluginFromPath(executable.name, executable.path); err != nil {
			return err
		}
	}
	return nil
}

func LoadPlugin(name string) error {
	if name == "" {
		return fmt.Errorf("插件名称不能为空")
	}
	if findPlugin(name) != nil {
		return fmt.Errorf("插件已加载：%s", name)
	}

	executable, err := findPluginExecutable(name)
	if err != nil {
		return err
	}
	return loadPluginFromPath(name, executable.path)
}

func loadPluginFromPath(expectedName, path string) error {
	if expectedName == "" {
		return fmt.Errorf("插件名称不能为空")
	}
	if findPlugin(expectedName) != nil {
		return fmt.Errorf("插件已加载：%s", expectedName)
	}

	metadata, p, err := plugin.Get(path)
	if err != nil {
		return err
	}
	if metadata.Name != expectedName {
		slog.Warn("插件文件名和元数据名称不一致", "expected", expectedName, "actual", metadata.Name)
	}
	if findPlugin(metadata.Name) != nil {
		plugin.Kill(metadata.Name)
		return fmt.Errorf("插件已加载：%s", metadata.Name)
	}

	if ability, ok := (*p).(plugin.Ability); ok {
		if err := ability.InjectAbilities(ability.GetAbilities()); err != nil {
			plugin.Kill(metadata.Name)
			return err
		}
	}

	cfg := configs[metadata.Name]
	if cfg == nil {
		cfg = &Config{Enable: true, Mode: "blacklist"}
		configs[metadata.Name] = cfg
	}
	if err := injectPluginConfig(metadata.Name, p, cfg); err != nil {
		slog.Warn("应用插件配置失败", "name", metadata.Name, "err", err)
	}
	applyMetadataConfig(metadata, cfg)

	w := newWrapper(metadata, cfg, p)
	if lifecycle, ok := (*p).(plugin.Lifecycle); ok {
		if err := lifecycle.OnLoad(); err != nil {
			plugin.Kill(metadata.Name)
			return err
		}
	}

	mu.Lock()
	plugins = append(plugins, w)
	sortPlugins()
	rebuildCommandIndex()
	rebuildCapabilityIndex()
	mu.Unlock()

	slog.Info("插件加载成功", "name", metadata.Name, "priority", metadata.Priority, "version", metadata.Version)
	return nil
}

func UnloadPlugin(name string) error {
	mu.Lock()
	w, index := findPluginWithIndex(name)
	if w == nil {
		mu.Unlock()
		return fmt.Errorf("插件不存在：%s", name)
	}
	if slices.Contains(w.types, "builtin") {
		mu.Unlock()
		return fmt.Errorf("内置插件禁止卸载：%s", name)
	}
	plugins = slices.Delete(plugins, index, index+1)
	rebuildCommandIndex()
	rebuildCapabilityIndex()
	mu.Unlock()

	if lifecycle, ok := (*w.plugin).(plugin.Lifecycle); ok {
		if err := lifecycle.OnUnload(); err != nil {
			return err
		}
	}
	plugin.Kill(w.Name)
	slog.Info("插件卸载成功", "name", w.Name)
	return nil
}

func ReloadPlugin(name string) error {
	if err := UnloadPlugin(name); err != nil {
		return err
	}
	return LoadPlugin(name)
}

func discoverPluginExecutables() ([]pluginExecutableFile, error) {
	if _, err := os.Stat(pluginDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取插件目录失败: %w", err)
	}

	var executables []pluginExecutableFile
	err := filepath.WalkDir(pluginDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("遍历插件目录失败: %w", err)
		}
		if entry.IsDir() {
			return nil
		}

		executable, err := pluginExecutableFromDirEntry(path, entry)
		if err != nil {
			return err
		}
		if executable == nil {
			return nil
		}

		executables = append(executables, *executable)
		return nil
	})
	if err != nil {
		return nil, err
	}
	
	sort.Slice(executables, func(i, j int) bool {
		return executables[i].path < executables[j].path
	})
	return executables, nil
}

func findPluginExecutable(name string) (pluginExecutableFile, error) {
	executables, err := discoverPluginExecutables()
	if err != nil {
		return pluginExecutableFile{}, err
	}
	for _, executable := range executables {
		if executable.name == name {
			return executable, nil
		}
	}
	return pluginExecutableFile{}, fmt.Errorf("插件文件不存在：%s", name)
}

func pluginExecutableFromDirEntry(path string, entry fs.DirEntry) (*pluginExecutableFile, error) {
	fileName := entry.Name()
	if !strings.HasPrefix(fileName, pluginExecutablePrefix) {
		return nil, nil
	}

	info, err := entry.Info()
	if err != nil {
		return nil, fmt.Errorf("检查插件文件失败: %w", err)
	}
	if !isPluginExecutable(info) {
		return nil, nil
	}

	name := pluginNameFromExecutable(fileName)
	if name == "" {
		return nil, nil
	}
	return &pluginExecutableFile{name: name, path: path}, nil
}

func isPluginExecutable(info fs.FileInfo) bool {
	if !info.Mode().IsRegular() {
		return false
	}
	if info.Mode().Perm()&0111 != 0 {
		return true
	}
	if runtime.GOOS != "windows" {
		return false
	}
	return hasExecutableExtension(info.Name())
}

func pluginNameFromExecutable(fileName string) string {
	name := strings.TrimPrefix(fileName, pluginExecutablePrefix)
	if ext := filepath.Ext(name); hasExecutableExtension(ext) {
		name = strings.TrimSuffix(name, ext)
	}
	return name
}

func hasExecutableExtension(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		ext = strings.ToLower(name)
	}
	switch ext {
	case ".exe", ".com", ".bat", ".cmd":
		return true
	}

	for _, candidate := range strings.Split(os.Getenv("PATHEXT"), ";") {
		if strings.EqualFold(strings.TrimSpace(candidate), ext) {
			return true
		}
	}
	return false
}

func injectPluginConfig(name string, p *plugin.Plugin, cfg *Config) error {
	pc, ok := (*p).(IPluginConfig)
	if !ok {
		return nil
	}

	if cfg.Config == nil {
		data, err := pc.GetDefaultConfig()
		if err != nil {
			return fmt.Errorf("获取插件默认配置失败: %w", err)
		}
		if len(data) == 0 {
			return nil
		}
		var value map[string]any
		if err := toml.Unmarshal(data, &value); err != nil {
			return fmt.Errorf("解析插件默认配置失败: %w", err)
		}
		cfg.Config = value
		return saveConfig()
	}

	data, err := toml.Marshal(cfg.Config)
	if err != nil {
		return fmt.Errorf("序列化插件配置失败: %w", err)
	}
	if err := pc.SetConfig(data); err != nil {
		return fmt.Errorf("注入插件配置失败: %w", err)
	}
	slog.Debug("插件配置已注入", "name", name)
	return nil
}

func applyMetadataConfig(metadata *plugin.Metadata, cfg *Config) {
	if cfg.Priority != nil {
		metadata.Priority = *cfg.Priority
	}
	if cfg.Next != nil {
		metadata.Next = *cfg.Next
	}
	if cfg.AlwaysRun != nil {
		metadata.AlwaysRun = *cfg.AlwaysRun
	}
}

func newWrapper(metadata *plugin.Metadata, cfg *Config, p *plugin.Plugin) *wrapper {
	w := &wrapper{
		Metadata: metadata,
		Config:   cfg,
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
	slog.Debug("插件wrapper创建完成", "name", w.Name, "types", w.types)
	return w
}

func findPlugin(name string) *wrapper {
	w, _ := findPluginWithIndex(name)
	return w
}

func findPluginWithIndex(name string) (*wrapper, int) {
	for i, w := range plugins {
		if w.Name == name {
			return w, i
		}
	}
	return nil, -1
}

func pluginSnapshot() []*wrapper {
	mu.Lock()
	defer mu.Unlock()

	values := make([]*wrapper, len(plugins))
	copy(values, plugins)
	return values
}

func sortPlugins() {
	sort.SliceStable(plugins, func(i, j int) bool {
		return plugins[i].Metadata.Priority < plugins[j].Metadata.Priority
	})
}

func rebuildCommandIndex() {
	commandIndex = map[string]*wrapper{}
	for _, w := range plugins {
		for _, command := range w.commands {
			if exist := commandIndex[command]; exist != nil {
				if slices.Contains(exist.types, "builtin") {
					slog.Warn("命令已被内置插件注册，忽略当前插件", "command", command, "current", w.Name, "exist", exist.Name)
					continue
				}
				if exist.Metadata.Priority <= w.Metadata.Priority {
					slog.Warn("命令已被更高优先级插件注册，忽略当前插件", "command", command, "current", w.Name, "exist", exist.Name)
					continue
				}
				slog.Warn("命令注册被更高优先级插件覆盖", "command", command, "current", w.Name, "exist", exist.Name)
			}
			commandIndex[command] = w
		}
	}
}

func rebuildCapabilityIndex() {
	capabilityIndex = map[string]*wrapper{}
	for _, w := range plugins {
		if w.Config != nil && !w.Config.Enable {
			continue
		}
		if w.calledPlugin == nil {
			continue
		}
		for _, capability := range w.capabilities {
			if _, exist := capabilityIndex[capability]; exist {
				continue
			}
			capabilityIndex[capability] = w
		}
	}
}
