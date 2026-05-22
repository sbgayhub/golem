// Package chatroomability 提供群聊能力的实现。
package chatroomability

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/duke-git/lancet/v2/maputil"
	"github.com/duke-git/lancet/v2/stream"
	contactability "github.com/sbgayhub/golem/host/ability/contact"
	"github.com/sbgayhub/golem/host/api"
	chatroomapi "github.com/sbgayhub/golem/host/api/chatroom"
	contactapi "github.com/sbgayhub/golem/host/api/contact"
	sdk "github.com/sbgayhub/golem/sdk/chatroom"
	"github.com/sbgayhub/golem/sdk/contact"
)

// ability 群聊能力实现（缓存型）
type ability struct {
	none  sdk.Member
	api   chatroomapi.ChatroomService
	cache map[string]map[string]*sdk.Member
}

var instance ability

func init() {
	instance = ability{api: chatroomapi.Get(), cache: map[string]map[string]*sdk.Member{}}
	sdk.Instance = &instance
}

func Initial() {
	// 从文件加载数据
	if file, err := os.ReadFile(filepath.Join("data", "chatroom.json")); err == nil {
		if err := json.Unmarshal(file, &instance.cache); err != nil {
			slog.Warn("[chatroom ability] 反序列化群组信息失败")
			return
		}
		slog.Info("[chatroom ability] 从文件加载群组信息成功", "count", len(instance.cache))
		return
	}

	// 从api获取数据
	stream.Of(contact.Instance.List()...).
		Filter(func(item *contact.Contact) bool { return item.Type == contact.ContactType_CONTACT_TYPE_CHATROOM }).
		ForEach(func(item *contact.Contact) {
			if chatroom, err := contactapi.Get().Detail([]string{item.Username}); err != nil {
				slog.Warn("[chatroom ability] 获取群组信息失败", "chatroom", item.Nickname, "err", err)
				return
			} else {
				for _, info := range chatroom.ContactList[0].Members.List {
					cache := maputil.GetOrSet(instance.cache, item.Username, map[string]*sdk.Member{})
					cache[info.GetUsername()] = &sdk.Member{
						Username:        info.GetUsername(),
						Nickname:        info.GetNickname(),
						DisplayName:     info.GetDisplayName(),
						Avatar:          info.GetSmallAvatarUrl(),
						Flag:            info.GetChatroomMemberFlag(),
						InviterUsername: info.GetInviterUsername(),
					}
				}
			}
			slog.Info("[chatroom ability] 群成员获取成功", "chatroom", item.Nickname, "count", len(instance.cache[item.Username]))
		})
	Destroy()
}

func Destroy() {
	marshal, err := json.Marshal(instance.cache)
	if err != nil {
		slog.Warn("[chatroom ability] 序列化群组信息失败", "err", err)
		return
	}
	if err := os.WriteFile(filepath.Join("data", "chatroom.json"), marshal, 0755); err != nil {
		slog.Warn("[chatroom ability] 保存群组信息失败", "err", err)
		return
	}
}

func Refresh(name string) error {
	// 清除缓存
	instance.cache[name] = map[string]*sdk.Member{}
	// 从api获取数据
	if chatroom, err := contactapi.Get().Detail([]string{name}); err != nil {
		return fmt.Errorf("[chatroom ability] 获取群组 [%s] 信息失败: %w", name, err)
	} else {
		for _, info := range chatroom.ContactList[0].Members.List {
			instance.cache[name][info.GetUsername()] = &sdk.Member{
				Username:        info.GetUsername(),
				Nickname:        info.GetNickname(),
				DisplayName:     info.GetDisplayName(),
				Avatar:          info.GetSmallAvatarUrl(),
				Flag:            info.GetChatroomMemberFlag(),
				InviterUsername: info.GetInviterUsername(),
			}
		}
	}
	slog.Info("[chatroom ability] 群成员获取成功", "chatroom", name, "count", len(instance.cache[name]))
	Destroy()
	return nil
}

func (a *ability) loadCache() error {
	return nil
}

func (a *ability) saveCache() error {
	return nil
}

func (a *ability) Create(members []string) (*sdk.Create_Response, error) {
	res, err := a.api.Create(members)
	if err != nil {
		return nil, err
	}
	response := sdk.Create_Response{
		Chatroom:       res.GetChatroom(),
		Name:           res.GetTopic(),
		Members:        make([]*sdk.Member, 0),
		BigAvatarUrl:   res.GetBigAvatarUrl(),
		SmallAvatarUrl: res.GetSmallAvatarUrl(),
	}
	for _, member := range res.MemberList {
		m := sdk.Member{
			Username:        member.Username.Value,
			Nickname:        member.Nickname.Value,
			Remark:          member.Remark.Value,
			Alias:           "",
			DisplayName:     "",
			Avatar:          "",
			Flag:            0,
			InviterUsername: "",
			Gender:          contactability.GetGender(member.GetGender()),
			Country:         member.GetCountry(),
			Province:        member.GetProvince(),
			City:            member.GetCity(),
			Signature:       member.GetSignature(),
		}
		response.Members = append(response.Members, &m)
		a.cache[response.Chatroom] = map[string]*sdk.Member{m.Username: &m}
	}
	_ = a.saveCache()
	return &response, nil
}

func (a *ability) FacingCreate(password string, latitude, longitude float32, operate uint32) (*sdk.FacingCreate_Response, error) {
	create, err := a.api.FacingCreate(password, latitude, longitude, operate)
	if err != nil {
		return nil, err
	}
	res := sdk.FacingCreate_Response{
		Chatroom:    create.GetChatroom(),
		Ticket:      create.GetTicket(),
		MemberCount: create.GetMemberCount(),
		Members:     make([]*sdk.FacingCreate_Member, 0),
	}
	for _, member := range create.MemberList {
		var m sdk.FacingCreate_Member
		if err := api.TransformProto(member, &m); err != nil {
			return nil, err
		}
		res.Members = append(res.Members, &m)
		a.cache[res.Chatroom] = map[string]*sdk.Member{
			member.GetUsername(): {
				Username:    member.GetUsername(),
				Nickname:    member.GetNickname(),
				DisplayName: member.GetDisplayName(),
				Avatar:      member.GetAvatarUrl(),
				Flag:        member.GetFlag(),
			},
		}
	}
	_ = a.saveCache()
	return &res, nil
}

func (a *ability) GetInfo(chatroom string) (*sdk.GetInfo_Response, error) {
	get := contact.Instance.Get(chatroom)
	if get == nil {
		return nil, fmt.Errorf("[%s] 不存在", chatroom)
	}
	res := sdk.GetInfo_Response{
		Username:    get.Username,
		Nickname:    get.Nickname,
		Remark:      get.Remark,
		Alias:       get.Alias,
		Avatar:      get.Avatar,
		Owner:       get.GetChatroom().GetOwner(),
		MemberCount: get.GetChatroom().GetMemberCount(),
	}
	return &res, nil
}

func (a *ability) GetQRCode(chatroom string) (*sdk.GetQRCode_Response, error) {
	code, err := a.api.GetQRCode(chatroom)
	if err != nil {
		return nil, err
	}
	return &sdk.GetQRCode_Response{
		Qrcode:        code.GetQrcodeBuffer().Data,
		QrcodeUrl:     code.GetQrcodeUrl(),
		FooterWording: code.GetFooterWording(),
	}, nil
}

func (a *ability) AddMember(chatroom string, members []string) (*sdk.AddMember_Response, error) {
	add, err := a.api.AddMember(chatroom, members)
	if err != nil {
		return nil, err
	}
	res := sdk.AddMember_Response{MemberCount: add.GetMemberCount(), Members: make([]*sdk.Member, 0)}
	for _, member := range add.MemberList {
		m := sdk.Member{
			Username:  member.Username.Value,
			Nickname:  member.Nickname.Value,
			Remark:    member.Remark.Value,
			Gender:    contactability.GetGender(member.GetGender()),
			Country:   member.GetCountry(),
			Province:  member.GetProvince(),
			City:      member.GetCity(),
			Signature: member.GetSignature(),
		}
		res.Members = append(res.Members, &m)
		cache := maputil.GetOrSet(a.cache, chatroom, map[string]*sdk.Member{})
		cache[m.Username] = &m
	}
	_ = a.saveCache()
	return &res, nil
}

func (a *ability) InviteMember(chatroom string, members []string) (*sdk.InviteMember_Response, error) {
	//TODO implement me
	panic("implement me")
}

func (a *ability) RemoveMember(chatroom string, members []string) (*sdk.RemoveMember_Response, error) {
	//TODO implement me
	panic("implement me")
}

func (a *ability) SetName(chatroom, name string) (*sdk.SetName_Response, error) {
	//TODO implement me
	panic("implement me")
}

func (a *ability) SetAnnouncement(chatroom, content string) (*sdk.SetAnnouncement_Response, error) {
	//TODO implement me
	panic("implement me")
}

func (a *ability) SetRemark(chatroom, remark string) (*sdk.SetRemark_Response, error) {
	//TODO implement me
	panic("implement me")
}

func (a *ability) Save(chatroom string, save bool) (*sdk.Save_Response, error) {
	//TODO implement me
	panic("implement me")
}

func (a *ability) SetAdmin(chatroom string, members []string) (*sdk.SetAdmin_Response, error) {
	//TODO implement me
	panic("implement me")
}

func (a *ability) RemoveAdmin(chatroom string, members []string) (*sdk.RemoveAdmin_Response, error) {
	//TODO implement me
	panic("implement me")
}

func (a *ability) TransferOwner(chatroom, newOwner string) (*sdk.TransferOwner_Response, error) {
	//TODO implement me
	panic("implement me")
}

func (a *ability) Quit(chatroom string) (*sdk.Quit_Response, error) {
	//TODO implement me
	panic("implement me")
}

func (a *ability) ScanJoin(qrcodeURL string) (*sdk.ScanJoin_Response, error) {
	//TODO implement me
	panic("implement me")
}

func (a *ability) ScanJoinEnterprise(qrcodeURL string) (*sdk.ScanJoin_Response, error) {
	//TODO implement me
	panic("implement me")
}

func (a *ability) ConsentJoin(inviteURL string) (*sdk.ConsentJoin_Response, error) {
	//TODO implement me
	panic("implement me")
}

func (a *ability) ListMembers(chatroom string) []*sdk.Member {
	//TODO implement me
	panic("implement me")
}

func (a *ability) GetMember(chatroom string, name string) *sdk.Member {
	cache := maputil.GetOrSet(a.cache, chatroom, map[string]*sdk.Member{}) // 获取群组缓存
	member, ex := cache[name]                                              // 获取群成员
	if ex && member.Username != "" {                                       // 如果缓存存在且username不为空，则返回
		return member
	} else if ex && member.Username == "" { // 如果缓存存在但是username为空，说明成员不存在，返回nil
		return nil
	}

	// 缓存不存在，通过api获取成员信息
	detail, err := a.api.GetMemberDetail(chatroom, []string{name})
	if err != nil {
		slog.Warn("[chatroom ability] 获取群成员列表失败", "err", err)
		return nil
	}
	c := detail.GetContactList()[0]
	res := sdk.Member{
		Username:        c.Username.Value,
		Nickname:        c.Nickname.Value,
		Remark:          c.Remark.Value,
		Alias:           c.GetAlias(),
		DisplayName:     "",
		Avatar:          c.GetSmallAvatarUrl(),
		Flag:            0,
		InviterUsername: "",
		Gender:          contactability.GetGender(c.GetGender()),
		Country:         c.GetCountry(),
		Province:        c.GetCity(),
		City:            c.GetCity(),
		Signature:       c.GetSignature(),
	}
	// 添加缓存
	cache[name] = &res
	_ = a.saveCache()

	return &res
}

func (a *ability) GetMembersDetail(chatroom string, members []string) []*sdk.Member {
	//TODO implement me
	panic("implement me")
}

//
//func (a *ability) GetInfo(chatroom string) (*sdk.GetInfo_Response, error) {
//	//TODO implement me
//	panic("implement me")
//}
//
//func (a *ability) Save(chatroom string, save bool) (*sdk.Save_Response, error) {
//	//TODO implement me
//	panic("implement me")
//}
//
//func (a *ability) ListMembers(chatroom string) []*sdk.Member {
//	//TODO implement me
//	panic("implement me")
//}
//
//func (a *ability) GetMember(chatroom string, member string) *sdk.Member {
//	//TODO implement me
//	panic("implement me")
//}
//
//func (a *ability) GetMembersDetail(chatroom string, members []string) []*sdk.Member {
//	//TODO implement me
//	panic("implement me")
//}
//
//func init() {
//	sdk.Instance = &ability{api: api.Get()}
//}
//
//// Create 创建群聊
//func (a *ability) Create(members []string) (*sdk.Create_Response, error) {
//	resp, err := a.api.Create(members)
//	if resp == nil || err != nil {
//		return nil, err
//	}
//	result := &sdk.Create_Response{
//		Chatroom:          resp.GetChatroom(),
//		Name:           resp.GetTopic(),
//		BigAvatarUrl:   resp.GetBigAvatarUrl(),
//		SmallAvatarUrl: resp.GetSmallAvatarUrl(),
//	}
//	for _, m := range resp.GetMemberList() {
//		result.Members = append(result.Members, &sdk.Member{
//			Username: m.GetUsername().GetValue(),
//			Nickname: m.GetNickname().GetValue(),
//			Avatar:   resp.GetBigAvatarUrl(),
//		})
//	}
//	// 写入群缓存
//	if result.Chatroom != "" {
//		a.chatroomCache.Store(result.Chatroom, &sdk.Chatroom{
//			ChatroomId:        result.Chatroom,
//			Name:           result.Name,
//			BigAvatarUrl:   result.BigAvatarUrl,
//			SmallAvatarUrl: result.SmallAvatarUrl,
//		})
//	}
//	return result, nil
//}
//
//// FacingCreate 面对面建群
//func (a *ability) FacingCreate(password string, latitude, longitude float32, operate uint32) (*sdk.FacingCreate_Response, error) {
//	resp, err := a.api.FacingCreate(password, latitude, longitude, operate)
//	if resp == nil || err != nil {
//		return nil, err
//	}
//	result := &sdk.FacingCreate_Response{
//		Chatroom:       resp.GetChatroom(),
//		Ticket:      resp.GetTicket(),
//		MemberCount: resp.GetMemberCount(),
//	}
//	if result.Chatroom != "" {
//		a.chatroomCache.Store(result.Chatroom, &sdk.Chatroom{ChatroomId: result.Chatroom})
//	}
//	return result, nil
//}
//
//// GetInfo 获取群信息（缓存优先）
//func (a *ability) GetInfo(chatroomID string) (*sdk.Chatroom, error) {
//	resp, err := a.api.GetInfo(chatroomID)
//	if resp == nil || err != nil {
//		return nil, err
//	}
//	info := &sdk.Chatroom{
//		ChatroomId:      chatroomID,
//		Announcement: resp.GetAnnouncement(),
//	}
//	a.chatroomCache.Store(chatroomID, info)
//	return info, nil
//}
//
//func (a *ability) GetMember(chatroomID string, member string) (*sdk.Member, error) {
//	//TODO implement me
//	panic("implement me")
//}
//
//// GetMemberDetail 获取群成员详情（缓存优先）
//func (a *ability) GetMemberDetail(chatroomID string) ([]*sdk.Member, error) {
//	resp, err := a.api.GetMemberDetail(chatroomID)
//	if resp == nil || err != nil {
//		return nil, err
//	}
//	result := resp.GetResult()
//	if result == nil {
//		return nil, nil
//	}
//	members := make([]*sdk.Member, 0, len(result.GetList()))
//	for _, m := range result.GetList() {
//		var sdkMember sdk.Member
//		if err := api.TransformProto(m, &sdkMember); err != nil {
//			return nil, err
//		}
//		members = append(members, &sdkMember)
//	}
//	a.memberCache.Store(chatroomID, members)
//	return members, nil
//}
//
//// GetQRCode 获取群二维码
//func (a *ability) GetQRCode(chatroomID string) (*sdk.GetQRCode_Response, error) {
//	resp, err := a.api.GetQRCode(chatroomID)
//	if resp == nil || err != nil {
//		return nil, err
//	}
//	var result sdk.GetQRCode_Response
//	if err := api.TransformProto(resp, &result); err != nil {
//		return nil, err
//	}
//	return &result, nil
//}
//
//// AddMember 添加群成员
//func (a *ability) AddMember(chatroomID string, members []string) (*sdk.AddMember_Response, error) {
//	resp, err := a.api.AddMember(chatroomID, members)
//	if resp == nil || err != nil {
//		return nil, err
//	}
//	var result sdk.AddMember_Response
//	if err := api.TransformProto(resp, &result); err != nil {
//		return nil, err
//	}
//	return &result, nil
//}
//
//// InviteMember 邀请群成员
//func (a *ability) InviteMember(chatroomID string, members []string) (*sdk.InviteMember_Response, error) {
//	resp, err := a.api.InviteMember(chatroomID, members)
//	if resp == nil || err != nil {
//		return nil, err
//	}
//	var result sdk.InviteMember_Response
//	if err := api.TransformProto(resp, &result); err != nil {
//		return nil, err
//	}
//	return &result, nil
//}
//
//// RemoveMember 移除群成员
//func (a *ability) RemoveMember(chatroomID string, members []string) (*sdk.RemoveMember_Response, error) {
//	resp, err := a.api.RemoveMember(chatroomID, members)
//	if resp == nil || err != nil {
//		return nil, err
//	}
//	var result sdk.RemoveMember_Response
//	if err := api.TransformProto(resp, &result); err != nil {
//		return nil, err
//	}
//	return &result, nil
//}
//
//// SetName 设置群名称（更新缓存）
//func (a *ability) SetName(chatroomID, name string) (*sdk.SetName_Response, error) {
//	resp, err := a.api.SetName(chatroomID, name)
//	if resp == nil || err != nil {
//		return nil, err
//	}
//	if v, ok := a.chatroomCache.Load(chatroomID); ok {
//		v.(*sdk.Chatroom).Name = name
//	}
//	var result sdk.SetName_Response
//	if err := api.TransformProto(resp, &result); err != nil {
//		return nil, err
//	}
//	return &result, nil
//}
//
//// SetAnnouncement 设置群公告（更新缓存）
//func (a *ability) SetAnnouncement(chatroomID, content string) (*sdk.SetAnnouncement_Response, error) {
//	resp, err := a.api.SetAnnouncement(chatroomID, content)
//	if resp == nil || err != nil {
//		return nil, err
//	}
//	if v, ok := a.chatroomCache.Load(chatroomID); ok {
//		v.(*sdk.Chatroom).Announcement = content
//	}
//	var result sdk.SetAnnouncement_Response
//	if err := api.TransformProto(resp, &result); err != nil {
//		return nil, err
//	}
//	return &result, nil
//}
//
//// SetRemark 设置群备注（更新缓存）
//func (a *ability) SetRemark(chatroomID, remark string) (*sdk.SetRemark_Response, error) {
//	resp, err := a.api.SetRemark(chatroomID, remark)
//	if resp == nil || err != nil {
//		return nil, err
//	}
//	if v, ok := a.chatroomCache.Load(chatroomID); ok {
//		v.(*sdk.Chatroom).Remark = remark
//	}
//	var result sdk.SetRemark_Response
//	if err := api.TransformProto(resp, &result); err != nil {
//		return nil, err
//	}
//	return &result, nil
//}
//
//// SetContactList 保存到通讯录
//func (a *ability) SetContactList(chatroomID string, save bool) (*sdk.Save_Response, error) {
//	resp, err := a.api.SetContactList(chatroomID, save)
//	if resp == nil || err != nil {
//		return nil, err
//	}
//	var result sdk.Save_Response
//	if err := api.TransformProto(resp, &result); err != nil {
//		return nil, err
//	}
//	return &result, nil
//}
//
//// SetAdmin 设置群管理员（更新缓存）
//func (a *ability) SetAdmin(chatroomID string, members []string) (*sdk.SetAdmin_Response, error) {
//	resp, err := a.api.SetAdmin(chatroomID, members)
//	if resp == nil || err != nil {
//		return nil, err
//	}
//	if v, ok := a.chatroomCache.Load(chatroomID); ok {
//		info := v.(*sdk.Chatroom)
//		existing := make(map[string]bool, len(info.Admins))
//		for _, admin := range info.Admins {
//			existing[admin] = true
//		}
//		for _, m := range members {
//			if !existing[m] {
//				info.Admins = append(info.Admins, m)
//			}
//		}
//	}
//	var result sdk.SetAdmin_Response
//	if err := api.TransformProto(resp, &result); err != nil {
//		return nil, err
//	}
//	return &result, nil
//}
//
//// RemoveAdmin 移除群管理员（更新缓存）
//func (a *ability) RemoveAdmin(chatroomID string, members []string) (*sdk.RemoveAdmin_Response, error) {
//	resp, err := a.api.RemoveAdmin(chatroomID, members)
//	if resp == nil || err != nil {
//		return nil, err
//	}
//	if v, ok := a.chatroomCache.Load(chatroomID); ok {
//		info := v.(*sdk.Chatroom)
//		removeSet := make(map[string]bool, len(members))
//		for _, m := range members {
//			removeSet[m] = true
//		}
//		filtered := info.Admins[:0]
//		for _, admin := range info.Admins {
//			if !removeSet[admin] {
//				filtered = append(filtered, admin)
//			}
//		}
//		info.Admins = filtered
//	}
//	var result sdk.RemoveAdmin_Response
//	if err := api.TransformProto(resp, &result); err != nil {
//		return nil, err
//	}
//	return &result, nil
//}
//
//// TransferOwner 转让群主（更新缓存）
//func (a *ability) TransferOwner(chatroomID, newOwner string) (*sdk.TransferOwner_Response, error) {
//	resp, err := a.api.TransferOwner(chatroomID, newOwner)
//	if resp == nil || err != nil {
//		return nil, err
//	}
//	if v, ok := a.chatroomCache.Load(chatroomID); ok {
//		v.(*sdk.Chatroom).Owner = newOwner
//	}
//	var result sdk.TransferOwner_Response
//	if err := api.TransformProto(resp, &result); err != nil {
//		return nil, err
//	}
//	return &result, nil
//}
//
//// Quit 退出群聊（清除缓存）
//func (a *ability) Quit(chatroomID string) (*sdk.Quit_Response, error) {
//	resp, err := a.api.Quit(chatroomID)
//	if resp == nil || err != nil {
//		return nil, err
//	}
//	a.chatroomCache.Delete(chatroomID)
//	a.memberCache.Delete(chatroomID)
//	var result sdk.Quit_Response
//	if err := api.TransformProto(resp, &result); err != nil {
//		return nil, err
//	}
//	return &result, nil
//}
//
//// ScanJoin 扫码进群
//func (a *ability) ScanJoin(qrcodeURL string) (*sdk.ScanJoin_Response, error) {
//	resp, err := a.api.ScanJoin(qrcodeURL)
//	if resp == nil || err != nil {
//		return nil, err
//	}
//	if resp.ChatroomId != "" {
//		a.chatroomCache.Store(resp.ChatroomId, &sdk.Chatroom{ChatroomId: resp.ChatroomId})
//	}
//	return &sdk.ScanJoin_Response{
//		Chatroom:   resp.ChatroomId,
//		Message: resp.Message,
//	}, nil
//}
//
//// ScanJoinEnterprise 企业微信扫码进群
//func (a *ability) ScanJoinEnterprise(qrcodeURL string) (*sdk.ScanJoin_Response, error) {
//	resp, err := a.api.ScanJoinEnterprise(qrcodeURL)
//	if resp == nil || err != nil {
//		return nil, err
//	}
//	if resp.ChatroomId != "" {
//		a.chatroomCache.Store(resp.ChatroomId, &sdk.Chatroom{ChatroomId: resp.ChatroomId})
//	}
//	return &sdk.ScanJoin_Response{
//		Chatroom:   resp.ChatroomId,
//		Message: resp.Message,
//	}, nil
//}
//
//// ConsentJoin 同意入群邀请
//func (a *ability) ConsentJoin(inviteURL string) (*sdk.ConsentJoin_Response, error) {
//	resp, err := a.api.ConsentJoin(inviteURL)
//	if resp == nil || err != nil {
//		return nil, err
//	}
//	if resp.ChatroomId != "" {
//		a.chatroomCache.Store(resp.ChatroomId, &sdk.Chatroom{ChatroomId: resp.ChatroomId})
//	}
//	return &sdk.ConsentJoin_Response{
//		Chatroom:   resp.ChatroomId,
//		Message: resp.Message,
//	}, nil
//}
//
//// GetChatroomByKey 按键查询缓存群信息
//func (a *ability) GetChatroomByKey(key string) (*sdk.Chatroom, bool) {
//	strategy, realKey := parseKeyPrefix(key)
//	return a.getByStrategy(realKey, strategy)
//}
//
//// GetChatroomByStrategy 按策略查询缓存群信息
//func (a *ability) GetChatroomByStrategy(key string, strategy sdk.RetrievalType) (*sdk.Chatroom, bool) {
//	return a.getByStrategy(key, strategy)
//}
//
//// GetChatroomMembers 获取缓存群成员列表
//func (a *ability) GetChatroomMembers(chatroomID string) ([]*sdk.Member, bool) {
//	v, ok := a.memberCache.Load(chatroomID)
//	if !ok {
//		return nil, false
//	}
//	return v.([]*sdk.Member), true
//}
//
//// getByStrategy 按策略从缓存查询群信息
//func (a *ability) getByStrategy(key string, strategy sdk.RetrievalType) (*sdk.Chatroom, bool) {
//	switch strategy {
//	case sdk.RetrievalType_RETRIEVAL_TYPE_GROUP_ID:
//		v, ok := a.chatroomCache.Load(key)
//		if !ok {
//			return nil, false
//		}
//		return v.(*sdk.Chatroom), true
//	case sdk.RetrievalType_RETRIEVAL_TYPE_NAME:
//		var found *sdk.Chatroom
//		a.chatroomCache.Range(func(_, value any) bool {
//			info := value.(*sdk.Chatroom)
//			if info.Name == key {
//				found = info
//				return false
//			}
//			return true
//		})
//		return found, found != nil
//	default:
//		v, ok := a.chatroomCache.Load(key)
//		if !ok {
//			return nil, false
//		}
//		return v.(*sdk.Chatroom), true
//	}
//}
//
//// parseKeyPrefix 解析查询键前缀
//func parseKeyPrefix(key string) (sdk.RetrievalType, string) {
//	prefix, value, ok := strings.Cut(key, "::")
//	if !ok {
//		return sdk.RetrievalType_RETRIEVAL_TYPE_GROUP_ID, key
//	}
//	switch prefix {
//	case "name":
//		return sdk.RetrievalType_RETRIEVAL_TYPE_NAME, value
//	case "chatroom_id":
//		return sdk.RetrievalType_RETRIEVAL_TYPE_GROUP_ID, value
//	default:
//		return sdk.RetrievalType_RETRIEVAL_TYPE_GROUP_ID, key
//	}
//}
