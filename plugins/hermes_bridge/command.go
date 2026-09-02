package main

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/sbgayhub/golem/sdk/plugin"
)

// 命令由 host 层校验主人权限；群聊里 member.Username == owner 也放行。

type hermesStatusCommand struct {
	_       struct{} `cmd:"hermes status" help:"查看 hermes_bridge 状态与白名单" usage:"/hermes status" example:"/hermes status"`
	Command *plugin.Command
}

type hermesEnableCommand struct {
	_       struct{} `cmd:"hermes enable" help:"把当前会话加入路由白名单" usage:"/hermes enable [名称]" example:"/hermes enable\n/hermes enable 老友群"`
	Name    string   `arg:"name" help:"会话显示名；省略则用当前会话名" variadic:"true"`
	Command *plugin.Command
}

type hermesDisableCommand struct {
	_       struct{} `cmd:"hermes disable" help:"把当前会话移出路由白名单" usage:"/hermes disable" example:"/hermes disable"`
	Command *plugin.Command
}

type hermesImageCommand struct {
	_       struct{} `cmd:"hermes image" help:"诊断：下载 URL 并直发图片" usage:"/hermes image <url>" example:"/hermes image https://example.com/a.png"`
	URL     string   `arg:"url" help:"图片 http/https 地址" required:"true" variadic:"true"`
	Command *plugin.Command
}

type hermesVideoCommand struct {
	_       struct{} `cmd:"hermes video" help:"诊断：下载 URL 并直发视频" usage:"/hermes video <url>" example:"/hermes video https://example.com/a.mp4"`
	URL     string   `arg:"url" help:"视频 http/https 地址" required:"true" variadic:"true"`
	Command *plugin.Command
}

type hermesEmojiCommand struct {
	_       struct{} `cmd:"hermes emoji" help:"诊断：下载 URL 并直发表情（TypeEmoji）" usage:"/hermes emoji <url>" example:"/hermes emoji https://example.com/a.png"`
	URL     string   `arg:"url" help:"表情图 http/https 地址" required:"true" variadic:"true"`
	Command *plugin.Command
}

type hermesHelpCommand struct {
	_ struct{} `cmd:"hermes help" help:"显示 hermes_bridge 管理命令" usage:"/hermes help" example:"/hermes help"`
}

func registerCommands(p *BridgePlugin) error {
	handlers := []func() error{
		func() error { return plugin.RegisterCommand(p.handleStatus) },
		func() error { return plugin.RegisterCommand(p.handleEnable) },
		func() error { return plugin.RegisterCommand(p.handleDisable) },
		func() error { return plugin.RegisterCommand(p.handleImage) },
		func() error { return plugin.RegisterCommand(p.handleVideo) },
		func() error { return plugin.RegisterCommand(p.handleEmoji) },
		func() error { return plugin.RegisterCommand(p.handleHelp) },
	}
	for _, reg := range handlers {
		if err := reg(); err != nil {
			return err
		}
	}
	return nil
}

// GetCommands 主命令列表
func (p *BridgePlugin) GetCommands() []string {
	return plugin.CommandCommands()
}

// OnCommand 分发
func (p *BridgePlugin) OnCommand(cmd *plugin.Command) (string, error) {
	return plugin.DispatchCommand(cmd)
}

func (p *BridgePlugin) handleStatus(cmd hermesStatusCommand) (string, error) {
	// 带上当前会话，好让「可撤 N 条」这类按会话的计数落在主人正看的这个会话上
	return p.statusText(cmd.Command.GetSender().GetUsername()), nil
}

func (p *BridgePlugin) handleHelp(hermesHelpCommand) (string, error) {
	cfg := p.configSnapshot()
	adminHint := "管理台未启用（admin_listen 为空）"
	if addr := strings.TrimSpace(cfg.AdminListen); addr != "" {
		adminHint = "本机管理台 http://" + addr + "/ui/ （需配置 admin_token，与业务 token 分离）"
	}
	return strings.Join([]string{
		"hermes_bridge 管理命令（仅主人，host 层已校验）：",
		"/hermes status — 查看桥状态、群门闩与白名单",
		"/hermes enable [名称] — 当前会话加入路由白名单",
		"/hermes disable — 当前会话移出白名单",
		"/hermes image <url> — 诊断直发图片",
		"/hermes video <url> — 诊断直发视频",
		"/hermes emoji <url|md5> — 诊断直发表情（URL下载压缩 / md5引用原图不压）",
		"/hermes help — 本说明",
		"",
		adminHint,
		"也可在管理台远程加白名单/调门闩，不必人在群里发 enable。",
		"",
		"群聊：闲聊进本地上下文；@ / 引用机器人 / trigger_names / 冒泡 才去抖一批推 SSE。",
		"私聊：白名单或主人会话逐条推。审批回复用 yes/no（不要 /approve）。",
		"打断：群/私聊整句发「打断」（不限主人）立即透传，并取消该会话未推去抖批。",
		"归档：主人整句「归档/归档群友/记群友」立即透传，适配器扩成批量写群友档案（不清会话）。",
		"撤回：主人整句「" + strings.Join(p.revokeTokens(), "/") + "」由桥直接撤掉自己最近发的一条" +
			fmt.Sprintf("（不经 agent；微信 %ds 时限内有效）。", int(p.revokeWindow().Seconds())),
		"　　　agent 也可自己撤（wechat_revoke 工具 → 桥 /revoke，可带 count 撤多条）。",
		"危险终端命令由 Hermes approvals 审批，不在本插件裁决。",
	}, "\n"), nil
}

func (p *BridgePlugin) handleEnable(cmd hermesEnableCommand) (string, error) {
	sender := cmd.Command.GetSender()
	if sender == nil || sender.GetUsername() == "" {
		return "", fmt.Errorf("无法识别当前会话")
	}
	id := sender.GetUsername()
	name := strings.TrimSpace(cmd.Name)
	if name == "" {
		name = displayContact(sender)
	}
	if p.findTarget(id) != nil {
		return "当前会话已在白名单中", nil
	}
	p.cfgMu.Lock()
	p.Config.Targets = append(append([]Target(nil), p.Config.Targets...), Target{ID: id, Name: name})
	p.cfgMu.Unlock()
	p.saveConfig()
	return "已加入白名单：" + name + "（" + id + "）", nil
}

func (p *BridgePlugin) handleDisable(cmd hermesDisableCommand) (string, error) {
	sender := cmd.Command.GetSender()
	if sender == nil || sender.GetUsername() == "" {
		return "", fmt.Errorf("无法识别当前会话")
	}
	id := sender.GetUsername()
	p.cfgMu.Lock()
	targets := make([]Target, 0, len(p.Config.Targets))
	removed := false
	for _, t := range p.Config.Targets {
		if t.ID == id {
			removed = true
			continue
		}
		targets = append(targets, t)
	}
	p.Config.Targets = targets
	p.cfgMu.Unlock()
	if !removed {
		return "当前会话不在白名单中", nil
	}
	p.saveConfig()
	return "已移出白名单", nil
}

func (p *BridgePlugin) handleImage(cmd hermesImageCommand) (string, error) {
	url := strings.TrimSpace(cmd.URL)
	if url == "" {
		return "", fmt.Errorf("用法：/hermes image <图片URL>")
	}
	sender := cmd.Command.GetSender()
	if sender == nil {
		return "", fmt.Errorf("无法识别当前会话")
	}
	if p.message == nil {
		return "", fmt.Errorf("消息能力未注入")
	}
	data, err := p.downloadImage(url)
	if err != nil {
		return "下载失败：" + err.Error(), nil
	}
	outcome, err := p.sendImageMessage(sender.GetUsername(), data)
	if outcome != uploadOK {
		return fmt.Sprintf("发送失败(outcome=%d)：%v", outcome, err), nil
	}
	return fmt.Sprintf("已直发图片（%d 字节）", len(data)), nil
}

func (p *BridgePlugin) handleVideo(cmd hermesVideoCommand) (string, error) {
	url := strings.TrimSpace(cmd.URL)
	if url == "" {
		return "", fmt.Errorf("用法：/hermes video <视频URL>")
	}
	sender := cmd.Command.GetSender()
	if sender == nil {
		return "", fmt.Errorf("无法识别当前会话")
	}
	if p.message == nil {
		return "", fmt.Errorf("消息能力未注入")
	}
	// 诊断命令不自动 fallback 链接，便于直接看 Send 的 outcome（与 image 一致）
	data, err := p.downloadBytes(url, maxVideoBytes)
	if err != nil {
		return "下载失败：" + err.Error(), nil
	}
	outcome, err := p.sendVideoMessage(sender.GetUsername(), data)
	if outcome != uploadOK {
		return fmt.Sprintf("发送失败(outcome=%d)：%v", outcome, err), nil
	}
	return fmt.Sprintf("已直发视频（%d 字节）", len(data)), nil
}

func (p *BridgePlugin) handleEmoji(cmd hermesEmojiCommand) (string, error) {
	arg := strings.TrimSpace(cmd.URL)
	if arg == "" {
		return "", fmt.Errorf("用法：/hermes emoji <URL 或 32位md5>（md5 为收藏引用发送，不压缩）")
	}
	sender := cmd.Command.GetSender()
	if sender == nil {
		return "", fmt.Errorf("无法识别当前会话")
	}
	if p.message == nil {
		return "", fmt.Errorf("消息能力未注入")
	}

	// md5 引用发送（32 位 hex，不走下载压缩）
	if len(arg) == 32 && !strings.Contains(arg, "/") {
		if _, err := hex.DecodeString(arg); err == nil {
			outcome, err := p.sendEmojiByMd5(sender.GetUsername(), arg)
			if outcome != uploadOK {
				return fmt.Sprintf("md5 引用发送失败(outcome=%d)：%v", outcome, err), nil
			}
			return fmt.Sprintf("已发 md5 引用表情 %s", arg), nil
		}
	}

	data, err := p.downloadEmoji(arg)
	if err != nil {
		return "下载失败：" + err.Error(), nil
	}
	before := len(data)
	data = ensureEmojiBytes(data)
	outcome, err := p.sendEmojiMessage(sender.GetUsername(), data)
	if outcome != uploadOK {
		return fmt.Sprintf("发送失败(outcome=%d)：%v", outcome, err), nil
	}
	if before != len(data) {
		return fmt.Sprintf("已直发表情（压缩 %d → %d 字节）", before, len(data)), nil
	}
	return fmt.Sprintf("已直发表情（%d 字节）", len(data)), nil
}

func unixNow() int64 {
	return time.Now().Unix()
}
