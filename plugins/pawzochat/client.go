package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxBridgeResponseBytes = 40 * 1024 * 1024

type bridgeRequest struct {
	PersonaID   string `json:"persona_id"`
	SessionKey  string `json:"session_key"`
	SessionName string `json:"session_name,omitempty"`
	Text        string `json:"text"`
	Quote       string `json:"quote,omitempty"`
}

type bridgeResponse struct {
	Messages []bridgeMessage `json:"messages"`
	Error    string          `json:"error"`
}

type bridgeMessage struct {
	Content []bridgeBlock `json:"content"`
}

type bridgeBlock struct {
	Type       string `json:"type"`
	Text       string `json:"text"`
	Data       string `json:"data"`
	Name       string `json:"name"`
	DurationMS uint32 `json:"duration_ms"`
}

type outbound struct {
	Kind       string
	Text       string
	Data       []byte
	DurationMS uint32
}

func (p *PawzoChatPlugin) requestReply(
	config Config,
	personaID string,
	incoming incomingMessage,
) ([]outbound, error) {
	payload, err := json.Marshal(bridgeRequest{
		PersonaID:   personaID,
		SessionKey:  incoming.SessionKey,
		SessionName: incoming.sessionName(),
		Text:        incoming.promptContent(),
		Quote:       incoming.Quote.Content,
	})
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(config.BaseURL, "/") + "/api/bridge/golem/messages"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build PawzoChat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if config.Token != "" {
		req.Header.Set("Authorization", "Bearer "+config.Token)
	}
	client := &http.Client{Timeout: time.Duration(config.HTTPTimeoutSeconds) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call PawzoChat: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBridgeResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read PawzoChat response: %w", err)
	}
	if len(body) > maxBridgeResponseBytes {
		return nil, errors.New("PawzoChat response exceeds 40 MiB")
	}
	var decoded bridgeResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("decode PawzoChat response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if decoded.Error == "" {
			decoded.Error = resp.Status
		}
		return nil, fmt.Errorf("PawzoChat request failed: %s", decoded.Error)
	}
	return decoded.outputs()
}

func (response bridgeResponse) outputs() ([]outbound, error) {
	var outputs []outbound
	for _, item := range response.Messages {
		for _, block := range item.Content {
			kind := strings.ToLower(strings.TrimSpace(block.Type))
			switch kind {
			case "text":
				if text := strings.TrimSpace(block.Text); text != "" {
					outputs = append(outputs, outbound{Kind: "text", Text: text})
				}
			case "image", "emoji", "voice":
				if block.Data == "" {
					placeholder := map[string]string{
						"image": "[图片]", "emoji": "[表情]", "voice": "[语音]",
					}[kind]
					outputs = append(outputs, outbound{Kind: "text", Text: placeholder})
					continue
				}
				data, err := base64.StdEncoding.DecodeString(block.Data)
				if err != nil {
					return nil, fmt.Errorf("decode PawzoChat %s: %w", kind, err)
				}
				outputs = append(outputs, outbound{
					Kind: kind, Text: strings.TrimSpace(block.Text), Data: data,
					DurationMS: block.DurationMS,
				})
			case "file":
				name := strings.TrimSpace(block.Name)
				if name == "" {
					name = "文件"
				}
				outputs = append(outputs, outbound{Kind: "text", Text: "[文件] " + name})
			}
		}
	}
	return outputs, nil
}
