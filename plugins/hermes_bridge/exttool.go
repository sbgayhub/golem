package main

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// 外部可执行程序解析：配置优先 → PATH 查找 → 明确报错。
//
// 为什么要可配置：ffmpeg 在 Windows 上常被解压到某个目录而没进 PATH（最常见的
// 部署故障），Linux 上又可能装在 /usr/local/bin 而服务的 PATH 里没有。写死程序名
// 时症状是「语音/视频功能整体不可用」，而错误里看不出是环境问题。
//
// 结果按「配置值」缓存：LookPath 有磁盘开销，而每条语音会调用多次。
// 配置改动通过 cachedName 失效，不必重启插件。

type toolCache struct {
	mu     sync.Mutex
	cfgVal string // 上次解析时的配置值，用于失效
	path   string
	err    error
	valid  bool
}

var (
	ffmpegCache  toolCache
	ffprobeCache toolCache
)

// resolve 解析可执行文件路径。cfgVal 为配置里填的路径（可空），name 为 PATH 中的程序名。
func (c *toolCache) resolve(cfgVal, name, purpose string) (string, error) {
	cfgVal = strings.TrimSpace(cfgVal)

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.valid && c.cfgVal == cfgVal {
		return c.path, c.err
	}

	path, err := lookupExecutable(cfgVal, name, purpose)
	c.cfgVal = cfgVal
	c.path = path
	c.err = err
	c.valid = true
	return path, err
}

// lookupExecutable 配置优先；配置为空时查 PATH。两者都不成立时返回可操作的错误。
func lookupExecutable(cfgVal, name, purpose string) (string, error) {
	if cfgVal != "" {
		// 配置了绝对/相对路径：直接用。仍走 LookPath 以便 Windows 自动补 .exe，
		// 且能提前发现「路径写错」而不是等到 exec 时报 file not found。
		if p, err := exec.LookPath(cfgVal); err == nil {
			return p, nil
		}
		return "", fmt.Errorf("配置的 %s 路径不可执行: %s（%s）", name, cfgVal, purpose)
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("未找到 %s：请安装后加入 PATH，或在 hermes_bridge 配置 %s_path（%s）",
		name, name, purpose)
}

// ffmpegPath 解析 ffmpeg；找不到时错误已含安装/配置指引。
func (p *BridgePlugin) ffmpegPath() (string, error) {
	return ffmpegCache.resolve(p.configSnapshot().FFmpegPath, "ffmpeg",
		"语音转码、表情/视频处理、视频抽封面")
}

// ffprobePath 解析 ffprobe；找不到时错误已含安装/配置指引。
func (p *BridgePlugin) ffprobePath() (string, error) {
	return ffprobeCache.resolve(p.configSnapshot().FFprobePath, "ffprobe",
		"取音频/视频时长")
}

// silkEncoderPath 解析 silk_v3_encoder；未配置时明确说明后果（降级 AMR）。
func (p *BridgePlugin) silkEncoderPath() (string, error) {
	raw := strings.TrimSpace(p.configSnapshot().SilkEncoderPath)
	if raw == "" {
		return "", fmt.Errorf("silk_encoder_path 未配置（语音将降级为 AMR）")
	}
	if path, err := exec.LookPath(raw); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("配置的 silk_encoder_path 不可执行: %s", raw)
}

// mediaToolStatus 供 /health 与管理台诊断：报告三个外部程序的可用性。
func (p *BridgePlugin) mediaToolStatus() map[string]any {
	out := map[string]any{}
	for _, it := range []struct {
		key     string
		resolve func() (string, error)
	}{
		{"ffmpeg", p.ffmpegPath},
		{"ffprobe", p.ffprobePath},
		{"silk_encoder", p.silkEncoderPath},
	} {
		path, err := it.resolve()
		entry := map[string]any{"ok": err == nil}
		if err != nil {
			entry["error"] = err.Error()
		} else {
			entry["path"] = path
		}
		out[it.key] = entry
	}
	return out
}
