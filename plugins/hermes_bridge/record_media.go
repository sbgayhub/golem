package main

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// 记录卡片内缩略图：对齐真机量级（约 10KB、小边长），手机列表靠它渲染。
	recordThumbMaxEdge = 240
	recordThumbQuality = 72
)

// resolveRecordImageFromSource 解析单张图为 datatype=2 CDN 元数据。
//
// 产品结论（2026-08-05，勿反复试网图嵌记录）：
//   - 日常只接受 media_ref（复用会话里已出现过的图 CDN）
//   - 仅 url、需重新上传时：不嵌记录——应直接发图；强行 Upload/Send 再嵌鸡肋且手机易过期
//   - record_image_via=send|cdn 仅为显式实验开关，默认关闭「为 url 重新上传」
func (p *BridgePlugin) resolveRecordImageFromSource(chatID, name, avatar, timeStr, desc, mediaRef, httpURL string) (recordItem, error) {
	chatID = strings.TrimSpace(chatID)
	mediaRef = strings.TrimSpace(mediaRef)
	httpURL = strings.TrimSpace(httpURL)
	if desc == "" {
		desc = "[图片]"
	}
	if name == "" {
		name = "消息"
	}

	if mediaRef != "" {
		item, err := p.resolveImageFromMediaRef(chatID, mediaRef, name, avatar, timeStr, desc)
		if err == nil {
			return item, nil
		}
		slog.Warn("[hermes_bridge] media_ref 嵌记录失败",
			"ref", mediaRef, "err", err)
		// 不再自动降级 url 重传（会双发/过期）；直接失败
		return recordItem{}, err
	}

	if httpURL == "" {
		return recordItem{}, fmt.Errorf("图片条目需要 media_ref（入站 media_N）；网图请直接发图，不要嵌记录")
	}
	if !strings.HasPrefix(httpURL, "http://") && !strings.HasPrefix(httpURL, "https://") {
		return recordItem{}, fmt.Errorf("图片 url 须为 http(s)")
	}

	// 显式实验：toml record_image_via=send|cdn 才允许为 url 重新取 CDN 再嵌
	via := strings.ToLower(strings.TrimSpace(p.configSnapshot().RecordImageVia))
	if via == "" || via == "off" || via == "none" || via == "media_ref_only" {
		return recordItem{}, fmt.Errorf(
			"拒绝 url 嵌记录（需重新上传，产品上不划算）；请用 media_ref 或直接发图。" +
				"实验可设 record_image_via=send（会话Send）或 cdn（filehelper Upload）")
	}

	data, err := p.downloadImage(httpURL)
	if err != nil {
		return recordItem{}, fmt.Errorf("下载图片失败: %w", err)
	}
	if len(data) == 0 {
		return recordItem{}, fmt.Errorf("下载图片为空")
	}
	slog.Warn("[hermes_bridge] 实验路径：url 嵌记录将重新取 CDN",
		"via", via, "chat", chatID, "bytes", len(data))
	return p.uploadImageForRecord(chatID, data, name, avatar, timeStr, desc)
}

func (p *BridgePlugin) resolveImageFromMediaRef(chatID, ref, name, avatar, timeStr, desc string) (recordItem, error) {
	p.mediaMu.Lock()
	entry := p.mediaRefs[ref]
	if entry != nil && time.Since(entry.CreatedAt) > mediaRefTTL {
		delete(p.mediaRefs, ref)
		entry = nil
	}
	var cands []cdnCand
	var cached []byte
	var imgBuf []byte
	kind := ""
	httpURL := ""
	if entry != nil {
		cands = append([]cdnCand(nil), entry.Cands...)
		cached = entry.Data
		imgBuf = entry.ImgBuf
		kind = entry.Kind
		httpURL = entry.HTTPURL
	}
	p.mediaMu.Unlock()
	if entry == nil {
		return recordItem{}, errMediaRefNotFound
	}
	if kind == "emoji" {
		if u := strings.TrimSpace(httpURL); u != "" {
			data, err := p.downloadEmoji(u)
			if err != nil {
				return recordItem{}, err
			}
			return p.uploadImageForRecord(chatID, data, name, avatar, timeStr, desc)
		}
		return recordItem{}, fmt.Errorf("表情 media_ref 无可用 url")
	}

	var midURL, midKey, bigURL, bigKey, thumbURL, thumbKey string
	for _, c := range cands {
		if c.FileID == "" || c.Key == "" {
			continue
		}
		switch c.Which {
		case "mid":
			if midURL == "" {
				midURL, midKey = c.FileID, c.Key
			}
		case "big":
			if bigURL == "" {
				bigURL, bigKey = c.FileID, c.Key
			}
		case "thumb":
			if thumbURL == "" {
				thumbURL, thumbKey = c.FileID, c.Key
			}
		}
	}
	dataURL, dataKey := midURL, midKey
	if dataURL == "" {
		dataURL, dataKey = bigURL, bigKey
	}

	// 原图像素
	data := cached
	if len(data) == 0 {
		for _, c := range cands {
			if c.Which == "thumb" {
				continue
			}
			if d := p.downloadViaCDN(c.FileID, c.Key); len(d) > 0 {
				data = d
				dataURL, dataKey = c.FileID, c.Key
				break
			}
		}
	}
	if len(data) == 0 && len(imgBuf) > 0 {
		slog.Info("[hermes_bridge] media_ref 仅 ImgBuf，改双传嵌记录", "ref", ref, "bytes", len(imgBuf))
		return p.uploadImageForRecord(chatID, imgBuf, name, avatar, timeStr, desc)
	}
	if len(data) == 0 {
		return recordItem{}, fmt.Errorf("media_ref 无法取到图片字节")
	}
	if dataURL == "" || dataKey == "" {
		return p.uploadImageForRecord(chatID, data, name, avatar, timeStr, desc)
	}

	p.mediaMu.Lock()
	if e := p.mediaRefs[ref]; e != nil && len(e.Data) == 0 {
		e.Data = data
	}
	p.mediaMu.Unlock()

	fullMD5 := md5Hex(data)
	dataSize := uint32(len(data))
	fmtStr := sniffImageFmt(data)

	// 缩略图：优先下真 thumb cand（须与 data 不同套 id/key）；否则本地出图再静默上传
	var tURL, tKey, tMD5 string
	var tSize uint32
	if thumbURL != "" && thumbKey != "" && (thumbURL != dataURL || thumbKey != dataKey) {
		if td := p.downloadViaCDN(thumbURL, thumbKey); len(td) > 0 {
			tURL, tKey = thumbURL, thumbKey
			tMD5 = md5Hex(td)
			tSize = uint32(len(td))
		}
	}
	if tURL == "" {
		thumbJPEG, err := makeRecordThumbJPEG(data)
		if err != nil {
			slog.Warn("[hermes_bridge] 生成记录缩略图失败，尝试整图当 thumb（手机可能预览差）", "err", err)
			// 最后手段：再静默传一份原图当 thumb 身份（仍是两套上传，id 不同）
			thumbJPEG = data
		}
		meta, err := p.silentUploadImageBytes(thumbJPEG)
		if err != nil {
			return recordItem{}, fmt.Errorf("上传记录缩略图失败: %w", err)
		}
		tURL, tKey = meta.FileID, meta.AesKey
		tMD5 = meta.FileMD5
		if tMD5 == "" {
			tMD5 = md5Hex(thumbJPEG)
		}
		tSize = meta.FileSize
		if tSize == 0 {
			tSize = uint32(len(thumbJPEG))
		}
	}

	return recordItem{
		Kind:      recordKindImage,
		Name:      name,
		Content:   desc,
		Avatar:    avatar,
		Time:      timeStr,
		DataURL:   dataURL,
		DataKey:   dataKey,
		FullMD5:   fullMD5,
		DataSize:  dataSize,
		DataFmt:   fmtStr,
		ThumbURL:  tURL,
		ThumbKey:  tKey,
		ThumbMD5:  tMD5,
		ThumbSize: tSize,
	}, nil
}

// cdnUploadMeta 一次上传/发图拿到的 CDN 字段。
type cdnUploadMeta struct {
	FileID   string
	AesKey   string
	FileMD5  string
	FileSize uint32
	NewMsgID uint64 // 仅 message.Send 路径有；Upload 为 0
}

// recordImageUploadReceivers CDN 上传「接收方」候选。
// cdn.UploadImage(receiver) 常会把图真发进该会话；嵌记录禁止对目标 chat 上传。
func (p *BridgePlugin) recordImageUploadReceivers(chatID string) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		if chatID != "" && id == chatID {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	add("filehelper")
	if self := p.selfSnapshot(); self != nil {
		add(self.GetUsername())
	}
	return out
}

func (p *BridgePlugin) silentUploadImageBytes(data []byte) (cdnUploadMeta, error) {
	var zero cdnUploadMeta
	if p.cdn == nil {
		return zero, fmt.Errorf("cdn 能力未注入")
	}
	if len(data) == 0 {
		return zero, fmt.Errorf("图片数据为空")
	}
	if len(data) > maxImageBytes {
		return zero, fmt.Errorf("图片过大：%d > %d", len(data), maxImageBytes)
	}
	receivers := p.recordImageUploadReceivers("")
	// chatID 空时 add 不会过滤；上面传 "" 仅 filehelper/self
	if self := p.selfSnapshot(); self != nil {
		// ensure self present even if snapshot raced
		_ = self
	}
	if len(receivers) == 0 {
		receivers = []string{"filehelper"}
	}

	var meta cdnUploadMeta
	var lastErr error
	for _, recv := range receivers {
		outcome, err := p.callSendWithRetry(uploadImageTimeout, func() error {
			up, e := p.cdn.UploadImage(recv, bytes.NewReader(data))
			if e != nil {
				return e
			}
			if up == nil {
				return fmt.Errorf("cdn.UploadImage 回包空")
			}
			meta.FileID = strings.TrimSpace(up.GetFileId())
			meta.AesKey = strings.TrimSpace(up.GetAesKey())
			meta.FileMD5 = strings.TrimSpace(up.GetFileMd5())
			meta.FileSize = up.GetFileSize()
			if meta.FileID == "" || meta.AesKey == "" {
				return fmt.Errorf("cdn.UploadImage 缺少 file_id/aes_key")
			}
			return nil
		})
		if outcome == uploadOK {
			if meta.FileMD5 == "" {
				meta.FileMD5 = md5Hex(data)
			}
			if meta.FileSize == 0 {
				meta.FileSize = uint32(len(data))
			}
			slog.Info("[hermes_bridge] 静默上传 CDN 成功",
				"recv", recv, "bytes", len(data), "file_id_len", len(meta.FileID))
			return meta, nil
		}
		lastErr = err
		if lastErr == nil {
			lastErr = fmt.Errorf("上传失败 outcome=%v", outcome)
		}
		slog.Warn("[hermes_bridge] 静默上传失败，试下一接收方", "recv", recv, "err", lastErr)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("静默上传失败")
	}
	return zero, lastErr
}

// uploadImageForRecord 实验路径：为 url 嵌图重新取两套 CDN（勿作产品默认）。
// via=send：会话内 SendImage；via=cdn：filehelper Upload。
func (p *BridgePlugin) uploadImageForRecord(chatID string, data []byte, name, avatar, timeStr, desc string) (recordItem, error) {
	if len(data) == 0 {
		return recordItem{}, fmt.Errorf("图片数据为空")
	}
	if len(data) > maxImageBytes {
		return recordItem{}, fmt.Errorf("图片过大：%d > %d", len(data), maxImageBytes)
	}

	via := strings.ToLower(strings.TrimSpace(p.configSnapshot().RecordImageVia))
	if via == "" {
		via = "send"
	}

	thumbJPEG, err := makeRecordThumbJPEG(data)
	if err != nil {
		slog.Warn("[hermes_bridge] 生成缩略图失败，thumb 用原图字节", "err", err)
		thumbJPEG = data
	}

	var fullMeta, thumbMeta cdnUploadMeta
	switch via {
	case "cdn", "upload", "filehelper":
		fullMeta, err = p.silentUploadImageBytes(data)
		if err != nil {
			return recordItem{}, fmt.Errorf("cdn 上传记录原图失败: %w", err)
		}
		thumbMeta, err = p.silentUploadImageBytes(thumbJPEG)
		if err != nil {
			return recordItem{}, fmt.Errorf("cdn 上传记录缩略图失败: %w", err)
		}
		slog.Info("[hermes_bridge] 记录嵌图实验 via=cdn",
			"chat", chatID, "full_bytes", len(data), "thumb_bytes", len(thumbJPEG))
	default: // send
		if strings.TrimSpace(chatID) == "" {
			return recordItem{}, fmt.Errorf("via=send 需要 chat_id")
		}
		fullMeta, _, err = p.sendImageMessageWithMeta(chatID, data)
		if err != nil {
			return recordItem{}, fmt.Errorf("SendImage 取原图 CDN 失败: %w", err)
		}
		thumbMeta, _, err = p.sendImageMessageWithMeta(chatID, thumbJPEG)
		if err != nil {
			return recordItem{}, fmt.Errorf("SendImage 取缩略图 CDN 失败: %w", err)
		}
		slog.Info("[hermes_bridge] 记录嵌图实验 via=send",
			"chat", chatID,
			"full_newid", fullMeta.NewMsgID,
			"thumb_newid", thumbMeta.NewMsgID,
			"full_bytes", len(data),
			"thumb_bytes", len(thumbJPEG),
			"will_revoke", p.recordImageShouldRevoke(),
		)
		p.queueRecordImageRevoke(chatID, fullMeta.NewMsgID, thumbMeta.NewMsgID)
	}

	fmtStr := sniffImageFmt(data)
	fullMD5, fullSize := pickMD5Size(fullMeta, data, "原图")
	thumbMD5, thumbSize := pickMD5Size(thumbMeta, thumbJPEG, "缩略图")

	item := recordItem{
		Kind:      recordKindImage,
		Name:      name,
		Content:   desc,
		Avatar:    avatar,
		Time:      timeStr,
		DataURL:   fullMeta.FileID,
		DataKey:   fullMeta.AesKey,
		FullMD5:   fullMD5,
		DataSize:  fullSize,
		DataFmt:   fmtStr,
		ThumbURL:  thumbMeta.FileID,
		ThumbKey:  thumbMeta.AesKey,
		ThumbMD5:  thumbMD5,
		ThumbSize: thumbSize,
	}
	logRecordImageCDN("嵌图实验就绪 via="+via, chatID, item,
		"full_bytes", len(data),
		"thumb_bytes", len(thumbJPEG),
		"fmt", fmtStr,
		"full_newid", fullMeta.NewMsgID,
		"thumb_newid", thumbMeta.NewMsgID,
	)
	return item, nil
}

func pickMD5Size(meta cdnUploadMeta, raw []byte, label string) (string, uint32) {
	local := md5Hex(raw)
	md := meta.FileMD5
	if md == "" {
		md = local
	} else if md != local {
		slog.Warn("[hermes_bridge] "+label+" FileMd5 与本地不一致，XML 用服务端/回包",
			"server", md, "local", local)
	}
	sz := meta.FileSize
	if sz == 0 {
		sz = uint32(len(raw))
	}
	return md, sz
}

// --- via=send 临时图撤回队列（发完记录卡片后执行）---

type recordRevokeJob struct {
	ChatID string
	IDs    []uint64
}

func (p *BridgePlugin) recordImageShouldRevoke() bool {
	cfg := p.configSnapshot()
	if cfg.RecordImageRevoke == nil {
		// 默认不撤：微信会显示「撤回了两条消息」，体验差。
		// 诊断叠图时可 toml: record_image_revoke = true
		return false
	}
	return *cfg.RecordImageRevoke
}

func (p *BridgePlugin) queueRecordImageRevoke(chatID string, ids ...uint64) {
	if !p.recordImageShouldRevoke() {
		return
	}
	var clean []uint64
	for _, id := range ids {
		if id != 0 {
			clean = append(clean, id)
		}
	}
	if len(clean) == 0 || strings.TrimSpace(chatID) == "" {
		return
	}
	p.recordRevokeMu.Lock()
	p.recordRevokeQueue = append(p.recordRevokeQueue, recordRevokeJob{ChatID: chatID, IDs: clean})
	p.recordRevokeMu.Unlock()
}

// flushRecordImageRevokes 在 send_record 成功后撤回 via=send 产生的临时图。
func (p *BridgePlugin) flushRecordImageRevokes() {
	p.recordRevokeMu.Lock()
	jobs := p.recordRevokeQueue
	p.recordRevokeQueue = nil
	p.recordRevokeMu.Unlock()
	if len(jobs) == 0 || p.message == nil {
		return
	}
	// 稍等客户端落盘/同步，再撤（过早撤可能导致记录里 CDN 也挂）
	time.Sleep(800 * time.Millisecond)
	for _, job := range jobs {
		// 这些临时图是记录卡片的副产物，不该再由用户的「撤回」重复撤一次
		p.forgetOutbox(job.ChatID, job.IDs...)
		for _, id := range job.IDs {
			if _, err := p.message.Revoke(job.ChatID, id); err != nil {
				slog.Warn("[hermes_bridge] 撤回记录临时图失败",
					"chat", job.ChatID, "newid", id, "err", err)
			} else {
				slog.Info("[hermes_bridge] 已撤回记录临时图",
					"chat", job.ChatID, "newid", id)
			}
		}
	}
}

// discardRecordImageRevokes send_record 失败时丢掉待撤队列（临时图可留着排障，或也撤）。
func (p *BridgePlugin) discardRecordImageRevokes(alsoRevoke bool) {
	p.recordRevokeMu.Lock()
	jobs := p.recordRevokeQueue
	p.recordRevokeQueue = nil
	p.recordRevokeMu.Unlock()
	if !alsoRevoke || len(jobs) == 0 || p.message == nil {
		return
	}
	for _, job := range jobs {
		p.forgetOutbox(job.ChatID, job.IDs...)
		for _, id := range job.IDs {
			_, _ = p.message.Revoke(job.ChatID, id)
		}
	}
}

// logRecordImageCDN 诊断手机「过期/无预览」：对照真机 thumb 与 data 必须两套。
func logRecordImageCDN(phase, chatID string, it recordItem, extra ...any) {
	args := []any{
		"phase", phase,
		"chat", chatID,
		"data_id_prefix", cdnIDPrefix(it.DataURL),
		"data_id_len", len(it.DataURL),
		"data_key_len", len(it.DataKey),
		"fullmd5", it.FullMD5,
		"datasize", it.DataSize,
		"datafmt", it.DataFmt,
		"thumb_id_prefix", cdnIDPrefix(it.ThumbURL),
		"thumb_id_len", len(it.ThumbURL),
		"thumb_key_len", len(it.ThumbKey),
		"thumbmd5", it.ThumbMD5,
		"thumbsize", it.ThumbSize,
		"same_id", it.DataURL != "" && it.DataURL == it.ThumbURL,
		"same_key", it.DataKey != "" && it.DataKey == it.ThumbKey,
		"same_md5", it.FullMD5 != "" && it.FullMD5 == it.ThumbMD5,
		"size_ok_thumb_lt_full", it.ThumbSize > 0 && it.DataSize > 0 && it.ThumbSize < it.DataSize,
	}
	args = append(args, extra...)
	slog.Info("[hermes_bridge] 记录图片CDN", args...)
}

func cdnIDPrefix(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= 16 {
		return id
	}
	return id[:16]
}

// dumpOutboundRecordXML 把即将 SendApp 的 type=19 XML 落到 dir，便于和真机 dump 对比。
// dir 为空时不落盘（默认）：这是纯诊断路径，之前写死相对目录会随 host 工作目录漂移。
func dumpOutboundRecordXML(dir, xml string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("outbound_record_%s.xml", time.Now().Format("20060102_150405"))
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(xml), 0o644); err != nil {
		return "", err
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs, nil
	}
	return path, nil
}

// makeRecordThumbJPEG 生成记录卡片用 JPEG 缩略图（小边长、约十余 KB）。
func makeRecordThumbJPEG(src []byte) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 1 || h < 1 {
		return nil, fmt.Errorf("invalid image size")
	}
	maxSide := w
	if h > maxSide {
		maxSide = h
	}
	scale := 1.0
	if maxSide > recordThumbMaxEdge {
		scale = float64(recordThumbMaxEdge) / float64(maxSide)
	}
	nw := int(float64(w) * scale)
	nh := int(float64(h) * scale)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	scaled := resizeBox(img, nw, nh)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, scaled, &jpeg.Options{Quality: recordThumbQuality}); err != nil {
		return nil, err
	}
	if buf.Len() == 0 {
		return nil, fmt.Errorf("empty jpeg thumb")
	}
	return buf.Bytes(), nil
}
