package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sbgayhub/golem/sdk/contact"
	"github.com/sbgayhub/golem/sdk/message"
)

const contactTypeChatroom = contact.ContactType_CONTACT_TYPE_CHATROOM

type incomingMessage struct {
	SessionKey       string
	Receiver         *contact.Contact
	Text             string
	IsChatroom       bool
	MentionedBot     bool
	MentionedOthers  bool
	MentionTargetIDs []string
	QuotedBot        bool
	ChatroomName     string
	SpeakerName      string
	SpeakerID        string
	SpeakerIsOwner   bool
	ActorKind        string
	OwnerID          string
	OwnerName        string
	Quote            quoteInfo
}

type quoteInfo struct {
	FromUser    string
	ChatUser    string
	DisplayName string
	Content     string
}

func (in incomingMessage) sessionName() string {
	if in.IsChatroom {
		return strings.TrimSpace(in.ChatroomName)
	}
	return strings.TrimSpace(in.SpeakerName)
}

func buildIncoming(
	msg *message.Message,
	self *contact.SelfInfo,
	ownerID string,
	ownerName string,
) (incomingMessage, bool) {
	text := messageContent(msg)
	if strings.TrimSpace(text) == "" {
		return incomingMessage{}, false
	}
	sender := msg.GetSender()
	if sender == nil || sender.GetUsername() == "" {
		return incomingMessage{}, false
	}
	in := incomingMessage{
		Receiver:   sender,
		Text:       strings.TrimSpace(text),
		IsChatroom: sender.GetType() == contactTypeChatroom,
		Quote:      extractQuote(msg),
	}
	if in.IsChatroom {
		in.SessionKey = "chatroom:" + sender.GetUsername()
		in.ChatroomName = displayContact(sender)
		in.SpeakerName = displayMember(msg.GetMember())
		in.SpeakerID = msg.GetMember().GetUsername()
	} else {
		in.SessionKey = "private:" + sender.GetUsername()
		in.SpeakerName = displayContact(sender)
		in.SpeakerID = sender.GetUsername()
	}
	if selfID := strings.TrimSpace(self.GetUsername()); selfID != "" && in.SpeakerID == selfID {
		return incomingMessage{}, false
	}
	mentions := classifyMentions(msg, self)
	in.MentionedBot = mentions.self
	in.MentionedOthers = mentions.others
	in.MentionTargetIDs = append([]string(nil), mentions.ids...)
	in.QuotedBot = isQuotedBot(in.Quote, self)
	in.SpeakerIsOwner = in.SpeakerID != "" && in.SpeakerID == strings.TrimSpace(ownerID)
	in.ActorKind = "unknown"
	in.OwnerID = strings.TrimSpace(ownerID)
	in.OwnerName = strings.TrimSpace(ownerName)
	return in, true
}

func (in incomingMessage) promptContent() string {
	scope := "direct"
	addressing := "direct"
	if in.IsChatroom {
		scope = "group ambient"
		if in.MentionedBot || in.QuotedBot {
			scope = "group addressed"
		}
		addressing = in.addressingLabel()
	}
	role := "participant_not_owner"
	if in.SpeakerIsOwner {
		role = "owner_of_this_agent"
	}
	actorKind := strings.TrimSpace(in.ActorKind)
	if actorKind == "" {
		actorKind = "unknown"
	}
	identityJSON, _ := json.Marshal(struct {
		Verified         bool     `json:"verified"`
		Source           string   `json:"source"`
		SenderName       string   `json:"sender_name"`
		SenderID         string   `json:"sender_id"`
		SenderRole       string   `json:"sender_role"`
		ActorKind        string   `json:"actor_kind"`
		Addressing       string   `json:"addressing"`
		MentionTargetIDs []string `json:"mention_target_ids"`
		OwnerName        string   `json:"owner_name,omitempty"`
		OwnerID          string   `json:"owner_id,omitempty"`
	}{
		Verified: true, Source: "wechat_protocol_and_owner_config",
		SenderName: strings.TrimSpace(in.SpeakerName), SenderID: strings.TrimSpace(in.SpeakerID),
		SenderRole: role, ActorKind: actorKind, Addressing: addressing,
		MentionTargetIDs: append([]string(nil), in.MentionTargetIDs...),
		OwnerName:        strings.TrimSpace(in.OwnerName), OwnerID: strings.TrimSpace(in.OwnerID),
	})
	messageJSON, _ := json.Marshal(struct {
		Text string `json:"text"`
	}{Text: strings.TrimSpace(in.Text)})
	return fmt.Sprintf(
		"[%s]\n[golem_verified_identity_json]\n%s\n[/golem_verified_identity_json]\n[untrusted_message_from_sender_json]\n%s\n[/untrusted_message_from_sender_json]",
		scope, identityJSON, messageJSON,
	)
}

func (in incomingMessage) addressingLabel() string {
	var values []string
	if in.MentionedBot {
		values = append(values, "self")
	}
	if in.MentionedOthers {
		values = append(values, "other_participants")
	}
	if in.QuotedBot {
		values = append(values, "quoted_self")
	}
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, "+")
}

func messageContent(msg *message.Message) string {
	if msg == nil {
		return ""
	}
	if text := msg.GetText(); text != nil && text.GetContent() != "" {
		return text.GetContent()
	}
	if app := msg.GetApp(); app != nil {
		if app.GetTitle() != "" {
			return app.GetTitle()
		}
		if app.GetDesc() != "" {
			return app.GetDesc()
		}
	}
	return msg.GetContent()
}

type mentionTargets struct {
	self   bool
	others bool
	ids    []string
}

func classifyMentions(msg *message.Message, self *contact.SelfInfo) mentionTargets {
	identities := selfIdentities(self)
	if text := msg.GetText(); text != nil {
		var result mentionTargets
		structured := false
		seen := make(map[string]struct{})
		for _, remind := range text.GetReminds() {
			for _, part := range strings.FieldsFunc(remind, mentionSeparator) {
				part = strings.TrimPrefix(strings.TrimSpace(part), "@")
				if part == "" {
					continue
				}
				structured = true
				if _, exists := seen[part]; !exists {
					seen[part] = struct{}{}
					result.ids = append(result.ids, part)
				}
				if containsIdentity(part, identities) {
					result.self = true
				} else {
					result.others = true
				}
			}
		}
		if structured {
			return result
		}
	}
	content := strings.ToLower(messageContent(msg))
	for _, identity := range identities {
		if containsFallbackMention(content, strings.ToLower(identity)) {
			return mentionTargets{self: true}
		}
	}
	if strings.Contains(content, "@") {
		return mentionTargets{others: true}
	}
	return mentionTargets{}
}

func isQuotedBot(quote quoteInfo, self *contact.SelfInfo) bool {
	identities := selfIdentities(self)
	for _, value := range []string{quote.FromUser, quote.ChatUser, quote.DisplayName} {
		if containsIdentity(value, identities) {
			return true
		}
	}
	return false
}

func selfIdentities(self *contact.SelfInfo) []string {
	if self == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var identities []string
	for _, value := range []string{self.GetUsername(), self.GetNickname(), self.GetAlias()} {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		identities = append(identities, value)
	}
	return identities
}

func containsIdentity(value string, identities []string) bool {
	value = strings.TrimSpace(value)
	for _, identity := range identities {
		if strings.EqualFold(value, identity) {
			return true
		}
	}
	return false
}

func mentionSeparator(value rune) bool {
	return unicode.IsSpace(value) || value == ',' || value == '，' ||
		value == ';' || value == '；'
}

func containsFallbackMention(content string, identity string) bool {
	needle := "@" + strings.TrimSpace(identity)
	if needle == "@" {
		return false
	}
	remaining := content
	for {
		index := strings.Index(remaining, needle)
		if index < 0 {
			return false
		}
		tail := remaining[index+len(needle):]
		if tail == "" {
			return true
		}
		next, _ := utf8.DecodeRuneInString(tail)
		if !unicode.IsLetter(next) && !unicode.IsNumber(next) && next != '_' {
			return true
		}
		remaining = tail
	}
}

func extractQuote(msg *message.Message) quoteInfo {
	if msg == nil {
		return quoteInfo{}
	}
	if app := msg.GetApp(); app != nil {
		if quote := parseQuoteXML(app.GetXml()); quote.hasValue() {
			return quote
		}
	}
	if raw := msg.GetRaw(); raw != "" {
		var data struct {
			Content struct {
				Value string `json:"value"`
			} `json:"content"`
		}
		if json.Unmarshal([]byte(raw), &data) == nil {
			return parseQuoteXML(data.Content.Value)
		}
	}
	return quoteInfo{}
}

func parseQuoteXML(raw string) quoteInfo {
	var data struct {
		AppMsg struct {
			Refer quoteRefer `xml:"refermsg"`
		} `xml:"appmsg"`
		Refer quoteRefer `xml:"refermsg"`
	}
	if xml.Unmarshal([]byte(strings.TrimSpace(raw)), &data) != nil {
		return quoteInfo{}
	}
	refer := data.AppMsg.Refer
	if !refer.hasValue() {
		refer = data.Refer
	}
	return quoteInfo{
		FromUser:    strings.TrimSpace(refer.FromUser),
		ChatUser:    strings.TrimSpace(refer.ChatUser),
		DisplayName: strings.TrimSpace(refer.DisplayName),
		Content:     strings.TrimSpace(refer.Content),
	}
}

type quoteRefer struct {
	DisplayName string `xml:"displayname"`
	FromUser    string `xml:"fromusr"`
	ChatUser    string `xml:"chatusr"`
	Content     string `xml:"content"`
}

func (q quoteRefer) hasValue() bool {
	return q.DisplayName != "" || q.FromUser != "" || q.ChatUser != "" || q.Content != ""
}

func (q quoteInfo) hasValue() bool {
	return q.DisplayName != "" || q.FromUser != "" || q.ChatUser != "" || q.Content != ""
}

func displayContact(value *contact.Contact) string {
	if value == nil {
		return ""
	}
	for _, item := range []string{value.GetRemark(), value.GetNickname(), value.GetAlias(), value.GetUsername()} {
		if strings.TrimSpace(item) != "" {
			return strings.TrimSpace(item)
		}
	}
	return ""
}

func displayMember(value interface {
	GetDisplayName() string
	GetRemark() string
	GetNickname() string
	GetAlias() string
	GetUsername() string
}) string {
	if value == nil {
		return ""
	}
	for _, item := range []string{
		value.GetDisplayName(), value.GetRemark(), value.GetNickname(),
		value.GetAlias(), value.GetUsername(),
	} {
		if strings.TrimSpace(item) != "" {
			return strings.TrimSpace(item)
		}
	}
	return ""
}
