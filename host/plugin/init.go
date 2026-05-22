package plugin

import (
	"log/slog"

	sdk "github.com/sbgayhub/golem/sdk/plugin"
)

// Initial 初始化插件管理器
func Initial() error {
	if err := loadConfig(); err != nil {
		return err
	}

	// 注册 HostService 实现（会话劫持 + 插件调用）
	sdk.HostServiceImpl = &hostService{}

	// 注册内置插件管理命令
	if err := registerBuiltinPM(); err != nil {
		return err
	}

	// 加载插件
	if err := LoadPlugins(); err != nil {
		return err
	}

	// 启动事件分发器
	go dispatcher()

	return nil
}

// Destroy 注销插件管理器
func Destroy() error {
	mu.Lock()
	close(events)
	mu.Unlock()

	for _, w := range pluginSnapshot() {
		sdk.Kill(w.Name)
		slog.Info("插件退出", "name", w.Name)
	}
	return nil
}
