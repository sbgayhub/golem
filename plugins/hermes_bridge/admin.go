package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/sbgayhub/golem/sdk/contact"
)

// startAdminHTTP 在 AdminListen 上起独立管理台（默认 127.0.0.1:8644）。
// 与业务桥分离：不共用 bot token，不绑 0.0.0.0（除非用户显式改配置）。
func (p *BridgePlugin) startAdminHTTP() error {
	p.srvMu.Lock()
	defer p.srvMu.Unlock()
	if p.adminSrv != nil {
		return nil
	}
	cfg := p.configSnapshot()
	addr := strings.TrimSpace(cfg.AdminListen)
	if addr == "" {
		slog.Info("[hermes_bridge] 管理台未启用（admin_listen 为空）")
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/admin/meta", p.adminPublicMeta) // 无鉴权：仅发现入口，不含敏感字段
	mux.Handle("/admin/overview", p.adminAuth(http.HandlerFunc(p.adminOverview)))
	mux.Handle("/admin/targets", p.adminAuth(http.HandlerFunc(p.adminTargets)))
	mux.Handle("/admin/targets/", p.adminAuth(http.HandlerFunc(p.adminTargetByID)))
	mux.Handle("/admin/config", p.adminAuth(http.HandlerFunc(p.adminConfig)))
	mux.Handle("/admin/contacts/search", p.adminAuth(http.HandlerFunc(p.adminContactSearch)))
	mux.Handle("/admin/inbound/recent", p.adminAuth(http.HandlerFunc(p.adminInboundRecent)))
	mux.Handle("/admin/inbound/stream", p.adminAuth(http.HandlerFunc(p.adminInboundStream)))
	mux.Handle("/admin/sessions", p.adminAuth(http.HandlerFunc(p.adminSessions)))
	mux.Handle("/admin/diagnose", p.adminAuth(http.HandlerFunc(p.adminDiagnose)))
	mux.Handle("/admin/hermes/meta", p.adminAuth(http.HandlerFunc(p.adminHermesMeta)))
	mux.Handle("/admin/hermes/", p.adminAuth(http.HandlerFunc(p.adminHermesProxy)))

	uiRoot, err := fs.Sub(adminUI, "ui")
	if err != nil {
		return fmt.Errorf("embed ui: %w", err)
	}
	mux.Handle("/ui/", http.StripPrefix("/ui/", http.FileServer(http.FS(uiRoot))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "" {
			http.Redirect(w, r, "/ui/", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	p.adminSrv = srv

	go func() {
		slog.Info("[hermes_bridge] 管理台监听中", "addr", addr, "ui", "http://"+addr+"/ui/")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("[hermes_bridge] 管理台异常退出", "err", err)
			p.srvMu.Lock()
			if p.adminSrv == srv {
				p.adminSrv = nil
			}
			p.srvMu.Unlock()
		}
	}()
	return nil
}

func (p *BridgePlugin) stopAdminHTTP() {
	p.srvMu.Lock()
	srv := p.adminSrv
	p.adminSrv = nil
	p.srvMu.Unlock()
	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
}

func (p *BridgePlugin) adminAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(p.configSnapshot().AdminToken)
		if token == "" {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"error": "admin_token 未配置，管理 API 不可用",
			})
			return
		}
		got := strings.TrimSpace(r.Header.Get("Authorization"))
		got = strings.TrimPrefix(got, "Bearer ")
		got = strings.TrimPrefix(got, "bearer ")
		// 也接受 X-Admin-Token，方便 fetch 时不跟业务 Authorization 混淆
		if got == "" {
			got = strings.TrimSpace(r.Header.Get("X-Admin-Token"))
		}
		// EventSource 无法自定义头：允许 ?token=
		if got == "" {
			got = strings.TrimSpace(r.URL.Query().Get("token"))
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// adminPublicMeta 供以后 Golem 总控发现 UI 入口；不含 token / 白名单。
func (p *BridgePlugin) adminPublicMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg := p.configSnapshot()
	meta := p.GetMetadata()
	writeJSON(w, http.StatusOK, map[string]any{
		"name":         meta.GetName(),
		"version":      meta.GetVersion(),
		"ui":           "/ui/",
		"admin_listen": strings.TrimSpace(cfg.AdminListen),
		"auth":         "admin_token", // 提示总控：管理 API 用独立 token
	})
}

func (p *BridgePlugin) adminOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg := p.configSnapshot()
	sessN, pendN, bufN := p.sessionStats()
	mediaN := p.mediaRefCount()

	ownerOK := false
	ownerName := ""
	p.selfMu.RLock()
	if p.owner != nil && strings.TrimSpace(p.owner.GetUsername()) != "" {
		ownerOK = true
		ownerName = displayContact(p.owner)
	}
	selfName, selfID := "", ""
	if p.self != nil {
		selfName = strings.TrimSpace(p.self.GetNickname())
		selfID = strings.TrimSpace(p.self.GetUsername())
	}
	p.selfMu.RUnlock()

	subs := p.hub.subscriberCount()
	alerts := []string{}
	if subs == 0 {
		alerts = append(alerts, "SSE 无订阅者：Hermes 适配器可能未连接")
	}
	if strings.TrimSpace(cfg.Token) == "" {
		alerts = append(alerts, "业务 token 未配置：桥对适配器不可用")
	}
	if strings.TrimSpace(cfg.AdminToken) == "" {
		alerts = append(alerts, "admin_token 未配置")
	}
	if !ownerOK {
		alerts = append(alerts, "主人未识别")
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":                "ok",
		"version":               p.GetMetadata().GetVersion(),
		"listen":                cfg.Listen,
		"admin_listen":          cfg.AdminListen,
		"subscribers":           subs,
		"targets":               len(cfg.Targets),
		"token_masked":          maskToken(cfg.Token),
		"admin_token_set":       strings.TrimSpace(cfg.AdminToken) != "",
		"send_rate_per_min":     cfg.SendRatePerMin,
		"max_text_len":          cfg.MaxTextLen,
		"max_body_mb":           cfg.MaxBodyBytes >> 20,
		"group_gate":            !cfg.GroupPushAll,
		"debounce_seconds":      cfg.DebounceSeconds,
		"bubble_rate":           cfg.BubbleRate,
		"bubble_cooldown":       cfg.BubbleCooldownMin,
		"max_context":           cfg.MaxContextMessages,
		"emoji_burst_count":     cfg.EmojiBurstCount,
		"emoji_burst_window":    cfg.EmojiBurstWindowSec,
		"emoji_burst_cooldown":  cfg.EmojiBurstCooldownMin,
		"trigger_names":         cfg.TriggerNames,
		"local_sessions":        sessN,
		"pending_debounce":      pendN,
		"buffered_unflushed":    bufN,
		"media_refs":            mediaN,
		"owner_ok":              ownerOK,
		"owner_name":            ownerName,
		"self_name":             selfName,
		"self_id":               selfID,
		"gate_summary":          gateSummary(cfg),
		"alerts":                alerts,
		"ui":                    "/ui/",
		"hermes_ops_configured": strings.TrimSpace(cfg.HermesOpsURL) != "",
	})
}

func gateSummary(cfg Config) string {
	if cfg.GroupPushAll {
		return "群门闩关闭：白名单群每条都推 SSE"
	}
	parts := []string{"群须 @/引用/点名/冒泡 才推"}
	if cfg.DebounceSeconds > 0 {
		parts = append(parts, fmt.Sprintf("去抖 %ds", cfg.DebounceSeconds))
	}
	if cfg.BubbleRate > 0 {
		parts = append(parts, fmt.Sprintf("冒泡 %.0f%% / %dmin", cfg.BubbleRate*100, cfg.BubbleCooldownMin))
	} else {
		parts = append(parts, "冒泡关")
	}
	if cfg.EmojiBurstCount > 0 {
		parts = append(parts, fmt.Sprintf("斗图 %ds 内第 %d 条", cfg.EmojiBurstWindowSec, cfg.EmojiBurstCount))
	} else {
		parts = append(parts, "斗图关")
	}
	if len(cfg.TriggerNames) > 0 {
		parts = append(parts, "点名: "+strings.Join(cfg.TriggerNames, "/"))
	}
	return strings.Join(parts, " · ")
}

func (p *BridgePlugin) mediaRefCount() int {
	p.mediaMu.Lock()
	defer p.mediaMu.Unlock()
	return len(p.mediaRefs)
}

// ---- targets ----

type adminTarget struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"` // group / private
}

func targetKind(id string) string {
	if isChatroomID(id) {
		return "group"
	}
	return "private"
}

func (p *BridgePlugin) listAdminTargets() []adminTarget {
	cfg := p.configSnapshot()
	out := make([]adminTarget, 0, len(cfg.Targets))
	for _, t := range cfg.Targets {
		out = append(out, adminTarget{
			ID:   t.ID,
			Name: t.Name,
			Kind: targetKind(t.ID),
		})
	}
	return out
}

func (p *BridgePlugin) adminTargets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"targets": p.listAdminTargets()})
	case http.MethodPost:
		p.adminAddTarget(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (p *BridgePlugin) adminAddTarget(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json: " + err.Error()})
		return
	}
	id := strings.TrimSpace(req.ID)
	name := strings.TrimSpace(req.Name)
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "id 必填（wxid 或 xxx@chatroom）"})
		return
	}
	if p.findTarget(id) != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "已在白名单", "id": id})
		return
	}
	if name == "" {
		name = p.resolveTargetName(id)
	}
	p.cfgMu.Lock()
	p.Config.Targets = append(append([]Target(nil), p.Config.Targets...), Target{ID: id, Name: name})
	p.cfgMu.Unlock()
	p.saveConfig()
	slog.Info("[hermes_bridge] 管理台加入白名单", "id", id, "name", name)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"target": adminTarget{ID: id, Name: name, Kind: targetKind(id)},
	})
}

func (p *BridgePlugin) resolveTargetName(id string) string {
	if p.contact != nil {
		if c := p.contact.Get("username::" + id); c != nil {
			if n := displayContact(c); n != "" {
				return n
			}
		}
	}
	if isChatroomID(id) && p.chatroom != nil {
		if info, err := p.chatroom.GetInfo(id); err == nil && info != nil {
			for _, n := range []string{info.GetRemark(), info.GetNickname(), info.GetUsername()} {
				if strings.TrimSpace(n) != "" {
					return strings.TrimSpace(n)
				}
			}
		}
	}
	return id
}

func (p *BridgePlugin) adminTargetByID(w http.ResponseWriter, r *http.Request) {
	// path: /admin/targets/<id>  id 可能含 @，用 TrimPrefix 取剩余
	id := strings.TrimPrefix(r.URL.Path, "/admin/targets/")
	id = strings.TrimSpace(id)
	// 兼容 URL 编码后的路径
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "缺少 id"})
		return
	}
	switch r.Method {
	case http.MethodDelete:
		p.adminDeleteTarget(w, id)
	case http.MethodPatch:
		p.adminPatchTarget(w, r, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (p *BridgePlugin) adminDeleteTarget(w http.ResponseWriter, id string) {
	p.cfgMu.Lock()
	targets := make([]Target, 0, len(p.Config.Targets))
	removed := false
	var removedName string
	for _, t := range p.Config.Targets {
		if t.ID == id {
			removed = true
			removedName = t.Name
			continue
		}
		targets = append(targets, t)
	}
	p.Config.Targets = targets
	p.cfgMu.Unlock()
	if !removed {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "不在白名单", "id": id})
		return
	}
	p.saveConfig()
	slog.Info("[hermes_bridge] 管理台移出白名单", "id", id, "name", removedName)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
}

func (p *BridgePlugin) adminPatchTarget(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json: " + err.Error()})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "name 必填"})
		return
	}
	p.cfgMu.Lock()
	found := false
	for i := range p.Config.Targets {
		if p.Config.Targets[i].ID == id {
			p.Config.Targets[i].Name = name
			found = true
			break
		}
	}
	p.cfgMu.Unlock()
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "不在白名单", "id": id})
		return
	}
	p.saveConfig()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"target": adminTarget{ID: id, Name: name, Kind: targetKind(id)},
	})
}

// ---- config (gate fields) ----

type adminConfigView struct {
	TriggerNames          []string `json:"trigger_names"`
	BubbleRate            float64  `json:"bubble_rate"`
	BubbleCooldownMinutes int      `json:"bubble_cooldown_minutes"`
	DebounceSeconds       int      `json:"debounce_seconds"`
	MaxContextMessages    int      `json:"max_context_messages"`
	GroupPushAll          bool     `json:"group_push_all"`
	EmojiBurstCount       int      `json:"emoji_burst_count"`
	EmojiBurstWindowSec   int      `json:"emoji_burst_window_seconds"`
	EmojiBurstCooldownMin int      `json:"emoji_burst_cooldown_minutes"`
	MaxTextLen            int      `json:"max_text_len"`
	SendRatePerMin        int      `json:"send_rate_per_min"`
	GateSummary           string   `json:"gate_summary"`
}

func (p *BridgePlugin) adminConfigView() adminConfigView {
	cfg := p.configSnapshot()
	names := append([]string(nil), cfg.TriggerNames...)
	return adminConfigView{
		TriggerNames:          names,
		BubbleRate:            cfg.BubbleRate,
		BubbleCooldownMinutes: cfg.BubbleCooldownMin,
		DebounceSeconds:       cfg.DebounceSeconds,
		MaxContextMessages:    cfg.MaxContextMessages,
		GroupPushAll:          cfg.GroupPushAll,
		EmojiBurstCount:       cfg.EmojiBurstCount,
		EmojiBurstWindowSec:   cfg.EmojiBurstWindowSec,
		EmojiBurstCooldownMin: cfg.EmojiBurstCooldownMin,
		MaxTextLen:            cfg.MaxTextLen,
		SendRatePerMin:        cfg.SendRatePerMin,
		GateSummary:           gateSummary(cfg),
	}
}

func (p *BridgePlugin) adminConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, p.adminConfigView())
	case http.MethodPatch:
		p.adminPatchConfig(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (p *BridgePlugin) adminPatchConfig(w http.ResponseWriter, r *http.Request) {
	// 用 map 区分「未传」与「传了零值」
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json: " + err.Error()})
		return
	}
	if len(raw) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "空 body"})
		return
	}

	setFloat := func(key string, dst *float64) error {
		v, ok := raw[key]
		if !ok {
			return nil
		}
		var f float64
		if err := json.Unmarshal(v, &f); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		*dst = f
		return nil
	}
	setInt := func(key string, dst *int) error {
		v, ok := raw[key]
		if !ok {
			return nil
		}
		var n int
		if err := json.Unmarshal(v, &n); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		*dst = n
		return nil
	}
	setBool := func(key string, dst *bool) error {
		v, ok := raw[key]
		if !ok {
			return nil
		}
		var b bool
		if err := json.Unmarshal(v, &b); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		*dst = b
		return nil
	}

	p.cfgMu.Lock()
	if err := setFloat("bubble_rate", &p.Config.BubbleRate); err != nil {
		p.cfgMu.Unlock()
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if p.Config.BubbleRate < 0 {
		p.Config.BubbleRate = 0
	}
	if p.Config.BubbleRate > 1 {
		p.Config.BubbleRate = 1
	}
	if err := setInt("bubble_cooldown_minutes", &p.Config.BubbleCooldownMin); err != nil {
		p.cfgMu.Unlock()
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := setInt("debounce_seconds", &p.Config.DebounceSeconds); err != nil {
		p.cfgMu.Unlock()
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := setInt("max_context_messages", &p.Config.MaxContextMessages); err != nil {
		p.cfgMu.Unlock()
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := setBool("group_push_all", &p.Config.GroupPushAll); err != nil {
		p.cfgMu.Unlock()
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := setInt("emoji_burst_count", &p.Config.EmojiBurstCount); err != nil {
		p.cfgMu.Unlock()
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := setInt("emoji_burst_window_seconds", &p.Config.EmojiBurstWindowSec); err != nil {
		p.cfgMu.Unlock()
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := setInt("emoji_burst_cooldown_minutes", &p.Config.EmojiBurstCooldownMin); err != nil {
		p.cfgMu.Unlock()
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := setInt("max_text_len", &p.Config.MaxTextLen); err != nil {
		p.cfgMu.Unlock()
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := setInt("send_rate_per_min", &p.Config.SendRatePerMin); err != nil {
		p.cfgMu.Unlock()
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if v, ok := raw["trigger_names"]; ok {
		var names []string
		if err := json.Unmarshal(v, &names); err != nil {
			p.cfgMu.Unlock()
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "trigger_names: " + err.Error()})
			return
		}
		clean := make([]string, 0, len(names))
		seen := map[string]struct{}{}
		for _, n := range names {
			n = strings.TrimSpace(n)
			if n == "" {
				continue
			}
			if _, dup := seen[n]; dup {
				continue
			}
			seen[n] = struct{}{}
			clean = append(clean, n)
		}
		p.Config.TriggerNames = clean
	}
	p.cfgMu.Unlock()

	p.saveConfig()
	slog.Info("[hermes_bridge] 管理台更新门闩配置")
	writeJSON(w, http.StatusOK, p.adminConfigView())
}

// ---- contact search（便于不在会话内加白名单）----

func (p *BridgePlugin) adminContactSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "q 必填"})
		return
	}
	if p.contact == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "联系人能力未注入"})
		return
	}
	qLower := strings.ToLower(q)
	type hit struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Kind string `json:"kind"`
	}
	const limit = 30
	hits := make([]hit, 0, limit)
	for _, c := range p.contact.List() {
		if c == nil {
			continue
		}
		id := strings.TrimSpace(c.GetUsername())
		if id == "" {
			continue
		}
		// 跳过特殊号
		if c.GetType() == contact.ContactType_CONTACT_TYPE_SPECIAL {
			continue
		}
		name := displayContact(c)
		if !contactMatch(q, qLower, id, name, c.GetRemark(), c.GetNickname(), c.GetAlias()) {
			continue
		}
		hits = append(hits, hit{ID: id, Name: name, Kind: targetKind(id)})
		if len(hits) >= limit {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"q": q, "results": hits})
}

func contactMatch(q, qLower string, fields ...string) bool {
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if strings.Contains(f, q) || strings.Contains(strings.ToLower(f), qLower) {
			return true
		}
	}
	return false
}
