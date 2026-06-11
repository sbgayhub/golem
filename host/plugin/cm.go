package plugin

import (
	"fmt"
	"math"

	chatroomability "github.com/sbgayhub/golem/host/ability/chatroom"
	contactability "github.com/sbgayhub/golem/host/ability/contact"
	"github.com/sbgayhub/golem/sdk/contact"
	sdk "github.com/sbgayhub/golem/sdk/plugin"
)

type cmCommandPlugin struct {
	registry *sdk.CommandRegistry
}

type cmContactCommand struct {
	_   struct{} `cmd:"cm contact" help:"刷新联系人缓存" usage:"/cm contact [key]" example:"/cm contact\n/cm contact wxid_xxx\n/cm contact remark::张三"`
	Key string   `arg:"key" help:"联系人 key，可使用 username::、nickname::、remark:: 前缀"`
}

type cmChatroomCommand struct {
	_       struct{} `cmd:"cm chatroom" help:"刷新当前群成员缓存" usage:"/cm chatroom [key]" example:"/cm chatroom\n/cm chatroom wxid_xxx"`
	Key     string   `arg:"key" help:"群成员 username"`
	Command *sdk.Command
}

func registerBuiltinCM() error {
	cm, err := newCMCommandPlugin()
	if err != nil {
		return err
	}
	metadata := &sdk.Metadata{
		Name:        "cm",
		Author:      "golem",
		Version:     "builtin",
		Description: "联系人管理器",
		Priority:    math.MinInt32 + 1,
		Next:        false,
		AlwaysRun:   true,
	}
	w := &wrapper{
		Metadata:       metadata,
		Config:         &Config{Enable: true, Mode: "blacklist"},
		commands:       cm.GetCommands(),
		commandSchemas: cm.GetCommandSchemas(),
		types:          []string{"command", "builtin"},
	}
	cp := sdk.CommandPlugin(cm)
	w.commandPlugin = &cp

	plugins = append(plugins, w)
	sortPlugins()
	rebuildCommandIndex()
	rebuildCapabilityIndex()
	return nil
}

func newCMCommandPlugin() (*cmCommandPlugin, error) {
	cm := &cmCommandPlugin{registry: sdk.NewCommandRegistry()}
	if err := sdk.RegisterCommandTo(cm.registry, cm.contact); err != nil {
		return nil, err
	}
	if err := sdk.RegisterCommandTo(cm.registry, cm.chatroom); err != nil {
		return nil, err
	}
	return cm, nil
}

func (p *cmCommandPlugin) GetCommands() []string {
	return p.registry.Commands()
}

func (p *cmCommandPlugin) GetCommandSchemas() []*sdk.CommandSchema {
	return p.registry.Schemas()
}

func (p *cmCommandPlugin) OnCommand(cmd *sdk.Command) (string, error) {
	return p.registry.Dispatch(cmd)
}

func (p *cmCommandPlugin) contact(cmd cmContactCommand) (string, error) {
	if cmd.Key == "" {
		if err := contactability.Refresh(); err != nil {
			return "", err
		}
		return "联系人缓存已全部刷新", nil
	}

	c, err := contactability.RefreshOne(cmd.Key)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("联系人缓存已刷新：%s(%s)", displayContactName(c.Nickname, c.Remark), c.Username), nil
}

func (p *cmCommandPlugin) chatroom(cmd cmChatroomCommand) (string, error) {
	sender := cmd.Command.GetSender()
	if sender.GetType() != contact.ContactType_CONTACT_TYPE_CHATROOM {
		return "", fmt.Errorf("该命令仅支持在群聊中使用")
	}
	chatroom := sender.GetUsername()
	if chatroom == "" {
		return "", fmt.Errorf("命令来源群聊为空")
	}

	if cmd.Key == "" {
		if err := chatroomability.Refresh(chatroom); err != nil {
			return "", err
		}
		return "当前群成员缓存已全部刷新", nil
	}

	member, err := chatroomability.RefreshMember(chatroom, cmd.Key)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("群成员缓存已刷新：%s(%s)", displayContactName(member.Nickname, member.Remark), member.Username), nil
}

func displayContactName(nickname, remark string) string {
	if remark != "" {
		return remark
	}
	if nickname != "" {
		return nickname
	}
	return "未命名"
}
