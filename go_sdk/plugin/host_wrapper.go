package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"
)

// HostServiceImpl 宿主侧 HostService 实现，由宿主在初始化时设置
var HostServiceImpl HostServiceServer

// --- SessionClient 会话能力 gRPC 客户端包装（注入到插件） ---

type SessionClient struct {
	Client HostServiceClient
}

func (c SessionClient) Hold(p Plugin, id string, duration time.Duration) {
	if _, err := c.Client.SessionHold(context.Background(), &SessionHold_Request{
		PluginId: p.GetMetadata().Name,
		Sender:   id,
		Duration: uint32(duration.Seconds()),
	}); err != nil {
		slog.Error("[session] 劫持会话失败", "plugin", p.GetMetadata().Name, "id", id, "err", err)
	}
}

func (c SessionClient) Release(id string) {
	if _, err := c.Client.SessionRelease(context.Background(), &SessionRelease_Request{
		Sender: id,
	}); err != nil {
		slog.Error("[session] 释放会话失败", "id", id, "err", err)
	}
}

// --- CallerClient 插件调用能力 gRPC 客户端包装（注入到插件） ---

type CallerClient struct {
	Client HostServiceClient
}

func (c CallerClient) CallPlugin(capability string, args map[string]string) (string, []byte, error) {
	data, err := json.Marshal(args)
	if err != nil {
		return "", nil, err
	}
	resp, err := c.Client.CallPlugin(context.Background(), &CallPlugin_Request{
		Capability: capability,
		Args:       data,
	})
	if err != nil {
		return "", nil, err
	}
	if resp != nil && resp.Message != "" {
		return "", nil, errors.New(resp.Message)
	}
	return resp.GetMime(), resp.GetData(), nil
}
