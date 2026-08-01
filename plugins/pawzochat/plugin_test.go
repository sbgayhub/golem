package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sbgayhub/golem/sdk/chatroom"
	"github.com/sbgayhub/golem/sdk/contact"
	"github.com/sbgayhub/golem/sdk/message"
	"github.com/sbgayhub/golem/sdk/plugin"
)

type recordingMessageAbility struct {
	messages []*message.Message
}

func (ability *recordingMessageAbility) Send(msg *message.Message) (*message.Send_Response, error) {
	ability.messages = append(ability.messages, msg)
	return &message.Send_Response{NewId: uint64(len(ability.messages))}, nil
}

func (*recordingMessageAbility) Forward(*message.Message, string) (*message.Forward_Response, error) {
	return &message.Forward_Response{}, nil
}

func (*recordingMessageAbility) Revoke(string, uint64) (*message.Revoke_Response, error) {
	return &message.Revoke_Response{}, nil
}

func (*recordingMessageAbility) Download(*message.Message) (io.ReadCloser, error) {
	return io.NopCloser(nil), nil
}

func TestOnEventRoutesPrivateReplyToOriginalSender(t *testing.T) {
	requests := make(chan bridgeRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request bridgeRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("Decode: %v", err)
		}
		requests <- request
		_, _ = w.Write([]byte(`{"messages":[{"content":[{"type":"text","text":"reply"}]}]}`))
	}))
	defer server.Close()

	recorder := &recordingMessageAbility{}
	pawzo := newPawzoChatPlugin()
	pawzo.message = recorder
	pawzo.self = &contact.SelfInfo{Username: "wxid_self", Nickname: "Bot"}
	pawzo.ownerID = "wxid_owner"
	pawzo.ownerName = "Owner"
	pawzo.Config = normalizeConfigValue(Config{
		BaseURL:            server.URL,
		HTTPTimeoutSeconds: 2,
		Routes:             map[string]string{"private:wxid_friend": "persona"},
	})
	sender := &contact.Contact{
		Username: "wxid_friend", Nickname: "Friend",
		Type: contact.ContactType_CONTACT_TYPE_FRIEND,
	}
	event := &plugin.Event{Payload: &plugin.Event_Message{Message: &message.Message{
		Type: message.TypeText, Sender: sender,
		Data: &message.Message_Text{Text: &message.TextData{Content: "hello"}},
	}}}
	handled, err := pawzo.OnEvent(event)
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	request := <-requests
	if request.PersonaID != "persona" {
		t.Fatalf("request=%#v", request)
	}
	if request.SessionName != "Friend" {
		t.Fatalf("session name=%q", request.SessionName)
	}
	var senderText struct {
		Text string `json:"text"`
	}
	decodeEnvelopeSection(t, request.Text, "untrusted_message_from_sender_json", &senderText)
	if senderText.Text != "hello" {
		t.Fatalf("sender text=%q", senderText.Text)
	}
	if len(recorder.messages) != 1 || recorder.messages[0].GetReceiver().GetUsername() != "wxid_friend" ||
		recorder.messages[0].GetText().GetContent() != "reply" {
		t.Fatalf("messages=%#v", recorder.messages)
	}
}

func TestOnEventHandlesAmbientGroupWhenConfigured(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"messages":[{"content":[{"type":"text","text":"ambient reply"}]}]}`))
	}))
	defer server.Close()

	recorder := &recordingMessageAbility{}
	pawzo := newPawzoChatPlugin()
	pawzo.message = recorder
	pawzo.self = &contact.SelfInfo{Username: "wxid_self", Nickname: "Bot"}
	pawzo.ownerID = "wxid_owner"
	pawzo.ownerName = "Owner"
	pawzo.Config = normalizeConfigValue(Config{
		BaseURL:                   server.URL,
		DefaultPersonaID:          "persona",
		RespondToAllGroupMessages: true,
		HTTPTimeoutSeconds:        2,
	})
	event := &plugin.Event{Payload: &plugin.Event_Message{Message: groupTextMessage(
		"ambient message", nil, "wxid_member", "Member",
	)}}

	handled, err := pawzo.OnEvent(event)
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if len(recorder.messages) != 1 || recorder.messages[0].GetText().GetContent() != "ambient reply" {
		t.Fatalf("messages=%#v", recorder.messages)
	}
}

func TestOnEventRejectsMissingVerifiedIdentity(t *testing.T) {
	pawzo := newPawzoChatPlugin()
	pawzo.Config = normalizeConfigValue(Config{DefaultPersonaID: "persona"})
	event := &plugin.Event{Payload: &plugin.Event_Message{Message: &message.Message{
		Type: message.TypeText,
		Sender: &contact.Contact{
			Username: "wxid_friend",
			Type:     contact.ContactType_CONTACT_TYPE_FRIEND,
		},
		Data: &message.Message_Text{Text: &message.TextData{Content: "hello"}},
	}}}

	handled, err := pawzo.OnEvent(event)
	if !handled || err == nil || err.Error() != "golem self identity is unavailable" {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
}

func TestOnEventIgnoresUnmentionedGroupMessage(t *testing.T) {
	pawzo := newPawzoChatPlugin()
	pawzo.self = &contact.SelfInfo{Username: "wxid_bot", Nickname: "Bot"}
	pawzo.ownerID = "wxid_owner"
	pawzo.Config = normalizeConfigValue(Config{
		Routes: map[string]string{"chatroom:room@chatroom": "persona"},
	})
	room := &contact.Contact{
		Username: "room@chatroom", Type: contact.ContactType_CONTACT_TYPE_CHATROOM,
	}
	event := &plugin.Event{Payload: &plugin.Event_Message{Message: &message.Message{
		Type: message.TypeText, Sender: room,
		Member: &chatroom.Member{Username: "wxid_member", DisplayName: "Member"},
		Data:   &message.Message_Text{Text: &message.TextData{Content: "hello"}},
	}}}
	handled, err := pawzo.OnEvent(event)
	if err != nil || handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
}

func TestSendOutputUsesNativeEmojiMessage(t *testing.T) {
	recorder := &recordingMessageAbility{}
	pawzo := newPawzoChatPlugin()
	pawzo.message = recorder
	receiver := &contact.Contact{Username: "wxid_friend"}

	if err := pawzo.sendOutput(receiver, outbound{
		Kind: "emoji", Text: "happy", Data: []byte("emoji"),
	}); err != nil {
		t.Fatalf("sendOutput: %v", err)
	}
	if len(recorder.messages) != 1 {
		t.Fatalf("messages=%#v", recorder.messages)
	}
	msg := recorder.messages[0]
	if msg.GetType() != message.TypeEmoji || string(msg.GetEmoji().GetMedia().GetData()) != "emoji" {
		t.Fatalf("message=%#v", msg)
	}
}
