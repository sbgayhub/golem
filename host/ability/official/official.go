// Package officialability 提供公众号能力的实现（直连型）。
package officialability

import (
	sdk "github.com/sbgayhub/golem/sdk/official"

	baseapi "github.com/sbgayhub/golem/host/api/base"
	contactapi "github.com/sbgayhub/golem/host/api/contact"
	officialapi "github.com/sbgayhub/golem/host/api/official"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// ability 公众号能力实现（直连型）。
type ability struct {
	api officialapi.OfficialService
}

func init() {
	sdk.Instance = &ability{api: officialapi.Get()}
}

// Follow 关注公众号
func (a ability) Follow(appid string) (*sdk.Follow_Response, error) {
	resp, err := a.api.Follow(appid)
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.Follow_Response{
		BaseResult: mapContactBaseResult(resp),
		Username:   resp.GetUsername(),
	}, nil
}

// Quit 取关公众号
func (a ability) Quit(appid string) (*sdk.Quit_Response, error) {
	resp, err := a.api.Quit(appid)
	if resp == nil || err != nil {
		return nil, err
	}
	result := resp.GetResult()
	return &sdk.Quit_Response{
		Code:       resp.GetCode(),
		Count:      result.GetCount(),
		ResultCode: result.GetCode(),
		Message:    result.GetMessage(),
	}, nil
}

// MpGetA8Key 获取公众号 A8Key
func (a ability) MpGetA8Key(url string) (*sdk.MpGetA8Key_Response, error) {
	resp, err := a.api.MpGetA8Key(url)
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.MpGetA8Key_Response{
		BaseResult:        mapBaseResult(resp.GetBaseResponse()),
		FullUrl:           resp.GetFullUrl(),
		A8Key:             resp.GetA8Key(),
		ActionCode:        resp.GetActionCode(),
		Title:             resp.GetTitle(),
		Content:           resp.GetContent(),
		JsapiPermission:   mapJSAPIPermission(resp.GetJsapiPermission()),
		GeneralControl:    resp.GetGeneralControl().GetValue(),
		UserName:          resp.GetUserName(),
		ShareUrl:          resp.GetShareUrl(),
		ScopeCount:        resp.GetScopeCount(),
		ScopeList:         mapScopeList(resp.GetScopeList()),
		AntispamTicket:    resp.GetAntispamTicket(),
		Ssid:              resp.GetSsid(),
		Mid:               resp.GetMid(),
		DeepLink:          resp.GetDeepLink().GetValue(),
		JsapiControlBytes: mapBuffer(resp.GetJsapiControlBytes()),
		HttpHeaderCount:   resp.GetHttpHeaderCount(),
		HttpHeaderList:    mapHTTPHeaders(resp.GetHttpHeaderList()),
		Wording:           resp.GetWording(),
		Avatar:            resp.GetAvatar(),
		Cookie:            mapBuffer(resp.GetCookie()),
		MenuWording:       resp.GetMenuWording(),
	}, nil
}

// JSAPIPreVerify JSAPI 预验证
func (a ability) JSAPIPreVerify(url, appid string, jsapiList []string) (*sdk.JSAPIPreVerify_Response, error) {
	resp, err := a.api.JSAPIPreVerify(url, appid, jsapiList)
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.JSAPIPreVerify_Response{
		BaseResult:     mapBaseResult(resp.GetBaseResponse()),
		JsapiResult:    mapJSAPIResult(resp.GetJsapiResult()),
		VerifyInfoList: resp.GetVerifyInfoList(),
		DomainPathList: resp.GetDomainPathList(),
		AppAvatarUrl:   resp.GetAppAvatarUrl(),
		AppNickname:    resp.GetAppNickname(),
	}, nil
}

// OauthAuthorize OAuth 授权
func (a ability) OauthAuthorize(url, appid string) (*sdk.OauthAuthorize_Response, error) {
	resp, err := a.api.OauthAuthorize(url, appid)
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.OauthAuthorize_Response{
		BaseResult:              mapBaseResult(resp.GetBaseResponse()),
		ScopeList:               resp.GetScopeList(),
		AppName:                 resp.GetAppName(),
		AppIconUrl:              resp.GetAppIconUrl(),
		RedirectUrl:             resp.GetRedirectUrl(),
		IsRecentHasAuth:         resp.GetIsRecentHasAuth(),
		IsSilentAuth:            resp.GetIsSilentAuth(),
		IsCallServerWhenConfirm: resp.GetIsCallServerWhenConfirm(),
	}, nil
}

// ReadArticle 阅读公众号文章
func (a ability) ReadArticle(url string) (*sdk.ReadArticle_Response, error) {
	resp, err := a.api.ReadArticle(url)
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.ReadArticle_Response{Result: mapArticleResult(resp)}, nil
}

// LikeArticle 点赞公众号文章
func (a ability) LikeArticle(url string) (*sdk.LikeArticle_Response, error) {
	resp, err := a.api.LikeArticle(url)
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.LikeArticle_Response{Result: mapArticleResult(resp)}, nil
}

func mapContactBaseResult(resp *contactapi.VerifyUserResponse) *sdk.BaseResult {
	if resp == nil {
		return nil
	}
	return mapBaseResult(resp.GetBaseResponse())
}

func mapBaseResult(resp *baseapi.BaseResponse) *sdk.BaseResult {
	if resp == nil {
		return nil
	}
	return &sdk.BaseResult{
		Code:    resp.GetCode(),
		Message: resp.GetMessage().GetValue(),
	}
}

func mapBuffer(buffer *baseapi.Buffer) *sdk.Buffer {
	if buffer == nil {
		return nil
	}
	return &sdk.Buffer{
		Size: buffer.GetSize(),
		Data: buffer.GetData(),
	}
}

func mapJSAPIPermission(permission *officialapi.GetA8KeyResponse_JSAPIPermissionBitSet) *sdk.JSAPIPermissionBitSet {
	if permission == nil {
		return nil
	}
	return &sdk.JSAPIPermissionBitSet{
		Value1: permission.GetValue1(),
		Value2: permission.GetValue2(),
		Value3: permission.GetValue3(),
		Value4: permission.GetValue4(),
	}
}

func mapScope(scope *officialapi.GetA8KeyResponse_BizScopeInfo) *sdk.BizScopeInfo {
	if scope == nil {
		return nil
	}
	return &sdk.BizScopeInfo{
		Scope:            scope.GetScope(),
		ScopeStatus:      scope.GetScopeStatus(),
		ScopeDescription: scope.GetScopeDescription(),
		ApiCount:         scope.GetApiCount(),
		ApiList:          mapStringValues(scope.GetApiList()),
	}
}

func mapScopeList(scopes []*officialapi.GetA8KeyResponse_BizScopeInfo) []*sdk.BizScopeInfo {
	result := make([]*sdk.BizScopeInfo, 0, len(scopes))
	for _, scope := range scopes {
		if scope == nil {
			continue
		}
		result = append(result, mapScope(scope))
	}
	return result
}

func mapStringValues(values []*wrapperspb.StringValue) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		result = append(result, value.GetValue())
	}
	return result
}

func mapHTTPHeader(header *officialapi.GetA8KeyResponse_HttpHeader) *sdk.HttpHeader {
	if header == nil {
		return nil
	}
	return &sdk.HttpHeader{
		Key:   header.GetKey(),
		Value: header.GetValue(),
	}
}

func mapHTTPHeaders(headers []*officialapi.GetA8KeyResponse_HttpHeader) []*sdk.HttpHeader {
	result := make([]*sdk.HttpHeader, 0, len(headers))
	for _, header := range headers {
		if header == nil {
			continue
		}
		result = append(result, mapHTTPHeader(header))
	}
	return result
}

func mapJSAPIResult(result *officialapi.JSAPIPreVerifyResponse_JSAPIResult) *sdk.JSAPIResult {
	if result == nil {
		return nil
	}
	return &sdk.JSAPIResult{
		Code:  result.GetCode(),
		Error: result.GetError(),
		Json:  result.GetJson(),
	}
}

func mapArticleResult(result *officialapi.ArticleResult) *sdk.ArticleResult {
	if result == nil {
		return nil
	}
	headers := make(map[string]string, len(result.GetHeaders()))
	for key, value := range result.GetHeaders() {
		headers[key] = value
	}
	return &sdk.ArticleResult{
		ResponseBody: result.GetResponseBody(),
		Cookies:      result.GetCookies(),
		Headers:      headers,
	}
}
