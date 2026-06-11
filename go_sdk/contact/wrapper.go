package contact

import (
	"context"
	"errors"
)

// Client 实现 Ability 接口，通过 gRPC 调用远程联系人服务
type Client struct {
	Client ContactServiceClient
}

var _ Ability = (*Client)(nil)

// Get 按键查询缓存联系人
func (c Client) Get(key string) *Contact {
	resp, err := c.Client.Get(context.Background(), &Get_Request{Key: key})
	if err != nil || resp.Contact == nil {
		return nil
	}
	return resp.Contact
}

// List 获取联系人列表
func (c Client) List() []*Contact {
	resp, err := c.Client.List(context.Background(), &List_Request{})
	if err != nil {
		return nil
	}
	return resp.Contacts
}

// GetSelf 获取当前登录账号信息
func (c Client) GetSelf() *SelfInfo {
	resp, err := c.Client.GetSelf(context.Background(), &GetSelf_Request{})
	if err != nil || resp.Self == nil {
		return nil
	}
	return resp.Self
}

// SetRemark 设置联系人备注
func (c Client) SetRemark(username, remark string) error {
	if _, err := c.Client.SetRemark(context.Background(), &SetRemark_Request{
		Username: username,
		Remark:   remark,
	}); err != nil {
		return err
	}
	return nil
}

// AddFriend 发送好友申请
func (c Client) AddFriend(v1, v2, content string, operate, scene int) error {
	if _, err := c.Client.AddFriend(context.Background(), &AddFriend_Request{
		V1:      v1,
		V2:      v2,
		Content: content,
		Operate: int32(operate),
		Scene:   int32(scene),
	}); err != nil {
		return err
	}
	return nil
}

// VerifyFriend 通过好友验证
func (c Client) VerifyFriend(v1, v2 string, scene int) error {
	if _, err := c.Client.VerifyFriend(context.Background(), &VerifyFriend_Request{
		V1:    v1,
		V2:    v2,
		Scene: int32(scene),
	}); err != nil {
		return err
	}
	return nil
}

// Delete 删除联系人
func (c Client) Delete(username string) error {
	if _, err := c.Client.Delete(context.Background(), &Delete_Request{Username: username}); err != nil {
		return err
	}
	return nil
}

// BlacklistAdd 添加到黑名单
func (c Client) BlacklistAdd(username string) error {
	if _, err := c.Client.BlacklistAdd(context.Background(), &BlacklistAdd_Request{Username: username}); err != nil {
		return err
	}
	return nil
}

// BlacklistRemove 从黑名单移除
func (c Client) BlacklistRemove(username string) error {
	if _, err := c.Client.BlacklistRemove(context.Background(), &BlacklistRemove_Request{Username: username}); err != nil {
		return err
	}
	return nil
}

// Search 搜索联系人
func (c Client) Search(keyword string, fromScene, searchScene uint32) *Contact {
	resp, err := c.Client.Search(context.Background(), &Search_Request{
		Keyword:     keyword,
		FromScene:   fromScene,
		SearchScene: searchScene,
	})
	if err != nil {
		return nil
	}
	return resp.Contacts
}

// Server 实现 ContactServiceServer 接口，将 gRPC 请求委托给 Ability 实现
type Server struct {
	UnimplementedContactServiceServer
	Impl Ability
}

// Get 按键查询缓存联系人
func (s Server) Get(ctx context.Context, request *Get_Request) (*Get_Response, error) {
	contact := s.Impl.Get(request.Key)
	return &Get_Response{Contact: contact}, nil
}

// List 获取联系人列表
func (s Server) List(ctx context.Context, request *List_Request) (*List_Response, error) {
	contacts := s.Impl.List()
	if contacts == nil {
		return nil, errors.New("not found")
	}
	return &List_Response{Contacts: contacts}, nil
}

// GetSelf 获取当前登录账号信息
func (s Server) GetSelf(ctx context.Context, request *GetSelf_Request) (*GetSelf_Response, error) {
	self := s.Impl.GetSelf()
	return &GetSelf_Response{Self: self}, nil
}

// SetRemark 设置联系人备注
func (s Server) SetRemark(ctx context.Context, request *SetRemark_Request) (*SetRemark_Response, error) {
	if err := s.Impl.SetRemark(request.Username, request.Remark); err != nil {
		return nil, err
	}
	return &SetRemark_Response{}, nil
}

// AddFriend 发送好友申请
func (s Server) AddFriend(ctx context.Context, request *AddFriend_Request) (*AddFriend_Response, error) {
	if err := s.Impl.AddFriend(request.V1, request.V2, request.Content, int(request.Operate), int(request.Scene)); err != nil {
		return nil, err
	}
	return &AddFriend_Response{}, nil
}

// VerifyFriend 通过好友验证
func (s Server) VerifyFriend(ctx context.Context, request *VerifyFriend_Request) (*VerifyFriend_Response, error) {
	if err := s.Impl.VerifyFriend(request.V1, request.V2, int(request.Scene)); err != nil {
		return nil, err
	}
	return &VerifyFriend_Response{}, nil
}

// Delete 删除联系人
func (s Server) Delete(ctx context.Context, request *Delete_Request) (*Delete_Response, error) {
	if err := s.Impl.Delete(request.Username); err != nil {
		return nil, err
	}
	return &Delete_Response{}, nil
}

// BlacklistAdd 添加到黑名单
func (s Server) BlacklistAdd(ctx context.Context, request *BlacklistAdd_Request) (*BlacklistAdd_Response, error) {
	if err := s.Impl.BlacklistAdd(request.Username); err != nil {
		return nil, err
	}
	return &BlacklistAdd_Response{}, nil
}

// BlacklistRemove 从黑名单移除
func (s Server) BlacklistRemove(ctx context.Context, request *BlacklistRemove_Request) (*BlacklistRemove_Response, error) {
	if err := s.Impl.BlacklistRemove(request.Username); err != nil {
		return nil, err
	}
	return &BlacklistRemove_Response{}, nil
}

// Search 搜索联系人
func (s Server) Search(ctx context.Context, request *Search_Request) (*Search_Response, error) {
	contact := s.Impl.Search(request.Keyword, request.FromScene, request.SearchScene)
	if contact == nil {
		return nil, errors.New("not found")
	}
	return &Search_Response{Contacts: contact}, nil
}
