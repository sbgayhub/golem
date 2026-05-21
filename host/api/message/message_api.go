// Package messageapi 提供消息服务的 API 接口定义。
package messageapi

import (
	"io"
)

// MessageService 消息服务 API 接口（返回 API proto 类型）
type MessageService interface {
	// Sync 同步消息（返回新消息、联系人变更、用户信息变更等同步数据）
	Sync(selector uint32) (*SyncResult, error)
	// SendText 发送文本消息
	SendText(receiver, content, remind string) (*SendMessageResponse, error)
	// SendImage 发送图片消息
	SendImage(receiver string, reader io.Reader) (*UploadImageResponse, error)
	// SendVideo 发送视频消息
	SendVideo(receiver string, thumb, video io.Reader, duration uint32) (*UploadVideoResponse, error)
	// SendVoice 发送语音消息
	SendVoice(receiver string, reader io.Reader, duration, format int32) (*UploadVoiceResponse, error)
	// SendEmoji 发送表情消息
	SendEmoji(receiver, md5 string, data []byte) (*UploadEmojiResponse, error)
	// SendApp 发送应用消息
	SendApp(receiver, xml string, typ int32) (*SendAppMessageResponse, error)
	// SendLink 发送链接消息
	SendLink(receiver, title, desc, url, thumbUrl string) (*SendAppMessageResponse, error)
	// SendCard 发送名片消息
	SendCard(receiver, cardUsername, cardNickname, cardAlias string) (*SendMessageResponse, error)
	// SendPosition 发送位置消息
	SendPosition(receiver, label, poiName string, lon, lat, scale float64) (*SendMessageResponse, error)
	// ForwardImage 转发 CDN 图片
	ForwardImage(receiver string, reader io.Reader) (*UploadImageResponse, error)
	// ForwardVideo 转发 CDN 视频
	ForwardVideo(receiver string, reader io.Reader) (*UploadVideoResponse, error)
	// ForwardFile 转发文件
	ForwardFile(receiver, xml string) (*SendAppMessageResponse, error)
	// Revoke 撤回消息
	Revoke(receiver string, newMsgId, clientMsgId, timestamp uint64) (*RevokeMessageResponse, error)
	// DownloadImg 下载消息图片
	DownloadImg(receiver, fileId, aesKey string, totalSize uint32) (*DownloadImageResponse, error)
	// DownloadVideo 下载视频
	DownloadVideo(receiver, fileId, aesKey string, totalSize uint32) (*DownloadVideoResponse, error)
	// DownloadVoice 下载语音
	DownloadVoice(receiver, fileId, aesKey string, totalSize, voiceLength uint32) (*DownloadVoiceResponse, error)
	// DownloadFile 下载文件附件
	DownloadFile(receiver, fileId, aesKey string, totalSize uint32) (*DownloadFileResponse, error)
}
