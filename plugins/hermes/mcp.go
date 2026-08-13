package main

import (
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// startHTTP 启动内嵌 MCP server（幂等）
func (p *HermesPlugin) startHTTP() error {
	p.srvMu.Lock()
	defer p.srvMu.Unlock()
	if p.httpSrv != nil {
		return nil
	}
	cfg := p.configSnapshot()
	if strings.TrimSpace(cfg.MCPListen) == "" {
		return errors.New("未配置 mcp_listen")
	}

	server := p.buildMCPServer()
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless: true,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"golem-hermes-mcp"}`))
	})
	mux.Handle("/mcp", p.authMiddleware(handler))

	srv := &http.Server{
		Addr:              cfg.MCPListen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	p.httpSrv = srv

	go func() {
		slog.Info("[hermes] MCP server 监听中", "addr", cfg.MCPListen)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("[hermes] MCP server 异常退出", "err", err)
			p.srvMu.Lock()
			if p.httpSrv == srv {
				p.httpSrv = nil
			}
			p.srvMu.Unlock()
		}
	}()
	return nil
}

// stopHTTP 停止内嵌 MCP server（幂等）
func (p *HermesPlugin) stopHTTP() {
	p.srvMu.Lock()
	srv := p.httpSrv
	p.httpSrv = nil
	p.srvMu.Unlock()
	if srv != nil {
		_ = srv.Close()
	}
}

// authMiddleware Bearer token 校验；token 未配置时拒绝所有请求
func (p *HermesPlugin) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(p.configSnapshot().MCPToken)
		if token == "" {
			http.Error(w, "mcp_token 未配置，服务不可用", http.StatusServiceUnavailable)
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

// buildMCPServer 组装 MCP server 并注册工具
func (p *HermesPlugin) buildMCPServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "golem-wechat", Version: "0.1.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "wechat_list_targets",
		Description: "列出当前允许发送的微信会话。发送前先调用它确认 target_id；权限随触发场景变化，以本工具返回为准。",
	}, p.toolListTargets)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "wechat_send_text",
		Description: "发送一条文本消息到微信会话。这是你在微信里说话的唯一方式；回复对话时 target_id 用当前触发会话的。想发多条就多次调用，一轮最多 3 条。",
	}, p.toolSendText)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "wechat_send_image",
		Description: "下载图片 URL 并作为图片消息发送到微信会话（仅 http/https，10MB 以内）。",
	}, p.toolSendImage)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "wechat_send_voice",
		Description: "下载音频 URL 并作为微信语音消息发送（mp3/wav/amr/silk，10MB 以内，自动转微信语音格式）。适合说悄悄话、唱歌、讲段子等场景。",
	}, p.toolSendVoice)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "wechat_send_video",
		Description: "下载视频 URL 并作为视频消息发送到微信会话（mp4 等，100MB 以内）。",
	}, p.toolSendVideo)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "wechat_query_history",
		Description: "查询某会话的历史文本发言（来自 statistics 插件存档，时间正序）。注意：返回不含发言人昵称，只有内容与时间，适合做话题总结、活跃度分析。",
	}, p.toolQueryHistory)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "wechat_group_members",
		Description: "获取某个微信群的成员列表（wxid 与显示昵称）。",
	}, p.toolGroupMembers)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "wechat_send_emoji",
		Description: "下载图片 URL 并作为微信表情消息发送（走表情而非普通图片，适合发斗图/动图表情）。入参同 wechat_send_image；表情体积上限约 500KB，过大自动压缩。",
	}, p.toolSendEmoji)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "wechat_self_info",
		Description: "获取机器人自身的微信账号信息（昵称、微信号、wxid）。用于你知道自己是谁、避免把自己发的消息当成别人发的。",
	}, p.toolSelfInfo)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "wechat_group_info",
		Description: "获取某个微信群的基本信息：群名、群主 wxid、成员数。群公告/管理员列表当前微信侧接口未回传，故不包含。target_id 留空时默认当前群。",
	}, p.toolGroupInfo)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "wechat_group_member_detail",
		Description: "按 wxid 批量拉取群成员详情（昵称、群内显示名、头像 URL），补 wechat_group_members 缺的头像与详情。入参 wxids 为要查询的成员 wxid 列表。",
	}, p.toolGroupMemberDetail)
	return server
}

// allowSend 发送类工具的全局滑动窗口限流
func (p *HermesPlugin) allowSend() bool {
	limit := p.configSnapshot().SendRatePerMin
	if limit <= 0 {
		limit = 10
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
