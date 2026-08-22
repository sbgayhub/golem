package main

// 入站媒体懒下载：OnEvent 只登记引用（不下载、不内嵌 base64），SSE 事件带 media_ref；
// agent 对话中真需要看图/处理图时，适配器经 GET /media?ref= 取回，桥此刻才下载。
// 好处：闲聊里刷图不再白耗 CDN 下载与 SSE 大包，OnEvent 也不被下载阻塞。

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/sbgayhub/golem/sdk/message"
)

const (
	mediaRefTTL        = 2 * time.Hour    // 引用有效期；微信 CDN 链接本身也会过期，不必久留
	mediaRefMax        = 128              // 引用上限，超出淘汰最旧
	fetchMediaMaxBytes = 20 << 20         // 单个媒体 20MB 上限（HTTP 直传，不受 SSE JSON 限制）
	fetchMediaTimeout  = 15 * time.Second // 按需下载超时（在 HTTP handler 里，可比 OnEvent 宽）
)

// mediaRunID 本次插件运行的随机前缀，拼进 media_ref。
//
// 为什么必须有：适配器把取回的媒体按 ref 名缓存在磁盘上（TTL 24h），命中即直接返回。
// 而 ref 曾是纯进程内自增计数，桥重启后又从 1 开始——于是重启后 24h 内的 media_1
// 会命中上一轮遗留的旧文件，agent 拿到的是上次会话里的另一张图，且没有任何报错。
// 加运行前缀后跨重启不可能重号。
var mediaRunID = newMediaRunID()

func newMediaRunID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// 熵不可用时退回时间戳；只要不同运行间不同即可，不需要密码学强度
		return fmt.Sprintf("%x", time.Now().UnixNano()&0xffffffff)
	}
	return hex.EncodeToString(b[:])
}

var errMediaRefNotFound = errors.New("media_ref 不存在或已过期")

// cdnCand 一个可尝试的 CDN 下载候选（中图/原图/缩略图）。
type cdnCand struct {
	Which  string
	FileID string
	Key    string
}

// inboundMedia 单条入站媒体的懒下载参数与缓存。
type inboundMedia struct {
	Kind      string    // image / emoji
	Cands     []cdnCand // 图片：mid → big → thumb 依次尝试
	ImgBuf    []byte    // 图片：sync 自带缩略图，CDN 全失败时兜底
	HTTPURL   string    // 表情：真 HTTP 直链
	Data      []byte    // 首次下载成功后的缓存，重复取同一 ref 零开销
	CreatedAt time.Time
}

// registerInboundMedia 登记入站媒体，返回 media_ref（无任何可下载途径时返空）。
// 图片：host 的 buildImage 拿不到 CDN file_id（Media.Url 恒空、md5 不能当 file_id、
// 本后端标签还是 imgmsg 非 img），所以从 Raw 的 content.value 自解 XML。
func (p *BridgePlugin) registerInboundMedia(msg *message.Message) string {
	if msg == nil {
		return ""
	}
	entry := &inboundMedia{CreatedAt: time.Now()}
	switch {
	case msg.GetImage() != nil:
		entry.Kind = "image"
		raw := msg.GetRaw()
		info := parseImageCDNInfo(rawContentValue(raw))
		for _, c := range []cdnCand{
			{Which: "mid", FileID: info.MidURL, Key: info.AesKey},
			{Which: "big", FileID: info.BigURL, Key: info.AesKey},
			{Which: "thumb", FileID: info.ThumbURL, Key: info.thumbKey()},
		} {
			if c.FileID != "" && c.Key != "" {
				entry.Cands = append(entry.Cands, c)
			}
		}
		entry.ImgBuf = rawImageBuffer(raw)
		if len(entry.Cands) == 0 && len(entry.ImgBuf) == 0 {
			return ""
		}
	case msg.GetEmoji() != nil:
		// 表情的 cdnurl 是真 HTTP 地址（不同于图片的 CDN file_id）
		u := strings.TrimSpace(msg.GetEmoji().GetMedia().GetUrl())
		if !strings.HasPrefix(u, "http") {
			return ""
		}
		entry.Kind = "emoji"
		entry.HTTPURL = u
	default:
		// 语音/视频：host 未透传可用下载参数，暂不支持按需取
		return ""
	}

	ref := fmt.Sprintf("media_%s_%d", mediaRunID, p.mediaSeq.Add(1))
	p.mediaMu.Lock()
	if p.mediaRefs == nil {
		p.mediaRefs = map[string]*inboundMedia{}
	}
	p.mediaRefs[ref] = entry
	p.pruneMediaRefsLocked()
	p.mediaMu.Unlock()
	slog.Debug("[hermes_bridge] 已登记入站媒体引用",
		"ref", ref, "kind", entry.Kind, "cdn_cands", len(entry.Cands), "imgbuf_bytes", len(entry.ImgBuf))
	return ref
}

// pruneMediaRefsLocked 淘汰过期与超量引用（调用方持 mediaMu）。
func (p *BridgePlugin) pruneMediaRefsLocked() {
	now := time.Now()
	for ref, e := range p.mediaRefs {
		if now.Sub(e.CreatedAt) > mediaRefTTL {
			delete(p.mediaRefs, ref)
		}
	}
	for len(p.mediaRefs) > mediaRefMax {
		oldestRef := ""
		var oldest time.Time
		for ref, e := range p.mediaRefs {
			if oldestRef == "" || e.CreatedAt.Before(oldest) {
				oldestRef, oldest = ref, e.CreatedAt
			}
		}
		delete(p.mediaRefs, oldestRef)
	}
}

// fetchInboundMedia 按 ref 取媒体字节——此刻才真正下载；成功后缓存供重复取用。
// 下载在锁外进行，不阻塞新消息登记。
func (p *BridgePlugin) fetchInboundMedia(ref string) ([]byte, string, error) {
	p.mediaMu.Lock()
	entry := p.mediaRefs[ref]
	if entry != nil && time.Since(entry.CreatedAt) > mediaRefTTL {
		delete(p.mediaRefs, ref)
		entry = nil
	}
	var cached []byte
	if entry != nil {
		cached = entry.Data
	}
	p.mediaMu.Unlock()
	if entry == nil {
		return nil, "", errMediaRefNotFound
	}
	if len(cached) > 0 {
		return cached, entry.Kind, nil
	}

	var data []byte
	switch entry.Kind {
	case "image":
		for _, c := range entry.Cands {
			if data = p.downloadViaCDN(c.FileID, c.Key); len(data) > 0 {
				slog.Info("[hermes_bridge] 入站图片按需下载成功",
					"ref", ref, "which", c.Which, "bytes", len(data))
				break
			}
		}
		if len(data) == 0 && len(entry.ImgBuf) > 0 {
			slog.Info("[hermes_bridge] 入站图片使用 ImgBuf 缩略图兜底",
				"ref", ref, "bytes", len(entry.ImgBuf))
			data = entry.ImgBuf
		}
	case "emoji":
		d, err := p.downloadBytes(entry.HTTPURL, fetchMediaMaxBytes)
		if err != nil {
			slog.Warn("[hermes_bridge] 入站表情 HTTP 下载失败", "ref", ref, "err", err)
		} else {
			data = d
		}
	}
	if len(data) == 0 {
		return nil, entry.Kind, errors.New("媒体下载失败（CDN 与兜底均不可用）")
	}
	p.mediaMu.Lock()
	if e := p.mediaRefs[ref]; e != nil {
		e.Data = data
	}
	p.mediaMu.Unlock()
	return data, entry.Kind, nil
}

// downloadViaCDN 走全局 cdn.Instance.DownloadImage(fileID, aesKey) 流式下载。
// fileID 是 CDN 文件 ID（图片 XML 的 cdnmid/cdnbig/cdnthumb imgurl 属性值，与
// 发送图片后回包存进 Media.Url 的 file_id 同源），不是 md5。
// 带超时与大小上限，失败返 nil。
func (p *BridgePlugin) downloadViaCDN(fileID, key string) []byte {
	if strings.TrimSpace(fileID) == "" || strings.TrimSpace(key) == "" {
		return nil
	}
	if p.cdn == nil {
		slog.Warn("[hermes_bridge] cdn 能力未注入，跳过 CDN 下载")
		return nil
	}
	rc, err := p.cdn.DownloadImage(fileID, key)
	if err != nil {
		slog.Warn("[hermes_bridge] CDN 下载启动失败", "file_id", fileID[:min(8, len(fileID))], "err", err)
		return nil
	}
	defer rc.Close()

	done := make(chan struct{})
	var buf []byte
	var readErr error
	go func() {
		defer close(done)
		lr := io.LimitReader(rc, fetchMediaMaxBytes+1) // +1 检测超标
		buf, readErr = io.ReadAll(lr)
	}()
	select {
	case <-done:
		if readErr != nil {
			slog.Warn("[hermes_bridge] CDN 下载读取失败", "err", readErr)
			return nil
		}
		if len(buf) > fetchMediaMaxBytes {
			slog.Info("[hermes_bridge] CDN 下载数据超上限丢弃", "bytes", len(buf))
			return nil
		}
		if len(buf) == 0 {
			slog.Warn("[hermes_bridge] CDN 下载返回空数据")
			return nil
		}
		return buf
	case <-time.After(fetchMediaTimeout):
		slog.Warn("[hermes_bridge] CDN 下载超时", "file_id", fileID[:min(8, len(fileID))], "timeout", fetchMediaTimeout)
		return nil
	}
}
