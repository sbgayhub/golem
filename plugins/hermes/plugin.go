package main

import (
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"github.com/sbgayhub/golem/sdk/contact"
	"github.com/sbgayhub/golem/sdk/message"
	"github.com/sbgayhub/golem/sdk/plugin"
	"google.golang.org/protobuf/proto"
)

// GetMetadata 返回插件元数据
func (p *HermesPlugin) GetMetadata() *plugin.Metadata {
	return &plugin.Metadata{
		Name:        "hermes",
		Author:      "ovo",
		Version:     "0.1.0",
		Description: "[已弃用] 请改用 hermes_bridge + Hermes wechat_golem 平台适配器。",
		Priority:    1<<31 - 2,
		Next:        false,
		AlwaysRun:   false,
	}
}

// GetSubscriptions 订阅文本与引用消息
func (p *HermesPlugin) GetSubscriptions() []string {
	return []string{message.TypeText.Topic, message.TypeAppQuote.Topic}
}

// OnLoad 旧架构已弃用：不再启动 MCP / worker。
// 请切到 plugins/hermes_bridge + Hermes 官方平台适配器 wechat_golem（见 t-doc/wechat_golem/）。
func (p *HermesPlugin) OnLoad() error {
	slog.Warn("[hermes] 此插件已弃用，不再启动。请改用 hermes_bridge + Hermes wechat_golem 平台适配器")
	return nil
}

// OnUnload 已弃用：无操作
func (p *HermesPlugin) OnUnload() error {
	return nil
}

// OnEnable 插件启用（本 host 不调用，保留以满足生命周期接口）
func (p *HermesPlugin) OnEnable() error {
	return nil
}

// OnDisable 插件禁用（本 host 不调用，保留以满足生命周期接口）
func (p *HermesPlugin) OnDisable() error {
	return nil
}

// OnEvent 已弃用：不处理任何消息
func (p *HermesPlugin) OnEvent(event *plugin.Event) (bool, error) {
	_ = event
	return false, nil
}

// deprecatedOnEvent 保留旧入站逻辑供阅读/回滚，不再被调用。
func (p *HermesPlugin) deprecatedOnEvent(event *plugin.Event) (bool, error) {
	payload, ok := event.GetPayload().(*plugin.Event_Message)
	if !ok || payload.Message == nil {
		return false, nil
	}
	if payload.Message.Sender.Type == contact.ContactType_CONTACT_TYPE_SPECIAL {
		return false, nil
	}
	self := p.selfForEvent()
	in, ok := buildIncoming(payload.Message, self)
	if !ok {
		return false, nil
	}
	// 机器人自己发出的消息不再入站（工具发送时已回写上下文，避免回环）
	if self != nil && in.SpeakerID == self.GetUsername() {
		return false, nil
	}

	isOwner := p.isOwnerID(in.SpeakerID)
	in.SpeakerIsOwner = isOwner

	// 主人管理命令：本地执行，不进 agent；非主人发送命令样式的消息直接吞掉
	if cmd, isCmd := matchCommand(in.Text); isCmd {
		if !isOwner {
			return true, nil
		}
		reply := p.handleOwnerCommand(cmd, in)
		if reply != "" {
			p.sendPlainText(in.Receiver, reply)
		}
		return true, nil
	}

	// 白名单过滤：不在白名单的会话完全忽略；主人私聊始终允许
	target := p.findTarget(in.Receiver.GetUsername())
	if target == nil && !(isOwner && !in.IsChatroom) {
		return false, nil
	}

	// 无论是否触发，消息一律进滚动上下文（“没回复也看得到前面聊了什么”）
	p.appendSession(in.SessionKey, chatMessage{Role: "user", Content: in.promptContent()})

	if !p.shouldTrigger(in) {
		return false, nil
	}

	name := in.ChatroomName
	if !in.IsChatroom {
		name = in.SpeakerName
	}
	if target != nil && target.Name != "" {
		name = target.Name
	}
	rc := &runContext{
		SessionKey: in.SessionKey,
		TargetID:   in.Receiver.GetUsername(),
		TargetName: name,
		SenderID:   in.SpeakerID,
		SenderName: in.SpeakerName,
		IsOwner:    isOwner,
		IsChatroom: in.IsChatroom,
		Receiver:   in.Receiver,
	}
	p.enqueue(rc)
	return true, nil
}

// shouldTrigger 判断该消息是否触发一次 agent run
func (p *HermesPlugin) shouldTrigger(in incomingMessage) bool {
	// 私聊即对话，逐条触发
	if !in.IsChatroom {
		return true
	}
	if in.MentionedBot || in.QuotedBot {
		return true
	}
	cfg := p.configSnapshot()
	lower := strings.ToLower(in.Text)
	for _, name := range cfg.TriggerNames {
		name = strings.ToLower(strings.TrimSpace(name))
		if name != "" && strings.Contains(lower, name) {
			return true
		}
	}
	// 冒泡：低概率主动看一眼群聊，是否说话由 agent 决定
	if cfg.BubbleRate > 0 && rand.Float64() < cfg.BubbleRate {
		cooldown := time.Duration(cfg.BubbleCooldownMin) * time.Minute
		p.bubbleMu.Lock()
		defer p.bubbleMu.Unlock()
		if time.Since(p.lastBubble[in.SessionKey]) >= cooldown {
			p.lastBubble[in.SessionKey] = time.Now()
			return true
		}
	}
	return false
}

// isOwnerID 判断 wxid 是否为主人（协议层身份硬比对，权限判定唯一入口）
func (p *HermesPlugin) isOwnerID(wxid string) bool {
	if strings.TrimSpace(wxid) == "" {
		return false
	}
	p.selfMu.RLock()
	owner := p.owner
	p.selfMu.RUnlock()
	return owner != nil && owner.GetUsername() == wxid
}

// findTarget 按会话 wxid 查白名单
func (p *HermesPlugin) findTarget(id string) *Target {
	cfg := p.configSnapshot()
	for i := range cfg.Targets {
		if cfg.Targets[i].ID == id {
			t := cfg.Targets[i]
			return &t
		}
	}
	return nil
}

// ---- 主人管理命令 ----

type ownerCommand struct {
	verb string
	arg  string
}

// matchCommand 匹配管理命令。命令固定以 hermes 开头且紧跟命令词（无空格），
// 如「hermes状态」「hermes冒泡 0.2」；普通聊天里喊 hermes 不会被误判。
func matchCommand(text string) (ownerCommand, bool) {
	t := strings.TrimSpace(text)
	lower := strings.ToLower(t)
	if !strings.HasPrefix(lower, "hermes") {
		return ownerCommand{}, false
	}
	rest := strings.TrimSpace(t[len("hermes"):])
	for _, verb := range []string{"帮助", "状态", "启用", "停用", "主动", "重置", "冒泡", "兜底", "发图"} {
		if strings.HasPrefix(rest, verb) {
			return ownerCommand{verb: verb, arg: strings.TrimSpace(strings.TrimPrefix(rest, verb))}, true
		}
	}
	return ownerCommand{}, false
}

// handleOwnerCommand 执行主人管理命令并返回回执文本
func (p *HermesPlugin) handleOwnerCommand(cmd ownerCommand, in incomingMessage) string {
	switch cmd.verb {
	case "帮助":
		return strings.Join([]string{
			"hermes 管理命令（仅主人）：",
			"hermes状态 — 查看配置与白名单",
			"hermes启用 [名称] — 把当前会话加入白名单",
			"hermes停用 — 把当前会话移出白名单",
			"hermes主动 开|关 — 允许/禁止无触发场景（定时任务等）主动发到当前会话",
			"hermes重置 — 清空当前会话上下文",
			"hermes冒泡 <0~1> — 设置冒泡概率",
			"hermes兜底 开|关 — agent 未调工具时是否兜底回复",
			"hermes发图 <url> — 诊断用：同步直发一张图（走 message.Send，不经过 agent/MCP）",
		}, "\n")
	case "状态":
		return p.statusText()
	case "启用":
		return p.enableTarget(in, cmd.arg)
	case "停用":
		return p.disableTarget(in)
	case "主动":
		return p.toggleProactive(in, cmd.arg)
	case "重置":
		p.clearSession(in.SessionKey)
		return "已清空当前会话上下文"
	case "冒泡":
		rate, err := strconv.ParseFloat(cmd.arg, 64)
		if err != nil || rate < 0 || rate > 1 {
			return "用法：hermes冒泡 <0~1>，如 hermes冒泡 0.05"
		}
		p.cfgMu.Lock()
		p.Config.BubbleRate = rate
		p.cfgMu.Unlock()
		p.saveConfig()
		return fmt.Sprintf("冒泡概率已设为 %.2f", rate)
	case "兜底":
		switch cmd.arg {
		case "开":
			p.cfgMu.Lock()
			p.Config.FallbackReply = true
			p.cfgMu.Unlock()
		case "关":
			p.cfgMu.Lock()
			p.Config.FallbackReply = false
			p.cfgMu.Unlock()
		default:
			return "用法：hermes兜底 开|关"
		}
		p.saveConfig()
		return "兜底回复已" + cmd.arg
	case "发图":
		return p.diagSendImage(in, cmd.arg)
	}
	return ""
}

// diagSendImage 诊断命令「hermes发图 <url>」的执行体：
// 在 OnEvent 同步栈里直走 sendImageMessage（message.Send + ImageData，与正常发图、
// 与 universal 实测稳成那条同一路径），不经过 agent run、不经过内嵌 MCP，
// 用于隔离“hermes 这条发送链路本身是否能成”。失败时把底层 err 原样回执，方便对照日志。
func (p *HermesPlugin) diagSendImage(in incomingMessage, imageRawURL string) string {
	if strings.TrimSpace(imageRawURL) == "" {
		return "用法：hermes发图 <图片URL>"
	}
	if p.message == nil {
		return "消息能力未注入"
	}
	data, err := p.downloadImage(imageRawURL)
	if err != nil {
		return "下载失败：" + err.Error()
	}
	outcome, err := p.sendImageMessage(in.Receiver.GetUsername(), data)
	if outcome != uploadOK {
		slog.Error("[hermes] 诊断发图 sendImageMessage 失败", "target", in.Receiver.GetUsername(), "outcome", outcome, "err", err)
		return fmt.Sprintf("发送失败(outcome=%d)：%v", outcome, err)
	}
	return fmt.Sprintf("已直发图片（%d 字节）", len(data))
}

// statusText 生成状态回执
func (p *HermesPlugin) statusText() string {
	cfg := p.configSnapshot()
	lines := []string{
		"hermes 状态：",
		"API: " + cfg.BaseURL + " (" + cfg.Model + ")",
		"MCP: " + cfg.MCPListen,
		fmt.Sprintf("冒泡 %.2f / 去抖 %ds / 兜底 %v / 限流 %d条/分", cfg.BubbleRate, cfg.DebounceSeconds, cfg.FallbackReply, cfg.SendRatePerMin),
		fmt.Sprintf("白名单 %d 个：", len(cfg.Targets)),
	}
	for _, t := range cfg.Targets {
		mark := ""
		if t.Proactive {
			mark = "（可主动发送）"
		}
		lines = append(lines, "- "+t.Name+" "+t.ID+mark)
	}
	return strings.Join(lines, "\n")
}

// enableTarget 把当前会话加入白名单
func (p *HermesPlugin) enableTarget(in incomingMessage, name string) string {
	id := in.Receiver.GetUsername()
	if name == "" {
		if in.IsChatroom {
			name = in.ChatroomName
		} else {
			name = in.SpeakerName
		}
	}
	if p.findTarget(id) != nil {
		return "当前会话已在白名单中"
	}
	p.cfgMu.Lock()
	targets := append(append([]Target(nil), p.Config.Targets...), Target{ID: id, Name: name, Proactive: false})
	p.Config.Targets = targets
	p.cfgMu.Unlock()
	p.saveConfig()
	return "已加入白名单：" + name + "（" + id + "）"
}

// disableTarget 把当前会话移出白名单
func (p *HermesPlugin) disableTarget(in incomingMessage) string {
	id := in.Receiver.GetUsername()
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
		return "当前会话不在白名单中"
	}
	p.saveConfig()
	p.clearSession(in.SessionKey)
	return "已移出白名单并清空上下文"
}

// toggleProactive 开关当前会话的 proactive 标志：
// 开 → 允许无触发场景（Hermes cron/CLI/自主任务）主动发到本会话；
// 关 → 恢复默认，无触发时不得发。配合 run_token 机制：令牌只发给微信触发的 run，
// 无触发 run 拿不到令牌，发本会话需要它在 proactive 允许集合内。
func (p *HermesPlugin) toggleProactive(in incomingMessage, arg string) string {
	want := strings.TrimSpace(arg)
	id := in.Receiver.GetUsername()
	name := in.ChatroomName
	if !in.IsChatroom {
		name = in.SpeakerName
	}
	// 找当前会话显示名，回执里用得上
	for _, t := range p.configSnapshot().Targets {
		if t.ID == id {
			name = t.Name
			break
		}
	}
	p.cfgMu.Lock()
	touched := false
	for i := range p.Config.Targets {
		if p.Config.Targets[i].ID == id {
			switch want {
			case "开":
				p.Config.Targets[i].Proactive = true
			case "关":
				p.Config.Targets[i].Proactive = false
			default:
				p.cfgMu.Unlock()
				return "用法：hermes主动 开|关"
			}
			touched = true
			break
		}
	}
	p.cfgMu.Unlock()
	if !touched {
		return "当前会话不在白名单中，先用 hermes启用 加入"
	}
	p.saveConfig()
	switch want {
	case "开":
		return name + " 已允许定时任务/自主任务主动发消息"
	default:
		return name + " 已禁止定时任务/自主任务主动发消息"
	}
}

// saveConfig 持久化配置
func (p *HermesPlugin) saveConfig() {
	if err := p.SaveConfig(p); err != nil {
		slog.Error("[hermes] 保存配置失败", "err", err)
	}
}

// ---- 自身与主人信息 ----

// refreshSelf 刷新机器人与主人信息
func (p *HermesPlugin) refreshSelf() {
	if p.contact == nil {
		slog.Warn("[hermes] contact ability 未注入，无法识别机器人与主人")
		return
	}
	self := p.contact.GetSelf()
	owner := p.contact.GetOwner()
	if self == nil {
		slog.Warn("[hermes] 获取机器人账号信息失败")
		return
	}
	p.selfMu.Lock()
	p.self = self
	p.owner = owner
	p.selfMu.Unlock()
}

// selfSnapshot 获取自身信息快照
func (p *HermesPlugin) selfSnapshot() *contact.SelfInfo {
	p.selfMu.RLock()
	defer p.selfMu.RUnlock()
	if p.self == nil {
		return nil
	}
	return proto.Clone(p.self).(*contact.SelfInfo)
}

// selfForEvent 获取事件用的自身信息（缺失时补拉一次）
func (p *HermesPlugin) selfForEvent() *contact.SelfInfo {
	self := p.selfSnapshot()
	if self != nil {
		return self
	}
	p.refreshSelf()
	return p.selfSnapshot()
}
