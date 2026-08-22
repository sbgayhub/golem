package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var errOpsNotConfigured = fmt.Errorf("未配置 hermes_ops_url")

// hermesOpsDo 桥 → hermes_ops（Hermes 侧只读运维服务）；method 支持 GET/PUT/DELETE。
func (p *BridgePlugin) hermesOpsDo(method, path string, query url.Values, body io.Reader, contentType string) (int, []byte, string, error) {
	cfg := p.configSnapshot()
	base := strings.TrimSpace(cfg.HermesOpsURL)
	if base == "" {
		return 0, nil, "", errOpsNotConfigured
	}
	base = strings.TrimRight(base, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u, err := url.Parse(base + path)
	if err != nil {
		return 0, nil, "", err
	}
	if query != nil {
		u.RawQuery = query.Encode()
	}
	req, err := http.NewRequest(method, u.String(), body)
	if err != nil {
		return 0, nil, "", err
	}
	if tok := strings.TrimSpace(cfg.HermesOpsToken); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	if contentType != "" && body != nil {
		req.Header.Set("Content-Type", contentType)
	}
	client := p.dlClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, "", err
	}
	defer resp.Body.Close()
	// 桥侧中转上限 32MB：与 ops 端 16MB 限制配合，留点余量给 JSON/错误体等
	// 之前 2MB 太小，把 8MB+ 的动图表情直接截掉，前端就拿到"file too large"
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return resp.StatusCode, nil, resp.Header.Get("Content-Type"), err
	}
	return resp.StatusCode, raw, resp.Header.Get("Content-Type"), nil
}

func (p *BridgePlugin) writeOpsError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if err == errOpsNotConfigured {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   err.Error(),
			"hint":    "在 config.toml 设置 hermes_ops_url（Hermes 侧 hermes_ops 的监听地址）",
			"example": "hermes_ops_url = \"http://<hermes-host>:8650\"",
		})
		return true
	}
	writeJSON(w, http.StatusBadGateway, map[string]any{"error": "ops 请求失败: " + err.Error()})
	return true
}

func writeOpsResponse(w http.ResponseWriter, code int, body []byte, ct string) {
	if ct == "" {
		ct = "application/json"
	}
	// ops 404 且像 JSON：补升级提示（二进制 404 很少见）
	if code == http.StatusNotFound && strings.Contains(ct, "json") {
		var ops any
		if err := json.Unmarshal(body, &ops); err != nil {
			ops = strings.TrimSpace(string(body))
		}
		writeJSON(w, code, map[string]any{
			"error": "ops 返回 404（请确认 Hermes 侧 hermes_ops.py 已更新到含 stickers 的版本并 restart）",
			"ops":   ops,
			"fix":   "cp 仓库 hermes_ops.py → $HERMES_OPS_DIR（默认 ~/.hermes/ops/，不要拷到 systemd 目录）后重启 ops 服务",
			"check": "curl …/health 应含 version≥0.3；…/stickers 与 …/stickers/<md5>/file 可用",
		})
		return
	}
	w.Header().Set("Content-Type", ct)
	// 表情预览等：允许管理台 <img> 缓存一小会儿
	if strings.HasPrefix(ct, "image/") {
		w.Header().Set("Cache-Control", "private, max-age=3600")
	}
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

// mapAdminHermesPath 把 /admin/hermes/... 映到 ops 路径；ok=false 表示非法。
func mapAdminHermesPath(rest string) (opsPath string, ok bool) {
	rest = strings.TrimSpace(rest)
	if rest == "" || rest == "/" {
		return "", false
	}
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		rest = rest[:i]
	}
	if dec, err := url.PathUnescape(rest); err == nil {
		rest = dec
	}
	if !strings.HasPrefix(rest, "/") {
		rest = "/" + rest
	}
	if rest != "/" {
		rest = strings.TrimSuffix(rest, "/")
		if rest == "" {
			rest = "/"
		}
		if !strings.HasPrefix(rest, "/") {
			rest = "/" + rest
		}
	}
	switch rest {
	case "/health", "/overview", "/tools/check", "/sessions", "/logs",
		"/stickers", "/stickers/facets", "/member_profiles":
		return rest, true
	}
	if strings.HasPrefix(rest, "/stickers/") {
		tail := strings.TrimPrefix(rest, "/stickers/")
		if tail == "" {
			return "", false
		}
		// /stickers/<md5> 或 /stickers/<md5>/file
		parts := strings.Split(tail, "/")
		if len(parts) == 1 {
			return "/stickers/" + parts[0], true
		}
		if len(parts) == 2 && parts[1] == "file" && parts[0] != "" {
			return "/stickers/" + parts[0] + "/file", true
		}
		return "", false
	}
	if strings.HasPrefix(rest, "/member_profiles/") {
		tail := strings.TrimPrefix(rest, "/member_profiles/")
		if tail == "" || strings.Contains(tail, "/") {
			return "", false
		}
		return "/member_profiles/" + tail, true
	}
	return "", false
}

func (p *BridgePlugin) adminHermesProxy(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	rest := strings.TrimPrefix(path, "/admin/hermes")
	opsPath, ok := mapAdminHermesPath(rest)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error":      "桥侧不识别该 hermes 路径（请确认已编译并重载 hermes_bridge ≥0.8）",
			"path":       path,
			"rest":       rest,
			"hint":       "允许：health|overview|tools/check|sessions|logs|stickers|stickers/<md5>[/file]|member_profiles|member_profiles/<wxid>",
			"bridge_ver": p.GetMetadata().GetVersion(),
		})
		return
	}

	switch r.Method {
	case http.MethodGet:
		code, body, ct, err := p.hermesOpsDo(http.MethodGet, opsPath, r.URL.Query(), nil, "")
		if p.writeOpsError(w, err) {
			return
		}
		writeOpsResponse(w, code, body, ct)

	case http.MethodPut:
		if !strings.HasPrefix(opsPath, "/member_profiles/") {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "仅 member_profiles/<wxid> 支持 PUT"})
			return
		}
		code, body, ct, err := p.hermesOpsDo(http.MethodPut, opsPath, nil, r.Body, "application/json")
		if p.writeOpsError(w, err) {
			return
		}
		writeOpsResponse(w, code, body, ct)

	case http.MethodDelete:
		if !strings.HasPrefix(opsPath, "/member_profiles/") {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "仅 member_profiles/<wxid> 支持 DELETE"})
			return
		}
		code, body, ct, err := p.hermesOpsDo(http.MethodDelete, opsPath, nil, nil, "")
		if p.writeOpsError(w, err) {
			return
		}
		writeOpsResponse(w, code, body, ct)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// adminHermesMeta 告诉 UI 是否配置了 ops，并探活 ops 版本（旧版无 stickers 会 404）。
func (p *BridgePlugin) adminHermesMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg := p.configSnapshot()
	u := strings.TrimSpace(cfg.HermesOpsURL)
	out := map[string]any{
		"configured": u != "",
		"ops_url":    u,
		"token_set":  strings.TrimSpace(cfg.HermesOpsToken) != "",
		"bridge_ver": p.GetMetadata().GetVersion(),
		"paths": []string{
			"/admin/hermes/health",
			"/admin/hermes/overview",
			"/admin/hermes/tools/check",
			"/admin/hermes/sessions",
			"/admin/hermes/logs",
			"/admin/hermes/stickers",
			"/admin/hermes/stickers/facets",
			"/admin/hermes/stickers/<md5>/file",
			"/admin/hermes/member_profiles",
		},
	}
	if u != "" {
		code, body, _, err := p.hermesOpsDo(http.MethodGet, "/health", nil, nil, "")
		if err != nil {
			out["ops_reachable"] = false
			out["ops_error"] = err.Error()
		} else {
			out["ops_reachable"] = code >= 200 && code < 300
			out["ops_http_status"] = code
			var health map[string]any
			if json.Unmarshal(body, &health) == nil {
				out["ops_health"] = health
				ver, _ := health["version"].(string)
				if ver != "" {
					out["ops_version"] = ver
					if ver < "0.4" {
						out["ops_warn"] = "ops version < 0.4：缺 stickers/facets；请更新 hermes_ops.py 后重启 ops 服务"
					}
				} else if code >= 200 && code < 300 {
					out["ops_warn"] = "ops /health 无 version 字段：多半是旧脚本，表情/档案接口会 404"
				}
			} else {
				out["ops_body"] = string(body)
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}
