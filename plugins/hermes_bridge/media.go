package main

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sbgayhub/golem/sdk/message"
)

const (
	maxVoiceBytes    = 10 << 20
	maxImageBytes    = 45 << 20
	maxVideoBytes    = 45 << 20
	maxEmojiRawBytes = 45 << 20  // 表情源下载上限；发送前压到 maxEmojiBytes
	maxEmojiBytes    = 500 << 10 // 微信表情体积约 500KB（参照 meme 插件）
	maxEmojiEdge     = 512       // 表情最长边；大图只降质量仍 1920px 时客户端常不显示

	// 正式等待：超时时长不改（不因网络慢而拉长整段体验）
	uploadImageTimeout = 45 * time.Second
	uploadVideoTimeout = 120 * time.Second
	// grace：正式超时后再多等 in-flight Send 收尾，不新开一轮（防「假超时真送达」叠发）
	uploadGrace = 30 * time.Second
	// 仅真失败（uploadFailed）可重试；超时不重试
	uploadFailedRetries = 1
	uploadRetryDelay    = time.Second
)

type uploadOutcome int

const (
	uploadOK uploadOutcome = iota
	uploadFailed
	uploadTimeout // 超时且 grace 内也未确认；不排除已达微信但回包很慢
)

// errEmojiNoNewID 表情 Send 有回包但 NewId=0：微信服务端收下却未上屏（实测 2MB 大表情必现，
// 自定义表情上传上限约 1MB）。重发同样字节无意义，callSendWithRetry 对此不重试。
var errEmojiNoNewID = errors.New("表情已回包但无 NewId（微信未上屏，通常是体积超限）")

// errAppMsgNoNewID AppMsg Send 有回包但 NewId=0：未真正上屏（XML/subType 被拒、
// 或 host 未路由到 SendApp 等）。与表情共用「不重试」语义，文案分开以免误导。
var errAppMsgNoNewID = errors.New("AppMsg 已回包但无 NewId（未真正上屏；查 sub_type/XML/host 路由）")

// callSendWithRetry 发媒体：单次 Send + 超时 grace 收尾；仅真错误重试，超时绝不再开 Send。
//
// 历史 bug：超时后 goroutine 里的 message.Send 仍在跑且常已达微信，
// 若再发一轮会叠出 2～3 张同图，Hermes 还被 HTTP 拖满 3 轮。
func (p *BridgePlugin) callSendWithRetry(perCallTimeout time.Duration, do func() error) (uploadOutcome, error) {
	var lastErr error
	for attempt := 0; attempt <= uploadFailedRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(uploadRetryDelay)
			slog.Info("[hermes_bridge] 真失败后重试发送", "attempt", attempt+1)
		}
		outcome, err := p.callOnceWithTimeout(perCallTimeout, do)
		if outcome == uploadOK {
			return uploadOK, nil
		}
		lastErr = err
		// 超时：不再新开 Send（in-flight 可能已送达）
		if outcome == uploadTimeout {
			slog.Warn("[hermes_bridge] 发送超时且 grace 内未确认，不重试", "attempt", attempt+1, "err", err)
			return uploadTimeout, lastErr
		}
		// NewId=0：服务端拒收/未上屏，重发同样内容通常无意义
		if errors.Is(err, errEmojiNoNewID) || errors.Is(err, errAppMsgNoNewID) {
			return uploadFailed, lastErr
		}
		slog.Warn("[hermes_bridge] 发送失败", "attempt", attempt+1, "err", err,
			"will_retry", attempt < uploadFailedRetries)
	}
	return uploadFailed, lastErr
}

// callOnceWithTimeout 单次同步 Send：正式等待 + 超时后 grace 收尾同一 goroutine。
// 超时不强杀 Send（避免竞态），也不在此函数内再发一次。
func (p *BridgePlugin) callOnceWithTimeout(perCallTimeout time.Duration, do func() error) (uploadOutcome, error) {
	type result struct{ err error }
	done := make(chan result, 1)
	started := time.Now()
	go func() {
		done <- result{err: do()}
	}()

	timer := time.NewTimer(perCallTimeout)
	defer timer.Stop()
	select {
	case r := <-done:
		if r.err != nil {
			return uploadFailed, r.err
		}
		return uploadOK, nil
	case <-timer.C:
		slog.Warn("[hermes_bridge] 发送超时，等待 in-flight 收尾（不重发）",
			"waited", perCallTimeout, "grace", uploadGrace)
	}

	// grace：只等已经在跑的那一次，绝不新开 Send
	graceTimer := time.NewTimer(uploadGrace)
	defer graceTimer.Stop()
	select {
	case r := <-done:
		elapsed := time.Since(started)
		if r.err != nil {
			slog.Warn("[hermes_bridge] in-flight 收尾为失败", "elapsed", elapsed, "err", r.err)
			return uploadFailed, r.err
		}
		slog.Info("[hermes_bridge] in-flight 收尾成功（慢回包，已当成功）", "elapsed", elapsed)
		return uploadOK, nil
	case <-graceTimer.C:
		// goroutine 仍在跑；可能后续仍会送到微信，但不再等、不重发
		slog.Warn("[hermes_bridge] grace 也已过，结果未确认（不重发）",
			"elapsed", time.Since(started))
		return uploadTimeout, errors.New("发送超时（结果未确认，可能已送达）")
	}
}

// sendAppMessage 发 AppMsg 卡片（音乐/链接/聊天记录/引用等）。
//
// 业务侧（搜歌、选 AppID、构造 XML）全在 Hermes 侧；桥只补数据通道：
// 读取适配器传来的 sub_type 与整段 XML，经 message.Send(…, AppData)
// 走 host 的 SendApp 推送。防叠发与媒体同用 callSendWithRetry（超时不重开 Send）。
//
// 参考 plugins/music：TypeAppMusic + AppData{SubType=76, Xml=<appmsg appid=…>}。
// type=19 → TypeAppChatRecord；type=57 → TypeAppMusic + SubType=57（host 不认 TypeAppQuote）。
func (p *BridgePlugin) sendAppMessage(receiver string, subType uint32, xml string) (uploadOutcome, error) {
	if p.message == nil {
		return uploadFailed, errors.New("消息能力未注入")
	}
	receiver = strings.TrimSpace(receiver)
	if receiver == "" {
		return uploadFailed, errors.New("接收方为空")
	}
	xml = strings.TrimSpace(xml)
	if xml == "" {
		return uploadFailed, errors.New("AppMsg XML 为空")
	}
	// Content 设个简短展示文本，便于宿主侧日志可读；卡片实际内容以 XML 为准。
	//
	// host ability.Send 仅对 TypeApplication / TypeAppChatRecord / TypeAppMusic
	// 走 SendApp(xml, subType)（见 host/ability/message/message.go）。
	// TypeAppQuote(4957) **不在该 case**，会 default 丢弃 → NewId=0 假失败。
	// 故出站引用必须用已支持的 Type + SubType=57，不能设 TypeAppQuote。
	// （入站订阅仍用 TypeAppQuote.Topic，与此无关。）
	content := "[AppMsg]"
	msgType := message.TypeAppMusic
	switch subType {
	case 19:
		content = "[聊天记录]"
		msgType = message.TypeAppChatRecord
	case 57:
		content = "[引用]"
		// 故意不用 TypeAppQuote：host 未路由；TypeAppMusic 会 SendApp(..., 57)
		msgType = message.TypeAppMusic
	}
	msg := &message.Message{
		Type:     msgType,
		Receiver: p.resolveReceiver(receiver),
		Content:  content,
		Data: &message.Message_App{App: &message.AppData{
			SubType: subType,
			Xml:     xml,
		}},
	}
	return p.callSendWithRetry(uploadImageTimeout, func() error {
		resp, err := p.message.Send(msg)
		if err != nil {
			return err
		}
		// 有回包但 NewId=0 视为未上屏（XML 被拒 / 类型未路由等）。
		if resp != nil && resp.GetNewId() == 0 {
			slog.Warn("[hermes_bridge] AppMsg Send 无 NewId（未真正上屏）",
				"sub_type", subType, "msg_type", msgType.GetCode())
			return errAppMsgNoNewID
		}
		return nil
	})
}

func (p *BridgePlugin) sendImageMessage(receiver string, data []byte) (uploadOutcome, error) {
	_, outcome, err := p.sendImageMessageWithMeta(receiver, data)
	return outcome, err
}

// sendImageMessageWithMeta 对 receiver 发图并返回 CDN 字段（FileId→Media.Url, AesKey→Media.Key）。
// 记录嵌图 A 路径：会话内发图拿身份，再写入 datatype=2。
func (p *BridgePlugin) sendImageMessageWithMeta(receiver string, data []byte) (cdnUploadMeta, uploadOutcome, error) {
	var zero cdnUploadMeta
	if p.message == nil {
		return zero, uploadFailed, errors.New("消息能力未注入")
	}
	if len(data) == 0 {
		return zero, uploadFailed, errors.New("图片数据为空")
	}
	msg := &message.Message{
		Type:     message.TypeImage,
		Receiver: p.resolveReceiver(receiver),
		Content:  "[图片]",
		Data:     &message.Message_Image{Image: &message.ImageData{Media: &message.Media{Data: data}}},
	}
	var meta cdnUploadMeta
	outcome, err := p.callSendWithRetry(uploadImageTimeout, func() error {
		resp, e := p.message.Send(msg)
		if e != nil {
			return e
		}
		if resp == nil {
			return fmt.Errorf("SendImage 回包空")
		}
		meta.NewMsgID = resp.GetNewId()
		if m := resp.GetMedia(); m != nil {
			meta.FileID = strings.TrimSpace(m.GetUrl())
			meta.AesKey = strings.TrimSpace(m.GetKey())
			meta.FileSize = m.GetSize()
			meta.FileMD5 = strings.TrimSpace(m.GetMd5())
		}
		if meta.FileID == "" || meta.AesKey == "" {
			return fmt.Errorf("SendImage 回包缺少 file_id/aes_key (NewId=%d)", meta.NewMsgID)
		}
		if meta.NewMsgID == 0 {
			slog.Warn("[hermes_bridge] SendImage 无 NewId（可能未上屏）",
				"receiver", receiver, "file_id_len", len(meta.FileID))
		}
		return nil
	})
	if outcome != uploadOK {
		return zero, outcome, err
	}
	if meta.FileMD5 == "" {
		meta.FileMD5 = md5Hex(data)
	}
	if meta.FileSize == 0 {
		meta.FileSize = uint32(len(data))
	}
	return meta, uploadOK, nil
}

// sendEmojiMessage 经 message.Send 发表情（TypeEmoji, Code 47），不是普通图片。
// 调用方应先 ensureEmojiBytes；防叠发与图片同用 callSendWithRetry（超时不重开 Send）。
// host SendEmoji(receiver, md5, data) 需要 Media.Md5；空 md5 时部分路径回 OK 但客户端不渲染。
func (p *BridgePlugin) sendEmojiMessage(receiver string, data []byte) (uploadOutcome, error) {
	if p.message == nil {
		return uploadFailed, errors.New("消息能力未注入")
	}
	sum := md5.Sum(data)
	md5hex := hex.EncodeToString(sum[:])
	msg := &message.Message{
		Type:     message.TypeEmoji,
		Receiver: p.resolveReceiver(receiver),
		Content:  "[表情]",
		Data: &message.Message_Emoji{Emoji: &message.EmojiData{
			Media: &message.Media{
				Md5:  md5hex,
				Data: data,
				Size: uint32(len(data)),
			},
		}},
	}
	return p.callSendWithRetry(uploadImageTimeout, func() error {
		resp, err := p.message.Send(msg)
		if err != nil {
			return err
		}
		// 有回包无 NewId：微信侧「假成功」不上屏（实测 2MB 大表情必现）。
		// 必须当真失败返回，否则适配器/agent 以为发出去了，群里却什么都没有。
		if resp != nil && resp.GetNewId() == 0 {
			slog.Warn("[hermes_bridge] 表情 Send 无 NewId（未真正上屏）",
				"bytes", len(data), "md5", md5hex)
			return errEmojiNoNewID
		}
		return nil
	})
}

// sendEmojiByMd5 仅通过 md5 引用发送已收藏的表情（不传数据字节）。
// 适用场景：微信群里已流通过的表情（CDN 上有原文件），Hermes 侧只需记 md5，
// 发送时传 md5 即可——微信直接用 CDN 原文件上屏，保原图画质与动画。
func (p *BridgePlugin) sendEmojiByMd5(receiver string, md5hex string) (uploadOutcome, error) {
	if p.message == nil {
		return uploadFailed, errors.New("消息能力未注入")
	}
	md5hex = strings.TrimSpace(md5hex)
	if md5hex == "" {
		return uploadFailed, errors.New("md5 为空")
	}
	msg := &message.Message{
		Type:     message.TypeEmoji,
		Receiver: p.resolveReceiver(receiver),
		Content:  "[表情]",
		Data: &message.Message_Emoji{Emoji: &message.EmojiData{
			Media: &message.Media{Md5: md5hex},
		}},
	}
	return p.callSendWithRetry(uploadImageTimeout, func() error {
		resp, err := p.message.Send(msg)
		if err != nil {
			return err
		}
		// NewId=0：微信未识别该 md5（该表情从未在当前账号流通过）
		if resp != nil && resp.GetNewId() == 0 {
			slog.Warn("[hermes_bridge] 表情 md5 引用发送无 NewId（md5 未被微信识别）",
				"md5", md5hex)
			return errEmojiNoNewID
		}
		return nil
	})
}

// ensureEmojiBytes 把表情控在「体积 + 边长」可展示范围。
// GIF 动图优先走 compressGIF 保动画；失败或非 GIF 再走静图路径（JPEG，丢动画）。
// 大风景图仅降 JPEG 质量会仍是 1920px：协议层可能 OK，手机却不出表情气泡。
// 策略：超边长先缩，再逐级降质量；仍超则继续缩小。
func ensureEmojiBytes(data []byte) []byte {
	if isGIFData(data) {
		cfg, _, cfgErr := image.DecodeConfig(bytes.NewReader(data))
		oversize := len(data) > maxEmojiBytes ||
			(cfgErr == nil && (cfg.Width > maxEmojiEdge || cfg.Height > maxEmojiEdge))
		if !oversize {
			return data
		}
		if out := compressGIF(data); out != nil {
			return out
		}
		slog.Warn("[hermes_bridge] GIF 保动画压缩失败，降级为静图", "bytes", len(data))
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		if len(data) > maxEmojiBytes {
			slog.Warn("[hermes_bridge] 表情体积超限且解码失败，原样发送", "bytes", len(data), "err", err)
		}
		return data
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 1 || h < 1 {
		return data
	}
	maxSide := w
	if h > maxSide {
		maxSide = h
	}
	// 小图且已 ≤500KB：不动
	if maxSide <= maxEmojiEdge && len(data) <= maxEmojiBytes {
		return data
	}

	scale := 1.0
	if maxSide > maxEmojiEdge {
		scale = float64(maxEmojiEdge) / float64(maxSide)
	}
	for attempt := 0; attempt < 8; attempt++ {
		nw := int(float64(w) * scale)
		nh := int(float64(h) * scale)
		if nw < 1 {
			nw = 1
		}
		if nh < 1 {
			nh = 1
		}
		scaled := resizeBox(img, nw, nh)
		// PNG 无损优先：保透明、无块状伪影；超限再退 JPEG 逐级降质量
		var pngBuf bytes.Buffer
		if err := png.Encode(&pngBuf, scaled); err == nil && pngBuf.Len() <= maxEmojiBytes {
			slog.Info("[hermes_bridge] 表情已压缩(PNG)",
				"from", len(data),
				"to", pngBuf.Len(),
				"edge", fmt.Sprintf("%dx%d→%dx%d", w, h, nw, nh),
			)
			return pngBuf.Bytes()
		}
		for quality := 85; quality >= 30; quality -= 15 {
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, scaled, &jpeg.Options{Quality: quality}); err != nil {
				continue
			}
			if buf.Len() <= maxEmojiBytes {
				slog.Info("[hermes_bridge] 表情已压缩(JPEG)",
					"from", len(data),
					"to", buf.Len(),
					"quality", quality,
					"edge", fmt.Sprintf("%dx%d→%dx%d", w, h, nw, nh),
				)
				return buf.Bytes()
			}
		}
		scale *= 0.75
	}
	slog.Warn("[hermes_bridge] 表情压不到上限，原样发送", "bytes", len(data), "edge", fmt.Sprintf("%dx%d", w, h))
	return data
}

// resizeNearest 近邻缩放，无额外依赖；GIF 帧专用（不产生新颜色，保调色板精确）。
func resizeNearest(src image.Image, nw, nh int) *image.RGBA {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	if sw < 1 || sh < 1 || nw < 1 || nh < 1 {
		return dst
	}
	for y := 0; y < nh; y++ {
		sy := sb.Min.Y + y*sh/nh
		for x := 0; x < nw; x++ {
			sx := sb.Min.X + x*sw/nw
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

func isGIFData(data []byte) bool {
	return len(data) >= 4 && string(data[:4]) == "GIF8"
}

// resizeBox 面积平均缩小，静图路径用（近邻缩小对细纹理会出锯齿/摩尔纹）。
// 仅为缩小设计；目标不小于源时退化为近邻。
func resizeBox(src image.Image, nw, nh int) *image.RGBA {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	if nw >= sw || nh >= sh || nw < 1 || nh < 1 {
		return resizeNearest(src, nw, nh)
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy0 := sb.Min.Y + y*sh/nh
		sy1 := sb.Min.Y + (y+1)*sh/nh
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		for x := 0; x < nw; x++ {
			sx0 := sb.Min.X + x*sw/nw
			sx1 := sb.Min.X + (x+1)*sw/nw
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			var r, g, b, a, n uint64
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					pr, pg, pb, pa := src.At(sx, sy).RGBA()
					r += uint64(pr)
					g += uint64(pg)
					b += uint64(pb)
					a += uint64(pa)
					n++
				}
			}
			dst.SetRGBA(x, y, color.RGBA{
				uint8(r / n >> 8), uint8(g / n >> 8), uint8(b / n >> 8), uint8(a / n >> 8),
			})
		}
	}
	return dst
}

// compressGIF 保动画压缩：增量帧先合成完整画面，再缩放、按需抽帧、重编码，
// 逐级缩小直到 ≤maxEmojiBytes。失败返回 nil，调用方降级静图。
func compressGIF(data []byte) []byte {
	src, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil || len(src.Image) == 0 {
		return nil
	}
	w, h := src.Config.Width, src.Config.Height
	if w < 1 || h < 1 {
		b := src.Image[0].Bounds()
		w, h = b.Max.X, b.Max.Y
	}
	if w < 1 || h < 1 {
		return nil
	}
	frames, delays := flattenGIF(src, w, h)
	if len(frames) == 0 {
		return nil
	}

	maxSide := w
	if h > maxSide {
		maxSide = h
	}
	scale := 1.0
	if maxSide > maxEmojiEdge {
		scale = float64(maxEmojiEdge) / float64(maxSide)
	}
	step := 1 // 抽帧步长：先缩分辨率，不够再隔帧丢（Delay 顺延，节奏不变）
	for attempt := 0; attempt < 7; attempt++ {
		nw := int(float64(w) * scale)
		nh := int(float64(h) * scale)
		if nw < 1 {
			nw = 1
		}
		if nh < 1 {
			nh = 1
		}
		out := &gif.GIF{
			LoopCount: src.LoopCount,
			Config:    image.Config{Width: nw, Height: nh},
		}
		for i := 0; i < len(frames); i += step {
			// 面积平均缩放：近邻缩小会把源 GIF 自带的抖动纹理混叠成粗大横竖网点
			scaled := resizeBox(frames[i], nw, nh)
			pf := palettedFrame(scaled)
			d := 0
			for j := i; j < len(frames) && j < i+step; j++ {
				d += delays[j]
			}
			out.Image = append(out.Image, pf)
			out.Delay = append(out.Delay, d)
			out.Disposal = append(out.Disposal, gif.DisposalNone)
		}
		var buf bytes.Buffer
		if err := gif.EncodeAll(&buf, out); err != nil {
			slog.Warn("[hermes_bridge] GIF 重编码失败", "err", err)
			return nil
		}
		if buf.Len() <= maxEmojiBytes {
			slog.Info("[hermes_bridge] GIF 已保动画压缩",
				"from", len(data), "to", buf.Len(),
				"edge", fmt.Sprintf("%dx%d→%dx%d", w, h, nw, nh),
				"frames", fmt.Sprintf("%d→%d", len(frames), len(out.Image)))
			return buf.Bytes()
		}
		// 前两轮只缩分辨率；仍超则开始抽帧（保底 4 帧以上才继续抽）
		if attempt >= 1 && step < 4 && len(frames)/(step+1) >= 4 {
			step++
		}
		scale *= 0.75
	}
	return nil
}

// palettedFrame 把缩放后的 RGBA 帧转为 Paletted 帧。
// 按帧实际颜色建板：≤256 色时精确逐像素映射（零误差），超 256 色才中位切分量化。
// 两条路径都**不做 Floyd-Steinberg 抖动**：表情尺寸小，抖动出来就是可见的
// 横竖噪点（用户实际投诉点），且噪点让 LZW 压缩率大幅变差、被迫缩得更小；
// 256 色无抖动的轻微色带在表情上几乎不可见。
// （历史：旧版拿源 GIF 增量帧的局部调色板给完整画面配色再抖动，满屏噪点。）
func palettedFrame(rgba *image.RGBA) *image.Paletted {
	b := rgba.Bounds()
	idx := make(map[color.RGBA]uint8)
	var pal color.Palette
	exact := true
scan:
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := rgba.RGBAAt(x, y)
			if c.A < 128 {
				c = color.RGBA{} // 统一透明色，编码器据 A=0 写透明索引
			}
			if _, ok := idx[c]; ok {
				continue
			}
			if len(pal) >= 256 {
				exact = false
				break scan
			}
			idx[c] = uint8(len(pal))
			pal = append(pal, c)
		}
	}
	pf := image.NewPaletted(b, pal)
	if exact {
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				c := rgba.RGBAAt(x, y)
				if c.A < 128 {
					c = color.RGBA{}
				}
				pf.SetColorIndex(x, y, idx[c])
			}
		}
		return pf
	}
	pf.Palette = append(color.Palette{color.RGBA{}}, medianCutPalette(rgba, 255)...)
	draw.Draw(pf, b, rgba, b.Min, draw.Src) // 最近色映射，不抖动
	return pf
}

// medianCutPalette 对帧内不透明像素做中位切分量化，返回至多 n 色（均不透明）。
func medianCutPalette(rgba *image.RGBA, n int) color.Palette {
	b := rgba.Bounds()
	weight := make(map[color.RGBA]int)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if c := rgba.RGBAAt(x, y); c.A >= 128 {
				weight[c]++
			}
		}
	}
	colors := make([]color.RGBA, 0, len(weight))
	for c := range weight {
		colors = append(colors, c)
	}
	boxes := [][]color.RGBA{colors}
	for len(boxes) < n {
		bi, bch, brange := -1, 0, 0
		for i, box := range boxes {
			if len(box) < 2 {
				continue
			}
			if ch, rg := widestChannel(box); rg > brange {
				bi, bch, brange = i, ch, rg
			}
		}
		if bi < 0 {
			break
		}
		box := boxes[bi]
		ch := bch
		sort.Slice(box, func(a, b int) bool { return rgbaChannel(box[a], ch) < rgbaChannel(box[b], ch) })
		mid := len(box) / 2
		boxes[bi] = box[:mid]
		boxes = append(boxes, box[mid:])
	}
	pal := make(color.Palette, 0, len(boxes))
	for _, box := range boxes {
		var r, g, bl, cnt int
		for _, c := range box {
			w := weight[c]
			r += int(c.R) * w
			g += int(c.G) * w
			bl += int(c.B) * w
			cnt += w
		}
		if cnt == 0 {
			continue
		}
		pal = append(pal, color.RGBA{uint8(r / cnt), uint8(g / cnt), uint8(bl / cnt), 255})
	}
	return pal
}

func rgbaChannel(c color.RGBA, ch int) uint8 {
	switch ch {
	case 0:
		return c.R
	case 1:
		return c.G
	}
	return c.B
}

// widestChannel 返回颜色箱内极差最大的通道及其极差。
func widestChannel(box []color.RGBA) (int, int) {
	lo := [3]uint8{255, 255, 255}
	var hi [3]uint8
	for _, c := range box {
		for ch := 0; ch < 3; ch++ {
			v := rgbaChannel(c, ch)
			if v < lo[ch] {
				lo[ch] = v
			}
			if v > hi[ch] {
				hi[ch] = v
			}
		}
	}
	bestCh, best := 0, -1
	for ch := 0; ch < 3; ch++ {
		if d := int(hi[ch]) - int(lo[ch]); d > best {
			best, bestCh = d, ch
		}
	}
	return bestCh, best
}

// flattenGIF 把 GIF 增量帧按 Disposal 规则合成为逐帧完整画面。
// 不合成直接缩放增量帧会花屏（帧只覆盖变化区域）。
func flattenGIF(g *gif.GIF, w, h int) ([]*image.RGBA, []int) {
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	frames := make([]*image.RGBA, 0, len(g.Image))
	delays := make([]int, 0, len(g.Image))
	for i, frame := range g.Image {
		if frame == nil {
			continue
		}
		disposal := byte(gif.DisposalNone)
		if i < len(g.Disposal) {
			disposal = g.Disposal[i]
		}
		var backup *image.RGBA
		if disposal == gif.DisposalPrevious {
			backup = image.NewRGBA(canvas.Rect)
			copy(backup.Pix, canvas.Pix)
		}
		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)
		snap := image.NewRGBA(canvas.Rect)
		copy(snap.Pix, canvas.Pix)
		frames = append(frames, snap)
		d := 10
		if i < len(g.Delay) && g.Delay[i] > 0 {
			d = g.Delay[i]
		}
		delays = append(delays, d)
		switch disposal {
		case gif.DisposalBackground:
			clearRGBARect(canvas, frame.Bounds())
		case gif.DisposalPrevious:
			canvas = backup
		}
	}
	return frames, delays
}

// clearRGBARect 把画布指定区域清为透明（DisposalBackground 用）。
func clearRGBARect(img *image.RGBA, r image.Rectangle) {
	r = r.Intersect(img.Rect)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := img.Pix[img.PixOffset(r.Min.X, y):img.PixOffset(r.Max.X, y)]
		for i := range row {
			row[i] = 0
		}
	}
}

func (p *BridgePlugin) downloadImage(rawURL string) ([]byte, error) {
	return p.downloadBytes(rawURL, maxImageBytes)
}

func (p *BridgePlugin) downloadEmoji(rawURL string) ([]byte, error) {
	return p.downloadBytes(rawURL, maxEmojiRawBytes)
}

func (p *BridgePlugin) downloadBytes(rawURL string, maxBytes int64) ([]byte, error) {
	rawURL = strings.TrimSpace(rawURL)
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return nil, errors.New("仅支持 http/https 地址")
	}
	client := p.dlClient
	if maxBytes > maxImageBytes {
		client = p.mediaClient
	}
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("下载失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载失败，状态码 %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取失败: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("文件超过 %dMB 上限", maxBytes>>20)
	}
	if len(data) == 0 {
		return nil, errors.New("下载内容为空")
	}
	return data, nil
}

func (p *BridgePlugin) downloadToTemp(rawURL, pattern string, maxBytes int64) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return "", errors.New("仅支持 http/https 地址")
	}
	resp, err := p.mediaClient.Get(rawURL)
	if err != nil {
		return "", fmt.Errorf("下载失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载失败，状态码 %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", fmt.Errorf("创建临时文件失败: %w", err)
	}
	n, err := io.Copy(tmp, io.LimitReader(resp.Body, maxBytes+1))
	closeErr := tmp.Close()
	if err != nil || closeErr != nil {
		_ = os.Remove(tmp.Name())
		return "", errors.New("保存临时文件失败")
	}
	if n > maxBytes {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("文件超过 %dMB 上限", maxBytes>>20)
	}
	if n == 0 {
		_ = os.Remove(tmp.Name())
		return "", errors.New("下载内容为空")
	}
	return tmp.Name(), nil
}

func (p *BridgePlugin) sendVoiceBytes(targetID string, srcData []byte) error {
	if p.message == nil {
		return errors.New("消息能力未注入")
	}
	// 写临时文件以便 ffmpeg / ffprobe
	srcPath, err := writeTemp("hermes-bridge-audio-*", srcData)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(srcPath) }()

	srcFormatCode := detectAudioFormatCode(srcData)
	slog.Debug("[hermes_bridge] 语音源文件信息",
		"src_format", srcFormatCode,
		"src_size", len(srcData),
		"src_header", hexHeader(srcData, 16),
	)

	// 转码前从源文件取时长（silk 文件 ffprobe 取不到，必须提前取）
	durationMs, err := mediaDurationMs(srcPath)
	if err != nil {
		slog.Warn("[hermes_bridge] 获取源文件时长失败，使用默认值", "err", err)
		durationMs = 5000
	}

	voicePath := srcPath
	converted := false
	if srcFormatCode != 0 && srcFormatCode != 4 {
		// 优先尝试 SILK，失败或无配置则降级到 AMR
		var silkMs int
		voicePath, silkMs, err = p.tryConvertToSILK(srcPath, durationMs)
		if err != nil {
			slog.Warn("[hermes_bridge] silk 编码失败，降级到 amr", "err", err)
			voicePath, err = tryConvertToAMRFile(srcPath)
			if err != nil {
				slog.Error("[hermes_bridge] 语音转码全部失败", "err", err)
				return fmt.Errorf("音频转码失败（宿主机需安装 ffmpeg / silk_v3_encoder）: %w", err)
			}
		} else {
			durationMs = silkMs
		}
		converted = true
		defer os.Remove(voicePath)
	}

	voiceData, err := os.ReadFile(voicePath)
	if err != nil {
		return fmt.Errorf("读取语音失败: %w", err)
	}
	voiceData = ensureTencentSilk(voiceData)

	finalFmt := int32(detectAudioFormatCode(voiceData))
	slog.Debug("[hermes_bridge] 语音准备发送",
		"converted", converted,
		"final_format", finalFmt,
		"size", len(voiceData),
		"duration_ms", durationMs,
		"header_hex", hexHeader(voiceData, 16),
	)

	voice := &message.VoiceData{
		Media:    &message.Media{Data: voiceData},
		Duration: uint32(durationMs),
	}
	// host 现状：Format 字段非空即按 4=SILK 发送，为空按 0=AMR 发送（与字段值无关）。
	// 因此只在 SILK 数据时设置 Format，AMR 保持空，两条路径均正确。
	if finalFmt == 4 {
		voice.Format = &finalFmt
	}

	msg := &message.Message{
		Type:     message.TypeVoice,
		Receiver: p.resolveReceiver(targetID),
		Content:  "[语音]",
		Data:     &message.Message_Voice{Voice: voice},
	}
	if _, err := p.message.Send(msg); err != nil {
		if strings.Contains(err.Error(), "code: -104") {
			slog.Warn("[hermes_bridge] 语音发送返回 -104（经验证实际已送达）",
				"err", err,
				"converted", converted,
				"duration_ms", durationMs,
				"size", len(voiceData),
				"final_format", finalFmt,
			)
			return nil
		}
		slog.Error("[hermes_bridge] 发送语音失败",
			"err", err,
			"converted", converted,
			"duration_ms", durationMs,
			"size", len(voiceData),
			"final_format", finalFmt,
		)
		return fmt.Errorf("发送语音失败: %w", err)
	}
	return nil
}

// sendVideoMessage 经 message.Send 发视频（Duration + Thumb + 本体），返回 outcome 供诊断/降级。
// host 需要 Thumb + Duration + Media.Data；早期只塞视频字节、不抽封面时会失败。
func (p *BridgePlugin) sendVideoMessage(targetID string, videoData []byte) (uploadOutcome, error) {
	if p.message == nil {
		return uploadFailed, errors.New("消息能力未注入")
	}
	videoPath, err := writeTemp("hermes-bridge-video-*.mp4", videoData)
	if err != nil {
		return uploadFailed, err
	}
	defer func() { _ = os.Remove(videoPath) }()

	durationSec, err := mediaDurationSec(videoPath)
	if err != nil {
		slog.Warn("[hermes_bridge] 获取视频时长失败，使用默认值", "err", err)
		durationSec = 10
	}

	thumbData, err := extractVideoThumbnail(videoPath)
	if err != nil {
		slog.Warn("[hermes_bridge] 提取视频缩略图失败，使用空缩略图", "err", err)
		thumbData = nil
	}
	slog.Info("[hermes_bridge] 准备发送视频",
		"target", targetID,
		"bytes", len(videoData),
		"duration_sec", durationSec,
		"thumb_bytes", len(thumbData),
	)

	msg := &message.Message{
		Type:     message.TypeVideo,
		Receiver: p.resolveReceiver(targetID),
		Content:  "[视频]",
		Data: &message.Message_Video{Video: &message.VideoData{
			Media:    &message.Media{Data: videoData},
			Duration: uint32(durationSec),
			Thumb:    thumbData,
		}},
	}
	return p.callSendWithRetry(uploadVideoTimeout, func() error {
		_, err := p.message.Send(msg)
		return err
	})
}

// sendVideoBytes HTTP 出站用：失败且有 URL 时降级发链接（含超时未确认，由调用方保留该策略）。
func (p *BridgePlugin) sendVideoBytes(targetID string, videoData []byte, fallbackURL string) error {
	// 视频与图片一样走 message.Send（host 底层 messageapi.SendVideo）。
	// 历史：cdn.Upload* 在部分环境偶发 RST，旧 hermes 实测 message.Send 更稳。
	outcome, upErr := p.sendVideoMessage(targetID, videoData)
	if outcome == uploadOK {
		return nil
	}
	slog.Error("[hermes_bridge] 视频发送最终失败",
		"target", targetID, "outcome", outcome, "bytes", len(videoData), "err", upErr)
	if fallbackURL != "" {
		p.fallbackLink(targetID, "视频", fallbackURL, upErr)
		return nil
	}
	return upErr
}

func (p *BridgePlugin) fallbackLink(targetID, kind, rawURL string, cause error) {
	if p.message == nil {
		return
	}
	slog.Info("[hermes_bridge] 媒体上传降级发链接", "kind", kind, "target", targetID, "url", rawURL, "cause", cause)
	txt := fmt.Sprintf("（%s发送失败，看链接吧：%s）", kind, rawURL)
	_ = p.sendPlainText(p.resolveReceiver(targetID), txt)
}

func (p *BridgePlugin) sendPlainTextWithReminds(targetID, content string, reminds []string) error {
	if p.message == nil {
		return errors.New("消息能力未注入")
	}
	text := &message.TextData{Content: content}
	if len(reminds) > 0 {
		text.Reminds = reminds
	}
	msg := &message.Message{
		Type:     message.TypeText,
		Receiver: p.resolveReceiver(targetID),
		Content:  content,
		Data:     &message.Message_Text{Text: text},
	}
	_, err := p.message.Send(msg)
	return err
}

func writeTemp(pattern string, data []byte) (string, error) {
	tmp, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", fmt.Errorf("创建临时文件失败: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("写入临时文件失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

func detectAudioFormatCode(data []byte) int {
	// 微信语音是腾讯变体 SILK：标准头 "#!SILK_V3" 前多一个 0x02 字节
	if len(data) >= 10 && data[0] == 0x02 && string(data[1:10]) == "#!SILK_V3" {
		return 4
	}
	if len(data) >= 9 && string(data[:9]) == "#!SILK_V3" {
		return 4
	}
	// 只认 AMR-NB（"#!AMR\n"）；AMR-WB 微信不收，落到 -1 走转码
	if isValidAMRNB(data) {
		return 0
	}
	if len(data) >= 12 {
		if string(data[:3]) == "ID3" || (data[0] == 0xFF && (data[1]&0xE0) == 0xE0) {
			return 2
		}
		if string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE" {
			return 3
		}
	}
	return -1
}

func convertToAMR(inputPath, outputPath string) error {
	cmd := exec.Command("ffmpeg",
		"-i", inputPath,
		"-acodec", "amr_nb",
		"-ar", "8000",
		"-ac", "1",
		"-ab", "12.2k",
		"-f", "amr",
		"-y",
		outputPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg 转 AMR 失败: %w, output: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// ensureTencentSilk 修正 silk_v3_encoder -tencent 产出的 SILK 头部。
// 去尾部 0xFF 0xFF（部分编码器会误加），并在最前补 0x02。
// 没这两个修正，微信客户端会判语音损坏。
func ensureTencentSilk(data []byte) []byte {
	if len(data) < 9 || string(data[:9]) != "#!SILK_V3" {
		return data
	}
	if n := len(data); n >= 2 && data[n-2] == 0xFF && data[n-1] == 0xFF {
		data = data[:n-2]
	}
	out := make([]byte, 0, len(data)+1)
	out = append(out, 0x02)
	return append(out, data...)
}

// tryConvertToAMRFile 将音频转为 AMR-NB；返回临时文件路径。
func tryConvertToAMRFile(srcPath string) (string, error) {
	outFile, err := os.CreateTemp("", "hermes-bridge-audio-*.amr")
	if err != nil {
		return "", err
	}
	outPath := outFile.Name()
	_ = outFile.Close()

	if err := convertToAMR(srcPath, outPath); err != nil {
		_ = os.Remove(outPath)
		return "", err
	}

	amrData, err := os.ReadFile(outPath)
	if err != nil {
		_ = os.Remove(outPath)
		return "", fmt.Errorf("读取 AMR 文件失败: %w", err)
	}
	if !isValidAMRNB(amrData) {
		_ = os.Remove(outPath)
		return "", fmt.Errorf("AMR 转码结果校验失败：文件头不是 AMR-NB")
	}

	return outPath, nil
}

// tryConvertToSILK 用 ffmpeg + silk_v3_encoder.exe 把音频转为微信可用的腾讯变体 SILK。
// durationMs 为源音频时长；上传通道对单条语音有大小上限（见 SilkMaxBytes），
// 超预算时先降码率（下限 8000bps），仍不够则裁剪时长。返回 silk 路径与实际编码时长(毫秒)。
// 若编码器路径未配置或执行失败返回 error，调用方应降级到 AMR。
func (p *BridgePlugin) tryConvertToSILK(srcPath string, durationMs int) (string, int, error) {
	cfg := p.configSnapshot()
	enc := strings.TrimSpace(cfg.SilkEncoderPath)
	if enc == "" {
		return "", 0, fmt.Errorf("silk_encoder_path 未配置")
	}

	sampleRate := cfg.SilkSampleRate
	if sampleRate <= 0 {
		sampleRate = 24000
	}

	const minRate, maxRate = 8000, 24000
	rate := maxRate
	actualMs := durationMs
	if budget := cfg.SilkMaxBytes; budget > 0 && durationMs > 0 {
		if need := budget * 8 * 1000 / durationMs; need < rate {
			rate = need
		}
		if rate < minRate {
			rate = minRate
			actualMs = budget * 8 * 1000 / rate
		}
	}

	pcmFile, err := os.CreateTemp("", "hermes-bridge-audio-*.pcm")
	if err != nil {
		return "", 0, err
	}
	pcmPath := pcmFile.Name()
	_ = pcmFile.Close()
	defer os.Remove(pcmPath)

	// step 1: ffmpeg 转 PCM（s16le 单声道；超预算时 -t 裁尾）
	ffArgs := []string{
		"-i", srcPath,
		"-ar", strconv.Itoa(sampleRate),
		"-ac", "1",
		"-f", "s16le",
	}
	if actualMs < durationMs {
		ffArgs = append(ffArgs, "-t", fmt.Sprintf("%.3f", float64(actualMs)/1000))
	}
	ffArgs = append(ffArgs, "-y", pcmPath)
	out, err := exec.Command("ffmpeg", ffArgs...).CombinedOutput()
	if err != nil {
		return "", 0, fmt.Errorf("ffmpeg 转 PCM 失败: %w, output: %s", err, strings.TrimSpace(string(out)))
	}

	// step 2: PCM -> SILK
	silkFile, err := os.CreateTemp("", "hermes-bridge-audio-*.silk")
	if err != nil {
		return "", 0, err
	}
	silkPath := silkFile.Name()
	_ = silkFile.Close()

	// -Fs_API 是输入 PCM 采样率，必须与 ffmpeg 的 -ar 一致，否则变速变调；
	// -rate 是目标码率(bps)而非采样率（Mp3ToSilkUtil.java 注释有误，值恰好撞对）；
	// -tencent 输出微信要求的腾讯变体（头部 0x02 前缀、无 0xFFFF 结尾），
	// 缺了它产出标准 SILK，微信手机/PC 端都判语音损坏。
	silkCmd := exec.Command(enc,
		pcmPath,
		silkPath,
		"-Fs_API", strconv.Itoa(sampleRate),
		"-rate", strconv.Itoa(rate),
		"-tencent",
	)
	out, err = silkCmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(silkPath)
		return "", 0, fmt.Errorf("silk_v3_encoder 编码失败: %w, output: %s", err, strings.TrimSpace(string(out)))
	}

	// 校验产出
	sData, err := os.ReadFile(silkPath)
	if err != nil {
		_ = os.Remove(silkPath)
		return "", 0, fmt.Errorf("读取 silk 文件失败: %w", err)
	}
	if detectAudioFormatCode(sData) != 4 {
		_ = os.Remove(silkPath)
		return "", 0, fmt.Errorf("silk 编码结果不是合法 SILK 格式")
	}
	if rate < maxRate || actualMs < durationMs {
		slog.Info("[hermes_bridge] 语音超出大小预算，已自适应",
			"budget_bytes", cfg.SilkMaxBytes,
			"rate_bps", rate,
			"src_ms", durationMs,
			"encoded_ms", actualMs,
			"silk_bytes", len(sData),
		)
	}
	return silkPath, actualMs, nil
}

func isValidAMRNB(data []byte) bool {
	return len(data) >= 6 &&
		data[0] == 0x23 && data[1] == 0x21 && data[2] == 0x41 &&
		data[3] == 0x4D && data[4] == 0x52 && data[5] == 0x0A
}

// hexHeader 返回字节切片前 n 字节的 hex 字符串，供诊断日志用。
func hexHeader(data []byte, n int) string {
	if len(data) == 0 {
		return "(empty)"
	}
	s := fmt.Sprintf("% x", data[:min(n, len(data))])
	return s
}

func mediaDurationMs(path string) (int, error) {
	d, err := probeDurationSeconds(path)
	if err != nil {
		return 0, err
	}
	return int(d * 1000), nil
}

func mediaDurationSec(path string) (int, error) {
	d, err := probeDurationSeconds(path)
	if err != nil {
		return 0, err
	}
	return int(d), nil
}

func probeDurationSeconds(path string) (float64, error) {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe 执行失败（宿主机需安装 ffmpeg/ffprobe）: %w", err)
	}
	return strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
}

// extractVideoThumbnail 用 ffmpeg 抽一帧 JPEG 作视频封面。
// host message.Send → SendVideo(receiver, thumb, video, duration) 需要 Thumb。
func extractVideoThumbnail(videoPath string) ([]byte, error) {
	tmp, err := os.CreateTemp("", "hermes-bridge-thumb-*.jpg")
	if err != nil {
		return nil, err
	}
	outPath := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(outPath) }()

	// 先尝试 1s 处；短视频可能不够长，再试 0s
	for _, ss := range []string{"00:00:01", "00:00:00"} {
		cmd := exec.Command("ffmpeg",
			"-i", videoPath,
			"-ss", ss,
			"-vframes", "1",
			"-f", "image2",
			"-y",
			outPath,
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			slog.Debug("[hermes_bridge] ffmpeg 抽帧失败", "ss", ss, "err", err, "out", strings.TrimSpace(string(out)))
			continue
		}
		data, readErr := os.ReadFile(outPath)
		if readErr != nil {
			return nil, readErr
		}
		if len(data) == 0 {
			continue
		}
		return data, nil
	}
	return nil, errors.New("ffmpeg 抽帧失败（宿主机需安装 ffmpeg）")
}
