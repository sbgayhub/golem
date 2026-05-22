package plugin

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

var configs = map[string]*Config{}                       // 插件配置
var pluginDir = "../plugins"                             // 插件所在目录
var configPath = filepath.Join(pluginDir, "config.toml") // 插件配置文件路径

// Config 插件配置
type Config struct {
	Enable    bool     `toml:"enable"`               // 是否启用插件
	Next      *bool    `toml:"next,commented"`       // 插件成功处理之后是否继续执行下一个插件
	Priority  *int32   `toml:"priority,commented"`   // 插件优先级
	AlwaysRun *bool    `toml:"always_run,commented"` // 是否一直运行，忽略会话劫持
	Mode      string   `toml:"mode"`                 // 插件限制模式：blacklist|whitelist
	Limits    []string `toml:"limits"`               // 插件限制联系人列表
	Config    any      `toml:"config"`               // 插件配置
}

func loadConfig() error {
	// 检查配置文件是否存在
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// 配置文件不存在，创建默认配置文件
		return saveConfig()
	}

	file, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("读取插件配置文件失败: %w", err)
	}
	if err := toml.Unmarshal(file, &configs); err != nil {
		return err
	}
	return nil
}

func saveConfig() error {
	// 确保目录存在
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		return fmt.Errorf("创建插件目录失败: %w", err)
	}

	if b, err := toml.Marshal(&configs); err != nil {
		return err
	} else {
		if err := os.WriteFile(configPath, b, 0755); err != nil {
			return err
		}
	}
	return nil
}
