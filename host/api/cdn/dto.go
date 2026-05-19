package cdnapi

// ==================== 上传请求 DTO ====================
// Web API 使用 base64 编码的字符串传输二进制数据

// webUploadImageParam web 上传聊天图片参数
type webUploadImageParam struct {
	Receiver  string `json:"receiver"`   // 接收人 wxid
	ImageData string `json:"image_data"` // 图片数据（base64编码）
}

// webUploadSnsImageParam web 上传朋友圈图片参数
type webUploadSnsImageParam struct {
	ImageData string `json:"image_data"` // 图片数据（base64编码）
}

// webUploadVideoParam web 上传聊天视频参数
type webUploadVideoParam struct {
	Receiver  string `json:"receiver"`   // 接收人 wxid
	VideoData string `json:"video_data"` // 视频数据（base64编码）
	ThumbData string `json:"thumb_data"` // 缩略图数据（base64编码）
	Duration  uint32 `json:"duration"`   // 视频时长（秒）
}

// webUploadSnsVideoParam web 上传朋友圈视频参数
type webUploadSnsVideoParam struct {
	VideoData string `json:"video_data"` // 视频数据（base64编码）
	ThumbData string `json:"thumb_data"` // 缩略图数据（base64编码）
}

// ==================== 下载请求 DTO ====================

// webDownloadImageParam web 下载图片参数
type webDownloadImageParam struct {
	FileID  string `json:"file_id"`  // web 文件ID
	FileKey string `json:"file_key"` // AES 密钥（hex编码）
}

// webDownloadVideoParam web 下载视频参数
type webDownloadVideoParam struct {
	FileID  string `json:"file_id"`  // web 文件ID
	FileKey string `json:"file_key"` // AES 密钥（hex编码）
}

// webDownloadSnsVideoParam web 下载朋友圈视频参数
type webDownloadSnsVideoParam struct {
	VideoURL string `json:"video_url"` // 加密视频 URL
	EncKey   uint64 `json:"enc_key"`   // 解密密钥
}
