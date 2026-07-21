package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/sbgayhub/golem/sdk/message"
	"github.com/sbgayhub/golem/sdk/plugin"
)

type MusicPlugin struct {
	message message.Ability
}

var xmlTemplate = `<appmsg appid="%s" sdkver="0">
    <title>%s</title>
    <des>%s</des>
    <action>view</action>
    <type>3</type>
    <dataurl>%s</dataurl>
    <songalbumurl>%s</songalbumurl>
    <songlyric>%s</songlyric>
</appmsg>
`

var prefixes = []string{"音乐 ", "点歌 ", "music "}

func (m *MusicPlugin) GetMetadata() *plugin.Metadata {
	return &plugin.Metadata{
		Name:        "music",
		Author:      "ovo",
		Version:     "1.0.0",
		Description: "音乐点播，发送 '[音乐|点歌|music] music_name' 点歌",
		Priority:    10,
		Next:        false,
		AlwaysRun:   false,
	}
}

func (m *MusicPlugin) GetSubscriptions() []string {
	return []string{message.TypeText.Topic}
}

func (m *MusicPlugin) OnEvent(event *plugin.Event) (bool, error) {
	msg := event.Payload.(*plugin.Event_Message).Message

	if !hasOnePrefix(msg.Content, prefixes) {
		return false, nil
	}

	name := ltrim(msg.Content, prefixes...)
	resp, err := http.DefaultClient.Get("https://109a.cn/API/qqyy/api.php?msg=" + url.PathEscape(name))
	if err != nil {
		slog.Warn("[music] 请求失败", "err", err)
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()

	all, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Warn("[music] 读取响应失败", "err", err)
		return false, err
	}

	result, err := parseMusicResponse(all)
	if err != nil {
		slog.Warn("[music] 解析响应失败", "err", err, "status", resp.StatusCode)
		return false, err
	}

	xmlContent := fmt.Sprintf(xmlTemplate,
		getProvider(),
		result.Song,
		result.Singer,
		result.URL,
		result.Cover,
		result.Lyric,
	)

	_, err = m.message.Send(&message.Message{
		Receiver: msg.Sender,
		Type:     message.TypeAppMusic,
		Content:  fmt.Sprintf("[音乐] %s - %s", result.Song, result.Singer),
		Data: &message.Message_App{App: &message.AppData{
			SubType: 76, // 音乐子类型
			Title:   result.Song,
			Desc:    result.Singer,
			Xml:     xmlContent,
		}},
	})
	if err != nil {
		slog.Warn("[music] 发送消息失败", "err", err)
		return false, err
	}
	return true, nil
}

func hasOnePrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func ltrim(s string, prefixes ...string) string {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return strings.TrimPrefix(s, p)
		}
	}
	return s
}

type musicResult struct {
	Song   string `json:"song,omitempty"`
	Singer string `json:"singer,omitempty"`
	URL    string `json:"url,omitempty"`
	Cover  string `json:"cover,omitempty"`
	Lyric  string `json:"lyric,omitempty"`
}

type musicAPIResponse struct {
	Code int           `json:"code"`
	Data []musicResult `json:"data"`
}

func parseMusicResponse(body []byte) (*musicResult, error) {
	var resp musicAPIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("json 解析失败：%w", err)
	}
	if resp.Code != 200 || len(resp.Data) == 0 {
		return nil, fmt.Errorf("接口返回错误：code=%d, 结果数=%d", resp.Code, len(resp.Data))
	}
	return &resp.Data[0], nil
}

func main() {
	plugin.Start(&MusicPlugin{})
}
