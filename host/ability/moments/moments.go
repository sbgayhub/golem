// Package momentsability 提供朋友圈能力的实现（直连型）。
package momentsability

import (
	"bytes"

	sdk "github.com/sbgayhub/golem/sdk/moments"

	baseapi "github.com/sbgayhub/golem/host/api/base"
	messageapi "github.com/sbgayhub/golem/host/api/message"
	momentsapi "github.com/sbgayhub/golem/host/api/moments"
)

// ability 朋友圈能力实现（直连型）。
type ability struct {
	api momentsapi.MomentsService
}

func init() {
	sdk.Instance = &ability{api: momentsapi.Get()}
}

// Timeline 获取朋友圈时间线
func (a ability) Timeline(firstPageMd5 string, maxId uint64) (*sdk.Timeline_Response, error) {
	resp, err := a.api.Timeline(firstPageMd5, maxId)
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.Timeline_Response{
		BaseResult:            mapBaseResult(resp.GetBaseResponse()),
		FirstPageMd5:          resp.GetFirstPageMd5(),
		ObjectCount:           resp.GetObjectCount(),
		ObjectList:            mapMoments(resp.GetObjectList()),
		NewRequestTime:        resp.GetNewRequestTime(),
		ObjectCountForSameMd5: resp.GetObjectCountForSameMd5(),
		ControlFlag:           resp.GetControlFlag(),
		ServerConfig:          mapServerConfig(resp.GetServerConfig()),
		UnreadCount:           resp.GetUnreadCount(),
		UnreadList:            resp.GetUnreadList(),
		Session:               mapBuffer(resp.GetSession()),
	}, nil
}

// UserPage 获取用户朋友圈主页
func (a ability) UserPage(username, firstPageMd5 string, maxId uint64) (*sdk.UserPage_Response, error) {
	resp, err := a.api.UserPage(username, firstPageMd5, maxId)
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.UserPage_Response{
		BaseResult:            mapBaseResult(resp.GetBaseResponse()),
		FirstPageMd5:          resp.GetFirstPageMd5(),
		ObjectCount:           resp.GetObjectCount(),
		ObjectList:            mapMoments(resp.GetObjectList()),
		ObjectTotalCount:      resp.GetObjectTotalCount(),
		NewRequestTime:        resp.GetNewRequestTime(),
		ObjectCountForSameMd5: resp.GetObjectCountForSameMd5(),
		ServerConfig:          mapServerConfig(resp.GetServerConfig()),
		LimitedId:             resp.GetLimitedId(),
		ContinueId:            resp.GetContinueId(),
		ReturnTips:            resp.GetReturnTips(),
	}, nil
}

// Detail 获取单条朋友圈详情
func (a ability) Detail(id uint64) (*sdk.Detail_Response, error) {
	resp, err := a.api.Detail(id)
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.Detail_Response{
		BaseResult: mapBaseResult(resp.GetBaseResponse()),
		Object:     mapMoment(resp.GetObject()),
	}, nil
}

// Comment 评论朋友圈
func (a ability) Comment(id uint64, content string, typ, replyCommentId int32) (*sdk.Comment_Response, error) {
	resp, err := a.api.Comment(id, content, typ, replyCommentId)
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.Comment_Response{
		BaseResult: mapBaseResult(resp.GetBaseResponse()),
		Object:     mapMoment(resp.GetObject()),
	}, nil
}

// Like 点赞朋友圈
func (a ability) Like(id uint64) (*sdk.Like_Response, error) {
	resp, err := a.api.Like(id)
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.Like_Response{
		BaseResult: mapBaseResult(resp.GetBaseResponse()),
		Object:     mapMoment(resp.GetObject()),
	}, nil
}

// Unlike 取消点赞
func (a ability) Unlike(id uint64) (*sdk.Unlike_Response, error) {
	resp, err := a.api.Unlike(id)
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.Unlike_Response{
		BaseResult: mapBaseResult(resp.GetBaseResponse()),
		Count:      resp.GetCount(),
		List:       resp.GetList(),
	}, nil
}

// Delete 删除朋友圈
func (a ability) Delete(id uint64) (*sdk.Delete_Response, error) {
	resp, err := a.api.Delete(id)
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.Delete_Response{
		BaseResult: mapBaseResult(resp.GetBaseResponse()),
		Count:      resp.GetCount(),
		List:       resp.GetList(),
	}, nil
}

// DeleteComment 删除朋友圈评论
func (a ability) DeleteComment(id uint64, commentId uint32) (*sdk.DeleteComment_Response, error) {
	resp, err := a.api.DeleteComment(id, commentId)
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.DeleteComment_Response{
		BaseResult: mapBaseResult(resp.GetBaseResponse()),
		Count:      resp.GetCount(),
		List:       resp.GetList(),
	}, nil
}

// Post 发布朋友圈
func (a ability) Post(content string, blacklist, withUsers []string) (*sdk.Post_Response, error) {
	resp, err := a.api.Post(content, blacklist, withUsers)
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.Post_Response{
		BaseResult: mapBaseResult(resp.GetBaseResponse()),
		Object:     mapMoment(resp.GetObject()),
		SpamTips:   resp.GetSpamTips(),
	}, nil
}

// Upload 上传朋友圈媒体
func (a ability) Upload(data []byte) (*sdk.Upload_Response, error) {
	resp, err := a.api.Upload(bytes.NewReader(data))
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.Upload_Response{
		BaseResult:    mapBaseResult(resp.GetBaseResponse()),
		Offset:        resp.GetOffset(),
		Size:          resp.GetSize(),
		ClientId:      resp.GetClientId(),
		BufferUrl:     mapBufferURL(resp.GetBufferUrl()),
		ThumbUrlCount: resp.GetThumbUrlCount(),
		ThumbUrlList:  mapBufferURLs(resp.GetThumbUrlList()),
		Id:            resp.GetId(),
		Type:          resp.GetType(),
	}, nil
}

// Sync 同步朋友圈数据
func (a ability) Sync(key []byte) (*sdk.Sync_Response, error) {
	resp, err := a.api.Sync(key)
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.Sync_Response{
		BaseResult:   mapBaseResult(resp.GetBaseResponse()),
		Commands:     mapCommands(resp.GetCommands()),
		ContinueFlag: resp.GetContinueFlag(),
		Key:          mapBuffer(resp.GetKey()),
	}, nil
}

// SetPrivacy 设置朋友圈隐私
func (a ability) SetPrivacy(function int32, value uint32) (*sdk.SetPrivacy_Response, error) {
	resp, err := a.api.SetPrivacy(function, value)
	if resp == nil || err != nil {
		return nil, err
	}
	result := resp.GetResult()
	return &sdk.SetPrivacy_Response{
		Code:       resp.GetCode(),
		Count:      result.GetCount(),
		ResultCode: result.GetCode(),
		Message:    result.GetMessage(),
	}, nil
}

func mapBaseResult(resp *baseapi.BaseResponse) *sdk.BaseResult {
	if resp == nil {
		return nil
	}
	return &sdk.BaseResult{
		Code:    resp.GetCode(),
		Message: resp.GetMessage().GetValue(),
	}
}

func mapBuffer(buffer *baseapi.Buffer) *sdk.Buffer {
	if buffer == nil {
		return nil
	}
	return &sdk.Buffer{
		Size: buffer.GetSize(),
		Data: buffer.GetData(),
	}
}

func mapServerConfig(config *momentsapi.ServerConfig) *sdk.ServerConfig {
	if config == nil {
		return nil
	}
	return &sdk.ServerConfig{
		PostMentionLimit:      config.GetPostMentionLimit(),
		CopyAndPasteWordLimit: config.GetCopyAndPasteWordLimit(),
	}
}

func mapBufferURL(url *momentsapi.BufferUrl) *sdk.BufferUrl {
	if url == nil {
		return nil
	}
	return &sdk.BufferUrl{
		Url:  url.GetUrl(),
		Type: url.GetType(),
		Size: url.GetSize(),
		Md5:  url.GetMd5(),
	}
}

func mapBufferURLs(urls []*momentsapi.BufferUrl) []*sdk.BufferUrl {
	result := make([]*sdk.BufferUrl, 0, len(urls))
	for _, url := range urls {
		if url == nil {
			continue
		}
		result = append(result, mapBufferURL(url))
	}
	return result
}

func mapMoment(object *messageapi.MomentsObject) *sdk.Moment {
	if object == nil {
		return nil
	}
	return &sdk.Moment{
		Id:                object.GetId(),
		Username:          object.GetUsername(),
		Nickname:          object.GetNickname(),
		CreateTime:        object.GetCreateTime(),
		ObjectDescription: object.GetObjectDescription().GetValue(),
		LikeFlag:          object.GetLikeFlag(),
		LikeCount:         object.GetLikeCount(),
		LikeUserList:      mapComments(object.GetLikeUserList()),
		CommentCount:      object.GetCommentCount(),
		CommentUserList:   mapComments(object.GetCommentUserList()),
		WithCount:         object.GetWithCount(),
		WithUserList:      mapComments(object.GetWithUserList()),
		ReferUsername:     object.GetReferUsername(),
		ReferId:           object.GetReferId(),
		DeleteFlag:        object.GetDeleteFlag(),
	}
}

func mapMoments(objects []*messageapi.MomentsObject) []*sdk.Moment {
	result := make([]*sdk.Moment, 0, len(objects))
	for _, object := range objects {
		if object == nil {
			continue
		}
		result = append(result, mapMoment(object))
	}
	return result
}

func mapComment(comment *messageapi.MomentsObject_CommentInfo) *sdk.CommentInfo {
	if comment == nil {
		return nil
	}
	return &sdk.CommentInfo{
		Username:        comment.GetUsername(),
		Nickname:        comment.GetNickname(),
		Source:          comment.GetSource(),
		Type:            comment.GetType(),
		Content:         comment.GetContent(),
		CreateTime:      comment.GetCreateTime(),
		CommentId:       comment.GetCommentId(),
		ReplyCommentId:  comment.GetReplyCommentId(),
		ReplyUsername:   comment.GetReplyUsername(),
		IsNotRichText:   comment.GetIsNotRichText(),
		ReplyCommentId2: comment.GetReplyCommentId2(),
		CommentId2:      comment.GetCommentId2(),
		DeleteFlag:      comment.GetDeleteFlag(),
		CommentFlag:     comment.GetCommentFlag(),
	}
}

func mapComments(comments []*messageapi.MomentsObject_CommentInfo) []*sdk.CommentInfo {
	result := make([]*sdk.CommentInfo, 0, len(comments))
	for _, comment := range comments {
		if comment == nil {
			continue
		}
		result = append(result, mapComment(comment))
	}
	return result
}

func mapCommands(commands *baseapi.Commands) []*sdk.Command {
	if commands == nil {
		return nil
	}
	result := make([]*sdk.Command, 0, len(commands.GetList()))
	for _, command := range commands.GetList() {
		if command == nil {
			continue
		}
		result = append(result, &sdk.Command{
			Id:     command.GetId(),
			Buffer: mapBuffer(command.GetBuffer()),
		})
	}
	return result
}
