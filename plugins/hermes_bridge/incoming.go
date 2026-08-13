package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"unicode"

	"github.com/sbgayhub/golem/sdk/contact"
	"github.com/sbgayhub/golem/sdk/message"
)

const contactTypeChatroom = contact.ContactType_CONTACT_TYPE_CHATROOM

// incomingMessage 解析后的入站消息
type incomingMessage struct {
	Receiver       *contact.Contact
	Text           string
	IsChatroom     bool
	SessionKey     string
	ChatroomName   string
	SpeakerName    string
	SpeakerID      string
	SpeakerIsOwner bool
	// MsgID 本条消息 new_id（host factory: Message.Id = sync new_id）。
	// 字符串形式，避免 JSON 大整数精度问题；出站引用 svrid 用此值。
	MsgID          string
	Quote          quoteInfo
	MentionedBot   bool
	QuotedBot      bool
	MentionedOther bool
	QuotedOther    bool
	Addressing     string // self / quoted_self / other_participants / none
	TriggerReason  string // mention_self / quoted_self / trigger_name / bubble / none
	// 入站媒体引用（media_ref）：登记在桥内存不预下载；
	// agent 需要看图时适配器经 /media?ref= 按需取回（见 mediaref.go）。
	MediaRef string
	// 表情消息附加信息：md5 供收藏判重（同 md5 = 同表情），desc 是表情描述（如「[捂脸]」）。
	// IsEmoji 含 XML 解析失败（无 md5）的表情消息，供斗图门闩计数。
	EmojiMd5  string
	EmojiDesc string
	IsEmoji   bool
}

type quoteInfo struct {
	FromUser    string
	ChatUser    string
	DisplayName string
	Content     string
	// 被引用消息元数据（入站对方发的引用气泡里的 refermsg；出站引用本条用 MsgID 而非这些）
	Type       int    // 1=文本 3=图片
	SvrID      string // 被引用消息 new_id
	CreateTime int64  // 被引用消息 unix 秒
}

// bridgeEvent 推给 Hermes 适配器的 SSE JSON
type bridgeEvent struct {
	SessionKey string `json:"session_key"`
	ChatID     string `json:"chat_id"`
	ChatName   string `json:"chat_name,omitempty"`
	ChatType   string `json:"chat_type"` // group / private
	UserID     string `json:"user_id"`
	UserName   string `json:"user_name,omitempty"`
	IsOwner    bool   `json:"is_owner"`
	Text       string `json:"text"`
	QuoteText  string `json:"quote_text,omitempty"`
	// 本条消息 new_id，agent 出站 wechat_send_quote 时作 svrid
	MsgID string `json:"msg_id,omitempty"`
	// 若本条本身是引用气泡，附带 refer 元数据（可选；引用本条仍用 msg_id）
	QuoteSvrID       string `json:"quote_svrid,omitempty"`
	QuoteFromUsr     string `json:"quote_fromusr,omitempty"`
	QuoteType        int    `json:"quote_type,omitempty"`
	QuoteDisplayName string `json:"quote_displayname,omitempty"`
	MentionedBot     bool   `json:"mentioned_bot"`
	QuotedBot        bool   `json:"quoted_bot"`
	Addressing       string `json:"addressing,omitempty"`
	TriggerReason    string `json:"trigger_reason,omitempty"`
	Timestamp        int64  `json:"timestamp"`
	// 入站媒体引用；适配器提示 agent 需要时用 wechat_fetch_media 按 ref 取回本地文件
	MediaRef string `json:"media_ref,omitempty"`
	// 表情消息：md5 供收藏判重与去重，desc 是微信侧表情描述（发送者可控，不可信）
	EmojiMd5  string `json:"emoji_md5,omitempty"`
	EmojiDesc string `json:"emoji_desc,omitempty"`
}

func (e bridgeEvent) toJSON() ([]byte, error) {
	return json.Marshal(e)
}

func (p *BridgePlugin) buildIncoming(msg *message.Message, self *contact.SelfInfo) (incomingMessage, bool) {
	text := messageContent(msg)
	// 非文本消息用平台标记代替，后续走相同的触发/去抖/SSE 逻辑。
	// host 侧图片消息的 Content 常为 "[图片]" 或 "[0 x 0] 0B" 等占位描述，
	// 统一替换为 [图片]/[表情]/[语音]/[视频]，避免模型看到无意义描述。
	var mediaTags = map[int32]string{
		message.TypeImage.Code: "[图片]",
		message.TypeEmoji.Code: "[表情]",
		message.TypeVoice.Code: "[语音]",
		message.TypeVideo.Code: "[视频]",
	}
	var mediaRef string
	var emojiMd5, emojiDesc string
	var isEmoji bool
	if msg != nil && msg.GetType() != nil {
		if tag, ok := mediaTags[msg.GetType().GetCode()]; ok {
			isEmoji = msg.GetType().GetCode() == message.TypeEmoji.Code
			if emoji := msg.GetEmoji(); emoji != nil {
				// host 的 buildEmoji 把 Content 填成裸 md5 串，对模型无可读价值：
				// 文本统一为 [表情]，md5/desc 结构化放独立字段（供收藏判重/打标签）。
				text = tag
				emojiMd5 = strings.TrimSpace(emoji.GetMedia().GetMd5())
				emojiDesc = strings.TrimSpace(emoji.GetDesc())
			} else if strings.TrimSpace(text) == "" || strings.Contains(text, "[0 x 0]") {
				text = tag
			}
			// 只登记引用不下载；agent 对话中需要看图时才经 /media 按需取（见 mediaref.go）
			mediaRef = p.registerInboundMedia(msg)
			if mediaRef == "" && strings.Contains(text, "[图片]") {
				// 连引用都建不起来（XML 没解出 CDN 参数且无 ImgBuf），标一下方便 agent 知道
				text = "[图片(无法获取)]"
			}
		}
	}
	sender := msg.GetSender()
	if sender == nil || sender.GetUsername() == "" {
		slog.Warn("[hermes_bridge] buildIncoming sender 无效", "type", fmt.Sprint(msg.GetType()), "content", text[:min(len(text), 60)])
		return incomingMessage{}, false
	}

	quote := extractQuote(msg)
	// 引用气泡 title 可能为空：仍要入站，否则 SSE 全无、群也触发不了 quoted_self。
	text = strings.TrimSpace(text)
	if text == "" {
		if quote.hasValue() || isAppQuoteMsg(msg) {
			text = "[引用]"
		} else {
			return incomingMessage{}, false
		}
	}

	in := incomingMessage{
		Receiver:   sender,
		Text:       text,
		IsChatroom: sender.GetType() == contactTypeChatroom,
		MsgID:      formatMsgID(msg.GetId()),
		Quote:      quote,
		MediaRef:   mediaRef,
		EmojiMd5:   emojiMd5,
		EmojiDesc:  emojiDesc,
		IsEmoji:    isEmoji,
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

	// 引用判定：引用气泡是 AppMsg(type=57)，不是 TypeText。
	// 旧逻辑在「非文本」处提前 return，导致 QuotedBot 永远 false，
	// 群里「引用机器人」无法 quoted_self 触发 → 整条不推 SSE。
	in.QuotedBot = isQuotedBot(in.Quote, self)
	in.QuotedOther = in.Quote.hasValue() && !in.QuotedBot

	// @ 判定只对文本（Reminds 仅 TypeText）。
	if msg.GetType() != nil && msg.GetType().GetCode() == message.TypeText.Code {
		in.MentionedBot = isMentionedBot(msg, self)
		// TextData.Reminds 是微信真实 @ 的 wxid 列表。若明确 @ 了非本机器人，
		// 标为 other_participants，避免 agent 把别人 bot 的任务当作自己的。
		in.MentionedOther = isMentionedOther(msg, self)
	}
	return in, true
}

func isAppQuoteMsg(msg *message.Message) bool {
	if msg == nil || msg.GetType() == nil {
		return false
	}
	return msg.GetType().GetCode() == message.TypeAppQuote.Code
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

// imageCDNInfo 入站图片 XML 里的 CDN 下载参数。
type imageCDNInfo struct {
	AesKey      string // aeskey：中图/原图共用密钥（hex）
	MidURL      string // cdnmidimgurl：中图 file_id，通常几十~几百 KB
	BigURL      string // cdnbigimgurl：原图 file_id，可能为空
	ThumbURL    string // cdnthumburl：缩略图 file_id
	ThumbAesKey string // cdnthumbaeskey：缩略图密钥，缺省时与 aeskey 相同
}

func (i imageCDNInfo) thumbKey() string {
	if i.ThumbAesKey != "" {
		return i.ThumbAesKey
	}
	return i.AesKey
}

// parseImageCDNInfo 解析原始图片 XML（Raw 的 content.value）。
// 群聊消息正文带 "wxid:\n" 前缀，xml 解码器会跳过根元素前的杂散文本，不必剥离。
// 标签名两种都见过：标准微信是 <msg><img .../>，本后端实测是 <msg><imgmsg .../>。
func parseImageCDNInfo(rawXML string) imageCDNInfo {
	rawXML = strings.TrimSpace(rawXML)
	if rawXML == "" {
		return imageCDNInfo{}
	}
	type imgAttrs struct {
		AesKey      string `xml:"aeskey,attr"`
		MidURL      string `xml:"cdnmidimgurl,attr"`
		BigURL      string `xml:"cdnbigimgurl,attr"`
		ThumbURL    string `xml:"cdnthumburl,attr"`
		ThumbAesKey string `xml:"cdnthumbaeskey,attr"`
	}
	var temp struct {
		XMLName xml.Name `xml:"msg"`
		Img     imgAttrs `xml:"img"`
		ImgMsg  imgAttrs `xml:"imgmsg"`
	}
	if err := xml.Unmarshal([]byte(rawXML), &temp); err != nil {
		slog.Warn("[hermes_bridge] 解析入站图片 XML 失败", "err", err)
		return imageCDNInfo{}
	}
	attrs := temp.Img
	if attrs.AesKey == "" && attrs.MidURL == "" && attrs.ThumbURL == "" {
		attrs = temp.ImgMsg
	}
	return imageCDNInfo{
		AesKey:      strings.TrimSpace(attrs.AesKey),
		MidURL:      strings.TrimSpace(attrs.MidURL),
		BigURL:      strings.TrimSpace(attrs.BigURL),
		ThumbURL:    strings.TrimSpace(attrs.ThumbURL),
		ThumbAesKey: strings.TrimSpace(attrs.ThumbAesKey),
	}
}

// rawImageBuffer 取 sync NewMessage 自带的 ImgBuf 缩略图。
// factory 把整个 NewMessage JSON 进了 Raw，proto bytes 字段经 encoding/json
// 序列化成 base64 字符串，这里反序列化回 []byte 自动完成解码。
func rawImageBuffer(raw string) []byte {
	if raw == "" {
		return nil
	}
	var data struct {
		ImageBuffer struct {
			Data []byte `json:"data"`
		} `json:"image_buffer"`
	}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil
	}
	return data.ImageBuffer.Data
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

// isMentionedOther 判断消息是否真实 @ 了本机器人以外的成员。
// 优先信任 TextData.Reminds。微信偶尔缺 Reminds 时，只接受符合群聊点名形态的
// @昵称 作为兜底，不能把邮箱、代码或任意正文中的 @ 都误判成提及他人。
func isMentionedOther(msg *message.Message, self *contact.SelfInfo) bool {
	if msg == nil {
		return false
	}
	identities := selfIdentities(self)
	if text := msg.GetText(); text != nil {
		for _, remind := range text.GetReminds() {
			for _, part := range strings.FieldsFunc(remind, isReminderSeparator) {
				part = strings.TrimPrefix(strings.TrimSpace(part), "@")
				if part != "" && !containsIdentity(part, identities) {
					return true
				}
			}
		}
	}
	return !isMentionedBot(msg, self) && hasOtherMentionToken(messageContent(msg))
}

// hasOtherMentionToken 在 Reminds 缺失时识别可能的群聊 @昵称。
// 真 @ 的昵称末尾是 U+2005，展示名可含 ASCII 空格，因此优先找该特殊空格。
// 对手工输入的 "@昵称 内容"，仅接受消息开头或明确文本边界后的 @，避免把
// name@example.com、代码中的 foo@bar 等普通 @ 标为 other_participants。
func hasOtherMentionToken(content string) bool {
	runes := []rune(content)
	for i, r := range runes {
		if r != '@' || i+1 >= len(runes) || runes[i+1] == '@' || unicode.IsSpace(runes[i+1]) {
			continue
		}
		if hasMentionSpacerAfter(runes[i+1:]) {
			return true
		}
		if (i == 0 || isMentionStartBoundary(runes[i-1])) && hasPlainMentionToken(runes[i+1:]) {
			return true
		}
	}
	return false
}

func hasMentionSpacerAfter(runes []rune) bool {
	const maxMentionRunes = 64
	for i, r := range runes {
		if i >= maxMentionRunes || r == '\n' || r == '\r' || r == '@' {
			return false
		}
		if r == '\u2005' {
			return i > 0
		}
	}
	return false
}

func hasPlainMentionToken(runes []rune) bool {
	for _, r := range runes {
		if unicode.IsSpace(r) {
			return true
		}
		if r == '@' {
			return false
		}
	}
	return len(runes) > 0
}

func isMentionStartBoundary(r rune) bool {
	if unicode.IsSpace(r) {
		return true
	}
	return strings.ContainsRune("([{\"'，；：！？、】【《“‘", r)
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
	ct := int64(0)
	if t := strings.TrimSpace(refer.CreateTime); t != "" {
		if v, err := strconv.ParseInt(t, 10, 64); err == nil {
			ct = v
		}
	}
	qt := 0
	if t := strings.TrimSpace(refer.Type); t != "" {
		if v, err := strconv.Atoi(t); err == nil {
			qt = v
		}
	}
	return quoteInfo{
		FromUser:    strings.TrimSpace(refer.FromUser),
		ChatUser:    strings.TrimSpace(refer.ChatUser),
		DisplayName: strings.TrimSpace(refer.DisplayName),
		Content:     strings.TrimSpace(refer.Content),
		Type:        qt,
		SvrID:       strings.TrimSpace(refer.SvrID),
		CreateTime:  ct,
	}
}

type quoteRefer struct {
	DisplayName string `xml:"displayname"`
	FromUser    string `xml:"fromusr"`
	ChatUser    string `xml:"chatusr"`
	Content     string `xml:"content"`
	Type        string `xml:"type"`
	SvrID       string `xml:"svrid"`
	CreateTime  string `xml:"createtime"`
}

func (q quoteRefer) hasValue() bool {
	return q.DisplayName != "" || q.FromUser != "" || q.ChatUser != "" || q.Content != "" ||
		q.SvrID != ""
}

func (q quoteInfo) hasValue() bool {
	return q.DisplayName != "" || q.FromUser != "" || q.ChatUser != "" || q.Content != "" ||
		q.SvrID != ""
}

// formatMsgID 把 Message.Id（new_id）编成字符串；0 返空（omit）。
func formatMsgID(id int64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}

const maxQuoteSummaryRunes = 200

// quoteBodySummary 被引用内容的人读摘要：引图/内嵌 img XML → [图片]；文本截断。
// 不含 displayname 前缀（由 quoteSummary 组合）。
func quoteBodySummary(q quoteInfo) string {
	if q.Type == 3 {
		return "[图片]"
	}
	c := strings.TrimSpace(q.Content)
	if c == "" {
		if q.Type != 0 || q.SvrID != "" {
			return "[引用内容]"
		}
		return ""
	}
	lower := strings.ToLower(c)
	// 真机引图 content 为转义或未转义的 <msg><img …>
	if strings.Contains(lower, "<img") || strings.Contains(lower, "&lt;img") ||
		strings.Contains(lower, "<imgmsg") || strings.Contains(lower, "&lt;imgmsg") ||
		(strings.Contains(lower, "<msg") && strings.Contains(lower, "cdn")) {
		return "[图片]"
	}
	return truncateRunes(c, maxQuoteSummaryRunes)
}

// quoteSummary 给 agent / SSE 的被引用摘要：「名: 正文」或「[图片]」。
func quoteSummary(q quoteInfo) string {
	if !q.hasValue() {
		return ""
	}
	body := quoteBodySummary(q)
	if body == "" {
		return ""
	}
	name := strings.TrimSpace(q.DisplayName)
	if name == "" {
		return body
	}
	// 已是「名: …」或纯占位时不再叠名
	if body == "[图片]" || body == "[引用内容]" {
		return name + ": " + body
	}
	return name + ": " + body
}

// applyQuoteMeta 把入站引用元数据填到 SSE 事件（有值才写）。
// QuoteText 用 summary（非 raw XML），避免引图把 img 整段甩给 agent。
func applyQuoteMeta(ev *bridgeEvent, q quoteInfo) {
	if !q.hasValue() {
		return
	}
	if s := quoteSummary(q); s != "" {
		ev.QuoteText = s
	}
	if q.SvrID != "" {
		ev.QuoteSvrID = q.SvrID
	}
	if q.FromUser != "" {
		ev.QuoteFromUsr = q.FromUser
	}
	if q.Type != 0 {
		ev.QuoteType = q.Type
	}
	if dn := strings.TrimSpace(q.DisplayName); dn != "" {
		ev.QuoteDisplayName = dn
	}
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

func isChatroomID(id string) bool {
	return strings.HasSuffix(id, "@chatroom")
}
