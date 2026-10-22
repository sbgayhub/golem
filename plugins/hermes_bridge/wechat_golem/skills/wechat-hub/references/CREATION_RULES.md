# 微信技能创建与维护规范

仅在用户明确要求，或当前流程已经稳定、可复用且值得长期维护时，创建微信专属技能。

## 存放与命名

1. 微信专属技能统一使用 `wechat-` 前缀。
2. 默认目录为 `skills/social-media/wechat-<topic>/SKILL.md`；确属其他领域时可放对应 category，但名称仍保留 `wechat-` 前缀。
3. `name` 必须唯一；创建前先用 `skills_list` 和目录索引检查重复或近义技能。
4. 不要在 frontmatter 的 `platforms` 中填写 `wechat` 或 `wechat_golem`。该字段表示操作系统，只能用于 `linux`、`macos`、`windows` 等宿主过滤。

## Frontmatter

至少包含：

```yaml
---
name: wechat-<topic>
description: 用户在微信中提出哪些意图时应加载；说明能力边界，不写实现历史。
version: 1.0.0
metadata:
  hermes:
    tags: [wechat, <topic>]
    related_skills: [wechat-hub]
---
```

`description` 必须使用意图语言，覆盖用户会说的中文请求；不要只写工具名、库名或“使用说明”。

## 内容边界

- `SKILL.md` 只保留当前有效流程、约束、失败处理和验证方法。
- API 大表、长模板和背景资料放进该技能自己的 `references/`，按需读取。
- 不记录会话事故、临时 endpoint、job ID、短期 provider 状态或“本次纠正”。
- 微信公共投递规则引用 `wechat-hub/references/COMMON_CONTRACT.md`，不在每个子技能复制。
- 一个领域只保留一个权威技能；发现重叠时优先合并或明确归档旧技能。

## 路由登记事务

创建或改变微信技能路由时，必须在同一任务中更新 `wechat-hub/SKILL.md` 的路由表：

1. 添加或修改一行“用户意图 → 唯一技能名”。
2. 必要时更新 `metadata.hermes.related_skills`。
3. 确认总控技能仍保持短小，不把子技能正文复制进来。
4. 新技能与路由登记必须同时成功；只完成一项视为未完成。
5. 至少准备一条新会话触发样例，验证首次实质回复前会加载目标技能。

## 修改后的检查

- frontmatter 可解析且 `name` 与目录职责一致；
- `description` 能匹配真实中文意图；
- 没有旧 IP、旧 MCP 路径或重复流程；
- 路由只加载必要技能，不形成循环加载；
- 变更只对新 session 或 system prompt 重建后的 session 生效。
