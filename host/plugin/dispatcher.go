package plugin

import (
	"fmt"
	"log/slog"
	"slices"

	"github.com/duke-git/lancet/v2/strutil"
	"github.com/sbgayhub/golem/sdk/plugin"
)

var events = make(chan *plugin.Event, 100) // 事件通道

func Publish(e *plugin.Event) {
	events <- e
	slog.Debug("事件发布完成", "topic", e.Topic)
}

// dispatcher 事件分发循环
func dispatcher() {
	for e := range events {
		slog.Debug("消费事件", "topic", e.Topic)

		for _, p := range pluginSnapshot() {
			// 跳过禁用的插件
			if p.Config != nil && !p.Config.Enable {
				continue
			}

			// 检查事件主题是否匹配插件订阅
			if !strutil.HasPrefixAny(e.Topic, p.subscriptions) {
				continue
			}

			// 检查发送者是否被允许
			if e.Sender != "" && !isAllowed(e.Sender, p) {
				continue
			}

			// 会话劫持检查：只有劫持插件和 AlwaysRun 插件允许接收
			if e.Sender != "" && !isSessionAllowed(e.Sender, p.Metadata) {
				continue
			}

			// 安全调用插件 OnEvent，捕获 panic
			func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("插件处理事件时发生崩溃", "plugin", p.Name, "error", r)
					}
				}()

				if res, err := (*p.eventPlugin).OnEvent(e); err != nil {
					slog.Error("插件处理事件失败", "plugin", p.Name, "res", res, "err", err)
				}

				// 事件分发成功，刷新会话时间
				if e.Sender != "" && isSessionActive(e.Sender) && p.Name == getSessionPlugin(e.Sender) {
					refreshSession(e.Sender)
				}
			}()
		}
	}
}

// DispatchCommand 分发命令给插件
func DispatchCommand(cmd *plugin.Command, plugins []*wrapper) {
	for _, p := range plugins {
		if p.Config != nil && !p.Config.Enable {
			continue
		}

		if !slices.Contains(p.commands, cmd.Main) {
			continue
		}

		sender := ""
		if cmd.Sender != nil {
			sender = cmd.Sender.GetUsername()
		}
		if sender != "" && !isAllowed(sender, p) {
			continue
		}

		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("插件处理命令时发生崩溃", "plugin", p.Name, "error", r)
				}
			}()

			if _, err := (*p.commandPlugin).OnCommand(cmd); err != nil {
				errMsg := fmt.Sprintf("插件[%s]处理命令失败: %v", p.Name, err)
				slog.Error(errMsg)
			}
		}()
	}
}
