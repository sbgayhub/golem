package message

import (
	"bytes"
	"context"
	"io"

	"google.golang.org/grpc"
)

// Client 实现 Ability 接口，通过 gRPC 调用远程消息服务
type Client struct {
	Client MessageServiceClient
}

var _ Ability = (*Client)(nil)

// Send 发送消息（client-stream：首包消息元数据 + 后续二进制数据块）
func (c Client) Send(msg *Message) (*Send_Response, error) {
	stream, err := c.Client.Send(context.Background())
	if err != nil {
		return nil, err
	}

	// 发送首包：消息元数据
	if err := stream.Send(&Send_Request{Message: msg}); err != nil {
		return nil, err
	}

	// TODO: 如果 Message 中携带了二进制数据，需要通过 stream 分块发送

	return stream.CloseAndRecv()
}

// Forward 转发消息
func (c Client) Forward(msg *Message, receiver string) (*Forward_Response, error) {
	return c.Client.Forward(context.Background(), &Forward_Request{
		Receiver: receiver,
		Message:  msg,
	})
}

// Revoke 撤回消息
func (c Client) Revoke(receiver string, newMsgId uint64) (*Revoke_Response, error) {
	return c.Client.Revoke(context.Background(), &Revoke_Request{
		Receiver: receiver,
		NewMsgId: newMsgId,
	})
}

// Download 下载媒体资源（server-stream）
func (c Client) Download(msg *Message) (io.ReadCloser, error) {
	stream, err := c.Client.Download(context.Background(), &Download_Request{Message: msg})
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		buf.Write(resp.Data)
	}

	return io.NopCloser(&buf), nil
}

// Server 实现 MessageServiceServer 接口，将 gRPC 请求委托给 Ability 实现
type Server struct {
	UnimplementedMessageServiceServer
	Impl Ability
}

// Send 发送消息（client-stream）
func (s Server) Send(stream grpc.ClientStreamingServer[Send_Request, Send_Response]) error {
	// 接收首包：消息元数据
	req, err := stream.Recv()
	if err != nil {
		return err
	}

	// 消耗后续数据包（如有）
	for {
		_, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	// 调用 Ability 实现
	resp, err := s.Impl.Send(req.Message)
	if err != nil {
		return err
	}

	return stream.SendAndClose(resp)
}

// Forward 转发消息
func (s Server) Forward(ctx context.Context, request *Forward_Request) (*Forward_Response, error) {
	return s.Impl.Forward(request.Message, request.Receiver)
}

// Revoke 撤回消息
func (s Server) Revoke(ctx context.Context, request *Revoke_Request) (*Revoke_Response, error) {
	return s.Impl.Revoke(request.Receiver, request.NewMsgId)
}

// Download 下载媒体资源（server-stream）
func (s Server) Download(request *Download_Request, stream grpc.ServerStreamingServer[Download_Response]) error {
	reader, err := s.Impl.Download(request.Message)
	if err != nil {
		return err
	}
	defer reader.Close()

	buf := make([]byte, 3*1024*1024) // 3MB chunks
	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			if sendErr := stream.Send(&Download_Response{Data: buf[:n]}); sendErr != nil {
				return sendErr
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	return nil
}
