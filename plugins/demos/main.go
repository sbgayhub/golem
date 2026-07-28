package main

import (
	"log/slog"

	"github.com/sbgayhub/golem/sdk/plugin"
)

func main() {
	p := &DemosPlugin{
		ConfigAbility: plugin.ConfigAbility[Config]{
			Config: Config{
				VideoNative:    true,
				MaxList:        3,
				SilkSampleRate: 24000,
				SilkMaxBytes:   28000,
			},
		},
		client: newHTTPClient(),
	}

	p.handlers = map[string]handlerFunc{
		// 图片
		"撸猫":    p.handleCat,
		"旺财":    p.handleDog,
		"看星空":   p.handleTw,
		"名画赏析":  p.handlePainting,
		"小黑子表情": p.handleXhzbq,
		"随机二次元": p.handleSjecy,
		"acg美图": p.handleAcg,

		// 视频
		"--小姐姐视频": p.handleXjj,
		"小姐姐视频":   p.handleXjj2,
		"热点视频":    p.handleRdVideo,
		"娱乐视频":    p.handleYlVideo,
		"靓仔视频":    p.handleBoyVideo,
		"懒羊羊k歌":   p.handleLyyKg,
		"怼脸自拍视频":  p.handleDuilian,
		"看穿搭":     p.handleChuanda,
		"丝滑舞蹈视频":  p.handleShwd,
		"快手随机翻唱":  p.handleKsfc,

		// 文本
		"温馨提示":   p.handleWxts,
		"一句":     p.handleYiju,
		"一言":     p.handleYiyan,
		"今日句子":   p.handleYiyan,
		"吟诗":     p.handleShici,
		"讲笑话":    p.handleHaha,
		"脑筋急转弯":  p.handleJzw,
		"来段绕口令":  p.handleRao,
		"谚语":     p.handleYanyu,
		"抽签":     p.handleChouqian,
		"答案之书":   p.handleBay,
		"吃什么":    p.handleEat,
		"保安日记":   p.handleRj,
		"king台词": p.handleKingTc,
		"l台词":    p.handleLolTc,
		"随机坤坤":   p.handleSjkk,

		// 搜索
		"百度百科": p.handleBdbk,
		"搜短剧":  p.handleSdj,

		// 生成
		"婚宴请柬":     p.handleHunYanQingJian,
		"狗屁不通文章生成": p.handleDogDoc,

		// 音乐
		"随机唱":  p.handleSjSing,
		"火子搜歌": p.handleSearchMusic,
		"火子点歌": p.handleChooseMusic,

		// 帮助
		"demos":   p.handleExplain,
		"demos帮助": p.handleExplain,
	}

	slog.Info("[demos] 娱乐插件启动中...")
	plugin.Start(p)
}
