package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// adminDiagnose POST /admin/diagnose
// body: { "kind": "image"|"video"|"emoji", "chat_id": "...", "url": "..." }
// emoji 也支持 md5 字段（32 位 hex）代替 url。
func (p *BridgePlugin) adminDiagnose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Kind   string `json:"kind"`
		ChatID string `json:"chat_id"`
		URL    string `json:"url"`
		Md5    string `json:"md5"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json: " + err.Error()})
		return
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	chatID := strings.TrimSpace(req.ChatID)
	url := strings.TrimSpace(req.URL)
	md5hex := strings.ToLower(strings.TrimSpace(req.Md5))
	if chatID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "chat_id 必填"})
		return
	}
	if kind != "image" && kind != "video" && kind != "emoji" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "kind 须为 image / video / emoji"})
		return
	}
	// 诊断也走白名单（与出站一致）；主人私聊放行
	if err := p.guardSendTarget(chatID); err != nil {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": err.Error()})
		return
	}
	if p.message == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "消息能力未注入"})
		return
	}

	switch kind {
	case "image":
		if url == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "image 需要 url"})
			return
		}
		data, err := p.downloadImage(url)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "下载失败: " + err.Error()})
			return
		}
		outcome, err := p.sendImageMessage(chatID, data)
		writeDiagnoseResult(w, kind, chatID, len(data), 0, outcome, err)

	case "video":
		if url == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "video 需要 url"})
			return
		}
		data, err := p.downloadBytes(url, maxVideoBytes)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "下载失败: " + err.Error()})
			return
		}
		outcome, err := p.sendVideoMessage(chatID, data)
		writeDiagnoseResult(w, kind, chatID, len(data), 0, outcome, err)

	case "emoji":
		if md5hex != "" && len(md5hex) == 32 {
			if _, err := hex.DecodeString(md5hex); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "md5 非法"})
				return
			}
			outcome, err := p.sendEmojiByMd5(chatID, md5hex)
			writeDiagnoseResult(w, kind, chatID, 0, 0, outcome, err)
			return
		}
		if url == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "emoji 需要 url 或 32 位 md5"})
			return
		}
		data, err := p.downloadEmoji(url)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "下载失败: " + err.Error()})
			return
		}
		before := len(data)
		data = ensureEmojiBytes(data)
		outcome, err := p.sendEmojiMessage(chatID, data)
		writeDiagnoseResult(w, kind, chatID, before, len(data), outcome, err)
	}
}

func writeDiagnoseResult(w http.ResponseWriter, kind, chatID string, bytesIn, bytesOut int, outcome uploadOutcome, err error) {
	ok := outcome == uploadOK
	resp := map[string]any{
		"ok":       ok,
		"kind":     kind,
		"chat_id":  chatID,
		"outcome":  int(outcome),
		"bytes_in": bytesIn,
	}
	if bytesOut > 0 {
		resp["bytes_out"] = bytesOut
	}
	if err != nil {
		resp["error"] = err.Error()
	}
	if !ok && err == nil {
		resp["error"] = fmt.Sprintf("发送未确认 outcome=%d", outcome)
	}
	code := http.StatusOK
	if !ok {
		code = http.StatusBadGateway
	}
	writeJSON(w, code, resp)
}
