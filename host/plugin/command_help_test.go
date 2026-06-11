package plugin

import (
	"strings"
	"testing"

	sdk "github.com/sbgayhub/golem/sdk/plugin"
)

func TestRenderCommandHelpAlignsOptionDescriptions(t *testing.T) {
	schema := &sdk.CommandSchema{
		Main:        "pm",
		Sub:         "set",
		Description: "修改插件运行配置",
		Usage:       "/pm set <name> [-p priority] [-a true|false] [-n true|false] [-c config]",
		Arguments: []*sdk.CommandArgument{
			{Name: "name", Description: "插件名称", Required: true},
		},
		Options: []*sdk.CommandOption{
			{Short: "p", Long: "priority", Description: "插件优先级"},
			{Short: "a", Long: "always_run", Description: "是否一直运行"},
			{Short: "n", Long: "next", Description: "成功后是否继续处理后续插件"},
			{Short: "c", Long: "config", Description: "TOML 配置字符串"},
		},
	}

	got := renderCommandHelp(schema)
	want := strings.Join([]string{
		"参数：",
		"  -p, --priority      可选  插件优先级",
		"  -a, --always_run    可选  是否一直运行",
		"  -n, --next          可选  成功后是否继续处理后续插件",
		"  -c, --config        可选  TOML 配置字符串",
	}, "\n")
	if !strings.Contains(got, want) {
		t.Fatalf("help options are not aligned:\n%s", got)
	}
}

func TestRenderMainHelpAlignsSubcommandDescriptions(t *testing.T) {
	schemas := []*sdk.CommandSchema{
		{Main: "pm", Description: "插件管理器"},
		{Main: "pm", Sub: "load", Description: "加载插件"},
		{Main: "pm", Sub: "unload", Description: "卸载插件"},
		{Main: "pm", Sub: "reload", Description: "重载插件"},
	}

	got := renderMainHelp("pm", schemas)
	want := strings.Join([]string{
		"子命令：",
		"  load      加载插件",
		"  unload    卸载插件",
		"  reload    重载插件",
	}, "\n")
	if !strings.Contains(got, want) {
		t.Fatalf("help subcommands are not aligned:\n%s", got)
	}
}
