//go:build lib

// Package cdnapi 提供 CDN 文件上传/下载服务的 lib 实现（直接调用底层实现）。
package cdnapi

import (
	"bytes"
	"io"
	"sync"

	"golem/pkg/cdn"

	"github.com/sbgayhub/golem/host/api/util"
)

// lib CDN 服务 lib 实现（直接调用底层实现）
type lib struct{}

// Get 获取 CDNService 单例（lib 模式）
var Get = sync.OnceValue(func() CDNService {
	return &lib{}
})

// UploadImage CDN 上传聊天图片（读取流数据后调用底层实现）
func (l lib) UploadImage(receiver string, reader io.Reader, totalSize uint32) (*UploadImageResult, error) {
	imageData, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	resp, err := cdn.UploadImage(receiver, imageData)
	if resp == nil || err != nil {
		return nil, err
	}
	var result UploadImageResult
	if err := util.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UploadMomentsImage CDN 上传朋友圈图片
func (l lib) UploadMomentsImage(imageData []byte) (*UploadSnsImageResult, error) {
	resp, err := cdn.UploadSnsImage(imageData)
	if resp == nil || err != nil {
		return nil, err
	}
	var result UploadSnsImageResult
	if err := util.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UploadVideo CDN 上传聊天视频（读取流数据后调用底层实现）
func (l lib) UploadVideo(receiver string, videoReader io.Reader, videoSize uint32, thumbReader io.Reader, thumbSize uint32, duration uint32) (*UploadVideoResult, error) {
	videoData, err := io.ReadAll(videoReader)
	if err != nil {
		return nil, err
	}
	thumbData, err := io.ReadAll(thumbReader)
	if err != nil {
		return nil, err
	}
	resp, err := cdn.UploadVideo(receiver, videoData, thumbData, duration)
	if resp == nil || err != nil {
		return nil, err
	}
	var result UploadVideoResult
	if err := util.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UploadMomentsVideo CDN 上传朋友圈视频
func (l lib) UploadMomentsVideo(videoData, thumbData []byte) (*UploadSnsVideoResult, error) {
	resp, err := cdn.UploadSnsVideo(videoData, thumbData)
	if resp == nil || err != nil {
		return nil, err
	}
	var result UploadSnsVideoResult
	if err := util.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DownloadImage CDN 下载高清图片（返回 ReadCloser 流）
func (l lib) DownloadImage(fileID, fileAesKey string) (io.ReadCloser, error) {
	data, err := cdn.DownloadImage(fileID, fileAesKey)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// DownloadVideoCover CDN 下载视频封面
func (l lib) DownloadVideoCover(fileID, fileAesKey string) ([]byte, error) {
	return cdn.DownloadVideoCover(fileID, fileAesKey)
}

// DownloadVideo CDN 下载聊天视频（返回 ReadCloser 流）
func (l lib) DownloadVideo(fileID, fileAesKey string) (io.ReadCloser, error) {
	data, err := cdn.DownloadVideo(fileID, fileAesKey)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// DownloadSnsVideo CDN 下载朋友圈视频
func (l lib) DownloadSnsVideo(videoURL string, encKey uint64) ([]byte, error) {
	return cdn.DownloadSnsVideo(videoURL, encKey)
}
