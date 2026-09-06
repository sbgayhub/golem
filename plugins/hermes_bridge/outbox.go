package main

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// 出站消息记账 + 撤回。
//
// 微信撤回要 (会话 wxid, new_msg_id)，而 new_msg_id 只在 message.Send 的回包里
// 出现一次——桥不记下来，事后就再没有任何办法拿回句柄。所以经桥发出的每条消息
// （文本/图片/表情/语音/视频/AppMsg 卡片）都在这里登记，供「撤回」捷径词与
// wechat_revoke 工具按「最近 N 条」或指定 id 定位。
//
// 只记自己发的：撤回本就只能撤自己的消息，记账表天然是白名单，也免得 agent 被
// 人诱导去撤别人的消息。
//
// 一个必须知道的局限：host 的 ability.Revoke 只要底层 HTTP 没报错就回 Code=0，
// 微信服务端是否真撤掉（例如已过时限）从返回值里区分不出来。故桥自己先卡时间窗
// （revoke_window_seconds，默认 120s = 微信规则），超窗直接拒绝并说明原因，而不是
// 发一次注定失败的请求再假报成功。
const (
	// maxOutboxPerChat 每会话保留的出站条数；撤回只关心最近几条，多留无用。
	maxOutboxPerChat = 24
	// outboxTTL 记账保留时长。比撤回窗口长，好让「超时了」这个原因能被说出来，
	// 而不是笼统报「没有可撤回的消息」。
	outboxTTL = 10 * time.Minute
	// defaultRevokeWindowSec 微信撤回时限（秒）。
	defaultRevokeWindowSec = 120
)

// outboxEntry 一条已发出的自己的消息。
type outboxEntry struct {
	ID      uint64    // NewMsgID，撤回唯一句柄
	Kind    string    // text / image / emoji / voice / video / card
	Preview string    // 回执与日志用，已截断
	At      time.Time // 发出时间，用于窗口判定
}

// revokeOutcome 单条撤回结果（成功或失败）。
type revokeOutcome struct {
	MessageID string `json:"message_id"`
	Kind      string `json:"kind,omitempty"`
	Preview   string `json:"preview,omitempty"`
	AgeSec    int    `json:"age_seconds"`
	Error     string `json:"error,omitempty"`
}

// recordOutbox 登记一条出站消息。newID=0（微信未上屏）或 kind 为空则不记。
//
// kind 留空是「刻意不记账」的开关：撤回回执一类系统话术走这条，否则下一次
// 「撤回」会先把提示本身撤掉。
func (p *BridgePlugin) recordOutbox(chatID, kind, preview string, newID uint64) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" || strings.TrimSpace(kind) == "" || newID == 0 {
		return
	}
	entry := outboxEntry{
		ID:      newID,
		Kind:    kind,
		Preview: clipPreview(preview),
		At:      time.Now(),
	}
	p.outMu.Lock()
	defer p.outMu.Unlock()
	if p.outbox == nil {
		p.outbox = map[string][]outboxEntry{}
	}
	list := append(pruneOutbox(p.outbox[chatID]), entry)
	if len(list) > maxOutboxPerChat {
		list = append([]outboxEntry(nil), list[len(list)-maxOutboxPerChat:]...)
	}
	p.outbox[chatID] = list
	slog.Debug("[hermes_bridge] 出站记账", "chat", chatID, "kind", kind, "newid", newID, "kept", len(list))
}

// lastOutboxID 该会话最近一条出站消息 id（十进制字符串，无则空）。
//
// 供各 send 接口回 message_id：发送是同步完成的，返回前读到的就是刚发那条。
// 同会话并发发送时可能读到另一条——只影响这个附带字段，撤回主路径按「最近 N 条」
// 定位，不依赖它。用字符串是防 JSON 大整数在 Python/JS 侧丢精度。
func (p *BridgePlugin) lastOutboxID(chatID string) string {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return ""
	}
	p.outMu.Lock()
	defer p.outMu.Unlock()
	list := p.outbox[chatID]
	if len(list) == 0 {
		return ""
	}
	return strconv.FormatUint(list[len(list)-1].ID, 10)
}

// forgetOutbox 移除若干条记账（撤成功、或确认再也撤不掉时）。
func (p *BridgePlugin) forgetOutbox(chatID string, ids ...uint64) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" || len(ids) == 0 {
		return
	}
	drop := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		drop[id] = struct{}{}
	}
	p.outMu.Lock()
	defer p.outMu.Unlock()
	list := p.outbox[chatID]
	if len(list) == 0 {
		return
	}
	kept := make([]outboxEntry, 0, len(list))
	for _, e := range list {
		if _, gone := drop[e.ID]; gone {
			continue
		}
		kept = append(kept, e)
	}
	if len(kept) == 0 {
		delete(p.outbox, chatID)
		return
	}
	p.outbox[chatID] = kept
}

// outboxCount 该会话当前可撤条数（已剔过期），供状态展示。
func (p *BridgePlugin) outboxCount(chatID string) int {
	p.outMu.Lock()
	defer p.outMu.Unlock()
	return len(pruneOutbox(p.outbox[strings.TrimSpace(chatID)]))
}

// pruneOutbox 剔掉超过 TTL 的条目。调用方须持锁。
//
// 必须**不就地改**入参：outboxCount 之类只读路径不会把结果写回 map，
// 若复用底层数组（kept := list[:0]）就会把 map 里那份切片的元素前移覆盖，
// 长度却还是旧的——表现为条目重复、最老一条凭空消失。
// 条目按时间升序追加，故最老一条没过期就等于全没过期，走零分配快路径。
func pruneOutbox(list []outboxEntry) []outboxEntry {
	if len(list) == 0 {
		return nil
	}
	cut := time.Now().Add(-outboxTTL)
	if list[0].At.After(cut) {
		return list
	}
	kept := make([]outboxEntry, 0, len(list))
	for _, e := range list {
		if e.At.After(cut) {
			kept = append(kept, e)
		}
	}
	return kept
}

func clipPreview(s string) string {
	s = singleLine(strings.TrimSpace(s))
	r := []rune(s)
	if len(r) > 40 {
		return string(r[:40]) + "…"
	}
	return s
}

// revokeWindow 撤回时间窗；<=0 视为不限（自行承担「假成功」风险）。
func (p *BridgePlugin) revokeWindow() time.Duration {
	sec := p.configSnapshot().RevokeWindowSec
	if sec == 0 {
		sec = defaultRevokeWindowSec
	}
	if sec < 0 {
		return 0
	}
	return time.Duration(sec) * time.Second
}

// pickOutbox 选出待撤条目，从新到旧。target≠0 时只找那一条。
// 不移除：撤成功后由调用方 forgetOutbox，失败的留着可重试。
func (p *BridgePlugin) pickOutbox(chatID string, count int, target uint64) []outboxEntry {
	p.outMu.Lock()
	defer p.outMu.Unlock()
	list := pruneOutbox(p.outbox[chatID])
	if len(list) == 0 {
		delete(p.outbox, chatID)
		return nil
	}
	p.outbox[chatID] = list

	if target != 0 {
		for _, e := range list {
			if e.ID == target {
				return []outboxEntry{e}
			}
		}
		return nil
	}
	if count > len(list) {
		count = len(list)
	}
	out := make([]outboxEntry, 0, count)
	for i := len(list) - 1; i >= len(list)-count; i-- {
		out = append(out, list[i])
	}
	return out
}

// revokeOutbox 撤回该会话自己发的消息：target≠0 撤指定 id，否则撤最近 count 条
// （从新到旧）。返回 (成功, 失败, 致命错误)——致命错误指连一条候选都没有。
func (p *BridgePlugin) revokeOutbox(chatID string, count int, target uint64) ([]revokeOutcome, []revokeOutcome, error) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return nil, nil, errors.New("chat_id 为空")
	}
	if p.message == nil {
		return nil, nil, errors.New("消息能力未注入")
	}
	if count <= 0 {
		count = 1
	}
	picked := p.pickOutbox(chatID, count, target)
	if len(picked) == 0 {
		if target != 0 {
			return nil, nil, fmt.Errorf("message_id %d 不在本会话的可撤记录里（只能撤桥自己发的，且桥重启或超过 %d 分钟即失效）",
				target, int(outboxTTL.Minutes()))
		}
		return nil, nil, fmt.Errorf("没有可撤回的消息（桥重启、或最近 %d 分钟内没发过东西）", int(outboxTTL.Minutes()))
	}

	window := p.revokeWindow()
	var done, failed []revokeOutcome
	for _, e := range picked {
		age := time.Since(e.At)
		out := revokeOutcome{
			MessageID: strconv.FormatUint(e.ID, 10),
			Kind:      e.Kind,
			Preview:   e.Preview,
			AgeSec:    int(age.Seconds()),
		}
		// 超窗的先拦下：微信必拒，而 host 仍会回 Code=0，发出去只会得到「假成功」。
		if window > 0 && age > window {
			out.Error = fmt.Sprintf("已过 %d 秒，超出微信 %d 秒撤回时限", out.AgeSec, int(window.Seconds()))
			failed = append(failed, out)
			p.forgetOutbox(chatID, e.ID) // 再也撤不掉，别继续占位
			slog.Info("[hermes_bridge] 撤回超窗跳过", "chat", chatID, "newid", e.ID, "age_sec", out.AgeSec)
			continue
		}
		if _, err := p.message.Revoke(chatID, e.ID); err != nil {
			out.Error = err.Error()
			failed = append(failed, out)
			slog.Warn("[hermes_bridge] 撤回失败", "chat", chatID, "newid", e.ID, "kind", e.Kind, "err", err)
			continue
		}
		p.forgetOutbox(chatID, e.ID)
		done = append(done, out)
		slog.Info("[hermes_bridge] 已撤回", "chat", chatID, "newid", e.ID, "kind", e.Kind, "age_sec", out.AgeSec)
	}
	return done, failed, nil
}

// revokeFailSummary 把失败结果拼成一句话，用于微信回执与日志。
func revokeFailSummary(failed []revokeOutcome) string {
	if len(failed) == 0 {
		return ""
	}
	parts := make([]string, 0, len(failed))
	for _, f := range failed {
		msg := f.Error
		if msg == "" {
			msg = "未知原因"
		}
		parts = append(parts, msg)
	}
	// 同因失败（常见：整批都超窗）只说一次
	if len(parts) > 1 {
		same := true
		for _, s := range parts[1:] {
			if s != parts[0] {
				same = false
				break
			}
		}
		if same {
			return fmt.Sprintf("%d 条均失败：%s", len(parts), parts[0])
		}
	}
	return strings.Join(parts, "；")
}
