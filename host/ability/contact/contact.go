// Package contactability 提供联系人能力的实现（缓存型）。
package contactability

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/duke-git/lancet/v2/slice"
	"github.com/sbgayhub/golem/host/api"
	contactapi "github.com/sbgayhub/golem/host/api/contact"
	sdk "github.com/sbgayhub/golem/sdk/contact"
)

// ability 联系人能力实现（缓存型）
type ability struct {
	api   contactapi.ContactService
	cache map[string]*sdk.Contact
}

var instance ability

func init() {
	instance = ability{api: contactapi.Get(), cache: map[string]*sdk.Contact{}}
	sdk.Instance = &instance
}

func Initial() {
	// 从文件读取联系人信息
	if file, err := os.ReadFile(filepath.Join("data", "contact.json")); err == nil {
		if err := json.Unmarshal(file, &instance.cache); err != nil {
			slog.Warn("[contact ability] 反序列化联系人信息失败")
			return
		}
		slog.Info("[contact ability] 从文件加载联系人信息成功", "count", len(instance.cache))
		return
	}

	// 通过api获取联系人信息
	list, err := instance.api.List()
	if err != nil {
		slog.Warn("[contact ability] 获取联系人列表失败", "err", err)
		return
	}
	for _, usernames := range slice.Chunk(list, 20) {
		detail, err := instance.api.Detail(usernames)
		if err != nil {
			slog.Warn("[contact ability] 获取联系人详细信息失败", "err", err)
			return
		}
		for _, c := range detail.GetContactList() {
			if build, err := Build(c); err != nil {
				slog.Warn("[contact ability] 构建联系人失败", "err", err)
				continue
			} else {
				instance.cache[build.Username] = build
			}
		}
	}
	slog.Info("[contact ability] 获取联系人信息成功", "count", len(instance.cache))
	Destroy()
}

func Destroy() {
	marshal, err := json.Marshal(instance.cache)
	if err != nil {
		slog.Warn("[contact ability] 序列化联系人信息失败", "err", err)
		return
	}
	if err := os.WriteFile(filepath.Join("data", "contact.json"), marshal, 0755); err != nil {
		slog.Warn("[contact ability] 保存联系人信息失败", "err", err)
		return
	}
}

// Refresh 刷新缓存
func Refresh() error {
	// 清除缓存
	instance.cache = map[string]*sdk.Contact{}
	// 通过api获取联系人信息
	list, err := instance.api.List()
	if err != nil {
		return fmt.Errorf("[contact ability] 获取联系人列表失败: %w", err)
	}
	for _, usernames := range slice.Chunk(list, 20) {
		detail, err := instance.api.Detail(usernames)
		if err != nil {
			return fmt.Errorf("[contact ability] 获取联系人详细信息失败: %w", err)
		}
		for _, c := range detail.GetContactList() {
			if build, err := Build(c); err != nil {
				slog.Warn("[contact ability] 构建联系人失败", "err", err)
				continue
			} else {
				instance.cache[build.Username] = build
			}
		}
	}
	slog.Info("[contact ability] 获取联系人信息成功", "count", len(instance.cache))
	Destroy()
	return nil
}

// Get 按键查询缓存联系人，支持前缀：username::（默认）、nickname::、remark::
func (a *ability) Get(key string) *sdk.Contact {
	if c := getStrategy(key).find(a.cache); c != nil {
		return c
	}
	return nil
}

// List 获取联系人列表
func (a *ability) List() []*sdk.Contact {
	var result []*sdk.Contact
	for _, c := range a.cache {
		result = append(result, c)
	}
	return result
}

// SetRemark 设置联系人备注
func (a *ability) SetRemark(username, remark string) error {
	if response, err := a.api.SetRemark(username, remark); err != nil {
		return err
	} else {
		var result sdk.SetRemark_Response
		if err := api.TransformProto(response, &result); err != nil {
			return err
		}
		return nil
	}
}

// AddFriend 发送好友申请
func (a *ability) AddFriend(v1, v2, content string, operate, scene int) error {
	_, err := a.api.Request(v1, v2, content, operate, scene)
	if err != nil {
		return err
	}
	return nil
}

// VerifyFriend 通过好友验证
func (a *ability) VerifyFriend(v1, v2 string, scene int) error {
	if _, err := a.api.Verify(v1, v2, scene); err != nil {
		return err
	}
	return nil
}

// Delete 删除联系人
func (a *ability) Delete(username string) error {
	if _, err := a.api.Delete(username); err != nil {
		return err
	}
	return nil
}

// BlacklistAdd 添加到黑名单
func (a *ability) BlacklistAdd(username string) error {
	if _, err := a.api.BlacklistAdd(username); err != nil {
		return err
	}
	return nil
}

// BlacklistRemove 从黑名单移除
func (a *ability) BlacklistRemove(username string) error {
	if _, err := a.api.BlacklistRemove(username); err != nil {
		return err
	}
	return nil
}

// Search 搜索联系人
func (a *ability) Search(keyword string, fromScene, searchScene uint32) *sdk.Contact {
	search, err := a.api.Search(keyword, fromScene, searchScene)
	if err != nil {
		return nil
	}
	return &sdk.Contact{
		Username: search.Username.Value,
		Nickname: search.Nickname.Value,
		Remark:   "",
		Alias:    search.GetAlias(),
		Avatar:   search.GetSmallAvatarUrl(),
		Type:     sdk.ContactType_CONTACT_TYPE_FRIEND,
		Data:     nil,
	}
}
