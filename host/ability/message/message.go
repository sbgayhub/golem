// Package messageability 提供消息能力的实现。
package messageability

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"log/slog"
	"strings"

	messagepb "golem/proto/message"

	api "github.com/sbgayhub/golem/host/api/message"
	"github.com/sbgayhub/golem/sdk/contact"
	"github.com/sbgayhub/golem/sdk/group"
	sdk "github.com/sbgayhub/golem/sdk/message"
)

// ability 消息能力实现
type ability struct {
	api api.MessageService
}

func init() {
	sdk.Instance = &ability{api: api.Get()}
}

// Send 发送消息（根据类型分发到对应 API）
func (a *ability) Send(msg *sdk.Message) (*sdk.SendMessageResponse, error) {
	receiver := msg.GetReceiver().GetUsername()
	switch msg.GetType() {
	case sdk.MessageType_MESSAGE_TYPE_TEXT:
		data := msg.GetText()
		content := data.GetContent()
		remind := strings.Join(data.GetReminds(), ",")
		_, err := a.api.SendText(receiver, content, remind)
		if err != nil {
			return nil, err
		}
	case sdk.MessageType_MESSAGE_TYPE_IMAGE:
		data := msg.GetImage()
		if data.GetMedia() == nil {
			break
		}
		_, err := a.api.SendImage(receiver, bytes.NewReader(nil))
		if err != nil {
			return nil, err
		}
	case sdk.MessageType_MESSAGE_TYPE_VOICE:
		data := msg.GetVoice()
		if data.GetMedia() == nil {
			break
		}
		_, err := a.api.SendVoice(receiver, bytes.NewReader(nil), int32(data.GetDuration()), 0)
		if err != nil {
			return nil, err
		}
	case sdk.MessageType_MESSAGE_TYPE_VIDEO:
		data := msg.GetVideo()
		if data.GetMedia() == nil {
			break
		}
		_, err := a.api.SendVideo(receiver, nil, bytes.NewReader(nil), data.GetDuration())
		if err != nil {
			return nil, err
		}
	case sdk.MessageType_MESSAGE_TYPE_EMOJI:
		data := msg.GetEmoji()
		if data.GetMedia() == nil {
			break
		}
		_, err := a.api.SendEmoji(receiver, data.GetMedia().GetMd5(), nil)
		if err != nil {
			return nil, err
		}
	case sdk.MessageType_MESSAGE_TYPE_LOCATION:
		data := msg.GetLocation()
		_, err := a.api.SendPosition(receiver, data.GetLabel(), data.GetPoiName(), 0, 0, 0)
		if err != nil {
			return nil, err
		}
	case sdk.MessageType_MESSAGE_TYPE_APP:
		data := msg.GetApp()
		_, err := a.api.SendApp(receiver, data.GetXml(), int32(data.GetSubType()))
		if err != nil {
			return nil, err
		}
	default:
		slog.Debug("发送消息", "content", msg.Content)
	}
	return &sdk.SendMessageResponse{}, nil
}

// Forward 转发消息（根据类型调用对应转发 API）
func (a *ability) Forward(msg *sdk.Message, receiver string) (*sdk.SendMessageResponse, error) {
	switch msg.GetType() {
	case sdk.MessageType_MESSAGE_TYPE_IMAGE:
		data := msg.GetImage()
		if data.GetMedia() != nil {
			_, err := a.api.ForwardImage(receiver, bytes.NewReader(nil))
			if err != nil {
				return nil, err
			}
		}
	case sdk.MessageType_MESSAGE_TYPE_VIDEO:
		data := msg.GetVideo()
		if data.GetMedia() != nil {
			_, err := a.api.ForwardVideo(receiver, bytes.NewReader(nil))
			if err != nil {
				return nil, err
			}
		}
	case sdk.MessageType_MESSAGE_TYPE_APP:
		data := msg.GetApp()
		_, err := a.api.ForwardFile(receiver, data.GetXml())
		if err != nil {
			return nil, err
		}
	default:
		// 文本等类型直接用 Send 转发
		msg.Receiver = &contact.Contact{Username: receiver}
		return a.Send(msg)
	}
	return &sdk.SendMessageResponse{}, nil
}

// Revoke 撤回消息
func (a *ability) Revoke(receiver string, newMsgId uint64) (*sdk.RevokeMessageResponse, error) {
	_, err := a.api.Revoke(receiver, newMsgId, 0, 0)
	if err != nil {
		return nil, err
	}
	return &sdk.RevokeMessageResponse{Code: 0}, nil
}

// Download 下载媒体资源
func (a *ability) Download(msg *sdk.Message) (io.ReadCloser, error) {
	receiver := msg.GetReceiver().GetUsername()
	switch msg.GetType() {
	case sdk.MessageType_MESSAGE_TYPE_IMAGE:
		data := msg.GetImage()
		if data.GetMedia() == nil {
			return io.NopCloser(bytes.NewReader(nil)), nil
		}
		resp, err := a.api.DownloadImg(receiver, data.GetMedia().GetMd5(), data.GetMedia().GetKey(), data.GetMedia().GetSize())
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(resp.GetData())), nil
	case sdk.MessageType_MESSAGE_TYPE_VIDEO:
		data := msg.GetVideo()
		if data.GetMedia() == nil {
			return io.NopCloser(bytes.NewReader(nil)), nil
		}
		resp, err := a.api.DownloadVideo(receiver, data.GetMedia().GetMd5(), data.GetMedia().GetKey(), data.GetMedia().GetSize())
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(resp.GetData())), nil
	case sdk.MessageType_MESSAGE_TYPE_VOICE:
		data := msg.GetVoice()
		if data.GetMedia() == nil {
			return io.NopCloser(bytes.NewReader(nil)), nil
		}
		resp, err := a.api.DownloadVoice(receiver, data.GetMedia().GetMd5(), data.GetMedia().GetKey(), data.GetMedia().GetSize(), data.GetDuration())
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(resp.GetData())), nil
	}
	return nil, io.ErrUnexpectedEOF
}

// Build 从协议层 NewMessage 构建 SDK Message（由 host 同步调用）
func Build(msg *messagepb.NewMessage, contactCache *contactCacheFunc) *sdk.Message {
	marshal, _ := json.Marshal(msg)

	sender := resolveContact(contactCache, msg.GetSender().GetValue())
	receiver := resolveContact(contactCache, msg.GetReceiver().GetValue())

	content := msg.GetContent().GetValue()
	msgType := sdk.MessageType(msg.GetType())

	// 群消息解析 member
	var member *group.GroupMember
	if strings.Contains(content, ":\n") && msgType != sdk.MessageType_MESSAGE_TYPE_SYSTEM_TIP {
		parts := strings.SplitN(content, ":\n", 2)
		memberName := parts[0]
		content = parts[1]
		member = &group.GroupMember{Username: memberName}
	}

	result := &sdk.Message{
		Id:        msg.GetNewId(),
		Type:      msgType,
		Sender:    sender,
		Receiver:  receiver,
		Member:    member,
		Content:   content,
		Raw:       string(marshal),
		Timestamp: msg.GetCreateTime(),
	}

	switch msgType {
	case sdk.MessageType_MESSAGE_TYPE_TEXT:
		buildText(result, msg)
	case sdk.MessageType_MESSAGE_TYPE_IMAGE:
		buildImage(result, msg)
	case sdk.MessageType_MESSAGE_TYPE_VOICE:
		buildVoice(result, msg)
	case sdk.MessageType_MESSAGE_TYPE_VIDEO:
		buildVideo(result, msg)
	case sdk.MessageType_MESSAGE_TYPE_EMOJI:
		buildEmoji(result, msg)
	case sdk.MessageType_MESSAGE_TYPE_LOCATION:
		buildLocation(result, msg)
	case sdk.MessageType_MESSAGE_TYPE_APP:
		buildApp(result, msg)
	}

	return result
}

// contactCacheFunc 联系人缓存查询函数类型
type contactCacheFunc func(wxid string) *contact.Contact

// resolveContact 从缓存获取联系人，未命中则返回基础信息
func resolveContact(cache *contactCacheFunc, wxid string) *contact.Contact {
	if cache != nil && *cache != nil {
		if c := (*cache)(wxid); c != nil {
			return c
		}
	}
	return &contact.Contact{Username: wxid}
}

func buildText(msg *sdk.Message, raw *messagepb.NewMessage) {
	var t struct {
		_       xml.Name `xml:"msgsource"`
		Reminds string   `xml:"atuserlist"`
	}
	xml.Unmarshal([]byte(raw.GetSource()), &t)
	reminds := []string{}
	if t.Reminds != "" {
		reminds = strings.Split(t.Reminds, ",")
	}
	msg.Data = &sdk.Message_Text{Text: &sdk.TextData{
		Content: msg.Content,
		Reminds: reminds,
	}}
}

func buildImage(msg *sdk.Message, raw *messagepb.NewMessage) {
	var temp struct {
		Msg   xml.Name `xml:"msg"`
		Image struct {
			Md5    string `xml:"md5,attr"`
			Key    string `xml:"aeskey,attr"`
			Url    string `xml:"url,attr"`
			Size   uint32 `xml:"length,attr"`
			Width  uint32 `xml:"cdnthumbwidth,attr"`
			Height uint32 `xml:"cdnthumbheight,attr"`
		} `xml:"img"`
	}
	if err := xml.Unmarshal([]byte(raw.GetContent().GetValue()), &temp); err != nil {
		slog.Warn("parse image xml failed", "err", err)
		return
	}
	msg.Content = formatSize(temp.Image.Width, temp.Image.Height, temp.Image.Size)
	msg.Data = &sdk.Message_Image{Image: &sdk.ImageData{
		Media:  &sdk.Media{Md5: temp.Image.Md5, Key: temp.Image.Key, Url: temp.Image.Url, Size: temp.Image.Size},
		Width:  temp.Image.Width,
		Height: temp.Image.Height,
	}}
}

func buildVoice(msg *sdk.Message, raw *messagepb.NewMessage) {
	var temp struct {
		_     xml.Name `xml:"msg"`
		Voice struct {
			Key      string `xml:"aeskey,attr"`
			Url      string `xml:"voiceurl,attr"`
			Size     uint32 `xml:"length,attr"`
			Duration uint32 `xml:"voicelength,attr"`
		} `xml:"voicemsg"`
	}
	if err := xml.Unmarshal([]byte(raw.GetContent().GetValue()), &temp); err != nil {
		slog.Warn("parse voice xml failed", "err", err)
		return
	}
	msg.Data = &sdk.Message_Voice{Voice: &sdk.VoiceData{
		Media:    &sdk.Media{Key: temp.Voice.Key, Url: temp.Voice.Url, Size: temp.Voice.Size},
		Duration: temp.Voice.Duration,
	}}
}

func buildVideo(msg *sdk.Message, raw *messagepb.NewMessage) {
	var temp struct {
		_     xml.Name `xml:"msg"`
		Video struct {
			Size     uint32 `xml:"length,attr"`
			Duration uint32 `xml:"playlength,attr"`
			Md5      string `xml:"md5,attr"`
			NewMd5   string `xml:"newmd5,attr"`
			Key      string `xml:"aeskey,attr"`
			Url      string `xml:"cdnvideourl,attr"`
			ThumbUrl string `xml:"cdnthumburl,attr"`
		} `xml:"videomsg"`
	}
	if err := xml.Unmarshal([]byte(raw.GetContent().GetValue()), &temp); err != nil {
		slog.Warn("parse video xml failed", "err", err)
		return
	}
	msg.Data = &sdk.Message_Video{Video: &sdk.VideoData{
		Media:    &sdk.Media{Md5: temp.Video.Md5, Key: temp.Video.Key, Url: temp.Video.Url, Size: temp.Video.Size},
		Duration: temp.Video.Duration,
		ThumbUrl: temp.Video.ThumbUrl,
		NewMd5:   temp.Video.NewMd5,
	}}
}

func buildEmoji(msg *sdk.Message, raw *messagepb.NewMessage) {
	var temp struct {
		Msg   xml.Name `xml:"msg"`
		Emoji struct {
			Md5  string `xml:"md5,attr"`
			Desc string `xml:"desc,attr"`
			Key  string `xml:"aeskey,attr"`
			Url  string `xml:"cdnurl,attr"`
		} `xml:"emoji"`
	}
	if err := xml.Unmarshal([]byte(raw.GetContent().GetValue()), &temp); err != nil {
		slog.Warn("parse emoji xml failed", "err", err)
		return
	}
	msg.Content = temp.Emoji.Md5
	msg.Data = &sdk.Message_Emoji{Emoji: &sdk.EmojiData{
		Media: &sdk.Media{Md5: temp.Emoji.Md5, Key: temp.Emoji.Key, Url: temp.Emoji.Url},
		Desc:  temp.Emoji.Desc,
	}}
}

func buildLocation(msg *sdk.Message, raw *messagepb.NewMessage) {
	var temp struct {
		_        xml.Name `xml:"msg"`
		Location struct {
			Latitude  string `xml:"latitude"`
			Longitude string `xml:"longitude"`
			Scale     string `xml:"scale"`
			PoiName   string `xml:"poiname"`
			Label     string `xml:"label"`
		} `xml:"location"`
	}
	if err := xml.Unmarshal([]byte(raw.GetContent().GetValue()), &temp); err != nil {
		slog.Warn("parse location xml failed", "err", err)
		return
	}
	msg.Content = temp.Location.PoiName
	msg.Data = &sdk.Message_Location{Location: &sdk.LocationData{
		Latitude:  temp.Location.Latitude,
		Longitude: temp.Location.Longitude,
		Scale:     temp.Location.Scale,
		PoiName:   temp.Location.PoiName,
		Label:     temp.Location.Label,
	}}
}

func buildApp(msg *sdk.Message, raw *messagepb.NewMessage) {
	var temp struct {
		_     xml.Name `xml:"msg"`
		Title string   `xml:"appmsg>title"`
		Type  uint32   `xml:"appmsg>type"`
		Url   string   `xml:"appmsg>url"`
		Desc  string   `xml:"appmsg>des"`
	}
	if err := xml.Unmarshal([]byte(raw.GetContent().GetValue()), &temp); err != nil {
		slog.Warn("parse app xml failed", "err", err)
		return
	}
	msg.Content = temp.Title
	msg.Data = &sdk.Message_App{App: &sdk.AppData{
		SubType: temp.Type,
		Title:   temp.Title,
		Desc:    temp.Desc,
		Url:     temp.Url,
		Xml:     raw.GetContent().GetValue(),
	}}
}

func formatSize(width, height, size uint32) string {
	return string(rune(width)) + "x" + string(rune(height))
}
