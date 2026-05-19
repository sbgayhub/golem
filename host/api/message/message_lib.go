//go:build lib

// Package messageapi 提供消息服务的 lib 实现（直接调用底层实现）。
package messageapi

import (
	"bytes"
	"errors"
	"io"
	"sync"

	"golem/pkg/message"

	"github.com/sbgayhub/golem/host/api/util"
)

// lib 消息服务 lib 实现
type lib struct{}

// Get 获取 MessageService 单例（lib 模式）
var Get = sync.OnceValue(func() MessageService {
	return &lib{}
})

// Sync 同步消息
func (l lib) Sync(selector uint32) (*SyncMessageResponse, error) {
	_, err := message.Sync(selector)
	if err != nil {
		return nil, err
	}
	return &SyncMessageResponse{Code: 0, Message: "ok"}, nil
}

// SendText 发送文本消息
func (l lib) SendText(receiver, content, remind string) (*SendMessageResponse, error) {
	resp, err := message.SendText(receiver, content, remind)
	if resp == nil || err != nil {
		return nil, err
	}
	var result SendMessageResponse
	if err := util.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SendImage 发送图片消息
func (l lib) SendImage(receiver string, reader io.Reader) (*UploadImageResponse, error) {
	resp, err := message.SendImage(receiver, reader)
	if resp == nil || err != nil {
		return nil, err
	}
	var result UploadImageResponse
	if err := util.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SendVideo 发送视频消息
func (l lib) SendVideo(receiver string, thumb, video io.Reader, duration uint32) (*UploadVideoResponse, error) {
	resp, err := message.SendVideo(receiver, thumb, video, duration)
	if resp == nil || err != nil {
		return nil, err
	}
	var result UploadVideoResponse
	if err := util.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SendVoice 发送语音消息
func (l lib) SendVoice(receiver string, reader io.Reader, duration, format int32) (*UploadVoiceResponse, error) {
	resp, err := message.SendVoice(receiver, reader, duration, format)
	if resp == nil || err != nil {
		return nil, err
	}
	var result UploadVoiceResponse
	if err := util.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SendEmoji 发送表情消息
func (l lib) SendEmoji(receiver, md5 string, data []byte) (*UploadEmojiResponse, error) {
	var reader io.Reader
	if len(data) > 0 {
		reader = bytes.NewReader(data)
	}
	resp, err := message.SendEmoji(receiver, md5, reader)
	if resp == nil || err != nil {
		return nil, err
	}
	var result UploadEmojiResponse
	if err := util.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SendApp 发送应用消息
func (l lib) SendApp(receiver, xml string, typ int32) (*SendAppMessageResponse, error) {
	resp, err := message.SendApp(receiver, xml, typ)
	if resp == nil || err != nil {
		return nil, err
	}
	var result SendAppMessageResponse
	if err := util.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SendLink 发送链接消息
func (l lib) SendLink(receiver, title, desc, url, thumbUrl string) (*SendAppMessageResponse, error) {
	resp, err := message.SendLink(receiver, title, desc, url, thumbUrl)
	if resp == nil || err != nil {
		return nil, err
	}
	var result SendAppMessageResponse
	if err := util.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SendCard 发送名片消息
func (l lib) SendCard(receiver, cardUsername, cardNickname, cardAlias string) (*SendMessageResponse, error) {
	resp, err := message.SendCard(receiver, cardUsername, cardNickname, cardAlias)
	if resp == nil || err != nil {
		return nil, err
	}
	var result SendMessageResponse
	if err := util.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SendPosition 发送位置消息
func (l lib) SendPosition(receiver, label, poiName string, lon, lat, scale float64) (*SendMessageResponse, error) {
	resp, err := message.SendPosition(receiver, label, poiName, lon, lat, scale)
	if resp == nil || err != nil {
		return nil, err
	}
	var result SendMessageResponse
	if err := util.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ForwardImage 转发 CDN 图片（lib 模式不支持，内部类型受模块边界保护）
func (l lib) ForwardImage(receiver string, reader io.Reader) (*UploadImageResponse, error) {
	return nil, errors.New("ForwardImage not supported in lib mode, use web mode")
}

// ForwardVideo 转发 CDN 视频（lib 模式不支持）
func (l lib) ForwardVideo(receiver string, reader io.Reader) (*UploadVideoResponse, error) {
	return nil, errors.New("ForwardVideo not supported in lib mode, use web mode")
}

// ForwardFile 转发文件
func (l lib) ForwardFile(receiver, xml string) (*SendAppMessageResponse, error) {
	resp, err := message.ForwardFile(receiver, xml)
	if resp == nil || err != nil {
		return nil, err
	}
	var result SendAppMessageResponse
	if err := util.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Revoke 撤回消息
func (l lib) Revoke(receiver string, newMsgId, clientMsgId, timestamp uint64) (*RevokeMessageResponse, error) {
	resp, err := message.Revoke(receiver, newMsgId, clientMsgId, timestamp)
	if resp == nil || err != nil {
		return nil, err
	}
	var result RevokeMessageResponse
	if err := util.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DownloadImg 下载消息图片（lib 模式不支持，内部类型受模块边界保护）
func (l lib) DownloadImg(receiver, fileId, aesKey string, totalSize uint32) (*DownloadImageResponse, error) {
	return nil, errors.New("DownloadImg not supported in lib mode, use web mode")
}

// DownloadVideo 下载视频（lib 模式不支持）
func (l lib) DownloadVideo(receiver, fileId, aesKey string, totalSize uint32) (*DownloadVideoResponse, error) {
	return nil, errors.New("DownloadVideo not supported in lib mode, use web mode")
}

// DownloadVoice 下载语音（lib 模式不支持）
func (l lib) DownloadVoice(receiver, fileId, aesKey string, totalSize, voiceLength uint32) (*DownloadVoiceResponse, error) {
	return nil, errors.New("DownloadVoice not supported in lib mode, use web mode")
}

// DownloadFile 下载文件附件（lib 模式不支持）
func (l lib) DownloadFile(receiver, fileId, aesKey string, totalSize uint32) (*DownloadFileResponse, error) {
	return nil, errors.New("DownloadFile not supported in lib mode, use web mode")
}
