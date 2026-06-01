//go:build lib

// Package userapi 提供用户服务的 lib 实现（直接调用底层实现）。
package userapi

import (
	"errors"
	"io"
	"reflect"
	"sync"

	"golem/pkg/user"

	"github.com/sbgayhub/golem/host/api"
	"google.golang.org/protobuf/proto"
)

// lib 用户服务 lib 实现（直接调用底层实现）。
type lib struct{}

// Get 获取 UserService 单例（lib 模式）。
var Get = sync.OnceValue(func() UserService {
	return &lib{}
})

// GetProfile 获取个人信息
func (l lib) GetProfile() (*GetProfileResponse, error) {
	resp, err := user.GetProfile()
	return transform(resp, err, &GetProfileResponse{})
}

// UpdateProfile 更新个人信息
func (l lib) UpdateProfile(params UpdateProfileParams) (*OperateResponse, error) {
	resp, err := callUpdateProfile(params)
	return transform(resp, err, &OperateResponse{})
}

// UploadAvatar 上传头像
func (l lib) UploadAvatar(reader io.Reader) (*UploadAvatarResponse, error) {
	resp, err := user.UploadAvatar(reader)
	return transform(resp, err, &UploadAvatarResponse{})
}

// GetQRCode 获取个人二维码
func (l lib) GetQRCode(style int32) (*GetQRCodeResponse, error) {
	resp, err := user.GetQRCode(style)
	return transform(resp, err, &GetQRCodeResponse{})
}

// SetPrivacy 设置隐私选项
func (l lib) SetPrivacy(function, value int32) (*OperateResponse, error) {
	resp, err := user.SetPrivacy(function, value)
	return transform(resp, err, &OperateResponse{})
}

// SetAlias 设置微信号
func (l lib) SetAlias(alias string) (*GeneralSetResponse, error) {
	resp, err := user.SetAlias(alias)
	return transform(resp, err, &GeneralSetResponse{})
}

// VerifyPassword 验证密码
func (l lib) VerifyPassword(password string) (*VerifyPasswordResponse, error) {
	resp, err := user.VerifyPassword(password)
	return transform(resp, err, &VerifyPasswordResponse{})
}

// SetPassword 设置密码
func (l lib) SetPassword(password, ticket string) (*SetPasswordResponse, error) {
	resp, err := user.SetPassword(password, ticket)
	return transform(resp, err, &SetPasswordResponse{})
}

// SendVerifyMobile 发送手机验证码
func (l lib) SendVerifyMobile(mobile string, opcode uint32) (*BindOpMobileResponse, error) {
	resp, err := user.SendVerifyMobile(mobile, opcode)
	return transform(resp, err, &BindOpMobileResponse{})
}

// BindMobile 绑定手机号
func (l lib) BindMobile(mobile, verifyCode string) (*BindOpMobileResponse, error) {
	resp, err := user.BindMobile(mobile, verifyCode)
	return transform(resp, err, &BindOpMobileResponse{})
}

// BindEmail 绑定邮箱
func (l lib) BindEmail(email string) (*BindEmailResponse, error) {
	resp, err := user.BindEmail(email)
	return transform(resp, err, &BindEmailResponse{})
}

// SendVerifyEmail 发送验证邮件
func (l lib) SendVerifyEmail() (*SendVerifyEmailResponse, error) {
	resp, err := user.SendVerifyEmail()
	return transform(resp, err, &SendVerifyEmailResponse{})
}

// GetSafetyInfo 获取安全设备列表
func (l lib) GetSafetyInfo() (*GetSafetyInfoResponse, error) {
	resp, err := user.GetSafetyInfo()
	return transform(resp, err, &GetSafetyInfoResponse{})
}

// DelSafeDevice 删除安全设备
func (l lib) DelSafeDevice(uuid string) (*DeleteSafeDeviceResponse, error) {
	resp, err := user.DelSafeDevice(uuid)
	return transform(resp, err, &DeleteSafeDeviceResponse{})
}

// ReportMotion 上报运动步数
func (l lib) ReportMotion(deviceID, deviceType string, stepCount int64) (*UploadDeviceStepResponse, error) {
	resp, err := user.ReportMotion(deviceID, deviceType, stepCount)
	return transform(resp, err, &UploadDeviceStepResponse{})
}

// GetBoundHardDevices 获取绑定的硬件设备列表
func (l lib) GetBoundHardDevices() (*GetBoundHardDevicesResponse, error) {
	resp, err := user.GetBoundHardDevices()
	return transform(resp, err, &GetBoundHardDevicesResponse{})
}

// GetCert 获取证书信息
func (l lib) GetCert(currentVersion uint32) (*GetCertResponse, error) {
	resp, err := user.GetCert(currentVersion)
	return transform(resp, err, &GetCertResponse{})
}

func callUpdateProfile(params UpdateProfileParams) (proto.Message, error) {
	fn := reflect.ValueOf(user.UpdateProfile)
	arg := reflect.New(fn.Type().In(0)).Elem()
	fields := map[string]any{
		"NickName":  params.NickName,
		"Sex":       params.Sex,
		"Country":   params.Country,
		"Province":  params.Province,
		"City":      params.City,
		"Signature": params.Signature,
	}
	for name, value := range fields {
		field := arg.FieldByName(name)
		if !field.IsValid() || !field.CanSet() {
			return nil, errors.New("invalid user UpdateProfile parameter field: " + name)
		}
		source := reflect.ValueOf(value)
		if source.Type().AssignableTo(field.Type()) {
			field.Set(source)
			continue
		}
		if source.Type().ConvertibleTo(field.Type()) {
			field.Set(source.Convert(field.Type()))
			continue
		}
		return nil, errors.New("incompatible user UpdateProfile parameter field: " + name)
	}
	out := fn.Call([]reflect.Value{arg})
	if !out[1].IsNil() {
		return nil, out[1].Interface().(error)
	}
	if out[0].IsNil() {
		return nil, nil
	}
	resp, ok := out[0].Interface().(proto.Message)
	if !ok {
		return nil, errors.New("user UpdateProfile returned non-proto response")
	}
	return resp, nil
}

func transform[T proto.Message](resp proto.Message, err error, target T) (T, error) {
	if resp == nil || err != nil {
		var zero T
		return zero, err
	}
	if err := api.TransformProto(resp, target); err != nil {
		var zero T
		return zero, err
	}
	return target, nil
}
