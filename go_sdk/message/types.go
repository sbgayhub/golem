package message

import "log/slog"

var (
	TypeUnknown        = &Type{Code: 0, Topic: "message::unknown", Desc: "未知"}                // 未知消息类型
	TypeText           = &Type{Code: 1, Topic: "message::text", Desc: "文本"}                   // 文本消息
	TypeHtml           = &Type{Code: 2, Topic: "message::html", Desc: "html"}                 // html消息
	TypeImage          = &Type{Code: 3, Topic: "message::image", Desc: "图片"}                  // 图片消息
	TypeFile           = &Type{Code: 6, Topic: "message::file", Desc: "文件"}                   // 文件消息
	TypeVoice          = &Type{Code: 34, Topic: "message::voice", Desc: "语音"}                 // 语音消息
	TypeVerify         = &Type{Code: 37, Topic: "message::verify", Desc: "好友验证"}              // 好友验证消息
	TypePossibleFriend = &Type{Code: 40, Topic: "message::possible_friend", Desc: "可能为好友"}    // 可能为好友消息
	TypePersonalCard   = &Type{Code: 42, Topic: "message::personal_card", Desc: "个人名片"}       // 个人名片消息
	TypeVideo          = &Type{Code: 43, Topic: "message::video", Desc: "视频"}                 // 视频消息
	TypeEmoji          = &Type{Code: 47, Topic: "message::emoji", Desc: "表情包"}                // 表情包消息
	TypeLocation       = &Type{Code: 48, Topic: "message::location", Desc: "位置"}              // 位置消息
	TypeApplication    = &Type{Code: 49, Topic: "message::app", Desc: "应用"}                   // 应用消息
	TypeAppNote        = &Type{Code: 4924, Topic: "message::app::note", Desc: "笔记"}           // 笔记消息
	TypeAppMiniapp     = &Type{Code: 4933, Topic: "message::app::miniapp", Desc: "小程序"}       // 小程序消息
	TypeAppFileNotify  = &Type{Code: 4974, Topic: "message::app::file::notify", Desc: "文件通知"} // 文件通知消息
	TypeAppFileAttach  = &Type{Code: 4906, Topic: "message::app::file::attach", Desc: "文件附件"} // 文件附件消息
	TypeAppChatRecord  = &Type{Code: 4919, Topic: "message::app::chat_record", Desc: "聊天记录"}  // 聊天记录消息
	TypeAppMusic       = &Type{Code: 4976, Topic: "message::app::music", Desc: "音乐"}          // 音乐消息
	TypeAppLink        = &Type{Code: 4905, Topic: "message::app::link", Desc: "链接"}           // 链接消息
	TypeAppQuote       = &Type{Code: 4957, Topic: "message::app::quote", Desc: "引用"}          // 引用消息
	TypeAppFinder      = &Type{Code: 4951, Topic: "message::app::finder", Desc: "视频号"}        // 视频号消息
	TypeVOIP           = &Type{Code: 50, Topic: "message::voip", Desc: "VoIP通话"}              // VoIP通话消息
	TypeStatusNotify   = &Type{Code: 51, Topic: "message::status_notify", Desc: "状态通知"}       // 状态通知消息
	TypeVOIPNotify     = &Type{Code: 52, Topic: "message::voip::notify", Desc: "VoIP通知"}      // VoIP通知消息
	TypeVOIPInvite     = &Type{Code: 53, Topic: "message::voip::invite", Desc: "VoIP邀请"}      // VoIP邀请消息
	TypeTinyVideo      = &Type{Code: 62, Topic: "message::tiny_video", Desc: "小视频"}           // 小视频消息
	TypeBusinessCard   = &Type{Code: 66, Topic: "message::business_card", Desc: "企微名片"}       // 企微名片消息
	TypeSystemNotify   = &Type{Code: 9999, Topic: "message::system_notify", Desc: "系统通知"}     // 系统通知消息
	TypeSystem         = &Type{Code: 10000, Topic: "message::system", Desc: "系统"}             // 系统消息
	TypeSystemTip      = &Type{Code: 10002, Topic: "message::system_tip", Desc: "系统提示"}       // 系统提示消息
)

var m = map[int32]*Type{
	0:     TypeUnknown,
	1:     TypeText,
	2:     TypeHtml,
	3:     TypeImage,
	6:     TypeFile,
	34:    TypeVoice,
	37:    TypeVerify,
	40:    TypePossibleFriend,
	42:    TypePersonalCard,
	43:    TypeVideo,
	47:    TypeEmoji,
	48:    TypeLocation,
	49:    TypeApplication,
	4924:  TypeAppNote,
	4933:  TypeAppMiniapp,
	4974:  TypeAppFileNotify,
	4906:  TypeAppFileAttach,
	4919:  TypeAppChatRecord,
	4976:  TypeAppMusic,
	4905:  TypeAppLink,
	4957:  TypeAppQuote,
	4951:  TypeAppFinder,
	50:    TypeVOIP,
	51:    TypeStatusNotify,
	52:    TypeVOIPNotify,
	53:    TypeVOIPInvite,
	62:    TypeTinyVideo,
	66:    TypeBusinessCard,
	9999:  TypeSystemNotify,
	10000: TypeSystem,
	10002: TypeSystemTip,
}

func TypeOf(code int32) *Type {
	if t, ex := m[code]; ex {
		return t
	}
	slog.Warn("未知的消息类型", "code", code)
	return TypeUnknown
}
