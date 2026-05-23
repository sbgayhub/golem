package plugin

import (
	"log/slog"
	"sync"
	"time"

	"github.com/sbgayhub/golem/sdk/contact"
	"github.com/sbgayhub/golem/sdk/plugin"
)

var (
	sessions  = make(map[string]*session) // key: sender username
	sessionMu sync.RWMutex
)

type session struct {
	PluginName    string
	Sender        string
	SenderContact *contact.Contact // 会话关联的发送者 Contact（用于事件通知）
	Duration      time.Duration
	ExpireAt      time.Time
	Timer         *time.Timer
}

// --- 内部辅助函数 ---

func findWrapper(capability string) *wrapper {
	if w := capabilityIndex[capability]; w != nil {
		return w
	}
	return nil
}

func findWrapperByName(name string) *wrapper {
	for _, w := range plugins {
		if w.Metadata.Name == name {
			return w
		}
	}
	return nil
}

// sessionRelease 释放会话
func sessionRelease(sender string) {
	sessionMu.Lock()
	defer sessionMu.Unlock()

	if s, ok := sessions[sender]; ok {
		s.Timer.Stop()
		slog.Info("释放会话", "plugin", s.PluginName, "sender", sender)
		delete(sessions, sender)
	}
}

// refreshSession 刷新会话过期时间（使用插件设置的原始时长）
func refreshSession(sender string) {
	sessionMu.Lock()
	defer sessionMu.Unlock()

	s, ok := sessions[sender]
	if !ok {
		return
	}

	s.ExpireAt = time.Now().Add(s.Duration)
	s.Timer.Reset(s.Duration)

	slog.Debug("刷新会话", "plugin", s.PluginName, "sender", sender, "expire", s.ExpireAt)
}

// newSessionTimer 创建会话超时定时器（到期时发布事件再释放）
func newSessionTimer(s *session) *time.Timer {
	return time.AfterFunc(s.Duration, func() {
		// 先发事件（session 仍在，dispatcher 过滤确保只有劫持插件收到）
		Publish(&plugin.Event{
			Topic:  "session::expired",
			Sender: s.Sender,
			Payload: &plugin.Event_SessionExpired{
				SessionExpired: &plugin.SessionExpired{
					PluginId:  s.PluginName,
					SessionId: s.Sender,
					Reason:    "timeout",
				},
			},
		})
		// 再释放
		sessionRelease(s.Sender)
	})
}

// isSessionActive 检查指定 sender 的会话是否活跃
func isSessionActive(sender string) bool {
	sessionMu.RLock()
	defer sessionMu.RUnlock()

	s, ok := sessions[sender]
	return ok && time.Now().Before(s.ExpireAt)
}

// getSessionPlugin 获取指定 sender 当前劫持会话的插件名称
func getSessionPlugin(sender string) string {
	sessionMu.RLock()
	defer sessionMu.RUnlock()

	if s, ok := sessions[sender]; ok {
		return s.PluginName
	}
	return ""
}

// isSessionAllowed 检查插件是否允许接收指定 sender 的事件
func isSessionAllowed(sender string, metadata *plugin.Metadata) bool {
	sessionMu.RLock()
	defer sessionMu.RUnlock()

	s, ok := sessions[sender]
	if !ok || !time.Now().Before(s.ExpireAt) {
		return true
	}

	if metadata.AlwaysRun {
		return true
	}

	return metadata.Name == s.PluginName
}
