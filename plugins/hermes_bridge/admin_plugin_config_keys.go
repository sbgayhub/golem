package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

// 「Golem 插件设置台」的键级读写：保存时只替换被改动键的值字节，其余字节
// （注释、顺序、表单没显示的键）逐字节保留。代价是只能改「已存在的值」，
// 不能增删键/元素/段——那类操作回落到整段原文编辑。
//
// go-toml v2.3.1 的坑（已逐源码核实 unstable/parser.go）：
//   - KeyValue.Raw 覆盖整个 `key = value`，不含尾随空白与行尾注释；
//   - Key.Raw 是完整 token（含引号）；
//   - String / Integer / Float 的 Node.Raw 精确；
//   - Bool 的节点没有 Raw；Array / InlineTable 的 Raw 只有开括号。
//
// 所以值区间只能用「KeyValue.Raw 右端 + 最后一个 Key.Raw 右端」反推，
// 绝不能用 value 节点的 Raw。反推失败就标成不可编辑，不猜。

// pluginConfigKeySeg 是键路径的一段。Index < 0 表示普通键；
// Index >= 0 表示该段是 [[数组表]] 的第 Index 个实例（0 基）。
type pluginConfigKeySeg struct {
	Key   string `json:"k"`
	Index int    `json:"i"`
}

// pluginConfigKeySite 是一个键在文件里的字节位置与展示信息。
type pluginConfigKeySite struct {
	ID              string               `json:"id"`
	Segs            []pluginConfigKeySeg `json:"segs"`
	Table           string               `json:"table"`
	Line            int                  `json:"line"`
	Kind            string               `json:"kind"`
	Raw             string               `json:"raw"`
	Value           any                  `json:"value"`
	Editable        bool                 `json:"editable"`
	Reason          string               `json:"reason,omitempty"`
	HasInnerComment bool                 `json:"has_inner_comment,omitempty"`

	valStart int
	valEnd   int
}

// pluginConfigKeyTable 描述一个数组表（前端据此生成「第 N 条」标题）。
type pluginConfigKeyTable struct {
	Path       []string `json:"path"`
	Array      bool     `json:"array"`
	Count      int      `json:"count"`
	ElementIDs []string `json:"element_ids"`
}

// 一次保存最多改这么多键，防止超大请求把管理台卡住。
const pluginConfigKeyEditMax = 200

// ---------- 键身份 ----------

// isBareKey 判断能否不加引号直接写进 TOML 键路径。
func isBareKey(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

// identityOf 把路径拼成前端用的不透明主键：
// a.b / config.prompts["群成员"] / config.rules[0].url
func identityOf(segs []pluginConfigKeySeg) string {
	var b strings.Builder
	for i, s := range segs {
		if i > 0 {
			b.WriteByte('.')
		}
		switch {
		case s.Index >= 0:
			b.WriteString(s.Key)
			b.WriteByte('[')
			b.WriteString(strconv.Itoa(s.Index))
			b.WriteByte(']')
		case isBareKey(s.Key):
			b.WriteString(s.Key)
		default:
			b.WriteByte('[')
			b.WriteString(strconv.Quote(s.Key))
			b.WriteByte(']')
		}
	}
	return b.String()
}

func copySegs(in []pluginConfigKeySeg) []pluginConfigKeySeg {
	out := make([]pluginConfigKeySeg, len(in))
	copy(out, in)
	return out
}

// nodeKeyNames 取表头/键值里各段键名。调用方必须已确认 Kind 可调 Key()，
// 且必须在 NextExpression 之后立刻调用（节点指针跨表达式失效）。
func nodeKeyNames(n *unstable.Node) []string {
	var names []string
	it := n.Key()
	for it.Next() {
		k := it.Node()
		if k == nil {
			continue
		}
		names = append(names, string(k.Data))
	}
	return names
}

// nodeLastKeyEnd 返回最后一个 Key 节点的字节右端。
func nodeLastKeyEnd(n *unstable.Node) (int, bool) {
	end := -1
	it := n.Key()
	for it.Next() {
		k := it.Node()
		if k == nil || k.Raw.Length == 0 {
			continue
		}
		end = int(k.Raw.Offset + k.Raw.Length)
	}
	return end, end > 0
}

// ---------- 值区间 ----------

// keyValueSpan 从 KeyValue.Raw 右端与最后一个 Key.Raw 右端反推值区间。
// 任何一步不自洽都返回 false —— 宁可不可编辑，也不能写坏文件。
func keyValueSpan(data []byte, kv *unstable.Node) (int, int, bool) {
	end := int(kv.Raw.Offset + kv.Raw.Length)
	keyEnd, ok := nodeLastKeyEnd(kv)
	if !ok || end <= 0 || end > len(data) || keyEnd > end {
		return 0, 0, false
	}
	i := keyEnd
	for i < end && (data[i] == ' ' || data[i] == '\t') {
		i++
	}
	// '=' 不能从 kv.Raw 起点往后搜：带引号的 key 里可能有 '='。
	if i >= end || data[i] != '=' {
		return 0, 0, false
	}
	i++
	for i < end && (data[i] == ' ' || data[i] == '\t') {
		i++
	}
	start := i
	if start >= end {
		return 0, 0, false
	}
	// 交叉校验：String / Integer / Float 的值节点 Raw 必须与反推区间完全一致。
	if v := kv.Value(); v != nil && v.Raw.Length > 0 {
		if int(v.Raw.Offset) != start || int(v.Raw.Offset+v.Raw.Length) != end {
			return 0, 0, false
		}
	}
	return start, end, true
}

func kindOfNode(k unstable.Kind) string {
	switch k {
	case unstable.String:
		return "string"
	case unstable.Integer:
		return "integer"
	case unstable.Float:
		return "float"
	case unstable.Bool:
		return "bool"
	case unstable.Array:
		return "array"
	case unstable.InlineTable:
		return "inline_table"
	case unstable.LocalDate, unstable.LocalTime, unstable.LocalDateTime, unstable.DateTime:
		return "datetime"
	default:
		return "unknown"
	}
}

// sliceHasComment 判断值区间里是否有「字符串外」的 # 注释。
// 只有 Array / InlineTable 才关心：整体替换会丢掉数组内的注释。
func sliceHasComment(b []byte) bool {
	inBasic, inLiteral, escaped := false, false, false
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch {
		case escaped:
			escaped = false
		case inBasic:
			if c == '\\' {
				escaped = true
			} else if c == '"' {
				inBasic = false
			}
		case inLiteral:
			if c == '\'' {
				inLiteral = false
			}
		case c == '"':
			inBasic = true
		case c == '\'':
			inLiteral = true
		case c == '#':
			return true
		}
	}
	return false
}

// ---------- 采集 ----------

// collectPluginConfigKeySites 顺序扫一遍整个文件，产出所有叶子键的值区间与路径。
// 必须在整份文件能 toml.Unmarshal 的前提下调用。
func collectPluginConfigKeySites(data []byte) ([]pluginConfigKeySite, []pluginConfigKeyTable, error) {
	var ps unstable.Parser
	ps.Reset(data)

	var sites []pluginConfigKeySite
	var cur []pluginConfigKeySeg
	// arrayPrefix 记录哪些前缀是数组表；arrayIdx 记录该前缀当前是第几个实例。
	arrayPrefix := map[string]bool{}
	arrayIdx := map[string]int{}

	insts := map[string][]string{} // 数组表前缀（不带序号）→ 各实例 id
	var instOrder []string

	for ps.NextExpression() {
		n := ps.Expression()
		switch n.Kind {
		case unstable.Table, unstable.ArrayTable:
			names := nodeKeyNames(n)
			if len(names) == 0 {
				continue
			}
			segs := make([]pluginConfigKeySeg, 0, len(names))
			for _, name := range names {
				seg := pluginConfigKeySeg{Key: name, Index: -1}
				probe := append(copySegs(segs), seg)
				if arrayPrefix[identityOf(probe)] {
					seg.Index = arrayIdx[identityOf(probe)]
				}
				segs = append(segs, seg)
			}
			if n.Kind == unstable.ArrayTable {
				// TOML 规定 [[a.b]] 之后的 [a.b.c] 属于最后一个 a.b 实例：
				// 数组表前缀用「不带序号」的形式存，序号按出现顺序递增。
				last := len(segs) - 1
				probe := copySegs(segs)
				probe[last].Index = -1
				instKey := identityOf(probe)
				if _, ok := insts[instKey]; !ok {
					instOrder = append(instOrder, instKey)
				}
				arrayPrefix[instKey] = true
				idx := len(insts[instKey])
				arrayIdx[instKey] = idx
				segs[last].Index = idx
				insts[instKey] = append(insts[instKey], identityOf(segs))
			}
			cur = segs

		case unstable.KeyValue:
			names := nodeKeyNames(n)
			if len(names) == 0 {
				continue
			}
			full := copySegs(cur)
			for _, name := range names {
				full = append(full, pluginConfigKeySeg{Key: name, Index: -1})
			}
			start, end, ok := keyValueSpan(data, n)
			site := pluginConfigKeySite{
				ID:       identityOf(full),
				Segs:     full,
				Table:    identityOf(cur),
				Kind:     kindOfNode(n.Value().Kind),
				Editable: true,
				valStart: start,
				valEnd:   end,
			}
			if !ok {
				site.Editable = false
				site.Reason = "值位置无法确定（解析器偏移不自洽），请用原文编辑"
				sites = append(sites, site)
				continue
			}
			site.Raw = string(data[start:end])
			site.Line = 1 + bytes.Count(data[:start], []byte{'\n'})
			if site.Kind == "array" || site.Kind == "inline_table" {
				site.HasInnerComment = sliceHasComment(data[start:end])
			}
			sites = append(sites, site)
		}
	}
	if err := ps.Error(); err != nil {
		return nil, nil, err
	}

	// 同名键被赋值多次 → 两份都不可编辑（写哪份都可能不是生效的那份）。
	count := map[string]int{}
	for _, s := range sites {
		count[s.ID]++
	}
	for i := range sites {
		if count[sites[i].ID] > 1 {
			sites[i].Editable = false
			sites[i].Reason = "同名键在文件里被赋值多次，无法确定要改哪一处，请用原文编辑"
			continue
		}
		if sites[i].Editable {
			// 片段重解析：抓多行值截断、字符串里含 # 一类的偏移错误。
			v, err := parsePluginConfigFragment(sites[i].Raw)
			if err != nil {
				sites[i].Editable = false
				sites[i].Reason = "值片段无法单独解析，请用原文编辑"
				continue
			}
			sites[i].Value = v
		}
	}

	tables := make([]pluginConfigKeyTable, 0, len(instOrder))
	for _, key := range instOrder {
		ids := insts[key]
		var path []string
		for _, s := range parseKeyID(ids[0]) {
			path = append(path, s.Key)
		}
		tables = append(tables, pluginConfigKeyTable{
			Path: path, Array: true, Count: len(ids), ElementIDs: ids,
		})
	}
	return sites, tables, nil
}

// parsePluginConfigFragment 单独解析一个值片段；返回解码后的值。
func parsePluginConfigFragment(raw string) (any, error) {
	var m map[string]any
	if err := toml.Unmarshal([]byte("v = "+raw+"\n"), &m); err != nil {
		return nil, err
	}
	return m["v"], nil
}

// ---------- 值编码 ----------

func kindOfGoValue(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "bool"
	case int64:
		return "integer"
	case float64:
		return "float"
	case []any:
		return "array"
	case map[string]any:
		return "inline_table"
	default:
		return "unknown"
	}
}

// encodePluginConfigValue 用 go-toml 自己编码，避免手写转义。
// 编码后必须 round-trip，且类型与旧值一致——integer 写成 string/float 会让
// host 整次 toml.Unmarshal 失败，用户只会看到「保存成功却没生效」。
func encodePluginConfigValue(v any, oldKind string) ([]byte, error) {
	if oldKind == "float" {
		// JSON 的 50 解出来是整数，但 TOML 原值是浮点；写成整数会让目标字段变成 int64。
		if i, ok := v.(int64); ok {
			v = float64(i)
		}
	}
	b, err := toml.Marshal(map[string]any{"v": v})
	if err != nil {
		return nil, fmt.Errorf("编码失败：%w", err)
	}
	prefix := []byte("v = ")
	if !bytes.HasPrefix(b, prefix) {
		return nil, errors.New("该值不是单行标量（嵌套内联表请用原文编辑）")
	}
	body := bytes.TrimSuffix(b[len(prefix):], []byte("\n"))
	if len(body) == 0 {
		return nil, errors.New("编码结果为空")
	}
	got, err := parsePluginConfigFragment(string(body))
	if err != nil {
		return nil, fmt.Errorf("编码结果无法解析：%w", err)
	}
	if k := kindOfGoValue(got); k != oldKind {
		return nil, fmt.Errorf("类型不允许改变：原值 %s，新值会被写成 %s", oldKind, k)
	}
	return body, nil
}

// decodeEditValue 把 JSON 值转成 TOML 可编码的 Go 值，并保留整数精度。
func decodeEditValue(raw json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return normalizeJSONNumbers(v)
}

func normalizeJSONNumbers(v any) (any, error) {
	switch t := v.(type) {
	case json.Number:
		if i, err := strconv.ParseInt(string(t), 10, 64); err == nil {
			return i, nil
		}
		f, err := strconv.ParseFloat(string(t), 64)
		if err != nil {
			return nil, fmt.Errorf("数字 %s 无法解析", t)
		}
		return f, nil
	case []any:
		out := make([]any, len(t))
		for i := range t {
			nv, err := normalizeJSONNumbers(t[i])
			if err != nil {
				return nil, err
			}
			out[i] = nv
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(t))
		for k := range t {
			nv, err := normalizeJSONNumbers(t[k])
			if err != nil {
				return nil, err
			}
			out[k] = nv
		}
		return out, nil
	default:
		return v, nil
	}
}

// ---------- 编辑应用 ----------

type pluginConfigKeyEditReq struct {
	ID               string          `json:"id"`
	ExpectRaw        string          `json:"expect_raw"`
	Value            json.RawMessage `json:"value"`
	AllowCommentLoss bool            `json:"allow_comment_loss"`
}

type pluginConfigKeyEditResult struct {
	ID     string `json:"id"`
	Line   int    `json:"line"`
	OldRaw string `json:"old_raw"`
	NewRaw string `json:"new_raw"`
}

// pluginConfigKeyEditError 是可直接回给前端的结构化错误。
type pluginConfigKeyEditError struct {
	Code       string `json:"code"`
	Error      string `json:"error"`
	ID         string `json:"id,omitempty"`
	CurrentRaw string `json:"current_raw,omitempty"`
	Hint       string `json:"hint,omitempty"`
	HTTP       int    `json:"-"`
}

func (e *pluginConfigKeyEditError) write(w http.ResponseWriter) {
	writeJSON(w, e.HTTP, map[string]any{
		"ok": false, "code": e.Code, "error": e.Error,
		"id": e.ID, "current_raw": e.CurrentRaw, "hint": e.Hint,
	})
}

// applyPluginConfigKeyEdits 在已采集的站点上做字节 splice；其余校验由调用方负责。
func applyPluginConfigKeyEdits(data []byte, sites []pluginConfigKeySite, edits []pluginConfigKeyEditReq) ([]byte, []pluginConfigKeyEditResult, *pluginConfigKeyEditError) {
	if len(edits) == 0 {
		return nil, nil, &pluginConfigKeyEditError{Code: "no_edits", Error: "没有要保存的改动", HTTP: http.StatusBadRequest}
	}
	if len(edits) > pluginConfigKeyEditMax {
		return nil, nil, &pluginConfigKeyEditError{Code: "too_many_edits", Error: fmt.Sprintf("一次最多改 %d 个键", pluginConfigKeyEditMax), HTTP: http.StatusBadRequest}
	}
	byID := make(map[string]*pluginConfigKeySite, len(sites))
	for i := range sites {
		if _, dup := byID[sites[i].ID]; !dup {
			byID[sites[i].ID] = &sites[i]
		}
	}

	type planned struct {
		site *pluginConfigKeySite
		body []byte
	}
	plan := make([]planned, 0, len(edits))
	seen := map[string]bool{}
	for _, e := range edits {
		id := normalizeKeyID(e.ID)
		if id == "" {
			return nil, nil, &pluginConfigKeyEditError{Code: "bad_id", Error: "edit.id 为空", HTTP: http.StatusBadRequest}
		}
		if seen[id] {
			return nil, nil, &pluginConfigKeyEditError{Code: "duplicate_edit", Error: "同一个键提交了多次", ID: id, HTTP: http.StatusBadRequest}
		}
		seen[id] = true
		site, ok := byID[id]
		if !ok {
			return nil, nil, &pluginConfigKeyEditError{
				Code: "unknown_key", Error: "配置文件里找不到这个键", ID: id,
				Hint: "本页只能改已存在的值；新增键或元素请用原文编辑", HTTP: http.StatusUnprocessableEntity,
			}
		}
		if !site.Editable {
			return nil, nil, &pluginConfigKeyEditError{Code: "key_readonly", Error: firstNonEmpty(site.Reason, "该键不可编辑"), ID: id, HTTP: http.StatusUnprocessableEntity}
		}
		if e.ExpectRaw != site.Raw {
			return nil, nil, &pluginConfigKeyEditError{
				Code: "key_changed", Error: "该键已被其它途径修改，请重新载入", ID: id,
				CurrentRaw: site.Raw, HTTP: http.StatusConflict,
			}
		}
		if site.HasInnerComment && !e.AllowCommentLoss {
			return nil, nil, &pluginConfigKeyEditError{
				Code: "comment_loss", Error: "该值是含注释的多行数组/内联表，整体替换会丢掉注释",
				ID: id, Hint: "确认继续请带 allow_comment_loss", HTTP: http.StatusUnprocessableEntity,
			}
		}
		v, err := decodeEditValue(e.Value)
		if err != nil {
			return nil, nil, &pluginConfigKeyEditError{Code: "bad_value", Error: err.Error(), ID: id, HTTP: http.StatusBadRequest}
		}
		body, err := encodePluginConfigValue(v, site.Kind)
		if err != nil {
			return nil, nil, &pluginConfigKeyEditError{Code: "type_changed", Error: err.Error(), ID: id, HTTP: http.StatusUnprocessableEntity}
		}
		plan = append(plan, planned{site: site, body: body})
	}

	// 降序拼接：后面的偏移不受前面替换影响。
	sort.Slice(plan, func(i, j int) bool { return plan[i].site.valStart > plan[j].site.valStart })
	for i := 1; i < len(plan); i++ {
		if plan[i].site.valEnd > plan[i-1].site.valStart {
			return nil, nil, &pluginConfigKeyEditError{Code: "overlapping_edits", Error: "改动区间重叠", HTTP: http.StatusUnprocessableEntity}
		}
	}
	out := data
	results := make([]pluginConfigKeyEditResult, 0, len(plan))
	for _, p := range plan {
		out = append(append(append([]byte{}, out[:p.site.valStart]...), p.body...), out[p.site.valEnd:]...)
		results = append(results, pluginConfigKeyEditResult{
			ID: p.site.ID, Line: p.site.Line, OldRaw: p.site.Raw, NewRaw: string(p.body),
		})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Line < results[j].Line })
	return out, results, nil
}

// verifyPluginConfigKeyEdits 写前最后一道闸：除被改的键之外，所有叶子值必须完全不变。
// 它抓的是「splice 偏移错位改了邻居」这类最难自查的错。
func verifyPluginConfigKeyEdits(before, after []byte, edits []pluginConfigKeyEditReq) error {
	var bMap, aMap map[string]any
	if err := toml.Unmarshal(before, &bMap); err != nil {
		return fmt.Errorf("原文件解析失败：%w", err)
	}
	if err := toml.Unmarshal(after, &aMap); err != nil {
		return fmt.Errorf("改后文件解析失败：%w", err)
	}
	bLeaves := map[string]any{}
	aLeaves := map[string]any{}
	flattenPluginConfigValues(nil, bMap, bLeaves)
	flattenPluginConfigValues(nil, aMap, aLeaves)

	want := make(map[string]any, len(bLeaves))
	for k, v := range bLeaves {
		want[k] = v
	}
	for _, e := range edits {
		id := normalizeKeyID(e.ID)
		np, ok := lookupPluginConfigValue(aMap, id)
		if !ok {
			return fmt.Errorf("改后找不到键 %s", id)
		}
		want[id] = np
	}
	if len(want) != len(aLeaves) {
		return errors.New("改后键集合发生变化")
	}
	for k, v := range want {
		av, ok := aLeaves[k]
		if !ok {
			return fmt.Errorf("改后缺少键 %s", k)
		}
		if !reflect.DeepEqual(v, av) {
			return fmt.Errorf("键 %s 被意外改动（%v → %v）", k, v, av)
		}
	}
	return nil
}

// flattenPluginConfigValues 把解码后的嵌套结构拍平成「键身份 → 叶子值」。
// 数组里全是 map 才当数组表继续下钻，否则当标量数组（如 limits）整体作为一个叶子。
func flattenPluginConfigValues(prefix []pluginConfigKeySeg, v any, out map[string]any) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			next := append(copySegs(prefix), pluginConfigKeySeg{Key: k, Index: -1})
			flattenPluginConfigValues(next, child, out)
		}
	case []any:
		if len(t) == 0 || !isArrayOfTables(t) {
			out[identityOf(prefix)] = t
			return
		}
		last := prefix[len(prefix)-1]
		for i, child := range t {
			next := append(copySegs(prefix[:len(prefix)-1]), pluginConfigKeySeg{Key: last.Key, Index: i})
			flattenPluginConfigValues(next, child, out)
		}
	default:
		out[identityOf(prefix)] = v
	}
}

func isArrayOfTables(arr []any) bool {
	for _, v := range arr {
		if _, ok := v.(map[string]any); !ok {
			return false
		}
	}
	return true
}

// normalizeKeyID 目前只做 TrimSpace，保留函数以便以后加别名。
func normalizeKeyID(id string) string { return strings.TrimSpace(id) }

// lookupPluginConfigValue 按 id 在解码后的 map 里取值。
func lookupPluginConfigValue(root map[string]any, id string) (any, bool) {
	segs := parseKeyID(id)
	if len(segs) == 0 {
		return nil, false
	}
	var cur any = root
	for _, s := range segs {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[s.Key]
		if !ok {
			return nil, false
		}
		// 序号是作用在「这个键上」的：rules[1].x 先取 rules 数组再取第 1 个。
		if s.Index >= 0 {
			arr, ok := cur.([]any)
			if !ok || s.Index >= len(arr) {
				return nil, false
			}
			cur = arr[s.Index]
		}
	}
	return cur, true
}

// parseKeyID 解析 identityOf 生成的主键。
func parseKeyID(id string) []pluginConfigKeySeg {
	var segs []pluginConfigKeySeg
	i := 0
	for i < len(id) {
		if id[i] == '.' {
			i++
			continue
		}
		if id[i] == '[' && i+1 < len(id) && id[i+1] == '"' {
			j := i + 2
			var sb strings.Builder
			for j < len(id) {
				if id[j] == '\\' && j+1 < len(id) {
					j++
					sb.WriteByte(id[j])
					j++
					continue
				}
				if id[j] == '"' {
					break
				}
				sb.WriteByte(id[j])
				j++
			}
			if j+1 >= len(id) || id[j] != '"' || id[j+1] != ']' {
				return nil
			}
			segs = append(segs, pluginConfigKeySeg{Key: sb.String(), Index: -1})
			i = j + 2
			continue
		}
		j := i
		for j < len(id) && id[j] != '.' && id[j] != '[' {
			j++
		}
		if j == i {
			return nil
		}
		seg := pluginConfigKeySeg{Key: id[i:j], Index: -1}
		i = j
		if i < len(id) && id[i] == '[' {
			k := i + 1
			for k < len(id) && id[k] != ']' {
				k++
			}
			n, err := strconv.Atoi(id[i+1 : k])
			if err != nil || n < 0 {
				return nil
			}
			seg.Index = n
			i = k + 1
		}
		segs = append(segs, seg)
	}
	return segs
}
