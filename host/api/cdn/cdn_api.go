// Package cdnapi 提供 CDN 文件上传/下载服务的 API 接口定义。
package cdnapi

import (
	"io"
)

// CDNService CDN 服务 API 接口（纯协议层通信）
type CDNService interface {
	// UploadImage CDN 上传聊天图片
	UploadImage(receiver string, reader io.Reader) (*UploadImageResult, error)
	// UploadVideo CDN 上传聊天视频
	UploadVideo(receiver string, thumb []byte, reader io.Reader, duration uint32) (*UploadVideoResult, error)
	// DownloadImage CDN 下载高清图片
	DownloadImage(fileID, fileAesKey string) (io.ReadCloser, error)
	// DownloadVideo CDN 下载聊天视频
	DownloadVideo(fileID, fileAesKey string) (io.ReadCloser, error)

	// UploadMomentsImage CDN 上传朋友圈图片
	UploadMomentsImage(imageData []byte) (*UploadSnsImageResult, error)
	// UploadMomentsVideo CDN 上传朋友圈视频
	UploadMomentsVideo(videoData, thumbData []byte) (*UploadSnsVideoResult, error)
	// DownloadVideoCover CDN 下载视频封面
	DownloadVideoCover(fileID, fileAesKey string) ([]byte, error)
	// DownloadSnsVideo CDN 下载朋友圈视频
	DownloadSnsVideo(videoURL string, encKey uint64) ([]byte, error)
}
