//go:build lib

// Package momentsapi 提供朋友圈服务的 lib 实现（直接调用底层实现）。
package momentsapi

import (
	"io"
	"sync"

	"golem/pkg/moments"

	"github.com/sbgayhub/golem/host/api"
	baseapi "github.com/sbgayhub/golem/host/api/base"
)

// lib 朋友圈服务 lib 实现（直接调用底层实现）。
type lib struct{}

// Get 获取 MomentsService 单例（lib 模式）。
var Get = sync.OnceValue(func() MomentsService {
	return &lib{}
})

// Timeline 获取朋友圈时间线
func (l lib) Timeline(firstPageMd5 string, maxId uint64) (*TimelineResponse, error) {
	resp, err := moments.Timeline(firstPageMd5, maxId)
	if resp == nil || err != nil {
		return nil, err
	}
	var result TimelineResponse
	if err := api.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UserPage 获取用户朋友圈主页
func (l lib) UserPage(username, firstPageMd5 string, maxId uint64) (*UserPageResponse, error) {
	resp, err := moments.UserPage(username, firstPageMd5, maxId)
	if resp == nil || err != nil {
		return nil, err
	}
	var result UserPageResponse
	if err := api.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Detail 获取单条朋友圈详情
func (l lib) Detail(id uint64) (*DetailResponse, error) {
	resp, err := moments.Detail(id)
	if resp == nil || err != nil {
		return nil, err
	}
	var result DetailResponse
	if err := api.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Comment 评论朋友圈
func (l lib) Comment(id uint64, content string, typ, replyCommentId int32) (*CommentResponse, error) {
	resp, err := moments.Comment(id, content, typ, replyCommentId)
	if resp == nil || err != nil {
		return nil, err
	}
	var result CommentResponse
	if err := api.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Like 点赞朋友圈
func (l lib) Like(id uint64) (*CommentResponse, error) {
	resp, err := moments.Like(id)
	if resp == nil || err != nil {
		return nil, err
	}
	var result CommentResponse
	if err := api.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Unlike 取消点赞
func (l lib) Unlike(id uint64) (*OperateResponse, error) {
	resp, err := moments.Unlike(id)
	if resp == nil || err != nil {
		return nil, err
	}
	var result OperateResponse
	if err := api.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Delete 删除朋友圈
func (l lib) Delete(id uint64) (*OperateResponse, error) {
	resp, err := moments.Delete(id)
	if resp == nil || err != nil {
		return nil, err
	}
	var result OperateResponse
	if err := api.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteComment 删除朋友圈评论
func (l lib) DeleteComment(id uint64, commentId uint32) (*OperateResponse, error) {
	resp, err := moments.DeleteComment(id, commentId)
	if resp == nil || err != nil {
		return nil, err
	}
	var result OperateResponse
	if err := api.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Post 发布朋友圈
func (l lib) Post(content string, blacklist, withUsers []string) (*PostResponse, error) {
	resp, err := moments.Post(content, blacklist, withUsers)
	if resp == nil || err != nil {
		return nil, err
	}
	var result PostResponse
	if err := api.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Upload 上传朋友圈媒体
func (l lib) Upload(reader io.Reader) (*UploadResponse, error) {
	resp, err := moments.Upload(reader)
	if resp == nil || err != nil {
		return nil, err
	}
	var result UploadResponse
	if err := api.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Sync 同步朋友圈数据
func (l lib) Sync(syncKey []byte) (*SyncResponse, error) {
	resp, err := moments.Sync(syncKey)
	if resp == nil || err != nil {
		return nil, err
	}
	var result SyncResponse
	if err := api.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SetPrivacy 设置朋友圈隐私
func (l lib) SetPrivacy(function int32, value uint32) (*baseapi.OperateResponse, error) {
	resp, err := moments.SetPrivacy(function, value)
	if resp == nil || err != nil {
		return nil, err
	}
	var result baseapi.OperateResponse
	if err := api.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
