package main

import (
	"fmt"
	"strings"
	"time"
)

// quoteOutbound 出站引用（AppMsg type=57）语义字段。
// 一期仅文本 refer type=1；外层顶层 <appmsg>（勿包 <msg>，见 type=19 教训）。
type quoteOutbound struct {
	Reply       string // appmsg/title：自己的回复
	SvrID       string // refermsg/svrid：被引用消息 new_id
	FromUsr     string // 被引用发送者 wxid
	ChatUsr     string // 会话相关 wxid
	DisplayName string // 被引用展示名
	Content     string // 被引用文本摘要
	QuoteType   int    // 1=文本；一期仅 1
	CreateTime  int64  // 被引用消息 unix 秒；0 则现填
}

const (
	maxQuoteReplyRunes   = 500
	maxQuoteContentRunes = 2000
)

// buildQuoteXML 构造 AppMsg type=57 引用卡片 XML（顶层仅 <appmsg>）。
// 字段与转义对齐 t-doc/wechat-msg-formats.md §三；省略真机噪音块。
func buildQuoteXML(q quoteOutbound) (string, error) {
	reply := truncateRunes(strings.TrimSpace(q.Reply), maxQuoteReplyRunes)
	svrid := strings.TrimSpace(q.SvrID)
	fromusr := strings.TrimSpace(q.FromUsr)
	chatusr := strings.TrimSpace(q.ChatUsr)
	displayname := truncateRunes(strings.TrimSpace(q.DisplayName), maxRecordNameRunes)
	content := truncateRunes(strings.TrimSpace(q.Content), maxQuoteContentRunes)

	if reply == "" {
		return "", fmt.Errorf("reply 必填")
	}
	if svrid == "" {
		return "", fmt.Errorf("svrid 必填（被引用消息 new_id）")
	}
	if fromusr == "" {
		return "", fmt.Errorf("fromusr 必填")
	}
	if content == "" {
		return "", fmt.Errorf("quote_content 必填")
	}

	qt := q.QuoteType
	if qt == 0 {
		qt = 1
	}
	if qt != 1 {
		return "", fmt.Errorf("一期仅支持 quote_type=1（文本引用）；图片引用二期")
	}
	if chatusr == "" {
		chatusr = fromusr
	}
	if displayname == "" {
		displayname = fromusr
	}
	ct := q.CreateTime
	if ct <= 0 {
		ct = time.Now().Unix()
	}

	// 出站必须顶层 <appmsg>（与 music / record 一致）。
	// 真机入站是 <msg><appmsg>…，原样给 SendApp 会 code=-2 ARG。
	return fmt.Sprintf(
		`<appmsg appid="" sdkver="0">`+
			`<title>%s</title>`+
			`<action>view</action>`+
			`<type>57</type>`+
			`<refermsg>`+
			`<type>%d</type>`+
			`<svrid>%s</svrid>`+
			`<fromusr>%s</fromusr>`+
			`<chatusr>%s</chatusr>`+
			`<displayname>%s</displayname>`+
			`<content>%s</content>`+
			`<createtime>%d</createtime>`+
			`</refermsg>`+
			`</appmsg>`,
		escapeXML(reply),
		qt,
		escapeXML(svrid),
		escapeXML(fromusr),
		escapeXML(chatusr),
		escapeXML(displayname),
		escapeXML(content),
		ct,
	), nil
}

// defaultQuoteChatUsr 未传 chatusr 时的默认：
// 私聊 = chat_id（对端）；群 = fromusr（成员）。均可被请求覆盖。
func defaultQuoteChatUsr(chatID, fromUsr string) string {
	chatID = strings.TrimSpace(chatID)
	fromUsr = strings.TrimSpace(fromUsr)
	if isChatroomID(chatID) {
		if fromUsr != "" {
			return fromUsr
		}
		return chatID
	}
	if chatID != "" {
		return chatID
	}
	return fromUsr
}
