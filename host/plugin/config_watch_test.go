package plugin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"
	sdk "github.com/sbgayhub/golem/sdk/plugin"
)

type configWatchTestPlugin struct {
	metadata *sdk.Metadata
	calls    int
	data     []byte
}

func (p *configWatchTestPlugin) GetMetadata() *sdk.Metadata {
	return p.metadata
}

func (p *configWatchTestPlugin) GetDefaultConfig() ([]byte, error) {
	return nil, nil
}

func (p *configWatchTestPlugin) SetConfig(data []byte) error {
	p.calls++
	p.data = append(p.data[:0], data...)
	return nil
}

func TestReloadPluginConfigFromFileReplacesMemory(t *testing.T) {
	withConfigWatchState(t)

	oldCfg := &Config{Enable: true, Mode: "blacklist"}
	configs = map[string]*Config{"example": oldCfg}

	metadata := &sdk.Metadata{Name: "example", Priority: 100}
	impl := &configWatchTestPlugin{metadata: metadata}
	var p sdk.Plugin = impl
	plugins = []*wrapper{{
		Metadata: metadata,
		Config:   oldCfg,
		plugin:   &p,
	}}

	data := []byte(`
[example]
enable = false
priority = 7
next = true
always_run = true
mode = "whitelist"
limits = ["room@chatroom"]

[example.config]
name = "changed"
`)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	if err := reloadPluginConfigFromFile(); err != nil {
		t.Fatal(err)
	}

	if configs["example"] == oldCfg {
		t.Fatal("config should be replaced")
	}
	if plugins[0].Config != configs["example"] {
		t.Fatal("wrapper should reference reloaded config")
	}
	if plugins[0].Config.Enable {
		t.Fatal("enable should be reloaded")
	}
	if plugins[0].Config.Mode != "whitelist" {
		t.Fatalf("mode = %q, want whitelist", plugins[0].Config.Mode)
	}
	if plugins[0].Metadata.Priority != 7 {
		t.Fatalf("priority = %d, want 7", plugins[0].Metadata.Priority)
	}
	if !plugins[0].Metadata.Next {
		t.Fatal("next should be reloaded")
	}
	if !plugins[0].Metadata.AlwaysRun {
		t.Fatal("always_run should be reloaded")
	}
	if impl.calls != 1 {
		t.Fatalf("SetConfig calls = %d, want 1", impl.calls)
	}

	var pluginCfg map[string]any
	if err := toml.Unmarshal(impl.data, &pluginCfg); err != nil {
		t.Fatal(err)
	}
	if pluginCfg["name"] != "changed" {
		t.Fatalf("plugin config name = %v, want changed", pluginCfg["name"])
	}
}

func TestReloadPluginConfigFromFileKeepsMemoryOnInvalidTOML(t *testing.T) {
	withConfigWatchState(t)

	oldCfg := &Config{Enable: true, Mode: "blacklist"}
	configs = map[string]*Config{"example": oldCfg}

	metadata := &sdk.Metadata{Name: "example", Priority: 100}
	impl := &configWatchTestPlugin{metadata: metadata}
	var p sdk.Plugin = impl
	plugins = []*wrapper{{
		Metadata: metadata,
		Config:   oldCfg,
		plugin:   &p,
	}}

	if err := os.WriteFile(configPath, []byte("[example\ninvalid"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := reloadPluginConfigFromFile(); err == nil {
		t.Fatal("expected invalid TOML error")
	}
	if configs["example"] != oldCfg {
		t.Fatal("invalid TOML should keep old config map entry")
	}
	if plugins[0].Config != oldCfg {
		t.Fatal("invalid TOML should keep wrapper config")
	}
	if impl.calls != 0 {
		t.Fatalf("SetConfig calls = %d, want 0", impl.calls)
	}
}

func withConfigWatchState(t *testing.T) {
	t.Helper()

	oldPlugins := plugins
	oldCommandIndex := commandIndex
	oldCapabilityIndex := capabilityIndex
	oldConfigs := configs
	oldPluginDir := pluginDir
	oldConfigPath := configPath
	oldConfigWatcher := configWatcher

	pluginDir = t.TempDir()
	configPath = filepath.Join(pluginDir, "config.toml")
	configs = map[string]*Config{}
	plugins = nil
	commandIndex = map[string]*wrapper{}
	capabilityIndex = map[string]*wrapper{}
	configWatcher = nil

	t.Cleanup(func() {
		if configWatcher != nil {
			_ = configWatcher.Close()
		}
		plugins = oldPlugins
		commandIndex = oldCommandIndex
		capabilityIndex = oldCapabilityIndex
		configs = oldConfigs
		pluginDir = oldPluginDir
		configPath = oldConfigPath
		configWatcher = oldConfigWatcher
	})
}
