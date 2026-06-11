// Package messageability 提供消息能力的实现。
package messageability

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	api "github.com/sbgayhub/golem/host/api/message"
	"github.com/sbgayhub/golem/sdk/contact"
	sdk "github.com/sbgayhub/golem/sdk/message"
)

// ability 消息能力实现
type ability struct {
	api api.MessageService
}

var (
	outboundReady       atomic.Bool
	errOutboundNotReady = errors.New("基础数据初始化未完成，已禁止发送消息")
)

func init() {
	outboundReady.Store(true)
	sdk.Instance = &ability{api: api.Get()}
}

// SetOutboundReady 设置消息出站开关。
func SetOutboundReady(ready bool) {
	outboundReady.Store(ready)
}

// OutboundReady 返回消息出站是否可用。
func OutboundReady() bool {
	return outboundReady.Load()
}

func ensureOutboundReady() error {
	if !OutboundReady() {
		return errOutboundNotReady
	}
	return nil
}

// Send 发送消息（根据类型分发到对应 API）
func (a *ability) Send(msg *sdk.Message) (*sdk.Send_Response, error) {
	if msg == nil {
		return nil, errors.New("消息不可为空")
	}
	if msg.GetReceiver() == nil || msg.GetReceiver().GetUsername() == "" {
		return nil, errors.New("消息接收人不可为空")
	}
	if err := ensureOutboundReady(); err != nil {
		return nil, err
	}

	var result sdk.Send_Response
	receiver := msg.GetReceiver().GetUsername()
	slog.Info(fmt.Sprintf("[%s] -> %s : %s", msg.GetType().GetDesc(), msg.GetReceiver().GetNickname(), msg.Content))

	switch msg.GetType().GetCode() {
	case sdk.TypeText.Code:
		data := msg.GetText()
		content := msg.GetContent()
		if content == "" {
			content = data.GetContent()
		}
		if content == "" {
			return nil, errors.New("消息正文不可为空")
		}
		remind := strings.Join(data.GetReminds(), ",")
		resp, err := a.api.SendText(receiver, content, remind)
		if err != nil {
			return nil, err
		}
		if len(resp.GetList()) > 0 {
			item := resp.GetList()[0]
			result.NewId = item.GetNewId()
			result.CreateTime = item.GetCreateTime()
		}
	case sdk.TypeImage.Code:
		data := msg.GetImage()
		if data.GetMedia() == nil {
			break
		}
		resp, err := a.api.SendImage(receiver, bytes.NewReader(data.GetMedia().Data))
		if err != nil {
			return nil, err
		}
		result.NewId = resp.GetNewId()
		result.CreateTime = resp.GetCreateTime()
		result.Media = &sdk.Media{
			Key:  resp.GetAesKey(),
			Url:  resp.GetFileId(),
			Size: resp.GetSize(),
		}
	case sdk.TypeVoice.Code:
		data := msg.GetVoice()
		if data.GetMedia() == nil {
			break
		}
		resp, err := a.api.SendVoice(receiver, bytes.NewReader(data.GetMedia().Data), int32(data.GetDuration()), 0)
		if err != nil {
			return nil, err
		}
		result.NewId = resp.GetNewId()
		result.CreateTime = resp.GetCreateTime()
		result.Media = &sdk.Media{
			Url:  resp.GetClientId(),
			Size: uint32(resp.GetSize()),
		}
	case sdk.TypeVideo.Code:
		data := msg.GetVideo()
		if data.GetMedia() == nil {
			break
		}
		resp, err := a.api.SendVideo(receiver, nil, bytes.NewReader(data.GetMedia().Data), data.GetDuration())
		if err != nil {
			return nil, err
		}
		result.NewId = resp.GetNewId()
		//result.CreateTime = resp.CreateTime
		result.Media = &sdk.Media{
			Key:  resp.GetAesKey(),
			Url:  resp.GetExtendXml(),
			Size: uint32(resp.GetVideoOffset()),
		}
	case sdk.TypeEmoji.Code:
		data := msg.GetEmoji()
		if data.GetMedia() == nil {
			break
		}
		resp, err := a.api.SendEmoji(receiver, data.GetMedia().GetMd5(), data.GetMedia().Data)
		if err != nil {
			return nil, err
		}
		if len(resp.GetResult()) > 0 {
			item := resp.GetResult()[0]
			result.NewId = item.GetNewId()
			result.Media = &sdk.Media{Md5: item.GetMd5(), Size: uint32(item.GetSize())}
		}
	case sdk.TypeLocation.Code:
		data := msg.GetLocation()
		resp, err := a.api.SendPosition(receiver, data.GetLabel(), data.GetPoiName(), data.GetLongitude(), data.GetLatitude(), float64(data.GetScale()))
		if err != nil {
			return nil, err
		}
		if len(resp.GetList()) > 0 {
			item := resp.GetList()[0]
			result.NewId = item.GetNewId()
			result.CreateTime = item.GetCreateTime()
		}
	case sdk.TypeApplication.Code:
		data := msg.GetApp()
		resp, err := a.api.SendApp(receiver, data.GetXml(), int32(data.GetSubType()))
		if err != nil {
			return nil, err
		}
		result.NewId = resp.GetNewId()
		result.CreateTime = uint32(resp.GetCreateTime())
	default:
		slog.Debug("发送消息", "content", msg.Content)
	}

	// 如果有撤回时间
	if msg.Timestamp != 0 && result.NewId != 0 {
		time.AfterFunc(time.Duration(msg.Timestamp)*time.Second, func() {
			if _, err := a.api.Revoke(receiver, result.NewId, 0, uint64(result.CreateTime)); err != nil {
				slog.Warn("消息撤回失败", "err", err)
			}
		})
	}
	return &result, nil
}

// Forward 转发消息（根据类型调用对应转发 API）
func (a *ability) Forward(msg *sdk.Message, receiver string) (*sdk.Forward_Response, error) {
	if err := ensureOutboundReady(); err != nil {
		return nil, err
	}
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
		return &sdk.Forward_Response{NewId: resp.NewId, CreateTime: resp.CreateTime}, nil
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
		return io.NopCloser(bytes.NewReader(resp.GetChunk().GetData())), nil
	case sdk.TypeVideo:
		data := msg.GetVideo()
		if data.GetMedia() == nil {
			return io.NopCloser(bytes.NewReader(nil)), nil
		}
		resp, err := a.api.DownloadVideo(receiver, data.GetMedia().GetMd5(), data.GetMedia().GetKey(), data.GetMedia().GetSize())
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(resp.GetChunk().GetData())), nil
	case sdk.TypeVoice:
		data := msg.GetVoice()
		if data.GetMedia() == nil {
			return io.NopCloser(bytes.NewReader(nil)), nil
		}
		resp, err := a.api.DownloadVoice(receiver, data.GetMedia().GetMd5(), data.GetMedia().GetKey(), data.GetMedia().GetSize(), data.GetDuration())
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(resp.GetData().GetData())), nil
	}
	return nil, io.ErrUnexpectedEOF
}
