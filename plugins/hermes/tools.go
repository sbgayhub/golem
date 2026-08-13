package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sbgayhub/golem/sdk/contact"
	"github.com/sbgayhub/golem/sdk/message"
)

// isChatroomID 判断 wxid 是否为群聊
func isChatroomID(id string) bool {
	return strings.HasSuffix(id, "@chatroom")
}

// runFromToken 按 run_token 认领当前 run 上下文，是工具调用归属判定的唯一入口。
// 令牌由 processRun 生成、只写进该 run 的 system prompt——只有那次 run 的模型
// 抄得出来。对不上（没带/带错/无活跃 run）一律返回 nil，按"无触发"档处理；
// 绝不回退到 currentRun()，否则插件外发起的 run（cron/CLI）撞车时会借走权限。
func (p *HermesPlugin) runFromToken(token string) *runContext {
	rc := p.currentRun()
	if rc != nil && token != "" && rc.Token != "" &&
		subtle.ConstantTimeCompare([]byte(token), []byte(rc.Token)) == 1 {
		return rc
	}
	return nil
}

// allowedTargets 计算当前调用可操作的目标集合，权限模型的核心：
//   - 无触发上下文（Hermes 定时/自主任务）：仅 proactive=true 的白名单目标
//   - 普通成员触发：仅触发会话本身（target 锁）
//   - 主人触发：全部白名单 + 触发会话（主人私聊可能不在白名单）
func (p *HermesPlugin) allowedTargets(rc *runContext) []Target {
	cfg := p.configSnapshot()
	if rc == nil {
		var out []Target
		for _, t := range cfg.Targets {
			if t.Proactive {
				out = append(out, t)
			}
		}
		return out
	}
	current := Target{ID: rc.TargetID, Name: rc.TargetName}
	if !rc.IsOwner {
		return []Target{current}
	}
	out := append([]Target(nil), cfg.Targets...)
	found := false
	for _, t := range out {
		if t.ID == current.ID {
			found = true
			break
		}
	}
	if !found {
		out = append(out, current)
	}
	return out
}

// resolveAllowed 校验 targetID 是否在允许集合内；空值时回退当前触发会话。
// 拒绝文案里提示 run_token：微信 run 忘带令牌被降档时，模型读到提示即可自愈重试。
func (p *HermesPlugin) resolveAllowed(rc *runContext, targetID string) (Target, error) {
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		if rc == nil {
			return Target{}, errors.New("当前按无触发场景处理，必须显式指定 target_id（可先调 wechat_list_targets）；若你在微信会话中，请携带系统提示里的 run_token 参数重试")
		}
		targetID = rc.TargetID
	}
	for _, t := range p.allowedTargets(rc) {
		if t.ID == targetID {
			return t, nil
		}
	}
	if rc == nil {
		return Target{}, fmt.Errorf("无权操作会话 %s：当前按无触发场景处理，仅可操作标记为可主动发送的会话；若你在微信会话中，请携带系统提示里的 run_token 参数重试", targetID)
	}
	return Target{}, fmt.Errorf("无权操作会话 %s：超出当前权限范围，调用 wechat_list_targets 查看可用目标", targetID)
}

// resolveReceiver 由 wxid 构造联系人（优先取缓存补全显示名）
func (p *HermesPlugin) resolveReceiver(id string) *contact.Contact {
	if p.contact != nil {
		if c := p.contact.Get("username::" + id); c != nil {
			return c
		}
	}
	return &contact.Contact{Username: id}
}

// ---- wechat_list_targets ----

// listTargetsIn 各工具通用的 run_token 入参说明见 runFromToken
type listTargetsIn struct {
	RunToken string `json:"run_token,omitempty" jsonschema:"本次会话令牌，从系统提示中原样复制；微信触发的会话必带"`
}

type listTargetsOut struct {
	Targets []targetItem `json:"targets"`
	Note    string       `json:"note"`
}

type targetItem struct {
	TargetID  string `json:"target_id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	IsCurrent bool   `json:"is_current" jsonschema:"是否为本次触发会话"`
}

func (p *HermesPlugin) toolListTargets(_ context.Context, _ *mcp.CallToolRequest, in listTargetsIn) (*mcp.CallToolResult, listTargetsOut, error) {
	rc := p.runFromToken(in.RunToken)
	items := []targetItem{}
	for _, t := range p.allowedTargets(rc) {
		typ := "private"
		if isChatroomID(t.ID) {
			typ = "chatroom"
		}
		items = append(items, targetItem{
			TargetID:  t.ID,
			Name:      t.Name,
			Type:      typ,
			IsCurrent: rc != nil && rc.TargetID == t.ID,
		})
	}
	note := "以上是你当前有权发送的全部会话。"
	if rc != nil && !rc.IsOwner {
		note = "本次由普通成员触发，你只能在当前会话内发言。"
	}
	if rc == nil {
		note = "当前按无触发场景处理，仅可发送到标记为可主动发送的会话；若你在微信会话中，请携带系统提示里的 run_token 重试。"
	}
	return nil, listTargetsOut{Targets: items, Note: note}, nil
}

// ---- wechat_send_text ----

// mentionTarget 被@的人：wxid 与文本里展示的昵称。展示名仅用于工具侧记录，
// 真正发给微信的是 wxid 列表（TextData.Reminds），顺序要与文本里 @昵称 出现顺序对齐。
type mentionTarget struct {
	TargetID    string `json:"target_id" jsonschema:"被@人的 wxid"`
	DisplayName string `json:"display_name,omitempty" jsonschema:"文本里展示的@昵称，需与 content 里出现的 @昵称 一一对应、顺序对齐"`
}

type sendTextIn struct {
	RunToken string          `json:"run_token,omitempty" jsonschema:"本次会话令牌，从系统提示中原样复制；微信触发的会话必带"`
	TargetID string          `json:"target_id,omitempty" jsonschema:"目标会话ID；回复当前对话可省略"`
	Content  string          `json:"content" jsonschema:"要发送的文本内容，一条微信消息"`
	Mentions []mentionTarget `json:"mentions,omitempty" jsonschema:"要@的人列表；群聊里才有效，私聊忽略"`
}

type sendOut struct {
	Status string `json:"status"`
}

func (p *HermesPlugin) toolSendText(_ context.Context, _ *mcp.CallToolRequest, in sendTextIn) (*mcp.CallToolResult, sendOut, error) {
	content := strings.TrimSpace(in.Content)
	if content == "" {
		return nil, sendOut{}, errors.New("content 不能为空")
	}
	maxLen := p.configSnapshot().MaxTextLen
	if maxLen > 0 && len([]rune(content)) > maxLen {
		return nil, sendOut{}, fmt.Errorf("内容超过 %d 字上限，请精简或分多条发送", maxLen)
	}
	rc := p.runFromToken(in.RunToken)
	target, err := p.resolveAllowed(rc, in.TargetID)
	if err != nil {
		return nil, sendOut{}, err
	}
	if !p.allowSend() {
		return nil, sendOut{}, errors.New("发送频率超限，请稍后再发或减少条数")
	}
	if p.message == nil {
		return nil, sendOut{}, errors.New("消息能力未注入")
	}
	receiver := p.resolveReceiver(target.ID)
	text := &message.TextData{Content: content}
	// @人：仅在群聊生效。把 mentions 的 wxid 按 content 里 @昵称 的出现顺序重排，
	// 与 universal/mention.go 的惯例一致，避免错位 @ 到别人。
	if rc != nil && rc.IsChatroom && len(in.Mentions) > 0 {
		reordered := p.alignMentions(content, in.Mentions)
		if len(reordered) > 0 {
			text.Reminds = reordered
		}
	}
	msg := &message.Message{
		Type:     message.TypeText,
		Receiver: receiver,
		Content:  content,
		Data:     &message.Message_Text{Text: text},
	}
	if _, err := p.message.Send(msg); err != nil {
		slog.Error("[hermes] MCP 发送文本失败", "target", target.ID, "err", err)
		return nil, sendOut{}, fmt.Errorf("发送失败: %w", err)
	}
	if rc != nil {
		rc.Sent.Add(1)
	}
	p.appendSession(sessionKeyOf(target.ID), chatMessage{Role: "assistant", Content: content})
	slog.Info("[hermes] MCP 发送文本", "target", target.ID, "len", len(content), "mentions", len(text.Reminds), "owner_run", rc != nil && rc.IsOwner)
	return nil, sendOut{Status: "已发送到 " + target.Name}, nil
}

// alignMentions 按 content 里各 @display_name 的出现顺序重排 mentions 的 wxid，
// 丢弃没出现在 content 里的项；未指定 display_name 的按原顺序追加到末尾。
func (p *HermesPlugin) alignMentions(content string, mentions []mentionTarget) []string {
	type slot struct {
		wxid string
		name string
	}
	var ordered []slot
	var tail []string
	for _, m := range mentions {
		m.TargetID = strings.TrimSpace(m.TargetID)
		if m.TargetID == "" {
			continue
		}
		name := strings.TrimSpace(m.DisplayName)
		if name == "" || !strings.Contains(content, "@"+name) {
			tail = append(tail, m.TargetID)
			continue
		}
		ordered = append(ordered, slot{wxid: m.TargetID, name: name})
	}
	// 按 content 里 @name 的首个出现位置排序
	// 简单冒泡即可，mentions 数量很小
	for i := 0; i < len(ordered); i++ {
		for j := i + 1; j < len(ordered); j++ {
			if strings.Index(content, "@"+ordered[j].name) < strings.Index(content, "@"+ordered[i].name) {
				ordered[i], ordered[j] = ordered[j], ordered[i]
			}
		}
	}
	out := make([]string, 0, len(ordered)+len(tail))
	for _, s := range ordered {
		out = append(out, s.wxid)
	}
	out = append(out, tail...)
	return out
}

// ---- wechat_send_image ----

type sendImageIn struct {
	RunToken string `json:"run_token,omitempty" jsonschema:"本次会话令牌，从系统提示中原样复制；微信触发的会话必带"`
	TargetID string `json:"target_id,omitempty" jsonschema:"目标会话ID；回复当前对话可省略"`
	ImageURL string `json:"image_url" jsonschema:"图片的 http/https 下载地址"`
}

func (p *HermesPlugin) toolSendImage(_ context.Context, _ *mcp.CallToolRequest, in sendImageIn) (*mcp.CallToolResult, sendOut, error) {
	rc := p.runFromToken(in.RunToken)
	target, err := p.resolveAllowed(rc, in.TargetID)
	if err != nil {
		return nil, sendOut{}, err
	}
	if !p.allowSend() {
		return nil, sendOut{}, errors.New("发送频率超限，请稍后再发")
	}
	if p.message == nil {
		return nil, sendOut{}, errors.New("消息能力未注入")
	}
	data, err := p.downloadImage(in.ImageURL)
	if err != nil {
		return nil, sendOut{}, err
	}
	outcome, upErr := p.sendImageMessage(target.ID, data)
	if outcome != uploadOK {
		// 发送失败/超时：不再把 error 抛回 agent（会让它误判本轮失败而重复兜底），
		// 改为降级发一条文本链接，并照常计入 sent，让本次会话视为有回复。
		slog.Error("[hermes] 图片发送最终失败，降级发链接", "target", target.ID, "outcome", outcome, "err", upErr)
		p.fallbackLink(target.ID, "图片", in.ImageURL, upErr)
	} else {
		slog.Info("[hermes] MCP 发送图片", "target", target.ID, "bytes", len(data))
	}
	if rc != nil {
		rc.Sent.Add(1)
	}
	p.appendSession(sessionKeyOf(target.ID), chatMessage{Role: "assistant", Content: "[发送了一张图片]"})
	return nil, sendOut{Status: "图片已发送到 " + target.Name}, nil
}

// downloadImage 下载图片，限制协议与大小
func (p *HermesPlugin) downloadImage(rawURL string) ([]byte, error) {
	rawURL = strings.TrimSpace(rawURL)
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return nil, errors.New("image_url 仅支持 http/https")
	}
	resp, err := p.dlClient.Get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("下载图片失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载图片失败，状态码 %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取图片失败: %w", err)
	}
	if len(data) > maxImageBytes {
		return nil, errors.New("图片超过 10MB 上限")
	}
	if len(data) == 0 {
		return nil, errors.New("图片内容为空")
	}
	return data, nil
}

// ---- wechat_send_voice ----

type sendVoiceIn struct {
	RunToken string `json:"run_token,omitempty" jsonschema:"本次会话令牌，从系统提示中原样复制；微信触发的会话必带"`
	TargetID string `json:"target_id,omitempty" jsonschema:"目标会话ID；回复当前对话可省略"`
	AudioURL string `json:"audio_url" jsonschema:"音频的 http/https 下载地址，支持 mp3/wav/amr/silk，会自动转微信语音格式"`
}

func (p *HermesPlugin) toolSendVoice(_ context.Context, _ *mcp.CallToolRequest, in sendVoiceIn) (*mcp.CallToolResult, sendOut, error) {
	rc := p.runFromToken(in.RunToken)
	target, err := p.resolveAllowed(rc, in.TargetID)
	if err != nil {
		return nil, sendOut{}, err
	}
	if !p.allowSend() {
		return nil, sendOut{}, errors.New("发送频率超限，请稍后再发")
	}
	if err := p.sendVoiceFromURL(target.ID, in.AudioURL); err != nil {
		slog.Error("[hermes] MCP 发送语音失败", "target", target.ID, "err", err)
		return nil, sendOut{}, err
	}
	if rc != nil {
		rc.Sent.Add(1)
	}
	p.appendSession(sessionKeyOf(target.ID), chatMessage{Role: "assistant", Content: "[发送了一条语音]"})
	slog.Info("[hermes] MCP 发送语音", "target", target.ID)
	return nil, sendOut{Status: "语音已发送到 " + target.Name}, nil
}

// ---- wechat_send_video ----

type sendVideoIn struct {
	RunToken string `json:"run_token,omitempty" jsonschema:"本次会话令牌，从系统提示中原样复制；微信触发的会话必带"`
	TargetID string `json:"target_id,omitempty" jsonschema:"目标会话ID；回复当前对话可省略"`
	VideoURL string `json:"video_url" jsonschema:"视频的 http/https 下载地址（mp4 等，100MB 以内）"`
}

func (p *HermesPlugin) toolSendVideo(_ context.Context, _ *mcp.CallToolRequest, in sendVideoIn) (*mcp.CallToolResult, sendOut, error) {
	rc := p.runFromToken(in.RunToken)
	target, err := p.resolveAllowed(rc, in.TargetID)
	if err != nil {
		return nil, sendOut{}, err
	}
	if !p.allowSend() {
		return nil, sendOut{}, errors.New("发送频率超限，请稍后再发")
	}
	if err := p.sendVideoFromURL(target.ID, in.VideoURL); err != nil {
		slog.Error("[hermes] MCP 发送视频失败", "target", target.ID, "err", err)
		return nil, sendOut{}, err
	}
	if rc != nil {
		rc.Sent.Add(1)
	}
	p.appendSession(sessionKeyOf(target.ID), chatMessage{Role: "assistant", Content: "[发送了一个视频]"})
	slog.Info("[hermes] MCP 发送视频", "target", target.ID)
	return nil, sendOut{Status: "视频已发送到 " + target.Name}, nil
}

// ---- wechat_query_history ----

type queryHistoryIn struct {
	RunToken string `json:"run_token,omitempty" jsonschema:"本次会话令牌，从系统提示中原样复制；微信触发的会话必带"`
	TargetID string `json:"target_id,omitempty" jsonschema:"要查询的会话ID，默认当前会话"`
	Hours    int    `json:"hours,omitempty" jsonschema:"查询最近多少小时，默认24，最大720"`
	Limit    int    `json:"limit,omitempty" jsonschema:"最多返回条数，默认200，最大1000"`
}

type queryHistoryOut struct {
	Count    int        `json:"count"`
	Messages []histItem `json:"messages"`
	Note     string     `json:"note,omitempty"`
}

type histItem struct {
	Time    string `json:"time"`
	Content string `json:"content"`
}

// statHistoryMsg statistics.query_messages 返回的单条记录
type statHistoryMsg struct {
	ID        int64  `json:"id"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
}

const statTimeLayout = "2006-01-02 15:04:05"

func (p *HermesPlugin) toolQueryHistory(_ context.Context, _ *mcp.CallToolRequest, in queryHistoryIn) (*mcp.CallToolResult, queryHistoryOut, error) {
	rc := p.runFromToken(in.RunToken)
	target, err := p.resolveAllowed(rc, in.TargetID)
	if err != nil {
		return nil, queryHistoryOut{}, err
	}
	if p.caller == nil {
		return nil, queryHistoryOut{}, errors.New("跨插件调用能力未注入（需要 statistics 插件）")
	}
	hours := in.Hours
	if hours <= 0 {
		hours = 24
	}
	if hours > 720 {
		hours = 720
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	args := map[string]string{
		"chatroom": target.ID,
		"since":    time.Now().Add(-time.Duration(hours) * time.Hour).Format(statTimeLayout),
		"limit":    strconv.Itoa(limit),
	}
	_, data, err := p.caller.CallPlugin("statistics.query_messages", args)
	if err != nil {
		return nil, queryHistoryOut{}, fmt.Errorf("查询历史失败: %w", err)
	}
	var msgs []statHistoryMsg
	if err := json.Unmarshal(data, &msgs); err != nil {
		return nil, queryHistoryOut{}, fmt.Errorf("解析历史失败: %w", err)
	}
	out := queryHistoryOut{Count: len(msgs)}
	for _, m := range msgs {
		content := m.Content
		if r := []rune(content); len(r) > 300 {
			content = string(r[:300]) + "…"
		}
		out.Messages = append(out.Messages, histItem{Time: m.Timestamp, Content: content})
	}
	if len(msgs) == limit {
		out.Note = "结果达到条数上限，可能不完整；可缩小 hours 或分段查询。"
	}
	return nil, out, nil
}

// ---- wechat_group_members ----

type groupMembersIn struct {
	RunToken string `json:"run_token,omitempty" jsonschema:"本次会话令牌，从系统提示中原样复制；微信触发的会话必带"`
	TargetID string `json:"target_id,omitempty" jsonschema:"群会话ID（xxx@chatroom），默认当前会话"`
}

type groupMembersOut struct {
	Count   int          `json:"count"`
	Members []memberItem `json:"members"`
}

type memberItem struct {
	Wxid string `json:"wxid"`
	Name string `json:"name"`
}

func (p *HermesPlugin) toolGroupMembers(_ context.Context, _ *mcp.CallToolRequest, in groupMembersIn) (*mcp.CallToolResult, groupMembersOut, error) {
	rc := p.runFromToken(in.RunToken)
	target, err := p.resolveAllowed(rc, in.TargetID)
	if err != nil {
		return nil, groupMembersOut{}, err
	}
	if !isChatroomID(target.ID) {
		return nil, groupMembersOut{}, errors.New("目标不是群聊会话")
	}
	if p.chatroom == nil {
		return nil, groupMembersOut{}, errors.New("群聊能力未注入")
	}
	members := p.chatroom.ListMembers(target.ID)
	out := groupMembersOut{Count: len(members)}
	for i, m := range members {
		if i >= 500 {
			break
		}
		out.Members = append(out.Members, memberItem{Wxid: m.GetUsername(), Name: displayMember(m)})
	}
	return nil, out, nil
}

// ---- wechat_send_emoji ----

type sendEmojiIn struct {
	RunToken string `json:"run_token,omitempty" jsonschema:"本次会话令牌，从系统提示中原样复制；微信触发的会话必带"`
	TargetID string `json:"target_id,omitempty" jsonschema:"目标会话ID；回复当前对话可省略"`
	ImageURL string `json:"image_url" jsonschema:"表情图的 http/https 下载地址；过大将自动压缩到 500KB 以内"`
}

func (p *HermesPlugin) toolSendEmoji(_ context.Context, _ *mcp.CallToolRequest, in sendEmojiIn) (*mcp.CallToolResult, sendOut, error) {
	rc := p.runFromToken(in.RunToken)
	target, err := p.resolveAllowed(rc, in.TargetID)
	if err != nil {
		return nil, sendOut{}, err
	}
	if !p.allowSend() {
		return nil, sendOut{}, errors.New("发送频率超限，请稍后再发")
	}
	if p.message == nil {
		return nil, sendOut{}, errors.New("消息能力未注入")
	}
	data, err := p.downloadImage(in.ImageURL)
	if err != nil {
		return nil, sendOut{}, err
	}
	data = ensureEmojiBytes(data)
	outcome, upErr := p.sendEmojiMessage(target.ID, data)
	if outcome != uploadOK {
		slog.Error("[hermes] 表情发送最终失败，降级发链接", "target", target.ID, "outcome", outcome, "err", upErr)
		p.fallbackLink(target.ID, "表情", in.ImageURL, upErr)
	} else {
		slog.Info("[hermes] MCP 发送表情", "target", target.ID, "bytes", len(data))
	}
	if rc != nil {
		rc.Sent.Add(1)
	}
	p.appendSession(sessionKeyOf(target.ID), chatMessage{Role: "assistant", Content: "[发送了一个表情]"})
	return nil, sendOut{Status: "表情已发送到 " + target.Name}, nil
}

// ---- wechat_self_info ----

type selfInfoOut struct {
	Nickname string `json:"nickname"`
	Alias    string `json:"alias,omitempty" jsonschema:"微信号"`
	Wxid     string `json:"wxid"`
}

func (p *HermesPlugin) toolSelfInfo(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, selfInfoOut, error) {
	self := p.selfSnapshot()
	if self == nil {
		return nil, selfInfoOut{}, errors.New("机器人账号信息暂不可用（contact 能力未就绪）")
	}
	return nil, selfInfoOut{
		Nickname: self.GetNickname(),
		Alias:    self.GetAlias(),
		Wxid:     self.GetUsername(),
	}, nil
}

// ---- wechat_group_info ----

type groupInfoIn struct {
	RunToken string `json:"run_token,omitempty" jsonschema:"本次会话令牌，从系统提示中原样复制；微信触发的会话必带"`
	TargetID string `json:"target_id,omitempty" jsonschema:"群会话ID（xxx@chatroom），默认当前会话"`
}

type groupInfoOut struct {
	Name        string `json:"name"`
	OwnerWxid   string `json:"owner_wxid,omitempty" jsonschema:"群主 wxid"`
	OwnerName   string `json:"owner_name,omitempty" jsonschema:"群主昵称（备注优先，其次昵称/微信号）"`
	MemberCount uint32 `json:"member_count"`
	Note        string `json:"note,omitempty"`
}

func (p *HermesPlugin) toolGroupInfo(_ context.Context, _ *mcp.CallToolRequest, in groupInfoIn) (*mcp.CallToolResult, groupInfoOut, error) {
	rc := p.runFromToken(in.RunToken)
	target, err := p.resolveAllowed(rc, in.TargetID)
	if err != nil {
		return nil, groupInfoOut{}, err
	}
	if !isChatroomID(target.ID) {
		return nil, groupInfoOut{}, errors.New("目标不是群聊会话")
	}
	if p.chatroom == nil {
		return nil, groupInfoOut{}, errors.New("群聊能力未注入")
	}
	resp, err := p.chatroom.GetInfo(target.ID)
	if err != nil {
		return nil, groupInfoOut{}, fmt.Errorf("获取群信息失败: %w", err)
	}
	ownerWxid := resp.GetOwner()
	// 微信侧 GetInfo 只回群主 wxid，补一个可读昵称（备注>昵称>微信号>wxid）给 agent，
	// 免得它拿到裸 wxid 不知道群主是谁。owner 不在联系人缓存里时留空。
	ownerName := ""
	if ownerWxid != "" && p.contact != nil {
		if c := p.resolveReceiver(ownerWxid); c != nil {
			ownerName = displayContact(c)
		}
	}
	return nil, groupInfoOut{
		Name:        resp.GetNickname(),
		OwnerWxid:   ownerWxid,
		OwnerName:   ownerName,
		MemberCount: resp.GetMemberCount(),
		Note:        "群公告与管理员列表当前微信侧接口未回传，故不包含。",
	}, nil
}

// ---- wechat_group_member_detail ----

type groupMemberDetailIn struct {
	RunToken string   `json:"run_token,omitempty" jsonschema:"本次会话令牌，从系统提示中原样复制；微信触发的会话必带"`
	TargetID string   `json:"target_id,omitempty" jsonschema:"群会话ID（xxx@chatroom），默认当前会话"`
	Wxids    []string `json:"wxids" jsonschema:"要查询详情的成员 wxid 列表"`
}

type memberDetailItem struct {
	Wxid        string `json:"wxid"`
	Nickname    string `json:"nickname,omitempty"`
	DisplayName string `json:"display_name,omitempty" jsonschema:"群内显示名"`
	AvatarURL   string `json:"avatar_url,omitempty"`
}

type groupMemberDetailOut struct {
	Count   int                `json:"count"`
	Members []memberDetailItem `json:"members"`
	Note    string             `json:"note,omitempty"`
}

func (p *HermesPlugin) toolGroupMemberDetail(_ context.Context, _ *mcp.CallToolRequest, in groupMemberDetailIn) (*mcp.CallToolResult, groupMemberDetailOut, error) {
	rc := p.runFromToken(in.RunToken)
	target, err := p.resolveAllowed(rc, in.TargetID)
	if err != nil {
		return nil, groupMemberDetailOut{}, err
	}
	if !isChatroomID(target.ID) {
		return nil, groupMemberDetailOut{}, errors.New("目标不是群聊会话")
	}
	if p.chatroom == nil {
		return nil, groupMemberDetailOut{}, errors.New("群聊能力未注入")
	}
	wxids := make([]string, 0, len(in.Wxids))
	for _, id := range in.Wxids {
		if id = strings.TrimSpace(id); id != "" {
			wxids = append(wxids, id)
		}
	}
	if len(wxids) == 0 {
		return nil, groupMemberDetailOut{}, errors.New("wxids 不能为空")
	}
	if len(wxids) > 50 {
		return nil, groupMemberDetailOut{}, errors.New("单次最多查询 50 个成员，请分批")
	}
	// 不走 chatroom.GetMembersDetail：部署中的 host 该 RPC 是 panic("implement me")
	// 占位，一调就把整个 host 打崩（2026-07-16 实测）。改从 ListMembers 缓存筛，
	// 与 wechat_group_members 同源，缓存里本就带头像 URL，够用且稳。
	want := make(map[string]bool, len(wxids))
	for _, id := range wxids {
		want[id] = true
	}
	out := groupMemberDetailOut{}
	for _, m := range p.chatroom.ListMembers(target.ID) {
		if !want[m.GetUsername()] {
			continue
		}
		out.Members = append(out.Members, memberDetailItem{
			Wxid:        m.GetUsername(),
			Nickname:    m.GetNickname(),
			DisplayName: m.GetDisplayName(),
			AvatarURL:   m.GetAvatar(),
		})
	}
	out.Count = len(out.Members)
	if out.Count < len(wxids) {
		out.Note = "部分 wxid 不在群成员缓存中，未返回。"
	}
	return nil, out, nil
}
