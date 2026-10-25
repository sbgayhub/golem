package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// 群 / 联系人缓存读取：运行目录下 data/chatroom.json 与 data/contact.json。
//
// 这两个文件由 host 自己维护：启动时读入（host 里 contact.go / chatroom.go），
// 退出或刷新时整份重写。所以本页给的是「缓存快照」，不是实时数据——UI 必须标时间。
//
// 分工：
//   - chatroom.json 有群全集 + 每个群的成员明细（但没有群名）；
//   - contact.json 有群名 / 群主 / 好友（type: 2 好友、3 群、4/5 公众号与系统）。
//
// 群全集取两者并集，名字只来自 contact.json。

const (
	pluginDataMaxBytes = 8 << 20
	pluginDataChatroom = "chatroom.json"
	pluginDataContact  = "contact.json"
)

type pluginDataFile struct {
	Path    string `json:"path"`
	ModTime string `json:"mtime,omitempty"`
	Bytes   int64  `json:"bytes"`
	Exists  bool   `json:"exists"`
	Error   string `json:"error,omitempty"`
}

type pluginDataPerson struct {
	Username  string `json:"username"`
	Nickname  string `json:"nickname"`
	Remark    string `json:"remark"`
	Alias     string `json:"alias"`
	Avatar    string `json:"avatar"`
	Type      int    `json:"type"`
	Owner     string `json:"owner,omitempty"`
	OwnerName string `json:"owner_name,omitempty"`
}

type pluginDataGroup struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Owner       string `json:"owner"`
	OwnerName   string `json:"owner_name"`
	MemberCount int    `json:"member_count"`
	InChatroom  bool   `json:"in_chatroom_cache"`
}

type pluginDataMember struct {
	Username        string `json:"username"`
	Nickname        string `json:"nickname"`
	DisplayName     string `json:"display_name"`
	Remark          string `json:"remark"`
	Alias           string `json:"alias"`
	Avatar          string `json:"avatar"`
	Gender          string `json:"gender"`
	Country         string `json:"country"`
	Province        string `json:"province"`
	City            string `json:"city"`
	Signature       string `json:"signature"`
	InviterUsername string `json:"inviter_username"`
	Flag            int    `json:"flag"`
	IsOwner         bool   `json:"is_owner"`
	InContact       bool   `json:"in_contact_cache"`
}

// 原始 JSON 结构：字段名与 host 落盘的完全一致。
type rawContactEntry struct {
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Remark   string `json:"remark"`
	Alias    string `json:"alias"`
	Avatar   string `json:"avatar"`
	Type     int    `json:"type"`
	Data     struct {
		Chatroom struct {
			Owner string `json:"owner"`
		} `json:"Chatroom"`
	} `json:"Data"`
}

type rawMember struct {
	Username        string `json:"username"`
	Nickname        string `json:"nickname"`
	DisplayName     string `json:"display_name"`
	Remark          string `json:"remark"`
	Alias           string `json:"alias"`
	Avatar          string `json:"avatar"`
	Gender          string `json:"gender"`
	Country         string `json:"country"`
	Province        string `json:"province"`
	City            string `json:"city"`
	Signature       string `json:"signature"`
	InviterUsername string `json:"inviter_username"`
	Flag            int    `json:"flag"`
}

// resolvePluginDataDir 依次探测 data/：cwd、exe 同目录、exe 上级、exe 上级的 plugins/。
// 与 resolvePluginConfigPath 同款思路，但目标换成数据目录。
func resolvePluginDataDir() (dir string, tried []string) {
	candidates := []string{filepath.Join("data")}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(exeDir, "data"),
			filepath.Join(filepath.Dir(exeDir), "data"),
			filepath.Join(filepath.Dir(exeDir), "plugins", "data"),
		)
	}
	for _, c := range candidates {
		abs, err := filepath.Abs(c)
		if err != nil {
			abs = c
		}
		tried = append(tried, abs)
		if dir != "" {
			continue
		}
		if info, err := os.Stat(filepath.Join(abs, pluginDataChatroom)); err == nil && info.Mode().IsRegular() {
			dir = abs
		} else if info, err := os.Stat(filepath.Join(abs, pluginDataContact)); err == nil && info.Mode().IsRegular() {
			dir = abs
		}
	}
	return dir, tried
}

// readPluginDataFile 读一个数据文件并给出快照信息；不存在不算错误。
func readPluginDataFile(dir, name string) (pluginDataFile, []byte) {
	f := pluginDataFile{Path: filepath.Join(dir, name)}
	if dir == "" {
		return f, nil
	}
	info, err := os.Stat(f.Path)
	if err != nil {
		f.Error = err.Error()
		return f, nil
	}
	if info.Size() > pluginDataMaxBytes {
		f.Error = "文件过大，本页不读"
		return f, nil
	}
	data, err := os.ReadFile(f.Path)
	if err != nil {
		f.Error = err.Error()
		return f, nil
	}
	f.Exists = true
	f.Bytes = info.Size()
	f.ModTime = info.ModTime().Format(time.RFC3339)
	return f, data
}

func loadChatroomCache(dir string) (map[string]map[string]rawMember, pluginDataFile) {
	f, data := readPluginDataFile(dir, pluginDataChatroom)
	if data == nil {
		return nil, f
	}
	var m map[string]map[string]rawMember
	if err := json.Unmarshal(data, &m); err != nil {
		f.Error = "解析失败：" + err.Error()
		return nil, f
	}
	return m, f
}

func loadContactCache(dir string) (map[string]rawContactEntry, pluginDataFile) {
	f, data := readPluginDataFile(dir, pluginDataContact)
	if data == nil {
		return nil, f
	}
	var m map[string]rawContactEntry
	if err := json.Unmarshal(data, &m); err != nil {
		f.Error = "解析失败：" + err.Error()
		return nil, f
	}
	return m, f
}

func contactDisplayName(e rawContactEntry) string {
	if e.Remark != "" {
		return e.Remark
	}
	if e.Nickname != "" {
		return e.Nickname
	}
	if e.Alias != "" {
		return e.Alias
	}
	return e.Username
}

// buildRecipients 合并两份缓存：群全集取并集，名字/群主只来自 contact.json。
func buildRecipients(chatrooms map[string]map[string]rawMember, contacts map[string]rawContactEntry) ([]pluginDataGroup, []pluginDataPerson) {
	byID := map[string]*pluginDataGroup{}
	ensure := func(id string) *pluginDataGroup {
		if g, ok := byID[id]; ok {
			return g
		}
		g := &pluginDataGroup{ID: id}
		byID[id] = g
		return g
	}

	for id, members := range chatrooms {
		g := ensure(id)
		g.InChatroom = true
		g.MemberCount = len(members)
	}
	for id, c := range contacts {
		if c.Type != 3 || id == "" {
			continue
		}
		g := ensure(id)
		g.Name = c.Nickname
		if g.Name == "" {
			g.Name = c.Remark
		}
		g.Owner = c.Data.Chatroom.Owner
	}
	for id, g := range byID {
		if g.Name == "" {
			g.Name = id
		}
		if g.Owner != "" {
			if owner, ok := contacts[g.Owner]; ok {
				g.OwnerName = contactDisplayName(owner)
			}
		}
	}

	groups := make([]pluginDataGroup, 0, len(byID))
	for _, g := range byID {
		groups = append(groups, *g)
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].MemberCount != groups[j].MemberCount {
			return groups[i].MemberCount > groups[j].MemberCount
		}
		return groups[i].Name < groups[j].Name
	})

	friends := make([]pluginDataPerson, 0, len(contacts))
	for id, c := range contacts {
		// type 2 才是好友；4/5 是公众号与系统号，这里不列。
		if c.Type != 2 || id == "" {
			continue
		}
		friends = append(friends, pluginDataPerson{
			Username: id, Nickname: c.Nickname, Remark: c.Remark,
			Alias: c.Alias, Avatar: c.Avatar, Type: c.Type,
		})
	}
	sort.Slice(friends, func(i, j int) bool {
		return contactDisplayName(rawContactEntry{Nickname: friends[i].Nickname, Remark: friends[i].Remark, Alias: friends[i].Alias, Username: friends[i].Username}) <
			contactDisplayName(rawContactEntry{Nickname: friends[j].Nickname, Remark: friends[j].Remark, Alias: friends[j].Alias, Username: friends[j].Username})
	})
	return groups, friends
}

// ---- handlers ----

// adminDataRecipients 供设置台的 limits 选择器与白名单页使用：群全集 + 好友。
func (p *BridgePlugin) adminDataRecipients(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	dir, tried := resolvePluginDataDir()
	if dir == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "code": "data_not_found", "tried": tried,
			"error": "找不到 data/chatroom.json 与 data/contact.json",
			"hint":  "确认 host 的启动目录下有 data/；本页只读缓存，不创建文件。",
		})
		return
	}
	chatrooms, chatFile := loadChatroomCache(dir)
	contacts, contactFile := loadContactCache(dir)
	groups, friends := buildRecipients(chatrooms, contacts)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"dir":      dir,
		"tried":    tried,
		"groups":   groups,
		"contacts": friends,
		"files":    map[string]any{"chatroom": chatFile, "contact": contactFile},
	})
}

// adminDataMemberGroups 反查若干成员各自在哪些群（扫 chatroom.json 的成员表）。
//
// 档案页用它在列表里标「N 群」、在详情里列出群名。参数是 wxid 列表
// （?wxids=a,b,c，最多 200 个），一次请求批量返回，避免每人一次往返。
//
// 注意只有 chatroom.json 里有成员明细的群才查得到；contact.json 里有、但
// 缓存里没有成员表的群（host 没刷新过）不参与匹配，前端会标「缓存里没有成员表」。
func (p *BridgePlugin) adminDataMemberGroups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	raw := strings.TrimSpace(r.URL.Query().Get("wxids"))
	if raw == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "缺少 wxids 参数"})
		return
	}
	wanted := map[string]bool{}
	order := make([]string, 0, 32)
	for _, part := range strings.Split(raw, ",") {
		id := strings.TrimSpace(part)
		if id == "" || wanted[id] {
			continue
		}
		if len(order) >= 200 {
			break
		}
		wanted[id] = true
		order = append(order, id)
	}
	dir, tried := resolvePluginDataDir()
	if dir == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "code": "data_not_found", "tried": tried, "error": "找不到 data/"})
		return
	}
	chatrooms, chatFile := loadChatroomCache(dir)
	contacts, contactFile := loadContactCache(dir)

	out := make(map[string][]pluginDataGroup, len(order))
	for id := range wanted {
		out[id] = []pluginDataGroup{}
	}
	for gid, members := range chatrooms {
		var name string
		if c, ok := contacts[gid]; ok {
			name = c.Nickname
			if name == "" {
				name = c.Remark
			}
		}
		if name == "" {
			name = gid
		}
		for uid := range members {
			if !wanted[uid] {
				continue
			}
			out[uid] = append(out[uid], pluginDataGroup{
				ID: gid, Name: name, MemberCount: len(members), InChatroom: true,
			})
		}
	}
	for uid, list := range out {
		sort.Slice(list, func(i, j int) bool {
			if list[i].MemberCount != list[j].MemberCount {
				return list[i].MemberCount > list[j].MemberCount
			}
			return list[i].Name < list[j].Name
		})
		out[uid] = list
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "groups": out,
		"files": map[string]any{"chatroom": chatFile, "contact": contactFile},
	})
}

// adminDataChatrooms 群列表（不带成员明细，170 人的群也不必整份传）。
func (p *BridgePlugin) adminDataChatrooms(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	dir, tried := resolvePluginDataDir()
	if dir == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "code": "data_not_found", "tried": tried, "error": "找不到 data/"})
		return
	}
	chatrooms, chatFile := loadChatroomCache(dir)
	contacts, contactFile := loadContactCache(dir)
	groups, _ := buildRecipients(chatrooms, contacts)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "dir": dir, "groups": groups,
		"files": map[string]any{"chatroom": chatFile, "contact": contactFile},
	})
}

// adminDataChatroomMembers 单个群的成员明细，按 contact.json 补备注与微信号。
func (p *BridgePlugin) adminDataChatroomMembers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	id := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/admin/data/chatrooms/"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "缺少群 ID"})
		return
	}
	dir, _ := resolvePluginDataDir()
	if dir == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "code": "data_not_found", "error": "找不到 data/"})
		return
	}
	chatrooms, chatFile := loadChatroomCache(dir)
	contacts, contactFile := loadContactCache(dir)

	raw, inCache := chatrooms[id]
	var owner string
	if c, ok := contacts[id]; ok {
		owner = c.Data.Chatroom.Owner
	}
	members := make([]pluginDataMember, 0, len(raw))
	for uid, m := range raw {
		mm := pluginDataMember{
			Username: m.Username, Nickname: m.Nickname, DisplayName: m.DisplayName,
			Remark: m.Remark, Alias: m.Alias, Avatar: m.Avatar,
			Gender: m.Gender, Country: m.Country, Province: m.Province, City: m.City,
			Signature: m.Signature, InviterUsername: m.InviterUsername, Flag: m.Flag,
			IsOwner: owner != "" && uid == owner,
		}
		if mm.Username == "" {
			mm.Username = uid
		}
		if c, ok := contacts[uid]; ok {
			mm.InContact = true
			if mm.Remark == "" {
				mm.Remark = c.Remark
			}
			if mm.Alias == "" {
				mm.Alias = c.Alias
			}
			if mm.Nickname == "" {
				mm.Nickname = c.Nickname
			}
			if mm.Avatar == "" {
				mm.Avatar = c.Avatar
			}
		}
		members = append(members, mm)
	}
	sort.Slice(members, func(i, j int) bool {
		if members[i].IsOwner != members[j].IsOwner {
			return members[i].IsOwner
		}
		return memberSortName(members[i]) < memberSortName(members[j])
	})

	name := ""
	if c, ok := contacts[id]; ok {
		name = c.Nickname
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "id": id, "name": name, "owner": owner,
		"in_chatroom_cache": inCache,
		"member_count":      len(members),
		"members":           members,
		"files":             map[string]any{"chatroom": chatFile, "contact": contactFile},
	})
}

func memberSortName(m pluginDataMember) string {
	for _, s := range []string{m.Remark, m.DisplayName, m.Nickname, m.Username} {
		if s != "" {
			return s
		}
	}
	return ""
}
