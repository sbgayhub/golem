package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/sbgayhub/golem/sdk/message"
)

const (
	maxVoiceBytes    = 10 << 20  // 语音源文件上限
	maxImageBytes    = 45 << 20  // 图片源文件上限（受 gRPC 单消息 50MB 限制，留余量）
	maxVideoBytes    = 45 << 20  // 视频源文件上限（同上，超过走降级发链接）
	maxEmojiRawBytes = 45 << 20  // 表情源文件上限（下载阶段），实际发送前压缩到 maxEmojiBytes
	maxEmojiBytes    = 500 << 10 // 表情发送上限 ~500KB（微信对表情体积有限制，参照 meme 插件实测）

	uploadImageTimeout = 45 * time.Second  // 单次图片发送超时
	uploadVideoTimeout = 120 * time.Second // 单次视频发送超时
	uploadRetries      = 2                 // 最多重试次数（瞬断多在重试时恢复）
	uploadRetryDelay   = time.Second       // 重试间隔
)

// uploadOutcome 标记一次发送尝试的最终结果，降级逻辑据此分派
type uploadOutcome int

const (
	uploadOK      uploadOutcome = iota
	uploadFailed                // 重试耗尽仍失败
	uploadTimeout               // 超时，goroutine 内的 send 会被自然带跑完
)

// callSendWithRetry 把一次「向微信发送媒体」的同步动作包进带超时的 goroutine，并在失败时重试。
//
// 为什么走 message.Send 而非 cdn.UploadImage：实测同进程、同秒、同 receiver、同图，
// cdn 这条上传实现会被微信侧偶发 RST（读取 CDN 响应头：connection forcibly closed），
// 而经 message.Send + ImageData/VideoData（底层 messageapi.SendImage/SendVideo）这条路稳定。
// 两条路径底层都打微信 CDN 服务器，但实现不同，微信侧对其放行策略不同；故默认走 message.Send。
// 超时脱身：send 一旦卡住不能拖死整个串行 run 队列，故 goroutine + select 定限时放弃。
func (p *HermesPlugin) callSendWithRetry(perCallTimeout time.Duration, do func() error) (uploadOutcome, error) {
	var lastErr error
	for attempt := 0; attempt <= uploadRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(uploadRetryDelay)
		}
		outcome, err := p.callOnceWithTimeout(perCallTimeout, do)
		if outcome == uploadOK {
			return uploadOK, nil
		}
		lastErr = err
		if outcome == uploadTimeout {
			slog.Warn("[hermes] 发送超时，准备重试", "attempt", attempt+1, "err", err)
		} else {
			slog.Warn("[hermes] 发送失败，准备重试", "attempt", attempt+1, "err", err)
		}
	}
	return uploadFailed, lastErr
}

// callOnceWithTimeout 执行单次同步调用，用 goroutine + select 实现超时脱身。
// 超时后 do 内部若仍在跑，会被其所在 goroutine 自然带着跑完（不强行打断，避免竞争）。
func (p *HermesPlugin) callOnceWithTimeout(perCallTimeout time.Duration, do func() error) (uploadOutcome, error) {
	type result struct{ err error }
	done := make(chan result, 1)
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
		return uploadTimeout, errors.New("发送超时")
	}
}

// sendImageMessage 经 message.Send 发送图片消息（走 universal 实测稳成的路径）。
// 返回最终结果与错误，供 toolSendImage 据此决定降级与否。
func (p *HermesPlugin) sendImageMessage(receiver string, data []byte) (uploadOutcome, error) {
	if p.message == nil {
		return uploadFailed, errors.New("消息能力未注入")
	}
	msg := &message.Message{
		Type:     message.TypeImage,
		Receiver: p.resolveReceiver(receiver),
		Content:  "[图片]",
		Data:     &message.Message_Image{Image: &message.ImageData{Media: &message.Media{Data: data}}},
	}
	return p.callSendWithRetry(uploadImageTimeout, func() error {
		_, err := p.message.Send(msg)
		return err
	})
}

// sendEmojiMessage 经 message.Send 发送表情消息（TypeEmoji, Code 47）。
// 与 sendImageMessage 同走 message.Send + 超时重试骨架，区别仅在 Type 与 Data 用 EmojiData。
// 参照 meme 插件：表情体积上限 ~500KB，调用方应先用 ensureEmojiBytes 压缩。
func (p *HermesPlugin) sendEmojiMessage(receiver string, data []byte) (uploadOutcome, error) {
	if p.message == nil {
		return uploadFailed, errors.New("消息能力未注入")
	}
	msg := &message.Message{
		Type:     message.TypeEmoji,
		Receiver: p.resolveReceiver(receiver),
		Content:  "[表情]",
		Data:     &message.Message_Emoji{Emoji: &message.EmojiData{Media: &message.Media{Data: data}}},
	}
	return p.callSendWithRetry(uploadImageTimeout, func() error {
		_, err := p.message.Send(msg)
		return err
	})
}

// ensureEmojiBytes 把表情字节控制在微信表情体积上限内。
// 超过 maxEmojiBytes 时按 jpeg 逐级降质量重编码，保证能被微信侧收下；
// 解码失败（非图片格式）或压不到上限则原样返回（由发送侧决定是否降级）。
func ensureEmojiBytes(data []byte) []byte {
	if len(data) <= maxEmojiBytes {
		return data
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		// 非标准图或解码失败，不强行处理，留给发送链与降级机制
		slog.Warn("[hermes] 表情体积超限且解码失败，原样发送", "bytes", len(data), "err", err)
		return data
	}
	for quality := 85; quality >= 30; quality -= 15 {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err == nil && buf.Len() <= maxEmojiBytes {
			slog.Info("[hermes] 表情已压缩", "from", len(data), "to", buf.Len(), "quality", quality)
			return buf.Bytes()
		}
	}
	slog.Warn("[hermes] 表情压不到上限，原样发送", "bytes", len(data))
	return data
}

// downloadToTemp 下载 URL 到临时文件，返回路径（调用方负责删除）
func (p *HermesPlugin) downloadToTemp(rawURL, pattern string, maxBytes int64) (string, error) {
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

// sendVoiceFromURL 下载音频并作为微信语音发送（非 AMR/SILK 自动经 ffmpeg 转 AMR-NB）
func (p *HermesPlugin) sendVoiceFromURL(targetID, audioURL string) error {
	if p.message == nil {
		return errors.New("消息能力未注入")
	}
	srcPath, err := p.downloadToTemp(audioURL, "hermes-audio-src-*", maxVoiceBytes)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(srcPath) }()

	srcData, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("读取音频失败: %w", err)
	}

	voicePath := srcPath
	// 格式码：4=SILK、0=AMR 可直发；其余（mp3/wav 等）转 AMR-NB
	if code := detectAudioFormatCode(srcData); code != 0 && code != 4 {
		out, err := os.CreateTemp("", "hermes-audio-*.amr")
		if err != nil {
			return fmt.Errorf("创建临时文件失败: %w", err)
		}
		outPath := out.Name()
		_ = out.Close()
		defer func() { _ = os.Remove(outPath) }()
		if err := convertToAMR(srcPath, outPath); err != nil {
			return fmt.Errorf("音频转码失败（宿主机需安装 ffmpeg）: %w", err)
		}
		voicePath = outPath
	}

	voiceData, err := os.ReadFile(voicePath)
	if err != nil {
		return fmt.Errorf("读取语音失败: %w", err)
	}
	if voicePath != srcPath && !isValidAMRNB(voiceData) {
		return errors.New("转码结果不是合法的 AMR-NB，放弃发送")
	}
	durationMs, err := mediaDurationMs(voicePath)
	if err != nil {
		slog.Warn("[hermes] 获取语音时长失败，使用默认值", "err", err)
		durationMs = 5000
	}

	msg := &message.Message{
		Type:     message.TypeVoice,
		Receiver: p.resolveReceiver(targetID),
		Content:  "[语音]",
		Data: &message.Message_Voice{Voice: &message.VoiceData{
			Media:    &message.Media{Data: voiceData},
			Duration: uint32(durationMs),
		}},
	}
	if _, err := p.message.Send(msg); err != nil {
		// demos 插件经验：-104 时语音实际已送达
		if strings.Contains(err.Error(), "code: -104") {
			slog.Warn("[hermes] 语音发送返回 -104（经验证实际已送达）", "target", targetID)
			return nil
		}
		return fmt.Errorf("发送语音失败: %w", err)
	}
	return nil
}

// sendVideoFromURL 下载视频并经 message.Send 发送（ffprobe 取时长）。
// 走 message.Send + VideoData（与图片同改走 universal 实测稳成的路径），不走 cdn.UploadVideo。
// 说明：本插件依赖的 sdk v0.1.1 的 VideoData 只有 ThumbUrl（URL）字段、不支持塞缩略图字节，
// 故此处不传缩略图（微信仍可正常展示视频首帧）。host 侧若后续放开 Thumb 字节字段再补。
// 发送失败/超时不再抛 error 给上层——改为降级发一条文本说明 + 原始 URL，
// 避免一次发送卡死拖垮串行 run 队列、或让 agent 误判本轮失败。
func (p *HermesPlugin) sendVideoFromURL(targetID, videoURL string) error {
	if p.message == nil {
		return errors.New("消息能力未注入")
	}
	videoPath, err := p.downloadToTemp(videoURL, "hermes-video-*.mp4", maxVideoBytes)
	if err != nil {
		// 下载都失败了，没有本地文件，直接降级发链接
		p.fallbackLink(targetID, "视频", videoURL, err)
		return nil
	}
	defer func() { _ = os.Remove(videoPath) }()

	durationSec, err := mediaDurationSec(videoPath)
	if err != nil {
		slog.Warn("[hermes] 获取视频时长失败，使用默认值", "err", err)
		durationSec = 10
	}
	videoData, err := os.ReadFile(videoPath)
	if err != nil {
		p.fallbackLink(targetID, "视频", videoURL, err)
		return nil
	}

	msg := &message.Message{
		Type:     message.TypeVideo,
		Receiver: p.resolveReceiver(targetID),
		Content:  "[视频]",
		Data: &message.Message_Video{Video: &message.VideoData{
			Media:    &message.Media{Data: videoData},
			Duration: uint32(durationSec),
		}},
	}
	outcome, upErr := p.callSendWithRetry(uploadVideoTimeout, func() error {
		_, err := p.message.Send(msg)
		return err
	})
	if outcome == uploadOK {
		return nil
	}
	slog.Error("[hermes] 视频发送最终失败，降级发链接", "target", targetID, "outcome", outcome, "err", upErr)
	p.fallbackLink(targetID, "视频", videoURL, upErr)
	return nil
}

// fallbackLink CDN 发不出来时的降级：发一条文本说明 + 原始 URL，让用户自己点开。
func (p *HermesPlugin) fallbackLink(targetID, kind, rawURL string, cause error) {
	if p.message == nil {
		return
	}
	slog.Info("[hermes] 媒体上传降级发链接", "kind", kind, "target", targetID, "url", rawURL, "cause", cause)
	txt := fmt.Sprintf("（%s发送失败，看链接吧：%s）", kind, rawURL)
	msg := &message.Message{
		Type:     message.TypeText,
		Receiver: p.resolveReceiver(targetID),
		Content:  txt,
		Data:     &message.Message_Text{Text: &message.TextData{Content: txt}},
	}
	if _, err := p.message.Send(msg); err != nil {
		slog.Error("[hermes] 降级文本也发送失败", "target", targetID, "err", err)
	}
}

// detectAudioFormatCode 探测音频格式：4=SILK、0=AMR、2=MP3、3=WAV、-1=未知
func detectAudioFormatCode(data []byte) int {
	if len(data) >= 9 && string(data[:9]) == "#!SILK_V3" {
		return 4
	}
	if len(data) >= 6 && strings.HasPrefix(string(data[:6]), "#!AMR") {
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

// convertToAMR 用 ffmpeg 转 AMR-NB（微信语音格式：8kHz 单声道）
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

// isValidAMRNB 校验 AMR-NB 文件头 "#!AMR\n"（AMR-WB 微信不收）
func isValidAMRNB(data []byte) bool {
	return len(data) >= 6 &&
		data[0] == 0x23 && data[1] == 0x21 && data[2] == 0x41 &&
		data[3] == 0x4D && data[4] == 0x52 && data[5] == 0x0A
}

// mediaDurationMs ffprobe 取媒体时长（毫秒）
func mediaDurationMs(path string) (int, error) {
	d, err := probeDurationSeconds(path)
	if err != nil {
		return 0, err
	}
	return int(d * 1000), nil
}

// mediaDurationSec ffprobe 取媒体时长（秒）
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
