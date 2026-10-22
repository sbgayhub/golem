#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""微信小游戏 · 农场 + 打劫引擎。

不依赖 Golem 的 farm / robbery 插件，数据单独落盘，只要求 Python 3.8+ 标准库。
规则移植自 plugins/farm 与 plugins/robbery，整合点见 references/farm-rules.md。
存档与修仙（xiuxian_core.py）共用 games_core.py，两边是同一笔灵石。

用法（每次调用执行一个动作，输出可直接作为微信回复的纯文本）：

    python3 farm_core.py status  --world 群A --player wxid_x --player-name 老彭
    python3 farm_core.py plant   --world 群A --player wxid_x --item 土豆
    python3 farm_core.py harvest --world 群A --player wxid_x
    python3 farm_core.py steal   --world 群A --player wxid_x --target 张三
    python3 farm_core.py rob     --world 群A --player wxid_x [--target 张三]
    python3 farm_core.py shop    --world 群A --player wxid_x [--kind crop|pet|equip|profession]
    python3 farm_core.py buy     --world 群A --player wxid_x --item 铁剑 [--count 1]
    python3 farm_core.py gamble  --world 群A --player wxid_x --amount 100
    python3 farm_core.py rank    --world 群A

输出中若出现 `IMAGE:<绝对路径>` 行，表示可附带的配图（调用方转成 MEDIA: 标记）。
解释器：写死 /usr/local/lib/hermes-agent/venv/bin/python，脚本只用标准库。
"""

import argparse
import os
import random
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import games_core as core
from games_core import Game, ambiguous, now_ts  # noqa: E402

# ---------------------------------------------------------------- 数据表

# (等级, 名称, 种子价, 最少果实, 最多果实, 果实单价, 单果经验, 阶段小时, 阶段表情, 成熟表情)
CROPS = [
    (1,  "土豆", 10,  8,  12, 4,   4,   [1, 2, 3], ["🌱", "🌱", "🎍"], "🥔"),
    (2,  "萝卜", 20,  10, 15, 8,   4,   [1, 2, 3], ["🌱", "🎍", "🎍"], "🥕"),
    (3,  "花生", 30,  15, 17, 8,   4,   [1, 3, 4], ["🌱", "🎍", "🌿"], "🥜"),
    (4,  "番茄", 40,  10, 15, 20,  9,   [1, 3, 4], ["🌱", "🎍", "🌿"], "🍅"),
    (5,  "茄子", 50,  10, 15, 25,  12,  [2, 4, 5], ["🌱", "🎍", "🌿"], "🍆"),
    (6,  "辣椒", 120, 20, 25, 25,  12,  [2, 4, 5], ["🌱", "🎍", "🌾"], "🌶"),
    (7,  "蘑菇", 140, 25, 30, 25,  12,  [2, 4, 6], ["🌱", "🎍", "🌾"], "🍄"),
    (8,  "玉米", 160, 30, 35, 50,  20,  [2, 4, 6], ["🌱", "🎍", "🌾"], "🌽"),
    (11, "苹果", 220, 30, 35, 60,  30,  [3, 6, 8], ["🌱", "🎍", "🌳"], "🍎"),
    (13, "雪梨", 260, 30, 35, 70,  30,  [3, 6, 8], ["🌱", "🎍", "🌳"], "🍐"),
    (15, "桃子", 300, 30, 35, 100, 70,  [3, 6, 8], ["🌱", "🎍", "🌳"], "🍑"),
    (17, "橙子", 510, 30, 35, 150, 100, [3, 6, 8], ["🌱", "🎍", "🌳"], "🍊"),
    (19, "柠檬", 999, 30, 35, 200, 150, [3, 6, 8], ["🌱", "🎍", "🌳"], "🍋"),
]

CROP_FIELDS = ("level", "name", "price", "fmin", "fmax", "fruit_price",
               "fruit_exp", "steps", "step_emojis", "emoji")
CROP_BY_NAME = {}
CROP_BY_LEVEL = {}
CROP_LIST = []
for _row in CROPS:
    _c = dict(zip(CROP_FIELDS, _row))
    CROP_BY_NAME[_c["name"]] = _c
    CROP_BY_LEVEL[_c["level"]] = _c
    CROP_LIST.append(_c)

PETS = {
    # 守卫：防偷菜，并降低被"打劫"的成功率（整合点）
    "斗牛犬": {"level": 1, "price": 10000, "rob_dodge": 0.15},
}

PROFESSIONS = {
    "盗贼": {"strength": 20, "money": 10, "skill": 0.15, "desc": "打劫成功率 +15%"},
    "战士": {"strength": 30, "money": 5,  "skill": 0.00, "desc": "初始武力 +30"},
    "商人": {"strength": 10, "money": 20, "skill": 0.05, "desc": "打劫收益 +20%，赌博胜率 +10%"},
    "刺客": {"strength": 25, "money": 15, "skill": 0.10, "desc": "暗杀：暴击率 +10%"},
    "牧师": {"strength": 15, "money": 12, "skill": 0.08, "desc": "治愈：每小时恢复 5 金币"},
}

EQUIPMENT = {
    "木棍": {"price": 50,  "strength": 5,  "desc": "简陋的武器"},
    "铁剑": {"price": 150, "strength": 12, "desc": "锋利的铁剑"},
    "钢刀": {"price": 400, "strength": 20, "desc": "精钢打造的刀"},
    "匕首": {"price": 300, "strength": 15, "desc": "刺客的最爱"},
    "护甲": {"price": 500, "strength": 0,  "desc": "打劫失败时损失 -30%"},
    "头盔": {"price": 200, "strength": 0,  "desc": "额外保护"},
}

# ---------------------------------------------------------------- 常量

# 初始金币 / 土地 / 武力在 games_core.new_player() 里给（两个玩法共用），此处不再重复定义
MAX_SEED_PER_CROP = 99
MAX_STEALERS_PER_FIELD = 2
CHANGE_PROF_COST = 500
BASE_WANTED_CHANCE = 0.2
CRIME_WANTED_STEP = 0.1
EVENT_CHANCE = 0.3
JOB_COOLDOWN_H = 24
WELFARE_THRESHOLD = 100
WELFARE_AMOUNT = 50
WELFARE_COOLDOWN_H = 24
JAIL_TURNS = 3
STEAL_CRIME = 1          # 偷菜也算一次犯罪（整合点：偷菜与打劫共用通缉值）

MATURE = -1


# ---------------------------------------------------------------- 数值

def compute_level(exp: int) -> int:
    for i in range(21, 0, -1):
        if exp >= (i ** 4 - 1) // 5:
            return i
    return 0


def field_price(current: int) -> int:
    return int(2.5 ** (0.75 * current) * current) * 1000


# 修仙境界对农场/打劫的加成（整合点）：境界越高，干活越省力、打架越狠。
# 下标与 xiuxian_core.REALMS 一致；只取前几境的温和加成，避免修仙碾压农场玩法。
REALM_FARM_BONUS = {
    0: (1.00, 0.00),   # 凡人
    1: (1.10, 0.10),   # 淬体
    2: (1.20, 0.25),   # 聚气
    3: (1.35, 0.50),   # 筑基
    4: (1.50, 1.00),   # 金丹
    5: (1.70, 1.80),   # 元婴
    6: (2.00, 3.00),   # 化神
    7: (2.40, 5.00),   # 炼虚
    8: (3.00, 8.00),   # 合道
    9: (4.00, 13.0),   # 渡劫
    10: (5.00, 20.0),  # 大乘
    11: (7.00, 30.0),  # 飞升
}


def realm_bonus(p: dict):
    """返回 (收菜经验倍率, 武力加成)。没修仙就是 (1.0, 0)。"""
    if not p.get("root"):
        return 1.0, 0
    idx = max(0, min(11, int(p.get("realm") or 0)))
    return REALM_FARM_BONUS[idx]


def effective_strength(p: dict) -> int:
    s = int(p.get("strength", 0))
    prof = PROFESSIONS.get(p.get("profession", ""))
    if prof:
        s += prof["strength"]
    for name in p.get("equipment", []):
        e = EQUIPMENT.get(name)
        if e:
            s += e["strength"]
    return int(s + realm_bonus(p)[1])


def crop_state(crop: dict, planted: int, now: int):
    """返回 (阶段序号, 表情)；阶段序号 = MATURE 表示成熟。"""
    elapsed = now - planted
    state, emoji = 0, crop["step_emojis"][0]
    steps = crop["steps"]
    for i, hours in enumerate(steps):
        state, emoji = i, crop["step_emojis"][i]
        band = hours * 3600
        if elapsed > band:
            if i == len(steps) - 1:
                return MATURE, crop["emoji"]
            elapsed -= band
        else:
            break
    return state, emoji


def growth_index(crop: dict, planted: int, now: int) -> int:
    total = sum(crop["steps"]) * 3600
    elapsed = max(0, now - planted)
    if total <= 0 or elapsed >= total:
        return 4
    ratio = elapsed / total
    return max(0, min(3, int(ratio * 4)))


# ---------------------------------------------------------------- 动作实现

def _jail_tick(g: Game, p: dict) -> bool:
    """坐牢期间所有动作停摆；每次动作消耗一回合。返回 True 表示本次被拦下。"""
    turns = int(p.get("jail_turns", 0))
    if turns <= 0:
        p["jail_turns"] = 0
        return False
    turns -= 1
    p["jail_turns"] = turns
    if turns <= 0:
        g.say("✅ %s 刑满释放，重获自由！" % p["name"])
        g.say("💰 当前金币：%d" % p["coins"])
        return True
    g.say("🔒 %s 正在服刑，剩余 %d 回合，农场和打劫都无法进行。" % (p["name"], turns))
    return True


def act_help(g: Game, args) -> None:
    g.say("🌾🎭【农场 + 打劫】玩法")
    g.say("")
    g.say("🌱 农场：买种子 → 种下 → 等成熟 → 收菜换金币")
    g.say("　　shop / buy <物品> / plant <作物> / harvest")
    g.say("　　water [@某人] 浇水加产量　steal @某人 偷菜")
    g.say("　　land 买地　level 等级　status 我的农场")
    g.say("")
    g.say("🗡 打劫：武力越高越容易得手")
    g.say("　　rob [@某人] 打劫　gamble <金额> 赌博")
    g.say("　　job 每日任务　welfare 破产救济　rank 排行榜")
    g.say("")
    g.say("💡 金币是唯一货币：种菜赚的钱能买装备和转职，打劫抢来的也能买种子。")
    g.say("💡 斗牛犬既防偷菜，也让打劫你的人更难成功。")
    g.image("farm", "菜单_农场.jpg")


def act_shop(g: Game, args) -> None:
    p = g.player()
    level = compute_level(p["exp"])
    kind = (args.kind or "").strip()

    if kind in ("", "crop"):
        g.say("🔰 农场商店　　　🔆金币　🏕库存")
        for crop in CROP_LIST:
            owned = p["seeds"].get(str(crop["level"]), 0)
            g.say("%02d %s%s %d　(存 %d)" % (
                crop["level"], crop["emoji"], crop["name"], crop["price"], owned))
        g.say("")
        g.say("当前：等级 %d　金币 %d" % (level, p["coins"]))
        if kind:
            return

    if kind in ("", "pet"):
        g.say("")
        g.say("🐕 守卫商店")
        for name, pet in PETS.items():
            owned = "🈶" if pet["level"] in p["pets"] else "🈚"
            g.say("%s　%d 金币　%s" % (name, pet["price"], owned))
            g.say("　防偷菜；被抢时额外闪避 15%")
        g.say("当前：等级 %d　金币 %d" % (level, p["coins"]))
        if kind:
            return

    if kind in ("", "equip"):
        g.say("")
        g.say("⚔️ 装备商店")
        for name, e in EQUIPMENT.items():
            g.say("%s　%d 金币　武力+%d　%s" % (name, e["price"], e["strength"], e["desc"]))
        g.say("当前：武力 %d　金币 %d" % (effective_strength(p), p["coins"]))
        if kind:
            return

    if kind in ("", "profession"):
        g.say("")
        g.say("🎭 职业（转职 %d 金币）" % CHANGE_PROF_COST)
        for name, prof in PROFESSIONS.items():
            mark = "（当前）" if p.get("profession") == name else ""
            g.say("%s　武力+%d 金币+%d　%s%s" % (
                name, prof["strength"], prof["money"], prof["desc"], mark))
        if p.get("profession"):
            g.say("当前职业：%s" % p["profession"])


def act_buy(g: Game, args) -> None:
    p = g.player()
    item = (args.item or "").strip()
    count = max(1, int(args.count or 1))
    if not item:
        g.say("要买什么？例如：买土豆、买铁剑、转职盗贼、买斗牛犬")
        return

    if item in CROP_BY_NAME:
        crop = CROP_BY_NAME[item]
        level = compute_level(p["exp"])
        if crop["level"] > level:
            g.say("购买%s需要 %d 级，你当前 %d 级。" % (item, crop["level"], level))
            return
        cost = crop["price"] * count
        if p["coins"] < cost:
            g.say("金币不足：买 %d 个%s种需要 %d，你只有 %d。" % (count, item, cost, p["coins"]))
            return
        key = str(crop["level"])
        cur = p["seeds"].get(key, 0)
        if cur + count > MAX_SEED_PER_CROP:
            g.say("同一种种子最多持有 %d 个，你现在有 %d 个。" % (MAX_SEED_PER_CROP, cur))
            return
        p["seeds"][key] = cur + count
        p["coins"] -= cost
        g.say("✅ 购买成功")
        g.say("%s %d → %d" % (crop["emoji"], cur, cur + count))
        g.say("🔆 -%d → %d" % (cost, p["coins"]))
        return

    if item in PETS:
        pet = PETS[item]
        if pet["level"] in p["pets"]:
            g.say("你已经有一只%s了。" % item)
            return
        if p["coins"] < pet["price"]:
            g.say("金币不足：%s要 %d，你只有 %d。" % (item, pet["price"], p["coins"]))
            return
        p["coins"] -= pet["price"]
        p["pets"].append(pet["level"])
        g.say("✅ 购买成功　🐕 %s" % item)
        g.say("🔆 -%d → %d" % (pet["price"], p["coins"]))
        return

    if item in EQUIPMENT:
        e = EQUIPMENT[item]
        if item in p["equipment"]:
            g.say("你已经拥有%s了。" % item)
            return
        if p["coins"] < e["price"]:
            g.say("金币不足：%s要 %d，你只有 %d。" % (item, e["price"], p["coins"]))
            return
        p["coins"] -= e["price"]
        p["equipment"].append(item)
        g.say("✅ 购买成功　⚔️ %s" % item)
        g.say("当前：金币 %d　武力 %d" % (p["coins"], effective_strength(p)))
        return

    if item in PROFESSIONS:
        prof = PROFESSIONS[item]
        if p["coins"] < CHANGE_PROF_COST:
            g.say("转职需要 %d 金币，你还差 %d。" % (
                CHANGE_PROF_COST, CHANGE_PROF_COST - p["coins"]))
            return
        p["coins"] -= CHANGE_PROF_COST
        p["profession"] = item
        p["strength"] += prof["strength"]
        p["coins"] += prof["money"]
        g.say("✅ 转职成功：🎭 %s" % item)
        g.say("当前：金币 %d　武力 %d" % (p["coins"], effective_strength(p)))
        return

    g.say("没有「%s」这个物品，先看 shop。" % item)


def act_plant(g: Game, args) -> None:
    p = g.player()
    item = (args.item or "").strip()
    if not item:
        g.say("要种什么？例如：种土豆")
        return
    crop = CROP_BY_NAME.get(item)
    if not crop:
        g.say("没有「%s」这种作物，先看 shop。" % item)
        return

    # 先说结论，再说细节：群里只看到第一行「种子不足」会以为是失败
    free = [str(i) for i in range(int(p["fields"]))
            if not (p["land"].get(str(i)) or {}).get("level")]
    if not free:
        g.say("🌱 你现在的地都种满了，先「收个菜」，或者「买地」扩一块。")
        return
    stock = int(p.get("seeds", {}).get(str(crop["level"])) or 0)
    if stock <= 0:
        g.say("🌱 你手上没有%s种子，先在商店买（发「商店」或「买 %s」）。" % (
            crop["name"], crop["name"]))
        return

    now = now_ts()
    exp_gain = 0
    planted_any = False
    for i in range(int(p["fields"])):
        key = str(i)
        land = p["land"].get(key)
        if land and land.get("level", 0) > 0:
            c = CROP_BY_LEVEL.get(land["level"])
            state, emoji = crop_state(c, land["planted"], now)
            g.say("土地(%d) %s %s 已存在" % (i + 1, emoji, c["name"]))
            continue
        stock = int(p["seeds"].get(str(crop["level"])) or 0)
        if stock > 0:
            p["seeds"][str(crop["level"])] = stock - 1
            p["land"][key] = {"level": crop["level"], "planted": now,
                              "watered": {}, "stealers": []}
            exp_gain += crop["fruit_exp"]
            planted_any = True
            g.say("土地(%d) → %s" % (i + 1, crop["emoji"]))
        else:
            g.say("土地(%d) %s种子不足" % (i + 1, crop["name"]))

    if not planted_any:
        # 一颗都没种下（地已满/种子刚好用完），给一句能照做的下一步
        g.say("")
        g.say("💡 地都占着就先「收个菜」，或者「买地」扩一块。")
    if exp_gain:
        p["exp"] += exp_gain
        g.say("")
        g.say("📒 +%d → %d" % (exp_gain, p["exp"]))
    if planted_any:
        g.image("farm", "植物_已耕.jpg")
        g.image("farm", "植物一_1.png")


def act_harvest(g: Game, args) -> None:
    p = g.player()
    now = now_ts()
    exp_gain = coins_gain = 0
    waterers, stealers = set(), set()
    any_harvest = False

    for i in range(int(p["fields"])):
        key = str(i)
        land = p["land"].get(key)
        if not land or land.get("level", 0) <= 0:
            g.say("土地(%d) 未种植" % (i + 1))
            continue
        crop = CROP_BY_LEVEL.get(land["level"])
        state, emoji = crop_state(crop, land["planted"], now)
        if state != MATURE:
            watered = str(state) in (land.get("watered") or {})
            mark = "💦" if watered else ""
            g.say("土地(%d) %s%s %s 未成熟" % (i + 1, emoji, mark, crop["name"]))
            continue

        n = random.randint(crop["fmin"], crop["fmax"]) if crop["fmax"] > crop["fmin"] else crop["fmax"]
        n += len(land.get("watered") or {})
        for who, by in (land.get("watered") or {}).items():
            if by != p["id"]:
                waterers.add(by)
        stolen = len(land.get("stealers") or [])
        n = max(0, n - stolen)
        for who in land.get("stealers") or []:
            stealers.add(who)

        g.say("土地(%d) %s %s %d 枚%s" % (
            i + 1, emoji, crop["name"], n, "（被偷 %d 枚）" % stolen if stolen else ""))
        exp_gain += n * crop["fruit_exp"]
        coins_gain += n * crop["fruit_price"]
        del p["land"][key]
        any_harvest = True

    if coins_gain or exp_gain:
        # 修仙境界加成只作用于农场经验（灵识清明，种地更有体会），不影响金币
        mult = realm_bonus(p)[0]
        exp_gain = int(exp_gain * mult)
        p["exp"] += exp_gain
        p["coins"] += coins_gain
        g.say("")
        if waterers:
            g.say("帮你浇水的群友：")
            for who in waterers:
                g.say("　　" + _name_of(g, who))
        if stealers:
            g.say("偷你菜的群友：")
            for who in stealers:
                g.say("　　" + _name_of(g, who))
        g.say("📒 +%d → %d" % (exp_gain, p["exp"]))
        g.say("🔆 +%d → %d" % (coins_gain, p["coins"]))
    elif not any_harvest:
        g.image("farm", "植物_未耕.jpg")


def act_water(g: Game, args) -> None:
    me = g.player()
    target = me
    if (args.target or "").strip():
        target, cands = g.find(args.target)
        if target is None:
            _ambiguous(g, args.target, cands)
            return

    now = now_ts()
    exp_gain = 0
    g.say("%s的农场" % target["name"] if target["id"] != me["id"] else "浇水@一个人可以给群友浇水")

    for i in range(int(target["fields"])):
        key = str(i)
        land = target["land"].get(key)
        if not land or land.get("level", 0) <= 0:
            g.say("土地(%d) 未种植" % (i + 1))
            continue
        crop = CROP_BY_LEVEL.get(land["level"])
        state, emoji = crop_state(crop, land["planted"], now)
        if state == MATURE:
            g.say("土地(%d) %s %s 已成熟" % (i + 1, emoji, crop["name"]))
            continue
        skey = str(state)
        if skey in (land.get("watered") or {}):
            g.say("土地(%d) %s💦 %s 无需浇水" % (i + 1, emoji, crop["name"]))
            continue
        land.setdefault("watered", {})[skey] = me["id"]
        exp_gain += crop["fruit_exp"]
        g.say("土地(%d) %s🌧 %s 浇水成功" % (i + 1, emoji, crop["name"]))

    if exp_gain:
        me["exp"] += exp_gain
        g.say("")
        g.say("📒 +%d → %d" % (exp_gain, me["exp"]))


def act_steal(g: Game, args) -> None:
    me = g.player()
    raw = (args.target or "").strip()
    if not raw:
        g.say("偷菜要指定对象，例如：偷菜@张三")
        return
    target, cands = g.find(raw)
    if target is None:
        _ambiguous(g, raw, cands)
        return
    if target["id"] == me["id"]:
        g.say("你不能偷自己的菜。")
        return

    now = now_ts()
    g.say("偷偷进入了 %s 的农场" % target["name"])
    g.say("")

    # 守卫：狗在时会咬人（沿用原插件规则）
    if PETS["斗牛犬"]["level"] in target.get("pets", []):
        sleepy = random.random() < 0.5
        alert_pct = 10 if sleepy else 20
        g.say("🐕斗牛犬%s" % ("正在瞌睡 " if sleepy else ""))
        if random.random() * 100 < alert_pct:
            fine = min(100, max(0, me["coins"]))
            me["coins"] -= fine
            g.say("把你咬了，损失 🔆%d" % fine)
            return

    exp_gain = coins_gain = 0
    for i in range(int(target["fields"])):
        key = str(i)
        land = target["land"].get(key)
        if not land or land.get("level", 0) <= 0:
            g.say("土地(%d) 未种植" % (i + 1))
            continue
        crop = CROP_BY_LEVEL.get(land["level"])
        state, emoji = crop_state(crop, land["planted"], now)
        if state != MATURE:
            g.say("土地(%d) %s %s 未成熟" % (i + 1, emoji, crop["name"]))
            continue
        stealers = land.setdefault("stealers", [])
        if me["id"] in stealers:
            g.say("土地(%d) %s %s 偷过了" % (i + 1, emoji, crop["name"]))
            continue
        if len(stealers) >= MAX_STEALERS_PER_FIELD:
            g.say("土地(%d) %s %s 快被偷光了" % (i + 1, emoji, crop["name"]))
            continue
        stealers.append(me["id"])
        exp_gain += crop["fruit_exp"]
        coins_gain += crop["fruit_price"]
        g.say("土地(%d) %s %s 1 枚" % (i + 1, emoji, crop["name"]))

    if coins_gain:
        me["exp"] += exp_gain
        me["coins"] += coins_gain
        me["crime"] += STEAL_CRIME
        g.say("")
        g.say("📒 +%d → %d" % (exp_gain, me["exp"]))
        g.say("🔆 +%d → %d" % (coins_gain, me["coins"]))
        if STEAL_CRIME:
            g.say("📝 犯罪 +%d → %d（被通缉概率上升）" % (STEAL_CRIME, me["crime"]))


def act_land(g: Game, args) -> None:
    p = g.player()
    price = field_price(int(p["fields"]))
    if p["coins"] >= price:
        p["coins"] -= price
        p["fields"] += 1
        g.say("✅ 购买土地成功，土地 +1")
        g.say("🔆 -%d → %d" % (price, p["coins"]))
        g.say("📜 现有土地：%d" % p["fields"])
    else:
        g.say("购买第 %d 块土地需要 🔆%d，你还差 %d。" % (
            p["fields"] + 1, price, price - p["coins"]))


def act_level(g: Game, args) -> None:
    p = g.player()
    level = compute_level(p["exp"])
    if level >= 20:
        g.say("当前农场等级 %d 级（📒%d），已满级。" % (level, p["exp"]))
        return
    need = ((level + 1) ** 4 - 1) // 5 - p["exp"]
    g.say("当前农场等级 %d 级（📒%d），距离升级还需 📒%d。" % (level, p["exp"], need))


def act_status(g: Game, args) -> None:
    p = g.player()
    now = now_ts()
    level = compute_level(p["exp"])
    g.say("📊 %s 的农场与资产" % p["name"])
    g.say("🔆金币 %d　📒经验 %d　🔰等级 %d" % (p["coins"], p["exp"], level))
    g.say("📜土地 %d　💪武力 %d" % (p["fields"], effective_strength(p)))
    if p.get("profession"):
        g.say("🎭职业 %s" % p["profession"])
    if p.get("jail_turns", 0) > 0:
        g.say("🔒监禁中：剩余 %d 回合" % p["jail_turns"])
    if p.get("crime", 0):
        g.say("📝犯罪记录 %d 次" % p["crime"])

    seeds = [c["emoji"] + c["name"] + "x" + str(p["seeds"][str(c["level"])])
             for c in CROP_LIST if p["seeds"].get(str(c["level"]), 0) > 0]
    g.say("🌱种子：" + ("，".join(seeds) if seeds else "无"))
    pets = [name for name, pet in PETS.items() if pet["level"] in p.get("pets", [])]
    g.say("🐕守卫：" + ("，".join(pets) if pets else "无"))
    equips = p.get("equipment") or []
    g.say("⚔️装备：" + ("，".join(equips) if equips else "无"))
    if p.get("rob_total"):
        g.say("🗡打劫：%d 次，成功 %d 次" % (p["rob_total"], p["rob_win"]))

    g.say("")
    g.say("🌾 土地状态")
    max_stage = -1
    for i in range(int(p["fields"])):
        land = p["land"].get(str(i))
        if not land or land.get("level", 0) <= 0:
            g.say("土地(%d) 未种植" % (i + 1))
            continue
        crop = CROP_BY_LEVEL.get(land["level"])
        state, emoji = crop_state(crop, land["planted"], now)
        if state == MATURE:
            g.say("土地(%d) %s %s 🌟可收" % (i + 1, emoji, crop["name"]))
            max_stage = 4
        else:
            left = _time_left(crop, land["planted"], now)
            g.say("土地(%d) %s %s（%s 后成熟）" % (i + 1, emoji, crop["name"], left))
            max_stage = max(max_stage, growth_index(crop, land["planted"], now))

    if max_stage < 0:
        g.image("farm", "植物_未耕.jpg")
    else:
        g.image("farm", "植物一_%d.png" % min(4, max_stage) if max_stage < 4 else "植物_已耕.jpg")


def act_rob(g: Game, args) -> None:
    me = g.player()
    me["rob_total"] = int(me.get("rob_total", 0)) + 1

    if (args.target or "").strip():
        target, cands = g.find(args.target)
        if target is None:
            _ambiguous(g, args.target, cands)
            return
        if target["id"] == me["id"]:
            g.say("不能打劫自己。")
            return
        if target.get("jail_turns", 0) > 0 or target["coins"] <= 0:
            g.say("%s 现在没有可抢的东西。" % target["name"])
            return
    else:
        pool = [p for p in g.world["players"].values()
                if p["id"] != me["id"] and p.get("jail_turns", 0) <= 0 and p["coins"] > 0]
        if not pool:
            g.say("现在没有可以打劫的目标~")
            return
        target = random.choice(pool)

    chance = _rob_chance(me, target)
    if PETS["斗牛犬"]["level"] in target.get("pets", []):
        chance *= (1 - PETS["斗牛犬"]["rob_dodge"])
        dog_note = "🐕 %s 的斗牛犬冲你狂吠，得手更难。\n" % target["name"]
    else:
        dog_note = ""

    if random.random() < chance:
        taken = _stolen_amount(target)
        if me.get("profession") == "商人":
            taken = int(taken * 1.2)
        me["coins"] += taken
        target["coins"] = max(0, target["coins"] - taken)
        me["crime"] += 1
        me["rob_win"] = int(me.get("rob_win", 0)) + 1
        if dog_note:
            g.say(dog_note.rstrip())
        g.say("✅ %s 成功打劫了 %s，抢得 🔆%d" % (me["name"], target["name"], taken))
        g.say("当前：🔆%d　武力 %d" % (me["coins"], effective_strength(me)))
        if random.random() < EVENT_CHANCE:
            g.say("")
            _random_event(g, me)
        _wanted_check(g, me)
    else:
        penalty = _penalty_amount(me)
        me["coins"] = max(0, me["coins"] - penalty)
        target["coins"] += penalty
        if dog_note:
            g.say(dog_note.rstrip())
        g.say("❌ %s 打劫失败，反被 %s 抢走 🔆%d" % (me["name"], target["name"], penalty))
        g.say("剩余：🔆%d" % me["coins"])


def act_gamble(g: Game, args) -> None:
    p = g.player()
    amount = int(args.amount or 0)
    if amount <= 0:
        g.say("赌多少？例如：赌博 100")
        return
    if p["coins"] < amount:
        g.say("金币不够下注：你有 🔆%d" % p["coins"])
        return
    win_chance = 0.5 + (0.1 if p.get("profession") == "商人" else 0.0)
    if random.random() < win_chance:
        gain = amount * 2
        p["coins"] += gain
        g.say("🎲 赢了！赢得 🔆%d" % gain)
        g.say("当前：🔆%d" % p["coins"])
    else:
        p["coins"] -= amount
        g.say("❌ 输了，损失 🔆%d" % amount)
        g.say("剩余：🔆%d" % p["coins"])


def act_job(g: Game, args) -> None:
    p = g.player()
    hours = (now_ts() * 1000 - int(p.get("last_job", 0))) // (1000 * 3600)
    if hours < JOB_COOLDOWN_H:
        g.say("📋 任务还没刷新，还需等待 %d 小时。" % (JOB_COOLDOWN_H - hours))
        return
    tasks = ["打劫成功 3 次", "积累 1000 金币", "武力达到 50", "给群友浇水 3 次"]
    task = random.choice(tasks)
    reward = random.randint(100, 300)
    p["coins"] += reward
    p["last_job"] = now_ts() * 1000
    g.say("📋 今日任务：%s" % task)
    g.say("💰 完成奖励：🔆%d" % reward)
    g.say("当前：🔆%d" % p["coins"])


def act_welfare(g: Game, args) -> None:
    p = g.player()
    if p["coins"] >= WELFARE_THRESHOLD:
        g.say("你的钱还够花，不需要救济~")
        return
    hours = (now_ts() * 1000 - int(p.get("last_welfare", 0))) // (1000 * 3600)
    if hours < WELFARE_COOLDOWN_H:
        g.say("🕐 救济金冷却中，还需 %d 小时。" % (WELFARE_COOLDOWN_H - hours))
        return
    p["coins"] += WELFARE_AMOUNT
    p["last_welfare"] = now_ts() * 1000
    g.say("✅ 领取救济金 🔆%d" % WELFARE_AMOUNT)
    g.say("当前：🔆%d" % p["coins"])


def act_rank(g: Game, args) -> None:
    players = list(g.world["players"].values())
    if not players:
        g.say("这个场地还没有玩家数据~")
        return
    players.sort(key=lambda x: (-int(x.get("coins", 0)), -int(x.get("exp", 0))))
    g.say("🏆 财富排行榜")
    g.say("")
    for i, p in enumerate(players[:10], 1):
        g.say("%d. %s　🔆%d　🔰%d级" % (
            i, p["name"], p["coins"], compute_level(p["exp"])))


# ---------------------------------------------------------------- 打劫辅助

def _rob_chance(attacker: dict, defender: dict) -> float:
    a = float(effective_strength(attacker)) ** 2
    d = float(effective_strength(defender)) ** 2
    base = a / (a + d) if (a + d) > 0 else 0.5
    prof = PROFESSIONS.get(attacker.get("profession", ""))
    bonus = prof["skill"] if prof else 0.0
    chance = base * 0.8 + 0.1 + bonus + random.random() * 0.2
    return max(0.1, min(0.9, chance))


def _stolen_amount(target: dict) -> int:
    if target["coins"] <= 0:
        return 0
    bonus = 1.2 if target.get("profession") == "商人" else 1.0
    hi = int(target["coins"] * 0.5 * bonus)
    lo = int(target["coins"] * 0.2 * bonus)
    if hi <= lo:
        return 1
    return random.randint(lo, hi)


def _penalty_amount(attacker: dict) -> int:
    if attacker["coins"] <= 0:
        return 0
    reduction = 0.7 if "护甲" in (attacker.get("equipment") or []) else 1.0
    hi = int(attacker["coins"] * 0.3 * reduction)
    lo = int(attacker["coins"] * 0.1 * reduction)
    if hi <= lo:
        return 1
    return random.randint(lo, hi)


def _wanted_check(g: Game, p: dict) -> None:
    chance = BASE_WANTED_CHANCE + float(p.get("crime", 0)) * CRIME_WANTED_STEP
    if random.random() >= chance:
        return
    guards = random.randint(1, 3)
    total = guards * 25
    g.say("")
    g.say("⚠️ 恶行引起卫兵注意！%d 名卫兵正在追捕你！" % guards)
    if effective_strength(p) > total * 0.6:
        g.say("🏃 你成功摆脱了 %d 名卫兵的追捕！" % guards)
        return
    fine = int(p["coins"] * 0.4)
    p["coins"] = max(0, p["coins"] - fine)
    p["jail_turns"] = JAIL_TURNS
    p["crime"] = 0
    g.say("🔒 你被抓获！罚金 🔆%d，监禁 %d 回合" % (fine, JAIL_TURNS))
    g.say("（服刑期间农场和打劫都会停摆，每发一次指令消耗一回合）")


def _random_event(g: Game, p: dict) -> None:
    roll = random.randint(0, 99)
    if roll < 40:
        cost = random.randint(50, 200)
        if p["coins"] >= cost:
            p["coins"] -= cost
            p["strength"] += 3
            g.say("🕶️ 黑市商人出售强化药剂：支付 🔆%d，武力 +3（现 %d）" % (cost, p["strength"]))
        else:
            g.say("🕶️ 黑市商人出现，但你的钱不够买强化药剂…")
    elif roll < 70:
        windfall = random.randint(200, 500)
        p["coins"] += windfall
        g.say("💰 你发现了逃跑的运钞车，捡到 🔆%d！" % windfall)
    else:
        cut = min(2, int(p.get("crime", 0)))
        p["crime"] = max(0, int(p.get("crime", 0)) - cut)
        g.say("💼 神秘商人提供洗钱服务，犯罪记录 -%d" % cut)


# ---------------------------------------------------------------- 杂项

def _time_left(crop: dict, planted: int, now: int) -> str:
    total = sum(crop["steps"]) * 3600
    left = max(0, total - (now - planted))
    if left >= 3600:
        return "%d小时%d分" % (left // 3600, (left % 3600) // 60)
    return "%d分钟" % max(1, left // 60)


ACTIONS = {
    "help": act_help,
    "menu": act_help,
    "status": act_status,
    "me": act_status,
    "shop": act_shop,
    "buy": act_buy,
    "plant": act_plant,
    "harvest": act_harvest,
    "collect": act_harvest,
    "water": act_water,
    "steal": act_steal,
    "land": act_land,
    "level": act_level,
    "rob": act_rob,
    "gamble": act_gamble,
    "job": act_job,
    "welfare": act_welfare,
    "rank": act_rank,
}


def main(argv=None) -> int:
    # Windows 控制台默认 GBK，中文错误信息到 stderr 会变乱码；统一成 UTF-8
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            try:
                stream.reconfigure(encoding="utf-8", errors="replace")
            except (AttributeError, ValueError):
                pass

    parser = argparse.ArgumentParser(description="Hermes 农场 + 打劫引擎")
    parser.add_argument("action", choices=sorted(ACTIONS))
    # world/player 默认空：空 world 归到 default，空 player 由调用方保证（脚本会报错）
    parser.add_argument("--world", default=os.environ.get("FARM_WORLD", "default"))
    parser.add_argument("--player", default=os.environ.get("FARM_PLAYER", ""))
    parser.add_argument("--player-name", default=os.environ.get("FARM_PLAYER_NAME", ""))
    parser.add_argument("--target", default="")
    parser.add_argument("--item", default="")
    parser.add_argument("--count", type=int, default=1)
    parser.add_argument("--amount", type=int, default=0)
    parser.add_argument("--kind", default="")
    args = parser.parse_args(argv)

    with Game(args.world, args.player, args.player_name) as g:
        # help / rank 不绑定玩家
        if args.action in ("help", "menu", "rank"):
            ACTIONS[args.action](g, args)
            print(g.render())
            return 0
        p = g.player()
        # 坐牢拦截：只放行查看类动作
        if args.action not in ("status", "me", "level", "shop", "rank", "help", "menu"):
            if _jail_tick(g, p):
                print(g.render())
                return 0
        ACTIONS[args.action](g, args)
        print(g.render())
    return 0


if __name__ == "__main__":
    sys.exit(main())
