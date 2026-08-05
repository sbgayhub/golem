package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ---- 查询 API：给 Hermes 侧 tool / 人工诊断，先查 wxid 再带进 POST /send ----

// sanitizeMentionWxids 清洗 mentions：去空白、去重、上限 50；不校验是否在群内。
func sanitizeMentionWxids(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
		if len(out) >= 50 {
			break
		}
	}
	return out
}

func (p *BridgePlugin) handleSelf(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	self := p.selfForEvent()
	if self == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"success": false,
			"error":   "机器人账号信息暂不可用（contact 未就绪）",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"nickname": self.GetNickname(),
		"alias":    self.GetAlias(),
		"wxid":     self.GetUsername(),
	})
}

func (p *BridgePlugin) handleGroupInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	chatID := strings.TrimSpace(r.URL.Query().Get("chat_id"))
	if chatID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "chat_id 必填"})
		return
	}
	if !isChatroomID(chatID) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "目标不是群聊会话"})
		return
	}
	if err := p.guardSendTarget(chatID); err != nil {
		writeJSON(w, http.StatusForbidden, map[string]any{"success": false, "error": err.Error()})
		return
	}
	if p.chatroom == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"success": false, "error": "群聊能力未注入"})
		return
	}
	resp, err := p.chatroom.GetInfo(chatID)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"success": false,
			"error":   fmt.Sprintf("获取群信息失败: %v", err),
		})
		return
	}
	ownerWxid := resp.GetOwner()
	ownerName := ""
	if ownerWxid != "" && p.contact != nil {
		if c := p.resolveReceiver(ownerWxid); c != nil {
			ownerName = displayContact(c)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":      true,
		"name":         resp.GetNickname(),
		"owner_wxid":   ownerWxid,
		"owner_name":   ownerName,
		"member_count": resp.GetMemberCount(),
		// GetInfo 通常不回公告/管理员；有则以后再扩，勿让 agent 装有
		"note": "群公告与管理员列表当前微信侧 GetInfo 通常未回传，故不包含。",
	})
}

func (p *BridgePlugin) handleGroupMembers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	chatID := strings.TrimSpace(r.URL.Query().Get("chat_id"))
	if chatID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "chat_id 必填"})
		return
	}
	if !isChatroomID(chatID) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "目标不是群聊会话"})
		return
	}
	if err := p.guardSendTarget(chatID); err != nil {
		writeJSON(w, http.StatusForbidden, map[string]any{"success": false, "error": err.Error()})
		return
	}
	if p.chatroom == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"success": false, "error": "群聊能力未注入"})
		return
	}
	members := p.chatroom.ListMembers(chatID)
	type item struct {
		Wxid string `json:"wxid"`
		Name string `json:"name"`
	}
	out := make([]item, 0, len(members))
	const maxN = 500
	for i, m := range members {
		if i >= maxN {
			break
		}
		if m == nil {
			continue
		}
		out = append(out, item{Wxid: m.GetUsername(), Name: displayMember(m)})
	}
	note := ""
	if len(members) > maxN {
		note = fmt.Sprintf("成员超过 %d，仅返回前 %d 条。", maxN, maxN)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"count":   len(out),
		"members": out,
		"note":    note,
	})
}

type groupMemberDetailReq struct {
	ChatID string   `json:"chat_id"`
	Wxids  []string `json:"wxids"`
}

func (p *BridgePlugin) handleGroupMemberDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req groupMemberDetailReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "invalid json: " + err.Error()})
		return
	}
	chatID := strings.TrimSpace(req.ChatID)
	if chatID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "chat_id 必填"})
		return
	}
	if !isChatroomID(chatID) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "目标不是群聊会话"})
		return
	}
	if err := p.guardSendTarget(chatID); err != nil {
		writeJSON(w, http.StatusForbidden, map[string]any{"success": false, "error": err.Error()})
		return
	}
	if p.chatroom == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"success": false, "error": "群聊能力未注入"})
		return
	}
	wxids := sanitizeMentionWxids(req.Wxids)
	if len(wxids) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "wxids 不能为空"})
		return
	}
	// 不用 GetMembersDetail：host 侧曾为 panic 占位。与旧 hermes 一致走 ListMembers 缓存。
	want := make(map[string]bool, len(wxids))
	for _, id := range wxids {
		want[id] = true
	}
	type detail struct {
		Wxid        string `json:"wxid"`
		Nickname    string `json:"nickname,omitempty"`
		DisplayName string `json:"display_name,omitempty"`
		AvatarURL   string `json:"avatar_url,omitempty"`
	}
	found := make([]detail, 0, len(wxids))
	for _, m := range p.chatroom.ListMembers(chatID) {
		if m == nil || !want[m.GetUsername()] {
			continue
		}
		found = append(found, detail{
			Wxid:        m.GetUsername(),
			Nickname:    m.GetNickname(),
			DisplayName: m.GetDisplayName(),
			AvatarURL:   m.GetAvatar(),
		})
	}
	note := ""
	if len(found) < len(wxids) {
		note = "部分 wxid 不在群成员缓存中，未返回。"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"count":   len(found),
		"members": found,
		"note":    note,
	})
}
