package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type chatCompletionRequest struct {
	Model    string          `json:"model"`
	Messages []openAIMessage `json:"messages"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message openAIMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

func (p *AiPlugin) chat(sessionKey string) (string, error) {
	config := p.configSnapshot()
	if config.BaseURL == "" {
		return "", errors.New("AI base_url 未配置")
	}
	if config.APIKey == "" {
		return "", errors.New("AI api_key 未配置")
	}
	if config.Model == "" {
		return "", errors.New("AI model 未配置")
	}

	messages := make([]openAIMessage, 0, p.maxContextMessages()+1)
	if prompt := activePromptContent(config); prompt != "" {
		messages = append(messages, openAIMessage{Role: "system", Content: p.getPreMadePrompts() + prompt})
	}
	messages = append(messages, p.contextMessages(sessionKey)...)
	if len(messages) == 0 {
		return "", errors.New("AI 上下文为空")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(config.HTTPTimeoutSeconds)*time.Second)
	defer cancel()
	return callOpenAI(ctx, http.DefaultClient, config.BaseURL, config.APIKey, chatCompletionRequest{
		Model:    config.Model,
		Messages: messages,
	})
}

func callOpenAI(ctx context.Context, client *http.Client, baseURL, apiKey string, payload chatCompletionRequest) (string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("序列化 AI 请求失败: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatCompletionURL(baseURL), bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("创建 AI 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36 Edg/141.0.0.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求 AI 接口失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取 AI 响应失败: %w", err)
	}
	var result chatCompletionResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析 AI 响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if result.Error != nil && result.Error.Message != "" {
			return "", fmt.Errorf("AI 接口返回错误: %s", result.Error.Message)
		}
		return "", fmt.Errorf("AI 接口返回状态码: %d", resp.StatusCode)
	}
	if result.Error != nil && result.Error.Message != "" {
		return "", fmt.Errorf("AI 接口返回错误: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return "", errors.New("AI 响应缺少 choices")
	}
	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}

func chatCompletionURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(baseURL, "/chat/completions") {
		return baseURL
	}
	return baseURL + "/chat/completions"
}
