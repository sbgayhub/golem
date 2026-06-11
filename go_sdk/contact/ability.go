package contact

// Ability 联系人能力接口（供插件嵌入使用）
type Ability interface {
	// Get 按键查询缓存联系人, 支持前缀：username::（默认）、nickname::、remark::
	Get(key string) *Contact
	// List 获取联系人列表
	List() []*Contact
	// GetSelf 获取当前登录账号信息
	GetSelf() *SelfInfo
	// SetRemark 设置联系人备注
	SetRemark(username, remark string) error
	// AddFriend 发送好友申请
	AddFriend(v1, v2, content string, operate, scene int) error
	// VerifyFriend 通过好友验证
	VerifyFriend(v1, v2 string, scene int) error
	// Delete 删除联系人
	Delete(username string) error
	// BlacklistAdd 添加到黑名单
	BlacklistAdd(username string) error
	// BlacklistRemove 从黑名单移除
	BlacklistRemove(username string) error
	// Search 搜索联系人
	Search(keyword string, fromScene, searchScene uint32) *Contact
}

// Instance 联系人能力实例（由 host/ability 层注入）
var Instance Ability
