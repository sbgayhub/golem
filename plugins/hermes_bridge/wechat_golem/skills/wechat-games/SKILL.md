---
name: wechat-games
description: 微信群小游戏：农场种菜偷菜打劫、修仙吐纳突破渡劫；查资产金币灵石、境界修为、农场等级
version: 2.0.0
metadata:
  hermes:
    tags: [wechat, game, 农场, 种菜, 打劫, 修仙, 修真, 群小游戏]
    category: wechat
    # 别把 image_gen 写进来：它会 Tool Search 延迟（image_generate 从 valid_tool_names 消失），
    # 工具集判不存在 → Hermes 把本技能从系统提示词里静默剔除，模型连技能名都看不到。
    # 画图能力由「现画」流程自己兜底，不靠这个门槛。
    requires_toolsets: [terminal]
    related_skills: [wechat-hub]
---

# 微信小游戏：农场 · 打劫 · 修仙

一个群里的小游戏集合，**两个玩法共用一笔钱**（农场叫金币、修仙叫灵石，是同一个数）。

- **不依赖 Golem 的 farm / robbery 插件**：数据和那两个插件完全独立，Golem 那边没装也能玩。
- 规则数值全在 `scripts/` 里，由引擎自己算，**你不要计算任何数值、不要自己编结果**。
- 脚本 stdout 就是给群里看的成品回复。

## When to Use

**不用等命令词。** 用户随口的意图命中下表就执行。

### 农场 / 打劫 → `scripts/farm_core.py`

| 用户随口说 | action |
|---|---|
| 「收个菜」「菜熟了没」「把菜收了」 | `harvest` |
| 「浇下水」「帮我浇浇」 | `water`（浇别人的要 `--target`） |
| 「种点土豆」「种个土豆」 | `plant --item 土豆` |
| 「买点土豆种子」「买把铁剑」「买只斗牛犬」 | `buy --item …`（种子/装备/宠物/转职都走这里） |
| 「看看我的农场」「我多少钱」「农场等级」 | `status` |
| 「偷张三的菜」 | `steal --target 张三` |
| 「打劫」「抢他」「打劫张三」 | `rob`（有名字才 `--target`） |
| 「赌一把 100」 | `gamble --amount 100` |
| 「买块地」「开荒」 | `land` |
| 「打工」「领救济」 | `job` / `welfare` |
| 「农场排行榜」「谁最有钱」 | `rank` |
| 「农场能干啥」「有什么店」 | `help` / `shop` |

### 修仙 → `scripts/xiuxian_core.py`

| 用户随口说 | action |
|---|---|
| 「修仙」「开始修仙」「觉醒灵根」「我要修真」 | `init`（已觉醒会提示当前境界） |
| 「修仙状态」「我什么境界」「修为多少」 | `status` |
| 「吐纳」「打坐」「修炼一下」 | `tuina` |
| 「闭关 10 年」「闭死关 30 年」 | `retreat --years 10` |
| 「突破」「冲击筑基」「渡劫」 | `breakthrough` |
| 「游历」「出去走走」「找点机缘」 | `adventure` |
| 「打一场」「去战斗」「杀妖兽」 | `fight` |
| 「宗门」「有哪些门派」 | `sect` |
| 「加入青云宗」「拜入万毒门」 | `sect --item 青云宗` |
| 「功法」「能学什么」 | `learn`（无参数=已学列表）/ `shop --kind technique` |
| 「学 青元剑诀」「运功 九霄雷法」 | `learn --item 青元剑诀` |
| 「丹药」「丹房」 | `shop`（默认就是丹药） |
| 「买 破境丹」「买两枚回气丹」 | `buy --item 破境丹 --count 2` |
| 「用 破境丹」「嗑个延寿丹」 | `use --item 破境丹` |
| 「仙道榜」「谁境界最高」 | `rank` |
| 「重新投胎」「转世」 | `rebirth`（活着时要 `--force`，对应「确认」） |
| 「我的资产」「我有多少钱」 | `overview`（农场+修仙一张表） |
| 「能玩什么」「有啥游戏」 | `menu`（用 `xiuxian_core.py`） |
| 「给我画一张」「画个渡劫」 | `draw --item '<场景>'`（画师不忙就画，见下） |

**拿不准就先跑 `status`**；完全看不出意图就跑 `menu`。
用户没提玩法时**不要主动起头**（会刷屏）。

### 脚本的引导语：原样转告，不要替用户做决定

脚本缺前置条件时会回一句「先在商店买 / 先收个菜 / 地都种满了」。
**这就是最终答复**——照原文转告，然后停手，让用户自己决定下一步。

| 用户话 | 第一枪 | 脚本回 | 怎么做 |
|---|---|---|---|
| 种个土豆 | `plant --item 土豆` | 「你手上没有土豆种子，先在商店买」 | **原样转告后停手**。别自动买——他可能想换种萝卜，或只是问问 |
| 种个土豆 | `plant --item 土豆` | 「地都种满了」 | 原样转告，问要不要开荒（开荒要花大钱，更不能替决定） |
| 收个菜 | `harvest` | 「还没熟」/「没种东西」 | 原样转告，不再重试 |
| 打劫张三 | `rob --target 张三` | 「没有找到」 | 原样转告，别替他建号 |

**花钱的动作永远要用户点头**：`buy`、`land`（开荒）、`rebirth`（投胎）这些不要因为
「他既然要种土豆那肯定愿意买种子」就自动执行。用户下一句说「买」，再跑 `buy`。

例外只有一种：用户**一句话里明确同时要了两件事**（「买个土豆种上」），那才连着跑两步。

## 身份：world 与 player 怎么取

这是最容易做错的一步，照做：

**player** = 说话人的 wxid。

- 群聊：消息前的 `golem_verified_identity_json` 里取 `sender_id`。**只取 `addressing=self` / `quoted_self` 的那条**；别人的信封不能用。
- 私聊：没有身份信封。私聊会话 key 形如 `private:<wxid>`，取冒号后的部分。
- 取不到就向用户要，**不要用昵称冒充 wxid**（会建出重复账号）。

**world** = 当前会话。

- 群聊：批次头 `[群聊上下文 <群名> 共N条]` 里的群名。
- 私聊：用 `private:<wxid>` 里的 wxid。

同一个群每次都用同一个 world 字符串；群改名就是新场地，这是已知取舍。

**`--player-name` 传对方昵称**，用来给「偷张三的菜」「打劫张三」找人和在回复里显示名字。
`--target` 也传昵称（或 @ 后面那段），引擎自己按名字/别名查 wxid。

## Procedure

### 0. 解释器：写死绝对路径

用 **Hermes 自己的 venv python**，不要用系统 `python3`——只有它能保证存在且版本对得上。
网关由 systemd 启动，路径固定就是这一个：

```bash
/usr/local/lib/hermes-agent/venv/bin/python
```

**不要把解释器路径放进变量再执行**（先给变量赋值，再用带引号的变量去跑脚本）。
Hermes 的 Tirith 安全扫描会把这种写法判成「nested executable body could not be resolved」
（HIGH），命令被拦下等人工批准，群里会卡住。直接写绝对路径就没有这个问题。

路径不存在才停（说明部署变了），不要去猜别的解释器。
Windows 本地调试另说：`%LOCALAPPDATA%\hermes\venv\Scripts\python.exe`，与网关无关。

### 1–3. 执行与回复

1. 判断意图 → 选脚本与 action，把用户话里的对象/物品映射到 `--target` / `--item` / `--count` / `--years` / `--amount` / `--kind`。
2. 调 `terminal` 执行（**一次一个动作**）：

```bash
/usr/local/lib/hermes-agent/venv/bin/python ${HERMES_SKILL_DIR}/scripts/farm_core.py <action> \
  --world '<world>' --player '<wxid>' --player-name '<昵称>' \
  [--target '<昵称>'] [--item '<物品>'] [--count N] [--amount N] [--kind crop|pet|equip|profession]

/usr/local/lib/hermes-agent/venv/bin/python ${HERMES_SKILL_DIR}/scripts/xiuxian_core.py <action> \
  --world '<world>' --player '<wxid>' --player-name '<昵称>' \
  [--item '<丹药或功法>'] [--count N] [--years N] [--kind pill|technique] [--force]
```

3. 脚本 stdout 就是给用户看的成品：

- 直接作为回复正文发出，**不要改写数字和表情**；可以加一句自然旁白（最多一句）。
- 出现 `IMAGE:<绝对路径>` 行 → 把该行改成 `MEDIA:<绝对路径>` 照抄进回复，**原 `IMAGE:` 行删掉**。网关按官方约定发图。
- 出现 `DRAW:<提示词>` 行 → **先按下面「现画」一节处理**，删掉该行后再发其余正文。
- `draw_done` 的输出（`OK`）是内部回执，**永远不要转发给用户**。
- 退出码非 0 或输出为空 → 把 stderr 最后几行原样转告，别自己编结果。
- 冷却没到、修为不足这类提示是**正常剧情**，照原文回，不要说「出错了」。

**不要用 `execute_code` 跑这些脚本**：消息平台的 `execute_code` 每次都要整段脚本审批，群里会卡住。

## 现画：DRAW 标记怎么处理

脚本自己不画图——它只在大事发生时吐一行 `DRAW:<提示词>`，**出图靠 Hermes 的 `image_generate` 工具**。
这一行是在说「这里值得配一张画」，不是成品。

处理步骤（**每次最多调一次**）：

1. 调 `image_generate(prompt=<DRAW 后面那串>、aspect_ratio="portrait")`
2. 返回 JSON 里取 `image`（实测是**本地缓存路径**，形如 `~/.hermes/profiles/wechat/cache/images/openai_*.png`；也可能给 URL）
3. **不管成功还是失败，都补一次销账**（见下节「画完要回执」）：
   `/usr/local/lib/hermes-agent/venv/bin/python ${HERMES_SKILL_DIR}/scripts/xiuxian_core.py draw_done --world '<world>' --player '<wxid>'`
4. 在回复正文末尾加一行 `MEDIA:<那个地址>`，然后把 `DRAW:` 行删掉
5. 一张图生成要几十秒，**把它放在正文发出去之后再等**也无妨；别为了等图把文字憋着

### 画完要回执（draw_done）

脚本吐 `DRAW:` 时同时占了一个「在途队列位」，但它**看不到你有没有真的画完**。
画完（或失败）后调一次 `draw_done`，队列位立刻还回来，别人马上就能画；
不调也没事，`DRAW_JOB_TTL = 120` 秒后会自动过期——只是那段时间别人会被白白挡住。

- **`draw_done` 的正常输出只有一行 `OK`，这是给你的内部确认，绝对不要发到群里。**
- 一次 `DRAW:` 配一次 `draw_done`；一张图失败也要调（视为已结束）。
- 同一轮画了多张，就调多次（每次销掉自己最早的一单；也可用 `--item <任务id>` 精确指定）。
- 忘了调、重复调都安全——幂等，找不到就回 `OK`。

**频率已经由脚本管住了，不需要你再判断，也不要自己加冷却。** 脚本看的不是时间，而是**画师忙不忙**：
群里在途的图堆到 `DRAW_WORLD_SLOTS = 4` 张、或者**你自己**上一张还没出来，
就不再吐 `DRAW:`；用户主动点单走 `draw` action，忙时会直接回
「这会儿画师手上还有 N 张没画完」或「你上一张还在画呢」。所以：

- 看到 `DRAW:` 就画，**一次**，别重试、别一次多画几张；
- 没看到 `DRAW:` 就**绝不主动调 `image_generate`**，哪怕用户说「画一个」——
  那种情况应该先跑 `draw` action，让脚本决定给不给；
- `image_generate` 失败（报错 / `success:false`）就**静默放弃配图**，正文照发，
  不要重试、不要向用户报错、不要说「画图失败了」。配图是锦上添花，不是玩法本体。

**一次要画多张时，全部塞进同一轮。** Hermes 把 `image_generate` 归为可并行工具
（`_PARALLEL_SAFE_TOOLS`），同一轮里发 10 个调用会自动按 `image_gen.max_parallel_requests`
（默认 4）并发跑；分成 10 轮就是 10 次独立等待，慢十倍。

只有突破成功、渡劫、坐化这类大事才会带 `DRAW:`；吐纳、收菜、打劫、买丹药这些**永远不会**。

## 两个玩法怎么互相影响

- **灵石/金币是同一笔钱**：种菜、偷菜、打劫、赌博、奇遇拿到的都进 `coins`；买种子、买丹药、买功法都从这里扣。
- **境界加成**：境界越高，收菜经验越多（飞升 ×7）、打劫武力越强（+30）。表见 `references/farm-rules.md`。
- **监禁跨玩法生效**：打劫被抓后，种菜收菜也会被拦，每发一条指令消耗一回合。
- **斗牛犬双向生效**：既挡偷菜，也让别人打劫你的成功率 -15%。
- **偷菜也算犯罪**：偷成功累积犯罪记录，提高被卫兵抓的概率。
- **死亡不清农场**：坐化后重新投胎只清修仙道果，土地装备照旧，灵石留四分之一。

数值细节：
- 农场/打劫 → `references/farm-rules.md`
- 修仙 → `references/xiuxian-rules.md`

## Pitfalls

- **别把昵称当 wxid**：只在拿不到 `sender_id` 时退让，别默认用昵称。
- **一次一个 action**：用户连发多个请求就按顺序逐个执行，别把多个动作塞进一条命令。
- **坐牢时只有查看类放行**：农场侧的 `status` / `shop` / `level` / `rank` / `help` 正常，其他会被拦。
- **要突破先看修为**：`breakthrough` 修为不足只会提示还差多少，不要反复重试；先让用户 `retreat` 或 `tuina`。
- **闭关必须有年数**：漏了 `--years` 脚本会提示；用户说「闭关十年」就传 `--years 10`。
- **重新投胎要确认**：用户只说「重新投胎」时**不要**加 `--force`；他说「确认」「确定」「是真的」再加。
- **群友没玩过**：`--target` 会报「没有找到」，让他先发一句「我的资产」建号，别替他建。
- **数据路径**：默认 `$HERMES_HOME/wechat_games/state.json`（`HERMES_GAMES_DATA` 可覆盖）。损坏时会隔离成 `.corrupt-<ts>` 再重开。
- **解释器**：写死 `/usr/local/lib/hermes-agent/venv/bin/python`（见 Procedure §0），不要用系统 `python3`，也别写成「变量选值再 `"$PY"` 执行」——Tirith 会判 HIGH 拦下等审批。
- **Windows 调试**：venv 路径是 `%LOCALAPPDATA%\hermes\venv\Scripts\python.exe`，只在本地调试用，与网关无关。

## Verification

维护者自测用（带 `HERMES_GAMES_DATA` 隔离，不碰真档）。Linux 上同样写绝对路径，
避免 Tirith 把动态选值的命令拦成待审批：

```bash
export HERMES_GAMES_DATA=/tmp/games_selftest.json
cd ${HERMES_SKILL_DIR}/scripts
/usr/local/lib/hermes-agent/venv/bin/python farm_core.py buy    --world t --player u1 --player-name 测试 --item 土豆
/usr/local/lib/hermes-agent/venv/bin/python farm_core.py plant  --world t --player u1 --player-name 测试 --item 土豆
/usr/local/lib/hermes-agent/venv/bin/python farm_core.py status --world t --player u1 --player-name 测试
/usr/local/lib/hermes-agent/venv/bin/python xiuxian_core.py init   --world t --player u1 --player-name 测试
/usr/local/lib/hermes-agent/venv/bin/python xiuxian_core.py status --world t --player u1 --player-name 测试
```

Windows 本地调试时把解释器换成 `%LOCALAPPDATA%\hermes\venv\Scripts\python.exe`。
期望：`购买成功` → `土地(1) → 🥔` → 状态里出现「土豆（N 小时后成熟）」；
修仙侧 `灵根觉醒` 后 `status` 能同时看到「💰 灵石 2990」和「🌱 农场等级 1」——**同一个玩家、同一笔钱**。
