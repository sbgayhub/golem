#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""修仙引擎：境界 / 灵根 / 吐纳闭关 / 突破渡劫 / 奇遇战斗 / 宗门功法 / 丹药法宝。

与农场共用 games_core 的存档与灵石：种菜赚的灵石可以买丹药，渡劫失败的伤可以用
灵药恢复。规则原创，数值参考公开的修仙文字游戏常见设计（境界九阶、灵根六品、
突破有失败率、寿元是硬约束）。

用法（每次调用执行一个动作，stdout 直接作为微信回复）：

    python3 xiuxian_core.py init     --world 群A --player wxid_x --player-name 老彭 --root auto
    python3 xiuxian_core.py status   --world 群A --player wxid_x
    python3 xiuxian_core.py tuina    --world 群A --player wxid_x
    python3 xiuxian_core.py retreat  --world 群A --player wxid_x --years 5
    python3 xiuxian_core.py breakthrough --world 群A --player wxid_x
    python3 xiuxian_core.py adventure  --world 群A --player wxid_x
    python3 xiuxian_core.py fight    --world 群A --player wxid_x
    python3 xiuxian_core.py sect     --world 群A --player wxid_x --item 青云宗
    python3 xiuxian_core.py learn    --world 群A --player wxid_x --item 太虚吐纳诀
    python3 xiuxian_core.py shop     --world 群A --player wxid_x --kind pill|technique
    python3 xiuxian_core.py buy      --world 群A --player wxid_x --item 破境丹 --count 1
    python3 xiuxian_core.py use      --world 群A --player wxid_x --item 破境丹
    python3 xiuxian_core.py rank     --world 群A
"""

import argparse
import os
import random
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import games_core as core
from games_core import Game, ambiguous, fmt_duration, now_ts  # noqa: E402

# ---------------------------------------------------------------- 数据表

# 境界：名字 / 所需修为 / 突破基础成功率 / 寿元加成 / 战力量级
REALMS = [
    {"name": "凡人",   "qi": 0,        "break_base": 1.00, "life": 0,    "power": 3},
    {"name": "淬体",   "qi": 100,      "break_base": 0.90, "life": 20,   "power": 8},
    {"name": "聚气",   "qi": 350,      "break_base": 0.80, "life": 40,   "power": 18},
    {"name": "筑基",   "qi": 1000,     "break_base": 0.70, "life": 80,   "power": 40},
    {"name": "金丹",   "qi": 3000,     "break_base": 0.60, "life": 200,  "power": 90},
    {"name": "元婴",   "qi": 8000,     "break_base": 0.50, "life": 500,  "power": 200},
    {"name": "化神",   "qi": 20000,    "break_base": 0.42, "life": 1000, "power": 450},
    {"name": "炼虚",   "qi": 50000,    "break_base": 0.35, "life": 2000, "power": 1000},
    {"name": "合道",   "qi": 120000,   "break_base": 0.28, "life": 4000, "power": 2200},
    {"name": "渡劫",   "qi": 300000,   "break_base": 0.20, "life": 8000, "power": 5000},
    {"name": "大乘",   "qi": 700000,   "break_base": 0.15, "life": 15000, "power": 11000},
    {"name": "飞升",   "qi": 1500000,  "break_base": 0.10, "life": 30000, "power": 26000},
]
MAX_REALM = len(REALMS) - 1

# 灵根：投胎决定起点，洗髓丹可重roll
ROOTS = {
    "废灵根": {"mult": 0.5, "enlighten": 0,  "weight": 30, "desc": "先天极弱，修行如逆水行舟"},
    "下品灵根": {"mult": 1.0, "enlighten": 1,  "weight": 30, "desc": "资质平平，胜在踏实"},
    "中品灵根": {"mult": 1.3, "enlighten": 3,  "weight": 22, "desc": "小有天赋，可入宗门"},
    "上品灵根": {"mult": 1.6, "enlighten": 5,  "weight": 12, "desc": "万里挑一，宗门争抢"},
    "天灵根": {"mult": 2.0, "enlighten": 8,  "weight": 5,  "desc": "千年一遇，天道眷顾"},
    "圣灵根": {"mult": 3.0, "enlighten": 12, "weight": 1,  "desc": "传说中的道体，成道之资"},
}
ROOT_NAMES = list(ROOTS)

# 功法：品阶 / 威力 / 修炼加成 / 门槛
TECHNIQUES = {
    "太虚吐纳诀": {"rank": "人阶", "atk": 10,  "cultivate": 0.10, "realm": 1,  "price": 200,
                   "desc": "最基础的吐纳法门，入门必学"},
    "青元剑诀": {"rank": "地阶", "atk": 35,  "cultivate": 0.20, "realm": 3,  "price": 1200,
                 "desc": "青元剑宗嫡传，御剑千里"},
    "玄水真经": {"rank": "地阶", "atk": 28,  "cultivate": 0.30, "realm": 3,  "price": 1500,
                 "desc": "以水养身，修炼效率大增"},
    "九霄雷法": {"rank": "天阶", "atk": 90,  "cultivate": 0.25, "realm": 4,  "price": 5000,
                 "desc": "引天雷入体，杀伐第一"},
    "血海吞天功": {"rank": "地阶", "atk": 55,  "cultivate": 0.35, "realm": 2,  "price": 1800,
                   "desc": "魔道功法，修炼快但伤道心"},
    "白骨观":   {"rank": "人阶", "atk": 22,  "cultivate": 0.28, "realm": 2,  "price": 700,
                 "desc": "观身不净，心志如铁"},
    "太清化神诀": {"rank": "天阶", "atk": 120, "cultivate": 0.40, "realm": 5,  "price": 12000,
                   "desc": "道门正宗，化神之法"},
    "混沌开天经": {"rank": "仙阶", "atk": 300, "cultivate": 0.60, "realm": 8,  "price": 60000,
                   "desc": "传说中开天辟地的功法，非大机缘不可得"},
}
TECH_LIST = list(TECHNIQUES)

# 丹药：价格 / 效果 / 奇遇掉落权重
PILLS = {
    "回气丹": {"price": 30,   "weight": 30, "desc": "立得 50 修为"},
    "聚元丹": {"price": 120,  "weight": 20, "desc": "立得 300 修为"},
    "破境丹": {"price": 800,  "weight": 8,  "desc": "下次突破成功率 +20%"},
    "渡厄丹": {"price": 3000, "weight": 3,  "desc": "下次突破失败修为不失"},
    "延寿丹": {"price": 1500, "weight": 5,  "desc": "寿元 +50 年"},
    "洗髓丹": {"price": 5000, "weight": 2,  "desc": "重掷灵根"},
    "疗伤丹": {"price": 80,   "weight": 30, "desc": "战斗中恢复 40 气血"},
}
PILL_LIST = list(PILLS)
PILL_WEIGHTS = [PILLS[n]["weight"] for n in PILL_LIST]

# 宗门：属性倾向 / 入门要求 / 位阶
SECTS = {
    "青云宗": {"style": "正道", "bonus": "突破成功率 +5%", "break_bonus": 0.05,
               "req": 0, "desc": "天下正道之首，重根基"},
    "万毒门": {"style": "魔道", "bonus": "战力 +25%", "power_bonus": 0.25,
               "req": 0, "desc": "以毒入道，杀伐果断"},
    "天机阁": {"style": "中立", "bonus": "奇遇概率 +30%", "adventure_bonus": 0.30,
               "req": 1, "desc": "窥探天机，机缘最多"},
    "剑冢":   {"style": "正道", "bonus": "功法威力 +30%", "atk_bonus": 0.30,
               "req": 2, "desc": "以剑证道，一往无前"},
}
SECT_LIST = list(SECTS)
SECT_RANKS = ["外门弟子", "内门弟子", "真传弟子", "长老", "副宗主", "宗主"]

# 敌人：按境界分段。hp/atk 不再是绝对值——玩家气血与战力按境界线性增长，
# 而旧版敌人的绝对值是指数增长，筑基以上必输。现在改成相对境界基准的倍率，
# 由 act_fight 用玩家当前境界的基准值现算，任何境界都可打。
# hp_k = 敌人气血 / 境界基准战力；atk_k = 敌人每击 / 境界基准气血
# 精英（同池第二个）更高；再乘 ±FIGHT_VARIANCE 的浮动，胜负才有真实概率分布。
ENEMIES = [
    {"name": "野狼",     "realm": 1, "hp_k": 8.5, "atk_k": 0.11, "reward": 15},
    {"name": "山匪",     "realm": 1, "hp_k": 9.5, "atk_k": 0.13, "reward": 30},
    {"name": "妖狐",     "realm": 2, "hp_k": 8.5, "atk_k": 0.11, "reward": 70},
    {"name": "黑山妖兽", "realm": 2, "hp_k": 9.5, "atk_k": 0.13, "reward": 120},
    {"name": "血蝠",     "realm": 3, "hp_k": 8.5, "atk_k": 0.11, "reward": 260},
    {"name": "魔修",     "realm": 3, "hp_k": 9.5, "atk_k": 0.13, "reward": 450},
    {"name": "妖将",     "realm": 4, "hp_k": 9.0, "atk_k": 0.12, "reward": 900},
    {"name": "血河分身", "realm": 5, "hp_k": 9.0, "atk_k": 0.12, "reward": 2000},
    {"name": "天机傀儡", "realm": 6, "hp_k": 9.5, "atk_k": 0.13, "reward": 5000},
    {"name": "上古凶兽", "realm": 7, "hp_k": 10.0, "atk_k": 0.14, "reward": 12000},
    {"name": "血河老祖", "realm": 9, "hp_k": 10.5, "atk_k": 0.14, "reward": 40000},
]
FIGHT_ROUNDS = 10          # 战斗回合上限
FIGHT_VARIANCE = 0.22      # 遭遇时敌人强弱浮动 ±22%，避免胜负变成 0/1 台阶

# 奇遇：类型 / 权重 / 文案由函数生成
ADVENTURES = [
    ("pill",     20),
    ("qi",       20),
    ("coin",     15),
    ("technique", 8),
    ("enlighten", 12),
    ("injury",   10),
    ("sect_offer", 5),
    ("nothing",  10),
]

# 常量
# 吐纳：挂机累积。离线也照算，玩家每隔几小时回来收一次功，不需要守着点。
# 想要更慢/更快的节奏，只改这两个数即可。
IDLE_SECONDS_PER_CHARGE = 300   # 每 5 分钟积 1 次吐纳
IDLE_MAX_CHARGES = 12           # 灵池最多攒 12 次（1 小时）；攒满后不再累积
TUINA_COOLDOWN = 120            # 旧版的冷却；现在只用于兼容旧存档，不再参与判定
RETREAT_LIFESPAN_COST = 1    # 每闭关 1 年，消耗 1 年寿元
ADVENTURE_COOLDOWN = 300     # 秒
FIGHT_COOLDOWN = 120         # 秒；没有冷却就能无限刷灵石，必须限
RETREAT_RATE = 0.03          # 闭关每年获得的修为 = 下一境界所需 × 此系数
BREAK_FAIL_QI_KEEP = 0.5     # 突破失败保留一半修为
BREAK_FAIL_LIFE = 5          # 失败损寿元
HEAL_LIFE = 10               # 疗伤丹战斗外服用恢复的寿元

# 配图（由 Hermes 的 image_generate 现画）。**没有任何时间冷却**，只看画图忙不忙：
# 脚本拿不到「画完了」的回执，所以用「在途任务 + TTL」估计忙碌度——
#   · 你自己上一张还在途  → 说明情况，等它出来
#   · 群里在途堆到 DRAW_WORLD_SLOTS 张 → 说明情况，拒绝
#   · 不忙 → 直接画
# 任务超过 DRAW_JOB_TTL 秒就视为已画完、自动出队（模型失败没回报也能自愈）。
DRAW_WORLD_SLOTS = 4         # 同群同时在途的配图上限；堆到这么多就说忙
DRAW_JOB_TTL = 120           # 秒；一张图实测几十秒，超过这个时长就当它画完了
DRAW_CHANCE = {              # 事件 -> 出图概率（突破成功/失败/渡劫必定出）
    "breakthrough": 1.0,     # 突破成功
    "breakthrough_fail": 0.5,  # 突破失败（有戏剧性，但不必每次都画）
    "ascend": 1.0,           # 渡劫 → 大乘
    "death": 1.0,            # 坐化
    "realm_up_high": 0.6,    # 金丹以上每次突破额外加成概率
}

# 出图风格统一，避免模型每次自由发挥导致一个群画风乱飞
DRAW_STYLE = ("中国水墨仙侠插画，竖幅构图，留白意境，淡青与赭石色，"
              "无文字无签名，画面主体：%s")


# ---------------------------------------------------------------- 数值

def root_of(p: dict) -> dict:
    return ROOTS.get(p.get("root") or "下品灵根", ROOTS["下品灵根"])


def realm_of(p: dict) -> dict:
    return REALMS[max(0, min(MAX_REALM, int(p.get("realm") or 0)))]


def realm_name(p: dict) -> str:
    return realm_of(p)["name"]


def cultivate_mult(p: dict) -> float:
    m = root_of(p)["mult"]
    tech = TECHNIQUES.get(p.get("active_technique") or "")
    if tech:
        m += tech["cultivate"]
    return m


def power_of(p: dict) -> int:
    base = realm_of(p)["power"] + int(p.get("enlighten") or 0) * 2
    tech = TECHNIQUES.get(p.get("active_technique") or "")
    if tech:
        base += tech["atk"]
    sect = SECTS.get(p.get("sect") or "")
    if sect and "power_bonus" in sect:
        base = int(base * (1 + sect["power_bonus"]))
    if sect and "atk_bonus" in sect:
        base = int(base * (1 + sect["atk_bonus"]))
    return max(1, base)


def break_chance(p: dict) -> float:
    realm = realm_of(p)
    chance = realm["break_base"] + int(p.get("enlighten") or 0) * 0.005
    sect = SECTS.get(p.get("sect") or "")
    if sect and "break_bonus" in sect:
        chance += sect["break_bonus"]
    # 连续失败补偿：每次失败 +8%，避免卡死在同一个坎上
    chance += int(p.get("tribulation_fails") or 0) * 0.08
    return max(0.05, min(0.98, chance))


def can_break(p: dict) -> bool:
    realm_idx = int(p.get("realm") or 0)
    if realm_idx >= MAX_REALM:
        return False
    return int(p.get("qi") or 0) >= REALMS[realm_idx + 1]["qi"]


def roll_root() -> str:
    names = [n for n in ROOT_NAMES]
    weights = [ROOTS[n]["weight"] for n in names]
    return random.choices(names, weights=weights, k=1)[0]


def gain_qi(p: dict, amount: int, g: Game = None) -> int:
    """加修为；返回实际增加量（修为无上限，突破时才扣）。"""
    amount = max(0, int(amount))
    p["qi"] = int(p.get("qi") or 0) + amount
    return amount


def lose_lifespan(p: dict, years: int) -> bool:
    """扣寿元；返回是否仍然活着。寿元归零即坐化。"""
    p["lifespan"] = int(p.get("lifespan") or 0) - int(years)
    if p["lifespan"] <= 0:
        p["lifespan"] = 0
        p["alive"] = False
        return False
    return True


def next_qi(p: dict) -> int:
    """当前境界突破到下一境所需修为；已至绝巅返回 0。"""
    idx = int(p.get("realm") or 0)
    if idx >= MAX_REALM:
        return 0
    return REALMS[idx + 1]["qi"]


def per_tuina(p: dict) -> float:
    """单次吐纳的原始收益（未乘灵根/功法倍率）。

    低境界按固定值，高境界按下一境界所需的比例，避免后期吐纳完全无感。
    """
    return max(8 + int(p.get("realm") or 0) * 6, int(next_qi(p) * 0.01))


def settle_tuina(p: dict) -> int:
    """把「离线到现在」的时间结算进灵池，返回当前可收次数。

    只读+写 p，不碰存档；act_status / act_tuina 都可反复调，结果一致（幂等）。
    零头会保留在 tuina_ts 里，不会因为频繁查看状态而丢掉。
    灵池顶满后 tuina_ts 直接推到 now——否则挂着不收的玩家会无限积压。
    """
    now = now_ts()
    charges = max(0, int(p.get("tuina_charges") or 0))
    since = int(p.get("tuina_ts") or 0) or now
    if since > now:          # 系统时间被回拨，别让玩家倒欠
        since = now
    elapsed = now - since
    gained = elapsed // IDLE_SECONDS_PER_CHARGE
    if gained <= 0:
        p["tuina_charges"] = charges
        p["tuina_ts"] = since
        return charges
    charges = min(IDLE_MAX_CHARGES, charges + gained)
    if charges >= IDLE_MAX_CHARGES:
        p["tuina_ts"] = now
    else:
        p["tuina_ts"] = since + gained * IDLE_SECONDS_PER_CHARGE
    p["tuina_charges"] = charges
    return charges


def age_line(p: dict) -> str:
    return "寿元 %d/%d 年" % (int(p.get("lifespan") or 0),
                              int(p.get("lifespan_max") or 0))


# ---------------------------------------------------------------- 配图

def _draw_jobs(g: Game) -> list:
    """本群还没过期的配图任务列表（顺手清理过期项）。

    脚本拿不到「画完了」的回执，所以用 TTL 自愈：任务记下时间戳，
    超过 DRAW_JOB_TTL 秒就当它已经结束，把名额还回去。
    """
    now = now_ts()
    jobs = [j for j in (g.world.get("draw_jobs") or [])
            if now - int(j.get("ts") or 0) < DRAW_JOB_TTL]
    g.world["draw_jobs"] = jobs
    return jobs


def world_draw_busy(g: Game) -> int:
    """当前在途的配图数。"""
    return len(_draw_jobs(g))


def draw_blocked(g: Game, p: dict) -> tuple:
    """返回 (等待秒数, 原因)。等待 0 表示画图不忙、可以出图。

    不设时间冷却，只看忙碌度：
      1. 这个人自己上一张还在途 → 等它出来（同一人同时只画一张）
      2. 群里在途堆到 DRAW_WORLD_SLOTS 张 → 画师忙不过来
    """
    now = now_ts()
    jobs = _draw_jobs(g)
    for j in jobs:
        if j.get("player") == str(p.get("id") or ""):
            return DRAW_JOB_TTL - (now - int(j.get("ts") or 0)), "self"

    if len(jobs) >= DRAW_WORLD_SLOTS:
        # 最早开始的那个最先画完，等它出队
        oldest = min(int(j.get("ts") or 0) for j in jobs)
        return DRAW_JOB_TTL - (now - oldest), "busy"

    return 0, ""


def mark_draw(g: Game, p: dict) -> str:
    """把一张图记进队列，返回任务 id（回调用）。

    判定通过就立即记账——即使模型最后没调 image_generate，这条也会照常
    过期出队，不会把位置永久占死。正常路径是模型画完后调 ``draw_done``
    立刻销账，让队列位更早还回来；回执丢了就靠 TTL 兜底。
    """
    now = now_ts()
    jobs = _draw_jobs(g)
    job_id = "%s-%d" % (str(p.get("id") or "u")[:8], now)
    # 同一秒同一人重复入队时 id 会撞，补个序号保证唯一
    seen = {j.get("id") for j in jobs}
    if job_id in seen:
        n = 1
        while "%s.%d" % (job_id, n) in seen:
            n += 1
        job_id = "%s.%d" % (job_id, n)
    jobs.append({"id": job_id, "player": str(p.get("id") or ""), "ts": now})
    g.world["draw_jobs"] = jobs
    return job_id


def act_draw_done(g: Game, args) -> None:
    """模型画完图后的回调：把这张任务销掉，队列位立刻还回来。

    不指定 ``--item``（任务 id）就销掉自己最早的一单，这样模型不需要
    记 id 也能用。回执丢了、重复调用都没关系——TTL 到期照样出队，
    所以这里对「找不到」也回 OK（幂等）。
    输出只有一行 OK，是给模型看的内部确认，**不要发到群里**。
    """
    p = g.player()
    pid = str(p.get("id") or "")
    jobs = _draw_jobs(g)
    want = (args.item or "").strip()
    for i, j in enumerate(jobs):
        if j.get("player") != pid:
            continue
        if want and j.get("id") != want:
            continue
        jobs.pop(i)
        g.world["draw_jobs"] = jobs
        break
    g.say("OK")


def maybe_draw(g: Game, p: dict, event: str, scene: str) -> None:
    """大事发生时请求一张现画插画；画师忙就静默跳过。

    不忙才画——自动触发不像主动点单那样值得让用户等，忙的时候直接放弃
    更符合「顺带配一张」的定位。概率判定放在忙碌判定之后，避免白白消耗随机数。
    """
    chance = DRAW_CHANCE.get(event, 0.0)
    if chance <= 0:
        return
    wait, _reason = draw_blocked(g, p)
    if wait > 0:
        return
    if random.random() > chance:
        return
    mark_draw(g, p)
    g.draw(DRAW_STYLE % scene)


# ---------------------------------------------------------------- 动作

def _require_alive(g: Game, p: dict) -> bool:
    if p.get("alive", True):
        return True
    g.say("💀 %s 已坐化，道消身陨。" % p["name"])
    g.say("发「重新投胎」可重开一世（修仙道果清零、灵石留四分之一，农场家当保留）。")
    return False


def act_init(g: Game, args) -> None:
    p = g.player()
    if p.get("root"):
        g.say("%s 早已踏入修行，当前境界：%s" % (p["name"], realm_name(p)))
        g.say("想重来一世请发「重新投胎」。")
        return

    want = (args.root or "auto").strip()
    if want in ROOTS:
        root = want
    else:
        root = roll_root()

    p["root"] = root
    p["realm"] = 0
    p["qi"] = 0
    p["enlighten"] = ROOTS[root]["enlighten"]
    p["lifespan"] = 80 + REALMS[0]["life"]
    p["lifespan_max"] = p["lifespan"]
    p["alive"] = True
    p["daoxiang"] = []
    # 入道即开灵池，别让新玩家看着「0 次、5 分钟后」干等
    p["tuina_charges"] = 1
    p["tuina_ts"] = now_ts()

    r = ROOTS[root]
    g.say("🌌 灵根觉醒")
    g.say("")
    g.say("%s　%s" % (root, r["desc"]))
    g.say("修炼效率 ×%.1f　悟性 %d" % (r["mult"], p["enlighten"]))
    g.say("")
    g.say("寿元 %d 年，境界：凡人" % p["lifespan"])
    g.say("灵气会随时间自动累积，离线也算。想修炼就发「吐纳」收功。")


def act_status(g: Game, args) -> None:
    p = g.player()
    if not p.get("root"):
        g.say("%s 尚未踏上修行之路。" % p["name"])
        g.say("发「修仙」开始：会为你觉醒灵根。")
        return
    if not _require_alive(g, p):
        return

    realm = realm_of(p)
    nxt = REALMS[min(MAX_REALM, int(p["realm"]) + 1)]
    tech = TECHNIQUES.get(p.get("active_technique") or "")

    g.say("📜 %s" % p["name"])
    g.say("境界　%s　战力 %d" % (realm["name"], power_of(p)))
    if int(p["realm"]) < MAX_REALM:
        qi = int(p.get("qi") or 0)
        need = max(1, nxt["qi"])
        if qi >= need:
            g.say("修为　%d（已满，突破后多余部分会清空）" % qi)
        else:
            pct = min(100, int(qi * 100 / need))
            bar = "█" * (pct // 10) + "░" * (10 - pct // 10)
            g.say("修为　%d / %d　%s %d%%" % (qi, need, bar, pct))
        g.say("突破　成功率 %d%%%s" % (
            int(break_chance(p) * 100),
            "　✅可以突破" if can_break(p) else ""))
    else:
        g.say("修为　%d（已至绝巅）" % p.get("qi", 0))

    g.say("灵根　%s（×%.1f）　悟性 %d" % (
        p.get("root", "无"), root_of(p)["mult"], p.get("enlighten", 0)))
    charges = settle_tuina(p)
    if charges >= IDLE_MAX_CHARGES:
        pool = "灵池　%d/%d　已满，可收功" % (charges, IDLE_MAX_CHARGES)
    elif charges:
        pool = "灵池　%d/%d　可收功" % (charges, IDLE_MAX_CHARGES)
    else:
        left = IDLE_SECONDS_PER_CHARGE - (now_ts() - int(p.get("tuina_ts") or 0))
        pool = "灵池　0/%d　%s后有灵气" % (IDLE_MAX_CHARGES, fmt_duration(left))
    g.say(pool)
    g.say(age_line(p))
    g.say("💰 灵石　%d　🌱 农场等级 %d" % (
        p.get("coins", 0), _farm_level(p)))

    if tech:
        g.say("⚔️ 运功　%s（%s，威力 +%d）" % (
            p.get("active_technique"), tech["rank"], tech["atk"]))
    elif p.get("techniques"):
        g.say("⚔️ 已学功法：%s（未运功）" % "、".join(p["techniques"]))
    else:
        g.say("⚔️ 功法：无")

    if p.get("sect"):
        rank_idx = min(len(SECT_RANKS) - 1, int(p.get("sect_rank") or 0))
        g.say("🏯 宗门　%s·%s" % (p["sect"], SECT_RANKS[rank_idx]))

    if p.get("pills"):
        g.say("💊 丹药　" + "、".join(
            "%s×%d" % (k, v) for k, v in p["pills"].items() if v))
    if p.get("tribulation_fails"):
        g.say("⚠️ 连续突破失败 %d 次（下次成功率 +%d%%）" % (
            p["tribulation_fails"], p["tribulation_fails"] * 8))


def _farm_level(p: dict) -> int:
    exp = int(p.get("exp") or 0)
    for i in range(21, 0, -1):
        if exp >= (i ** 4 - 1) // 5:
            return i
    return 0


def act_tuina(g: Game, args) -> None:
    """收功：把挂机攒下的灵池一次性收掉。

    吐纳不再是「点一次冷却 2 分钟」的高频动作——那样玩家得守着，模型会自己
    写循环去刷。现在灵气随时间自动累积（离线也算），玩家什么时候回来收都行。
    """
    p = g.player()
    if not _require_alive(g, p):
        return
    if not p.get("root"):
        g.say("你还没觉醒灵根。先发「修仙」。")
        return

    charges = settle_tuina(p)
    if charges <= 0:
        left = IDLE_SECONDS_PER_CHARGE - (now_ts() - int(p.get("tuina_ts") or 0))
        g.say("🌬️ 灵池还没攒满，还需 %s 才能收功。" % fmt_duration(left))
        g.say("（挂机自动累积，离线也算；最多攒 %d 次，不用守着。）"
              % IDLE_MAX_CHARGES)
        return

    realm = realm_of(p)
    gain = int(charges * per_tuina(p) * cultivate_mult(p)
               * random.uniform(0.85, 1.15))
    gain = max(charges, gain)
    p["tuina_charges"] = 0

    # 灵根越差，越容易耗寿元；一次收功只判一次，避免攒满一次收掉半个寿元
    cost = 0
    if root_of(p)["mult"] <= 1.0:
        cost = random.choice([0, 0, 0, 1])
    if cost:
        if not lose_lifespan(p, cost):
            g.say("💀 一气吐尽，寿元耗尽。")
            g.say("%s 坐化了，终年 %s 境界。" % (p["name"], realm["name"]))
            maybe_draw(g, p, "death",
                       "老修士在蒲团上坐化，烛火将熄，窗外孤鹤远去")
            return

    gain_qi(p, gain)
    g.say("🧘 %s 吐纳周天" % p["name"])
    g.say("灵池收功 ×%d" % charges)
    g.say("修为 +%d → %d" % (gain, p["qi"]))
    if cost:
        g.say("寿元 -%d → %d 年（灵根不够，只能以命换道）" % (cost, p["lifespan"]))
    if can_break(p):
        g.say("")
        g.say("✨ 修为已足，可以突破至%s！" % REALMS[int(p["realm"]) + 1]["name"])


def act_retreat(g: Game, args) -> None:
    p = g.player()
    if not _require_alive(g, p):
        return
    if not p.get("root"):
        g.say("你还没觉醒灵根。先发「修仙」。")
        return

    years = int(args.years or 0)
    if int(p["realm"]) >= MAX_REALM:
        g.say("已是飞升之境，无需再闭关。")
        return
    if years <= 0:
        g.say("闭关要指定年数，例如「闭关 5 年」。1 年消耗 1 年寿元。")
        return
    if years > int(p.get("lifespan") or 0):
        g.say("寿元只剩 %d 年，闭不了 %d 年。（可以服延寿丹续命）" % (p["lifespan"], years))
        return
    if years > 100:
        g.say("一次最多闭关 100 年，否则心魔丛生。")
        return

    # 收益按「下一境界所需修为」的比例结算，而不是按境界绝对数字——
    # 否则凡人闭关几十年就能一口气冲过好几个境界，寿元约束形同虚设。
    # 灵根/功法越好，同样年数走得越远；每次闭关最多走完下一境界的一截。
    per_year = next_qi(p) * RETREAT_RATE
    gain = int(per_year * years * cultivate_mult(p) * random.uniform(0.9, 1.2))
    gain = max(years, gain)

    p["last_retreat"] = now_ts()
    gain_qi(p, gain)
    alive = lose_lifespan(p, years)

    g.say("🕯️ 闭死关 %d 年" % years)
    g.say("修为 +%d → %d" % (gain, p["qi"]))
    g.say(age_line(p))
    if not alive:
        g.say("")
        g.say("💀 出关时已是枯骨。道途终绝于%s。" % realm_name(p))
        maybe_draw(g, p, "death",
                   "修士闭关千年，出关时洞府唯余枯骨与一盏将尽的孤灯")
        return
    if can_break(p):
        g.say("")
        g.say("✨ 出关即遇天机，可以突破至%s！" % REALMS[int(p["realm"]) + 1]["name"])


def act_breakthrough(g: Game, args) -> None:
    p = g.player()
    if not _require_alive(g, p):
        return
    if not p.get("root"):
        g.say("你还没觉醒灵根。先发「修仙」。")
        return
    if int(p["realm"]) >= MAX_REALM:
        g.say("已是飞升之境，再无境界可破。")
        return
    if not can_break(p):
        need = REALMS[int(p["realm"]) + 1]["qi"] - int(p.get("qi") or 0)
        g.say("修为不足，还差 %d。先吐纳或闭关。" % need)
        return

    nxt = REALMS[int(p["realm"]) + 1]
    chance = break_chance(p)
    pill_bonus = 0.0
    if p.get("break_pill"):
        pill_bonus = 0.20
        chance = min(0.98, chance + pill_bonus)
    ok = random.random() < chance

    g.say("⚡ 冲击%s境" % nxt["name"])
    g.say("成功率 %d%%%s" % (
        int(chance * 100), "（含破境丹 +20%）" if pill_bonus else ""))
    g.say("")

    if ok:
        p.pop("break_pill", None)
        p.pop("ward_pill", None)
        old_life = REALMS[int(p["realm"])]["life"]
        p["realm"] = int(p["realm"]) + 1
        p["qi"] = 0
        p["tribulation_fails"] = 0
        # 寿元上限随境界提升
        bonus = nxt["life"] - old_life
        p["lifespan_max"] = int(p.get("lifespan_max") or 80) + bonus
        p["lifespan"] = int(p["lifespan"]) + bonus
        if p.get("sect"):
            p["sect_rank"] = min(len(SECT_RANKS) - 1, int(p.get("sect_rank") or 0) + 1)
        g.say("✅ 突破成功！")
        g.say("境界 → %s　战力 %d" % (nxt["name"], power_of(p)))
        g.say("寿元上限 +%d → %s" % (bonus, age_line(p)))
        if p.get("sect"):
            g.say("🏯 宗门位阶提升：%s" % SECT_RANKS[p["sect_rank"]])
        if nxt["name"] == "渡劫":
            g.say("")
            g.say("🌩️ 九天雷劫将至。下一次突破，即是大乘。")
        # 突破是整局最值得画的一刻：新境界的第一次亮相
        ev = "ascend" if nxt["name"] in ("大乘", "飞升") else "breakthrough"
        maybe_draw(g, p, ev,
                   "修士突破至%s境，周身清气升腾，天际霞光与灵纹环绕"
                   % nxt["name"])
    else:
        p.pop("break_pill", None)
        p["tribulation_fails"] = int(p.get("tribulation_fails") or 0) + 1
        if p.get("ward_pill"):
            # 渡厄丹：保住修为，只损寿元
            p.pop("ward_pill", None)
            alive = lose_lifespan(p, BREAK_FAIL_LIFE)
            g.say("❌ 突破失败，气机反噬。")
            g.say("🍃 渡厄丹护住经脉，修为未损（%d）。" % p["qi"])
            g.say("寿元 -%d → %s" % (BREAK_FAIL_LIFE, age_line(p)))
        else:
            before = int(p.get("qi") or 0)
            kept = int(before * BREAK_FAIL_QI_KEEP)
            p["qi"] = kept
            alive = lose_lifespan(p, BREAK_FAIL_LIFE)
            g.say("❌ 突破失败，气机反噬。")
            g.say("修为 %d → %d　寿元 -%d" % (before, kept, BREAK_FAIL_LIFE))
        if not alive:
            g.say("💀 反噬过重，坐化了。")
            maybe_draw(g, p, "death",
                       "修士在雷光中道消身陨，衣袂消散为漫天流萤")
            return
        n = p["tribulation_fails"]
        g.say("再战成功率 +%d%%（累计失败 %d 次）" % (n * 8, n))
        g.say("💡 服用破境丹可再 +20%。")
        if random.random() < 0.5:
            maybe_draw(g, p, "breakthrough_fail",
                       "修士冲击%s境失败，经脉逆冲，山巅云雾翻涌"
                       % REALMS[int(p["realm"]) + 1]["name"])

def act_adventure(g: Game, args) -> None:
    p = g.player()
    if not _require_alive(g, p):
        return
    if not p.get("root"):
        g.say("你还没觉醒灵根。先发「修仙」。")
        return

    now = now_ts()
    elapsed = now - int(p.get("last_adventure") or 0)
    if elapsed < ADVENTURE_COOLDOWN:
        g.say("⏳ 心神未定，还需 %s 才能再次游历。" % fmt_duration(ADVENTURE_COOLDOWN - elapsed))
        return
    p["last_adventure"] = now

    sect = SECTS.get(p.get("sect") or "")
    bonus = sect.get("adventure_bonus", 0) if sect else 0
    kinds = [k for k, _ in ADVENTURES]
    weights = [w * (1 + bonus if k not in ("injury", "nothing") else 1)
               for k, w in ADVENTURES]
    kind = random.choices(kinds, weights=weights, k=1)[0]

    g.say("🌫️ %s 游历山野" % p["name"])
    g.say("")

    if kind == "pill":
        pill = random.choices(PILL_LIST, weights=PILL_WEIGHTS, k=1)[0]
        p.setdefault("pills", {})
        p["pills"][pill] = int(p["pills"].get(pill) or 0) + 1
        g.say("🍃 在废弃洞府中拾得一枚【%s】。" % pill)
    elif kind == "qi":
        amount = int((20 + int(p["realm"]) * 15) * random.uniform(1, 2))
        gain_qi(p, amount)
        g.say("🌊 偶遇灵脉喷涌，修为 +%d → %d。" % (amount, p["qi"]))
    elif kind == "coin":
        coins = int((50 + int(p["realm"]) * 40) * random.uniform(1, 3))
        p["coins"] = int(p.get("coins") or 0) + coins
        g.say("💰 挖出一小袋灵石 +%d → %d。" % (coins, p["coins"]))
    elif kind == "technique":
        missing = [t for t in TECH_LIST if t not in p.get("techniques", [])]
        if missing:
            t = random.choice(missing)
            p.setdefault("techniques", []).append(t)
            g.say("📖 石壁上刻着《%s》，细读之下竟有所悟。" % t)
            g.say("已收录功法，可「学 %s」运功。" % t)
        else:
            gain_qi(p, 100)
            g.say("📖 石壁上的功法你已尽数参悟，修为 +100。")
    elif kind == "enlighten":
        p["enlighten"] = int(p.get("enlighten") or 0) + 1
        g.say("🌌 观星悟道，悟性 +1 → %d。" % p["enlighten"])
    elif kind == "injure":
        dmg = int(int(p.get("lifespan") or 0) * 0.05) or 1
        alive = lose_lifespan(p, dmg)
        g.say("🩸 误入凶兽巢穴，仓皇逃出，寿元 -%d。" % dmg)
        g.say(age_line(p))
        if not alive:
            g.say("💀 伤重不治，身死道消。")
            maybe_draw(g, p, "death",
                       "修士倒毙于荒山古道，残月低垂，远景孤峰如剑")
            return
    elif kind == "sect_offer":
        g.say("🏯 一位宗门长老路过，见你根骨不凡，愿引你入门。")
        g.say("可发「加入 %s」查看宗门。" % random.choice(SECT_LIST))
    else:
        g.say("🍂 一无所获，但山风吹过，心境澄明。")


def act_fight(g: Game, args) -> None:
    p = g.player()
    if not _require_alive(g, p):
        return
    if not p.get("root"):
        g.say("你还没觉醒灵根。先发「修仙」。")
        return

    now = now_ts()
    elapsed = now - int(p.get("last_fight") or 0)
    if elapsed < FIGHT_COOLDOWN:
        g.say("⏳ 道基未稳，还需 %s 才能再战。" % fmt_duration(FIGHT_COOLDOWN - elapsed))
        return

    realm_idx = int(p["realm"])
    pool = [e for e in ENEMIES if abs(e["realm"] - realm_idx) <= 1]
    if not pool:
        pool = ENEMIES
    enemy = random.choice(pool)
    p["last_fight"] = now

    my_power = power_of(p)
    # 敌人属性按「境界基准」现算，不按玩家完整战力——否则买了功法敌人也跟着变强，
    # 变强没有任何正反馈。基准取自 REALMS[realm]["power"] 与 50+境界×40。
    base_power = realm_of(p)["power"]
    base_hp = 50 + realm_idx * 40
    my_hp = base_hp + int(p.get("enlighten") or 0) * 5
    my_hp_max = my_hp
    # 遭遇强度浮动：不加这一层，8~10 回合的固定结算会让胜负变成 0% 或 100% 的台阶
    swing = random.uniform(1 - FIGHT_VARIANCE, 1 + FIGHT_VARIANCE)
    enemy_hp = max(1, int(base_power * enemy["hp_k"] * swing))
    enemy_atk = max(1, int(base_hp * enemy["atk_k"] * swing))

    # 疗伤丹：战斗中自动救命（有就用，没用完不消耗）
    heal_left = int((p.get("pills") or {}).get("疗伤丹") or 0)
    healed = 0
    rounds = []

    for r in range(1, FIGHT_ROUNDS + 1):
        my_hit = max(1, int(my_power * random.uniform(0.8, 1.2)))
        enemy_hp -= my_hit
        if enemy_hp <= 0:
            rounds.append("第%d回合：你一击命中，%s 当场毙命。" % (r, enemy["name"]))
            break
        e_hit = max(1, int(enemy_atk * random.uniform(0.7, 1.3)))
        my_hp -= e_hit
        rounds.append("第%d回合：你造成 %d 伤害，%s 反击 %d。" % (r, my_hit, enemy["name"], e_hit))
        if my_hp <= 0:
            if heal_left > 0:
                heal_left -= 1
                healed += 1
                my_hp = 40
                rounds.append("　💊 危急关头服下疗伤丹，气血回到 40。")
                continue
            break

    g.say("⚔️ 遭遇【%s】（战力约 %d）" % (enemy["name"], enemy_atk * 3))
    g.say("")
    for line in rounds:
        g.say(line)
    g.say("")

    if healed:
        p.setdefault("pills", {})
        p["pills"]["疗伤丹"] = int(p["pills"].get("疗伤丹") or 0) - healed
        if p["pills"]["疗伤丹"] <= 0:
            p["pills"].pop("疗伤丹", None)
        g.say("💊 消耗疗伤丹 ×%d" % healed)
        g.say("")

    if enemy_hp <= 0:
        reward = int(enemy["reward"] * random.uniform(0.9, 1.3))
        qi_gain = max(1, reward // 3)
        p["coins"] = int(p.get("coins") or 0) + reward
        gain_qi(p, qi_gain)
        g.say("✅ 胜　灵石 +%d → %d　修为 +%d" % (reward, p["coins"], qi_gain))
        if random.random() < 0.25:
            pill = random.choices(PILL_LIST, weights=PILL_WEIGHTS, k=1)[0]
            p.setdefault("pills", {})
            p["pills"][pill] = int(p["pills"].get(pill) or 0) + 1
            g.say("🍃 战利品：%s ×1" % pill)
    else:
        loss = int(int(p.get("coins") or 0) * 0.1)
        p["coins"] = max(0, int(p.get("coins") or 0) - loss)
        alive = lose_lifespan(p, 2)
        g.say("❌ 败　你负伤遁走，灵石 -%d，寿元 -2" % loss)
        g.say(age_line(p))
        if not alive:
            g.say("💀 伤重不治，身死道消。")
            maybe_draw(g, p, "death",
                       "修士力竭倒在妖兽爪下，血染青石，远处山门朦胧")


def act_sect(g: Game, args) -> None:
    p = g.player()
    if not p.get("root"):
        g.say("你还没觉醒灵根。先发「修仙」。")
        return

    name = (args.item or "").strip()
    if not name:
        g.say("🏯 天下宗门")
        g.say("")
        for n in SECT_LIST:
            s = SECTS[n]
            mark = "（当前）" if p.get("sect") == n else ""
            g.say("%s　%s" % (n, s["style"]))
            g.say("　%s　门槛：%s%s%s" % (
                s["bonus"], REALMS[s["req"]]["name"], "　", mark))
        g.say("")
        g.say("发「加入 <宗门名>」拜入。进出自由，但换门会掉一级位阶。")
        return

    if name not in SECTS:
        g.say("没有「%s」这个宗门。" % name)
        return
    if not _require_alive(g, p):
        return
    s = SECTS[name]
    if int(p["realm"]) < s["req"]:
        g.say("拜入%s需 %s 境，你在 %s。" % (
            name, REALMS[s["req"]]["name"], realm_name(p)))
        return
    if p.get("sect") == name:
        g.say("你已在%s。" % name)
        return

    old = p.get("sect")
    p["sect"] = name
    if old:
        p["sect_rank"] = max(0, int(p.get("sect_rank") or 0) - 1)
        g.say("你自%s转投%s，位阶降一级。" % (old, name))
    else:
        p["sect_rank"] = 0
        g.say("✅ 拜入%s，列为%s。" % (name, SECT_RANKS[0]))
    g.say("宗门加持：%s" % s["bonus"])
    g.say("境界提升时位阶自动晋升，最高%s。" % SECT_RANKS[-1])


def act_learn(g: Game, args) -> None:
    p = g.player()
    name = (args.item or "").strip()
    if not name:
        learned = p.get("techniques") or []
        g.say("📖 已学功法：%s" % ("、".join(learned) if learned else "无"))
        g.say("看可学的发「功法」；学会后「学 <功法名>」运功。")
        return
    if not _require_alive(g, p):
        return
    if name not in TECHNIQUES:
        g.say("没有《%s》。发「功法」查看。" % name)
        return
    if name not in (p.get("techniques") or []):
        g.say("你还没学会《%s》。可在「功法」里用灵石购入。" % name)
        return
    p["active_technique"] = name
    t = TECHNIQUES[name]
    g.say("⚔️ 运功《%s》（%s）" % (name, t["rank"]))
    g.say("威力 +%d　修炼效率 +%d%%" % (t["atk"], int(t["cultivate"] * 100)))
    g.say("当前战力 %d" % power_of(p))


def act_shop(g: Game, args) -> None:
    p = g.player()
    kind = (args.kind or "pill").strip()
    if kind == "technique":
        g.say("📖 功法阁　灵石 %d" % int(p.get("coins") or 0))
        g.say("")
        for n, t in TECHNIQUES.items():
            owned = "✅" if n in (p.get("techniques") or []) else ""
            lock = "" if int(p["realm"]) >= t["realm"] else "　需%s" % REALMS[t["realm"]]["name"]
            g.say("%s　%s　%d 灵石%s%s" % (n, t["rank"], t["price"], lock, owned))
            g.say("　%s" % t["desc"])
        g.say("")
        g.say("发「买 <功法名>」购入，再「学 <功法名>」运功。")
        return

    g.say("💊 丹房　灵石 %d" % int(p.get("coins") or 0))
    g.say("")
    for n, d in PILLS.items():
        have = int((p.get("pills") or {}).get(n) or 0)
        g.say("%s　%d 灵石%s" % (n, d["price"], "　持有 %d" % have if have else ""))
        g.say("　%s" % d["desc"])


def act_buy(g: Game, args) -> None:
    p = g.player()
    name = (args.item or "").strip()
    count = max(1, int(args.count or 1))
    if not name:
        g.say("要买什么？例如「买破境丹」「买 青元剑诀」。")
        return
    if not _require_alive(g, p):
        return

    if name in PILLS:
        price = PILLS[name]["price"] * count
        if int(p.get("coins") or 0) < price:
            g.say("灵石不足：需要 %d，你有 %d。" % (price, int(p.get("coins") or 0)))
            return
        p["coins"] = int(p["coins"]) - price
        p.setdefault("pills", {})
        p["pills"][name] = int(p["pills"].get(name) or 0) + count
        g.say("✅ 购得 %s ×%d" % (name, count))
        g.say("💰 -%d → %d" % (price, p["coins"]))
        return

    if name in TECHNIQUES:
        t = TECHNIQUES[name]
        if name in (p.get("techniques") or []):
            g.say("你已学会《%s》。" % name)
            return
        if int(p["realm"]) < t["realm"]:
            g.say("《%s》需 %s 境才能修习。" % (name, REALMS[t["realm"]]["name"]))
            return
        if int(p.get("coins") or 0) < t["price"]:
            g.say("灵石不足：需要 %d，你有 %d。" % (t["price"], int(p.get("coins") or 0)))
            return
        p["coins"] = int(p["coins"]) - t["price"]
        p.setdefault("techniques", []).append(name)
        g.say("✅ 习得《%s》（%s）" % (name, t["rank"]))
        g.say("💰 -%d → %d" % (t["price"], p["coins"]))
        g.say("发「学 %s」运功。" % name)
        return

    g.say("没有「%s」这件东西。" % name)


def act_use(g: Game, args) -> None:
    p = g.player()
    name = (args.item or "").strip()
    if not name:
        owned = {k: v for k, v in (p.get("pills") or {}).items() if v}
        g.say("💊 持有丹药：%s" % (
            "、".join("%s×%d" % (k, v) for k, v in owned.items()) if owned else "无"))
        return
    have = int((p.get("pills") or {}).get(name) or 0)
    if have <= 0:
        g.say("你没有%s。可在「丹药」里购买。" % name)
        return
    if not _require_alive(g, p):
        return

    # 药力叠加型丹药：已经挂着效果时再嗑一枚只会覆盖，纯属白扔
    if name == "破境丹" and p.get("break_pill"):
        g.say("💊 破境丹药力还在，等这次突破用完再服。")
        return
    if name == "渡厄丹" and p.get("ward_pill"):
        g.say("💊 渡厄丹药力还在，等这次突破用完再服。")
        return
    if name == "疗伤丹" and int(p.get("lifespan") or 0) >= int(p.get("lifespan_max") or 0):
        g.say("💊 你气血充盈，现在用疗伤丹不划算。（战斗危急时会自动服用）")
        return

    if name == "回气丹":
        gain_qi(p, 50)
        g.say("💊 服下回气丹，修为 +50 → %d" % p["qi"])
    elif name == "聚元丹":
        gain_qi(p, 300)
        g.say("💊 服下聚元丹，修为 +300 → %d" % p["qi"])
    elif name == "破境丹":
        p["break_pill"] = 1
        g.say("💊 服下破境丹，下一次突破成功率 +20%。")
    elif name == "渡厄丹":
        p["ward_pill"] = 1
        g.say("💊 服下渡厄丹，下一次突破失败修为不失（只损寿元）。")
    elif name == "延寿丹":
        p["lifespan"] = int(p.get("lifespan") or 0) + 50
        p["lifespan_max"] = int(p.get("lifespan_max") or 0) + 50
        g.say("💊 服下延寿丹，寿元 +50 → %s" % age_line(p))
    elif name == "洗髓丹":
        old = p.get("root")
        old_mult = ROOTS.get(old, ROOTS["下品灵根"])["mult"]
        new = roll_root()
        p["root"] = new
        p["enlighten"] = ROOTS[new]["enlighten"]
        g.say("💊 服下洗髓丹，经脉重塑。")
        g.say("灵根：%s → %s（修炼 ×%.1f）" % (old, new, ROOTS[new]["mult"]))
        if ROOTS[new]["mult"] < old_mult:
            g.say("……福缘未至，反倒不如从前。")
    elif name == "疗伤丹":
        heal = min(HEAL_LIFE, int(p.get("lifespan_max") or 0) - int(p.get("lifespan") or 0))
        p["lifespan"] = int(p.get("lifespan") or 0) + max(0, heal)
        g.say("💊 服下疗伤丹，调息养伤，寿元 +%d → %s" % (heal, age_line(p)))
        g.say("（战斗中会自动服用，危急时救命）")
    else:
        g.say("这东西现在用不了。")

    p["pills"][name] = have - 1
    if p["pills"][name] <= 0:
        p["pills"].pop(name, None)


def act_rank(g: Game, args) -> None:
    players = [p for p in g.world["players"].values() if p.get("root")]
    if not players:
        g.say("还没有人踏上仙途。")
        return
    players.sort(key=lambda x: (-int(x.get("realm") or 0), -int(x.get("qi") or 0)))
    g.say("🏆 仙道榜")
    g.say("")
    for i, p in enumerate(players[:10], 1):
        g.say("%d. %s　%s　修为 %d　战力 %d" % (
            i, p["name"], realm_name(p), int(p.get("qi") or 0), power_of(p)))


def act_rebirth(g: Game, args) -> None:
    p = g.player()
    if p.get("alive", True) and not args.force:
        g.say("%s 尚在世（%s）。真要重开一世，发「重新投胎 确认」。" % (
            p["name"], realm_name(p)))
        return

    # 轮回只清修仙道果，农场与打劫的家当（土地、种子、守卫、装备、武力）属于另一条命，
    # 不该被"重新投胎"一并抹掉；灵石按规矩只留四分之一。
    keep_coins = int(p.get("coins") or 0) // 4
    old_realm = realm_name(p)
    for key in ("root", "realm", "qi", "sect", "sect_rank", "techniques",
                "active_technique", "pills", "tribulation_fails", "break_pill",
                "ward_pill", "last_tuina", "last_retreat", "last_adventure",
                "last_fight", "enlighten"):
        p.pop(key, None)
    p.update({
        "coins": keep_coins,
        "realm": 0,
        "qi": 0,
        "root": "",
        "enlighten": 0,
        "sect": "",
        "sect_rank": 0,
        "techniques": [],
        "active_technique": "",
        "pills": {},
        "tribulation_fails": 0,
        "last_tuina": 0,
        "tuina_charges": 0,
        "tuina_ts": now_ts(),
        "last_retreat": 0,
        "last_adventure": 0,
        "last_fight": 0,
        "lifespan": 80,
        "lifespan_max": 80,
        "alive": True,
    })
    core.migrate_player(p)
    p["daoxiang"] = (list(p.get("daoxiang") or []) + ["前世：%s境坐化" % old_realm])[-10:]
    g.say("🔄 轮回一转。")
    g.say("前世道果散尽，唯余灵石 %d 与一缕道心。" % keep_coins)
    g.say("农场与打劫的家当原样保留。")
    g.say("发「修仙」重新觉醒灵根。")


# ---------------------------------------------------------------- 一个隐藏动作：全局总览

def act_menu(g: Game, args) -> None:
    """两个玩法的总入口：用户问「能玩什么」时给一张不发散的目录。"""
    p = g.player()
    g.say("🎮 微信小游戏")
    g.say("")
    g.say("🌾【农场】")
    g.say("　种菜收菜　「种个土豆」「收个菜」「浇下水」")
    g.say("　偷菜打劫　「偷张三的菜」「打劫张三」「赌一把 100」")
    g.say("　买地升级　「买地」「买把铁剑」「农场等级」")
    g.say("")
    g.say("🌌【修仙】")
    g.say("　起步　　　「修仙」（觉醒灵根，随机六品）")
    g.say("　积累修为　「吐纳」（挂机攒灵气，回来收功）「闭关 10 年」（耗寿元）")
    g.say("　突破境界　「突破」（有成功率，失败损修为与寿元）")
    g.say("　机缘历练　「游历」「打一场」")
    g.say("　宗门功法　「宗门」「加入青云宗」「功法」「学 青元剑诀」")
    g.say("　丹药　　　「丹药」「买 破境丹」「用 破境丹」")
    g.say("　查榜　　　「修仙状态」「仙道榜」「我的资产」")
    g.say("")
    g.say("💰 农场金币 = 修仙灵石，同一笔钱。")
    g.say("境界越高，收菜经验越多、打劫武力越强。")
    if not p.get("root"):
        g.say("")
        g.say("你还没修仙。发「修仙」觉醒灵根试试。")


def act_overview(g: Game, args) -> None:
    p = g.player()
    g.say("📊 %s" % p["name"])
    g.say("💰 灵石 %d　🔰 农场等级 %d　📒 经验 %d" % (
        p.get("coins", 0), _farm_level(p), p.get("exp", 0)))
    if p.get("root"):
        g.say("🌌 %s · %s　修为 %d　战力 %d" % (
            p["root"], realm_name(p), p.get("qi", 0), power_of(p)))
    else:
        g.say("🌌 尚未修仙（发「修仙」开始）")
    g.say("🌾 土地 %d　🐕 守卫 %d" % (
        p.get("fields", 0), len(p.get("pets") or [])))
    if int(p.get("jail_turns") or 0) > 0:
        g.say("🔒 监禁中，剩 %d 回合" % p["jail_turns"])
    if int(p.get("lifespan") or 0) and p.get("root"):
        g.say(age_line(p))


def act_draw(g: Game, args) -> None:
    """用户主动点单要图：画师不忙就给，忙就说明情况让他等一会儿。

    没有时间冷却——只要画图通道不忙，随时可以再画一张。
    """
    p = g.player()
    if not _require_alive(g, p):
        return
    left, reason = draw_blocked(g, p)
    if left > 0:
        if reason == "self":
            g.say("🎨 你上一张还在画呢，约 %s 后就好，到时候再喊我。"
                  % fmt_duration(left))
        else:
            g.say("🎨 这会儿画师手上还有 %d 张没画完，忙不过来，大概 %s 后再来。"
                  % (world_draw_busy(g), fmt_duration(left)))
        return
    scene = (args.item or "").strip() or (
        "%s境修士立于云海之巅，衣袍猎猎" % realm_name(p))
    mark_draw(g, p)
    g.say("🎨 落笔成画：%s" % scene)
    g.draw(DRAW_STYLE % scene)

ACTIONS = {
    "menu": act_menu,
    "init": act_init,
    "status": act_status,
    "overview": act_overview,
    "tuina": act_tuina,
    "retreat": act_retreat,
    "breakthrough": act_breakthrough,
    "adventure": act_adventure,
    "fight": act_fight,
    "sect": act_sect,
    "learn": act_learn,
    "shop": act_shop,
    "buy": act_buy,
    "use": act_use,
    "rank": act_rank,
    "rebirth": act_rebirth,
    "draw": act_draw,
    "draw_done": act_draw_done,
}

# 这些动作不要求玩家已经建号（rank 是全场榜单）
NO_PLAYER_ACTIONS = {"rank"}


def main(argv=None) -> int:
    core.setup_stdio()

    parser = argparse.ArgumentParser(description="微信修仙引擎")
    parser.add_argument("action", choices=sorted(ACTIONS))
    parser.add_argument("--world", default=os.environ.get("GAME_WORLD", "default"))
    parser.add_argument("--player", default=os.environ.get("GAME_PLAYER", ""))
    parser.add_argument("--player-name", default=os.environ.get("GAME_PLAYER_NAME", ""))
    parser.add_argument("--item", default="")
    parser.add_argument("--kind", default="")
    parser.add_argument("--count", type=int, default=1)
    parser.add_argument("--years", type=int, default=0)
    parser.add_argument("--root", default="")
    parser.add_argument("--force", action="store_true")
    args = parser.parse_args(argv)

    with Game(args.world, args.player, args.player_name) as g:
        if args.action in NO_PLAYER_ACTIONS:
            ACTIONS[args.action](g, args)
            print(g.render())
            return 0
        g.player()  # 触发建号与迁移
        ACTIONS[args.action](g, args)
        print(g.render())
    return 0


if __name__ == "__main__":
    sys.exit(main())
