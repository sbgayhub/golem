//go:build !lib

// Package officialapi 提供公众号服务的 web 实现（通过 HTTP 调用远程服务）。
package officialapi

import (
	"fmt"
	"strings"
	"sync"

	"github.com/sbgayhub/golem/host/api"
	baseapi "github.com/sbgayhub/golem/host/api/base"
	contactapi "github.com/sbgayhub/golem/host/api/contact"
)

// web 公众号服务 web 实现（通过 HTTP 调用远程服务）。
type web struct{}

// Get 获取 OfficialService 单例（web 模式）。
var Get = sync.OnceValue(func() OfficialService {
	return &web{}
})

// Follow 关注公众号
func (w web) Follow(appid string) (*contactapi.VerifyUserResponse, error) {
	var resp contactapi.VerifyUserResponse
	if err := api.GetHttp().Post(fmt.Sprintf("/api/official/%s/follow", appid)).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Quit 取关公众号
func (w web) Quit(appid string) (*baseapi.OperateResponse, error) {
	var resp baseapi.OperateResponse
	if err := api.GetHttp().Delete(fmt.Sprintf("/api/official/%s", appid)).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// MpGetA8Key 获取公众号 A8Key
func (w web) MpGetA8Key(url string) (*GetA8KeyResponse, error) {
	var resp GetA8KeyResponse
	if err := api.GetHttp().Post("/api/official/a8key").Query("url", url).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// JSAPIPreVerify JSAPI 预验证
func (w web) JSAPIPreVerify(url, appid string, jsapiList []string) (*JSAPIPreVerifyResponse, error) {
	var resp JSAPIPreVerifyResponse
	if err := api.GetHttp().Post("/api/official/jsapi").
		Query("url", url, "appid", appid, "jsapi_list", strings.Join(jsapiList, ",")).
		DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// OauthAuthorize OAuth 授权
func (w web) OauthAuthorize(url, appid string) (*OauthAuthorizeResponse, error) {
	var resp OauthAuthorizeResponse
	if err := api.GetHttp().Post("/api/official/oauth").
		Query("url", url, "appid", appid).
		DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ReadArticle 阅读公众号文章
func (w web) ReadArticle(url string) (*ArticleResult, error) {
	var resp articleResultDTO
	if err := api.GetHttp().Post("/api/official/article/read").Query("url", url).DoJson(&resp); err != nil {
		return nil, err
	}
	return mapArticleResult(resp), nil
}

// LikeArticle 点赞公众号文章
func (w web) LikeArticle(url string) (*ArticleResult, error) {
	var resp articleResultDTO
	if err := api.GetHttp().Post("/api/official/article/like").Query("url", url).DoJson(&resp); err != nil {
		return nil, err
	}
	return mapArticleResult(resp), nil
}

type articleResultDTO struct {
	ResponseBody string            `json:"ResponseBody"`
	Cookies      string            `json:"Cookies"`
	Headers      map[string]string `json:"Headers"`
}

func mapArticleResult(result articleResultDTO) *ArticleResult {
	headers := make(map[string]string, len(result.Headers))
	for key, value := range result.Headers {
		headers[key] = value
	}
	return &ArticleResult{
		ResponseBody: result.ResponseBody,
		Cookies:      result.Cookies,
		Headers:      headers,
	}
}
