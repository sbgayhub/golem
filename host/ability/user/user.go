// Package userability 提供用户能力的实现（缓存型）。
package userability

import (
	"io"

	sdk "github.com/sbgayhub/golem/sdk/user"

	baseapi "github.com/sbgayhub/golem/host/api/base"
	messageapi "github.com/sbgayhub/golem/host/api/message"
	userapi "github.com/sbgayhub/golem/host/api/user"
)

// ability 用户能力实现（缓存型）。
type ability struct {
	api     userapi.UserService
	profile *sdk.Profile
}

var instance ability

func init() {
	instance = ability{api: userapi.Get()}
	sdk.Instance = &instance
}

// GetProfile 获取个人信息
func (a *ability) GetProfile() (*sdk.GetProfile_Response, error) {
	resp, err := a.api.GetProfile()
	if resp == nil || err != nil {
		return nil, err
	}
	profile := mapProfile(resp.GetUserInfo(), resp.GetUserInfoExt())
	a.profile = profile
	return &sdk.GetProfile_Response{
		BaseResult: mapBaseResult(resp.GetBaseResponse()),
		Profile:    profile,
	}, nil
}

// UpdateProfile 更新个人信息
func (a *ability) UpdateProfile(params sdk.UpdateProfileParams) (*sdk.UpdateProfile_Response, error) {
	resp, err := a.api.UpdateProfile(userapi.UpdateProfileParams{
		NickName:  params.NickName,
		Sex:       params.Sex,
		Country:   params.Country,
		Province:  params.Province,
		City:      params.City,
		Signature: params.Signature,
	})
	if resp == nil || err != nil {
		return nil, err
	}
	if a.profile != nil {
		a.profile.Nickname = params.NickName
		a.profile.Gender = params.Sex
		a.profile.Country = params.Country
		a.profile.Province = params.Province
		a.profile.City = params.City
		a.profile.Signature = params.Signature
	}
	return &sdk.UpdateProfile_Response{Result: mapOperateResponse(resp)}, nil
}

// UploadAvatar 上传头像
func (a *ability) UploadAvatar(reader io.Reader) (*sdk.UploadAvatar_Response, error) {
	resp, err := a.api.UploadAvatar(reader)
	if resp == nil || err != nil {
		return nil, err
	}
	avatar := &sdk.Avatar{
		Size:           resp.GetSize(),
		Offset:         resp.GetOffset(),
		FinalMd5:       resp.GetFinalMd5(),
		BigAvatarUrl:   resp.GetBigAvatarUrl(),
		SmallAvatarUrl: resp.GetSmallAvatarUrl(),
	}
	if a.profile != nil {
		a.profile.BigAvatarUrl = avatar.BigAvatarUrl
		a.profile.SmallAvatarUrl = avatar.SmallAvatarUrl
	}
	return &sdk.UploadAvatar_Response{
		BaseResult: mapBaseResult(resp.GetBaseResponse()),
		Avatar:     avatar,
	}, nil
}

// GetQRCode 获取个人二维码
func (a *ability) GetQRCode(style int32) (*sdk.GetQRCode_Response, error) {
	resp, err := a.api.GetQRCode(style)
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.GetQRCode_Response{
		BaseResult: mapBaseResult(resp.GetBaseResponse()),
		Qrcode: &sdk.QRCode{
			Qrcode:        mapBuffer(resp.GetQrcodeBuffer()),
			QrcodeUrl:     resp.GetQrcodeUrl(),
			FooterWording: resp.GetFooterWording(),
			NotifyWording: resp.GetNotifyWording(),
		},
	}, nil
}

// SetPrivacy 设置隐私选项
func (a *ability) SetPrivacy(function, value int32) (*sdk.SetPrivacy_Response, error) {
	resp, err := a.api.SetPrivacy(function, value)
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.SetPrivacy_Response{Result: mapOperateResponse(resp)}, nil
}

// SetAlias 设置微信号
func (a *ability) SetAlias(alias string) (*sdk.SetAlias_Response, error) {
	resp, err := a.api.SetAlias(alias)
	if resp == nil || err != nil {
		return nil, err
	}
	if a.profile != nil {
		a.profile.Alias = alias
	}
	return &sdk.SetAlias_Response{BaseResult: mapBaseResult(resp.GetBaseResponse())}, nil
}

// VerifyPassword 验证密码
func (a *ability) VerifyPassword(password string) (*sdk.VerifyPassword_Response, error) {
	resp, err := a.api.VerifyPassword(password)
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.VerifyPassword_Response{
		Result: &sdk.VerifyPasswordResult{
			BaseResult:      mapBaseResult(resp.GetBaseResponse()),
			ImageSid:        resp.GetImageSid().GetValue(),
			ImageBuffer:     mapBuffer(resp.GetImageBuffer()),
			Ticket:          resp.GetTicket(),
			ImageEncryptKey: resp.GetImageEncryptKey().GetValue(),
			A2Key:           mapBuffer(resp.GetA2_Key()),
			Ksid:            mapBuffer(resp.GetKsid()),
			AuthKey:         resp.GetAuthKey(),
		},
	}, nil
}

// SetPassword 设置密码
func (a *ability) SetPassword(password, ticket string) (*sdk.SetPassword_Response, error) {
	resp, err := a.api.SetPassword(password, ticket)
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.SetPassword_Response{
		BaseResult:  mapBaseResult(resp.GetBaseResponse()),
		AutoAuthKey: mapBuffer(resp.GetAutoAuthKey()),
	}, nil
}

// SendVerifyMobile 发送手机验证码
func (a *ability) SendVerifyMobile(mobile string, opcode uint32) (*sdk.SendVerifyMobile_Response, error) {
	resp, err := a.api.SendVerifyMobile(mobile, opcode)
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.SendVerifyMobile_Response{Result: mapMobileResult(resp)}, nil
}

// BindMobile 绑定手机号
func (a *ability) BindMobile(mobile, verifyCode string) (*sdk.BindMobile_Response, error) {
	resp, err := a.api.BindMobile(mobile, verifyCode)
	if resp == nil || err != nil {
		return nil, err
	}
	if a.profile != nil {
		a.profile.Mobile = mobile
	}
	return &sdk.BindMobile_Response{Result: mapMobileResult(resp)}, nil
}

// BindEmail 绑定邮箱
func (a *ability) BindEmail(email string) (*sdk.BindEmail_Response, error) {
	resp, err := a.api.BindEmail(email)
	if resp == nil || err != nil {
		return nil, err
	}
	if a.profile != nil {
		a.profile.Email = email
	}
	return &sdk.BindEmail_Response{BaseResult: mapBaseResult(resp.GetBaseResponse())}, nil
}

// SendVerifyEmail 发送验证邮件
func (a *ability) SendVerifyEmail() (*sdk.SendVerifyEmail_Response, error) {
	resp, err := a.api.SendVerifyEmail()
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.SendVerifyEmail_Response{BaseResult: mapBaseResult(resp.GetBaseResponse())}, nil
}

// GetSafetyInfo 获取安全设备列表
func (a *ability) GetSafetyInfo() (*sdk.GetSafetyInfo_Response, error) {
	resp, err := a.api.GetSafetyInfo()
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.GetSafetyInfo_Response{
		BaseResult: mapBaseResult(resp.GetBaseResponse()),
		Info:       mapSafetyInfo(resp.GetInfo()),
	}, nil
}

// DelSafeDevice 删除安全设备
func (a *ability) DelSafeDevice(uuid string) (*sdk.DelSafeDevice_Response, error) {
	resp, err := a.api.DelSafeDevice(uuid)
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.DelSafeDevice_Response{
		BaseResult: mapBaseResult(resp.GetBaseResponse()),
		RestCount:  resp.GetRestCount(),
	}, nil
}

// ReportMotion 上报运动步数
func (a *ability) ReportMotion(deviceID, deviceType string, stepCount int64) (*sdk.ReportMotion_Response, error) {
	resp, err := a.api.ReportMotion(deviceID, deviceType, stepCount)
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.ReportMotion_Response{
		BaseResult: mapBaseResult(resp.GetBaseResponse()),
		DeviceInfo: mapSportDeviceInfo(resp.GetDeviceInfo()),
	}, nil
}

// GetBoundHardDevices 获取绑定的硬件设备列表
func (a *ability) GetBoundHardDevices() (*sdk.GetBoundHardDevices_Response, error) {
	resp, err := a.api.GetBoundHardDevices()
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.GetBoundHardDevices_Response{
		BaseResult:   mapBaseResult(resp.GetBaseResponse()),
		Devices:      mapHardDevices(resp.GetList()),
		Version:      resp.GetVersion(),
		ContinueFlag: resp.GetContinueFlag(),
	}, nil
}

// GetCert 获取证书信息
func (a *ability) GetCert(currentVersion uint32) (*sdk.GetCert_Response, error) {
	resp, err := a.api.GetCert(currentVersion)
	if resp == nil || err != nil {
		return nil, err
	}
	cert := resp.GetCert()
	return &sdk.GetCert_Response{
		BaseResult: mapBaseResult(resp.GetBaseResponse()),
		Cert: &sdk.Cert{
			KeyN:        cert.GetKeyN(),
			KeyE:        cert.GetKeyE(),
			CertVersion: resp.GetCertVersion(),
		},
	}, nil
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

func mapOperateResponse(resp *baseapi.OperateResponse) *sdk.OperationResult {
	if resp == nil {
		return nil
	}
	result := resp.GetResult()
	return &sdk.OperationResult{
		Code:       resp.GetCode(),
		Count:      result.GetCount(),
		ResultCode: result.GetCode(),
		Message:    result.GetMessage(),
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

func mapProfile(info *messageapi.ModifyUserInfo, ext *messageapi.UserInfoExtend) *sdk.Profile {
	if info == nil && ext == nil {
		return nil
	}
	profile := &sdk.Profile{}
	if info != nil {
		profile.Username = info.GetUserName().GetValue()
		profile.Nickname = info.GetNickName().GetValue()
		profile.Alias = info.GetAlias()
		profile.Email = info.GetEmail().GetValue()
		profile.Mobile = info.GetMobile().GetValue()
		profile.Uin = info.GetUin()
		profile.Gender = int32(info.GetGender())
		profile.Country = info.GetCountry()
		profile.Province = info.GetProvince()
		profile.City = info.GetCity()
		profile.Signature = info.GetSignature()
	}
	if ext != nil {
		profile.BigAvatarUrl = ext.GetBigAvatarUrl()
		profile.SmallAvatarUrl = ext.GetSmallAvatarUrl()
		profile.SafeMobile = ext.GetSafeMobile()
	}
	return profile
}

func mapMobileResult(resp *userapi.BindOpMobileResponse) *sdk.MobileResult {
	if resp == nil {
		return nil
	}
	return &sdk.MobileResult{
		BaseResult:      mapBaseResult(resp.GetBaseResponse()),
		Ticket:          resp.GetTicket(),
		SmsNo:           resp.GetSmsNo(),
		NeedSetPassword: resp.GetNeedSetPassword(),
		Username:        resp.GetUsername(),
		AuthTicket:      resp.GetAuthTicket(),
		PureMobile:      resp.GetPureMobile(),
		FormatedMobile:  resp.GetFormatedMobile(),
		MobileCheckType: resp.GetMobileCheckType(),
		RegSessionId:    resp.GetRegSessionId(),
	}
}

func mapSafetyInfo(info *userapi.GetSafetyInfoResponse_SafetyInfo) *sdk.SafetyInfo {
	if info == nil {
		return nil
	}
	return &sdk.SafetyInfo{
		DeviceList:  mapSafetyDevices(info.GetDeviceList()),
		HasVoice:    info.GetHasVoice(),
		SwitchVoice: info.GetSwitchVoice(),
		HasFace:     info.GetHasFace(),
		SwitchFace:  info.GetSwitchFace(),
		HasPassword: info.GetHasPassword(),
	}
}

func mapSafetyDevices(devices []*userapi.GetSafetyInfoResponse_SafetyDevice) []*sdk.SafetyDevice {
	result := make([]*sdk.SafetyDevice, 0, len(devices))
	for _, device := range devices {
		if device == nil {
			continue
		}
		result = append(result, &sdk.SafetyDevice{
			Uuid:       device.GetUuid(),
			DeviceName: device.GetDeviceName(),
			DeviceType: device.GetDeviceType(),
			LastTime:   device.GetLastTime(),
		})
	}
	return result
}

func mapSportDeviceInfo(info *userapi.SportDeviceInfo) *sdk.SportDeviceInfo {
	if info == nil {
		return nil
	}
	return &sdk.SportDeviceInfo{
		BundleId:      info.GetBundleId(),
		AppName:       info.GetAppName(),
		StepCount:     info.GetStepCount(),
		IsAppleWatch:  info.GetIsAppleWatch(),
		IsWhiteList:   info.GetIsWhiteList(),
		IsLocalIphone: info.GetIsLocalIphone(),
	}
}

func mapHardDevices(devices []*userapi.GetBoundHardDevicesResponse_HardDevice) []*sdk.HardDevice {
	result := make([]*sdk.HardDevice, 0, len(devices))
	for _, device := range devices {
		if device == nil {
			continue
		}
		info := device.GetDevice()
		attr := device.GetAttribute()
		result = append(result, &sdk.HardDevice{
			Type:              info.GetType(),
			Id:                info.GetId(),
			BrandName:         attr.GetBrandName(),
			Alias:             attr.GetAlias(),
			IconUrl:           attr.GetIconUrl(),
			DeviceTitle:       attr.GetDeviceTitle(),
			DeviceDescription: attr.GetDeviceDescription(),
			Category:          attr.GetCategory(),
			Flag:              device.GetFlag(),
		})
	}
	return result
}
