package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

// 「插件配置」临时页：按段读写运行目录下的 plugins/config.toml。
//
// 为什么绕开 host 的 saveConfig：host 的写链是 toml.Marshal 全量重写，
// 会丢注释、按字母序重排；本页只替换用户选中的那一段，其余字节原样保留，
// 所以自己按字节拼接。代价是 host 再写一次（/pm set、插件 SaveConfig）
// 仍会整篇覆盖，本页只做写前 ETag 校验与写后复核，不做跨进程文件锁。
//
// 生效方式：host 用 fsnotify 监听该文件，Write/Create/Rename/Remove 后
// 300ms 去抖热重载（只重新注入 gRPC 配置、不重启进程）。启动期读取的字段
// （监听地址、目录、OnLoad 里起的服务）必须 /pm reload <name> 才生效。

const (
	pluginConfigSelfRoot   = "hermes_bridge"
	pluginConfigMaxBytes   = 2 << 20 // 整文件上限
	pluginConfigMaxSection = 256 << 10
	pluginConfigMaxBody    = 512 << 10
	pluginConfigBackupKeep = 20
	pluginConfigBackupDir  = "config_backups"
)

var (
	pluginConfigMu     sync.Mutex // 串行化本进程内的读改写
	pluginConfigNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

// pluginConfigSection 是 config.toml 里「同一个根的连续段落」。
// 区间 [Start,End) 以字节计，End 指向下一段段首；最后一段到文件尾。
type pluginConfigSection struct {
	Root      string
	Run       int
	Runs      int
	Start     int
	End       int
	StartLine int
	EndLine   int
	Tables    int
	Bytes     int
	EOL       string // "lf" | "crlf"
	Readonly  bool
	Reason    string
}

// pluginConfigProbe 与 host/plugin/config.go 的 Config 同构（标量用指针，
// 便于发现「键名写错/类型写错」这类会让 host 整文件重载失败的问题）。
// Config 保持 any：host 就是 any，写成 map 会误杀。
type pluginConfigProbe struct {
	Enable    *bool    `toml:"enable"`
	Next      *bool    `toml:"next"`
	Priority  *int32   `toml:"priority"`
	AlwaysRun *bool    `toml:"always_run"`
	Mode      *string  `toml:"mode"`
	Limits    []string `toml:"limits"`
	Config    any      `toml:"config"`
}

type pluginConfigResolve struct {
	path   string
	source string
	tried  []string
}

// resolvePluginConfigPath 找 host 正在读写的 plugins/config.toml。
// host 用相对路径 plugins/config.toml（相对 cwd），插件进程 cwd 继承 host，
// 所以 cwd 那条与 host 完全同源；exe 同目录那条对应常见部署布局
// （插件 exe 就放在 plugins/ 里）。
func resolvePluginConfigPath() pluginConfigResolve {
	var r pluginConfigResolve
	candidates := []struct{ path, source string }{
		{filepath.Join("plugins", "config.toml"), "cwd_plugins"},
		{"config.toml", "cwd"},
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			struct{ path, source string }{filepath.Join(exeDir, "config.toml"), "executable_dir"},
			struct{ path, source string }{filepath.Join(filepath.Dir(exeDir), "plugins", "config.toml"), "parent_plugins"},
		)
	}
	for _, c := range candidates {
		abs, err := filepath.Abs(c.path)
		if err != nil {
			abs = c.path
		}
		r.tried = append(r.tried, abs)
		if r.path != "" {
			continue
		}
		if info, err := os.Stat(abs); err == nil && info.Mode().IsRegular() {
			r.path = abs
			r.source = c.source
		}
	}
	return r
}

func etagOf(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// splitPluginConfig 用 go-toml 的 unstable AST 顺序消费顶层表达式，
// 只认真正的 [table] / [[array-table]]，不会被多行字符串或数组里的假表头骗。
func splitPluginConfig(data []byte) ([]pluginConfigSection, int, error) {
	body := data
	bomLen := 0
	if bytes.HasPrefix(body, []byte{0xEF, 0xBB, 0xBF}) {
		bomLen = 3
	}
	_ = bomLen

	type header struct {
		root   string
		offset int
	}
	var headers []header

	var ps unstable.Parser
	ps.Reset(body)
	for ps.NextExpression() {
		n := ps.Expression()
		if n.Kind != unstable.Table && n.Kind != unstable.ArrayTable {
			continue
		}
		it := n.Key()
		first := it.Node()
		if first == nil {
			continue
		}
		root := string(first.Data)
		off := headerLineStart(body, int(first.Raw.Offset))
		headers = append(headers, header{root: root, offset: off})
	}
	if err := ps.Error(); err != nil {
		return nil, 0, err
	}
	if len(headers) == 0 {
		return nil, 0, nil
	}

	// 首个表头之前若还有非空非注释内容（顶层键值），分段无法唯一定位，交给上层拒绝编辑。
	preStart := bomLen
	if hasContent(body[preStart:headers[0].offset]) {
		return nil, headers[0].offset, errPluginConfigPreamble
	}

	sections := make([]pluginConfigSection, 0, len(headers))
	for i, h := range headers {
		end := len(data)
		if i+1 < len(headers) {
			end = headers[i+1].offset
		}
		sec := pluginConfigSection{
			Root:      h.root,
			Start:     h.offset,
			End:       end,
			StartLine: 1 + bytes.Count(data[:h.offset], []byte{'\n'}),
			Tables:    1,
		}
		sec.EOL = eolOf(data[sec.Start:sec.End])
		// 同根的连续段落合并成一个 run（[ai] + [ai.config] + [[ai.config.targets]]）。
		if n := len(sections); n > 0 && sections[n-1].Root == sec.Root {
			prev := &sections[n-1]
			prev.End = sec.End
			prev.Tables++
			prev.EOL = eolOf(data[prev.Start:prev.End])
			if prev.EOL == "mixed" {
				prev.Readonly, prev.Reason = true, "段落内行尾混用（CRLF/LF），文本编辑会改写行尾"
			}
			continue
		}
		sections = append(sections, sec)
	}

	// run 序号与总数；同根存在多段不连续时全部只读（写入无法唯一定位）。
	runsPerRoot := map[string]int{}
	for i := range sections {
		runsPerRoot[sections[i].Root]++
	}
	seen := map[string]int{}
	for i := range sections {
		sec := &sections[i]
		seen[sec.Root]++
		sec.Run = seen[sec.Root]
		sec.Runs = runsPerRoot[sec.Root]
		sec.Bytes = sec.End - sec.Start
		sec.EndLine = endLineOf(data, sec.Start, sec.End)
		if sec.EOL == "mixed" {
			sec.Readonly, sec.Reason = true, "段落内行尾混用（CRLF/LF），文本编辑会改写行尾"
		} else if sec.EOL == "crlf" && hasMultiLineString(data[sec.Start:sec.End]) {
			sec.Readonly, sec.Reason = true, "段落含多行字符串且为 CRLF 行尾，文本编辑会改写字符串字节"
		}
		if sec.Runs > 1 {
			sec.Readonly = true
			sec.Reason = fmt.Sprintf("根 %s 的段落不连续（共 %d 段），写入无法唯一定位，请手工编辑", sec.Root, sec.Runs)
		}
		if sec.Root == pluginConfigSelfRoot {
			sec.Readonly = true
			sec.Reason = "本插件自身段落：请到「门闩」页修改；改 admin_listen/token 需 /pm reload hermes_bridge（会短暂断开管理台）"
		}
	}
	return sections, 0, nil
}

var errPluginConfigPreamble = errors.New("首个表头之前存在顶层键值")

// headerLineStart 从键的起始偏移向左回退到表头的行首。
func headerLineStart(data []byte, keyOff int) int {
	i := keyOff
	for i > 0 && (data[i-1] == ' ' || data[i-1] == '\t') {
		i--
	}
	if i > 0 && data[i-1] == '[' {
		i--
		if i > 0 && data[i-1] == '[' {
			i--
		}
	}
	for i > 0 && (data[i-1] == ' ' || data[i-1] == '\t') {
		i--
	}
	return i
}

func hasContent(b []byte) bool {
	for _, line := range bytes.Split(b, []byte{'\n'}) {
		s := strings.TrimSpace(string(line))
		if s != "" && !strings.HasPrefix(s, "#") {
			return true
		}
	}
	return false
}

func eolOf(b []byte) string {
	crlf := bytes.Count(b, []byte("\r\n"))
	lf := bytes.Count(b, []byte{'\n'})
	switch {
	case crlf == 0:
		return "lf"
	case crlf == lf:
		return "crlf"
	default:
		return "mixed"
	}
}

func hasMultiLineString(b []byte) bool {
	return bytes.Contains(b, []byte(`"""`)) || bytes.Contains(b, []byte("'''"))
}

func endLineOf(data []byte, start, end int) int {
	// 去掉段落尾部空行，行号指向最后一行有内容的代码。
	trimmed := bytes.TrimRight(data[start:end], "\r\n")
	if len(trimmed) == 0 {
		return 1 + bytes.Count(data[:start], []byte{'\n'})
	}
	return 1 + bytes.Count(data[:start+len(trimmed)-1], []byte{'\n'})
}

// loadPluginConfig 读取并校验整文件；任何一项不满足都返回只读视图。
func loadPluginConfig() (data []byte, sections []pluginConfigSection, reason string, err error) {
	res := resolvePluginConfigPath()
	if res.path == "" {
		return nil, nil, "", &pluginConfigError{code: "config_not_found", msg: "找不到 plugins/config.toml；已尝试：" + strings.Join(res.tried, "、")}
	}
	info, err := os.Stat(res.path)
	if err != nil {
		return nil, nil, "", &pluginConfigError{code: "config_stat", msg: err.Error()}
	}
	if !info.Mode().IsRegular() {
		return nil, nil, "不是普通文件", nil
	}
	if info.Size() > pluginConfigMaxBytes {
		return nil, nil, fmt.Sprintf("文件 %d 字节，超过本页 %d 字节上限", info.Size(), pluginConfigMaxBytes), nil
	}
	data, err = os.ReadFile(res.path)
	if err != nil {
		return nil, nil, "", &pluginConfigError{code: "config_read", msg: err.Error()}
	}
	if !utf8.Valid(data) {
		return data, nil, "文件不是合法 UTF-8", nil
	}
	sections, preOff, err := splitPluginConfig(data)
	if err != nil {
		return data, nil, "配置文件当前不可解析（host 的热重载此刻也在失败）：" + err.Error(), nil
	}
	if preOff > 0 {
		return data, sections, "首个表头之前存在顶层键值，分段无法唯一定位", nil
	}
	// 根集合自校验：扫描器结果必须与 TOML 解析出的顶层键完全一致，
	// 不一致说明分段不可信 —— 只读，绝不写坏文件。
	var all map[string]pluginConfigProbe
	if err := toml.Unmarshal(data, &all); err != nil {
		return data, sections, "配置文件当前不可解析（host 的热重载此刻也在失败）：" + err.Error(), nil
	}
	if !sameRoots(sections, all) {
		return data, sections, "分段结果与 TOML 解析的顶层键不一致（可能有多行字符串/数组干扰），为避免写坏文件已置为只读", nil
	}
	if len(sections) == 0 {
		return data, nil, "文件里没有插件段", nil
	}
	return data, sections, "", nil
}

func sameRoots(sections []pluginConfigSection, all map[string]pluginConfigProbe) bool {
	if len(all) != len(sections) {
		return false
	}
	for _, s := range sections {
		if _, ok := all[s.Root]; !ok {
			return false
		}
	}
	return true
}

// pluginEnableState 是配置里某个根的启用 / 权限模式状态。
// EnableKnown=false 表示段里没写 enable；ModeKnown=false 表示没写 mode。
type pluginEnableState struct {
	Enabled     bool
	EnableKnown bool
	Mode        string
	ModeKnown   bool
	Limits      int
}

// pluginSectionEnabled 从整份配置里取每个根的 enable / mode / limits 概况。
//
// 只认段里的顶层键（[ai] 下的 enable），不看 [ai.config] 里的同名键。
// Known=false：段里没写 —— host 用 `toml.Unmarshal` 注入，缺键会保留
// main() 里的默认值（通常 enable=true、mode=blacklist），但那个默认值只有
// 插件自己知道，本页不猜，由前端显示成「未知」。
//
// Mode 按 host 的 plugin/permission.go 解释：blacklist = limits 内的会话不可用，
// whitelist = 只有 limits 内的会话可用；其它取值一律放行。所以
// whitelist + limits 为空 = 除主人外谁都用不了，前端要单独标出来。
//
// 这是「配置里写的状态」，不是运行态：host 的 /pm enable 等操作会写回
// 这份文件，所以正常情况下两者一致；但本页读的是文件，不是内存。
func pluginSectionEnabled(data []byte) map[string]pluginEnableState {
	out := map[string]pluginEnableState{}
	if len(data) == 0 {
		return out
	}
	var all map[string]pluginConfigProbe
	if err := toml.Unmarshal(data, &all); err != nil {
		return out
	}
	for root, probe := range all {
		st := pluginEnableState{Limits: len(probe.Limits)}
		if probe.Enable != nil {
			st.Enabled, st.EnableKnown = *probe.Enable, true
		}
		if probe.Mode != nil && *probe.Mode != "" {
			st.Mode, st.ModeKnown = *probe.Mode, true
		}
		out[strings.ToLower(root)] = st
	}
	return out
}

// pluginExecutableNames 只回答「目录里有没有这个插件的 exe」，
// host 没有对外的插件列表能力，运行状态无从得知（UI 会写明）。
func pluginExecutableNames(dir string) map[string]bool {
	out := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	const prefix = "golem_plugin_"
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		name = strings.TrimPrefix(name, prefix)
		if runtime.GOOS == "windows" {
			name = strings.TrimSuffix(name, ".exe")
		}
		out[strings.ToLower(name)] = true
	}
	return out
}

type pluginConfigError struct {
	code string
	msg  string
}

func (e *pluginConfigError) Error() string { return e.msg }

// ---- handlers ----

func (p *BridgePlugin) adminPluginConfigList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")

	res := resolvePluginConfigPath()
	data, sections, reason, err := loadPluginConfig()
	if err != nil {
		var ce *pluginConfigError
		if errors.As(err, &ce) {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok": false, "code": ce.code, "error": ce.msg, "tried": res.tried,
				"hint": "确认 host 的启动目录下有 plugins/config.toml；本页不做创建。",
			})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	type sectionView struct {
		Root        string `json:"root"`
		Run         int    `json:"run"`
		Runs        int    `json:"runs"`
		StartLine   int    `json:"start_line"`
		EndLine     int    `json:"end_line"`
		Bytes       int    `json:"bytes"`
		Tables      int    `json:"tables"`
		Readonly    bool   `json:"readonly"`
		Reason      string `json:"reason"`
		HasExe      bool   `json:"has_exe"`
		Self        bool   `json:"self"`
		Enable      bool   `json:"enable"`
		EnableKnown bool   `json:"enable_known"`
		Mode        string `json:"mode"`
		ModeKnown   bool   `json:"mode_known"`
		Limits      int    `json:"limits"`
	}
	views := make([]sectionView, 0, len(sections))
	exes := pluginExecutableNames(filepath.Dir(res.path))
	enables := pluginSectionEnabled(data)
	for _, s := range sections {
		en := enables[strings.ToLower(s.Root)]
		views = append(views, sectionView{
			Root: s.Root, Run: s.Run, Runs: s.Runs,
			StartLine: s.StartLine, EndLine: s.EndLine,
			Bytes: s.Bytes, Tables: s.Tables,
			Readonly: s.Readonly, Reason: s.Reason,
			HasExe: exes[strings.ToLower(s.Root)],
			Self:   s.Root == pluginConfigSelfRoot,
			Enable: en.Enabled, EnableKnown: en.EnableKnown,
			Mode: en.Mode, ModeKnown: en.ModeKnown, Limits: en.Limits,
		})
	}

	eol := "lf"
	if len(sections) > 0 {
		eol = eolOf(data)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"path":          res.path,
		"resolved_from": res.source,
		"tried":         res.tried,
		"etag":          etagOf(data),
		"size":          len(data),
		"line_count":    1 + bytes.Count(data, []byte{'\n'}),
		"eol":           eol,
		"readonly":      reason != "",
		"reason":        reason,
		"sections":      views,
	})
}

// adminPluginConfigSection 统一接 /admin/plugin-config/<sub>：
//
//	section  按段读写整段原文（兜底）
//	keys     键级读写（表单用，只替换改动键的值字节）
func (p *BridgePlugin) adminPluginConfigSection(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	// 段落用查询参数定位（root 可能含 . / -），路径只保留固定前缀。
	sub := strings.Trim(strings.TrimPrefix(r.URL.Path, "/admin/plugin-config"), "/")
	switch sub {
	case "section":
		switch r.Method {
		case http.MethodGet:
			p.adminPluginConfigGet(w, r)
		case http.MethodPut:
			p.adminPluginConfigPut(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case "keys":
		switch r.Method {
		case http.MethodGet:
			p.adminPluginConfigKeysGet(w, r)
		case http.MethodPut:
			p.adminPluginConfigKeysPut(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	default:
		http.NotFound(w, r)
	}
}

func findSection(sections []pluginConfigSection, root string, run int) (*pluginConfigSection, bool) {
	for i := range sections {
		if sections[i].Root == root && sections[i].Run == run {
			return &sections[i], true
		}
	}
	return nil, false
}

func (p *BridgePlugin) adminPluginConfigGet(w http.ResponseWriter, r *http.Request) {
	root := strings.TrimSpace(r.URL.Query().Get("root"))
	run := queryInt(r, "run", 1)
	res := resolvePluginConfigPath()
	data, sections, reason, err := loadPluginConfig()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sec, ok := findSection(sections, root, run)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "段落不存在：" + root})
		return
	}
	text := string(data[sec.Start:sec.End])
	if sec.EOL == "crlf" {
		text = strings.ReplaceAll(text, "\r\n", "\n")
	}
	w.Header().Set("ETag", etagOf(data))
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"root":       sec.Root,
		"run":        sec.Run,
		"etag":       etagOf(data),
		"path":       res.path,
		"start_line": sec.StartLine,
		"end_line":   sec.EndLine,
		"bytes":      sec.Bytes,
		"tables":     sec.Tables,
		"eol":        sec.EOL,
		"readonly":   sec.Readonly || reason != "",
		"reason":     firstNonEmpty(sec.Reason, reason),
		"text":       text,
	})
}

func (p *BridgePlugin) adminPluginConfigPut(w http.ResponseWriter, r *http.Request) {
	root := strings.TrimSpace(r.URL.Query().Get("root"))
	run := queryInt(r, "run", 1)

	if root == pluginConfigSelfRoot {
		writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "code": "self_section", "error": "本插件自身段落只读；请到「门闩」页修改"})
		return
	}
	if !pluginConfigNameRE.MatchString(root) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "code": "bad_root", "error": "非法段名"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, pluginConfigMaxBody)
	var req struct {
		Etag string `json:"etag"`
		Text string `json:"text"`
	}
	if err := jsonDecode(r.Body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "code": "invalid_json", "error": err.Error()})
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "code": "empty_text", "error": "段内容为空；本页不提供删除段落，请手工编辑 config.toml"})
		return
	}

	pluginConfigMu.Lock()
	defer pluginConfigMu.Unlock()

	res := resolvePluginConfigPath()
	if res.path == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "code": "config_not_found", "error": "找不到 plugins/config.toml", "tried": res.tried})
		return
	}
	data, sections, reason, err := loadPluginConfig()
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "code": "config_unreadable", "error": err.Error()})
		return
	}
	if reason != "" {
		writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "code": "file_readonly", "error": reason})
		return
	}
	sec, ok := findSection(sections, root, run)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "code": "section_not_found", "error": "段落不存在：" + root})
		return
	}
	if sec.Readonly {
		writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "code": "section_readonly", "error": sec.Reason})
		return
	}
	cur := etagOf(data)
	if strings.TrimSpace(req.Etag) == "" || normalizeEtag(req.Etag) != cur {
		writeJSON(w, http.StatusConflict, map[string]any{
			"ok": false, "code": "etag_mismatch", "etag": cur,
			"error": "配置文件已被其它途径（host / /pm 命令 / 手动编辑）修改，请重新载入后再改",
		})
		return
	}

	body, err := normalizeSectionText(req.Text, sec.EOL, sec.End == len(data))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "code": "empty_text", "error": err.Error()})
		return
	}
	if len(body) > pluginConfigMaxSection {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"ok": false, "code": "too_large", "error": fmt.Sprintf("该段 %d 字节，超过 %d 字节上限", len(body), pluginConfigMaxSection)})
		return
	}

	// 校验 1：该段自身必须只含目标根。
	var block map[string]pluginConfigProbe
	if err := toml.Unmarshal(body, &block); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "code": "toml_parse", "line": sec.StartLine + tomlErrorLine(err) - 1,
			"error": "TOML 语法错误：" + err.Error(),
		})
		return
	}
	if len(block) != 1 {
		roots := make([]string, 0, len(block))
		for k := range block {
			roots = append(roots, k)
		}
		sort.Strings(roots)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"ok": false, "code": "root_mismatch",
			"error": fmt.Sprintf("这段应当只包含 [%s]，实际包含：%s", root, strings.Join(roots, "、")),
		})
		return
	}
	if _, ok := block[root]; !ok {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"ok": false, "code": "root_mismatch", "error": "段名被改动：本段必须是 [" + root + "]"})
		return
	}

	// 校验 2：拼回整文件后必须仍可解析，且顶层键集合不变。
	candidate := spliceSection(data, *sec, body)
	var after map[string]pluginConfigProbe
	if err := toml.Unmarshal(candidate, &after); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "code": "file_parse", "error": "改后整个文件无法解析，已放弃写入：" + err.Error()})
		return
	}
	if !sameRoots(sections, after) {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "code": "root_changed", "error": "顶层段集合发生变化（段名被改），已放弃写入"})
		return
	}

	// 校验 3：写前再读一次，缩小与 host saveConfig 的竞态窗口。
	if again, err := os.ReadFile(res.path); err != nil || etagOf(again) != cur {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "code": "etag_mismatch", "error": "写入前文件又被修改，请重新载入"})
		return
	}

	backup, err := writePluginConfigAtomic(res.path, candidate)
	if err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, os.ErrPermission) {
			code = http.StatusServiceUnavailable
		}
		writeJSON(w, code, map[string]any{"ok": false, "code": "write_failed", "error": "写入失败：" + err.Error(), "backup": backup})
		return
	}

	// 写后复核：300ms 内 host 可能整篇重写过（/pm set、插件 SaveConfig）。
	warnings := []string{"host 会在约 300ms 内热重载本段；启动期字段需 /pm reload " + root}
	verified := true
	if back, err := os.ReadFile(res.path); err == nil {
		if !bytes.Equal(back, candidate) {
			verified = false
			warnings = append(warnings, "写入后文件立刻被其它操作重写过（host /pm 命令或插件保存配置），本段改动可能未保留，请刷新确认")
			var backMap map[string]pluginConfigProbe
			if err := toml.Unmarshal(back, &backMap); err == nil && sameRoots(sections, backMap) {
				if a, b := backMap[root], after[root]; fmt.Sprint(a) == fmt.Sprint(b) {
					verified = true
					warnings[len(warnings)-1] = "文件被 host 重写为序列化格式；本段改动已生效，但注释与顺序可能丢失"
				}
			}
		}
	}
	slog.Info("[hermes_bridge] 插件配置已写入", "root", root, "bytes", len(body), "backup", backup, "verified", verified)

	startLine, endLine := sec.StartLine, sec.EndLine
	if newSections, _, err := splitPluginConfig(candidate); err == nil {
		if s, ok := findSection(newSections, root, run); ok {
			startLine, endLine = s.StartLine, s.EndLine
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "root": root, "run": run,
		"etag": etagOf(candidate), "bytes": len(body),
		"start_line": startLine, "end_line": endLine,
		"backup": backup, "verified": verified, "warnings": warnings,
	})
}

// spliceSection 只替换 [Start,End)，前后字节原样保留。
func spliceSection(data []byte, sec pluginConfigSection, body []byte) []byte {
	out := make([]byte, 0, len(data)-(sec.End-sec.Start)+len(body))
	out = append(out, data[:sec.Start]...)
	out = append(out, body...)
	out = append(out, data[sec.End:]...)
	return out
}

// normalizeSectionText 归一化编辑框内容：统一 LF、去尾部空行、
// 非末段补一个空行分隔；原段是 CRLF 就转回 CRLF。
func normalizeSectionText(text, eol string, isLast bool) ([]byte, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.TrimRight(text, "\n")
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("段内容为空；本页不提供删除段落")
	}
	text += "\n"
	if !isLast {
		text += "\n"
	}
	if eol == "crlf" {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	return []byte(text), nil
}

// writePluginConfigAtomic 先备份再 temp+rename。
// 备份放 config_backups/ 子目录：host 的 WalkDir 跳过目录，
// 且备份名不能以 golem_plugin_ 开头，否则会被当成插件扫到。
func writePluginConfigAtomic(path string, data []byte) (string, error) {
	dir := filepath.Dir(path)
	mode := os.FileMode(0o755)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}

	backupName := ""
	if old, err := os.ReadFile(path); err == nil && !bytes.Equal(old, data) {
		bdir := filepath.Join(dir, pluginConfigBackupDir)
		if err := os.MkdirAll(bdir, 0o755); err != nil {
			return "", fmt.Errorf("创建备份目录失败：%w", err)
		}
		backupName = filepath.Join(pluginConfigBackupDir, "config.toml."+time.Now().Format("20060102-150405.000")+".bak")
		if err := writeFileAtomic(filepath.Join(dir, backupName), old, 0o755); err != nil {
			return "", fmt.Errorf("写备份失败（已取消写入）：%w", err)
		}
		prunePluginConfigBackups(bdir)
	}

	if err := writeFileAtomic(path, data, mode); err != nil {
		return backupName, err
	}
	return backupName, nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".config.toml.tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil && runtime.GOOS != "windows" {
		return err
	}
	// Windows 上目标可能被杀软/索引器短暂占用，重试几次；失败时原文件不动。
	var lastErr error
	for i := 0; i < 5; i++ {
		if lastErr = os.Rename(tmpName, path); lastErr == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return lastErr
}

func prunePluginConfigBackups(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type item struct {
		name string
		mod  time.Time
	}
	var items []item
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".bak") {
			continue
		}
		if info, err := e.Info(); err == nil {
			items = append(items, item{name: e.Name(), mod: info.ModTime()})
		}
	}
	if len(items) <= pluginConfigBackupKeep {
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].mod.After(items[j].mod) })
	for _, it := range items[pluginConfigBackupKeep:] {
		_ = os.Remove(filepath.Join(dir, it.name))
	}
}

func normalizeEtag(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "W/")
	return strings.Trim(s, `"`)
}

func queryInt(r *http.Request, key string, def int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return def
	}
	n := 0
	for _, c := range raw {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	if n <= 0 {
		return def
	}
	return n
}

func tomlErrorLine(err error) int {
	var de *toml.DecodeError
	if errors.As(err, &de) {
		row, _ := de.Position()
		return row
	}
	return 1
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func jsonDecode(r io.Reader, v any) error {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// ---- 键级读写（表单用） ----

// adminPluginConfigKeysGet 回某一段的全部叶子键 + 值片段 + 所在表。
// 前端拿 value 做表单初值、raw 做乐观锁、editable 决定能不能改。
func (p *BridgePlugin) adminPluginConfigKeysGet(w http.ResponseWriter, r *http.Request) {
	root := strings.TrimSpace(r.URL.Query().Get("root"))
	run := queryInt(r, "run", 1)
	res := resolvePluginConfigPath()
	data, sections, reason, err := loadPluginConfig()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "tried": res.tried})
		return
	}
	sec, ok := findSection(sections, root, run)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "段落不存在：" + root})
		return
	}

	// 只在段内做键扫描，位置加上段起点即为文件绝对偏移。
	block := data[sec.Start:sec.End]
	sites, tables, scanErr := collectPluginConfigKeySites(block)
	readonly := sec.Readonly || reason != "" || scanErr != nil
	note := firstNonEmpty(sec.Reason, reason)
	if scanErr != nil {
		note = firstNonEmpty(note, "本段键扫描失败："+scanErr.Error())
	}
	for i := range sites {
		sites[i].valStart += sec.Start
		sites[i].valEnd += sec.Start
		sites[i].Line += sec.StartLine - 1
		if readonly && sites[i].Editable {
			sites[i].Editable = false
			sites[i].Reason = firstNonEmpty(note, "本段当前只读")
		}
	}

	w.Header().Set("ETag", etagOf(data))
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"root":       sec.Root,
		"run":        sec.Run,
		"etag":       etagOf(data),
		"path":       res.path,
		"start_line": sec.StartLine,
		"end_line":   sec.EndLine,
		"eol":        sec.EOL,
		"readonly":   readonly,
		"reason":     note,
		"self":       sec.Root == pluginConfigSelfRoot,
		"keys":       sites,
		"tables":     tables,
	})
}

// adminPluginConfigKeysPut 逐键替换值字节：N 个键 = 一次原子写 = 一份备份 = 一个 etag。
func (p *BridgePlugin) adminPluginConfigKeysPut(w http.ResponseWriter, r *http.Request) {
	root := strings.TrimSpace(r.URL.Query().Get("root"))
	run := queryInt(r, "run", 1)

	if root == pluginConfigSelfRoot {
		writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "code": "self_section", "error": "本插件自身段落只读；请到「门闩」页修改"})
		return
	}
	if !pluginConfigNameRE.MatchString(root) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "code": "bad_root", "error": "非法段名"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, pluginConfigMaxBody)
	var req struct {
		Etag  string                   `json:"etag"`
		Edits []pluginConfigKeyEditReq `json:"edits"`
	}
	if err := jsonDecode(r.Body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "code": "invalid_json", "error": err.Error()})
		return
	}

	pluginConfigMu.Lock()
	defer pluginConfigMu.Unlock()

	res := resolvePluginConfigPath()
	if res.path == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "code": "config_not_found", "error": "找不到 plugins/config.toml", "tried": res.tried})
		return
	}
	data, sections, reason, err := loadPluginConfig()
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "code": "config_unreadable", "error": err.Error()})
		return
	}
	if reason != "" {
		writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "code": "file_readonly", "error": reason})
		return
	}
	sec, ok := findSection(sections, root, run)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "code": "section_not_found", "error": "段落不存在：" + root})
		return
	}
	if sec.Readonly {
		writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "code": "section_readonly", "error": sec.Reason})
		return
	}
	cur := etagOf(data)
	if strings.TrimSpace(req.Etag) == "" || normalizeEtag(req.Etag) != cur {
		writeJSON(w, http.StatusConflict, map[string]any{
			"ok": false, "code": "etag_mismatch", "etag": cur,
			"error": "配置文件已被其它途径（host / /pm 命令 / 手动编辑）修改，请重新载入后再改",
		})
		return
	}

	// 键站点在整文件上采集：identity 里含表路径，段内采集也一样，
	// 但整文件采集能顺带校验「键确实属于这一段」。
	allSites, _, scanErr := collectPluginConfigKeySites(data)
	if scanErr != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "code": "scan_failed", "error": "键扫描失败：" + scanErr.Error()})
		return
	}
	sites := make([]pluginConfigKeySite, 0, len(allSites))
	for _, s := range allSites {
		if s.valStart >= sec.Start && s.valEnd <= sec.End {
			sites = append(sites, s)
		}
	}

	out, applied, editErr := applyPluginConfigKeyEdits(data, sites, req.Edits)
	if editErr != nil {
		editErr.write(w)
		return
	}

	// 闸门 1：整文件必须仍可解析，顶层段集合不变。
	var after map[string]pluginConfigProbe
	if err := toml.Unmarshal(out, &after); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "code": "file_parse", "error": "改后整个文件无法解析，已放弃写入：" + err.Error()})
		return
	}
	if !sameRoots(sections, after) {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "code": "root_changed", "error": "顶层段集合发生变化，已放弃写入"})
		return
	}
	// 闸门 2：除被改的键之外，所有叶子值必须完全不变（抓 splice 错位改到邻居）。
	if err := verifyPluginConfigKeyEdits(data, out, req.Edits); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"ok": false, "code": "verify_failed", "error": "写入前校验未通过，已放弃：" + err.Error()})
		return
	}

	// 闸门 3：写前再读一次，缩小与 host saveConfig 的竞态窗口。
	if again, err := os.ReadFile(res.path); err != nil || etagOf(again) != cur {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "code": "etag_mismatch", "error": "写入前文件又被修改，请重新载入"})
		return
	}

	backup, err := writePluginConfigAtomic(res.path, out)
	if err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, os.ErrPermission) {
			code = http.StatusServiceUnavailable
		}
		writeJSON(w, code, map[string]any{"ok": false, "code": "write_failed", "error": "写入失败：" + err.Error(), "backup": backup})
		return
	}

	warnings := []string{"host 会在约 300ms 内热重载本段；启动期字段需 /pm reload " + root}
	verified := true
	if back, err := os.ReadFile(res.path); err == nil && !bytes.Equal(back, out) {
		verified = false
		warnings = append(warnings, "写入后文件立刻被其它操作重写过（host /pm 命令或插件保存配置），本次改动可能未保留，请刷新确认")
	}
	for _, a := range applied {
		if a.OldRaw != a.NewRaw {
			slog.Info("[hermes_bridge] 插件配置键已改", "root", root, "key", a.ID, "old", a.OldRaw, "new", a.NewRaw)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "root": root, "run": run,
		"etag": etagOf(out), "count": len(applied),
		"applied": applied, "backup": backup,
		"verified": verified, "warnings": warnings,
	})
}
