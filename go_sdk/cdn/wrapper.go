package cdn

import (
	"bytes"
	"context"
	"errors"
	"io"
)

// Client 实现 Ability 接口，通过 gRPC 调用远程 CDN 服务
type Client struct {
	Client CDNServiceClient
}

// GetDns 获取 CDN DNS 信息
func (c Client) GetDns() (*DnsResponse, error) {
	resp, err := c.Client.GetDns(context.Background(), &GetDnsRequest{})
	if err != nil {
		return nil, err
	}
	return resp.Result, nil
}

// UploadImage 客户端流式上传聊天图片
func (c Client) UploadImage(receiver string, reader io.Reader, totalSize uint32) (*UploadImageResult, error) {
	stream, err := c.Client.UploadImage(context.Background())
	if err != nil {
		return nil, err
	}

	// 发送元数据
	if err := stream.Send(&UploadImageChunk{
		Chunk: &UploadImageChunk_Metadata{
			Metadata: &UploadImageMetadata{
				Receiver:  receiver,
				TotalSize: totalSize,
			},
		},
	}); err != nil {
		return nil, err
	}

	// 发送数据块
	buf := make([]byte, 32*1024) // 32KB chunks
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			if sendErr := stream.Send(&UploadImageChunk{
				Chunk: &UploadImageChunk_Data{
					Data: buf[:n],
				},
			}); sendErr != nil {
				return nil, sendErr
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}

	resp, err := stream.CloseAndRecv()
	if err != nil {
		return nil, err
	}
	return resp.Result, nil
}

// UploadVideo 客户端流式上传聊天视频
func (c Client) UploadVideo(receiver string, videoReader io.Reader, videoSize uint32, thumbReader io.Reader, thumbSize uint32, duration uint32) (*UploadVideoResult, error) {
	stream, err := c.Client.UploadVideo(context.Background())
	if err != nil {
		return nil, err
	}

	// 发送元数据
	if err := stream.Send(&UploadVideoChunk{
		Chunk: &UploadVideoChunk_Metadata{
			Metadata: &UploadVideoMetadata{
				Receiver:       receiver,
				VideoTotalSize: videoSize,
				ThumbTotalSize: thumbSize,
				Duration:       duration,
			},
		},
	}); err != nil {
		return nil, err
	}

	// 发送视频数据
	if err := sendData(stream.Send, videoReader); err != nil {
		return nil, err
	}

	// 发送缩略图数据
	if err := sendData(stream.Send, thumbReader); err != nil {
		return nil, err
	}

	resp, err := stream.CloseAndRecv()
	if err != nil {
		return nil, err
	}
	return resp.Result, nil
}

func sendData(send func(*UploadVideoChunk) error, reader io.Reader) error {
	buf := make([]byte, 32*1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			if sendErr := send(&UploadVideoChunk{
				Chunk: &UploadVideoChunk_Data{
					Data: buf[:n],
				},
			}); sendErr != nil {
				return sendErr
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// DownloadImage 服务端流式下载高清图片
func (c Client) DownloadImage(fileID, fileAesKey string) (io.ReadCloser, error) {
	stream, err := c.Client.DownloadImage(context.Background(), &DownloadImageRequest{
		FileId: fileID,
		AesKey: fileAesKey,
	})
	if err != nil {
		return nil, err
	}

	// 接收数据并写入 buffer
	var buf bytes.Buffer
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		buf.Write(chunk.Data)
	}

	return io.NopCloser(&buf), nil
}

// DownloadVideo 服务端流式下载聊天视频
func (c Client) DownloadVideo(fileID, fileAesKey string) (io.ReadCloser, error) {
	stream, err := c.Client.DownloadVideo(context.Background(), &DownloadVideoRequest{
		FileId: fileID,
		AesKey: fileAesKey,
	})
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		buf.Write(chunk.Data)
	}

	return io.NopCloser(&buf), nil
}

// UploadMomentsImage 上传朋友圈图片（小文件，非流式）
func (c Client) UploadMomentsImage(imageData []byte) (*UploadSnsImageResult, error) {
	resp, err := c.Client.UploadMomentsImage(context.Background(), &UploadMomentsImageRequest{
		Data: imageData,
	})
	if err != nil {
		return nil, err
	}
	return resp.Result, nil
}

// UploadMomentsVideo 上传朋友圈视频（小文件，非流式）
func (c Client) UploadMomentsVideo(videoData, thumbData []byte) (*UploadSnsVideoResult, error) {
	resp, err := c.Client.UploadMomentsVideo(context.Background(), &UploadMomentsVideoRequest{
		VideoData: videoData,
		ThumbData: thumbData,
	})
	if err != nil {
		return nil, err
	}
	return resp.Result, nil
}

// DownloadVideoCover 下载视频封面（小文件，非流式）
func (c Client) DownloadVideoCover(fileID, fileAesKey string) ([]byte, error) {
	resp, err := c.Client.DownloadVideoCover(context.Background(), &DownloadVideoCoverRequest{
		FileId: fileID,
		AesKey: fileAesKey,
	})
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// DownloadSnsVideo 下载朋友圈视频（小文件，非流式）
func (c Client) DownloadSnsVideo(videoURL string, encKey uint64) ([]byte, error) {
	resp, err := c.Client.DownloadSnsVideo(context.Background(), &DownloadSnsVideoRequest{
		VideoUrl: videoURL,
		EncKey:   encKey,
	})
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// Server 实现 CDNServiceServer 接口，将 gRPC 请求委托给 Ability 实现
type Server struct {
	UnimplementedCDNServiceServer
	Impl Ability
}

// GetDns 获取 CDN DNS 信息
func (s Server) GetDns(ctx context.Context, request *GetDnsRequest) (*GetDnsResponse, error) {
	result, err := s.Impl.GetDns()
	if err != nil {
		return nil, err
	}
	return &GetDnsResponse{Result: result}, nil
}

// UploadImage 客户端流式上传聊天图片
func (s Server) UploadImage(stream CDNService_UploadImageServer) error {
	// 接收元数据
	chunk, err := stream.Recv()
	if err != nil {
		return err
	}
	metadata := chunk.GetMetadata()
	if metadata == nil {
		return errors.New("first chunk must be metadata")
	}

	// 接收数据
	var buf bytes.Buffer
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		buf.Write(chunk.GetData())
	}

	// 调用 Ability 实现
	result, err := s.Impl.UploadImage(metadata.Receiver, &buf, metadata.TotalSize)
	if err != nil {
		return err
	}

	return stream.SendAndClose(&UploadImageResponse{Result: result})
}

// UploadVideo 客户端流式上传聊天视频
func (s Server) UploadVideo(stream CDNService_UploadVideoServer) error {
	// 接收元数据
	chunk, err := stream.Recv()
	if err != nil {
		return err
	}
	metadata := chunk.GetMetadata()
	if metadata == nil {
		return errors.New("first chunk must be metadata")
	}

	// 接收视频和缩略图数据
	var videoBuf, thumbBuf bytes.Buffer
	var currentWriter *bytes.Buffer = &videoBuf
	var videoReceived uint32

	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		data := chunk.GetData()
		if data == nil {
			continue
		}

		// 检查是否需要切换到缩略图
		if currentWriter == &videoBuf {
			videoReceived += uint32(len(data))
			if videoReceived >= metadata.VideoTotalSize {
				// 写入剩余的视频数据
				remaining := videoReceived - metadata.VideoTotalSize
				if remaining > 0 {
					videoBuf.Write(data[:len(data)-int(remaining)])
					thumbBuf.Write(data[len(data)-int(remaining):])
				} else {
					videoBuf.Write(data)
				}
				currentWriter = &thumbBuf
				continue
			}
		}

		currentWriter.Write(data)
	}

	// 调用 Ability 实现
	result, err := s.Impl.UploadVideo(
		metadata.Receiver,
		&videoBuf, metadata.VideoTotalSize,
		&thumbBuf, metadata.ThumbTotalSize,
		metadata.Duration,
	)
	if err != nil {
		return err
	}

	return stream.SendAndClose(&UploadVideoResponse{Result: result})
}

// DownloadImage 服务端流式下载高清图片
func (s Server) DownloadImage(request *DownloadImageRequest, stream CDNService_DownloadImageServer) error {
	reader, err := s.Impl.DownloadImage(request.FileId, request.AesKey)
	if err != nil {
		return err
	}
	defer reader.Close()

	buf := make([]byte, 32*1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			if sendErr := stream.Send(&DownloadImageChunk{Data: buf[:n]}); sendErr != nil {
				return sendErr
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	return nil
}

// DownloadVideo 服务端流式下载聊天视频
func (s Server) DownloadVideo(request *DownloadVideoRequest, stream CDNService_DownloadVideoServer) error {
	reader, err := s.Impl.DownloadVideo(request.FileId, request.AesKey)
	if err != nil {
		return err
	}
	defer reader.Close()

	buf := make([]byte, 32*1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			if sendErr := stream.Send(&DownloadVideoChunk{Data: buf[:n]}); sendErr != nil {
				return sendErr
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	return nil
}

// UploadMomentsImage 上传朋友圈图片（小文件，非流式）
func (s Server) UploadMomentsImage(ctx context.Context, request *UploadMomentsImageRequest) (*UploadMomentsImageResponse, error) {
	result, err := s.Impl.UploadMomentsImage(request.Data)
	if err != nil {
		return nil, err
	}
	return &UploadMomentsImageResponse{Result: result}, nil
}

// UploadMomentsVideo 上传朋友圈视频（小文件，非流式）
func (s Server) UploadMomentsVideo(ctx context.Context, request *UploadMomentsVideoRequest) (*UploadMomentsVideoResponse, error) {
	result, err := s.Impl.UploadMomentsVideo(request.VideoData, request.ThumbData)
	if err != nil {
		return nil, err
	}
	return &UploadMomentsVideoResponse{Result: result}, nil
}

// DownloadVideoCover 下载视频封面（小文件，非流式）
func (s Server) DownloadVideoCover(ctx context.Context, request *DownloadVideoCoverRequest) (*DownloadVideoCoverResponse, error) {
	data, err := s.Impl.DownloadVideoCover(request.FileId, request.AesKey)
	if err != nil {
		return nil, err
	}
	return &DownloadVideoCoverResponse{Data: data}, nil
}

// DownloadSnsVideo 下载朋友圈视频（小文件，非流式）
func (s Server) DownloadSnsVideo(ctx context.Context, request *DownloadSnsVideoRequest) (*DownloadSnsVideoResponse, error) {
	data, err := s.Impl.DownloadSnsVideo(request.VideoUrl, request.EncKey)
	if err != nil {
		return nil, err
	}
	return &DownloadSnsVideoResponse{Data: data}, nil
}
