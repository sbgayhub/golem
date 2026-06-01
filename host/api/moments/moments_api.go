// Package momentsapi 提供朋友圈服务的 API 接口定义。
package momentsapi

import (
	"io"

	baseapi "github.com/sbgayhub/golem/host/api/base"
)

// MomentsService 朋友圈服务 API 接口（返回 API proto 类型）。
type MomentsService interface {
	// Timeline 获取朋友圈时间线
	Timeline(firstPageMd5 string, maxId uint64) (*TimelineResponse, error)
	// UserPage 获取用户朋友圈主页
	UserPage(username, firstPageMd5 string, maxId uint64) (*UserPageResponse, error)
	// Detail 获取单条朋友圈详情
	Detail(id uint64) (*DetailResponse, error)
	// Comment 评论朋友圈
	Comment(id uint64, content string, typ, replyCommentId int32) (*CommentResponse, error)
	// Like 点赞朋友圈
	Like(id uint64) (*CommentResponse, error)
	// Unlike 取消点赞
	Unlike(id uint64) (*OperateResponse, error)
	// Delete 删除朋友圈
	Delete(id uint64) (*OperateResponse, error)
	// DeleteComment 删除朋友圈评论
	DeleteComment(id uint64, commentId uint32) (*OperateResponse, error)
	// Post 发布朋友圈
	Post(content string, blacklist, withUsers []string) (*PostResponse, error)
	// Upload 上传朋友圈媒体
	Upload(reader io.Reader) (*UploadResponse, error)
	// Sync 同步朋友圈数据
	Sync(syncKey []byte) (*SyncResponse, error)
	// SetPrivacy 设置朋友圈隐私
	SetPrivacy(function int32, value uint32) (*baseapi.OperateResponse, error)
}
