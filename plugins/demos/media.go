package main

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/sbgayhub/golem/sdk/contact"
	"github.com/sbgayhub/golem/sdk/message"
)

const (
	xjjURL      = "https://api.yujn.cn/api/zzxjj.php?type=video"
	xjj2URL     = "https://api.kuleu.com/api/MP4_xiaojiejie?type=json"
	rdVideoURL  = "https://api.52vmy.cn/api/video/redian"
	ylVideoURL  = "https://api.52vmy.cn/api/video/yule"
	boyURL      = "https://api.52vmy.cn/api/video/boy"
	sdjURL      = "https://api.kuleu.com/api/action?text="
	catURL      = "https://api.thecatapi.com/v1/images/search?limit=1"
	dogURL      = "https://dog.ceo/api/breeds/image/random"
	twURL       = "https://api.nasa.gov/planetary/apod?api_key=TJTjotiNFKFh541VXfSwmsKdwMBVuRUikDmyPCgN&count=1"
	paintingURL = "https://api.52vmy.cn/api/query/painting"
	wxtsURL     = "https://api.kuleu.com/api/getGreetingMessage?type=json"
	yijuURL     = "https://api.apiopen.top/api/tools/famous-sentence"
	shiciURL    = "https://v2.alapi.cn/api/shici?type=all&token=iildXgwOPO6d7BOa"
	hahaURL     = "https://v2.alapi.cn/api/joke/random?token=iildXgwOPO6d7BOa"
	jzwURL      = "https://api.52vmy.cn/api/wl/s/jzw"
	raoURL      = "https://api.52vmy.cn/api/wl/yan/rao"
	hunyanURL   = "https://api.52vmy.cn/api/img/tw/card"
	dogDocURL   = "https://api.52vmy.cn/api/wl/s/dog?msg="
	yanyuURL    = "https://api.52vmy.cn/api/wl/yan/yanyu"
	chouqianURL = "https://api.52vmy.cn/api/wl/s/draw"
	bayURL      = "https://api.52vmy.cn/api/wl/yan/bay"
	eatURL      = "https://api.52vmy.cn/api/wl/s/eat"
	kingTcURL   = "https://api.yujn.cn/api/wzry.php?type=json"
	lolTcURL    = "https://api.yujn.cn/api/yxlm.php?"
	xhzBqURL    = "http://api.yujn.cn/api/cxk.php?"
	sjecyURL    = "https://api.cenguigui.cn/api/pic/"
	rjURL       = "https://api.yujn.cn/api/baoan.php?"
	acgURL      = "https://api.yujn.cn/api/gzl_ACG.php?type=image&form=pc"

	lyyKgURL      = "http://api.yujn.cn/api/lyy.php?type=video"
	duilianURL    = "https://api.yujn.cn/api/duilian.php?type=video"
	chuandaURL    = "http://api.yujn.cn/api/chuanda.php?type=video"
	shwdURL       = "http://api.yujn.cn/api/shwd.php?type=video"
	ksFcURL       = "http://api.yujn.cn/api/ks_fc.php?type=video"
	sjkkURL       = "http://api.yujn.cn/api/sjkk.php?"
	sjSingURL     = "https://www.hhlqilongzhu.cn/api/changya.php"
	kuwoSearchURL = "http://search.kuwo.cn/r.s"
	kuwoMusicURL  = "http://nmobi.kuwo.cn/mobi.s"
	kuwoInfoURL   = "http://m.kuwo.cn/newh5/singles/songinfoandlrc"
)

var noRedirectClient = &http.Client{
	Timeout: 10 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("过多重定向")
			}
			loc := req.Response.Header.Get("Location")
			if loc != "" {
				loc = strings.Trim(loc, "'\"")
				if parsed, err := url.Parse(loc); err == nil {
					req.URL = req.URL.ResolveReference(parsed)
				}
			}
			return nil
		},
	}
}

// ==================== 工具方法 ====================

func (p *DemosPlugin) httpGet(urlStr string) (string, error) {
	resp, err := p.client.Get(urlStr)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

func (p *DemosPlugin) httpGetWithHeaders(urlStr string, headers map[string]string) (string, error) {
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return "", err
	}
	for k, v := range headers {
		if strings.EqualFold(k, "Host") {
			req.Host = v
		} else {
			req.Header.Set(k, v)
		}
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

func (p *DemosPlugin) downloadMedia(urlStr string) ([]byte, string, error) {
	resp, err := p.client.Get(urlStr)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	return data, resp.Header.Get("Content-Type"), nil
}

func (p *DemosPlugin) getRedirectURL(u string) (string, error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return "", err
	}
	resp, err := noRedirectClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", fmt.Errorf("没有 Location 头")
	}
	loc = strings.Trim(loc, "'\"")
	if strings.HasPrefix(loc, "http") {
		return loc, nil
	}
	base, err := url.Parse(u)
	if err != nil {
		return "", err
	}
	rel, err := url.Parse(loc)
	if err != nil {
		return "", err
	}
	return base.ResolveReference(rel).String(), nil
}

func (p *DemosPlugin) sendText(receiver *contact.Contact, text string) {
	msg := &message.Message{
		Type:     message.TypeText,
		Receiver: receiver,
		Content:  text,
		Data:     &message.Message_Text{Text: &message.TextData{Content: text}},
	}
	if _, err := p.message.Send(msg); err != nil {
		slog.Error("[demos] 发送文本失败", "err", err)
	}
}

func (p *DemosPlugin) sendImage(receiver *contact.Contact, imageURL string) error {
	data, _, err := p.downloadMedia(imageURL)
	if err != nil {
		return err
	}
	_, err = p.cdn.UploadImage(receiver.GetUsername(), bytes.NewReader(data))
	if err != nil {
		slog.Error("[demos] CDN 上传图片失败", "err", err)
		p.sendText(receiver, "图片发送失败，直接看链接吧："+imageURL)
		return nil
	}
	return nil
}

func (p *DemosPlugin) sendVideoCard(receiver *contact.Contact, title, desc, videoURL string) {
	xml := fmt.Sprintf(
		`<msg><appmsg appid="" sdkver="0"><title>%s</title><des>%s</des><action>view</action><type>5</type><showtype>0</showtype><url>%s</url><thumburl>%s</thumburl></appmsg></msg>`,
		escapeXML(title), escapeXML(desc), escapeXML(videoURL), escapeXML(defaultThumb()),
	)
	msg := &message.Message{
		Type:     message.TypeAppLink,
		Receiver: receiver,
		Content:  fmt.Sprintf("%s %s", title, desc),
		Data: &message.Message_App{App: &message.AppData{
			SubType: 5,
			Title:   title,
			Desc:    desc,
			Url:     videoURL,
			Xml:     xml,
		}},
	}
	if _, err := p.message.Send(msg); err != nil {
		slog.Error("[demos] 发送视频卡片失败", "err", err)
	}
}

func (p *DemosPlugin) sendVideoOrCard(receiver *contact.Contact, videoURL string) {
	if p.Config.VideoNative {
		err := p.sendNativeVideo(receiver, videoURL)
		if err == nil {
			return
		}
		slog.Warn("[demos] 原生视频发送失败，使用链接卡片", "err", err)
	}
	p.sendVideoCard(receiver, "视频链接", "点击播放", videoURL)
}

func (p *DemosPlugin) sendNativeVideo(receiver *contact.Contact, videoURL string) error {
	tmpVideo, err := os.CreateTemp("", "demos-video-*.mp4")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	defer os.Remove(tmpVideo.Name())
	defer tmpVideo.Close()

	resp, err := p.client.Get(videoURL)
	if err != nil {
		return fmt.Errorf("下载视频失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if _, err := io.Copy(tmpVideo, resp.Body); err != nil {
		return fmt.Errorf("保存视频失败: %w", err)
	}
	_ = tmpVideo.Close()

	duration, err := p.mediaDuration(tmpVideo.Name())
	if err != nil {
		slog.Warn("[demos] 获取视频时长失败，使用默认值", "err", err)
		duration = 10
	}

	thumbData, err := p.extractThumbnail(tmpVideo.Name())
	if err != nil {
		slog.Warn("[demos] 提取缩略图失败，使用空缩略图", "err", err)
		thumbData = []byte{}
	}

	videoFile, err := os.Open(tmpVideo.Name())
	if err != nil {
		return fmt.Errorf("打开视频文件失败: %w", err)
	}
	defer videoFile.Close()

	_, err = p.cdn.UploadVideo(receiver.GetUsername(), thumbData, videoFile, uint32(duration))
	if err != nil {
		return fmt.Errorf("CDN 上传视频失败: %w", err)
	}
	return nil
}

func (p *DemosPlugin) sendVoice(receiver *contact.Contact, audioURL string) {
	data, contentType, err := p.downloadMedia(audioURL)
	if err != nil {
		slog.Error("[demos] 语音下载失败", "url", audioURL, "err", err)
		p.sendText(receiver, "语音获取失败，请稍后再试")
		return
	}

	srcFile, err := os.CreateTemp("", "demos-audio-src-*")
	if err != nil {
		p.sendText(receiver, "语音处理失败")
		return
	}
	srcPath := srcFile.Name()
	defer os.Remove(srcPath)
	if _, err := srcFile.Write(data); err != nil {
		_ = srcFile.Close()
		p.sendText(receiver, "语音处理失败")
		return
	}
	if err := srcFile.Close(); err != nil {
		p.sendText(receiver, "语音处理失败")
		return
	}

	srcFormatCode := detectAudioFormatCode(data)
	slog.Debug("[demos] 语音源文件信息",
		"url", audioURL,
		"content_type", contentType,
		"src_format", srcFormatCode,
		"src_size", len(data),
		"src_header", hexHeader(data, 16),
	)

	// 转码前从源文件取时长（silk 文件 ffprobe 取不到，必须提前取）
	durationMs, err := p.mediaDurationMs(srcPath)
	if err != nil {
		slog.Warn("[demos] 获取源文件时长失败，使用默认值", "err", err)
		durationMs = 5000
	}

	voicePath := srcPath
	converted := false
	if srcFormatCode != 0 && srcFormatCode != 4 {
		// 优先尝试 SILK，失败或无配置则降级到 AMR
		var silkMs int
		voicePath, silkMs, err = p.tryConvertToSILK(srcPath, durationMs)
		if err != nil {
			slog.Warn("[demos] silk 编码失败，降级到 amr", "err", err)
			voicePath, err = p.tryConvertToAMR(srcPath)
			if err != nil {
				slog.Error("[demos] 语音转码全部失败", "url", audioURL, "err", err)
				p.sendText(receiver, "语音发送失败，链接："+audioURL)
				return
			}
		} else {
			// 超预算被裁剪时，气泡时长与实际数据保持一致
			durationMs = silkMs
		}
		converted = true
		defer os.Remove(voicePath)
	}

	voiceData, err := os.ReadFile(voicePath)
	if err != nil {
		p.sendText(receiver, "语音处理失败")
		return
	}
	voiceData = ensureTencentSilk(voiceData)

	finalFmt := int32(detectAudioFormatCode(voiceData))
	slog.Debug("[demos] 语音准备发送",
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
		Receiver: receiver,
		Content:  "[语音]",
		Data:     &message.Message_Voice{Voice: voice},
	}
	if _, err := p.message.Send(msg); err != nil {
		if strings.Contains(err.Error(), "code: -104") {
			slog.Warn("[demos] 语音发送返回 -104（语音已实际送达，跳过降级文本）",
				"err", err,
				"converted", converted,
				"duration_ms", durationMs,
				"size", len(voiceData),
				"final_format", finalFmt,
			)
		} else {
			slog.Error("[demos] 发送语音失败",
				"err", err,
				"converted", converted,
				"detected_format", finalFmt,
				"duration_ms", durationMs,
				"size", len(voiceData),
				"header_hex", hexHeader(voiceData, 16),
			)
			p.sendText(receiver, "语音发送失败，链接："+audioURL)
		}
	}
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

// ensureTencentSilk 把标准 SILK v3 流转成微信要求的腾讯变体：
// 头部补 0x02 前缀，去掉末尾的 0xFFFF 流结束标记（若存在）。
// 已是腾讯变体或非 SILK 数据则原样返回。
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

// tryConvertToSILK 用 ffmpeg + silk_v3_encoder.exe 把音频转为微信可用的腾讯变体 SILK。
// durationMs 为源音频时长；上传通道对单条语音有大小上限（见 silk_max_bytes），
// 超预算时先降码率（下限 8000bps），仍不够则裁剪时长。返回 silk 路径与实际编码时长(毫秒)。
// 若编码器路径未配置或执行失败返回 error，调用方应降级到 AMR。
func (p *DemosPlugin) tryConvertToSILK(srcPath string, durationMs int) (string, int, error) {
	enc := p.Config.SilkEncoderPath
	if enc == "" {
		return "", 0, fmt.Errorf("silk_encoder_path 未配置")
	}

	sampleRate := p.Config.SilkSampleRate
	if sampleRate <= 0 {
		sampleRate = 24000
	}

	const minRate, maxRate = 8000, 24000
	rate := maxRate
	actualMs := durationMs
	if budget := p.Config.SilkMaxBytes; budget > 0 && durationMs > 0 {
		if need := budget * 8 * 1000 / durationMs; need < rate {
			rate = need
		}
		if rate < minRate {
			rate = minRate
			actualMs = budget * 8 * 1000 / rate
		}
	}

	pcmFile, err := os.CreateTemp("", "demos-audio-*.pcm")
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
	silkFile, err := os.CreateTemp("", "demos-audio-*.silk")
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
		slog.Info("[demos] 语音超出大小预算，已自适应",
			"budget_bytes", p.Config.SilkMaxBytes,
			"rate_bps", rate,
			"src_ms", durationMs,
			"encoded_ms", actualMs,
			"silk_bytes", len(sData),
		)
	}

	return silkPath, actualMs, nil
}

// tryConvertToAMR 将音频转为 AMR-NB；返回临时文件路径。
func (p *DemosPlugin) tryConvertToAMR(srcPath string) (string, error) {
	outFile, err := os.CreateTemp("", "demos-audio-*.amr")
	if err != nil {
		return "", err
	}
	outPath := outFile.Name()
	_ = outFile.Close()

	if err := convertToAMR(srcPath, outPath); err != nil {
		_ = os.Remove(outPath)
		return "", err
	}

	// 校验 AMR 头
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

// isValidAMRNB 校验是否为合法的 AMR-NB 文件（微信语音要求）。
// AMR-NB 文件头为 "#!AMR\n"（6 字节: 0x23 0x21 0x41 0x4D 0x52 0x0A）。
// AMR-WB 文件头为 "#!AMR-WB\n"，微信不接收。
func isValidAMRNB(data []byte) bool {
	return len(data) >= 6 &&
		data[0] == 0x23 && data[1] == 0x21 && data[2] == 0x41 &&
		data[3] == 0x4D && data[4] == 0x52 && data[5] == 0x0A
}

func hexHeader(data []byte, max int) string {
	if len(data) < max {
		max = len(data)
	}
	if max == 0 {
		return ""
	}
	var sb strings.Builder
	for i := 0; i < max; i++ {
		if i > 0 {
			sb.WriteByte(' ')
		}
		fmt.Fprintf(&sb, "%02X", data[i])
	}
	return sb.String()
}

func (p *DemosPlugin) mediaDurationMs(path string) (int, error) {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil {
		return 0, err
	}
	return int(d * 1000), nil
}

func (p *DemosPlugin) mediaDuration(path string) (int, error) {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil {
		return 0, err
	}
	return int(d), nil
}

func (p *DemosPlugin) extractThumbnail(videoPath string) ([]byte, error) {
	tmpThumb, err := os.CreateTemp("", "demos-thumb-*.jpg")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmpThumb.Name())
	_ = tmpThumb.Close()

	cmd := exec.Command("ffmpeg",
		"-i", videoPath,
		"-ss", "00:00:01",
		"-vframes", "1",
		"-f", "image2",
		"-y",
		tmpThumb.Name(),
	)
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return os.ReadFile(tmpThumb.Name())
}

func defaultThumb() string {
	return "https://img0.baidu.com/it/u=3879589492,1588221464&fm=253&fmt=auto&app=120&f=JPEG?w=500&h=500"
}

func escapeXML(s string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&apos;",
	).Replace(s)
}
