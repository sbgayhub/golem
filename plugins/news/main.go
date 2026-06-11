package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/sbgayhub/golem/sdk/contact"
	"github.com/sbgayhub/golem/sdk/message"
	"github.com/sbgayhub/golem/sdk/plugin"
)

var keywords = []string{"今日新闻", "今日图卦"}

type NewsPlugin struct {
	message message.Ability
	contact contact.Ability
}

func (n *NewsPlugin) GetMetadata() *plugin.Metadata {
	return &plugin.Metadata{
		Name:        "news",
		Author:      "ovo",
		Version:     "v0.0.1",
		Description: "新闻插件，根据“今日新闻”、“今日图卦”关键词返回新闻图片",
		Priority:    0,
		Next:        false,
		AlwaysRun:   false,
	}
}

func (n *NewsPlugin) GetSubscriptions() []string {
	return []string{message.TypeText.Topic}
}

func (n *NewsPlugin) GetCapabilities() []string {
	return []string{"news.today", "news.diagram"}
}

func (n *NewsPlugin) OnCall(capability string, args map[string]string) (string, []byte, error) {
	receiver, ex := args["receiver"]
	if !ex || receiver == "" {
		return "", nil, errors.New("receiver 不可为空")
	}
	c := n.contact.Get(receiver)
	if c == nil {
		return "", nil, errors.New("未找到联系人：" + receiver)
	}
	switch capability {
	case "news.today":
		if _, err := n.news(c); err != nil {
			return "", nil, err
		}
		return "none", nil, nil
	case "news.diagram":
		if _, err := n.diagram(c); err != nil {
			return "", nil, err
		}
		return "none", nil, nil
	default:
		return "", nil, errors.New("不支持：" + capability)
	}
}

func (n *NewsPlugin) OnEvent(event *plugin.Event) (bool, error) {
	msg := event.Payload.(*plugin.Event_Message).Message
	if slices.Contains(keywords, msg.Content) {
		switch msg.Content {
		case "今日新闻":
			return n.news(msg.Sender)
		case "今日图卦":
			return n.diagram(msg.Sender)
		default:
			slog.Warn("暂不支持：" + msg.Content)
		}
	}
	return false, nil
}

func (n *NewsPlugin) news(receiver *contact.Contact) (bool, error) {
	date := time.Now().Format("2006-01-02")
	resp, err := http.DefaultClient.Get(fmt.Sprintf("https://cdn.jsdmirror.com/gh/vikiboss/60s-static-host@main/static/images/%s.png", date))
	if err != nil {
		return false, errors.New("[今日新闻] 请求失败")
	}
	defer func() { _ = resp.Body.Close() }()

	all, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("[今日新闻] 读取响应失败：%w", err)
	}

	data := &message.Message{
		Content:  "今日新闻",
		Receiver: receiver,
		Type:     message.TypeImage,
		Data:     &message.Message_Image{Image: &message.ImageData{Media: &message.Media{Data: all}}},
	}
	if _, err := n.message.Send(data); err != nil {
		return false, fmt.Errorf("[今日新闻] 发送消息失败：%w", err)
	}
	return true, nil
}

func (n *NewsPlugin) diagram(receiver *contact.Contact) (bool, error) {
	// 获取一天前的日期
	t := time.Now().AddDate(0, 0, -1)
	month := t.Format("200601")
	date := t.Format("20060102")
	template := "https://penti.5aihj.com/%s/tugua/%s%d.jpg"
	for i := range 2 {
		time.Sleep(1 * time.Second)
		url := fmt.Sprintf(template, month, date, i+1)
		resp, err := http.DefaultClient.Get(url)
		if err != nil {
			return false, errors.New("[今日图卦] 请求失败")
		}

		all, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return false, fmt.Errorf("[今日图卦] 读取响应失败：%w", err)
		}

		data := &message.Message{
			Content:  "今日图卦",
			Receiver: receiver,
			Type:     message.TypeImage,
			Data:     &message.Message_Image{Image: &message.ImageData{Media: &message.Media{Data: all}}},
		}
		if _, err := n.message.Send(data); err != nil {
			return false, fmt.Errorf("[今日图卦] 发送消息失败：%w", err)
		}
	}
	return true, nil
}

func main() {
	plugin.Start(&NewsPlugin{})
}
