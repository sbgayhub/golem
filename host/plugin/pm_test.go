package plugin

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/sbgayhub/golem/sdk/contact"
	sdk "github.com/sbgayhub/golem/sdk/plugin"
)

func TestPMEnableDisableUpdatesGlobalEnableOutsideChatroom(t *testing.T) {
	withPMState(t)
	pm := &pmCommandPlugin{}
	plugins = []*wrapper{{
		Metadata: &sdk.Metadata{Name: "example"},
		Config:   &Config{Enable: false, Mode: "blacklist"},
	}}
	configs["example"] = plugins[0].Config

	if _, err := pm.enable(pmEnableCommand{Name: "example"}); err != nil {
		t.Fatalf("enable plugin: %v", err)
	}
	if !plugins[0].Config.Enable {
		t.Fatalf("plugin should be globally enabled")
	}

	if _, err := pm.disable(pmDisableCommand{Name: "example"}); err != nil {
		t.Fatalf("disable plugin: %v", err)
	}
	if plugins[0].Config.Enable {
		t.Fatalf("plugin should be globally disabled")
	}
}

func TestPMEnableDisableUpdatesBlacklistLimitsInChatroom(t *testing.T) {
	withPMState(t)
	pm := &pmCommandPlugin{}
	plugins = []*wrapper{{
		Metadata: &sdk.Metadata{Name: "example"},
		Config: &Config{
			Enable: true,
			Mode:   "blacklist",
			Limits: []string{"room@chatroom"},
		},
	}}
	configs["example"] = plugins[0].Config
	cmd := chatroomPMCommand("room@chatroom")

	if _, err := pm.enable(pmEnableCommand{Name: "example", Command: cmd}); err != nil {
		t.Fatalf("enable plugin in chatroom: %v", err)
	}
	if slices.Contains(plugins[0].Config.Limits, "room@chatroom") {
		t.Fatalf("chatroom should be removed from blacklist limits: %+v", plugins[0].Config.Limits)
	}

	if _, err := pm.disable(pmDisableCommand{Name: "example", Command: cmd}); err != nil {
		t.Fatalf("disable plugin in chatroom: %v", err)
	}
	if !slices.Contains(plugins[0].Config.Limits, "room@chatroom") {
		t.Fatalf("chatroom should be added to blacklist limits: %+v", plugins[0].Config.Limits)
	}
}

func TestPMEnableDisableUpdatesWhitelistLimitsInChatroom(t *testing.T) {
	withPMState(t)
	pm := &pmCommandPlugin{}
	plugins = []*wrapper{{
		Metadata: &sdk.Metadata{Name: "example"},
		Config:   &Config{Enable: true, Mode: "whitelist"},
	}}
	configs["example"] = plugins[0].Config
	cmd := chatroomPMCommand("room@chatroom")

	if _, err := pm.enable(pmEnableCommand{Name: "example", Command: cmd}); err != nil {
		t.Fatalf("enable plugin in chatroom: %v", err)
	}
	if !slices.Contains(plugins[0].Config.Limits, "room@chatroom") {
		t.Fatalf("chatroom should be added to whitelist limits: %+v", plugins[0].Config.Limits)
	}

	if _, err := pm.disable(pmDisableCommand{Name: "example", Command: cmd}); err != nil {
		t.Fatalf("disable plugin in chatroom: %v", err)
	}
	if slices.Contains(plugins[0].Config.Limits, "room@chatroom") {
		t.Fatalf("chatroom should be removed from whitelist limits: %+v", plugins[0].Config.Limits)
	}
}

func TestPMEnableDisableNormalizesEmptyModeToBlacklistInChatroom(t *testing.T) {
	withPMState(t)
	pm := &pmCommandPlugin{}
	plugins = []*wrapper{{
		Metadata: &sdk.Metadata{Name: "example"},
		Config:   &Config{Enable: true},
	}}
	configs["example"] = plugins[0].Config

	if _, err := pm.disable(pmDisableCommand{Name: "example", Command: chatroomPMCommand("room@chatroom")}); err != nil {
		t.Fatalf("disable plugin in chatroom: %v", err)
	}
	if plugins[0].Config.Mode != "blacklist" {
		t.Fatalf("empty mode should be normalized to blacklist, got %q", plugins[0].Config.Mode)
	}
	if !slices.Contains(plugins[0].Config.Limits, "room@chatroom") {
		t.Fatalf("chatroom should be added to blacklist limits: %+v", plugins[0].Config.Limits)
	}
}

func chatroomPMCommand(username string) *sdk.Command {
	return &sdk.Command{
		Sender: &contact.Contact{
			Username: username,
			Type:     contact.ContactType_CONTACT_TYPE_CHATROOM,
		},
	}
}

func withPMState(t *testing.T) {
	t.Helper()

	oldPlugins := plugins
	oldCommandIndex := commandIndex
	oldCapabilityIndex := capabilityIndex
	oldConfigs := configs
	oldPluginDir := pluginDir
	oldConfigPath := configPath

	pluginDir = t.TempDir()
	configPath = filepath.Join(pluginDir, "config.toml")
	configs = map[string]*Config{}
	plugins = nil
	commandIndex = map[string]*wrapper{}
	capabilityIndex = map[string]*wrapper{}

	t.Cleanup(func() {
		plugins = oldPlugins
		commandIndex = oldCommandIndex
		capabilityIndex = oldCapabilityIndex
		configs = oldConfigs
		pluginDir = oldPluginDir
		configPath = oldConfigPath
	})
}
