package plugin

import (
	"context"
	"fmt"
	"testing"

	sdk "github.com/sbgayhub/golem/sdk/plugin"
)

type testCalledPlugin struct {
	name string
}

func (p testCalledPlugin) GetCapabilities() []string {
	return []string{"test.echo"}
}

func (p testCalledPlugin) OnCall(capability string, args map[string]string) (string, []byte, error) {
	return "text", []byte(fmt.Sprintf("%s:%s:%s", p.name, capability, args["value"])), nil
}

func TestCallPluginReturnsReadableMessageWhenCapabilityMissing(t *testing.T) {
	withPluginState(t)

	resp, err := (&hostService{}).CallPlugin(context.Background(), &sdk.CallPlugin_Request{Capability: "missing.capability"})
	if err != nil {
		t.Fatalf("call plugin: %v", err)
	}
	if got := resp.GetMessage(); got != "未找到可用能力： missing.capability" {
		t.Fatalf("unexpected response: %q", got)
	}
}

func TestCallPluginUsesHighestPriorityCapabilityProvider(t *testing.T) {
	withPluginState(t)

	low := sdk.CalledPlugin(testCalledPlugin{name: "low"})
	high := sdk.CalledPlugin(testCalledPlugin{name: "high"})
	plugins = []*wrapper{
		{
			Metadata:     &sdk.Metadata{Name: "low", Priority: 10},
			Config:       &Config{Enable: true},
			capabilities: []string{"test.echo"},
			calledPlugin: &low,
		},
		{
			Metadata:     &sdk.Metadata{Name: "high", Priority: 1},
			Config:       &Config{Enable: true},
			capabilities: []string{"test.echo"},
			calledPlugin: &high,
		},
	}
	sortPlugins()
	rebuildCapabilityIndex()

	resp, err := (&hostService{}).CallPlugin(context.Background(), &sdk.CallPlugin_Request{
		Capability: "test.echo",
		Args:       []byte(`{"value":"ok"}`),
	})
	if err != nil {
		t.Fatalf("call plugin: %v", err)
	}
	if got := resp.GetMime(); got != "text" {
		t.Fatalf("unexpected mime: %q", got)
	}
	if got := string(resp.GetData()); got != "high:test.echo:ok" {
		t.Fatalf("unexpected response: %q", got)
	}
}

func withPluginState(t *testing.T) {
	t.Helper()

	oldPlugins := plugins
	oldCommandIndex := commandIndex
	oldCapabilityIndex := capabilityIndex
	plugins = nil
	commandIndex = map[string]*wrapper{}
	capabilityIndex = map[string]*wrapper{}
	t.Cleanup(func() {
		plugins = oldPlugins
		commandIndex = oldCommandIndex
		capabilityIndex = oldCapabilityIndex
	})
}
