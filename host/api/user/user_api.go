// Package userapi 提供用户服务的 API 接口定义。
package userapi

import (
	"io"

	baseapi "github.com/sbgayhub/golem/host/api/base"
)

// UserService 用户服务 API 接口（返回 API proto 类型）。
type UserService interface {
	// GetProfile 获取个人信息
	GetProfile() (*GetProfileResponse, error)
	// UpdateProfile 更新个人信息
	UpdateProfile(params UpdateProfileParams) (*OperateResponse, error)
	// UploadAvatar 上传头像
	UploadAvatar(reader io.Reader) (*UploadAvatarResponse, error)
	// GetQRCode 获取个人二维码
	GetQRCode(style int32) (*GetQRCodeResponse, error)
	// SetPrivacy 设置隐私选项
	SetPrivacy(function, value int32) (*OperateResponse, error)
	// SetAlias 设置微信号
	SetAlias(alias string) (*GeneralSetResponse, error)
	// VerifyPassword 验证密码
	VerifyPassword(password string) (*VerifyPasswordResponse, error)
	// SetPassword 设置密码
	SetPassword(password, ticket string) (*SetPasswordResponse, error)
	// SendVerifyMobile 发送手机验证码
	SendVerifyMobile(mobile string, opcode uint32) (*BindOpMobileResponse, error)
	// BindMobile 绑定手机号
	BindMobile(mobile, verifyCode string) (*BindOpMobileResponse, error)
	// BindEmail 绑定邮箱
	BindEmail(email string) (*BindEmailResponse, error)
	// SendVerifyEmail 发送验证邮件
	SendVerifyEmail() (*SendVerifyEmailResponse, error)
	// GetSafetyInfo 获取安全设备列表
	GetSafetyInfo() (*GetSafetyInfoResponse, error)
	// DelSafeDevice 删除安全设备
	DelSafeDevice(uuid string) (*DeleteSafeDeviceResponse, error)
	// ReportMotion 上报运动步数
	ReportMotion(deviceID, deviceType string, stepCount int64) (*UploadDeviceStepResponse, error)
	// GetBoundHardDevices 获取绑定的硬件设备列表
	GetBoundHardDevices() (*GetBoundHardDevicesResponse, error)
	// GetCert 获取证书信息
	GetCert(currentVersion uint32) (*GetCertResponse, error)
}

// UpdateProfileParams 更新个人信息参数。
type UpdateProfileParams struct {
	NickName  string
	Sex       int32
	Country   string
	Province  string
	City      string
	Signature string
}

// OperateResponse 通用操作响应，复用 base proto 生成类型。
type OperateResponse = baseapi.OperateResponse
