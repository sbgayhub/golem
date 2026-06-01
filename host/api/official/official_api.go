// Package officialapi 提供公众号服务的 API 接口定义。
package officialapi

import (
	baseapi "github.com/sbgayhub/golem/host/api/base"
	contactapi "github.com/sbgayhub/golem/host/api/contact"
)

// OfficialService 公众号服务 API 接口（返回 API proto 类型）。
type OfficialService interface {
	// Follow 关注公众号
	Follow(appid string) (*contactapi.VerifyUserResponse, error)
	// Quit 取关公众号
	Quit(appid string) (*baseapi.OperateResponse, error)
	// MpGetA8Key 获取公众号 A8Key
	MpGetA8Key(url string) (*GetA8KeyResponse, error)
	// JSAPIPreVerify JSAPI 预验证
	JSAPIPreVerify(url, appid string, jsapiList []string) (*JSAPIPreVerifyResponse, error)
	// OauthAuthorize OAuth 授权
	OauthAuthorize(url, appid string) (*OauthAuthorizeResponse, error)
	// ReadArticle 阅读公众号文章
	ReadArticle(url string) (*ArticleResult, error)
	// LikeArticle 点赞公众号文章
	LikeArticle(url string) (*ArticleResult, error)
}
