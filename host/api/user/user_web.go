//go:build !lib

// Package userapi 提供用户服务的 web 实现（通过 HTTP 调用远程服务）。
package userapi

import (
	"io"
	"strconv"
	"sync"

	"github.com/sbgayhub/golem/host/api"
)

// web 用户服务 web 实现（通过 HTTP 调用远程服务）。
type web struct{}

// Get 获取 UserService 单例（web 模式）。
var Get = sync.OnceValue(func() UserService {
	return &web{}
})

// GetProfile 获取个人信息
func (w web) GetProfile() (*GetProfileResponse, error) {
	var resp GetProfileResponse
	if err := api.GetHttp().Get("/api/user/profile").DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// UpdateProfile 更新个人信息
func (w web) UpdateProfile(params UpdateProfileParams) (*OperateResponse, error) {
	var resp OperateResponse
	if err := api.GetHttp().Put("/api/user/profile").Body(map[string]any{
		"nickname":  params.NickName,
		"signature": params.Signature,
		"sex":       params.Sex,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// UploadAvatar 上传头像
func (w web) UploadAvatar(reader io.Reader) (*UploadAvatarResponse, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	var resp UploadAvatarResponse
	if err := api.GetHttp().Post("/api/user/avatar").Multipart(map[string][]byte{
		"avatar": data,
	}, nil).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetQRCode 获取个人二维码
func (w web) GetQRCode(style int32) (*GetQRCodeResponse, error) {
	var resp GetQRCodeResponse
	if err := api.GetHttp().Get("/api/user/qrcode").Query("style", style).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SetPrivacy 设置隐私选项
func (w web) SetPrivacy(function, value int32) (*OperateResponse, error) {
	var resp OperateResponse
	if err := api.GetHttp().Put("/api/user/privacy").Body(map[string]any{
		"options": []map[string]any{{"type": function, "value": value}},
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SetAlias 设置微信号
func (w web) SetAlias(alias string) (*GeneralSetResponse, error) {
	var resp GeneralSetResponse
	if err := api.GetHttp().Put("/api/user/alias").Body(map[string]any{
		"alias": alias,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// VerifyPassword 验证密码
func (w web) VerifyPassword(password string) (*VerifyPasswordResponse, error) {
	var resp VerifyPasswordResponse
	if err := api.GetHttp().Post("/api/user/password/verify").Body(map[string]any{
		"password": password,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SetPassword 设置密码
func (w web) SetPassword(password, ticket string) (*SetPasswordResponse, error) {
	var resp SetPasswordResponse
	if err := api.GetHttp().Put("/api/user/password").Body(map[string]any{
		"new_password": password,
		"ticket":       ticket,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendVerifyMobile 发送手机验证码
func (w web) SendVerifyMobile(mobile string, opcode uint32) (*BindOpMobileResponse, error) {
	var resp BindOpMobileResponse
	if err := api.GetHttp().Post("/api/user/mobile/verify-code").Body(map[string]any{
		"mobile":  mobile,
		"operate": opcode,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BindMobile 绑定手机号
func (w web) BindMobile(mobile, verifyCode string) (*BindOpMobileResponse, error) {
	var resp BindOpMobileResponse
	if err := api.GetHttp().Post("/api/user/mobile").Body(map[string]any{
		"mobile":      mobile,
		"verify_code": verifyCode,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BindEmail 绑定邮箱
func (w web) BindEmail(email string) (*BindEmailResponse, error) {
	var resp BindEmailResponse
	if err := api.GetHttp().Post("/api/user/email").Body(map[string]any{
		"email": email,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendVerifyEmail 发送验证邮件
func (w web) SendVerifyEmail() (*SendVerifyEmailResponse, error) {
	var resp SendVerifyEmailResponse
	if err := api.GetHttp().Post("/api/user/email/verify").DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetSafetyInfo 获取安全设备列表
func (w web) GetSafetyInfo() (*GetSafetyInfoResponse, error) {
	var resp GetSafetyInfoResponse
	if err := api.GetHttp().Get("/api/user/safety/devices").DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DelSafeDevice 删除安全设备
func (w web) DelSafeDevice(uuid string) (*DeleteSafeDeviceResponse, error) {
	var resp DeleteSafeDeviceResponse
	if err := api.GetHttp().Delete("/api/user/safety/devices").Body(map[string]any{
		"uuid": uuid,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ReportMotion 上报运动步数
func (w web) ReportMotion(deviceID, deviceType string, stepCount int64) (*UploadDeviceStepResponse, error) {
	var resp UploadDeviceStepResponse
	if err := api.GetHttp().Post("/api/user/motion").Body(map[string]any{
		"device_id":   deviceID,
		"device_type": deviceType,
		"step_count":  stepCount,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetBoundHardDevices 获取绑定的硬件设备列表
func (w web) GetBoundHardDevices() (*GetBoundHardDevicesResponse, error) {
	var resp GetBoundHardDevicesResponse
	if err := api.GetHttp().Get("/api/user/devices").DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetCert 获取证书信息
func (w web) GetCert(currentVersion uint32) (*GetCertResponse, error) {
	var resp GetCertResponse
	if err := api.GetHttp().Get("/api/user/cert").
		Query("current_version", strconv.FormatUint(uint64(currentVersion), 10)).
		DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
