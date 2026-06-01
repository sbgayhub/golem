//go:build lib

// Package officialapi 提供公众号服务的 lib 实现（直接调用底层实现）。
package officialapi

import (
	"sync"

	"golem/pkg/official"

	"github.com/sbgayhub/golem/host/api"
	baseapi "github.com/sbgayhub/golem/host/api/base"
	contactapi "github.com/sbgayhub/golem/host/api/contact"
)

// lib 公众号服务 lib 实现（直接调用底层实现）。
type lib struct{}

// Get 获取 OfficialService 单例（lib 模式）。
var Get = sync.OnceValue(func() OfficialService {
	return &lib{}
})

// Follow 关注公众号
func (l lib) Follow(appid string) (*contactapi.VerifyUserResponse, error) {
	resp, err := official.Follow(appid)
	if resp == nil || err != nil {
		return nil, err
	}
	var result contactapi.VerifyUserResponse
	if err := api.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Quit 取关公众号
func (l lib) Quit(appid string) (*baseapi.OperateResponse, error) {
	resp, err := official.Quit(appid)
	if resp == nil || err != nil {
		return nil, err
	}
	var result baseapi.OperateResponse
	if err := api.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// MpGetA8Key 获取公众号 A8Key
func (l lib) MpGetA8Key(url string) (*GetA8KeyResponse, error) {
	resp, err := official.MpGetA8Key(url)
	if resp == nil || err != nil {
		return nil, err
	}
	var result GetA8KeyResponse
	if err := api.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// JSAPIPreVerify JSAPI 预验证
func (l lib) JSAPIPreVerify(url, appid string, jsapiList []string) (*JSAPIPreVerifyResponse, error) {
	resp, err := official.JSAPIPreVerify(url, appid, jsapiList)
	if resp == nil || err != nil {
		return nil, err
	}
	var result JSAPIPreVerifyResponse
	if err := api.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// OauthAuthorize OAuth 授权
func (l lib) OauthAuthorize(url, appid string) (*OauthAuthorizeResponse, error) {
	resp, err := official.OauthAuthorize(url, appid)
	if resp == nil || err != nil {
		return nil, err
	}
	var result OauthAuthorizeResponse
	if err := api.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ReadArticle 阅读公众号文章
func (l lib) ReadArticle(url string) (*ArticleResult, error) {
	resp, err := official.ReadArticle(url)
	if resp == nil || err != nil {
		return nil, err
	}
	return mapArticleResult(resp.ResponseBody, resp.Cookies, resp.Headers), nil
}

// LikeArticle 点赞公众号文章
func (l lib) LikeArticle(url string) (*ArticleResult, error) {
	resp, err := official.LikeArticle(url)
	if resp == nil || err != nil {
		return nil, err
	}
	return mapArticleResult(resp.ResponseBody, resp.Cookies, resp.Headers), nil
}

func mapArticleResult(responseBody, cookies string, source map[string]string) *ArticleResult {
	headers := make(map[string]string, len(source))
	for key, value := range source {
		headers[key] = value
	}
	return &ArticleResult{
		ResponseBody: responseBody,
		Cookies:      cookies,
		Headers:      headers,
	}
}
