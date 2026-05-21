//go:build !lib

// Package messageapi 提供消息服务的 web 实现（通过 HTTP 调用远程服务）。
package messageapi

import (
	"fmt"
	"io"
	"sync"
)

// web 消息服务 web 实现
type web struct{}

// Get 获取 MessageService 单例（web 模式）
var Get = sync.OnceValue(func() MessageService {
	return &web{}
})

// Sync 同步消息
func (w web) Sync(selector uint32) (*SyncResult, error) {
	var resp SyncResult
	if err := api.GetHttp().Get("/api/message/sync").Query("selector", selector).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendText 发送文本消息
func (w web) SendText(receiver, content, remind string) (*SendMessageResponse, error) {
	var resp SendMessageResponse
	body := map[string]any{
		"receiver": receiver,
		"content":  content,
		"remind":   remind,
	}
	if err := api.GetHttp().Post("/api/message/text").Body(body).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendImage 发送图片消息（multipart/form-data）
func (w web) SendImage(receiver string, reader io.Reader) (*UploadImageResponse, error) {
	imageData, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	var resp UploadImageResponse
	if err := api.GetHttp().Post("/api/message/image").Multipart(
		map[string][]byte{"image": imageData},
		map[string]string{"receiver": receiver},
	).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendVideo 发送视频消息（multipart/form-data）
func (w web) SendVideo(receiver string, thumb, video io.Reader, duration uint32) (*UploadVideoResponse, error) {
	videoData, err := io.ReadAll(video)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{"video": videoData}
	if thumb != nil {
		thumbData, err := io.ReadAll(thumb)
		if err != nil {
			return nil, err
		}
		files["thumb"] = thumbData
	}
	var resp UploadVideoResponse
	if err := api.GetHttp().Post("/api/message/video").Multipart(files,
		map[string]string{
			"receiver": receiver,
			"duration": fmt.Sprintf("%d", duration),
		},
	).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendVoice 发送语音消息（multipart/form-data）
func (w web) SendVoice(receiver string, reader io.Reader, duration, format int32) (*UploadVoiceResponse, error) {
	voiceData, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	var resp UploadVoiceResponse
	if err := api.GetHttp().Post("/api/message/voice").Multipart(
		map[string][]byte{"voice": voiceData},
		map[string]string{
			"receiver": receiver,
			"duration": fmt.Sprintf("%d", duration),
			"format":   fmt.Sprintf("%d", format),
		},
	).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendEmoji 发送表情消息（multipart/form-data）
func (w web) SendEmoji(receiver, md5 string, data []byte) (*UploadEmojiResponse, error) {
	var resp UploadEmojiResponse
	if err := api.GetHttp().Post("/api/message/emoji").Multipart(
		map[string][]byte{"emoji": data},
		map[string]string{"receiver": receiver, "md5": md5},
	).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendApp 发送应用消息
func (w web) SendApp(receiver, xml string, typ int32) (*SendAppMessageResponse, error) {
	var resp SendAppMessageResponse
	if err := api.GetHttp().Post("/api/message/app").Body(map[string]any{
		"receiver": receiver,
		"xml":      xml,
		"type":     typ,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendLink 发送链接消息
func (w web) SendLink(receiver, title, desc, url, thumbUrl string) (*SendAppMessageResponse, error) {
	var resp SendAppMessageResponse
	if err := api.GetHttp().Post("/api/message/link").Body(map[string]any{
		"receiver":    receiver,
		"title":       title,
		"description": desc,
		"url":         url,
		"thumb_url":   thumbUrl,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendCard 发送名片消息
func (w web) SendCard(receiver, cardUsername, cardNickname, cardAlias string) (*SendMessageResponse, error) {
	var resp SendMessageResponse
	if err := api.GetHttp().Post("/api/message/card").Body(map[string]any{
		"receiver":      receiver,
		"card_username": cardUsername,
		"card_nickname": cardNickname,
		"card_alias":    cardAlias,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendPosition 发送位置消息
func (w web) SendPosition(receiver, label, poiName string, lon, lat, scale float64) (*SendMessageResponse, error) {
	var resp SendMessageResponse
	if err := api.GetHttp().Post("/api/message/position").Body(map[string]any{
		"receiver":  receiver,
		"label":     label,
		"poi_name":  poiName,
		"longitude": lon,
		"latitude":  lat,
		"scale":     scale,
	}).DoProto(&resp); err != nil {
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
	var resp RevokeMessageResponse
	if err := api.GetHttp().Post("/api/message/revoke").Body(map[string]any{
		"receiver":      receiver,
		"new_msg_id":    newMsgId,
		"client_msg_id": clientMsgId,
		"timestamp":     timestamp,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DownloadImg 下载消息图片
func (w web) DownloadImg(receiver, fileId, aesKey string, totalSize uint32) (*DownloadImageResponse, error) {
	var resp DownloadImageResponse
	if err := api.GetHttp().Post("/api/message/download/image").Body(map[string]any{
		"receiver":   receiver,
		"file_id":    fileId,
		"aes_key":    aesKey,
		"total_size": totalSize,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DownloadVideo 下载视频
func (w web) DownloadVideo(receiver, fileId, aesKey string, totalSize uint32) (*DownloadVideoResponse, error) {
	var resp DownloadVideoResponse
	if err := api.GetHttp().Post("/api/message/download/video").Body(map[string]any{
		"receiver":   receiver,
		"file_id":    fileId,
		"aes_key":    aesKey,
		"total_size": totalSize,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DownloadVoice 下载语音
func (w web) DownloadVoice(receiver, fileId, aesKey string, totalSize, voiceLength uint32) (*DownloadVoiceResponse, error) {
	var resp DownloadVoiceResponse
	if err := api.GetHttp().Post("/api/message/download/voice").Body(map[string]any{
		"receiver":     receiver,
		"file_id":      fileId,
		"aes_key":      aesKey,
		"total_size":   totalSize,
		"voice_length": voiceLength,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DownloadFile 下载文件附件
func (w web) DownloadFile(receiver, fileId, aesKey string, totalSize uint32) (*DownloadFileResponse, error) {
	var resp DownloadFileResponse
	if err := api.GetHttp().Post("/api/message/download/file").Body(map[string]any{
		"receiver":   receiver,
		"file_id":    fileId,
		"aes_key":    aesKey,
		"total_size": totalSize,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
