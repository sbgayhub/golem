//go:build !lib

// Package messageapi 提供消息服务的 web 实现（通过 HTTP 调用远程服务）。
package messageapi

import (
	"fmt"
	"io"
	"sync"

	"github.com/sbgayhub/golem/host/api/util"
)

// web 消息服务 web 实现
type web struct{}

// Get 获取 MessageService 单例（web 模式）
var Get = sync.OnceValue(func() MessageService {
	return &web{}
})

// Sync 同步消息
func (w web) Sync(selector uint32) (*SyncMessageResponse, error) {
	data, err := util.GetHttp().Get(fmt.Sprintf("/message/sync?selector=%d", selector))
	if err != nil {
		return nil, err
	}
	var resp SyncMessageResponse
	if err := util.ParseProtoResponse(data, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendText 发送文本消息
func (w web) SendText(receiver, content, remind string) (*SendMessageResponse, error) {
	req := SendTextRequest{Receiver: receiver, Content: content, Remind: remind}
	data, err := util.GetHttp().Post("/message/send/text", &req)
	if err != nil {
		return nil, err
	}
	var resp SendMessageResponse
	if err := util.ParseProtoResponse(data, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendImage 发送图片消息
func (w web) SendImage(receiver string, reader io.Reader) (*UploadImageResponse, error) {
	req := SendImageRequest{Receiver: receiver}
	if b, err := io.ReadAll(reader); err == nil {
		req.Data = b
	} else {
		return nil, err
	}
	data, err := util.GetHttp().Post("/message/send/image", &req)
	if err != nil {
		return nil, err
	}
	var resp UploadImageResponse
	if err := util.ParseProtoResponse(data, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendVideo 发送视频消息
func (w web) SendVideo(receiver string, thumb, video io.Reader, duration uint32) (*UploadVideoResponse, error) {
	req := SendVideoRequest{Receiver: receiver, Duration: duration}
	if b, err := io.ReadAll(thumb); err == nil {
		req.Thumb = b
	} else {
		return nil, err
	}
	if b, err := io.ReadAll(video); err == nil {
		req.Video = b
	} else {
		return nil, err
	}
	data, err := util.GetHttp().Post("/message/send/video", &req)
	if err != nil {
		return nil, err
	}
	var resp UploadVideoResponse
	if err := util.ParseProtoResponse(data, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendVoice 发送语音消息
func (w web) SendVoice(receiver string, reader io.Reader, duration, format int32) (*UploadVoiceResponse, error) {
	req := SendVoiceRequest{Receiver: receiver, Duration: duration, Format: format}
	if b, err := io.ReadAll(reader); err == nil {
		req.Data = b
	} else {
		return nil, err
	}
	data, err := util.GetHttp().Post("/message/send/voice", &req)
	if err != nil {
		return nil, err
	}
	var resp UploadVoiceResponse
	if err := util.ParseProtoResponse(data, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendEmoji 发送表情消息
func (w web) SendEmoji(receiver, md5 string, data []byte) (*UploadEmojiResponse, error) {
	req := SendEmojiRequest{Receiver: receiver, Md5: md5, Data: data}
	respData, err := util.GetHttp().Post("/message/send/emoji", &req)
	if err != nil {
		return nil, err
	}
	var resp UploadEmojiResponse
	if err := util.ParseProtoResponse(respData, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendApp 发送应用消息
func (w web) SendApp(receiver, xml string, typ int32) (*SendAppMessageResponse, error) {
	req := SendAppRequest{Receiver: receiver, Xml: xml, Type: typ}
	data, err := util.GetHttp().Post("/message/send/app", &req)
	if err != nil {
		return nil, err
	}
	var resp SendAppMessageResponse
	if err := util.ParseProtoResponse(data, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendLink 发送链接消息
func (w web) SendLink(receiver, title, desc, url, thumbUrl string) (*SendAppMessageResponse, error) {
	req := SendLinkRequest{Receiver: receiver, Title: title, Description: desc, Url: url, ThumbUrl: thumbUrl}
	data, err := util.GetHttp().Post("/message/send/link", &req)
	if err != nil {
		return nil, err
	}
	var resp SendAppMessageResponse
	if err := util.ParseProtoResponse(data, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendCard 发送名片消息
func (w web) SendCard(receiver, cardUsername, cardNickname, cardAlias string) (*SendMessageResponse, error) {
	req := SendCardRequest{Receiver: receiver, CardUsername: cardUsername, CardNickname: cardNickname, CardAlias: cardAlias}
	data, err := util.GetHttp().Post("/message/send/card", &req)
	if err != nil {
		return nil, err
	}
	var resp SendMessageResponse
	if err := util.ParseProtoResponse(data, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendPosition 发送位置消息
func (w web) SendPosition(receiver, label, poiName string, lon, lat, scale float64) (*SendMessageResponse, error) {
	req := SendPositionRequest{Receiver: receiver, Label: label, PoiName: poiName, Longitude: lon, Latitude: lat, Scale: scale}
	data, err := util.GetHttp().Post("/message/send/position", &req)
	if err != nil {
		return nil, err
	}
	var resp SendMessageResponse
	if err := util.ParseProtoResponse(data, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ForwardImage 转发 CDN 图片
func (w web) ForwardImage(receiver string, reader io.Reader) (*UploadImageResponse, error) {
	return w.SendImage(receiver, reader)
}

// ForwardVideo 转发 CDN 视频
func (w web) ForwardVideo(receiver string, reader io.Reader) (*UploadVideoResponse, error) {
	return w.SendVideo(receiver, nil, reader, 0)
}

// ForwardFile 转发文件
func (w web) ForwardFile(receiver, xml string) (*SendAppMessageResponse, error) {
	return w.SendApp(receiver, xml, 0)
}

// Revoke 撤回消息
func (w web) Revoke(receiver string, newMsgId, clientMsgId, timestamp uint64) (*RevokeMessageResponse, error) {
	req := RevokeMessageRequest{Receiver: receiver, NewMsgId: newMsgId, ClientMsgId: clientMsgId, Timestamp: timestamp}
	data, err := util.GetHttp().Post("/message/revoke", &req)
	if err != nil {
		return nil, err
	}
	var resp RevokeMessageResponse
	if err := util.ParseProtoResponse(data, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DownloadImg 下载消息图片
func (w web) DownloadImg(receiver, fileId, aesKey string, totalSize uint32) (*DownloadImageResponse, error) {
	req := DownloadImgRequest{Receiver: receiver, FileId: fileId, AesKey: aesKey, TotalSize: totalSize}
	data, err := util.GetHttp().Post("/message/download/image", &req)
	if err != nil {
		return nil, err
	}
	var resp DownloadImageResponse
	if err := util.ParseProtoResponse(data, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DownloadVideo 下载视频
func (w web) DownloadVideo(receiver, fileId, aesKey string, totalSize uint32) (*DownloadVideoResponse, error) {
	req := DownloadVideoRequest{Receiver: receiver, FileId: fileId, AesKey: aesKey, TotalSize: totalSize}
	data, err := util.GetHttp().Post("/message/download/video", &req)
	if err != nil {
		return nil, err
	}
	var resp DownloadVideoResponse
	if err := util.ParseProtoResponse(data, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DownloadVoice 下载语音
func (w web) DownloadVoice(receiver, fileId, aesKey string, totalSize, voiceLength uint32) (*DownloadVoiceResponse, error) {
	req := DownloadVoiceRequest{Receiver: receiver, FileId: fileId, AesKey: aesKey, TotalSize: totalSize, VoiceLength: voiceLength}
	data, err := util.GetHttp().Post("/message/download/voice", &req)
	if err != nil {
		return nil, err
	}
	var resp DownloadVoiceResponse
	if err := util.ParseProtoResponse(data, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DownloadFile 下载文件附件
func (w web) DownloadFile(receiver, fileId, aesKey string, totalSize uint32) (*DownloadFileResponse, error) {
	req := DownloadFileRequest{Receiver: receiver, FileId: fileId, AesKey: aesKey, TotalSize: totalSize}
	data, err := util.GetHttp().Post("/message/download/file", &req)
	if err != nil {
		return nil, err
	}
	var resp DownloadFileResponse
	if err := util.ParseProtoResponse(data, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
