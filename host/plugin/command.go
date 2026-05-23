package plugin

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/sbgayhub/golem/sdk/contact"
	"github.com/sbgayhub/golem/sdk/message"
	sdk "github.com/sbgayhub/golem/sdk/plugin"
)

type parsedCommand struct {
	cmd     *sdk.Command
	wrapper *wrapper
	schema  *sdk.CommandSchema
	help    string
}

// HandleCommand 尝试处理文本命令
func HandleCommand(raw string, sender *contact.Contact) (*message.Message, bool) {
	parsed, ok, err := parseCommand(raw, sender)
	if !ok {
		return nil, ok
	}
	if err != nil {
		if bytes := text2image(err.Error()); bytes != nil {
			return &message.Message{
				Type:     message.TypeImage,
				Receiver: sender,
				Content:  "命令错误",
				Data:     &message.Message_Image{Image: &message.ImageData{Media: &message.Media{Data: bytes}}},
			}, true
		}
		return &message.Message{
			Type:     message.TypeImage,
			Receiver: sender,
			Content:  err.Error(),
		}, true
	}
	if parsed.help != "" {
		if bytes := text2image(parsed.help); bytes != nil {
			return &message.Message{
				Type:     message.TypeImage,
				Receiver: sender,
				Content:  "命令错误",
				Data:     &message.Message_Image{Image: &message.ImageData{Media: &message.Media{Data: bytes}}},
			}, true
		}
		return &message.Message{
			Type:     message.TypeImage,
			Receiver: sender,
			Content:  parsed.help,
		}, true
	}
	result, err := (*parsed.wrapper.commandPlugin).OnCommand(parsed.cmd)
	if err != nil {
		return &message.Message{
			Type:     message.TypeImage,
			Receiver: sender,
			Content:  err.Error(),
		}, true
	}
	return &message.Message{
		Type:     message.TypeImage,
		Receiver: sender,
		Content:  result,
	}, true
}

func text2image(text string) []byte {
	if w, ex := capabilityIndex["text.to.image"]; ex {
		if called, err := (*w.calledPlugin).OnCall("text.to.image", map[string]string{"text": text}); err != nil {
			slog.Warn("[text.to.image] 能力调用失败", "plugin", w.Name, "err", err)
			return nil
		} else {
			if bytes, err := base64.StdEncoding.DecodeString(called); err != nil {
				return nil
			} else {
				return bytes
			}
		}
	}
	return nil
}

func parseCommand(raw string, sender *contact.Contact) (*parsedCommand, bool, error) {
	text := strings.TrimSpace(raw)
	if !strings.HasPrefix(text, "/") {
		return nil, false, nil
	}
	tokens, err := tokenize(text[1:])
	if err != nil {
		return nil, true, err
	}
	if len(tokens) == 0 {
		return nil, true, errors.New("空命令")
	}

	main := tokens[0]
	mu.Lock()
	target := commandIndex[main]
	mu.Unlock()
	if target == nil {
		return nil, true, fmt.Errorf("未知命令：/%s", main)
	}
	if target.Config != nil && !target.Config.Enable {
		return nil, true, fmt.Errorf("命令不可用：/%s", main)
	}
	if len(target.commandSchemas) == 0 {
		return nil, true, fmt.Errorf("命令缺少解析 schema：/%s", main)
	}
	if sender != nil && sender.GetUsername() != "" && !isAllowed(sender.GetUsername(), target) {
		return nil, true, fmt.Errorf("无权执行命令：/%s", main)
	}

	cmd := &sdk.Command{
		Raw:    raw,
		Main:   main,
		Args:   map[string]string{},
		Sender: sender,
	}

	rest := tokens[1:]
	if len(rest) == 0 {
		return &parsedCommand{cmd: cmd, wrapper: target, help: renderMainHelp(main, target.commandSchemas)}, true, nil
	}
	if isHelpToken(rest[0]) {
		cmd.Help = true
		return &parsedCommand{cmd: cmd, wrapper: target, help: renderMainHelp(main, target.commandSchemas)}, true, nil
	}

	schema, sub, remaining := matchCommandSchema(main, rest, target.commandSchemas)
	cmd.Sub = sub
	if schema == nil {
		return nil, true, fmt.Errorf("未知子命令：/%s %s", main, rest[0])
	}
	if containsHelp(remaining) {
		cmd.Help = true
		return &parsedCommand{cmd: cmd, wrapper: target, schema: schema, help: renderCommandHelp(schema)}, true, nil
	}

	if err := parseCommandArgs(cmd, schema, remaining); err != nil {
		return nil, true, fmt.Errorf("%w\n\n%s", err, renderCommandHelp(schema))
	}
	return &parsedCommand{cmd: cmd, wrapper: target, schema: schema}, true, nil
}

func tokenize(input string) ([]string, error) {
	var tokens []string
	var current strings.Builder
	var quote rune
	escaped := false

	for _, ch := range input {
		if escaped {
			current.WriteRune(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
				continue
			}
			current.WriteRune(ch)
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteRune(ch)
	}
	if escaped {
		return nil, errors.New("命令转义不完整")
	}
	if quote != 0 {
		return nil, errors.New("命令引号未闭合")
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens, nil
}

func matchCommandSchema(main string, rest []string, schemas []*sdk.CommandSchema) (*sdk.CommandSchema, string, []string) {
	if len(rest) > 0 {
		for _, schema := range schemas {
			if schema.GetMain() == main && schema.GetSub() == rest[0] {
				return schema, rest[0], rest[1:]
			}
		}
	}
	for _, schema := range schemas {
		if schema.GetMain() == main && schema.GetSub() == "" {
			return schema, "", rest
		}
	}
	return nil, "", rest
}

func parseCommandArgs(cmd *sdk.Command, schema *sdk.CommandSchema, tokens []string) error {
	shortOptions := map[string]*sdk.CommandOption{}
	longOptions := map[string]*sdk.CommandOption{}
	for _, opt := range schema.GetOptions() {
		if opt.GetShort() != "" {
			shortOptions[opt.GetShort()] = opt
		}
		if opt.GetLong() != "" {
			longOptions[opt.GetLong()] = opt
		}
	}

	positionals := []string{}
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		if strings.HasPrefix(token, "--") {
			name, value, hasInlineValue := strings.Cut(token[2:], "=")
			opt := longOptions[name]
			if opt == nil {
				return fmt.Errorf("未知参数：--%s", name)
			}
			if opt.GetHasValue() {
				if !hasInlineValue {
					i++
					if i >= len(tokens) {
						return fmt.Errorf("参数缺少值：--%s", name)
					}
					value = tokens[i]
				}
				cmd.Args[opt.GetName()] = value
			} else {
				cmd.Args[opt.GetName()] = "true"
			}
			continue
		}
		if strings.HasPrefix(token, "-") && token != "-" {
			name := strings.TrimPrefix(token, "-")
			opt := shortOptions[name]
			if opt == nil {
				return fmt.Errorf("未知参数：-%s", name)
			}
			if opt.GetHasValue() {
				i++
				if i >= len(tokens) {
					return fmt.Errorf("参数缺少值：-%s", name)
				}
				cmd.Args[opt.GetName()] = tokens[i]
			} else {
				cmd.Args[opt.GetName()] = "true"
			}
			continue
		}
		positionals = append(positionals, token)
	}

	if err := applyArguments(cmd, schema, positionals); err != nil {
		return err
	}
	for _, opt := range schema.GetOptions() {
		if cmd.Args[opt.GetName()] == "" && opt.GetDefaultValue() != "" {
			cmd.Args[opt.GetName()] = opt.GetDefaultValue()
		}
		if opt.GetRequired() && cmd.Args[opt.GetName()] == "" {
			return fmt.Errorf("缺少必填参数：--%s", optionDisplayName(opt))
		}
	}
	return nil
}

func applyArguments(cmd *sdk.Command, schema *sdk.CommandSchema, values []string) error {
	args := schema.GetArguments()
	positionals := make([]string, 0, len(values))
	index := 0
	for _, arg := range args {
		if arg.GetVariadic() {
			if index < len(values) {
				positionals = append(positionals, values[index:]...)
				index = len(values)
			} else if arg.GetDefaultValue() != "" {
				positionals = append(positionals, arg.GetDefaultValue())
			} else if arg.GetRequired() {
				return fmt.Errorf("缺少位置参数：%s", arg.GetName())
			}
			continue
		}
		if index < len(values) {
			positionals = append(positionals, values[index])
			index++
			continue
		}
		if arg.GetDefaultValue() != "" {
			positionals = append(positionals, arg.GetDefaultValue())
			continue
		}
		if arg.GetRequired() {
			return fmt.Errorf("缺少位置参数：%s", arg.GetName())
		}
	}
	if index < len(values) {
		return fmt.Errorf("多余的位置参数：%s", strings.Join(values[index:], " "))
	}
	cmd.Positionals = positionals
	return nil
}

func renderMainHelp(main string, schemas []*sdk.CommandSchema) string {
	lines := []string{"命令：/" + main}
	var subcommands []*sdk.CommandSchema
	for _, schema := range schemas {
		if schema.GetMain() != main {
			continue
		}
		if schema.GetSub() == "" {
			if schema.GetDescription() != "" {
				lines = append(lines, "", "说明："+schema.GetDescription())
			}
			continue
		}
		subcommands = append(subcommands, schema)
	}
	if len(subcommands) > 0 {
		lines = append(lines, "", "子命令：")
		subNameWidth := subcommandNameWidth(subcommands)
		for _, schema := range subcommands {
			lines = append(lines, formatSubcommandHelp(schema, subNameWidth))
		}
		lines = append(lines, "", "使用 /"+main+" <子命令> -h 查看详细帮助")
	}
	return strings.Join(lines, "\n")
}

func renderCommandHelp(schema *sdk.CommandSchema) string {
	name := "/" + schema.GetMain()
	if schema.GetSub() != "" {
		name += " " + schema.GetSub()
	}
	lines := []string{"命令：" + name}
	if schema.GetDescription() != "" {
		lines = append(lines, "", "说明："+schema.GetDescription())
	}
	if schema.GetUsage() != "" {
		lines = append(lines, "", "用法：", schema.GetUsage())
	}
	if len(schema.GetArguments()) > 0 {
		lines = append(lines, "", "位置参数：")
		nameWidth := argumentNameWidth(schema.GetArguments())
		for _, arg := range schema.GetArguments() {
			lines = append(lines, formatArgumentHelp(arg, nameWidth))
		}
	}
	optNameWidth := optionNameWidth(schema.GetOptions())
	if len(schema.GetOptions()) > 0 {
		lines = append(lines, "", "参数：")
		for _, opt := range schema.GetOptions() {
			lines = append(lines, formatOptionHelp(opt, optNameWidth))
		}
	}
	if len(schema.GetExamples()) > 0 {
		lines = append(lines, "", "示例：")
		lines = append(lines, schema.GetExamples()...)
	}
	lines = append(lines, "", formatHelpOption(optNameWidth))
	return strings.Join(lines, "\n")
}

func formatArgumentHelp(arg *sdk.CommandArgument, nameWidth int) string {
	state := "可选"
	if arg.GetRequired() {
		state = "必填"
	}
	line := fmt.Sprintf("  %-*s    %s", nameWidth, arg.GetName(), state)
	if arg.GetDescription() != "" {
		line += "  " + arg.GetDescription()
	}
	if arg.GetDefaultValue() != "" {
		line += "，默认 " + arg.GetDefaultValue()
	}
	return line
}

func formatOptionHelp(opt *sdk.CommandOption, nameWidth int) string {
	names := optionNames(opt)
	state := "可选"
	if opt.GetRequired() {
		state = "必填"
	}
	line := fmt.Sprintf("  %-*s    %s", nameWidth, names, state)
	if opt.GetDescription() != "" {
		line += "  " + opt.GetDescription()
	}
	if opt.GetDefaultValue() != "" {
		line += "，默认 " + opt.GetDefaultValue()
	}
	return line
}

func optionNames(opt *sdk.CommandOption) string {
	var names []string
	if opt.GetShort() != "" {
		names = append(names, "-"+opt.GetShort())
	}
	if opt.GetLong() != "" {
		names = append(names, "--"+opt.GetLong())
	}
	return strings.Join(names, ", ")
}

func formatSubcommandHelp(schema *sdk.CommandSchema, nameWidth int) string {
	line := fmt.Sprintf("  %-*s", nameWidth, schema.GetSub())
	if schema.GetDescription() != "" {
		line += "    " + schema.GetDescription()
	}
	return line
}

func formatHelpOption(nameWidth int) string {
	name := "-h, --help"
	if nameWidth < utf8.RuneCountInString(name) {
		nameWidth = utf8.RuneCountInString(name)
	}
	return fmt.Sprintf("%-*s    显示帮助信息", nameWidth, name)
}

func argumentNameWidth(args []*sdk.CommandArgument) int {
	var width int
	for _, arg := range args {
		width = max(width, utf8.RuneCountInString(arg.GetName()))
	}
	return width
}

func subcommandNameWidth(schemas []*sdk.CommandSchema) int {
	var width int
	for _, schema := range schemas {
		width = max(width, utf8.RuneCountInString(schema.GetSub()))
	}
	return width
}

func optionNameWidth(options []*sdk.CommandOption) int {
	var width int
	for _, opt := range options {
		width = max(width, utf8.RuneCountInString(optionNames(opt)))
	}
	return width
}

func isHelpToken(token string) bool {
	return token == "-h" || token == "--help"
}

func containsHelp(tokens []string) bool {
	for _, token := range tokens {
		if isHelpToken(token) {
			return true
		}
	}
	return false
}

func optionDisplayName(opt *sdk.CommandOption) string {
	if opt.GetLong() != "" {
		return opt.GetLong()
	}
	return opt.GetName()
}
