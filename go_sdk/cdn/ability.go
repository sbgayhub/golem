package cdn

import (
	"io"
)

type Ability interface {
	// GetDns 获取 CDN DNS 信息
	GetDns() (*DnsResponse, error)

	// 流式上传（大文件）
	// UploadImage 客户端流式上传聊天图片
	UploadImage(receiver string, reader io.Reader, totalSize uint32) (*UploadImageResult, error)
	// UploadVideo 客户端流式上传聊天视频
	UploadVideo(receiver string, videoReader io.Reader, videoSize uint32, thumbReader io.Reader, thumbSize uint32, duration uint32) (*UploadVideoResult, error)

	// 流式下载（大文件）
	// DownloadImage 服务端流式下载高清图片
	DownloadImage(fileID, fileAesKey string) (io.ReadCloser, error)
	// DownloadVideo 服务端流式下载聊天视频
	DownloadVideo(fileID, fileAesKey string) (io.ReadCloser, error)

	// 非流式接口（小文件）
	// UploadMomentsImage 上传朋友圈图片（小文件）
	UploadMomentsImage(imageData []byte) (*UploadSnsImageResult, error)
	// UploadMomentsVideo 上传朋友圈视频（小文件）
	UploadMomentsVideo(videoData, thumbData []byte) (*UploadSnsVideoResult, error)
	// DownloadVideoCover 下载视频封面（小文件）
	DownloadVideoCover(fileID, fileAesKey string) ([]byte, error)
	// DownloadSnsVideo 下载朋友圈视频（小文件）
	DownloadSnsVideo(videoURL string, encKey uint64) ([]byte, error)
}

var Instance Ability
