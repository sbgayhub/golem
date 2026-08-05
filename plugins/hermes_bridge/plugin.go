package main

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/sbgayhub/golem/sdk/contact"
	"github.com/sbgayhub/golem/sdk/message"
	"github.com/sbgayhub/golem/sdk/plugin"
	"google.golang.org/protobuf/proto"
)

// GetMetadata 返回插件元数据
func (p *BridgePlugin) GetMetadata() *plugin.Metadata {
	return &plugin.Metadata{
		Name:        "hermes_bridge",
		Author:      "ovo",
		Version:     "0.4.0",
		Description: "Hermes 官方平台适配器桥：SSE 入站、HTTP 出站（含 mentions 真 @、AppMsg 卡片/音乐卡片）、self/群/成员查询。群门闩+去抖。",
		Priority:    1<<31 - 2,
		Next:        false,
		AlwaysRun:   false,
	}
}

// GetSubscriptions 订阅文本、引用及媒体入站消息。
// 图片/表情/语音/视频等媒体消息会转成 [图片]/[表情] 等文本标记再推送。
// 聊天记录（type=19）已可入站（见 t-doc/wechat-msg-formats.md）；业务暂不订阅，避免刷 SSE。
func (p *BridgePlugin) GetSubscriptions() []string {
	return []string{
		message.TypeText.Topic,
		message.TypeAppQuote.Topic,
		message.TypeImage.Topic,
		message.TypeEmoji.Topic,
		message.TypeVoice.Topic,
		message.TypeVideo.Topic,
	}
}

// OnLoad 启动桥 HTTP；本 host 不调 OnEnable，启停绑在 OnLoad/OnUnload。
func (p *BridgePlugin) OnLoad() error {
	p.stopped.Store(false)
	p.refreshSelf()
	if err := p.startHTTP(); err != nil {
		slog.Error("[hermes_bridge] HTTP 桥启动失败", "err", err)
	}
	return nil
}

// OnUnload 停止桥 HTTP 与所有去抖 timer
func (p *BridgePlugin) OnUnload() error {
	p.stopSessions()
	p.stopHTTP()
	return nil
}

// OnEnable 保留以满足生命周期接口
func (p *BridgePlugin) OnEnable() error { return nil }

// OnDisable 保留以满足生命周期接口
func (p *BridgePlugin) OnDisable() error { return nil }

// OnEvent 入站：
//   - 白名单过滤（主人私聊始终放行）
//   - 群：写入本地上下文；仅触发条件满足才去抖一批推 SSE（已推送消息不重复推）
//   - 私聊 / 审批捷径 / 打断捷径：立即推（打断还作废该会话未推去抖批）
//   - 无订阅者时丢弃
func (p *BridgePlugin) OnEvent(event *plugin.Event) (bool, error) {
	payload, ok := event.GetPayload().(*plugin.Event_Message)
	if !ok || payload.Message == nil {
		slog.Warn("[hermes_bridge] OnEvent 非消息或消息为空", "topic", event.GetTopic())
		return false, nil
	}
	if payload.Message.Sender != nil && payload.Message.Sender.Type == contact.ContactType_CONTACT_TYPE_SPECIAL {
		return false, nil
	}
	self := p.selfForEvent()
	slog.Debug("[hermes_bridge] OnEvent 收到消息", "type", fmt.Sprint(payload.Message.GetType()), "sender", fmt.Sprint(payload.Message.GetSender()), "member", fmt.Sprint(payload.Message.GetMember()))
	in, ok := p.buildIncoming(payload.Message, self)
	if !ok {
		slog.Warn("[hermes_bridge] buildIncoming 返回 false", "type", fmt.Sprint(payload.Message.GetType()))
		return false, nil
	}
	// 机器人自己发出的消息不回环
	if self != nil && in.SpeakerID == self.GetUsername() {
		return false, nil
	}

	in.SpeakerIsOwner = p.isOwnerID(in.SpeakerID)

	// 会话白名单：不在白名单则忽略；主人私聊始终放行
	target := p.findTarget(in.Receiver.GetUsername())
	if target == nil && !(in.SpeakerIsOwner && !in.IsChatroom) {
		slog.Info("[hermes_bridge] 不在白名单且非主人私聊，丢弃",
			"session", in.SessionKey,
			"receiver", in.Receiver.GetUsername(),
			"speaker", in.SpeakerID,
			"is_owner", in.SpeakerIsOwner,
			"is_chatroom", in.IsChatroom,
			"text", singleLine(in.Text))
		return false, nil
	}

	chatName := in.ChatroomName
	if !in.IsChatroom {
		chatName = in.SpeakerName
	}
	if target != nil && target.Name != "" {
		chatName = target.Name
	}

	// 审批捷径：立刻透传，不进群去抖（否则卡 yes/no）
	if isApprovalReplyText(in.Text) {
		if p.hub.subscriberCount() == 0 {
			slog.Info("[hermes_bridge] 无 SSE 订阅者，丢弃审批捷径", "session", in.SessionKey)
			return false, nil
		}
		ev := bridgeEvent{
			SessionKey:    in.SessionKey,
			ChatID:        in.Receiver.GetUsername(),
			ChatName:      chatName,
			ChatType:      "private",
			UserID:        in.SpeakerID,
			UserName:      in.SpeakerName,
			IsOwner:       in.SpeakerIsOwner,
			Text:          in.Text,
			MsgID:         in.MsgID,
			Addressing:    "self",
			TriggerReason: "approval_reply",
			Timestamp:     timeNowUnix(),
			MediaRef:      in.MediaRef,
		}
		if in.IsChatroom {
			ev.ChatType = "group"
		}
		p.pushImmediate(ev, "", 0)
		return false, nil
	}

	// 打断捷径：整句「打断」立刻透传（不限主人）；
	// 同时作废该会话当前去抖 pending（停 timer +未推批标 Flushed），避免 ⚡ 后尸体批次再唤醒 agent。
	if isInterruptReplyText(in.Text) {
		cancelled, dropped := p.cancelGroupPending(in.SessionKey)
		if cancelled || dropped > 0 {
			slog.Info("[hermes_bridge] 打断捷径已取消去抖 pending",
				"session", in.SessionKey, "cancelled", cancelled, "dropped", dropped,
				"user", in.SpeakerName, "owner", in.SpeakerIsOwner)
		}
		if p.hub.subscriberCount() == 0 {
			slog.Info("[hermes_bridge] 无 SSE 订阅者，丢弃打断捷径", "session", in.SessionKey)
			return false, nil
		}
		ev := bridgeEvent{
			SessionKey:    in.SessionKey,
			ChatID:        in.Receiver.GetUsername(),
			ChatName:      chatName,
			ChatType:      "private",
			UserID:        in.SpeakerID,
			UserName:      in.SpeakerName,
			IsOwner:       in.SpeakerIsOwner,
			Text:          strings.TrimSpace(in.Text),
			MsgID:         in.MsgID,
			Addressing:    "self",
			TriggerReason: "interrupt",
			Timestamp:     timeNowUnix(),
			MediaRef:      in.MediaRef,
		}
		if in.IsChatroom {
			ev.ChatType = "group"
		}
		p.pushImmediate(ev, "", 0)
		slog.Info("[hermes_bridge] 打断捷径已立即推送",
			"session", in.SessionKey, "chat_type", ev.ChatType, "user", in.SpeakerName)
		return false, nil
	}

	// 新开会话捷径：仅主人整句触发，立即透传（trigger_reason=session_reset），
	// 真正清 gateway session 由适配器执行（CLI prune --chat-id）。
	// 同时作废该会话未推送的去抖批次——旧上下文不该再灌进新 session。
	if in.SpeakerIsOwner && isSessionResetText(in.Text) {
		cancelled, dropped := p.cancelGroupPending(in.SessionKey)
		if cancelled || dropped > 0 {
			slog.Info("[hermes_bridge] 新开会话已取消去抖 pending",
				"session", in.SessionKey, "cancelled", cancelled, "dropped", dropped)
		}
		if p.hub.subscriberCount() == 0 {
			slog.Info("[hermes_bridge] 无 SSE 订阅者，丢弃新开会话捷径", "session", in.SessionKey)
			return false, nil
		}
		ev := bridgeEvent{
			SessionKey:    in.SessionKey,
			ChatID:        in.Receiver.GetUsername(),
			ChatName:      chatName,
			ChatType:      "private",
			UserID:        in.SpeakerID,
			UserName:      in.SpeakerName,
			IsOwner:       true,
			Text:          strings.TrimSpace(in.Text),
			MsgID:         in.MsgID,
			Addressing:    "self",
			TriggerReason: "session_reset",
			Timestamp:     timeNowUnix(),
		}
		if in.IsChatroom {
			ev.ChatType = "group"
		}
		p.pushImmediate(ev, "", 0)
		slog.Info("[hermes_bridge] 新开会话捷径已立即推送",
			"session", in.SessionKey, "chat_type", ev.ChatType)
		return false, nil
	}

	// 归档群友捷径：仅主人整句「归档/归档群友/…」立即透传。
	// 群门闩会吞无 @ 短词；必须旁路。适配器把短词扩成完整 member_profile upsert 指令。
	// 不作废去抖 pending：未推批与本次归档无关，保留给后续触发。
	if in.SpeakerIsOwner && isMemberArchiveText(in.Text) {
		if p.hub.subscriberCount() == 0 {
			slog.Info("[hermes_bridge] 无 SSE 订阅者，丢弃归档捷径", "session", in.SessionKey)
			return false, nil
		}
		ev := bridgeEvent{
			SessionKey:    in.SessionKey,
			ChatID:        in.Receiver.GetUsername(),
			ChatName:      chatName,
			ChatType:      "private",
			UserID:        in.SpeakerID,
			UserName:      in.SpeakerName,
			IsOwner:       true,
			Text:          strings.TrimSpace(in.Text),
			MsgID:         in.MsgID,
			Addressing:    "self",
			TriggerReason: "member_archive",
			Timestamp:     timeNowUnix(),
		}
		if in.IsChatroom {
			ev.ChatType = "group"
		}
		p.pushImmediate(ev, "", 0)
		slog.Info("[hermes_bridge] 归档捷径已立即推送",
			"session", in.SessionKey, "chat_type", ev.ChatType, "text", singleLine(in.Text))
		return false, nil
	}

	// ---- 私聊：立即推（连发排队收敛归适配器项 2）----
	if !in.IsChatroom {
		if p.hub.subscriberCount() == 0 {
			slog.Info("[hermes_bridge] 无 SSE 订阅者，丢弃私聊", "session", in.SessionKey)
			return false, nil
		}
		ev := bridgeEvent{
			SessionKey:    in.SessionKey,
			ChatID:        in.Receiver.GetUsername(),
			ChatName:      chatName,
			ChatType:      "private",
			UserID:        in.SpeakerID,
			UserName:      in.SpeakerName,
			IsOwner:       in.SpeakerIsOwner,
			Text:          in.Text,
			MsgID:         in.MsgID,
			MentionedBot:  in.MentionedBot,
			QuotedBot:     in.QuotedBot,
			Addressing:    "self",
			TriggerReason: "private_message",
			Timestamp:     timeNowUnix(),
			MediaRef:      in.MediaRef,
			EmojiMd5:      in.EmojiMd5,
			EmojiDesc:     in.EmojiDesc,
		}
		applyQuoteMeta(&ev, in.Quote)
		p.pushImmediate(ev, "", 0)
		slog.Info("[hermes_bridge] 私聊已推送", "session", in.SessionKey, "text", singleLine(in.Text), "media_ref", in.MediaRef, "msg_id", in.MsgID)
		return false, nil
	}

	// ---- 群聊 ----
	cfg := p.configSnapshot()
	// 即使本条不触发也先标记收件人：它可能作为上下文随另一条触发消息推送。
	classifyGroupAddressing(&in)

	// 回滚开关：group_push_all=true 时与改前门闩前行为一致，每条立即推
	if cfg.GroupPushAll {
		if p.hub.subscriberCount() == 0 {
			return false, nil
		}
		ev := bridgeEvent{
			SessionKey:    in.SessionKey,
			ChatID:        in.Receiver.GetUsername(),
			ChatName:      chatName,
			ChatType:      "group",
			UserID:        in.SpeakerID,
			UserName:      in.SpeakerName,
			IsOwner:       in.SpeakerIsOwner,
			Text:          in.Text,
			MsgID:         in.MsgID,
			MentionedBot:  in.MentionedBot,
			QuotedBot:     in.QuotedBot,
			Addressing:    in.Addressing,
			TriggerReason: "group_push_all",
			Timestamp:     timeNowUnix(),
			MediaRef:      in.MediaRef,
			EmojiMd5:      in.EmojiMd5,
			EmojiDesc:     in.EmojiDesc,
		}
		applyQuoteMeta(&ev, in.Quote)
		p.pushImmediate(ev, "", 0)
		return false, nil
	}

	// 先判本条的触发原因。trigger_names 命中会得到
	// addressing=self + trigger_reason=trigger_name。
	triggered := p.classifyGroupTrigger(&in)

	// 斗图门闩：每条表情都计数（含已因别的原因触发的）；
	// 本条未触发且连发达标时以 emoji_burst 送出一批。
	if in.IsEmoji {
		if burst := p.recordEmojiAndCheckBurst(in.SessionKey); burst && !triggered {
			in.TriggerReason = "emoji_burst"
			triggered = true
		}
	}

	// 无论是否触发：先写入本地滚动上下文（闲聊也记）。
	// 每条独立保存 addressing / trigger_reason，不能由本批其他消息污染。
	p.appendContext(in.SessionKey, contextMsg{
		SpeakerID:     in.SpeakerID,
		SpeakerName:   in.SpeakerName,
		IsOwner:       in.SpeakerIsOwner,
		Text:          in.Text,
		MsgID:         in.MsgID,
		Quote:         quoteSummary(in.Quote),
		QuoteType:     in.Quote.Type,
		QuoteFromUsr:  in.Quote.FromUser,
		QuoteDisplay:  strings.TrimSpace(in.Quote.DisplayName),
		Mentioned:     in.MentionedBot,
		Quoted:        in.QuotedBot,
		Addressing:    in.Addressing,
		TriggerReason: in.TriggerReason,
		Timestamp:     timeNowUnix(),
		MediaRef:      in.MediaRef,
		EmojiMd5:      in.EmojiMd5,
		EmojiDesc:     in.EmojiDesc,
	})

	// 未触发：只记不推
	if !triggered {
		return false, nil
	}

	// 无订阅者仍保留上下文；不调度 flush（避免标 Flushed 却推不出去）
	if p.hub.subscriberCount() == 0 {
		slog.Info("[hermes_bridge] 群触发但无 SSE 订阅者，仅记上下文", "session", in.SessionKey)
		return false, nil
	}

	// 触发：续命/合并去抖窗口，到期只推未 Flushed 增量
	p.scheduleGroupFlush(in.SessionKey, flushMeta{
		ChatID:        in.Receiver.GetUsername(),
		ChatName:      chatName,
		ChatType:      "group",
		UserID:        in.SpeakerID,
		UserName:      in.SpeakerName,
		IsOwner:       in.SpeakerIsOwner,
		Addressing:    in.Addressing,
		TriggerReason: in.TriggerReason,
	})
	return false, nil
}

// ---- 自身 / 主人 / 白名单 ----

func (p *BridgePlugin) isOwnerID(wxid string) bool {
	if strings.TrimSpace(wxid) == "" {
		return false
	}
	p.selfMu.RLock()
	owner := p.owner
	p.selfMu.RUnlock()
	return owner != nil && owner.GetUsername() == wxid
}

func (p *BridgePlugin) findTarget(id string) *Target {
	cfg := p.configSnapshot()
	for i := range cfg.Targets {
		if cfg.Targets[i].ID == id {
			t := cfg.Targets[i]
			return &t
		}
	}
	return nil
}

func (p *BridgePlugin) refreshSelf() {
	if p.contact == nil {
		slog.Warn("[hermes_bridge] contact ability 未注入，无法识别机器人与主人")
		return
	}
	self := p.contact.GetSelf()
	owner := p.contact.GetOwner()
	if self == nil {
		slog.Warn("[hermes_bridge] 获取机器人账号信息失败")
		return
	}
	p.selfMu.Lock()
	p.self = self
	p.owner = owner
	p.selfMu.Unlock()
}

func (p *BridgePlugin) selfSnapshot() *contact.SelfInfo {
	p.selfMu.RLock()
	defer p.selfMu.RUnlock()
	if p.self == nil {
		return nil
	}
	return proto.Clone(p.self).(*contact.SelfInfo)
}

func (p *BridgePlugin) selfForEvent() *contact.SelfInfo {
	self := p.selfSnapshot()
	if self != nil {
		return self
	}
	p.refreshSelf()
	return p.selfSnapshot()
}

func (p *BridgePlugin) saveConfig() {
	if err := p.SaveConfig(p); err != nil {
		slog.Error("[hermes_bridge] 保存配置失败", "err", err)
	}
}

func (p *BridgePlugin) resolveReceiver(id string) *contact.Contact {
	if p.contact != nil {
		if c := p.contact.Get("username::" + id); c != nil {
			return c
		}
	}
	return &contact.Contact{Username: id}
}

func (p *BridgePlugin) sendPlainText(receiver *contact.Contact, content string) error {
	if p.message == nil || receiver == nil || strings.TrimSpace(receiver.GetUsername()) == "" {
		return fmt.Errorf("消息能力未注入或接收方无效")
	}
	msg := &message.Message{
		Type:     message.TypeText,
		Receiver: receiver,
		Content:  content,
		Data:     &message.Message_Text{Text: &message.TextData{Content: content}},
	}
	_, err := p.message.Send(msg)
	return err
}

func (p *BridgePlugin) statusText() string {
	cfg := p.configSnapshot()
	// 主人 / Listen 不写明文：status 可能在群里由主人触发，避免泄露 wxid 与监听地址。
	ownerOK := "未识别"
	p.selfMu.RLock()
	if p.owner != nil && strings.TrimSpace(p.owner.GetUsername()) != "" {
		ownerOK = "已识别"
	}
	p.selfMu.RUnlock()
	sessN, pendN, bufN := p.sessionStats()
	triggers := strings.Join(cfg.TriggerNames, ", ")
	if triggers == "" {
		triggers = "（未配置）"
	}
	lines := []string{
		"hermes_bridge 状态：",
		fmt.Sprintf("Token: %s", maskToken(cfg.Token)),
		fmt.Sprintf("SSE 订阅者: %d", p.hub.subscriberCount()),
		fmt.Sprintf("限流 %d 条/分 / 文本上限 %d 字 / body 上限 %dMB",
			cfg.SendRatePerMin, cfg.MaxTextLen, cfg.MaxBodyBytes>>20),
		fmt.Sprintf("群门闩: %v | 去抖 %ds | 冒泡 %.2f / 冷却 %dmin | 上下文 %d 条",
			!cfg.GroupPushAll, cfg.DebounceSeconds, cfg.BubbleRate, cfg.BubbleCooldownMin, cfg.MaxContextMessages),
		fmt.Sprintf("斗图门闩: 窗口%ds内第%d条表情触发 / 冷却 %dmin（0=关）",
			cfg.EmojiBurstWindowSec, cfg.EmojiBurstCount, cfg.EmojiBurstCooldownMin),
		"点名: " + triggers,
		fmt.Sprintf("本地会话 %d | pending 去抖 %d | 未推送缓冲 %d 条", sessN, pendN, bufN),
		"主人: " + ownerOK,
		// 白名单只报数量：status 常在群里回，避免摊开 wxid / @chatroom
		fmt.Sprintf("白名单: %d 个", len(cfg.Targets)),
	}
	return strings.Join(lines, "\n")
}

func maskToken(t string) string {
	t = strings.TrimSpace(t)
	if t == "" {
		return "（未配置）"
	}
	if len(t) <= 6 {
		return "***"
	}
	return t[:3] + "***" + t[len(t)-3:]
}

func timeNowUnix() int64 {
	return unixNow()
}
