package main

// Config 插件配置
type Config struct {
	VideoNative     bool   `toml:"video_native" comment:"视频使用 CDN 原生上传（失败则回退到链接卡片）"`
	MaxList         int    `toml:"max_list" comment:"列表类结果最大条数"`
	BdbkURL         string `toml:"bdbk_url" comment:"百度百科 API 地址（需自行配置）"`
	SilkEncoderPath string `toml:"silk_encoder_path" comment:"silk_v3_encoder.exe 的绝对路径；为空则语音转码回退到 AMR"`
	SilkSampleRate  int    `toml:"silk_sample_rate" comment:"SILK 编码采样率(Hz)，默认 24000；仅当 silk_encoder_path 非空时生效"`
	SilkMaxBytes    int    `toml:"silk_max_bytes" comment:"单条语音字节预算，默认 28000；上传通道超限会截断（播放戛然而止），超预算自动降码率、必要时裁剪时长；0 或负数关闭"`
}
