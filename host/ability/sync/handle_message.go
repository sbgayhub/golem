package sync

import (
	"fmt"
	"log/slog"

	messageability "github.com/sbgayhub/golem/host/ability/message"
	messageapi "github.com/sbgayhub/golem/host/api/message"
	"github.com/sbgayhub/golem/host/plugin"
	contactsdk "github.com/sbgayhub/golem/sdk/contact"
	messagesdk "github.com/sbgayhub/golem/sdk/message"
	pluginsdk "github.com/sbgayhub/golem/sdk/plugin"
)

func handleMessage(messages []*messageapi.NewMessage) {
	for _, msg := range messages {
		if data, err := messageability.Build(msg); err != nil {
			slog.Error("构建消息失败", "err", err)
		} else {
			log(data)

			if msg, ok := plugin.HandleCommand(data.GetContent(), data.GetSender()); ok {
				if _, err := messagesdk.Instance.Send(msg); err != nil {
					slog.Warn("命令回复失败", "receiver", data.GetSender().GetUsername(), "err", err)
				}
				continue
			}

			plugin.Publish(&pluginsdk.Event{
				Topic:  data.GetType().GetTopic(),
				Sender: data.GetSender().GetUsername(),
				Payload: &pluginsdk.Event_Message{
					Message: data,
				},
			})
		}
	}
}

func log(message *messagesdk.Message) {
	if message.Sender.GetType() == contactsdk.ContactType_CONTACT_TYPE_CHATROOM {
		sender := "system"
		if message.Member != nil {
			sender = message.Member.Nickname
		}
		slog.Info(fmt.Sprintf("%s -> %s: [%s] %s", sender, message.Sender.GetNickname(), message.Type.Desc, message.Content))
	} else {
		slog.Info(fmt.Sprintf("%s -> %s: [%s] %s", message.Sender.GetNickname(), message.Receiver.GetNickname(), message.Type.Desc, message.Content))
	}
}
