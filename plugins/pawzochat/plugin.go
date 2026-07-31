package main

import (
	"errors"
	"strings"
	"time"

	"github.com/sbgayhub/golem/sdk/contact"
	"github.com/sbgayhub/golem/sdk/message"
	"github.com/sbgayhub/golem/sdk/plugin"
	"google.golang.org/protobuf/proto"
)

func (p *PawzoChatPlugin) GetMetadata() *plugin.Metadata {
	return &plugin.Metadata{
		Name:        "pawzochat",
		Author:      "PawzoChat",
		Version:     "0.2.0",
		Description: "将 golem 微信消息路由到 PawzoChat 角色并回传回复。",
		Priority:    1<<31 - 1,
		Next:        false,
		AlwaysRun:   false,
	}
}

func (p *PawzoChatPlugin) GetSubscriptions() []string {
	return []string{message.TypeText.Topic, message.TypeAppQuote.Topic}
}

func (p *PawzoChatPlugin) OnLoad() error {
	p.normalizeConfig()
	p.refreshIdentity()
	return nil
}

func (p *PawzoChatPlugin) OnUnload() error { return nil }

func (p *PawzoChatPlugin) OnEnable() error {
	p.normalizeConfig()
	p.refreshIdentity()
	return nil
}

func (p *PawzoChatPlugin) OnDisable() error { return nil }

func (p *PawzoChatPlugin) OnConfigChange() error {
	p.normalizeConfig()
	return nil
}

func (p *PawzoChatPlugin) OnEvent(event *plugin.Event) (bool, error) {
	payload, ok := event.GetPayload().(*plugin.Event_Message)
	if !ok || payload.Message == nil {
		return false, nil
	}
	if payload.Message.GetSender().GetType() == contact.ContactType_CONTACT_TYPE_SPECIAL {
		return false, nil
	}
	config := p.configSnapshot()
	self, ownerID, ownerName := p.identityForEvent()
	incoming, ok := buildIncoming(payload.Message, self, ownerID, ownerName)
	if !ok {
		return false, nil
	}
	personaID := p.personaForSession(config, incoming.SessionKey)
	if personaID == "" {
		return false, nil
	}
	if self == nil || strings.TrimSpace(self.GetUsername()) == "" {
		return true, errors.New("golem self identity is unavailable")
	}
	if strings.TrimSpace(ownerID) == "" {
		return true, errors.New("golem owner identity is unavailable")
	}
	if incoming.IsChatroom && !config.RespondToAllGroupMessages &&
		!incoming.MentionedBot && !incoming.QuotedBot {
		return false, nil
	}

	outputs, err := p.requestReply(config, personaID, incoming)
	if err != nil {
		return true, err
	}
	if len(outputs) == 0 {
		return true, errors.New("PawzoChat returned no deliverable content")
	}
	for _, output := range outputs {
		if err := p.sendOutput(incoming.Receiver, output); err != nil {
			return true, err
		}
	}
	return true, nil
}

func (p *PawzoChatPlugin) sendOutput(receiver *contact.Contact, output outbound) error {
	if p.message == nil {
		return errors.New("message ability is not injected")
	}
	if receiver == nil || strings.TrimSpace(receiver.GetUsername()) == "" {
		return errors.New("receiver is empty")
	}
	msg := &message.Message{Receiver: receiver, Content: output.Text}
	switch output.Kind {
	case "text":
		msg.Type = message.TypeText
		msg.Data = &message.Message_Text{Text: &message.TextData{Content: output.Text}}
	case "image":
		msg.Type = message.TypeImage
		msg.Data = &message.Message_Image{Image: &message.ImageData{Media: mediaData(output.Data)}}
	case "emoji":
		msg.Type = message.TypeEmoji
		msg.Data = &message.Message_Emoji{Emoji: &message.EmojiData{
			Media: mediaData(output.Data), Desc: output.Text,
		}}
	case "voice":
		msg.Type = message.TypeVoice
		msg.Data = &message.Message_Voice{Voice: &message.VoiceData{
			Media: mediaData(output.Data), Duration: output.DurationMS,
		}}
	default:
		return errors.New("unsupported PawzoChat output kind: " + output.Kind)
	}
	_, err := p.message.Send(msg)
	return err
}

func mediaData(data []byte) *message.Media {
	return &message.Media{Data: data, Size: uint32(len(data))}
}

const identityRefreshTTL = 30 * time.Second

func (p *PawzoChatPlugin) refreshIdentity() {
	if p.contact == nil {
		return
	}
	p.identityRefresh.Lock()
	defer p.identityRefresh.Unlock()
	p.identityMu.RLock()
	fresh := p.self != nil && time.Since(p.identityUpdatedAt) < identityRefreshTTL
	p.identityMu.RUnlock()
	if fresh {
		return
	}
	self := p.contact.GetSelf()
	owner := p.contact.GetOwner()
	p.identityMu.Lock()
	if self != nil {
		p.self = proto.Clone(self).(*contact.SelfInfo)
	}
	if owner != nil && strings.TrimSpace(owner.GetUsername()) != "" {
		p.ownerID = strings.TrimSpace(owner.GetUsername())
		p.ownerName = displayContact(owner)
	}
	if self != nil || p.ownerID != "" {
		p.identityUpdatedAt = time.Now()
	}
	p.identityMu.Unlock()
}

func (p *PawzoChatPlugin) identityForEvent() (*contact.SelfInfo, string, string) {
	p.identityMu.RLock()
	fresh := p.self != nil && time.Since(p.identityUpdatedAt) < identityRefreshTTL
	p.identityMu.RUnlock()
	if !fresh {
		p.refreshIdentity()
	}
	p.identityMu.RLock()
	defer p.identityMu.RUnlock()
	if p.self == nil {
		return nil, p.ownerID, p.ownerName
	}
	return proto.Clone(p.self).(*contact.SelfInfo), p.ownerID, p.ownerName
}
