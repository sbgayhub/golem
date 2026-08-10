#!/usr/bin/env python3
# hermes_ops — Hermes gateway 只读运维小服务（跑在 Ubuntu VM）
#
# 设计：
#   - 只读：健康 / systemd 状态 / 工具注册自检 / sessions 列表 / 日志尾
#   - 禁止任意 shell；命令与路径白名单
#   - 默认绑 0.0.0.0:8650（供 Windows 宿主机桥反代）；生产可改 127.0.0.1 + SSH 隧道
#   - Bearer 与桥 hermes_ops_token 对齐（环境变量 HERMES_OPS_TOKEN）
#
# 依赖：标准库 only
#
# 启动示例：
#   export HERMES_HOME=$HOME/.hermes/profiles/wechat
#   export HERMES_OPS_TOKEN='长随机串'
#   export HERMES_OPS_LISTEN=0.0.0.0:8650
#   python3 hermes_ops.py
#
# systemd user 单元示例见同目录 hermes-ops.service.example

from __future__ import annotations

import json
import os
import re
import sqlite3
import subprocess
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlparse

LISTEN = os.environ.get("HERMES_OPS_LISTEN", "0.0.0.0:8650")
TOKEN = (os.environ.get("HERMES_OPS_TOKEN") or "").strip()
HERMES_HOME = Path(
    os.environ.get("HERMES_HOME")
    or os.path.expanduser("~/.hermes/profiles/wechat")
).resolve()
UNIT = os.environ.get("HERMES_GATEWAY_UNIT", "hermes-gateway-wechat.service")
LOG_DIR = HERMES_HOME / "logs"
# gateway 状态库常见路径（Hermes 版本可能变；找不到则 sessions 返回 note）
STATE_CANDIDATES = [
    HERMES_HOME / "state.db",
    HERMES_HOME / "data" / "state.db",
    HERMES_HOME / "gateway" / "state.db",
]

LOG_FILES = {
    "agent": LOG_DIR / "agent.log",
    "gateway": LOG_DIR / "gateway.log",
    "errors": LOG_DIR / "errors.log",
}

STICKER_DIR = Path(
    os.path.expanduser(
        os.environ.get("WECHAT_GOLEM_STICKER_DIR")
        or str(Path.home() / ".hermes" / "wechat_stickers")
    )
).resolve()
MEMBER_DIR = Path(
    os.path.expanduser(
        os.environ.get("WECHAT_GOLEM_MEMBER_PROFILE_DIR")
        or str(HERMES_HOME / "wechat_member_profiles")
    )
).resolve()

# 工具自检关键词（与 t-doc 踩坑一致）
TOOL_OK_PAT = re.compile(
    r"tool registered:\s*(wechat_\w+)|verify=ok=True", re.I
)
TOOL_CRASH_PAT = re.compile(
    r"registration crashed|query tools registration crashed", re.I
)


def _run(argv: list[str], timeout: float = 8.0) -> tuple[int, str]:
    try:
        p = subprocess.run(
            argv,
            capture_output=True,
            text=True,
            timeout=timeout,
            check=False,
        )
        out = (p.stdout or "") + (("\n" + p.stderr) if p.stderr else "")
        return p.returncode, out.strip()
    except Exception as e:
        return 1, f"{type(e).__name__}: {e}"


def systemd_status() -> dict:
    code, out = _run(
        ["systemctl", "--user", "is-active", UNIT], timeout=5
    )
    active = out.strip() if code == 0 else out.strip() or "unknown"
    code2, show = _run(
        [
            "systemctl",
            "--user",
            "show",
            UNIT,
            "-p",
            "ActiveState",
            "-p",
            "SubState",
            "-p",
            "MainPID",
            "-p",
            "NRestarts",
            "-p",
            "ActiveEnterTimestamp",
        ],
        timeout=5,
    )
    props = {}
    for line in (show or "").splitlines():
        if "=" in line:
            k, v = line.split("=", 1)
            props[k.strip()] = v.strip()
    return {
        "unit": UNIT,
        "is_active": active,
        "ok": active == "active",
        "props": props,
        "show_error": None if code2 == 0 else show,
    }


def tail_file(path: Path, n: int = 80, grep: str | None = None) -> dict:
    if not path.is_file():
        return {"path": str(path), "exists": False, "lines": []}
    n = max(1, min(n, 500))
    # 二进制安全：按字节读尾再按行拆（日志可能非严格 UTF-8）
    try:
        data = path.read_bytes()
    except OSError as e:
        return {"path": str(path), "exists": True, "error": str(e), "lines": []}
    # 最多扫最后 2MB
    if len(data) > 2 << 20:
        data = data[-(2 << 20) :]
    text = data.decode("utf-8", errors="replace")
    lines = text.splitlines()
    if grep:
        g = grep.lower()
        lines = [ln for ln in lines if g in ln.lower()]
    lines = lines[-n:]
    return {
        "path": str(path),
        "exists": True,
        "lines": lines,
        "grep": grep or "",
        "returned": len(lines),
    }


def tools_check() -> dict:
    agent = LOG_FILES["agent"]
    errors = LOG_FILES["errors"]
    # 扫 agent + errors 尾部
    registered: list[str] = []
    crashes: list[str] = []
    for path in (agent, errors):
        info = tail_file(path, n=400, grep=None)
        for ln in info.get("lines") or []:
            if TOOL_CRASH_PAT.search(ln):
                crashes.append(ln[-300:])
            m = TOOL_OK_PAT.search(ln)
            if m:
                registered.append(ln[-300:])
    # 去重保序
    def uniq(xs: list[str]) -> list[str]:
        seen = set()
        out = []
        for x in xs:
            if x in seen:
                continue
            seen.add(x)
            out.append(x)
        return out

    registered = uniq(registered)[-30:]
    crashes = uniq(crashes)[-20:]
    ok = len(registered) > 0 and len(crashes) == 0
    return {
        "ok": ok,
        "registered_samples": registered,
        "crash_samples": crashes,
        "hint": (
            "期望 agent.log 有 tool registered: wechat_… verify=ok=True；"
            "若只有 bootstrap 无 registered，去 errors.log/agent.log 找 registration crashed"
            if not ok
            else "工具注册看起来正常（基于日志尾扫描，非实时 API）"
        ),
        "log_dir": str(LOG_DIR),
    }


def find_state_db() -> Path | None:
    for c in STATE_CANDIDATES:
        if c.is_file():
            return c
    return None


def list_sessions(limit: int = 40) -> dict:
    limit = max(1, min(limit, 200))
    db = find_state_db()
    if db is None:
        # 尝试 hermes CLI
        code, out = _run(
            ["hermes", "-p", "wechat", "sessions", "list"], timeout=15
        )
        return {
            "source": "cli" if code == 0 else "none",
            "db": None,
            "ok": code == 0,
            "raw": out[-8000:],
            "sessions": [],
            "note": "未找到 state.db；已尝试 hermes sessions list"
            if code == 0
            else "未找到 state.db 且 CLI 失败；请确认 HERMES_HOME",
        }
    try:
        conn = sqlite3.connect(f"file:{db}?mode=ro", uri=True, timeout=3)
        conn.row_factory = sqlite3.Row
        cur = conn.cursor()
        # 表结构随版本变化：尝试常见名
        tables = {
            r[0]
            for r in cur.execute(
                "SELECT name FROM sqlite_master WHERE type='table'"
            ).fetchall()
        }
        rows = []
        note = ""
        if "sessions" in tables:
            cols = {
                r[1]
                for r in cur.execute("PRAGMA table_info(sessions)").fetchall()
            }
            # 尽量选有用的列
            want = [
                c
                for c in (
                    "session_key",
                    "session_id",
                    "key",
                    "id",
                    "source",
                    "chat_id",
                    "updated_at",
                    "created_at",
                    "last_prompt_tokens",
                    "title",
                    "status",
                )
                if c in cols
            ]
            if not want:
                want = list(cols)[:8]
            order = "updated_at" if "updated_at" in cols else want[0]
            sql = f"SELECT {', '.join(want)} FROM sessions ORDER BY {order} DESC LIMIT ?"
            try:
                for r in cur.execute(sql, (limit,)):
                    rows.append({k: r[k] for k in r.keys()})
            except sqlite3.Error as e:
                note = f"sessions 查询失败: {e}"
        else:
            note = f"库内无 sessions 表；tables={sorted(tables)[:20]}"
        conn.close()
        return {
            "source": "state.db",
            "db": str(db),
            "ok": True,
            "sessions": rows,
            "note": note or "只读打开 state.db；活会话以 gateway 内存为准，列表可能含历史堆叠",
        }
    except Exception as e:
        return {
            "source": "state.db",
            "db": str(db),
            "ok": False,
            "sessions": [],
            "error": str(e),
        }



def _safe_wxid_segment(s: str) -> str:
    s = (s or "").strip()
    if not s or ".." in s or "/" in s or "\\" in s:
        return ""
    if not re.fullmatch(r"[A-Za-z0-9_.@\-]+", s):
        return ""
    return s


def _safe_md5(s: str) -> str:
    s = (s or "").strip().lower()
    if re.fullmatch(r"[0-9a-f]{32}", s):
        return s
    return ""


def sticker_load_index() -> dict:
    path = STICKER_DIR / "index.json"
    if not path.is_file():
        return {}
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
        return data if isinstance(data, dict) else {}
    except Exception:
        return {}


def sticker_entry_brief(md5: str, e: dict) -> dict:
    if not isinstance(e, dict):
        e = {}
    moods = e.get("moods") if isinstance(e.get("moods"), list) else []
    tags = e.get("tags") if isinstance(e.get("tags"), list) else []
    # 文件大小：从 index.json 的 size 字段读；缺失则按文件名 stat 一次（缓存到 _STICKER_SIZE_CACHE）
    file_size = 0
    fname = (e.get("file") or "").strip()
    if fname and "/" not in fname and "\\" not in fname and ".." not in fname:
        cache = _STICKER_SIZE_CACHE.get(fname)
        if cache is None:
            try:
                cache = (STICKER_DIR / fname).stat().st_size
            except OSError:
                cache = 0
            _STICKER_SIZE_CACHE[fname] = cache
        file_size = cache
    return {
        "md5": md5,
        "moods": moods,
        "tags": tags,
        "desc": e.get("desc") or e.get("description") or "",
        "note": e.get("note") or "",
        "file": e.get("file") or "",
        "file_size": file_size,
        "use_count": e.get("use_count") or 0,
        "seen_count": e.get("seen_count") or 0,
        "last_used": e.get("last_used") or "",
        "added_at": e.get("added_at") or "",
        "source": e.get("source") or "",
    }


_STICKER_SIZE_CACHE: dict[str, int] = {}


def list_stickers(*, n: int = 20, page: int = 1, mood: str = "", tag: str = "", q: str = "", no_mood: bool = False) -> dict:
    """
    返回分页结果。
    - 支持 page/n 分页，默认每页 20 条；
    - 可通过 mood/tag/q 过滤；no_mood=True 只返回没有任何 mood 的条目（用来补漏）；
    - mood 精确匹配（项目里情绪表固定，避免「开心」误中「超开心」）；
    - tag 子串匹配（「猫」应能命中「猫咪/猫狗/黑猫」等同根词，比 exact 更友好）；
    - 无 mood/tag 也可查看（默认第 1 页显示前 20 条，可翻页查看全部）。
    """
    n = max(1, min(n, 200))
    page = max(1, page)
    idx = sticker_load_index()
    mood_l = mood.strip().lower()
    tag_l = tag.strip().lower()
    q_l = q.strip().lower()
    items = []
    for md5, e in idx.items():
        md5_s = str(md5).lower()
        if not re.fullmatch(r"[0-9a-f]{32}", md5_s):
            continue
        brief = sticker_entry_brief(md5_s, e if isinstance(e, dict) else {})
        moods = [str(x).lower() for x in (brief.get("moods") or []) if str(x).strip()]
        tags = [str(x).lower() for x in (brief.get("tags") or []) if str(x).strip()]
        if no_mood:
            if moods:
                continue
        elif mood_l:
            # 情绪表固定，精确匹配即可；模糊匹配会让用户点「开心」看到「超开心」错位
            if not any(m == mood_l for m in moods):
                continue
        if tag_l:
            # tag 子串匹配：「猫」能命中「猫咪/猫狗/招财猫」等，跟用户的认知对齐
            if not any(tag_l in t for t in tags):
                continue
        if q_l:
            blob = " ".join(
                [
                    brief.get("desc") or "",
                    brief.get("note") or "",
                    " ".join(moods),
                    " ".join(tags),
                    brief.get("md5") or "",
                ]
            ).lower()
            if q_l not in blob:
                continue
        items.append(brief)

    def sort_key(it: dict):
        return (it.get("use_count") or 0, str(it.get("last_used") or ""), it.get("md5") or "")

    items.sort(key=sort_key, reverse=True)
    total = len(items)
    start = (page - 1) * n
    end = start + n
    page_items = items[start:end]
    has_more = end < total
    return {
        "ok": True,
        "dir": str(STICKER_DIR),
        "index_exists": (STICKER_DIR / "index.json").is_file(),
        "total_matched": total,
        "page": page,
        "page_size": n,
        "has_more": has_more,
        "stickers": page_items,
        "note": "只读元数据，不含图片字节；库由适配器 wechat_sticker_* 维护",
    }



def sticker_facets() -> dict:
    """情绪/标签计数，供 UI 先选再加载，避免一次拉全库。
    计数规则必须和 list_stickers 完全一致：mood/tags 都用精确匹配（见 list_stickers 注释）。"""
    idx = sticker_load_index()
    mood_c: dict[str, int] = {}
    tag_c: dict[str, int] = {}
    total = 0
    no_mood = 0
    for md5, e in idx.items():
        md5_s = str(md5).lower()
        if not re.fullmatch(r"[0-9a-f]{32}", md5_s):
            continue
        total += 1
        brief = sticker_entry_brief(md5_s, e if isinstance(e, dict) else {})
        # 与 list_stickers 内部使用同一份清理逻辑：strip + 去空，保留原大小写
        # 计数也用 strip 后的小写做 key（与 list_stickers 的 .lower() 比对一致）
        moods_raw = [str(x).strip() for x in (brief.get("moods") or []) if str(x).strip()]
        tags_raw = [str(x).strip() for x in (brief.get("tags") or []) if str(x).strip()]
        if not moods_raw:
            no_mood += 1
        for m in moods_raw:
            mood_c[m.lower()] = mood_c.get(m.lower(), 0) + 1
        for tg in tags_raw:
            tag_c[tg.lower()] = tag_c.get(tg.lower(), 0) + 1
    moods_list = [{"name": k, "count": v} for k, v in mood_c.items()]
    moods_list.sort(key=lambda x: (-x["count"], x["name"]))
    tags_list = [{"name": k, "count": v} for k, v in tag_c.items()]
    tags_list.sort(key=lambda x: (-x["count"], x["name"]))
    # 题材太多时只回前 80，完整可再搜
    return {
        "ok": True,
        "dir": str(STICKER_DIR),
        "index_exists": (STICKER_DIR / "index.json").is_file(),
        "total": total,
        "no_mood": no_mood,
        "moods": moods_list,
        "tags": tags_list[:80],
        "tags_truncated": len(tags_list) > 80,
        "note": "先选 mood/tag 再 GET /stickers?mood=",
    }


def get_sticker(md5: str) -> dict:
    m = _safe_md5(md5)
    if not m:
        return {"ok": False, "error": "md5 须为 32 位 hex"}
    idx = sticker_load_index()
    e = idx.get(m)
    if not e:
        return {"ok": False, "error": "not found", "md5": m, "dir": str(STICKER_DIR)}
    brief = sticker_entry_brief(m, e if isinstance(e, dict) else {})
    f = brief.get("file") or ""
    fpath = STICKER_DIR / f if f else None
    brief["ok"] = True
    brief["file_exists"] = bool(fpath and fpath.is_file())
    brief["dir"] = str(STICKER_DIR)
    return brief



def sticker_file_path(md5: str):
    """返回 (path|None, err_dict|None)。仅允许 STICKER_DIR 内 index 登记的文件。"""
    m = _safe_md5(md5)
    if not m:
        return None, {"ok": False, "error": "md5 须为 32 位 hex"}
    idx = sticker_load_index()
    e = idx.get(m)
    if not isinstance(e, dict):
        return None, {"ok": False, "error": "not found", "md5": m}
    name = str(e.get("file") or "").strip()
    if not name or "/" in name or "\\" in name or ".." in name:
        return None, {"ok": False, "error": "index 文件名非法", "md5": m}
    path = (STICKER_DIR / name).resolve()
    try:
        path.relative_to(STICKER_DIR.resolve())
    except ValueError:
        return None, {"ok": False, "error": "path escape", "md5": m}
    if not path.is_file():
        return None, {"ok": False, "error": "file missing", "md5": m, "file": name}
    return path, None


def _guess_media_type(path: Path) -> str:
    ext = path.suffix.lower()
    return {
        ".gif": "image/gif",
        ".png": "image/png",
        ".jpg": "image/jpeg",
        ".jpeg": "image/jpeg",
        ".webp": "image/webp",
        ".bmp": "image/bmp",
    }.get(ext, "application/octet-stream")


def _member_empty(wxid: str) -> dict:
    return {
        "wxid": wxid,
        "display_name": "",
        "personality": "",
        "preferences": [],
        "notes": "",
        "aliases": [],
        "last_chat_id": "",
        "last_chat_name": "",
        "updated_at": "",
        "created_at": "",
    }


def _member_normalize(raw: dict, wxid: str) -> dict:
    base = _member_empty(wxid)
    if not isinstance(raw, dict):
        return base
    base["wxid"] = wxid or str(raw.get("wxid") or "")
    base["display_name"] = str(raw.get("display_name") or "")[:40]
    base["personality"] = str(raw.get("personality") or "")[:200]
    prefs = raw.get("preferences")
    if isinstance(prefs, list):
        base["preferences"] = [str(x)[:80] for x in prefs if str(x).strip()][:12]
    elif isinstance(prefs, str) and prefs.strip():
        base["preferences"] = [prefs.strip()[:80]]
    base["notes"] = str(raw.get("notes") or raw.get("note") or "")[:300]
    aliases = raw.get("aliases")
    if isinstance(aliases, list):
        base["aliases"] = [str(x)[:30] for x in aliases if str(x).strip()][:8]
    base["last_chat_id"] = str(raw.get("last_chat_id") or "")[:80]
    base["last_chat_name"] = str(raw.get("last_chat_name") or "")[:40]
    base["updated_at"] = str(raw.get("updated_at") or "")
    base["created_at"] = str(raw.get("created_at") or "")
    return base


def list_member_profiles(q: str = "") -> dict:
    q_l = (q or "").strip().lower()
    root = MEMBER_DIR
    if not root.is_dir():
        return {
            "ok": True,
            "dir": str(root),
            "profiles": [],
            "returned": 0,
            "note": "目录不存在（尚无档案或路径不对）",
        }
    items = []
    for path in sorted(root.glob("*.json")):
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
        except Exception:
            continue
        wxid = str((data or {}).get("wxid") or path.stem)
        prof = _member_normalize(data if isinstance(data, dict) else {}, wxid)
        if q_l:
            blob = " ".join(
                [
                    prof.get("wxid") or "",
                    prof.get("display_name") or "",
                    prof.get("personality") or "",
                    prof.get("notes") or "",
                    " ".join(prof.get("preferences") or []),
                    " ".join(prof.get("aliases") or []),
                ]
            ).lower()
            if q_l not in blob:
                continue
        items.append(
            {
                "wxid": prof.get("wxid"),
                "display_name": prof.get("display_name"),
                "personality": prof.get("personality"),
                "preferences": prof.get("preferences"),
                "notes": (prof.get("notes") or "")[:80],
                "updated_at": prof.get("updated_at"),
                "file": path.name,
            }
        )
    return {
        "ok": True,
        "dir": str(root),
        "returned": len(items),
        "profiles": items,
        "note": "跨 session 持久；新开会话不清。写入请短词。",
    }


def get_member_profile(wxid: str) -> dict:
    seg = _safe_wxid_segment(wxid)
    if not seg:
        return {"ok": False, "error": "wxid 非法"}
    path = MEMBER_DIR / f"{seg}.json"
    if path.is_file():
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
            prof = _member_normalize(data if isinstance(data, dict) else {}, seg)
            prof["ok"] = True
            prof["file"] = path.name
            prof["dir"] = str(MEMBER_DIR)
            return prof
        except Exception as e:
            return {"ok": False, "error": str(e), "wxid": seg}
    if MEMBER_DIR.is_dir():
        for pth in MEMBER_DIR.glob("*.json"):
            try:
                data = json.loads(pth.read_text(encoding="utf-8"))
            except Exception:
                continue
            if str((data or {}).get("wxid") or "") == wxid or pth.stem == seg:
                prof = _member_normalize(data if isinstance(data, dict) else {}, wxid)
                prof["ok"] = True
                prof["file"] = pth.name
                prof["dir"] = str(MEMBER_DIR)
                return prof
    return {"ok": False, "error": "not found", "wxid": wxid, "dir": str(MEMBER_DIR)}


def put_member_profile(wxid: str, body: dict) -> dict:
    seg = _safe_wxid_segment(wxid)
    if not seg:
        return {"ok": False, "error": "wxid 非法"}
    if not isinstance(body, dict):
        return {"ok": False, "error": "body 须为 JSON 对象"}
    MEMBER_DIR.mkdir(parents=True, exist_ok=True)
    path = MEMBER_DIR / f"{seg}.json"
    existing: dict = {}
    if path.is_file():
        try:
            existing = json.loads(path.read_text(encoding="utf-8"))
            if not isinstance(existing, dict):
                existing = {}
        except Exception:
            existing = {}
    merged = dict(existing)
    for k in (
        "display_name",
        "personality",
        "preferences",
        "notes",
        "aliases",
        "last_chat_id",
        "last_chat_name",
    ):
        if k in body:
            merged[k] = body[k]
    merged["wxid"] = str(existing.get("wxid") or body.get("wxid") or seg)
    now = time.strftime("%Y-%m-%dT%H:%M:%S")
    if not merged.get("created_at"):
        merged["created_at"] = existing.get("created_at") or now
    merged["updated_at"] = now
    clean = _member_normalize(merged, merged["wxid"])
    tmp = path.with_suffix(".json.tmp")
    tmp.write_text(json.dumps(clean, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    tmp.replace(path)
    clean["ok"] = True
    clean["file"] = path.name
    clean["dir"] = str(MEMBER_DIR)
    return clean


def delete_member_profile(wxid: str) -> dict:
    seg = _safe_wxid_segment(wxid)
    if not seg:
        return {"ok": False, "error": "wxid 非法"}
    path = MEMBER_DIR / f"{seg}.json"
    if path.is_file():
        path.unlink()
        return {"ok": True, "deleted": True, "wxid": seg}
    return {"ok": True, "deleted": False, "wxid": seg, "note": "文件本不存在"}



def overview() -> dict:
    st = systemd_status()
    tools = tools_check()
    alerts = []
    if not st.get("ok"):
        alerts.append(f"gateway 单元未 active：{st.get('is_active')}")
    if not tools.get("ok"):
        if tools.get("crash_samples"):
            alerts.append("工具注册日志出现 crash")
        elif not tools.get("registered_samples"):
            alerts.append("日志尾未见 tool registered")
    if not LOG_DIR.is_dir():
        alerts.append(f"日志目录不存在: {LOG_DIR}")
    return {
        "ok": len(alerts) == 0,
        "hermes_home": str(HERMES_HOME),
        "systemd": st,
        "tools_ok": tools.get("ok"),
        "tools_hint": tools.get("hint"),
        "alerts": alerts,
        "ts": int(time.time()),
    }


class Handler(BaseHTTPRequestHandler):
    server_version = "hermes_ops/0.5"

    def log_message(self, fmt: str, *args) -> None:
        # 简洁 stdout
        sys_stderr = __import__("sys").stderr
        print(f"[hermes_ops] {self.address_string()} {fmt % args}", file=sys_stderr)

    def _auth_ok(self) -> bool:
        if not TOKEN:
            # 未设 token：仅建议本机；仍放行但打警告一次可接受
            return True
        got = (self.headers.get("Authorization") or "").strip()
        if got.lower().startswith("bearer "):
            got = got[7:].strip()
        if not got:
            got = (self.headers.get("X-Ops-Token") or "").strip()
        if not got:
            qs = parse_qs(urlparse(self.path).query)
            got = (qs.get("token") or [""])[0].strip()
        return got == TOKEN

    def _json(self, code: int, obj: dict) -> None:
        raw = json.dumps(obj, ensure_ascii=False, default=str).encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self) -> None:  # noqa: N802
        if not self._auth_ok():
            self._json(401, {"error": "unauthorized"})
            return
        u = urlparse(self.path)
        path = u.path.rstrip("/") or "/"
        q = parse_qs(u.query)

        if path == "/health":
            self._json(
                200,
                {
                    "status": "ok",
                    "service": "hermes_ops",
                    "version": "0.5",
                    "hermes_home": str(HERMES_HOME),
                    "sticker_dir": str(STICKER_DIR),
                    "member_dir": str(MEMBER_DIR),
                    "ts": int(time.time()),
                },
            )
            return
        if path == "/overview":
            self._json(200, overview())
            return
        if path == "/tools/check":
            self._json(200, tools_check())
            return
        if path == "/sessions":
            limit = 40
            if q.get("n"):
                try:
                    limit = int(q["n"][0])
                except ValueError:
                    pass
            self._json(200, list_sessions(limit=limit))
            return
        if path == "/logs":
            which = (q.get("file") or ["agent"])[0].strip().lower()
            if which not in LOG_FILES:
                self._json(
                    400,
                    {"error": "file 须为 agent|gateway|errors", "got": which},
                )
                return
            n = 80
            if q.get("n"):
                try:
                    n = int(q["n"][0])
                except ValueError:
                    pass
            grep = (q.get("grep") or [""])[0] or None
            self._json(200, tail_file(LOG_FILES[which], n=n, grep=grep))
            return
        if path == "/stickers/facets":
            self._json(200, sticker_facets())
            return
        if path == "/stickers":
            n = 20
            page = 1
            if q.get("n"):
                try:
                    n = int(q["n"][0])
                except ValueError:
                    pass
            if q.get("page"):
                try:
                    page = int(q["page"][0])
                except ValueError:
                    pass
            self._json(
                200,
                list_stickers(
                    n=n,
                    page=page,
                    mood=(q.get("mood") or [""])[0],
                    tag=(q.get("tag") or [""])[0],
                    q=(q.get("q") or [""])[0],
                    no_mood=(q.get("no_mood") or [""])[0] in ("1", "true", "yes"),
                ),
            )
            return
        if path.startswith("/stickers/"):
            from urllib.parse import unquote

            rest = unquote(path[len("/stickers/") :]).strip("/")
            parts = [p for p in rest.split("/") if p]
            if len(parts) == 1:
                res = get_sticker(parts[0])
                self._json(200 if res.get("ok") else 404, res)
                return
            if len(parts) == 2 and parts[1] == "file":
                fpath, err = sticker_file_path(parts[0])
                if err:
                    self._json(404, err)
                    return
                # 单文件上限 16MB（动图/高清表情常见 3-10MB；阈值参考微信表情规格）
                # 大于阈值直接 413，前端 <img onerror> 会兜底成"加载失败"
                size_limit = 16 << 20
                try:
                    size = fpath.stat().st_size
                except OSError:
                    size = 0
                if size > size_limit:
                    self._json(
                        413,
                        {
                            "ok": False,
                            "error": f"file too large ({size} > {size_limit})",
                            "md5": parts[0],
                            "size": size,
                            "limit": size_limit,
                        },
                    )
                    return
                try:
                    self.send_response(200)
                    self.send_header("Content-Type", _guess_media_type(fpath))
                    self.send_header("Content-Length", str(size))
                    self.send_header("Cache-Control", "private, max-age=3600")
                    self.end_headers()
                    with fpath.open("rb") as fp:
                        while True:
                            chunk = fp.read(64 << 10)
                            if not chunk:
                                break
                            self.wfile.write(chunk)
                except (OSError, BrokenPipeError) as e:
                    # 客户端断连/磁盘错，没必要报错刷日志
                    return
                return
            self._json(404, {"error": "stickers 路径用法: /stickers/<md5> 或 /stickers/<md5>/file"})
            return
        if path == "/member_profiles":
            self._json(200, list_member_profiles(q=(q.get("q") or [""])[0]))
            return
        if path.startswith("/member_profiles/"):
            from urllib.parse import unquote

            wxid = unquote(path[len("/member_profiles/") :])
            res = get_member_profile(wxid)
            self._json(200 if res.get("ok") else 404, res)
            return

        self._json(
            404,
            {
                "error": "not found",
                "paths": [
                    "/health",
                    "/overview",
                    "/tools/check",
                    "/sessions",
                    "/logs?file=agent|gateway|errors&n=80&grep=",
                    "/stickers?n=100&mood=&tag=&q=",
                    "/stickers/<md5>",
                    "/stickers/<md5>/file",
                    "/member_profiles?q=",
                    "/member_profiles/<wxid>",
                    "PUT/DELETE /member_profiles/<wxid>",
                ],
            },
        )

    def _read_json_body(self):
        try:
            length = int(self.headers.get("Content-Length") or "0")
        except ValueError:
            length = 0
        if length <= 0:
            return {}, None
        if length > 1 << 20:
            return None, "body too large"
        raw = self.rfile.read(length)
        try:
            data = json.loads(raw.decode("utf-8"))
        except Exception as e:
            return None, f"invalid json: {e}"
        if not isinstance(data, dict):
            return None, "body 须为对象"
        return data, None

    def do_PUT(self) -> None:  # noqa: N802
        if not self._auth_ok():
            self._json(401, {"error": "unauthorized"})
            return
        u = urlparse(self.path)
        path = u.path.rstrip("/") or "/"
        if not path.startswith("/member_profiles/"):
            self._json(405, {"error": "仅 member_profiles/<wxid> 支持 PUT"})
            return
        from urllib.parse import unquote

        wxid = unquote(path[len("/member_profiles/") :])
        body, err = self._read_json_body()
        if err:
            self._json(400, {"error": err})
            return
        res = put_member_profile(wxid, body or {})
        self._json(200 if res.get("ok") else 400, res)

    def do_DELETE(self) -> None:  # noqa: N802
        if not self._auth_ok():
            self._json(401, {"error": "unauthorized"})
            return
        u = urlparse(self.path)
        path = u.path.rstrip("/") or "/"
        if not path.startswith("/member_profiles/"):
            self._json(405, {"error": "仅 member_profiles/<wxid> 支持 DELETE"})
            return
        from urllib.parse import unquote

        wxid = unquote(path[len("/member_profiles/") :])
        res = delete_member_profile(wxid)
        self._json(200 if res.get("ok") else 400, res)



def main() -> None:
    host, _, port_s = LISTEN.partition(":")
    port = int(port_s or "8650")
    if not TOKEN:
        print(
            "[hermes_ops] 警告: HERMES_OPS_TOKEN 为空，接口无鉴权",
            flush=True,
        )
    print(
        f"[hermes_ops] listen={host}:{port} HERMES_HOME={HERMES_HOME} unit={UNIT} stickers={STICKER_DIR} members={MEMBER_DIR}",
        flush=True,
    )
    httpd = ThreadingHTTPServer((host, port), Handler)
    try:
        httpd.serve_forever()
    except KeyboardInterrupt:
        print("\n[hermes_ops] bye", flush=True)


if __name__ == "__main__":
    main()
