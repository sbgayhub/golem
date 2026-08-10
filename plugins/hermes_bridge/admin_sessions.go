package main

import (
	"net/http"
	"sort"
	"time"
)

// adminSessionView 桥侧本地 session 态（不是 Hermes gateway session）。
type adminSessionView struct {
	SessionKey       string `json:"session_key"`
	ChatID           string `json:"chat_id,omitempty"`
	ChatName         string `json:"chat_name,omitempty"`
	ChatType         string `json:"chat_type,omitempty"`
	PendingDebounce  bool   `json:"pending_debounce"`
	ContextCount     int    `json:"context_count"`
	UnflushedCount   int    `json:"unflushed_count"`
	LastTrigger      string `json:"last_trigger,omitempty"`
	LastAddressing   string `json:"last_addressing,omitempty"`
	LastUserName     string `json:"last_user_name,omitempty"`
	BubbleCoolRemain int    `json:"bubble_cool_remain_sec,omitempty"` // 冒泡冷却剩余秒，0=可冒泡
	BurstCoolRemain  int    `json:"burst_cool_remain_sec,omitempty"`
	InWhitelist      bool   `json:"in_whitelist"`
}

func (p *BridgePlugin) listAdminSessions() []adminSessionView {
	cfg := p.configSnapshot()
	now := time.Now()

	bubbleCD := time.Duration(cfg.BubbleCooldownMin) * time.Minute
	burstCD := time.Duration(cfg.EmojiBurstCooldownMin) * time.Minute

	p.bubbleMu.Lock()
	bubbleCopy := make(map[string]time.Time, len(p.lastBubble))
	for k, t := range p.lastBubble {
		bubbleCopy[k] = t
	}
	p.bubbleMu.Unlock()

	p.burstMu.Lock()
	burstCopy := make(map[string]time.Time, len(p.lastBurst))
	for k, t := range p.lastBurst {
		burstCopy[k] = t
	}
	p.burstMu.Unlock()

	p.sessMu.Lock()
	defer p.sessMu.Unlock()

	out := make([]adminSessionView, 0, len(p.sessions))
	for key, st := range p.sessions {
		if st == nil {
			continue
		}
		v := adminSessionView{
			SessionKey:      key,
			ChatID:          st.meta.ChatID,
			ChatName:        st.meta.ChatName,
			ChatType:        st.meta.ChatType,
			PendingDebounce: st.pending,
			ContextCount:    len(st.msgs),
			LastTrigger:     st.meta.TriggerReason,
			LastAddressing:  st.meta.Addressing,
			LastUserName:    st.meta.UserName,
		}
		if v.ChatID == "" {
			// session key 通常就是 chat id
			v.ChatID = key
		}
		if v.ChatType == "" {
			if isChatroomID(v.ChatID) {
				v.ChatType = "group"
			} else {
				v.ChatType = "private"
			}
		}
		for _, m := range st.msgs {
			if !m.Flushed {
				v.UnflushedCount++
			}
		}
		if t, ok := bubbleCopy[key]; ok && bubbleCD > 0 {
			if rem := t.Add(bubbleCD).Sub(now); rem > 0 {
				v.BubbleCoolRemain = int(rem.Seconds()) + 1
			}
		}
		if t, ok := burstCopy[key]; ok && burstCD > 0 {
			if rem := t.Add(burstCD).Sub(now); rem > 0 {
				v.BurstCoolRemain = int(rem.Seconds()) + 1
			}
		}
		v.InWhitelist = p.findTarget(v.ChatID) != nil
		// 名称兜底：白名单备注
		if v.ChatName == "" {
			if t := p.findTarget(v.ChatID); t != nil {
				v.ChatName = t.Name
			}
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		// pending 优先，其次未推多的
		if out[i].PendingDebounce != out[j].PendingDebounce {
			return out[i].PendingDebounce
		}
		if out[i].UnflushedCount != out[j].UnflushedCount {
			return out[i].UnflushedCount > out[j].UnflushedCount
		}
		return out[i].SessionKey < out[j].SessionKey
	})
	return out
}

func (p *BridgePlugin) adminSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"note":     "桥侧本地 session（门闩/去抖），非 Hermes gateway session",
		"sessions": p.listAdminSessions(),
	})
}
