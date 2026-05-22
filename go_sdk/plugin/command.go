package plugin

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

var defaultCommandRegistry = NewCommandRegistry()

// CommandRegistry 收集命令声明与处理函数
type CommandRegistry struct {
	items map[string]*commandDefinition
	keys  []string
}

type commandDefinition struct {
	handler commandInvoker
	schema  *CommandSchema
}

// CommandHandler 命令处理函数
type CommandHandler[T any] func(T) (string, error)

type commandInvoker func(*Command) (string, error)

// RegisterCommand 注册到默认命令注册器，schema 由 handler 参数类型自动推导
func RegisterCommand[T any](handler CommandHandler[T]) error {
	return registerCommand(defaultCommandRegistry, handler)
}

// RegisterCommandTo 注册到指定命令注册器，schema 由 handler 参数类型自动推导
func RegisterCommandTo[T any](r *CommandRegistry, handler CommandHandler[T]) error {
	return registerCommand(r, handler)
}

// CommandCommands 返回默认注册器中的主命令集合
func CommandCommands() []string {
	return defaultCommandRegistry.Commands()
}

// CommandSchemas 返回默认注册器中的命令 schema
func CommandSchemas() []*CommandSchema {
	return defaultCommandRegistry.Schemas()
}

// DispatchCommand 使用默认注册器执行命令
func DispatchCommand(cmd *Command) (string, error) {
	return defaultCommandRegistry.Dispatch(cmd)
}

// NewCommandRegistry 创建命令注册器
func NewCommandRegistry() *CommandRegistry {
	return &CommandRegistry{
		items: make(map[string]*commandDefinition),
	}
}

func registerCommand[T any](r *CommandRegistry, handler CommandHandler[T]) error {
	if r == nil {
		return fmt.Errorf("command registry is nil")
	}
	if handler == nil {
		return fmt.Errorf("command handler is nil")
	}

	schema, err := buildCommandSchemaFromType(reflect.TypeFor[T]())
	if err != nil {
		return err
	}
	return r.register(schema, func(cmd *Command) (string, error) {
		value, err := Bind[T](cmd)
		if err != nil {
			return "", err
		}
		return handler(value)
	})
}

func (r *CommandRegistry) register(schema *CommandSchema, handler commandInvoker) error {
	if schema == nil {
		return fmt.Errorf("command schema is nil")
	}
	if handler == nil {
		return fmt.Errorf("command handler is nil")
	}
	key := commandKey(schema.Main, schema.Sub)
	if _, ok := r.items[key]; !ok {
		r.keys = append(r.keys, key)
	}
	r.items[key] = &commandDefinition{
		handler: handler,
		schema:  schema,
	}
	return nil
}

// Commands 返回主命令集合
func (r *CommandRegistry) Commands() []string {
	seen := map[string]struct{}{}
	values := make([]string, 0, len(r.items))
	for _, key := range r.keys {
		def := r.items[key]
		if def == nil {
			continue
		}
		if _, ok := seen[def.schema.Main]; ok {
			continue
		}
		seen[def.schema.Main] = struct{}{}
		values = append(values, def.schema.Main)
	}
	return values
}

// Schemas 返回全部命令 schema
func (r *CommandRegistry) Schemas() []*CommandSchema {
	values := make([]*CommandSchema, 0, len(r.items))
	for _, key := range r.keys {
		def := r.items[key]
		if def == nil {
			continue
		}
		values = append(values, def.schema)
	}
	return values
}

// Dispatch 执行已解析的命令
func (r *CommandRegistry) Dispatch(cmd *Command) (string, error) {
	if cmd == nil {
		return "", fmt.Errorf("command is nil")
	}
	def, ok := r.items[commandKey(cmd.Main, cmd.Sub)]
	if !ok {
		return "", fmt.Errorf("command not registered: %s %s", cmd.Main, cmd.Sub)
	}
	return def.handler(cmd)
}

// Bind 将已解析命令绑定到强类型结构体
func Bind[T any](cmd *Command) (T, error) {
	var value T
	if cmd == nil {
		return value, fmt.Errorf("command is nil")
	}
	if err := bindValue(reflect.ValueOf(&value).Elem(), cmd); err != nil {
		return value, err
	}
	return value, nil
}

// LookupSchema 查找命令 schema
func (r *CommandRegistry) LookupSchema(main, sub string) (*CommandSchema, bool) {
	def, ok := r.items[commandKey(main, sub)]
	if !ok {
		return nil, false
	}
	return def.schema, true
}

func commandKey(main, sub string) string {
	return strings.TrimSpace(main) + " " + strings.TrimSpace(sub)
}

func buildCommandSchema(spec any) (*CommandSchema, error) {
	if spec == nil {
		return nil, fmt.Errorf("command spec is nil")
	}
	return buildCommandSchemaFromType(reflect.TypeOf(spec))
}

func buildCommandSchemaFromType(typ reflect.Type) (*CommandSchema, error) {
	if typ == nil {
		return nil, fmt.Errorf("command spec is nil")
	}
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return nil, fmt.Errorf("command spec must be struct, got %s", typ.Kind())
	}

	schema := &CommandSchema{}

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if tag := field.Tag.Get("cmd"); tag != "" {
			if err := fillCommandSchema(schema, typ.Name(), tag, field); err != nil {
				return nil, err
			}
			continue
		}
		if !field.IsExported() && !field.Anonymous {
			continue
		}
		if field.Anonymous {
			if field.Type.Kind() == reflect.Struct {
				if err := appendEmbeddedSchema(schema, field.Type); err != nil {
					return nil, err
				}
			}
			continue
		}

		optTag := field.Tag.Get("flag")
		argTag := field.Tag.Get("arg")
		if optTag == "" && argTag == "" {
			continue
		}
		if optTag != "" {
			schema.Options = append(schema.Options, buildOptionSchema(field, optTag))
			continue
		}
		schema.Arguments = append(schema.Arguments, buildArgumentSchema(field, argTag))
	}

	if schema.Main == "" {
		return nil, fmt.Errorf("command spec %s missing cmd tag", typ.Name())
	}
	return schema, nil
}

func fillCommandSchema(schema *CommandSchema, typeName, tag string, field reflect.StructField) error {
	parts := strings.Fields(tag)
	if len(parts) == 0 {
		return fmt.Errorf("empty cmd tag on %s", typeName)
	}
	schema.Main = parts[0]
	if len(parts) > 1 {
		schema.Sub = strings.Join(parts[1:], " ")
	}
	schema.Description = field.Tag.Get("help")
	schema.Usage = field.Tag.Get("usage")
	if examples := field.Tag.Get("example"); examples != "" {
		schema.Examples = strings.Split(examples, "\n")
	}
	return nil
}

func appendEmbeddedSchema(schema *CommandSchema, typ reflect.Type) error {
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return nil
	}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() && !field.Anonymous {
			continue
		}
		if optTag := field.Tag.Get("flag"); optTag != "" {
			schema.Options = append(schema.Options, buildOptionSchema(field, optTag))
			continue
		}
		if argTag := field.Tag.Get("arg"); argTag != "" {
			schema.Arguments = append(schema.Arguments, buildArgumentSchema(field, argTag))
			continue
		}
		if field.Anonymous {
			if err := appendEmbeddedSchema(schema, field.Type); err != nil {
				return err
			}
		}
	}
	return nil
}

func bindValue(v reflect.Value, cmd *Command) error {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("command bind target must be struct, got %s", v.Kind())
	}
	t := v.Type()
	pos := 0
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fieldValue := v.Field(i)
		if field.Anonymous {
			if fieldValue.Kind() == reflect.Struct {
				if err := bindValue(fieldValue, cmd); err != nil {
					return err
				}
			}
			continue
		}
		if !field.IsExported() {
			continue
		}
		if optTag := field.Tag.Get("flag"); optTag != "" {
			option := buildOptionSchema(field, optTag)
			raw := cmd.Args[option.Name]
			if raw == "" {
				raw = option.DefaultValue
			}
			if raw == "" && !option.HasValue && cmd.Args[option.Name] != "" {
				raw = "true"
			}
			if raw == "" {
				continue
			}
			if err := setFieldValue(fieldValue, raw); err != nil {
				return fmt.Errorf("bind flag %s: %w", option.Name, err)
			}
			continue
		}
		if argTag := field.Tag.Get("arg"); argTag != "" {
			arg := buildArgumentSchema(field, argTag)
			raw := ""
			if arg.GetVariadic() {
				if pos < len(cmd.Positionals) {
					raw = strings.Join(cmd.Positionals[pos:], " ")
					pos = len(cmd.Positionals)
				}
			} else if pos < len(cmd.Positionals) {
				raw = cmd.Positionals[pos]
				pos++
			} else {
				raw = arg.DefaultValue
			}
			if raw == "" {
				continue
			}
			if err := setFieldValue(fieldValue, raw); err != nil {
				return fmt.Errorf("bind arg %s: %w", arg.Name, err)
			}
		}
	}
	return nil
}

func setFieldValue(field reflect.Value, raw string) error {
	if !field.CanSet() {
		return fmt.Errorf("field cannot be set")
	}
	if field.Kind() == reflect.Pointer {
		value := reflect.New(field.Type().Elem())
		if err := setFieldValue(value.Elem(), raw); err != nil {
			return err
		}
		field.Set(value)
		return nil
	}
	switch field.Kind() {
	case reflect.String:
		field.SetString(raw)
	case reflect.Bool:
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		field.SetBool(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value, err := strconv.ParseInt(raw, 10, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetInt(value)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value, err := strconv.ParseUint(raw, 10, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetUint(value)
	case reflect.Float32, reflect.Float64:
		value, err := strconv.ParseFloat(raw, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetFloat(value)
	default:
		return fmt.Errorf("unsupported field type %s", field.Type())
	}
	return nil
}

func buildOptionSchema(field reflect.StructField, tag string) *CommandOption {
	parts := strings.Split(tag, ",")
	name := strings.TrimSpace(parts[len(parts)-1])
	short := ""
	long := ""
	if len(parts) == 1 {
		long = name
	} else {
		short = strings.TrimSpace(parts[0])
		long = name
	}
	return &CommandOption{
		Name:         name,
		Short:        short,
		Long:         long,
		Description:  field.Tag.Get("help"),
		Required:     field.Tag.Get("required") == "true",
		HasValue:     field.Tag.Get("value") != "false",
		DefaultValue: field.Tag.Get("default"),
	}
}

func buildArgumentSchema(field reflect.StructField, tag string) *CommandArgument {
	name := strings.TrimSpace(tag)
	return &CommandArgument{
		Name:         name,
		Description:  field.Tag.Get("help"),
		Required:     field.Tag.Get("required") == "true",
		Variadic:     field.Tag.Get("variadic") == "true",
		DefaultValue: field.Tag.Get("default"),
	}
}
