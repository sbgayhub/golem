package main

import (
	"encoding/json"
	"encoding/xml"
	"strings"
	"unicode"

	"github.com/sbgayhub/golem/sdk/contact"
	"github.com/sbgayhub/golem/sdk/message"
)

const contactTypeChatroom = contact.ContactType_CONTACT_TYPE_CHATROOM

// incomingMessage 解析后的入站消息
type incomingMessage struct {
	Receiver     *contact.Contact // 会话联系人（群或私聊对方），回复目标
	Text         string
	IsChatroom   bool
	SessionKey   string // chatroom:xxx / private:xxx
	ChatroomName string
	SpeakerName  string
	SpeakerID    string
	// SpeakerIsOwner 发言人是否为主人。由 OnEvent 按协议层 wxid 与
	// contact.GetOwner() 硬比对后写入，是上下文里【主人】标注的唯一来源。
	SpeakerIsOwner bool
	Quote          quoteInfo
	MentionedBot   bool
	QuotedBot      bool
}

// quoteInfo 引用消息信息
type quoteInfo struct {
	FromUser    string
	ChatUser    string
	DisplayName string
	Content     string
}

// buildIncoming 从原始消息构建入站消息（沿用 ai 插件的解析惯例）
func buildIncoming(msg *message.Message, self *contact.SelfInfo) (incomingMessage, bool) {
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
	in.MentionedBot = isMentionedBot(msg, self)
	in.QuotedBot = isQuotedBot(in.Quote, self)
	return in, true
}

// promptContent 格式化为喂给 agent 的结构化单条消息。
// 【主人】标注按协议层 wxid 判定打上，昵称改成什么都不影响。
func (in incomingMessage) promptContent() string {
	ownerMark := ""
	if in.SpeakerIsOwner {
		ownerMark = "【主人】"
	}
	var lines []string
	if in.IsChatroom {
		lines = append(lines,
			"[群聊]",
			"群聊: "+emptyDash(in.ChatroomName),
			"发言人: "+emptyDash(in.SpeakerName)+"("+emptyDash(in.SpeakerID)+")"+ownerMark,
		)
	} else {
		lines = append(lines,
			"[私聊]",
			"发言人: "+emptyDash(in.SpeakerName)+"("+emptyDash(in.SpeakerID)+")"+ownerMark,
		)
	}
	if in.Quote.Content != "" {
		lines = append(lines, "引用消息: "+in.Quote.Content)
	}
	lines = append(lines, "消息: "+in.Text)
	return strings.Join(lines, "\n")
}

func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
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

func isMentionedBot(msg *message.Message, self *contact.SelfInfo) bool {
	identities := selfIdentities(self)
	if len(identities) == 0 {
		return false
	}
	if text := msg.GetText(); text != nil {
		for _, remind := range text.GetReminds() {
			if reminderMentionsIdentity(remind, identities) {
				return true
			}
		}
	}
	content := messageContent(msg)
	for _, identity := range identities {
		if strings.Contains(content, "@"+identity) {
			return true
		}
	}
	return false
}

func isQuotedBot(quote quoteInfo, self *contact.SelfInfo) bool {
	identities := selfIdentities(self)
	if len(identities) == 0 {
		return false
	}
	for _, value := range []string{quote.FromUser, quote.ChatUser} {
		if containsIdentity(value, identities) {
			return true
		}
	}
	displayName := strings.TrimSpace(quote.DisplayName)
	for _, identity := range identities {
		if displayName == identity || strings.Contains(displayName, identity) {
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
	values := []string{self.GetUsername(), self.GetNickname(), self.GetAlias()}
	identities := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		identities = append(identities, value)
	}
	return identities
}

func containsIdentity(value string, identities []string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, identity := range identities {
		if value == identity {
			return true
		}
	}
	return false
}

func reminderMentionsIdentity(remind string, identities []string) bool {
	for _, part := range strings.FieldsFunc(remind, isReminderSeparator) {
		part = strings.TrimPrefix(strings.TrimSpace(part), "@")
		if containsIdentity(part, identities) {
			return true
		}
	}
	return false
}

func isReminderSeparator(r rune) bool {
	return unicode.IsSpace(r) || r == ',' || r == '，' || r == ';' || r == '；'
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
		if content := rawContentValue(raw); content != "" {
			if quote := parseQuoteXML(content); quote.hasValue() {
				return quote
			}
		}
	}
	return quoteInfo{}
}

func rawContentValue(raw string) string {
	var data struct {
		Content struct {
			Value string `json:"value"`
		} `json:"content"`
	}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return ""
	}
	return data.Content.Value
}

func parseQuoteXML(raw string) quoteInfo {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return quoteInfo{}
	}

	var data struct {
		AppMsg struct {
			Refer quoteRefer `xml:"refermsg"`
		} `xml:"appmsg"`
		Refer quoteRefer `xml:"refermsg"`
	}
	if err := xml.Unmarshal([]byte(raw), &data); err != nil {
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

func displayContact(c *contact.Contact) string {
	if c == nil {
		return ""
	}
	for _, value := range []string{c.GetRemark(), c.GetNickname(), c.GetAlias(), c.GetUsername()} {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func displayMember(member interface {
	GetDisplayName() string
	GetRemark() string
	GetNickname() string
	GetAlias() string
	GetUsername() string
}) string {
	if member == nil {
		return ""
	}
	for _, value := range []string{
		member.GetDisplayName(),
		member.GetRemark(),
		member.GetNickname(),
		member.GetAlias(),
		member.GetUsername(),
	} {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
