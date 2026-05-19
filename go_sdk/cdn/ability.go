package cdn

import (
	"io"
)

type Ability interface {
	// UploadImage 客户端流式上传聊天图片
	UploadImage(receiver string, reader io.Reader) (*UploadImageResponse, error)
	// UploadVideo 客户端流式上传聊天视频
	UploadVideo(receiver string, thumb []byte, reader io.Reader, duration uint32) (*UploadVideoResponse, error)

	// DownloadImage 服务端流式下载高清图片
	DownloadImage(fileID, fileAesKey string) (io.ReadCloser, error)
	// DownloadVideo 服务端流式下载聊天视频
	DownloadVideo(fileID, fileAesKey string) (io.ReadCloser, error)

	// UploadMomentsImage 上传朋友圈图片（小文件）
	UploadMomentsImage(imageData []byte) (*UploadMomentsImageResponse, error)
	// UploadMomentsVideo 上传朋友圈视频（小文件）
	UploadMomentsVideo(videoData, thumbData []byte) (*UploadMomentsVideoResponse, error)
	// DownloadVideoCover 下载视频封面（小文件）
	DownloadVideoCover(fileID, fileAesKey string) ([]byte, error)
	// DownloadSnsVideo 下载朋友圈视频（小文件）
	DownloadSnsVideo(videoURL string, encKey uint64) ([]byte, error)
}

var Instance Ability
