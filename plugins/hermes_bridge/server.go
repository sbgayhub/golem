package main

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ---- SSE hub ----

type sseClient struct {
	ch     chan []byte
	closed atomic.Bool
}

type sseHub struct {
	mu      sync.Mutex
	clients map[*sseClient]struct{}
	// dropped 累计因客户端缓冲满而丢弃的事件数，供 /status 排查「偶发没反应」
	dropped atomic.Int64
}

func newSSEHub() *sseHub {
	return &sseHub{clients: map[*sseClient]struct{}{}}
}

func (h *sseHub) subscribe() *sseClient {
	c := &sseClient{ch: make(chan []byte, 32)}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	return c
}

func (h *sseHub) unsubscribe(c *sseClient) {
	if c == nil {
		return
	}
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		c.closed.Store(true)
		close(c.ch)
	}
	h.mu.Unlock()
}

func (h *sseHub) subscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// broadcast 非阻塞 fan-out；慢客户端丢该条（Hermes 停掉时本就允许丢消息）
func (h *sseHub) broadcast(data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c.ch <- data:
		default:
			h.dropped.Add(1)
			slog.Warn("[hermes_bridge] SSE 客户端缓冲满，丢弃一条事件", "dropped_total", h.dropped.Load())
		}
	}
}

func (h *sseHub) droppedCount() int64 { return h.dropped.Load() }

// ---- HTTP 生命周期 ----

func (p *BridgePlugin) startHTTP() error {
	p.srvMu.Lock()
	defer p.srvMu.Unlock()
	if p.httpSrv != nil {
		return nil
	}
	cfg := p.configSnapshot()
	if strings.TrimSpace(cfg.Listen) == "" {
		return errors.New("未配置 listen")
	}

	maxBody := cfg.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = 80 << 20
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", p.handleHealth)
	mux.Handle("/events", p.authMiddleware(http.HandlerFunc(p.handleEvents)))
	mux.Handle("/send", p.authMiddleware(p.limitBody(maxBody, http.HandlerFunc(p.handleSend))))
	mux.Handle("/send_image", p.authMiddleware(p.limitBody(maxBody, http.HandlerFunc(p.handleSendImage))))
	mux.Handle("/send_video", p.authMiddleware(p.limitBody(maxBody, http.HandlerFunc(p.handleSendVideo))))
	mux.Handle("/send_voice", p.authMiddleware(p.limitBody(maxBody, http.HandlerFunc(p.handleSendVoice))))
	mux.Handle("/send_emoji", p.authMiddleware(p.limitBody(maxBody, http.HandlerFunc(p.handleSendEmoji))))
	mux.Handle("/send_app", p.authMiddleware(p.limitBody(maxBody, http.HandlerFunc(p.handleSendApp))))
	// 聊天记录卡片（对齐 meme list / /pm list 的 type=19 AppMsg）
	mux.Handle("/send_record", p.authMiddleware(p.limitBody(maxBody, http.HandlerFunc(p.handleSendRecord))))
	// 引用回复（AppMsg type=57；桥拼 XML，一期仅文本 refer type=1）
	mux.Handle("/send_quote", p.authMiddleware(p.limitBody(maxBody, http.HandlerFunc(p.handleSendQuote))))
	mux.Handle("/status", p.authMiddleware(http.HandlerFunc(p.handleStatusAPI)))
	// 入站媒体按需取回：SSE 只带 media_ref，agent 要看图时才来取（此刻才下载）
	mux.Handle("/media", p.authMiddleware(http.HandlerFunc(p.handleFetchMedia)))
	// 查询：agent 先拿 wxid/群信息，再经 /send mentions 真 @
	mux.Handle("/self", p.authMiddleware(http.HandlerFunc(p.handleSelf)))
	mux.Handle("/group_info", p.authMiddleware(http.HandlerFunc(p.handleGroupInfo)))
	mux.Handle("/group_members", p.authMiddleware(http.HandlerFunc(p.handleGroupMembers)))
	mux.Handle("/group_member_detail", p.authMiddleware(p.limitBody(1<<20, http.HandlerFunc(p.handleGroupMemberDetail))))

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	p.httpSrv = srv

	go func() {
		slog.Info("[hermes_bridge] HTTP 桥监听中", "addr", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("[hermes_bridge] HTTP 桥异常退出", "err", err)
			p.srvMu.Lock()
			if p.httpSrv == srv {
				p.httpSrv = nil
			}
			p.srvMu.Unlock()
		}
	}()
	return nil
}

func (p *BridgePlugin) stopHTTP() {
	p.srvMu.Lock()
	srv := p.httpSrv
	p.httpSrv = nil
	p.srvMu.Unlock()
	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
}

func (p *BridgePlugin) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(p.configSnapshot().Token)
		if token == "" {
			http.Error(w, "token 未配置，服务不可用", http.StatusServiceUnavailable)
			return
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (p *BridgePlugin) limitBody(max int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, max)
		next.ServeHTTP(w, r)
	})
}

// ---- handlers ----

// handleHealth 探活；同时暴露桥的生效捷径词表与外部工具状态。
//
// 词表：适配器侧有同名 env，两边不一致时桥会先把消息吞在群门闩里，
// 适配器根本收不到（症状是「改了 env 没反应」）。适配器 connect 后拉一次做比对，
// 不一致就告警——让这类静默分叉可见。无鉴权路径，故只报词表不报 token / 白名单。
func (p *BridgePlugin) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"service":     "golem-hermes-bridge",
		"version":     p.GetMetadata().GetVersion(),
		"subscribers": p.hub.subscriberCount(),
		"tokens": map[string]any{
			"interrupt":     p.interruptTokens(),
			"session_reset": p.sessionResetTokens(),
			"archive":       p.archiveTokens(),
			"approval":      p.approvalTokens(),
		},
		"media_tools": p.mediaToolStatus(),
	})
}

func (p *BridgePlugin) handleStatusAPI(w http.ResponseWriter, _ *http.Request) {
	cfg := p.configSnapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"subscribers": p.hub.subscriberCount(),
		"sse_dropped": p.hub.droppedCount(),
		"targets":     len(cfg.Targets),
		"listen":      cfg.Listen,
	})
}

// handleFetchMedia GET /media?ref=media_N：按需取回入站媒体二进制。
// 404=引用不存在/过期；502=下载失败（CDN 与兜底均不可用）。
func (p *BridgePlugin) handleFetchMedia(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ref := strings.TrimSpace(r.URL.Query().Get("ref"))
	if ref == "" {
		http.Error(w, "ref 必填", http.StatusBadRequest)
		return
	}
	data, kind, err := p.fetchInboundMedia(ref)
	if err != nil {
		if errors.Is(err, errMediaRefNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		slog.Warn("[hermes_bridge] /media 取回失败", "ref", ref, "err", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Media-Kind", kind)
	_, _ = w.Write(data)
}

func (p *BridgePlugin) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	client := p.hub.subscribe()
	defer p.hub.unsubscribe(client)

	// 握手事件，便于适配器确认流已建立
	_, _ = fmt.Fprintf(w, "event: ready\ndata: {\"ok\":true}\n\n")
	flusher.Flush()

	// 心跳，防中间设备掐连接
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		case data, open := <-client.ch:
			if !open {
				return
			}
			_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// sendTextReq 文本发送
type sendTextReq struct {
	ChatID  string   `json:"chat_id"`
	Content string   `json:"content"`
	Mention []string `json:"mentions,omitempty"` // @ 的 wxid 列表（群聊）
}

type sendMediaReq struct {
	ChatID  string `json:"chat_id"`
	URL     string `json:"url,omitempty"`
	DataB64 string `json:"data_b64,omitempty"`
	Caption string `json:"caption,omitempty"`
	// Raw 仅表情生效：跳过压缩原样发送，保住动图与原 md5。
	// 收藏重发（微信里流通过的表情）必须 raw=true；任意网图仍走默认压缩。
	Raw bool `json:"raw,omitempty"`
	// Md5 仅表情生效：发送已收藏表情时仅传 md5 不传数据，微信用 CDN 原文件。
	// 与 url/data_b64 互斥——同时提供时忽略 md5，走数据上传路径。
	Md5 string `json:"md5,omitempty"`
}

type sendResult struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// sendAppReq 出站 AppMsg 卡片（音乐/链接/聊天记录等）：XML 与 sub_type 由适配器拼好，
// 桥只走 message.Send(TypeAppMusic, AppData)。caption 可选：成功后再补一条文本说明。
// appid 可选：留空且 XML 里写了 appid="" 时桥会随机回填一个（随机来源显示）；
// 传非空值则在 XML 里没有 appid="" 时会被忽略，避免不必要地修改调用方拼好的 XML。
type sendAppReq struct {
	ChatID  string `json:"chat_id"`
	SubType uint32 `json:"sub_type"`        // 微信 AppMsg 子类型，如 76=音乐、5=链接、19=聊天记录
	Xml     string `json:"xml"`             // 整段 <appmsg>…</appmsg>，桥原样透传给 host SendApp
	AppID   string `json:"appid,omitempty"` // 可选：调用方明确指定时优先；空则随机回填一个
	Caption string `json:"caption,omitempty"`
}

// sendRecordItem 聊天记录卡片条目（文本或图片）。
// type 空/text=文本（须 content）；image=图片（url 或 media_ref，不要 data_b64）。
type sendRecordItem struct {
	Type     string `json:"type,omitempty"`      // text | image
	Name     string `json:"name"`                // 展示名（卡片左侧）
	Content  string `json:"content,omitempty"`   // 文本正文；图片可写 [图片]
	Avatar   string `json:"avatar,omitempty"`    // 头像 URL，可空
	Time     string `json:"time,omitempty"`      // 展示时间，可空
	URL      string `json:"url,omitempty"`       // 图片 http(s)
	MediaRef string `json:"media_ref,omitempty"` // 入站 media_N，优先于 url
}

// sendRecordReq 出站「聊天记录」卡片（AppMsg type=19，可含 datatype=2 图片）。
// XML 由桥拼装；高级调用方仍可走 /send_app 自带 XML。
type sendRecordReq struct {
	ChatID  string            `json:"chat_id"`
	Title   string            `json:"title,omitempty"`
	Desc    string            `json:"desc,omitempty"`
	Items   []sendRecordItem  `json:"items,omitempty"`   // 优先：有序条目（可混 text/image）
	Records map[string]string `json:"records,omitempty"` // 兜底：{名字:内容} 仅文本
	Lines   []string          `json:"lines,omitempty"`   // 兜底："名字:内容" 仅文本
	Caption string            `json:"caption,omitempty"`
}

// sendQuoteReq 出站「引用回复」卡片（AppMsg type=57，一期仅文本 refer type=1）。
// XML 由桥拼装；svrid 用字符串防 JSON 大整数精度丢失。
// 字段形态对齐真机 dump。
type sendQuoteReq struct {
	ChatID      string `json:"chat_id"`
	Reply       string `json:"reply"`                 // 自己的回复 → appmsg/title
	SvrID       string `json:"svrid"`                 // 被引用消息 new_id
	FromUsr     string `json:"fromusr"`               // 被引用发送者 wxid
	ChatUsr     string `json:"chatusr,omitempty"`     // 会话相关；空则按私聊/群默认
	DisplayName string `json:"displayname,omitempty"` // 被引用展示名
	QuoteType   int    `json:"quote_type,omitempty"`  // 默认 1；一期仅 1
	Content     string `json:"quote_content"`         // 被引用文本
	CreateTime  int64  `json:"createtime,omitempty"`  // 0 → 现填
	Caption     string `json:"caption,omitempty"`
}

func (p *BridgePlugin) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req sendTextReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, sendResult{Error: "invalid json: " + err.Error()})
		return
	}
	req.ChatID = strings.TrimSpace(req.ChatID)
	req.Content = strings.TrimSpace(req.Content)
	if req.ChatID == "" || req.Content == "" {
		writeJSON(w, http.StatusBadRequest, sendResult{Error: "chat_id 与 content 必填"})
		return
	}
	if err := p.guardSendTarget(req.ChatID); err != nil {
		writeJSON(w, http.StatusForbidden, sendResult{Error: err.Error()})
		return
	}
	if !p.allowSend() {
		writeJSON(w, http.StatusTooManyRequests, sendResult{Error: "发送频率超限"})
		return
	}
	if p.message == nil {
		writeJSON(w, http.StatusServiceUnavailable, sendResult{Error: "消息能力未注入"})
		return
	}

	var reminds []string
	if isChatroomID(req.ChatID) && len(req.Mention) > 0 {
		// mentions 必须是真实 wxid；清洗去重后写入 Reminds
		reminds = sanitizeMentionWxids(req.Mention)
		// 真 @ 不只靠 Reminds：正文还须有 「@显示名 + 特殊空格(U+2005)」；缺则自动补全
		before := req.Content
		req.Content = ensureAtTokensInContent(req.Content, reminds, func(wxid string) string {
			return p.memberDisplayName(req.ChatID, wxid)
		})
		if req.Content != before {
			slog.Info("[hermes_bridge] 已补全正文@标记", "chat", req.ChatID, "mentions", len(reminds))
		}
	}
	maxLen := p.configSnapshot().MaxTextLen
	if maxLen > 0 && len([]rune(req.Content)) > maxLen {
		writeJSON(w, http.StatusBadRequest, sendResult{
			Error: fmt.Sprintf("内容超过 %d 字上限", maxLen),
		})
		return
	}
	if err := p.sendPlainTextWithReminds(req.ChatID, req.Content, reminds); err != nil {
		slog.Error("[hermes_bridge] 发文本失败", "chat", req.ChatID, "err", err, "mentions", len(reminds))
		writeJSON(w, http.StatusBadGateway, sendResult{Error: err.Error()})
		return
	}
	if len(reminds) > 0 {
		slog.Info("[hermes_bridge] 已发文本(含真@)", "chat", req.ChatID, "mentions", len(reminds))
	}
	writeJSON(w, http.StatusOK, sendResult{Success: true})
}

// mentionSpacer 微信真 @ 显示名后常用四分之一 em 空格（U+2005），不是普通 ASCII 空格。
const mentionSpacer = "\u2005"

// memberDisplayName 从群成员缓存取展示名；取不到返空串。
func (p *BridgePlugin) memberDisplayName(chatID, wxid string) string {
	wxid = strings.TrimSpace(wxid)
	if wxid == "" || p.chatroom == nil {
		return ""
	}
	if m := p.chatroom.GetMember(chatID, wxid); m != nil {
		return displayMember(m)
	}
	return ""
}

// ensureAtTokensInContent 保证每个 reminds 在正文里都有 「@名 + U+2005」。
// resolveName(wxid) 优先返回显示名；空则退回 wxid。
func ensureAtTokensInContent(content string, reminds []string, resolveName func(string) string) string {
	out := content
	for _, wxid := range reminds {
		wxid = strings.TrimSpace(wxid)
		if wxid == "" {
			continue
		}
		name := ""
		if resolveName != nil {
			name = strings.TrimSpace(resolveName(wxid))
		}
		if name == "" {
			name = wxid
		}
		out = ensureOneAtToken(out, name, wxid)
	}
	return out
}

func ensureOneAtToken(content, displayName, wxid string) string {
	// 已有正确形式 @名\u2005 或 @wxid\u2005
	if hasAtTokenWithSpacer(content, displayName) || hasAtTokenWithSpacer(content, wxid) {
		return content
	}
	// 已有 @名/@wxid 但间隔符不对：把后面的 ASCII 空白换成 U+2005；无间隔则插入
	for _, token := range []string{displayName, wxid} {
		if token == "" {
			continue
		}
		needle := "@" + token
		idx := strings.Index(content, needle)
		if idx < 0 {
			continue
		}
		end := idx + len(needle)
		runess := []rune(content[end:])
		if len(runess) == 0 {
			return content[:end] + mentionSpacer + content[end:]
		}
		switch runess[0] {
		case '\u2005':
			return content
		case ' ', '\t':
			// ASCII 空格 → 特殊空格
			return content[:end] + mentionSpacer + string(runess[1:])
		default:
			return content[:end] + mentionSpacer + content[end:]
		}
	}
	// 完全没有 @ 标记：前缀
	return "@" + displayName + mentionSpacer + content
}

func hasAtTokenWithSpacer(content, token string) bool {
	if token == "" {
		return false
	}
	return strings.Contains(content, "@"+token+mentionSpacer)
}

// handleSendRecord 出站聊天记录卡片（type=19；文本 datatype=1，图片 datatype=2）。
// 图片条目：产品仅 media_ref；url 重传仅 record_image_via 实验。
func (p *BridgePlugin) handleSendRecord(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req sendRecordReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, sendResult{Error: "invalid json: " + err.Error()})
		return
	}
	req.ChatID = strings.TrimSpace(req.ChatID)
	if req.ChatID == "" {
		writeJSON(w, http.StatusBadRequest, sendResult{Error: "chat_id 必填"})
		return
	}

	if err := p.guardSendTarget(req.ChatID); err != nil {
		writeJSON(w, http.StatusForbidden, sendResult{Error: err.Error()})
		return
	}
	if !p.allowSend() {
		writeJSON(w, http.StatusTooManyRequests, sendResult{Error: "发送频率超限"})
		return
	}
	if p.message == nil {
		writeJSON(w, http.StatusServiceUnavailable, sendResult{Error: "消息能力未注入"})
		return
	}

	var raw []recordItem
	for i, it := range req.Items {
		kind := strings.ToLower(strings.TrimSpace(it.Type))
		name := strings.TrimSpace(it.Name)
		avatar := strings.TrimSpace(it.Avatar)
		timeStr := strings.TrimSpace(it.Time)
		content := strings.TrimSpace(it.Content)
		mediaRef := strings.TrimSpace(it.MediaRef)
		imgURL := strings.TrimSpace(it.URL)
		// 无 type 时：有 url/media_ref → 图；content 仅为「[图片]」占位 → 当图（再校验源）
		if kind == "" || kind == "text" {
			if mediaRef != "" || imgURL != "" || isRecordImagePlaceholder(content) {
				kind = recordKindImage
			} else {
				kind = recordKindText
			}
		}
		switch kind {
		case recordKindImage, "img", "picture", "photo":
			if mediaRef == "" && imgURL == "" {
				slog.Warn("[hermes_bridge] 记录图片条目无 media_ref/url",
					"chat", req.ChatID, "index", i, "name", name, "content", content)
				writeJSON(w, http.StatusBadRequest, sendResult{
					Error: fmt.Sprintf(
						"第%d条图片需要 media_ref（会话内已出现的图）；网图请直接发图，勿只写 [图片]", i+1),
				})
				return
			}
			item, err := p.resolveRecordImageFromSource(req.ChatID, name, avatar, timeStr, content, mediaRef, imgURL)
			if err != nil {
				slog.Error("[hermes_bridge] 记录内图片解析失败",
					"chat", req.ChatID, "index", i, "err", err)
				writeJSON(w, http.StatusBadRequest, sendResult{
					Error: fmt.Sprintf("第%d条图片失败: %v", i+1, err),
				})
				return
			}
			raw = append(raw, item)
		default:
			if isRecordImagePlaceholder(content) {
				writeJSON(w, http.StatusBadRequest, sendResult{
					Error: fmt.Sprintf(
						"第%d条 content 为 [图片] 但 type 不是 image 且无 url/media_ref", i+1),
				})
				return
			}
			raw = append(raw, recordItem{
				Kind:    recordKindText,
				Name:    name,
				Content: content,
				Avatar:  avatar,
				Time:    timeStr,
			})
		}
	}
	if len(raw) == 0 && len(req.Lines) > 0 {
		raw = parseRecordItemsFromLines(req.Lines)
	}
	if len(raw) == 0 && len(req.Records) > 0 {
		raw = parseRecordItemsFromMap(req.Records)
	}
	items := normalizeRecordItems(raw)
	if len(items) == 0 {
		writeJSON(w, http.StatusBadRequest, sendResult{
			Error: "items 至少一条有效内容（文本 name+content；图片仅 media_ref，网图请直接发图）",
		})
		return
	}

	title := truncateRunes(strings.TrimSpace(req.Title), maxRecordTitleRunes)
	desc := truncateRunes(strings.TrimSpace(req.Desc), maxRecordDescRunes)
	if title == "" {
		title = fmt.Sprintf("聊天记录 共%d条", len(items))
	}
	if desc == "" {
		desc = fmt.Sprintf("共%d条消息", len(items))
	}

	defaultAvatar := ""
	if self := p.selfSnapshot(); self != nil {
		defaultAvatar = strings.TrimSpace(self.GetAvatar())
	}
	imgN := 0
	for i, it := range items {
		if it.Kind == recordKindImage {
			imgN++
			logRecordImageCDN("即将发送", req.ChatID, it, "index", i)
		}
	}
	xml := buildChatRecordXML(title, desc, items, defaultAvatar)
	// 诊断：落盘出站 XML（仅含图且配了 record_xml_dump_dir 时），对照手机「过期」与真机 dump
	if imgN > 0 {
		if path, err := dumpOutboundRecordXML(p.configSnapshot().RecordXMLDumpDir, xml); err != nil {
			slog.Warn("[hermes_bridge] 出站 record XML 落盘失败", "err", err)
		} else if path != "" {
			slog.Info("[hermes_bridge] 出站 record XML 已落盘", "path", path, "xml_len", len(xml))
		}
	}
	outcome, e := p.sendAppMessage(req.ChatID, 19, xml)
	if outcome != uploadOK {
		p.discardRecordImageRevokes(true)
		slog.Error("[hermes_bridge] 发聊天记录卡片失败",
			"chat", req.ChatID, "items", len(items), "images", imgN, "outcome", outcome, "err", e)
		msg := "发送失败"
		if e != nil {
			msg = e.Error()
		} else if outcome == uploadTimeout {
			msg = "发送超时（结果未确认，可能已送达）"
		}
		writeJSON(w, http.StatusBadGateway, sendResult{Error: msg})
		return
	}
	slog.Info("[hermes_bridge] 已发聊天记录卡片",
		"chat", req.ChatID, "items", len(items), "images", imgN, "title", title, "xml_len", len(xml))
	// via=send 时撤回为取 CDN 而发的临时图（默认开；诊断可 record_image_revoke=false）
	p.flushRecordImageRevokes()
	if cap := strings.TrimSpace(req.Caption); cap != "" {
		_ = p.sendPlainText(p.resolveReceiver(req.ChatID), cap)
	}
	writeJSON(w, http.StatusOK, sendResult{Success: true})
}

func (p *BridgePlugin) handleSendImage(w http.ResponseWriter, r *http.Request) {
	p.handleMedia(w, r, "image")
}

func (p *BridgePlugin) handleSendVideo(w http.ResponseWriter, r *http.Request) {
	p.handleMedia(w, r, "video")
}

func (p *BridgePlugin) handleSendVoice(w http.ResponseWriter, r *http.Request) {
	p.handleMedia(w, r, "voice")
}

func (p *BridgePlugin) handleSendEmoji(w http.ResponseWriter, r *http.Request) {
	p.handleMedia(w, r, "emoji")
}

// handleSendQuote 出站引用回复（type=57；一期文本 refer type=1）。
// 桥拼 XML 后 sendAppMessage；高级调用方仍可走 /send_app 自带 XML。
func (p *BridgePlugin) handleSendQuote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req sendQuoteReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, sendResult{Error: "invalid json: " + err.Error()})
		return
	}
	req.ChatID = strings.TrimSpace(req.ChatID)
	req.Reply = strings.TrimSpace(req.Reply)
	req.SvrID = strings.TrimSpace(req.SvrID)
	req.FromUsr = strings.TrimSpace(req.FromUsr)
	req.ChatUsr = strings.TrimSpace(req.ChatUsr)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.Content = strings.TrimSpace(req.Content)
	if req.ChatID == "" {
		writeJSON(w, http.StatusBadRequest, sendResult{Error: "chat_id 必填"})
		return
	}
	if req.Reply == "" {
		writeJSON(w, http.StatusBadRequest, sendResult{Error: "reply 必填（自己的回复正文）"})
		return
	}
	if req.SvrID == "" {
		writeJSON(w, http.StatusBadRequest, sendResult{Error: "svrid 必填（被引用消息 new_id / 入站 msg_id）"})
		return
	}
	if req.FromUsr == "" {
		writeJSON(w, http.StatusBadRequest, sendResult{Error: "fromusr 必填（被引用发送者 wxid）"})
		return
	}
	if req.Content == "" {
		writeJSON(w, http.StatusBadRequest, sendResult{Error: "quote_content 必填（被引用文本）"})
		return
	}
	if req.QuoteType != 0 && req.QuoteType != 1 {
		writeJSON(w, http.StatusBadRequest, sendResult{
			Error: fmt.Sprintf("一期仅支持 quote_type=1（文本）；收到 %d，图片引用二期", req.QuoteType),
		})
		return
	}
	if req.ChatUsr == "" {
		req.ChatUsr = defaultQuoteChatUsr(req.ChatID, req.FromUsr)
	}
	if req.DisplayName == "" {
		req.DisplayName = req.FromUsr
	}
	if req.CreateTime <= 0 {
		req.CreateTime = time.Now().Unix()
	}

	if err := p.guardSendTarget(req.ChatID); err != nil {
		writeJSON(w, http.StatusForbidden, sendResult{Error: err.Error()})
		return
	}
	if !p.allowSend() {
		writeJSON(w, http.StatusTooManyRequests, sendResult{Error: "发送频率超限"})
		return
	}
	if p.message == nil {
		writeJSON(w, http.StatusServiceUnavailable, sendResult{Error: "消息能力未注入"})
		return
	}

	xml, err := buildQuoteXML(quoteOutbound{
		Reply:       req.Reply,
		SvrID:       req.SvrID,
		FromUsr:     req.FromUsr,
		ChatUsr:     req.ChatUsr,
		DisplayName: req.DisplayName,
		Content:     req.Content,
		QuoteType:   1,
		CreateTime:  req.CreateTime,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, sendResult{Error: err.Error()})
		return
	}
	outcome, e := p.sendAppMessage(req.ChatID, 57, xml)
	if outcome != uploadOK {
		slog.Error("[hermes_bridge] 发引用消息失败",
			"chat", req.ChatID, "svrid", req.SvrID, "fromusr", req.FromUsr,
			"chatusr", req.ChatUsr, "outcome", outcome, "err", e)
		msg := "发送失败"
		if e != nil {
			msg = e.Error()
		} else if outcome == uploadTimeout {
			msg = "发送超时（结果未确认，可能已送达）"
		}
		writeJSON(w, http.StatusBadGateway, sendResult{Error: msg})
		return
	}
	slog.Info("[hermes_bridge] 已发引用消息",
		"chat", req.ChatID, "svrid", req.SvrID, "fromusr", req.FromUsr,
		"chatusr", req.ChatUsr, "displayname", req.DisplayName,
		"reply_len", len([]rune(req.Reply)), "xml_len", len(xml))
	if cap := strings.TrimSpace(req.Caption); cap != "" {
		_ = p.sendPlainText(p.resolveReceiver(req.ChatID), cap)
	}
	writeJSON(w, http.StatusOK, sendResult{Success: true})
}

// handleSendApp 出站 AppMsg 卡片：适配器拼好 XML + sub_type 后 POST 这里。
// 桥只补数据通道（业务/搜索/AppID 偏好都在 Hermes 侧）：校验 -> 限流 -> message.Send.
func (p *BridgePlugin) handleSendApp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req sendAppReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, sendResult{Error: "invalid json: " + err.Error()})
		return
	}
	req.ChatID = strings.TrimSpace(req.ChatID)
	req.Xml = strings.TrimSpace(req.Xml)
	if req.ChatID == "" {
		writeJSON(w, http.StatusBadRequest, sendResult{Error: "chat_id 必填"})
		return
	}
	if req.Xml == "" {
		writeJSON(w, http.StatusBadRequest, sendResult{Error: "xml 必填（整段 <appmsg>…</appmsg>）"})
		return
	}
	if req.SubType == 0 {
		writeJSON(w, http.StatusBadRequest, sendResult{Error: "sub_type 必填（76=音乐、5=链接、19=聊天记录）"})
		return
	}
	// AppId 兑底：调用方拼的 XML 里写的是 appid="" 才动；xml 里写了真实 appid 则以 xml 为准。
	if strings.Contains(req.Xml, `appid=""`) {
		appID := strings.TrimSpace(req.AppID)
		if appID == "" {
			appID = randomAppID()
		}
		req.Xml = strings.Replace(req.Xml, `appid=""`, `appid="`+appID+`"`, 1)
		tail := appID
		if len(tail) > 4 {
			tail = tail[len(tail)-4:]
		}
		slog.Info("[hermes_bridge] AppMsg 补随机 AppID", "chat", req.ChatID, "appid_tail", tail)
	}
	if err := p.guardSendTarget(req.ChatID); err != nil {
		writeJSON(w, http.StatusForbidden, sendResult{Error: err.Error()})
		return
	}
	if !p.allowSend() {
		writeJSON(w, http.StatusTooManyRequests, sendResult{Error: "发送频率超限"})
		return
	}
	if p.message == nil {
		writeJSON(w, http.StatusServiceUnavailable, sendResult{Error: "消息能力未注入"})
		return
	}
	outcome, e := p.sendAppMessage(req.ChatID, req.SubType, req.Xml)
	if outcome != uploadOK {
		slog.Error("[hermes_bridge] 发 AppMsg 卡片失败", "chat", req.ChatID, "outcome", outcome, "sub_type", req.SubType, "err", e)
		msg := "发送失败"
		if e != nil {
			msg = e.Error()
		} else if outcome == uploadTimeout {
			msg = "发送超时（结果未确认，可能已送达）"
		}
		writeJSON(w, http.StatusBadGateway, sendResult{Error: msg})
		return
	}
	slog.Info("[hermes_bridge] 已发 AppMsg 卡片", "chat", req.ChatID, "sub_type", req.SubType)
	if cap := strings.TrimSpace(req.Caption); cap != "" {
		_ = p.sendPlainText(p.resolveReceiver(req.ChatID), cap)
	}
	writeJSON(w, http.StatusOK, sendResult{Success: true})
}

func (p *BridgePlugin) handleMedia(w http.ResponseWriter, r *http.Request, kind string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req sendMediaReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, sendResult{Error: "invalid json: " + err.Error()})
		return
	}
	req.ChatID = strings.TrimSpace(req.ChatID)
	req.URL = strings.TrimSpace(req.URL)
	req.DataB64 = strings.TrimSpace(req.DataB64)
	if req.ChatID == "" {
		writeJSON(w, http.StatusBadRequest, sendResult{Error: "chat_id 必填"})
		return
	}
	// 表情允许仅传 md5（收藏重发，不传数据）
	if req.URL == "" && req.DataB64 == "" && !(kind == "emoji" && req.Md5 != "") {
		writeJSON(w, http.StatusBadRequest, sendResult{Error: "url 或 data_b64 至少提供一个（表情可仅传 md5）"})
		return
	}
	if err := p.guardSendTarget(req.ChatID); err != nil {
		writeJSON(w, http.StatusForbidden, sendResult{Error: err.Error()})
		return
	}
	if !p.allowSend() {
		writeJSON(w, http.StatusTooManyRequests, sendResult{Error: "发送频率超限"})
		return
	}

	var data []byte
	var err error
	if req.DataB64 != "" {
		data, err = base64.StdEncoding.DecodeString(req.DataB64)
		if err != nil {
			// 容错：URL-safe base64
			data, err = base64.RawURLEncoding.DecodeString(req.DataB64)
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, sendResult{Error: "data_b64 解码失败: " + err.Error()})
			return
		}
	} else {
		switch kind {
		case "image":
			data, err = p.downloadImage(req.URL)
		case "emoji":
			// md5 引用发送：跳过下载
			if req.Md5 != "" && req.URL == "" {
				break
			}
			data, err = p.downloadEmoji(req.URL)
		case "video", "voice":
			// 大文件走临时文件路径，这里仍先拿 bytes 便于复用
			data, err = p.downloadBytes(req.URL, mediaLimit(kind))
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, sendResult{Error: err.Error()})
			return
		}
	}
	if len(data) == 0 && !(kind == "emoji" && req.Md5 != "") {
		writeJSON(w, http.StatusBadRequest, sendResult{Error: "媒体内容为空"})
		return
	}
	if len(data) > int(mediaLimit(kind)) {
		writeJSON(w, http.StatusBadRequest, sendResult{
			Error: fmt.Sprintf("媒体超过 %dMB 上限", mediaLimit(kind)>>20),
		})
		return
	}

	var sendErr error
	switch kind {
	case "image":
		outcome, e := p.sendImageMessage(req.ChatID, data)
		if outcome != uploadOK {
			// 有 url 才降级发链接
			if req.URL != "" {
				p.fallbackLink(req.ChatID, "图片", req.URL, e)
				sendErr = nil
			} else {
				sendErr = e
			}
		}
	case "emoji":
		if req.Md5 != "" && len(data) == 0 {
			// 收藏重发：仅传 md5 不传数据，微信用 CDN 原文件，保原图画质。
			outcome, e := p.sendEmojiByMd5(req.ChatID, req.Md5)
			if outcome != uploadOK {
				slog.Error("[hermes_bridge] 表情 md5 引用发送失败", "chat", req.ChatID, "outcome", outcome, "md5", req.Md5, "err", e)
				sendErr = e
			} else {
				slog.Info("[hermes_bridge] 已发 md5 引用表情", "chat", req.ChatID, "md5", req.Md5)
			}
		} else if req.Raw && len(data) <= maxEmojiBytes {
			// 收藏重发且体积达标：原字节原 md5，不动（压缩会改 md5、JPEG 化丢动画）
			slog.Info("[hermes_bridge] 表情 raw 模式，原样发送", "chat", req.ChatID, "bytes", len(data))
			outcome, e := p.sendEmojiMessage(req.ChatID, data)
			if outcome != uploadOK {
				slog.Error("[hermes_bridge] 表情发送最终失败", "chat", req.ChatID, "outcome", outcome, "raw", req.Raw, "err", e)
				if req.URL != "" {
					p.fallbackLink(req.ChatID, "表情", req.URL, e)
					sendErr = nil
				} else {
					sendErr = e
				}
			} else {
				slog.Info("[hermes_bridge] 已发表情", "chat", req.ChatID, "bytes", len(data), "raw", req.Raw)
			}
		} else {
			// 实测 2MB 表情原样上传微信「假成功」不上屏（自定义表情上限约 1MB），
			// raw 超限时也必须压；GIF 走保动画压缩，静图才 JPEG。
			if req.Raw {
				slog.Info("[hermes_bridge] 表情 raw 超体积上限，转保动画压缩",
					"chat", req.ChatID, "bytes", len(data), "limit", maxEmojiBytes)
			}
			data = ensureEmojiBytes(data)
			outcome, e := p.sendEmojiMessage(req.ChatID, data)
			if outcome != uploadOK {
				slog.Error("[hermes_bridge] 表情发送最终失败", "chat", req.ChatID, "outcome", outcome, "raw", req.Raw, "err", e)
				if req.URL != "" {
					p.fallbackLink(req.ChatID, "表情", req.URL, e)
					sendErr = nil
				} else {
					sendErr = e
				}
			} else {
				slog.Info("[hermes_bridge] 已发表情", "chat", req.ChatID, "bytes", len(data), "raw", req.Raw)
			}
		}
	case "video":
		sendErr = p.sendVideoBytes(req.ChatID, data, req.URL)
	case "voice":
		sendErr = p.sendVoiceBytes(req.ChatID, data)
	}
	if sendErr != nil {
		slog.Error("[hermes_bridge] 发媒体失败", "kind", kind, "chat", req.ChatID, "err", sendErr)
		writeJSON(w, http.StatusBadGateway, sendResult{Error: sendErr.Error()})
		return
	}
	if cap := strings.TrimSpace(req.Caption); cap != "" {
		_ = p.sendPlainText(p.resolveReceiver(req.ChatID), cap)
	}
	writeJSON(w, http.StatusOK, sendResult{Success: true})
}

// guardSendTarget 出站目标须在白名单；主人私聊始终放行（与入站一致）。
// 空白名单时拒绝非主人目标（强制 /hermes enable）；cron home_channel 也须 enable。
func (p *BridgePlugin) guardSendTarget(chatID string) error {
	if strings.TrimSpace(chatID) == "" {
		return errors.New("chat_id 为空")
	}
	// 白名单命中：放行
	if p.findTarget(chatID) != nil {
		return nil
	}
	// 与入站一致：主人私聊始终可回发（无需 /hermes enable）
	p.selfMu.RLock()
	owner := p.owner
	p.selfMu.RUnlock()
	if owner != nil && owner.GetUsername() == chatID {
		return nil
	}
	cfg := p.configSnapshot()
	if len(cfg.Targets) == 0 {
		return errors.New("白名单为空，请先 /hermes enable（主人私聊除外）")
	}
	return fmt.Errorf("会话 %s 不在白名单", chatID)
}

func (p *BridgePlugin) allowSend() bool {
	limit := p.configSnapshot().SendRatePerMin
	if limit <= 0 {
		limit = 20
	}
	now := time.Now()
	p.rateMu.Lock()
	defer p.rateMu.Unlock()
	kept := p.sendTimes[:0]
	for _, t := range p.sendTimes {
		if now.Sub(t) < time.Minute {
			kept = append(kept, t)
		}
	}
	p.sendTimes = kept
	if len(p.sendTimes) >= limit {
		return false
	}
	p.sendTimes = append(p.sendTimes, now)
	return true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func mediaLimit(kind string) int64 {
	switch kind {
	case "image":
		return maxImageBytes
	case "emoji":
		return maxEmojiRawBytes
	case "video":
		return maxVideoBytes
	case "voice":
		return maxVoiceBytes
	default:
		return maxImageBytes
	}
}
