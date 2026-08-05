package main

// appendSession 追加一条消息到会话滚动上下文（超限截旧）
func (p *HermesPlugin) appendSession(key string, msg chatMessage) {
	if key == "" || msg.Content == "" {
		return
	}
	limit := p.configSnapshot().MaxContextMessages
	if limit <= 0 {
		limit = 40
	}
	p.sessMu.Lock()
	defer p.sessMu.Unlock()
	items := append(p.sessions[key], msg)
	if len(items) > limit {
		items = items[len(items)-limit:]
	}
	p.sessions[key] = items
}

// sessionMessages 获取会话上下文副本
func (p *HermesPlugin) sessionMessages(key string) []chatMessage {
	p.sessMu.Lock()
	defer p.sessMu.Unlock()
	items := p.sessions[key]
	if len(items) == 0 {
		return nil
	}
	return append([]chatMessage(nil), items...)
}

// clearSession 清空会话上下文
func (p *HermesPlugin) clearSession(key string) {
	p.sessMu.Lock()
	defer p.sessMu.Unlock()
	delete(p.sessions, key)
}

// sessionKeyOf 由会话 wxid 推导会话 key
func sessionKeyOf(targetID string) string {
	if isChatroomID(targetID) {
		return "chatroom:" + targetID
	}
	return "private:" + targetID
}
