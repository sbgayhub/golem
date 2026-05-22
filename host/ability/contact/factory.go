package contactability

import (
	"fmt"
	"slices"
	"strings"

	base "github.com/sbgayhub/golem/host/api/base"
	contactapi "github.com/sbgayhub/golem/host/api/contact"
	"github.com/sbgayhub/golem/sdk/contact"
)

// specials 特殊账号列表
var specials = []string{
	"weixin", "qqmail", "qmessage", "tmessage", "floatbottle",
	"facebookapp", "qqfriend", "newsapp", "feedsapp", "masssendapp",
	"blogapp", "voipapp", "voicevoipapp", "voiceinputapp", "googlecontact",
	"fmessage", "medianote", "qqsync", "lbsapp", "shakeapp", "linkedinplugin",
	"gh_43f2581f6fd6", "gh_3dfda90e39d6", "gh_f0a92aa7146c",
	"gh_579db1f2cf89", "gh_b4af18eac3d5", "gh_e087bb5b95e6", "gh_051d9102de63",
}

func Build(data *contactapi.ModifyContact) (*contact.Contact, error) {
	var result contact.Contact

	result.Username = data.Username.Value
	result.Nickname = data.Nickname.Value
	result.Remark = data.Remark.Value
	result.Alias = data.GetAlias()
	result.Avatar = data.GetBigAvatarUrl()
	result.Type = getType(data)

	switch result.Type {
	case contact.ContactType_CONTACT_TYPE_UNSPECIFIED:
		return nil, fmt.Errorf("未知联系人 [%s]", data.Username.Value)
	case contact.ContactType_CONTACT_TYPE_FRIEND, contact.ContactType_CONTACT_TYPE_SPECIAL:
		friend := contact.Friend{
			Country:   data.GetCountry(),
			Province:  data.GetProvince(),
			City:      data.GetCity(),
			Gender:    GetGender(data.GetGender()),
			Signature: data.GetSignature(),
		}
		result.Data = &contact.Contact_Friend{Friend: &friend}
	case contact.ContactType_CONTACT_TYPE_CHATROOM:
		chatroom := contact.Chatroom{
			Owner: data.GetChatroomOwner(),
		}
		result.Data = &contact.Contact_Chatroom{Chatroom: &chatroom}
	case contact.ContactType_CONTACT_TYPE_OFFICIAL:

	}

	return &result, nil
}

func getType(data *contactapi.ModifyContact) contact.ContactType {
	// 自己
	//if data.Username.Value == ca.user.Username {
	//	return contact.TypeSelf
	//}
	// 特殊账号
	if slices.Contains(specials, data.Username.Value) {
		return contact.ContactType_CONTACT_TYPE_SPECIAL
	}
	// 群组
	if data.GetChatroomOwner() != "" {
		return contact.ContactType_CONTACT_TYPE_CHATROOM
	}
	// 公众号
	if strings.HasPrefix(data.Username.Value, "gh_") {
		return contact.ContactType_CONTACT_TYPE_OFFICIAL
	}

	return contact.ContactType_CONTACT_TYPE_FRIEND
}

func GetGender(data base.Gender) string {
	switch data {
	case 1:
		return "男"
	case 2:
		return "女"
	default:
		return "未知"
	}
}
