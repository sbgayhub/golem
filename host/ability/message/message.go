// Package messageability 提供消息能力的实现。
package messageability

import (
	"bytes"
	"io"
	"log/slog"
	"strings"

	api "github.com/sbgayhub/golem/host/api/message"
	"github.com/sbgayhub/golem/sdk/contact"
	sdk "github.com/sbgayhub/golem/sdk/message"
)

// ability 消息能力实现
type ability struct {
	api api.MessageService
}

func init() {
	sdk.Instance = &ability{api: api.Get()}
}

// Send 发送消息（根据类型分发到对应 API）
func (a *ability) Send(msg *sdk.Message) (*sdk.Send_Response, error) {
	receiver := msg.GetReceiver().GetUsername()
	switch msg.GetType() {
	case sdk.TypeText:
		data := msg.GetText()
		content := msg.GetContent()
		if content == "" {
			content = data.GetContent()
		}
		remind := strings.Join(data.GetReminds(), ",")
		_, err := a.api.SendText(receiver, content, remind)
		if err != nil {
			return nil, err
		}
	case sdk.TypeImage:
		data := msg.GetImage()
		if data.GetMedia() == nil {
			break
		}
		_, err := a.api.SendImage(receiver, bytes.NewReader(nil))
		if err != nil {
			return nil, err
		}
	case sdk.TypeVoice:
		data := msg.GetVoice()
		if data.GetMedia() == nil {
			break
		}
		_, err := a.api.SendVoice(receiver, bytes.NewReader(nil), int32(data.GetDuration()), 0)
		if err != nil {
			return nil, err
		}
	case sdk.TypeVideo:
		data := msg.GetVideo()
		if data.GetMedia() == nil {
			break
		}
		_, err := a.api.SendVideo(receiver, nil, bytes.NewReader(nil), data.GetDuration())
		if err != nil {
			return nil, err
		}
	case sdk.TypeEmoji:
		data := msg.GetEmoji()
		if data.GetMedia() == nil {
			break
		}
		_, err := a.api.SendEmoji(receiver, data.GetMedia().GetMd5(), nil)
		if err != nil {
			return nil, err
		}
	case sdk.TypeLocation:
		data := msg.GetLocation()
		_, err := a.api.SendPosition(receiver, data.GetLabel(), data.GetPoiName(), 0, 0, 0)
		if err != nil {
			return nil, err
		}
	case sdk.TypeApplication:
		data := msg.GetApp()
		_, err := a.api.SendApp(receiver, data.GetXml(), int32(data.GetSubType()))
		if err != nil {
			return nil, err
		}
	default:
		slog.Debug("发送消息", "content", msg.Content)
	}
	return &sdk.Send_Response{}, nil
}

// Forward 转发消息（根据类型调用对应转发 API）
func (a *ability) Forward(msg *sdk.Message, receiver string) (*sdk.Forward_Response, error) {
	switch msg.GetType() {
	case sdk.TypeImage:
		data := msg.GetImage()
		if data.GetMedia() != nil {
			_, err := a.api.ForwardImage(receiver, bytes.NewReader(nil))
			if err != nil {
				return nil, err
			}
		}
	case sdk.TypeVideo:
		data := msg.GetVideo()
		if data.GetMedia() != nil {
			_, err := a.api.ForwardVideo(receiver, bytes.NewReader(nil))
			if err != nil {
				return nil, err
			}
		}
	case sdk.TypeApplication:
		data := msg.GetApp()
		_, err := a.api.ForwardFile(receiver, data.GetXml())
		if err != nil {
			return nil, err
		}
	default:
		// 文本等类型直接用 Send 转发
		msg.Receiver = &contact.Contact{Username: receiver}
		resp, err := a.Send(msg)
		if err != nil {
			return nil, err
		}
		return &sdk.Forward_Response{NewMsgId: resp.NewMsgId, CreateTime: resp.CreateTime}, nil
	}
	return &sdk.Forward_Response{}, nil
}

// Revoke 撤回消息
func (a *ability) Revoke(receiver string, newMsgId uint64) (*sdk.Revoke_Response, error) {
	_, err := a.api.Revoke(receiver, newMsgId, 0, 0)
	if err != nil {
		return nil, err
	}
	return &sdk.Revoke_Response{Code: 0}, nil
}

// Download 下载媒体资源
func (a *ability) Download(msg *sdk.Message) (io.ReadCloser, error) {
	receiver := msg.GetReceiver().GetUsername()
	switch msg.GetType() {
	case sdk.TypeImage:
		data := msg.GetImage()
		if data.GetMedia() == nil {
			return io.NopCloser(bytes.NewReader(nil)), nil
		}
		resp, err := a.api.DownloadImg(receiver, data.GetMedia().GetMd5(), data.GetMedia().GetKey(), data.GetMedia().GetSize())
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(resp.GetData())), nil
	case sdk.TypeVideo:
		data := msg.GetVideo()
		if data.GetMedia() == nil {
			return io.NopCloser(bytes.NewReader(nil)), nil
		}
		resp, err := a.api.DownloadVideo(receiver, data.GetMedia().GetMd5(), data.GetMedia().GetKey(), data.GetMedia().GetSize())
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(resp.GetData())), nil
	case sdk.TypeVoice:
		data := msg.GetVoice()
		if data.GetMedia() == nil {
			return io.NopCloser(bytes.NewReader(nil)), nil
		}
		resp, err := a.api.DownloadVoice(receiver, data.GetMedia().GetMd5(), data.GetMedia().GetKey(), data.GetMedia().GetSize(), data.GetDuration())
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(resp.GetData())), nil
	}
	return nil, io.ErrUnexpectedEOF
}
