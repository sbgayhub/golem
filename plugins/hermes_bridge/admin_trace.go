package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	adminTraceMax     = 200
	adminTraceTextMax = 120
)

// adminTrace 管理台入站旁路事件（脱敏预览，不含完整群批次正文）。
type adminTrace struct {
	ID            uint64 `json:"id"`
	Ts            int64  `json:"ts"`
	Kind          string `json:"kind"` // pushed / dropped / context_only / scheduled / cancelled
	Reason        string `json:"reason,omitempty"`
	SessionKey    string `json:"session_key,omitempty"`
	ChatID        string `json:"chat_id,omitempty"`
	ChatName      string `json:"chat_name,omitempty"`
	ChatType      string `json:"chat_type,omitempty"`
	UserName      string `json:"user_name,omitempty"`
	UserID        string `json:"user_id,omitempty"`
	Text          string `json:"text,omitempty"` // 截断预览
	TriggerReason string `json:"trigger_reason,omitempty"`
	Addressing    string `json:"addressing,omitempty"`
	Subscribers   int    `json:"subscribers,omitempty"`
	MsgCount      int    `json:"msg_count,omitempty"`
}

type adminTraceHub struct {
	mu      sync.Mutex
	seq     atomic.Uint64
	ring    []adminTrace
	clients map[chan adminTrace]struct{}
}

func newAdminTraceHub() *adminTraceHub {
	return &adminTraceHub{
		ring:    make([]adminTrace, 0, adminTraceMax),
		clients: map[chan adminTrace]struct{}{},
	}
}

func (h *adminTraceHub) record(t adminTrace) {
	if h == nil {
		return
	}
	t.ID = h.seq.Add(1)
	if t.Ts == 0 {
		t.Ts = time.Now().Unix()
	}
	t.Text = truncateRunes(t.Text, adminTraceTextMax)

	h.mu.Lock()
	h.ring = append(h.ring, t)
	if len(h.ring) > adminTraceMax {
		h.ring = append([]adminTrace(nil), h.ring[len(h.ring)-adminTraceMax:]...)
	}
	// 非阻塞 fan-out；慢客户端丢事件
	for ch := range h.clients {
		select {
		case ch <- t:
		default:
		}
	}
	h.mu.Unlock()
}

func (h *adminTraceHub) recent(n int) []adminTrace {
	if h == nil {
		return nil
	}
	if n <= 0 || n > adminTraceMax {
		n = 50
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.ring) == 0 {
		return []adminTrace{}
	}
	if n > len(h.ring) {
		n = len(h.ring)
	}
	out := make([]adminTrace, n)
	copy(out, h.ring[len(h.ring)-n:])
	// 新在前，便于 UI
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func (h *adminTraceHub) subscribe() chan adminTrace {
	ch := make(chan adminTrace, 64)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *adminTraceHub) unsubscribe(ch chan adminTrace) {
	if ch == nil || h == nil {
		return
	}
	h.mu.Lock()
	if _, ok := h.clients[ch]; ok {
		delete(h.clients, ch)
		close(ch)
	}
	h.mu.Unlock()
}

func (p *BridgePlugin) trace(t adminTrace) {
	if p.adminTrace == nil {
		return
	}
	if t.Subscribers == 0 && p.hub != nil {
		t.Subscribers = p.hub.subscriberCount()
	}
	p.adminTrace.record(t)
}

func (p *BridgePlugin) tracePushed(ev bridgeEvent, msgCount int) {
	text := ev.Text
	// 群批次正文是长信封，只留触发信息
	if msgCount > 1 || strings.Contains(text, "golem_verified_identity_json") {
		text = singleLine(ev.ChatName + " 批次")
	} else {
		text = singleLine(text)
	}
	p.trace(adminTrace{
		Kind:          "pushed",
		Reason:        "sse",
		SessionKey:    ev.SessionKey,
		ChatID:        ev.ChatID,
		ChatName:      ev.ChatName,
		ChatType:      ev.ChatType,
		UserName:      ev.UserName,
		UserID:        ev.UserID,
		Text:          text,
		TriggerReason: ev.TriggerReason,
		Addressing:    ev.Addressing,
		MsgCount:      msgCount,
	})
}

func (p *BridgePlugin) adminInboundRecent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	n := 50
	if v := strings.TrimSpace(r.URL.Query().Get("n")); v != "" {
		if x, err := parsePositiveInt(v); err == nil {
			n = x
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"events": p.adminTrace.recent(n),
	})
}

// adminInboundStream SSE；EventSource 不能自定义头，允许 ?token= 与 Header 等效。
func (p *BridgePlugin) adminInboundStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := p.adminTrace.subscribe()
	defer p.adminTrace.unsubscribe(ch)

	// 先推一截历史（时间正序），方便打开页就有内容
	hist := p.adminTrace.recent(30)
	for i := len(hist) - 1; i >= 0; i-- {
		b, err := json.Marshal(hist[i])
		if err != nil {
			continue
		}
		_, _ = w.Write([]byte("event: trace\ndata: "))
		_, _ = w.Write(b)
		_, _ = w.Write([]byte("\n\n"))
	}
	flusher.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		case ev, open := <-ch:
			if !open {
				return
			}
			b, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			_, _ = w.Write([]byte("event: trace\ndata: "))
			_, _ = w.Write(b)
			_, _ = w.Write([]byte("\n\n"))
			flusher.Flush()
		}
	}
}

func parsePositiveInt(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		n = 0
	}
	if n > 1000 {
		n = 1000
	}
	return n, nil
}
