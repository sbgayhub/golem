package main

import (
	"log/slog"
	"reflect"

	"github.com/sbgayhub/golem/sdk/message"
	"github.com/sbgayhub/golem/sdk/plugin"
)

func main() {
	plugin.Start(&RereadPlugin{
		read:  make(map[string]data),
		cache: make(map[string]data),
	})
}

type RereadPlugin struct {
	message message.Ability
	read    map[string]data
	cache   map[string]data
}

type data struct {
	typ     string
	content string
}

func (r *RereadPlugin) GetMetadata() *plugin.Metadata {
	return &plugin.Metadata{
		Name:        "reread",
		Author:      "ovo",
		Version:     "v1.0.0",
		Description: "人类的本质是复读机，复读消息或表情",
		Priority:    0,
		Next:        false,
		AlwaysRun:   false,
	}
}

func (r *RereadPlugin) GetSubscriptions() []string {
	return []string{message.TypeText.Topic, message.TypeEmoji.Topic}
}

func (r *RereadPlugin) OnEvent(event *plugin.Event) (bool, error) {
	msg := event.GetPayload().(*plugin.Event_Message).Message
	temp := data{typ: msg.Type.Topic, content: msg.Content}

	// 判断有没有复读过
	if read, ex := r.read[event.GetSender()]; ex && reflect.DeepEqual(read, temp) {
		slog.Debug("复读过了", "type", temp.typ, "content", temp.content)
		return false, nil
	}

	// 未复读过，判断消息与上一条是否一样，一样则复读
	if cache, ex := r.cache[event.GetSender()]; ex && reflect.DeepEqual(cache, temp) {
		msg.Receiver = msg.GetSender()
		if _, err := r.message.Send(msg); err != nil {
			return false, err
		}
		// 添加到已复读
		r.read[event.GetSender()] = temp
		return true, nil
	}

	// 添加到消息缓存
	r.cache[event.GetSender()] = temp
	return false, nil
}
