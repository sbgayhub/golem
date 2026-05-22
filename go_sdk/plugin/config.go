package plugin

import (
	"errors"
	"reflect"

	"github.com/pelletier/go-toml/v2"
)

// ConfigAbility 配置能力结构体，插件嵌入以声明配置
// 插件应初始化 Config 字段为默认值
//
// 用法：
//
//	type MyConfig struct { Token string; Enable bool }
//	type MyPlugin struct {
//	    plugin.ConfigAbility[MyConfig]
//	}
//	func New() *MyPlugin {
//	    return &MyPlugin{ConfigAbility: plugin.ConfigAbility[MyConfig]{
//	        Config: MyConfig{Token: "default", Enable: true},
//	    }}
//	}
type ConfigAbility[T any] struct {
	Config   T
	hostSave func(pluginName string, data []byte) error // 宿主注入，不导出
}

// SaveConfig 保存插件配置到宿主
func (c *ConfigAbility[T]) SaveConfig(p Plugin) error {
	if c.hostSave == nil {
		return errors.New("config save ability not injected")
	}
	data, err := toml.Marshal(c.Config)
	if err != nil {
		return err
	}
	return c.hostSave(p.GetMetadata().Name, data)
}

// findConfigField 查找插件结构体中 ConfigAbility 嵌入的 Config 字段
// 通过结构特征检测：匿名嵌入的结构体中包含名为 Config 的字段
func findConfigField(p Plugin) reflect.Value {
	elem := reflect.ValueOf(p)
	if elem.Kind() == reflect.Pointer {
		elem = elem.Elem()
	}
	for i := range elem.NumField() {
		field := elem.Type().Field(i)
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			if _, ok := field.Type.FieldByName("Config"); ok {
				return elem.Field(i).FieldByName("Config")
			}
		}
	}
	return reflect.Value{}
}
