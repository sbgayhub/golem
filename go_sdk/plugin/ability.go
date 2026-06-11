package plugin

import (
	"log/slog"
	"reflect"
	"unsafe"
)

func check(plugin Plugin, typ reflect.Type) bool {
	elem := reflect.ValueOf(plugin)
	if elem.Kind() == reflect.Pointer {
		elem = elem.Elem()
	}

	for i := range elem.NumField() {
		field := elem.Type().Field(i)

		// 匿名嵌入：精确类型匹配 或 接口实现
		if field.Anonymous {
			if field.Type == typ || (typ.Kind() == reflect.Interface && field.Type.Implements(typ)) {
				return true
			}
			continue
		}

		// 命名字段类型实现了目标接口（如 message MessageAbility / Message MessageAbility）
		//if field.Type.Implements(typ) {
		// 命名字段：仅精确类型匹配（避免 SessionAbility 被 message.Ability 误匹配）
		if field.Type == typ {
			return true
		}
	}
	return false
}

func inject(plugin Plugin, typ reflect.Type, client any) {
	elem := reflect.ValueOf(plugin)
	if elem.Kind() == reflect.Pointer {
		elem = elem.Elem()
	}

	for i := range elem.NumField() {
		field := elem.Type().Field(i)
		fieldVal := elem.Field(i)

		// 匿名嵌入：精确类型匹配 或 接口实现
		if field.Anonymous {
			if field.Type == typ || (typ.Kind() == reflect.Interface && field.Type.Implements(typ)) {
				fieldVal.Set(reflect.ValueOf(client))
				slog.Debug("[inject] 匿名嵌入注入成功", "type", typ.Name(), "field", field.Name)
				return
			}
			continue
		}

		// 命名字段：精确类型匹配
		if field.Type == typ {
			if fieldVal.CanSet() {
				fieldVal.Set(reflect.ValueOf(client))
			} else {
				ptr := unsafe.Pointer(fieldVal.UnsafeAddr())
				reflect.NewAt(fieldVal.Type(), ptr).Elem().Set(reflect.ValueOf(client))
			}
			slog.Debug("[inject] 命名字段注入成功", "type", typ.Name(), "field", field.Name)
			return
		}
	}
	slog.Warn("[inject] 未找到匹配的能力字段", "type", typ.Name(), "plugin", elem.Type().Name())
}

// injectConfigSave 向插件的 ConfigAbility 注入配置保存回调
func injectConfigSave(plugin Plugin, saveFunc func(pluginName string, data []byte) error) {
	elem := reflect.ValueOf(plugin)
	if elem.Kind() == reflect.Pointer {
		elem = elem.Elem()
	}

	for i := range elem.NumField() {
		field := elem.Type().Field(i)
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			if _, ok := field.Type.FieldByName("hostSave"); ok {
				fieldVal := elem.Field(i).FieldByName("hostSave")
				if fieldVal.CanSet() {
					fieldVal.Set(reflect.ValueOf(saveFunc))
				} else {
					ptr := unsafe.Pointer(fieldVal.UnsafeAddr())
					reflect.NewAt(fieldVal.Type(), ptr).Elem().Set(reflect.ValueOf(saveFunc))
				}
				slog.Debug("[inject] ConfigAbility.hostSave 注入成功")
				return
			}
		}
	}
	slog.Warn("[inject] 未找到 ConfigAbility")
}
