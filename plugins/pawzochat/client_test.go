package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestReplyUsesBridgeProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/bridge/golem/messages" || r.Method != http.MethodPost {
			t.Errorf("request=%s %s", r.Method, r.URL.Path)
		}
		if actual := r.Header.Get("Authorization"); actual != "Bearer token" {
			t.Errorf("Authorization=%q", actual)
		}
		var request bridgeRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("Decode request: %v", err)
		}
		if request.PersonaID != "persona" || request.SessionKey != "private:wxid" ||
			request.SessionName != "好友昵称" {
			t.Errorf("request=%#v", request)
		}
		var sender struct {
			Text string `json:"text"`
		}
		decodeEnvelopeSection(t, request.Text, "untrusted_message_from_sender_json", &sender)
		if sender.Text != "hello" {
			t.Errorf("sender text=%q", sender.Text)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"messages":[{"content":[{"type":"text","text":"reply"},{"type":"image","data":"cG5n"},{"type":"emoji","data":"ZW1vamk="}]}]}`))
	}))
	defer server.Close()

	plugin := newPawzoChatPlugin()
	outputs, err := plugin.requestReply(Config{
		BaseURL: server.URL, Token: "token", HTTPTimeoutSeconds: 2,
	}, "persona", incomingMessage{
		SessionKey: "private:wxid", SpeakerName: "好友昵称", Text: "hello",
	})
	if err != nil {
		t.Fatalf("requestReply: %v", err)
	}
	if len(outputs) != 3 || outputs[0].Kind != "text" || outputs[0].Text != "reply" {
		t.Fatalf("outputs=%#v", outputs)
	}
	if outputs[1].Kind != "image" || string(outputs[1].Data) != "png" {
		t.Fatalf("image output=%#v", outputs[1])
	}
	if outputs[2].Kind != "emoji" || string(outputs[2].Data) != "emoji" {
		t.Fatalf("emoji output=%#v", outputs[2])
	}
}

func TestRequestReplyReturnsBridgeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"Persona not found"}`))
	}))
	defer server.Close()

	plugin := newPawzoChatPlugin()
	_, err := plugin.requestReply(Config{
		BaseURL: server.URL, HTTPTimeoutSeconds: 2,
	}, "missing", incomingMessage{SessionKey: "private:wxid", Text: "hello"})
	if err == nil || err.Error() != "PawzoChat request failed: Persona not found" {
		t.Fatalf("error=%v", err)
	}
}

func TestNormalizeConfigAndRoute(t *testing.T) {
	config := normalizeConfigValue(Config{
		BaseURL: " http://localhost:62000/ ",
		Routes:  map[string]string{" private:wxid ": " persona ", "bad": ""},
	})
	plugin := newPawzoChatPlugin()
	if config.BaseURL != "http://localhost:62000" || config.HTTPTimeoutSeconds != 50 {
		t.Fatalf("config=%#v", config)
	}
	if actual := plugin.personaForSession(config, "private:wxid"); actual != "persona" {
		t.Fatalf("persona=%q", actual)
	}
}

func TestMissingMediaBecomesVisiblePlaceholder(t *testing.T) {
	outputs, err := (bridgeResponse{Messages: []bridgeMessage{{
		Content: []bridgeBlock{{Type: "image"}, {Type: "voice"}},
	}}}).outputs()
	if err != nil {
		t.Fatalf("outputs: %v", err)
	}
	if len(outputs) != 2 || outputs[0].Text != "[图片]" || outputs[1].Text != "[语音]" {
		t.Fatalf("outputs=%#v", outputs)
	}
}
