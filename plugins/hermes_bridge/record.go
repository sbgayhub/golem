package main

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// recordItem 聊天记录卡片里的一条伪消息（对齐 meme list / /pm list / 真机 type=19）。
// Kind 空或 text = 文本（datatype=1）；image = 图片（datatype=2，须填 CDN 元数据）。
type recordItem struct {
	Kind    string // text | image
	Name    string // 展示名（sourcename）
	Content string // 文本正文（datadesc）；图片默认 [图片]
	Avatar  string // 头像 URL，可空
	Time    string // 展示时间，可空则用当前时间

	// 图片 CDN（真机 datatype=2；见 t-doc/wechat-msg-formats.md）
	DataURL   string // cdndataurl
	DataKey   string // cdndatakey
	FullMD5   string // fullmd5
	DataSize  uint32 // datasize
	DataFmt   string // datafmt：jpg/png/…
	ThumbURL  string // cdnthumburl
	ThumbKey  string // cdnthumbkey
	ThumbMD5  string // thumbfullmd5
	ThumbSize uint32 // thumbsize
}

const (
	maxRecordItems        = 50
	maxRecordTitleRunes   = 64
	maxRecordDescRunes    = 128
	maxRecordNameRunes    = 32
	maxRecordContentRunes = 2000
	recordKindText        = "text"
	recordKindImage       = "image"
)

// buildChatRecordXML 构造 AppMsg type=19 聊天记录卡片 XML。
// 文本对齐 pm/meme；图片字段对齐 2026-08-04 真机 dump（datatype=2）。
func buildChatRecordXML(title, desc string, items []recordItem, defaultAvatar string) string {
	title = strings.TrimSpace(title)
	desc = strings.TrimSpace(desc)
	if title == "" {
		title = "聊天记录"
	}
	if desc == "" {
		desc = fmt.Sprintf("共%d条消息", len(items))
	}

	var inner strings.Builder
	inner.WriteString("<![CDATA[<recordinfo>\n")
	inner.WriteString(fmt.Sprintf("<title>%s</title>\n", escapeXML(title)))
	inner.WriteString(fmt.Sprintf("<desc>%s</desc>\n", escapeXML(desc)))
	inner.WriteString(fmt.Sprintf("<datalist count=\"%d\">\n", len(items)))

	now := time.Now()
	for i, item := range items {
		createAt := now.Add(time.Duration(i) * time.Second)
		inner.WriteString(buildRecordDataItem(item, defaultAvatar, createAt, i))
		inner.WriteString("\n")
	}
	inner.WriteString("</datalist></recordinfo>]]>")

	// 出站必须顶层 <appmsg>（与 meme / pm / statistics 一致）。
	// 真机入站是 <msg><appmsg>…，原样给 SendApp 会 code=-2 ARG。
	return fmt.Sprintf(
		`<appmsg appid="" sdkver="0">`+
			`<title>%s</title>`+
			`<des>%s</des>`+
			`<action>view</action>`+
			`<type>19</type>`+
			`<url>https://support.weixin.qq.com/cgi-bin/mmsupport-bin/readtemplate?t=page/favorite_record__w_unsupport&amp;from=singlemessage&amp;isappinstalled=0</url>`+
			`<recorditem>%s</recorditem>`+
			`</appmsg>`,
		escapeXML(title),
		escapeXML(desc),
		inner.String(),
	)
}

func buildRecordDataItem(item recordItem, defaultAvatar string, createAt time.Time, idx int) string {
	name := strings.TrimSpace(item.Name)
	if name == "" {
		name = "消息"
	}
	avatar := strings.TrimSpace(item.Avatar)
	if avatar == "" {
		avatar = defaultAvatar
	}
	timeStr := strings.TrimSpace(item.Time)
	if timeStr == "" {
		timeStr = createAt.Format(time.DateTime)
	}
	dataID := recordDataID(item, idx, createAt)
	kind := strings.ToLower(strings.TrimSpace(item.Kind))
	if kind == "" {
		kind = recordKindText
	}

	var b strings.Builder
	if kind == recordKindImage {
		desc := strings.TrimSpace(item.Content)
		if desc == "" {
			desc = "[图片]"
		}
		fmtStr := strings.TrimSpace(item.DataFmt)
		if fmtStr == "" {
			fmtStr = "jpg"
		}
		thumbURL := strings.TrimSpace(item.ThumbURL)
		if thumbURL == "" {
			thumbURL = strings.TrimSpace(item.DataURL)
		}
		thumbKey := strings.TrimSpace(item.ThumbKey)
		if thumbKey == "" {
			thumbKey = strings.TrimSpace(item.DataKey)
		}
		thumbMD5 := strings.TrimSpace(item.ThumbMD5)
		if thumbMD5 == "" {
			thumbMD5 = strings.TrimSpace(item.FullMD5)
		}
		thumbSize := item.ThumbSize
		if thumbSize == 0 {
			thumbSize = item.DataSize
		}
		b.WriteString(fmt.Sprintf(
			`<dataitem datatype="2" dataid="%s">`+
				`<datadesc>%s</datadesc>`+
				`<cdnthumburl>%s</cdnthumburl>`+
				`<cdnthumbkey>%s</cdnthumbkey>`+
				`<thumbfullmd5>%s</thumbfullmd5>`+
				`<thumbsize>%d</thumbsize>`+
				`<cdndataurl>%s</cdndataurl>`+
				`<cdndatakey>%s</cdndatakey>`+
				`<fullmd5>%s</fullmd5>`+
				`<datasize>%d</datasize>`+
				`<datafmt>%s</datafmt>`+
				`<sourcename>%s</sourcename>`+
				`<sourceheadurl>%s</sourceheadurl>`+
				`<sourcetime>%s</sourcetime>`+
				`<srcMsgCreateTime>%d</srcMsgCreateTime>`+
				`<fromnewmsgid>%d</fromnewmsgid>`+
				`<thumbfiletype>1</thumbfiletype>`+
				`<filetype>1</filetype>`+
				`</dataitem>`,
			escapeXML(dataID),
			escapeXML(desc),
			escapeXML(thumbURL),
			escapeXML(thumbKey),
			escapeXML(thumbMD5),
			thumbSize,
			escapeXML(strings.TrimSpace(item.DataURL)),
			escapeXML(strings.TrimSpace(item.DataKey)),
			escapeXML(strings.TrimSpace(item.FullMD5)),
			item.DataSize,
			escapeXML(fmtStr),
			escapeXML(name),
			escapeXML(avatar),
			escapeXML(timeStr),
			createAt.Unix(),
			createAt.UnixNano(),
		))
		return b.String()
	}

	// 文本
	content := strings.TrimSpace(item.Content)
	b.WriteString(fmt.Sprintf(
		`<dataitem datatype="1" dataid="%s" htmlid="">`+
			`<sourcename>%s</sourcename>`+
			`<datadesc>%s</datadesc>`+
			`<sourceheadurl>%s</sourceheadurl>`+
			`<sourcetime>%s</sourcetime>`+
			`<srcMsgLocalid></srcMsgLocalid>`+
			`<srcMsgCreateTime>%d</srcMsgCreateTime>`+
			`<fromnewmsgid>%d</fromnewmsgid>`+
			`</dataitem>`,
		escapeXML(dataID),
		escapeXML(name),
		escapeXML(content),
		escapeXML(avatar),
		escapeXML(timeStr),
		createAt.Unix(),
		createAt.UnixNano(),
	))
	return b.String()
}

func recordDataID(item recordItem, idx int, t time.Time) string {
	sum := md5.Sum([]byte(fmt.Sprintf("%s|%s|%s|%d|%d",
		item.Kind, item.Name, item.Content+item.FullMD5+item.DataURL, idx, t.UnixNano())))
	return hex.EncodeToString(sum[:])
}

func escapeXML(value string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(value)); err != nil {
		return value
	}
	return buf.String()
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// normalizeRecordItems 清洗条目：文本须有 content；图片须有 CDN 元数据（由调用方先 resolve）。
func normalizeRecordItems(raw []recordItem) []recordItem {
	out := make([]recordItem, 0, len(raw))
	for _, it := range raw {
		kind := strings.ToLower(strings.TrimSpace(it.Kind))
		if kind == "" {
			kind = recordKindText
		}
		name := truncateRunes(strings.TrimSpace(it.Name), maxRecordNameRunes)
		if name == "" {
			name = "消息"
		}
		switch kind {
		case recordKindImage:
			if strings.TrimSpace(it.DataURL) == "" || strings.TrimSpace(it.DataKey) == "" ||
				strings.TrimSpace(it.FullMD5) == "" || it.DataSize == 0 {
				continue
			}
			content := truncateRunes(strings.TrimSpace(it.Content), maxRecordNameRunes)
			if content == "" {
				content = "[图片]"
			}
			fmtStr := strings.ToLower(strings.TrimSpace(it.DataFmt))
			if fmtStr == "" {
				fmtStr = "jpg"
			}
			out = append(out, recordItem{
				Kind:      recordKindImage,
				Name:      name,
				Content:   content,
				Avatar:    strings.TrimSpace(it.Avatar),
				Time:      strings.TrimSpace(it.Time),
				DataURL:   strings.TrimSpace(it.DataURL),
				DataKey:   strings.TrimSpace(it.DataKey),
				FullMD5:   strings.TrimSpace(it.FullMD5),
				DataSize:  it.DataSize,
				DataFmt:   fmtStr,
				ThumbURL:  strings.TrimSpace(it.ThumbURL),
				ThumbKey:  strings.TrimSpace(it.ThumbKey),
				ThumbMD5:  strings.TrimSpace(it.ThumbMD5),
				ThumbSize: it.ThumbSize,
			})
		default:
			content := truncateRunes(strings.TrimSpace(it.Content), maxRecordContentRunes)
			if content == "" {
				continue
			}
			out = append(out, recordItem{
				Kind:    recordKindText,
				Name:    name,
				Content: content,
				Avatar:  strings.TrimSpace(it.Avatar),
				Time:    strings.TrimSpace(it.Time),
			})
		}
		if len(out) >= maxRecordItems {
			break
		}
	}
	return out
}

// parseRecordItemsFromMap 兼容 { "名字": "内容" }（仅文本，顺序不保证）。
func parseRecordItemsFromMap(m map[string]string) []recordItem {
	if len(m) == 0 {
		return nil
	}
	out := make([]recordItem, 0, len(m))
	for k, v := range m {
		out = append(out, recordItem{Kind: recordKindText, Name: k, Content: v})
	}
	return out
}

// parseRecordItemsFromLines 解析 "名字:内容" / "名字：内容" 行列表（仅文本）。
func parseRecordItemsFromLines(lines []string) []recordItem {
	out := make([]recordItem, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, content, ok := splitNameContent(line)
		if !ok {
			out = append(out, recordItem{
				Kind:    recordKindText,
				Name:    "消息" + strconv.Itoa(len(out)+1),
				Content: line,
			})
			continue
		}
		out = append(out, recordItem{Kind: recordKindText, Name: name, Content: content})
	}
	return out
}

func splitNameContent(line string) (name, content string, ok bool) {
	idx := strings.Index(line, ":")
	sepLen := 1
	if idx < 0 {
		idx = strings.Index(line, "：")
		sepLen = len("：")
	}
	if idx < 0 {
		return "", "", false
	}
	name = strings.TrimSpace(line[:idx])
	content = strings.TrimSpace(line[idx+sepLen:])
	if name == "" || content == "" {
		return "", "", false
	}
	return name, content, true
}

func sniffImageFmt(data []byte) string {
	if len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff {
		return "jpg"
	}
	if len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}) {
		return "png"
	}
	if len(data) >= 6 && (bytes.Equal(data[:6], []byte("GIF87a")) || bytes.Equal(data[:6], []byte("GIF89a"))) {
		return "gif"
	}
	if len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")) {
		return "webp"
	}
	return "jpg"
}

func md5Hex(data []byte) string {
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])
}

// isRecordImagePlaceholder 模型常把图片错写成纯文本 content="[图片]"。
func isRecordImagePlaceholder(s string) bool {
	switch strings.TrimSpace(s) {
	case "[图片]", "［图片］", "[image]", "[Image]", "图片":
		return true
	default:
		return false
	}
}
