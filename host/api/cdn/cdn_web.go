//go:build !lib

// Package webapi 提供 CDN 文件上传/下载服务的 web 实现（通过 HTTP 调用远程服务）。
package cdnapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"sync"

	"github.com/sbgayhub/golem/host/api/util"
)

// web CDN 服务 web 实现（通过 HTTP 调用远程服务）
type web struct{}

// Get 获取 CDNService 单例（web 模式）
var Get = sync.OnceValue(func() CDNService {
	return &web{}
})

// UploadImage CDN 上传聊天图片
func (w web) UploadImage(receiver string, reader io.Reader) (*UploadImageResult, error) {
	imageData, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	req := webUploadImageParam{
		Receiver:  receiver,
		ImageData: base64.StdEncoding.EncodeToString(imageData),
	}
	data, err := util.GetHttp().Post("/api/cdn/upload/image", &req)
	if err != nil {
		return nil, err
	}
	var resp UploadImageResult
	if err := util.ParseProtoResponse(data, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// UploadMomentsImage CDN 上传朋友圈图片
func (w web) UploadMomentsImage(imageData []byte) (*UploadSnsImageResult, error) {
	req := webUploadSnsImageParam{
		ImageData: base64.StdEncoding.EncodeToString(imageData),
	}
	data, err := util.GetHttp().Post("/api/cdn/upload/sns/image", &req)
	if err != nil {
		return nil, err
	}
	var resp UploadSnsImageResult
	if err := util.ParseProtoResponse(data, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// UploadVideo CDN 上传聊天视频
func (w web) UploadVideo(receiver string, thumb []byte, reader io.Reader, duration uint32) (*UploadVideoResult, error) {
	videoData, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	req := webUploadVideoParam{
		Receiver:  receiver,
		ThumbData: base64.StdEncoding.EncodeToString(thumb),
		VideoData: base64.StdEncoding.EncodeToString(videoData),
		Duration:  duration,
	}
	data, err := util.GetHttp().Post("/api/cdn/upload/video", &req)
	if err != nil {
		return nil, err
	}
	var resp UploadVideoResult
	if err := util.ParseProtoResponse(data, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// UploadMomentsVideo CDN 上传朋友圈视频
func (w web) UploadMomentsVideo(videoData, thumbData []byte) (*UploadSnsVideoResult, error) {
	req := webUploadSnsVideoParam{
		VideoData: base64.StdEncoding.EncodeToString(videoData),
		ThumbData: base64.StdEncoding.EncodeToString(thumbData),
	}
	data, err := util.GetHttp().Post("/web/upload/sns/video", &req)
	if err != nil {
		return nil, err
	}
	var resp UploadSnsVideoResult
	if err := util.ParseProtoResponse(data, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DownloadImage CDN 下载高清图片（返回 ReadCloser 流）
func (w web) DownloadImage(fileID, fileAesKey string) (io.ReadCloser, error) {
	req := webDownloadImageParam{
		FileID:  fileID,
		FileKey: fileAesKey,
	}
	raw, err := util.GetHttp().Post("/web/download/image", &req)
	if err != nil {
		return nil, err
	}
	decoded, err := decodeBase64Response(raw)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(decoded)), nil
}

// DownloadVideoCover CDN 下载视频封面
func (w web) DownloadVideoCover(fileID, fileAesKey string) ([]byte, error) {
	req := webDownloadVideoParam{
		FileID:  fileID,
		FileKey: fileAesKey,
	}
	raw, err := util.GetHttp().Post("/web/download/video/cover", &req)
	if err != nil {
		return nil, err
	}
	return decodeBase64Response(raw)
}

// DownloadVideo CDN 下载聊天视频（返回 ReadCloser 流）
func (w web) DownloadVideo(fileID, fileAesKey string) (io.ReadCloser, error) {
	req := webDownloadVideoParam{
		FileID:  fileID,
		FileKey: fileAesKey,
	}
	raw, err := util.GetHttp().Post("/web/download/video", &req)
	if err != nil {
		return nil, err
	}
	decoded, err := decodeBase64Response(raw)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(decoded)), nil
}

// DownloadSnsVideo CDN 下载朋友圈视频
func (w web) DownloadSnsVideo(videoURL string, encKey uint64) ([]byte, error) {
	req := webDownloadSnsVideoParam{
		VideoURL: videoURL,
		EncKey:   encKey,
	}
	raw, err := util.GetHttp().Post("/web/download/sns/video", &req)
	if err != nil {
		return nil, err
	}
	return decodeBase64Response(raw)
}

// decodeBase64Response 解析 JSON 中的 base64 字符串并解码为字节数组
// 服务端返回格式：{"code":0,"data":"base64encoded..."}
// util.Post 已提取 data 字段，此处为 JSON 字符串（带引号），需先 Unquote 再 base64 解码
func decodeBase64Response(raw []byte) ([]byte, error) {
	// raw 是 JSON 字符串，如 "SGVsbG8="，需要去掉引号
	var base64Str string
	if err := json.Unmarshal(raw, &base64Str); err != nil {
		// 如果不是 JSON 字符串，尝试直接 base64 解码
		return base64.StdEncoding.DecodeString(string(raw))
	}
	return base64.StdEncoding.DecodeString(base64Str)
}
