// Package cdnability 提供 CDN 能力的实现。
package cdnability

import (
	"io"

	sdk "github.com/sbgayhub/golem/sdk/cdn"

	api "github.com/sbgayhub/golem/host/api/cdn"
	"github.com/sbgayhub/golem/host/api/util"
)

// ability CDN 能力实现
type ability struct {
	api api.CDNService
}

func init() {
	sdk.Instance = &ability{api: api.Get()}
}

// UploadImage CDN 上传聊天图片
func (a ability) UploadImage(receiver string, reader io.Reader) (*sdk.UploadImageResponse, error) {
	resp, err := a.api.UploadImage(receiver, reader)
	if resp == nil || err != nil {
		return nil, err
	}
	var result sdk.UploadImageResponse
	if err := util.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UploadMomentsImage CDN 上传朋友圈图片
func (a ability) UploadMomentsImage(imageData []byte) (*sdk.UploadMomentsImageResponse, error) {
	resp, err := a.api.UploadMomentsImage(imageData)
	if resp == nil || err != nil {
		return nil, err
	}
	var result sdk.UploadMomentsImageResponse
	if err := util.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UploadVideo CDN 上传聊天视频
func (a ability) UploadVideo(receiver string, thumb []byte, reader io.Reader, duration uint32) (*sdk.UploadVideoResponse, error) {
	resp, err := a.api.UploadVideo(receiver, thumb, reader, duration)
	if resp == nil || err != nil {
		return nil, err
	}
	var result sdk.UploadVideoResponse
	if err := util.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UploadMomentsVideo CDN 上传朋友圈视频
func (a ability) UploadMomentsVideo(videoData, thumbData []byte) (*sdk.UploadMomentsVideoResponse, error) {
	resp, err := a.api.UploadMomentsVideo(videoData, thumbData)
	if resp == nil || err != nil {
		return nil, err
	}
	var result sdk.UploadMomentsVideoResponse
	if err := util.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DownloadImage CDN 下载高清图片
func (a ability) DownloadImage(fileID, fileAesKey string) (io.ReadCloser, error) {
	return a.api.DownloadImage(fileID, fileAesKey)
}

// DownloadVideoCover CDN 下载视频封面
func (a ability) DownloadVideoCover(fileID, fileAesKey string) ([]byte, error) {
	return a.api.DownloadVideoCover(fileID, fileAesKey)
}

// DownloadVideo CDN 下载聊天视频
func (a ability) DownloadVideo(fileID, fileAesKey string) (io.ReadCloser, error) {
	return a.api.DownloadVideo(fileID, fileAesKey)
}

// DownloadSnsVideo CDN 下载朋友圈视频
func (a ability) DownloadSnsVideo(videoURL string, encKey uint64) ([]byte, error) {
	return a.api.DownloadSnsVideo(videoURL, encKey)
}
