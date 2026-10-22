#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""微信小游戏共用底座：存档、文件锁、玩家模型、输出渲染。

农场（farm_core.py）与修仙（xiuxian_core.py）都从 import 这里取同一套状态，
所以两个玩法的灵石/金币是同一个数，`status` 能一次看到全部资产。

只依赖 Python 标准库。Python 3.8+（Linux）与 3.8+（Windows）均可。
"""

import json
import os
import sys
import time

STATE_VERSION = 2          # v1=只有农场；v2=加修仙字段（迁移是幂等的，见 migrate_player）
DEFAULT_WORLD = "default"

# 两个玩法共用的一笔钱。农场叫「阳光」、修仙叫「灵石」，是同一枚硬币的两面。
START_COINS = 3000


# ---------------------------------------------------------------- 路径

def hermes_home() -> str:
    home = os.environ.get("HERMES_HOME", "").strip()
    if home:
        return os.path.expanduser(home)
    return os.path.join(os.path.expanduser("~"), ".hermes")


def data_path() -> str:
    # 老变量名 HERMES_FARM_DATA 保留兼容：已经部署的机器不用改环境变量
    override = (os.environ.get("HERMES_GAMES_DATA")
                or os.environ.get("HERMES_FARM_DATA") or "").strip()
    if override:
        return os.path.expanduser(override)
    return os.path.join(hermes_home(), "wechat_games", "state.json")


def skill_dir() -> str:
    return os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def asset(*parts: str) -> str:
    return os.path.join(skill_dir(), "assets", *parts)


# ---------------------------------------------------------------- 锁

try:
    import fcntl

    def _lock(fh):
        fcntl.flock(fh, fcntl.LOCK_EX)

    def _unlock(fh):
        fcntl.flock(fh, fcntl.LOCK_UN)

except ImportError:  # Windows
    import msvcrt

    def _lock(fh):
        while True:
            try:
                fh.seek(0)
                msvcrt.locking(fh.fileno(), msvcrt.LK_LOCK, 1)
                return
            except OSError:
                # LK_LOCK 自己会重试 10 次；超出就睡一下再来，避免并发写坏档
                time.sleep(0.05)

    def _unlock(fh):
        try:
            fh.seek(0)
            msvcrt.locking(fh.fileno(), msvcrt.LK_UNLCK, 1)
        except OSError:
            pass


# ---------------------------------------------------------------- 存档

def _empty_state() -> dict:
    return {"version": STATE_VERSION, "worlds": {}}


def load_state() -> dict:
    path = data_path()
    if not os.path.exists(path):
        return _empty_state()
    try:
        with open(path, "r", encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, ValueError):
        # 损坏隔离，绝不静默覆盖用户存档
        try:
            os.replace(path, path + ".corrupt-%d" % int(time.time()))
        except OSError:
            pass
        return _empty_state()
    if not isinstance(data, dict) or not isinstance(data.get("worlds"), dict):
        return _empty_state()
    data.setdefault("version", 1)
    return data


def save_state(state: dict) -> None:
    path = data_path()
    os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
    tmp = "%s.tmp-%d" % (path, os.getpid())
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(state, f, ensure_ascii=False, indent=2, sort_keys=True)
    os.replace(tmp, path)


# ---------------------------------------------------------------- 玩家模型

def new_player(pid: str, name: str) -> dict:
    now = int(time.time())
    return {
        # ---- 身份 ----
        "id": pid,
        "name": name,
        "created": now,

        # ---- 共用经济 ----
        "coins": START_COINS,

        # ---- 农场 ----
        "exp": 0,                 # 农场经验（决定可种作物等级）
        "fields": 1,
        "seeds": {},              # "作物等级" -> 数量
        "land": {},               # 土地序号 -> {level, planted, watered, stealers}
        "pets": [],               # 守卫等级列表

        # ---- 打劫 ----
        "strength": 15,
        "profession": "",
        "equipment": [],
        "crime": 0,
        "jail_turns": 0,
        "rob_total": 0,
        "rob_win": 0,
        "last_job": 0,
        "last_welfare": 0,

        # ---- 修仙 ----
        "root": "",               # 灵根
        "realm": 0,               # 境界下标（见 xiuxian_core.REALMS）
        "qi": 0,                  # 当前修为（突破后被扣掉）
        "lifespan": 80,           # 剩余寿元（年）
        "lifespan_max": 80,
        "sect": "",               # 宗门
        "sect_rank": 0,           # 宗门位阶下标
        "techniques": [],         # 已学功法
        "active_technique": "",   # 当前运功
        "pills": {},              # 丹药名 -> 数量
        "tribulation_fails": 0,   # 连续突破失败次数（影响下次成功率）
        "enlighten": 0,           # 悟性（影响奇遇与突破）
        "last_tuina": 0,          # 上次吐纳时间戳（v1 遗留，仅供迁移，吐纳已改灵池）
        "tuina_charges": 0,       # 灵池里攒下的吐纳次数（挂机累积，见 xiuxian_core.settle_tuina）
        "tuina_ts": 0,            # 上次结算灵池的时间戳；0 表示还没开始攒
        "last_retreat": 0,        # 上次闭关时间戳
        "last_adventure": 0,      # 上次奇遇时间戳
        "last_fight": 0,          # 上次战斗时间戳（没有冷却就能无限刷，必须限）
        "alive": True,
        "daoxiang": [],           # 道心/因果记录（剧情用）

        # ---- 杂项 ----
        "aliases": [],
    }


def migrate_player(p: dict) -> dict:
    """把 v1 存档补齐到当前字段集；幂等，可反复调用。"""
    template = new_player(p.get("id", ""), p.get("name", ""))
    for key, default in template.items():
        if key not in p:
            p[key] = default if not isinstance(default, (list, dict)) else type(default)()
    # 修仙字段里列表/字典要单独保证类型正确（旧档可能是 null）
    for key in ("seeds", "land", "pills"):
        if not isinstance(p.get(key), dict):
            p[key] = {}
    for key in ("pets", "equipment", "techniques", "daoxiang", "aliases"):
        if not isinstance(p.get(key), list):
            p[key] = []
    p["realm"] = int(p.get("realm") or 0)
    p["qi"] = int(p.get("qi") or 0)
    p["lifespan_max"] = int(p.get("lifespan_max") or 80)
    # 寿元 0 是「坐化」的合法状态，绝不能被当成缺省值补回满血
    if p.get("lifespan") is None:
        p["lifespan"] = p["lifespan_max"]
    else:
        p["lifespan"] = int(p["lifespan"])
    p.setdefault("alive", True)
    # 吐纳从「冷却点一次」改成「灵池挂机累积」：老档没 tuina_ts 的，用上次吐纳时间起算，
    # 这样升级当天老玩家立刻有一池可收（符合直觉），不会被重置成 0。
    p["tuina_charges"] = max(0, int(p.get("tuina_charges") or 0))
    if not int(p.get("tuina_ts") or 0):
        p["tuina_ts"] = int(p.get("last_tuina") or 0) or int(time.time())
    else:
        p["tuina_ts"] = int(p["tuina_ts"])
    return p


# ---------------------------------------------------------------- 上下文

class Game:
    """一次动作的上下文：持锁 → 读档 → 提供玩家 → 渲染输出 → 写档。"""

    def __init__(self, world: str = DEFAULT_WORLD, player: str = "",
                 player_name: str = ""):
        self.world_id = (world or "").strip() or DEFAULT_WORLD
        self.player_id = (player or "").strip()
        self.player_name = (player_name or "").strip()
        self.state = None
        self.lines = []
        self.images = []
        self.draws = []
        self._fh = None

    # -- with 协议 --

    def __enter__(self):
        path = data_path()
        os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
        # 锁文件独立于数据文件：数据文件是原子替换的，锁挂在它身上会随 inode 丢失
        self._fh = open(path + ".lock", "a+")
        _lock(self._fh)
        try:
            self.state = load_state()
        except Exception:
            _unlock(self._fh)
            self._fh.close()
            raise
        return self

    def __exit__(self, exc_type, exc, tb):
        try:
            if exc_type is None and self.state is not None:
                save_state(self.state)
        finally:
            if self._fh is not None:
                try:
                    _unlock(self._fh)
                finally:
                    self._fh.close()
        return False

    # -- 世界 / 玩家 --

    @property
    def world(self) -> dict:
        w = self.state["worlds"].setdefault(self.world_id, {"players": {}})
        w.setdefault("players", {})
        return w

    def player(self) -> dict:
        if not self.player_id:
            raise SystemExit("缺少 --player（需要说话人 wxid）")
        players = self.world["players"]
        p = players.get(self.player_id)
        if p is None:
            p = new_player(self.player_id, self.player_name or self.player_id)
            players[self.player_id] = p
        if self.player_name and p.get("name") != self.player_name:
            p["name"] = self.player_name
        return migrate_player(p)

    def find(self, target: str):
        """按 wxid / 名字找人；返回 (player, 候选列表)。歧义时 player 为 None。"""
        t = (target or "").strip().lstrip("@")
        if not t:
            return None, []
        players = self.world["players"]
        for p in players.values():
            migrate_player(p)
        if t in players:
            return players[t], []
        hits = [p for p in players.values()
                if p.get("name") == t or t in (p.get("aliases") or [])]
        if not hits:
            low = t.lower()
            hits = [p for p in players.values()
                    if str(p.get("name", "")).lower() == low]
        if len(hits) == 1:
            return hits[0], []
        return None, hits

    def others(self, exclude_id: str) -> list:
        return [p for p in self.world["players"].values()
                if p.get("id") != exclude_id and p.get("alive", True)]

    # -- 输出 --

    def say(self, text: str = "") -> None:
        self.lines.append(text)

    def image(self, *parts: str) -> None:
        path = asset(*parts)
        if os.path.exists(path):
            self.images.append(path)

    def draw(self, prompt: str) -> None:
        """请求一张「现画」的插画（由 Hermes 的 image_generate 生成）。

        引擎只负责决定「该不该画」和「画什么」，真正出图是模型侧的事：
        脚本没法调工具，所以这里输出 DRAW: 标记，由 SKILL.md 教模型接着去调。
        冷却已经在 caller 里判过，这里不再管频率。
        """
        prompt = " ".join((prompt or "").split())
        if prompt:
            self.draws.append(prompt)

    def render(self) -> str:
        body = "\n".join(self.lines).rstrip()
        for path in self.images:
            body += "\nIMAGE:" + path
        for prompt in self.draws:
            body += "\nDRAW:" + prompt
        return body


# ---------------------------------------------------------------- 工具

def now_ts() -> int:
    return int(time.time())


def fmt_duration(seconds: int) -> str:
    seconds = max(0, int(seconds))
    if seconds >= 3600:
        return "%d小时%d分" % (seconds // 3600, (seconds % 3600) // 60)
    if seconds >= 60:
        return "%d分钟" % (seconds // 60)
    return "%d秒" % seconds


def ambiguous(g: Game, raw: str, cands) -> None:
    if cands:
        g.say("找到多个「%s」，说全名或 wxid：" % raw.strip().lstrip("@"))
        for c in cands[:10]:
            g.say("　%s" % c.get("name", c.get("id")))
    else:
        g.say("这个场地里没有找到「%s」，他还没在这玩过。" % raw.strip().lstrip("@"))


def setup_stdio() -> None:
    """Windows 控制台默认 GBK，中文错误信息会乱码；统一 UTF-8。"""
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            try:
                stream.reconfigure(encoding="utf-8", errors="replace")
            except (AttributeError, ValueError):
                pass
