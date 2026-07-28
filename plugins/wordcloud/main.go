package main

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"image/png"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gogpu/gg/text"
	"github.com/sbgayhub/golem/sdk/cdn"
	"github.com/sbgayhub/golem/sdk/chatroom"
	"github.com/sbgayhub/golem/sdk/contact"
	"github.com/sbgayhub/golem/sdk/message"
	"github.com/sbgayhub/golem/sdk/plugin"
)

//go:embed MapleMono-NF-CN-Regular.ttf
var embeddedFont []byte

// Config 插件配置
type Config struct {
	MaxWords    int     `toml:"max_words" comment:"词云最多展示的词数"`
	Width       int     `toml:"width" comment:"图片宽度（像素）"`
	Height      int     `toml:"height" comment:"图片高度（像素）"`
	MinFontSize float64 `toml:"min_font_size" comment:"最小字号"`
	MaxFontSize float64 `toml:"max_font_size" comment:"最大字号"`
	MaxMessages int     `toml:"max_messages" comment:"单次统计的消息条数上限（超出取最近）"`
	FontPath    string  `toml:"font_path" comment:"自定义字体文件路径（ttf/otf），留空使用内置字体"`
}

// WordCloudPlugin 词云插件：统计群聊历史发言并生成词云图片
type WordCloudPlugin struct {
	plugin.ConfigAbility[Config]

	message  message.Ability
	contact  contact.Ability
	chatroom chatroom.Ability
	cdn      cdn.Ability
	caller   plugin.CallerAbility

	segmenter *segmenter
	font      *text.FontSource

	// 词典/字体懒加载：首次生成词云时初始化，之后常驻直到卸载（/pm reload 会 Kill 进程，下次再懒加载）
	initMu       sync.Mutex
	initErr      error
	loadNotified atomic.Bool // 首次加载提示只发一次，避免多群并发刷屏

	mu      sync.Mutex
	running map[string]bool // 在途生成的群聊，防止同群并发刷屏
}

func main() {
	p := &WordCloudPlugin{
		ConfigAbility: plugin.ConfigAbility[Config]{
			Config: Config{
				MaxWords:    120,
				Width:       1000,
				Height:      640,
				MinFontSize: 16,
				MaxFontSize: 96,
				MaxMessages: 20000,
			},
		},
		running: make(map[string]bool),
	}
	plugin.Start(p)
}

// GetMetadata 返回插件元数据
func (p *WordCloudPlugin) GetMetadata() *plugin.Metadata {
	return &plugin.Metadata{
		Name:        "wordcloud",
		Author:      "ovo",
		Version:     "1.0.2",
		Description: "词云插件：统计群聊历史发言生成词云图片。历史发言经 statistics.query_messages 能力获取（需启用 statistics 插件），图片经 CDN 上传发送。分词词典与字体首次生成时再加载。",
		Priority:    0,
		Next:        false,
		AlwaysRun:   false,
	}
}

// GetSubscriptions 订阅文本消息，用于「词云」触发
func (p *WordCloudPlugin) GetSubscriptions() []string {
	return []string{message.TypeText.Topic}
}

// OnLoad 插件加载：不预加载词典/字体，避免启动即占用大量内存；首次生成词云时再懒加载。
func (p *WordCloudPlugin) OnLoad() error {
	slog.Info("[wordcloud] 插件加载成功（词典/字体将在首次生成时加载）")
	return nil
}

// OnUnload 插件卸载：释放字体与分词器引用（进程随卸被 Kill，内存一并回收）
func (p *WordCloudPlugin) OnUnload() error {
	if p.font != nil {
		_ = p.font.Close()
		p.font = nil
	}
	p.segmenter = nil
	slog.Info("[wordcloud] 插件已卸载")
	return nil
}

// resourcesReady 词典与字体是否已成功加载（只读检查，供发「首次稍慢」提示）
func (p *WordCloudPlugin) resourcesReady() bool {
	p.initMu.Lock()
	defer p.initMu.Unlock()
	return p.segmenter != nil && p.font != nil
}

// ensureResources 确保分词器与字体已加载。多群并发时串行化，成功后常驻；失败允许下次重试。
func (p *WordCloudPlugin) ensureResources() error {
	p.initMu.Lock()
	defer p.initMu.Unlock()
	if p.segmenter != nil && p.font != nil {
		return nil
	}

	slog.Info("[wordcloud] 开始懒加载分词词典与字体")
	seg, err := newSegmenter()
	if err != nil {
		p.initErr = fmt.Errorf("初始化分词器失败: %w", err)
		return p.initErr
	}
	font, err := p.loadFont()
	if err != nil {
		p.initErr = fmt.Errorf("加载字体失败: %w", err)
		return p.initErr
	}
	p.segmenter = seg
	p.font = font
	p.initErr = nil
	slog.Info("[wordcloud] 词典与字体加载完成", "font", font.Name())
	return nil
}

// OnEnable/OnDisable 无逻辑，但必须实现，否则不满足 Lifecycle 接口，OnLoad 不会被调用
func (p *WordCloudPlugin) OnEnable() error  { return nil }
func (p *WordCloudPlugin) OnDisable() error { return nil }

// loadFont 优先加载配置指定的字体文件，未配置或加载失败时回退内置字体
func (p *WordCloudPlugin) loadFont() (*text.FontSource, error) {
	if path := strings.TrimSpace(p.Config.FontPath); path != "" {
		font, err := text.NewFontSourceFromFile(path)
		if err == nil {
			return font, nil
		}
		slog.Warn("[wordcloud] 加载配置字体失败，回退内置字体", "path", path, "err", err)
	}
	return text.NewFontSource(embeddedFont)
}

// wordcloudHelpText 「词云帮助」回复的用法说明
const wordcloudHelpText = "【词云】用法（仅群聊）：\n" +
	"词云 → 全群近 7 天\n" +
	"词云 今日 / 昨日 / 本周 / 本月 / 全部 → 指定时间范围\n" +
	"词云 30天 → 近 N 天\n" +
	"词云 @张三 / 词云 张三 → 指定成员词云\n" +
	"可组合：词云 本周 @张三"

// OnEvent 消息事件：检测「词云」触发语，异步生成并回复
func (p *WordCloudPlugin) OnEvent(event *plugin.Event) (bool, error) {
	payload, ok := event.GetPayload().(*plugin.Event_Message)
	if !ok || payload.Message == nil {
		return false, nil
	}
	msg := payload.Message
	if msg.GetSender() == nil {
		return false, nil
	}

	if strings.TrimSpace(msg.GetContent()) == "词云帮助" {
		p.replyText(getReplyTo(msg), wordcloudHelpText)
		return true, nil
	}

	trg, triggered := parseTrigger(msg, time.Now())
	if !triggered {
		return false, nil
	}

	chatroomID := getChatroomID(msg)
	if chatroomID == "" {
		// statistics 按群记录发言，私聊没有可统计的数据
		p.replyText(msg.GetSender(), "词云仅支持在群聊中使用")
		return true, nil
	}

	if !p.tryAcquire(chatroomID) {
		p.replyText(getReplyTo(msg), "本群词云正在生成中，请稍候~")
		return true, nil
	}

	slog.Debug("[wordcloud] 触发词云生成", "chatroom", chatroomID, "label", trg.label, "name", trg.name)

	// 查询+分词+布局可能耗时较长，异步执行并立即消费事件，避免阻塞事件链
	go p.generateAndSend(msg, trg, chatroomID)
	return true, nil
}

// generateAndSend 完整生成流程：定位目标成员 → 查询历史 → 分词统计 → 渲染 → 发送。
// 在后台 goroutine 中运行，任何失败都以文本消息告知群内。
func (p *WordCloudPlugin) generateAndSend(msg *message.Message, trg trigger, chatroomID string) {
	defer p.release(chatroomID)
	defer func() {
		if r := recover(); r != nil {
			slog.Error("[wordcloud] 词云生成崩溃", "err", r)
		}
	}()

	replyTo := getReplyTo(msg)

	memberWxid, displayName, err := p.resolveTarget(msg, trg, chatroomID)
	if err != nil {
		p.replyText(replyTo, err.Error())
		return
	}

	msgs, err := p.queryHistory(chatroomID, memberWxid, trg.since, p.Config.MaxMessages)
	if err != nil {
		slog.Warn("[wordcloud] 查询历史消息失败", "err", err)
		p.replyText(replyTo, "查询历史消息失败："+err.Error())
		return
	}
	msgs = filterMessages(msgs, trg.until)
	if len(msgs) == 0 {
		p.replyText(replyTo, noMessageHint(trg, displayName))
		return
	}

	// 查询通过且确有可统计消息后，再懒加载词典/字体（无效请求不触发加载）
	if !p.resourcesReady() {
		// 提示必须在 ensureResources 阻塞加载之前发出；多群并发只提示一次
		if p.loadNotified.CompareAndSwap(false, true) {
			p.replyText(replyTo, "首次生成词云需加载分词词典与字体，可能稍慢，请稍候~")
		}
		if err := p.ensureResources(); err != nil {
			// 加载失败允许下次重试，并重新提示
			p.loadNotified.Store(false)
			slog.Warn("[wordcloud] 资源初始化失败", "err", err)
			p.replyText(replyTo, "初始化词云资源失败："+err.Error())
			return
		}
	}

	freq := p.segmenter.countWords(msgs)
	top := topN(freq, p.Config.MaxWords)
	if len(top) == 0 {
		p.replyText(replyTo, "该范围的发言分词后没有有效词汇，无法生成词云")
		return
	}

	img, err := renderWordCloud(p.font, renderOptions{
		width:   p.Config.Width,
		height:  p.Config.Height,
		minFont: p.Config.MinFontSize,
		maxFont: p.Config.MaxFontSize,
	}, top, footerText(trg, displayName, len(msgs)))
	if err != nil {
		slog.Warn("[wordcloud] 渲染词云失败", "err", err)
		p.replyText(replyTo, "渲染词云失败："+err.Error())
		return
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		slog.Warn("[wordcloud] 编码 PNG 失败", "err", err)
		p.replyText(replyTo, "编码词云图片失败")
		return
	}

	if p.cdn == nil {
		p.replyText(replyTo, "发送词云失败：cdn 能力未注入")
		return
	}
	if _, err := p.cdn.UploadImage(replyTo.GetUsername(), &buf); err != nil {
		slog.Warn("[wordcloud] 发送词云图片失败", "err", err)
		p.replyText(replyTo, "发送词云图片失败："+err.Error())
		return
	}
	slog.Debug("[wordcloud] 词云发送成功", "chatroom", chatroomID, "words", len(top), "messages", len(msgs))
}

// resolveTarget 解析词云目标成员：@ 提人的 wxid 优先（最可靠），其次按名字在群成员中
// 匹配；两者都没有则返回空 wxid，表示统计整个群。
func (p *WordCloudPlugin) resolveTarget(msg *message.Message, trg trigger, chatroomID string) (wxid, displayName string, err error) {
	selfWxid := ""
	if p.contact != nil {
		if self := p.contact.GetSelf(); self != nil {
			selfWxid = self.GetUsername()
		}
	}
	if atWxid := extractAtTargetWxid(msg, selfWxid); atWxid != "" {
		name := trg.name
		if mem, ok := p.findMemberByWxid(chatroomID, atWxid); ok {
			name = memberDisplayName(mem)
		}
		return atWxid, name, nil
	}

	if trg.name == "" {
		return "", "", nil
	}
	mem, ok := p.findMemberByName(chatroomID, trg.name)
	if !ok {
		return "", "", fmt.Errorf("未在当前群找到成员：%s（可尝试 @ 该成员）", trg.name)
	}
	return mem.GetUsername(), memberDisplayName(mem), nil
}

// extractAtTargetWxid 从消息的 @ 提人列表（atuserlist/Reminds）中取第一个非机器人
// 自身的 wxid。没有 @ 提人、或只 @ 了机器人时返回空。
func extractAtTargetWxid(msg *message.Message, selfWxid string) string {
	text := msg.GetText()
	if text == nil {
		return ""
	}
	for _, r := range text.GetReminds() {
		r = strings.TrimSpace(r)
		if r == "" || r == selfWxid {
			continue
		}
		return r
	}
	return ""
}

// findMemberByWxid 在当前群成员列表中按 wxid 精确匹配
func (p *WordCloudPlugin) findMemberByWxid(chatroomID, wxid string) (*chatroom.Member, bool) {
	if p.chatroom == nil {
		return nil, false
	}
	for _, m := range p.chatroom.ListMembers(chatroomID) {
		if m != nil && m.GetUsername() == wxid {
			return m, true
		}
	}
	return nil, false
}

// findMemberByName 在当前群成员列表中按群显示名/备注/昵称/别名/用户名匹配
func (p *WordCloudPlugin) findMemberByName(chatroomID, name string) (*chatroom.Member, bool) {
	if p.chatroom == nil {
		return nil, false
	}
	for _, m := range p.chatroom.ListMembers(chatroomID) {
		if m == nil {
			continue
		}
		for _, v := range []string{m.GetDisplayName(), m.GetRemark(), m.GetNickname(), m.GetAlias(), m.GetUsername()} {
			if strings.EqualFold(strings.TrimSpace(v), name) {
				return m, true
			}
		}
	}
	return nil, false
}

// memberDisplayName 群成员显示名：群昵称 > 备注 > 昵称 > 别名 > wxid
func memberDisplayName(m *chatroom.Member) string {
	for _, v := range []string{m.GetDisplayName(), m.GetRemark(), m.GetNickname(), m.GetAlias(), m.GetUsername()} {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// tryAcquire 标记群聊进入生成状态；已在生成中返回 false
func (p *WordCloudPlugin) tryAcquire(chatroomID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running[chatroomID] {
		return false
	}
	p.running[chatroomID] = true
	return true
}

// release 解除群聊的生成状态标记
func (p *WordCloudPlugin) release(chatroomID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.running, chatroomID)
}

// footerText 词云图片右下角脚注：时间范围 · 成员 · 发言条数 · 生成日期
func footerText(trg trigger, displayName string, msgCount int) string {
	parts := []string{trg.label}
	if displayName != "" {
		parts = append(parts, displayName)
	}
	parts = append(parts, fmt.Sprintf("%d 条发言", msgCount), time.Now().Format("2006-01-02"))
	return strings.Join(parts, " · ")
}

// noMessageHint 无可统计消息时的提示文案
func noMessageHint(trg trigger, displayName string) string {
	if displayName != "" {
		return fmt.Sprintf("%s 在「%s」范围内没有可统计的文本发言", displayName, trg.label)
	}
	return fmt.Sprintf("本群在「%s」范围内没有可统计的文本发言", trg.label)
}

// getChatroomID 获取群聊 ID；非群消息返回空。
// 兼容两种消息模型：Receiver 为群 Contact，或 Sender 直接是群账号（见插件开发指南「群聊判断」）。
func getChatroomID(msg *message.Message) string {
	if msg.GetReceiver().GetType() == contact.ContactType_CONTACT_TYPE_CHATROOM {
		return msg.GetReceiver().GetUsername()
	}
	if msg.GetSender().GetType() == contact.ContactType_CONTACT_TYPE_CHATROOM ||
		strings.HasSuffix(msg.GetSender().GetUsername(), "@chatroom") {
		return msg.GetSender().GetUsername()
	}
	return ""
}

// getReplyTo 回复目标：群消息回到群里，私聊回到发送者
func getReplyTo(msg *message.Message) *contact.Contact {
	if msg.GetReceiver().GetType() == contact.ContactType_CONTACT_TYPE_CHATROOM {
		return msg.GetReceiver()
	}
	if msg.GetSender().GetType() == contact.ContactType_CONTACT_TYPE_CHATROOM ||
		strings.HasSuffix(msg.GetSender().GetUsername(), "@chatroom") {
		return msg.GetSender()
	}
	return msg.GetSender()
}

// sendText 发送文本消息
func (p *WordCloudPlugin) sendText(receiver *contact.Contact, content string) error {
	if p.message == nil {
		return errors.New("message ability 未注入")
	}
	if receiver == nil || receiver.GetUsername() == "" {
		return errors.New("消息接收者为空")
	}
	_, err := p.message.Send(&message.Message{
		Type:     message.TypeText,
		Receiver: receiver,
		Content:  content,
		Data:     &message.Message_Text{Text: &message.TextData{Content: content}},
	})
	return err
}

// replyText 发送文本，失败时仅记录日志（后台流程中的尽力回复）
func (p *WordCloudPlugin) replyText(receiver *contact.Contact, content string) {
	if err := p.sendText(receiver, content); err != nil {
		slog.Warn("[wordcloud] 发送文本失败", "err", err)
	}
}
