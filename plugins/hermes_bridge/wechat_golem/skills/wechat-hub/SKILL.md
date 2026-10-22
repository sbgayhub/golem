---
name: wechat-hub
description: 微信技能目录。对不上 platform_hint【技能路由】表、或要新建/拆分微信技能时再加载；不是每轮必加载的总控。
version: 1.1.0
metadata:
  hermes:
    tags: [wechat, router, skill-governance]
    related_skills: [wechat-content-sharing, wechat-voice-sending, music-sourcing, wechat-sticker-management, wechat-games, wechat-group-behavior, cron-wechat-bridge]
---

# 微信任务目录

平台 `platform_hint` 的【技能路由】表才是每轮可见的路由。本技能不是每轮门禁，只在对不上表或要变更技能时加载。
系统、安全约束和用户当前指令始终高于本技能。

## 什么时候加载本技能

- 用户意图对不上 hint 路由表，需要查目录或 `references/`。
- 主人明确授权新建、拆分、合并或更新微信专属技能（先读 `references/CREATION_RULES.md`）。

不要为发语音、发图、点歌、闲聊应景表情而加载本技能。

## 路由备忘（与 platform_hint 对齐）

| 用户意图 | 做什么 |
|---|---|
| 闲聊应景贴表情 | **不加载技能**，直接 `wechat_sticker_send(mood=情绪核词)` |
| 找图、发图、视频、GIF、网页截图 | `skill_view wechat-content-sharing` |
| 发语音、念一句、指定音色 | `skill_view wechat-voice-sending` |
| 点歌、音乐卡片 | `skill_view music-sourcing` |
| 点名收藏/查找/发特定表情 | `skill_view wechat-sticker-management` |
| 群聊发言时机、社交边界 | 仅确实需要时 `skill_view wechat-group-behavior` |
| 天气 | `skill_view weather-forecast` |
| 定时任务 | `skill_view cron-wechat-bridge` |
| 种菜、收菜、偷菜、打劫、赌博、农场等级 | `skill_view wechat-games` |
| 修仙、修真、吐纳、闭关、突破、渡劫、灵根、宗门、丹药 | `skill_view wechat-games` |
| 新建/拆分/更新微信技能 | 本技能的 `references/CREATION_RULES.md` |

### 未命中时

- 先看当前 system prompt 的技能索引；仍不明确时只调用一次 `skills_list`。
- 找到匹配技能后再 `skill_view`，不要仅凭技能名猜流程。
- 没有匹配技能不等于必须创建新技能。

## 公共执行约束

- 以当前会话路由确定发送目标，不从记忆复制群 ID、用户 ID 或历史目标。
- 具体子技能是执行流程的唯一真源；记忆只提供长期偏好。
- 媒体找不到就明确说明，不用表情、占位链接或其他内容冒充。
- 不使用记忆中的旧接口、临时 endpoint、历史 job ID 或事故补丁。
- 涉及外发、删除、覆盖、重启、付费或其他难以撤销操作时，遵守既有审批要求。
- 需要完整公共契约时，读取 `references/COMMON_CONTRACT.md`。
