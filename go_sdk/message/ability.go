package message

import "io"

// Ability 消息能力接口（供插件嵌入使用）
type Ability interface {
	// Send 发送消息
	Send(msg *Message) (*Send_Response, error)
	// Forward 转发消息给新接收者
	Forward(msg *Message, receiver string) (*Forward_Response, error)
	// Revoke 撤回消息
	Revoke(receiver string, newMsgId uint64) (*Revoke_Response, error)
	// Download 下载消息中的媒体资源（server-stream：流式返回）
	Download(msg *Message) (io.ReadCloser, error)
}

// Instance 消息能力实例（由 host/ability 层注入）
var Instance Ability
