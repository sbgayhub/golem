(() => {
  const TOKEN_KEY = "hermes_bridge_admin_token";
  const $ = (id) => document.getElementById(id);
  const toastEl = $("toast");
  let stickerPage = 1;
  let stickerPageSize = 20;
  let profileSelectedWXID = "";
  const PERSONA_MAX_BYTES = 64 * 1024;
  let personaSelectedID = "";
  let personaETag = "";
  let personaBaseline = "";
  let personaBaselineID = "";
  let personaCreating = false;
  let personaCapabilities = null;
  let personaListCache = [];
  let personaBindingsCache = [];
  let personaBindingCount = 0;

  function toast(msg, opts) {
    opts = typeof opts === "boolean" ? { err: opts } : opts || {};
    const dur = opts.dur || 3000;
    toastEl.textContent = msg;
    toastEl.className = "toast";
    if (opts.err) toastEl.classList.add("err");
    if (opts.warn) toastEl.classList.add("warn");
    toastEl.classList.remove("hidden");
    clearTimeout(toastEl._t);
    toastEl._t = setTimeout(() => toastEl.classList.add("hidden"), dur);
  }

  function toastOk(msg) {
    toast(msg, { dur: 2000 });
  }
  function toastWarn(msg) {
    toast(msg, { warn: true, dur: 3500 });
  }
  function toastErr(msg) {
    toast(msg, { err: true, dur: 4000 });
  }

  function askConfirm(msg, title) {
    return new Promise((resolve) => {
      const root = $("confirm");
      const ok = $("confirm-ok");
      const cancel = $("confirm-cancel");
      if (!root || !ok || !cancel) {
        resolve(window.confirm(msg));
        return;
      }
      $("confirm-title").textContent = title || "确认";
      $("confirm-msg").textContent = msg;
      root.hidden = false;
      root.classList.remove("hidden");
      const done = (v) => {
        root.classList.add("hidden");
        root.hidden = true;
        ok.onclick = null;
        cancel.onclick = null;
        resolve(v);
      };
      ok.onclick = () => done(true);
      cancel.onclick = () => done(false);
    });
  }

  // ---- loading helpers ----
  function showLoading(el, msg) {
    if (typeof el === "string") el = $(el);
    if (!el) return;
    el.innerHTML = `<div class="loading-dots">${esc(msg || "加载中…")}</div>`;
  }

  function showSkeleton(el, count, type) {
    if (typeof el === "string") el = $(el);
    if (!el) return;
    type = type || "item";
    const cls = type === "card" ? "skeleton-card" : "skeleton-item";
    el.innerHTML = Array.from(
      { length: count },
      () => `<div class="skeleton ${cls}"></div>`,
    ).join("");
  }

  function showEmpty(el, msg) {
    if (typeof el === "string") el = $(el);
    if (!el) return;
    el.innerHTML = `<div class="empty">${esc(msg || "暂无数据")}</div>`;
  }

  function showError(el, msg) {
    if (typeof el === "string") el = $(el);
    if (!el) return;
    el.innerHTML = `<div class="alerts"><ul><li>${esc(msg)}</li></ul></div>`;
  }

  // 列表加载失败的统一出口：401 回登录页，其余把错误写进列表容器。
  // 不能只 toast——骨架屏/加载占位会永远留在原地，看起来像在无限加载。
  function handleLoadError(el, e) {
    if (e && e.status === 401) {
      setToken("");
      showLogin("登录已失效");
      return;
    }
    showError(el, (e && e.message) || "加载失败");
    toastErr((e && e.message) || "加载失败");
  }

  function getToken() {
    return (localStorage.getItem(TOKEN_KEY) || "").trim();
  }

  function setToken(t) {
    t = (t || "").trim();
    if (t) localStorage.setItem(TOKEN_KEY, t);
    else localStorage.removeItem(TOKEN_KEY);
  }

  function esc(s) {
    return String(s == null ? "" : s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  function num(v) {
    const n = Number(v);
    return Number.isFinite(n) ? n : 0;
  }

  // 触发/丢弃原因的中文映射（门闩语义，入站 trace 与本地会话共用）
  const TRIGGER_CN = {
    trigger_name: "点名",
    bubble: "冒泡",
    emoji_burst: "斗图",
    session_reset: "新开会话",
    member_archive: "归档",
  };
  const REASON_CN = {
    sse: "已推",
    debounce: "去抖",
    interrupt: "打断",
    no_adapter: "无适配器",
    gate_not_triggered: "未触发门闩",
    revoke_shortcut: "撤回捷径",
    revoke_none: "无可撤消息",
    revoke_failed: "撤回失败",
  };

  function fmtDate(ts) {
    if (!ts) return "";
    // 档案的 updated_at 有两种形态：adapter 写 Unix 秒，ops 写 ISO 字符串
    if (typeof ts === "string" && !/^\d+$/.test(ts)) {
      const d = new Date(ts);
      return isNaN(d.getTime()) ? ts : d.toLocaleDateString();
    }
    return new Date(Number(ts) * 1000).toLocaleDateString();
  }

  function truncate(s, n) {
    s = String(s == null ? "" : s);
    return s.length > n ? s.slice(0, n) + "…" : s;
  }

  function cleanLabel(v) {
    return v == null || v === "" || v === "none" || v === "unknown" ? null : v;
  }

  async function apiResponse(path, opts = {}) {
    const headers = Object.assign({}, opts.headers || {});
    const token = getToken();
    if (token) {
      headers["Authorization"] = "Bearer " + token;
      headers["X-Admin-Token"] = token;
    }
    if (opts.body && !headers["Content-Type"])
      headers["Content-Type"] = "application/json";
    const res = await fetch(path, Object.assign({}, opts, { headers }));
    let data = null;
    const ct = res.headers.get("content-type") || "";
    if (ct.includes("application/json")) data = await res.json();
    else data = { error: await res.text() };
    if (!res.ok) {
      const e = new Error((data && data.error) || res.statusText || "请求失败");
      e.status = res.status;
      e.data = data;
      throw e;
    }
    return { data, headers: res.headers, status: res.status };
  }

  async function api(path, opts = {}) {
    return (await apiResponse(path, opts)).data;
  }

  // ---- auth gate ----
  // 登录页副标题写实际监听地址：写死「仅 127.0.0.1」在改了 admin_listen
  // （Tailscale 等场景）之后就是假话。/admin/meta 无鉴权，登录前即可取。
  async function fillLoginListen() {
    const el = $("login-listen");
    if (!el) return;
    try {
      const res = await fetch("/admin/meta");
      if (!res.ok) return;
      const meta = await res.json();
      const addr = (meta && meta.admin_listen) || "";
      if (!addr) return;
      const local = /^(127\.0\.0\.1|localhost|\[::1\]):/.test(addr);
      el.textContent = local ? "本机管理台 · " + addr : "管理台 · " + addr;
    } catch (_) {
      // 取不到就保留 HTML 里的中性文案
    }
  }

  function showLogin(errMsg) {
    stopTraceLive();
    $("app-shell").classList.add("hidden");
    $("login-screen").classList.remove("hidden");
    $("login-token").value = "";
    fillLoginListen();
    const err = $("login-error");
    if (errMsg) {
      err.textContent = errMsg;
      err.classList.remove("hidden");
    } else {
      err.classList.add("hidden");
    }
    setTimeout(() => $("login-token").focus(), 50);
  }

  function showApp() {
    $("login-screen").classList.add("hidden");
    $("app-shell").classList.remove("hidden");
  }

  async function tryEnter() {
    const tok = getToken();
    if (!tok) {
      showLogin();
      return;
    }
    try {
      const o = await api("/admin/overview");
      showApp();
      $("ver").textContent = "v" + (o.version || "");
      applyOverview(o);
      // 默认总览已 active
    } catch (e) {
      setToken("");
      showLogin(
        e.status === 401 ? "token 无效" : e.message || "无法连接管理台",
      );
    }
  }

  $("login-form").addEventListener("submit", async (ev) => {
    ev.preventDefault();
    const t = $("login-token").value.trim();
    if (!t) return;
    setToken(t);
    $("btnLogin").disabled = true;
    try {
      await api("/admin/overview");
      showApp();
      toastOk("已登录");
      loadOverview();
      enterHashPage();
    } catch (e) {
      setToken("");
      showLogin(e.status === 401 ? "admin_token 不正确" : e.message);
    } finally {
      $("btnLogin").disabled = false;
    }
  });

  $("btnLogout").addEventListener("click", () => {
    setToken("");
    showLogin();
    toastOk("已退出");
  });

  // ---- navigation ----
  function refreshBtn(fn) {
    const b = document.createElement("button");
    b.className = "primary";
    b.textContent = "刷新";
    b.title = "重新拉取本页数据";
    b.onclick = async () => {
      // pending 态防连点：请求在途时按钮禁用
      b.disabled = true;
      b.textContent = "刷新中…";
      try {
        await fn();
      } finally {
        b.disabled = false;
        b.textContent = "刷新";
      }
    };
    return b;
  }

  // 监控类页面（总览/会话）的 30s 自动刷新：开关状态记在 localStorage，
  // 换页时 setPage 先停旧 interval，进页时按存储值恢复。
  const AUTO_REFRESH_KEY = "hermes_admin_auto_refresh";
  let autoTimer = 0;

  function stopAutoRefresh() {
    if (autoTimer) {
      clearInterval(autoTimer);
      autoTimer = 0;
    }
  }

  function autoRefreshBtn(fn) {
    const b = document.createElement("button");
    b.type = "button";
    const paint = () => {
      b.className = autoTimer ? "primary" : "ghost";
      b.textContent = autoTimer ? "自动刷新中" : "自动刷新 30s";
    };
    b.title = "每 30 秒自动刷新本页数据（静默拉取，不闪骨架屏）";
    b.onclick = () => {
      if (autoTimer) stopAutoRefresh();
      else autoTimer = setInterval(fn, 30000);
      localStorage.setItem(AUTO_REFRESH_KEY, autoTimer ? "1" : "");
      paint();
    };
    if (localStorage.getItem(AUTO_REFRESH_KEY) === "1") {
      autoTimer = setInterval(fn, 30000);
    }
    paint();
    return b;
  }

  const pageActions = {
    overview: () => [
      refreshBtn(() => loadOverview()),
      autoRefreshBtn(() => loadOverview(true)),
    ],
    targets: () => [refreshBtn(refreshTargetsBoth)],
    gate: () => [refreshBtn(loadGate)],
    sessions: () => [
      refreshBtn(() => loadSessions()),
      autoRefreshBtn(() => loadSessions(true)),
    ],
    hermes: () => [refreshBtn(loadHermes)],
    profiles: () => [refreshBtn(loadProfiles)],
    personas: () => [refreshBtn(loadPersonas)],
    diagnose: () => [refreshBtn(loadDiagChats)],
  };

  async function setPage(tab) {
    if (tab !== "personas" && personaIsDirty()) {
      if (!(await askConfirm("当前人格草稿尚未保存，仍要离开吗？", "放弃未保存内容")))
        return;
      personaSetBaseline($("persona-content").value, personaSelectedID);
    }
    if (tab !== "gate" && gateIsDirty()) {
      if (!(await askConfirm("门闩设置有未保存改动，仍要离开吗？", "放弃未保存内容")))
        return;
      gateDropDraft();
    }
    if (tab !== "profiles" && profileIsDirty()) {
      if (!(await askConfirm("档案有未保存改动，仍要离开吗？", "放弃未保存内容")))
        return;
      profileDropDraft();
    }
    document.querySelectorAll("#nav button").forEach((b) => {
      const on = b.dataset.tab === tab;
      b.classList.toggle("active", on);
      if (on) b.setAttribute("aria-current", "page");
      else b.removeAttribute("aria-current");
    });
    document
      .querySelectorAll(".panel")
      .forEach((p) => p.classList.remove("active"));
    const panel = $("panel-" + tab);
    if (!panel) return;
    panel.classList.add("active");
    $("page-title").textContent = panel.dataset.title || tab;
    $("page-sub").textContent = panel.dataset.sub || "";
    // 写进 hash：F5 / 分享链接后停在同一页（replaceState 不产生历史记录）
    if (location.hash.slice(1) !== tab)
      history.replaceState(null, "", "#" + tab);
    const box = $("topbar-actions");
    box.innerHTML = "";
    stopAutoRefresh(); // 换页先停旧的自动刷新，新页的按钮按开关状态重建
    const makers = pageActions[tab];
    if (makers) makers().forEach((el) => box.appendChild(el));
    setNavOpen(false);

    if (tab !== "inbound") stopTraceLive();
    if (tab !== "hermes") stopLogTail();
    if (tab === "overview") loadOverview();
    if (tab === "targets") {
      loadTargets();
      renderRecipients($("search-q").value);
    }
    if (tab === "gate") {
      loadGate();
      loadGateTargets();
    }
    if (tab === "inbound") loadTraceRecent();
    if (tab === "sessions") loadSessions();
    if (tab === "hermes") loadHermes();
    if (tab === "stickers") {
      // 切到表情面板：清空旧详情，避免从其它页进来时还残留选中态
      clearStickerDetail();
      loadStickerFacets();
    }
    if (tab === "profiles") loadProfiles();
    if (tab === "personas") loadPersonas();
    if (tab === "diagnose") loadDiagChats();
  }

  document.querySelectorAll("#nav button").forEach((btn) => {
    btn.addEventListener("click", () => setPage(btn.dataset.tab));
  });

  // ---- Hermes sub-tabs ----
  function setHermesTab(htab) {
    document.querySelectorAll(".hermes-tab").forEach((b) => {
      const on = b.dataset.htab === htab;
      b.classList.toggle("active", on);
      b.setAttribute("aria-selected", on ? "true" : "false");
    });
    document.querySelectorAll(".hermes-pane").forEach((p) => p.classList.remove("active"));
    const pane = $("htab-" + htab);
    if (pane) pane.classList.add("active");
  }
  document.querySelectorAll(".hermes-tab").forEach((btn) => {
    btn.addEventListener("click", () => setHermesTab(btn.dataset.htab));
  });

  function card(label, value, cls) {
    return `<div class="card ${cls || ""}"><div class="label">${label}</div><div class="value">${value}</div></div>`;
  }

  function applyOverview(o) {
    $("ver").textContent = "v" + (o.version || "");
    const subsCls = o.subscribers > 0 ? "ok" : "bad";
    const fill = (id, html) => {
      const el = $(id);
      if (el) el.innerHTML = html;
    };
    fill(
      "overview-link",
      [
        card("SSE 连接数", o.subscribers, subsCls),
        card(
          "运维反代",
          o.hermes_ops_configured ? "已配置" : "未配置",
          o.hermes_ops_configured ? "ok" : "",
        ),
        card("业务端口", esc(o.listen) || "—"),
        card("管理端口", esc(o.admin_listen) || "—"),
      ].join(""),
    );
    fill(
      "overview-capacity",
      [
        card("白名单数量", o.targets),
        card("群门闩覆盖", o.gate_overrides || 0),
        card("活跃会话", o.local_sessions),
        card(
          "等待合并",
          o.pending_debounce,
          o.pending_debounce ? "ok" : "",
        ),
        card(
          "待推送",
          o.buffered_unflushed,
          o.buffered_unflushed ? "ok" : "",
        ),
        card("媒体缓存", o.media_refs),
        card("出站限流", `${o.send_rate_per_min} 条/分`),
      ].join(""),
    );
    fill(
      "overview-identity",
      [
        card(
          "主人身份",
          o.owner_ok ? esc(o.owner_name) || "已识别" : "未识别",
          o.owner_ok ? "ok" : "bad",
        ),
        card("机器人昵称", esc(o.self_name || o.self_id) || "—"),
        card(
          "Token 掩码",
          `<span class="mono">${esc(o.token_masked || "—")}</span>`,
        ),
      ].join(""),
    );
    $("gate-summary").textContent = o.gate_summary || "";
    const alerts = o.alerts || [];
    const alertEl = $("alerts");
    alertEl.classList.toggle("ok", alerts.length === 0);
    alertEl.innerHTML = alerts.length
      ? "<ul>" + alerts.map((a) => `<li>${esc(a)}</li>`).join("") + "</ul>"
      : "<ul><li>链路安静，没有红灯</li></ul>";
  }

  // silent：自动刷新走静默拉取，保留旧卡片，不闪骨架屏
  async function loadOverview(silent) {
    try {
      if (!silent) {
        showSkeleton("overview-link", 4, "card");
        showSkeleton("overview-capacity", 6, "card");
        showSkeleton("overview-identity", 3, "card");
      }
      applyOverview(await api("/admin/overview"));
    } catch (e) {
      if (e.status === 401) {
        setToken("");
        showLogin("登录已失效");
        return;
      }
      showError("overview-link", e.message);
      $("alerts").innerHTML = `<ul><li>${esc(e.message)}</li></ul>`;
      toastErr(e.message);
    }
  }

  // ---- targets ----
  // 群 / 联系人的缓存快照（data/chatroom.json + data/contact.json）。
  // 白名单页、门闩按群覆盖、诊断试发、表情试发都用它补显示名：
  // Target.Name 可能是空串或很久以前的群名，缓存里的更新。
  let recipientsCache = null;
  let recipientsPromise = null;

  function loadRecipients(force) {
    if (recipientsCache && !force) return Promise.resolve(recipientsCache);
    if (recipientsPromise) return recipientsPromise;
    recipientsPromise = api("/admin/data/recipients")
      .then((d) => {
        const groups = new Map();
        const people = new Map();
        if (d && d.ok) {
          for (const g of d.groups || []) groups.set(g.id, g);
          for (const c of d.contacts || []) people.set(c.username, c);
        }
        recipientsCache = { groups, people, files: (d && d.files) || {} };
        return recipientsCache;
      })
      .catch(() => {
        recipientsCache = { groups: new Map(), people: new Map(), files: {} };
        return recipientsCache;
      })
      .finally(() => {
        recipientsPromise = null;
      });
    return recipientsPromise;
  }

  function personDisplayName(c) {
    return c.remark || c.nickname || c.alias || c.username;
  }

  // recipientName 用缓存给会话补名字；缓存里没有就回退到白名单自带的 name。
  function recipientName(id, fallback) {
    if (!recipientsCache) return fallback || id;
    const g = recipientsCache.groups.get(id);
    if (g && g.name) return g.name;
    const p = recipientsCache.people.get(id);
    if (p) return personDisplayName(p);
    return fallback || id;
  }

  // <select> 的 option 只显示 label、不显示 value，所以 id 必须写进文本里。
  function selectLabel(t) {
    const id = t.id || "";
    const name = recipientName(id, t.name);
    if (!name || name === id) return id;
    return name + "（" + id + "）";
  }

  // <datalist> 的 option 会分两行显示：value（id）在上、label 在下。
  // 所以 label 只放名字，不要把 id 再重复一遍。
  function datalistLabel(t) {
    const id = t.id || "";
    const name = recipientName(id, t.name);
    return name && name !== id ? name : "";
  }

  // silent：联动刷新（移除/加入/顶栏刷新）不闪骨架屏，旧内容保留到新数据到达。
  // 左栏是独立滚动区，重绘会把 scrollTop 清零——先存后还，移除中间某条不跳顶。
  async function loadTargets(silent) {
    const pane = $("target-list").parentElement;
    const keepScroll = pane ? pane.scrollTop : 0;
    try {
      if (!silent) showSkeleton("target-list", 4);
      const [data] = await Promise.all([api("/admin/targets"), loadRecipients()]);
      const list = (data.targets || []).map((t) =>
        Object.assign({}, t, { showName: recipientName(t.id, t.name) }),
      );
      $("target-count").textContent = list.length + " 个";
      $("target-list").innerHTML = list.length
        ? list
            .map(
              (t) => `<div class="item" data-id="${esc(t.id)}">
            <span class="tag ${t.kind}">${t.kind === "group" ? "群" : "私聊"}</span>
            <div class="grow"><strong>${esc(t.showName)}</strong><div class="muted">${esc(t.id)}</div></div>
            <button type="button" class="ghost danger btn-del">移除</button>
          </div>`,
            )
            .join("")
        : `<div class="empty">白名单为空</div>`;
      $("target-list")
        .querySelectorAll(".btn-del")
        .forEach((btn) => {
          btn.addEventListener("click", async () => {
            const id = btn.closest(".item").dataset.id;
            if (!(await askConfirm("移出白名单？\n" + id, "移出白名单")))
              return;
            try {
              await api("/admin/targets/" + encodeURIComponent(id), {
                method: "DELETE",
              });
              toastOk("已移除");
              refreshTargetsBoth();
            } catch (e) {
              toastErr(e.message);
            }
          });
        });
      if (pane) pane.scrollTop = keepScroll;
    } catch (e) {
      handleLoadError("target-list", e);
    }
  }

  $("btnAddTarget").addEventListener("click", async () => {
    const id = $("target-id").value.trim();
    const name = $("target-name").value.trim();
    if (!id) {
      toastErr("请填写 id");
      return;
    }
    try {
      await api("/admin/targets", {
        method: "POST",
        body: JSON.stringify({ id, name }),
      });
      toastOk("已加入");
      $("target-id").value = "";
      $("target-name").value = "";
      refreshTargetsBoth();
    } catch (e) {
      toastErr(e.message);
    }
  });

  // 白名单页两栏联动刷新：左栏重绘已生效清单，右栏候选随之增减。
  // 移除成功 / 加入成功 / 顶栏「刷新」都走这里；silent 不闪骨架屏。
  function refreshTargetsBoth() {
    loadTargets(true);
    return renderRecipients($("search-q").value);
  }

  $("btnSearch").addEventListener("click", () => renderRecipients($("search-q").value));
  // 边打边过滤；去抖一下，避免每个字符都重绘整份列表并重复拉 /admin/targets。
  let recipientFilterTimer = 0;
  $("search-q").addEventListener("input", () => {
    clearTimeout(recipientFilterTimer);
    recipientFilterTimer = setTimeout(() => renderRecipients($("search-q").value), 180);
  });
  $("search-q").addEventListener("keydown", (ev) => {
    if (ev.key === "Enter") {
      clearTimeout(recipientFilterTimer);
      renderRecipients($("search-q").value);
    }
  });

  // 手填 id 的联想：把缓存里的群/好友全量挂到 datalist，不输入也能点开看全部。
  function fillTargetIDList() {
    const dl = $("target-id-list");
    if (!dl || !recipientsCache) return;
    const groups = Array.from(recipientsCache.groups.values());
    const people = Array.from(recipientsCache.people.values());
    dl.innerHTML =
      groups
        .map(
          (g) =>
            `<option value="${esc(g.id)}">群 · ${esc(g.name || g.id)}${g.in_chatroom_cache ? " · " + g.member_count + " 人" : ""}</option>`,
        )
        .join("") +
      people
        .map(
          (c) =>
            `<option value="${esc(c.username)}">好友 · ${esc(personDisplayName(c))}${c.alias ? " · " + esc(c.alias) : ""}</option>`,
        )
        .join("");
  }

  // 选中联想里的 id 后，顺手把显示名填上（用户自己填过就不覆盖）。
  $("target-id").addEventListener("change", () => {
    const id = $("target-id").value.trim();
    const nameEl = $("target-name");
    if (!id || !recipientsCache || nameEl.value.trim()) return;
    const nm = recipientName(id, "");
    if (nm && nm !== id) nameEl.value = nm;
  });

  // renderRecipients 是白名单页的「辅助列表」：留空列全部，
  // 关键词只做过滤，不要求用户记得关键词才能看到有什么可选。
  async function renderRecipients(rawQ) {
    const q = (rawQ || "").trim().toLowerCase();
    const box = $("search-results");
    // 只在首次载入时占位；边打边过滤时保留旧列表，避免闪烁。
    if (!recipientsCache) showLoading("search-results", "载入联系人…");
    // 右栏也是独立滚动区：加入一条重建后把 scrollTop 还回去，连续添加不跳顶
    const pane = box.parentElement;
    const keepScroll = pane ? pane.scrollTop : 0;
    try {
      // 全量缓存（群 ∪ 好友），而不是 contact.List()——后者只含已注入缓存的那部分。
      const [rc, data] = await Promise.all([
        loadRecipients(),
        api("/admin/targets"),
      ]);
      fillTargetIDList();
      const have = new Set((data.targets || []).map((t) => t.id));
      // 候选列表只列「还没加的」：已在白名单的直接滤掉，不再用灰色
      // 「已在白名单」按钮占位（白名单越长噪音越多）。被滤掉的数量在空态里说明。
      const hits = [];
      let already = 0;
      for (const g of rc.groups.values()) {
        if (have.has(g.id)) {
          already++;
          continue;
        }
        if (
          !q ||
          g.id.toLowerCase().includes(q) ||
          (g.name || "").toLowerCase().includes(q) ||
          (g.owner_name || "").toLowerCase().includes(q)
        ) {
          hits.push({
            id: g.id,
            name: g.name || g.id,
            kind: "group",
            sub: g.in_chatroom_cache
              ? g.member_count + " 人"
              : "不在群成员缓存",
          });
        }
      }
      for (const c of rc.people.values()) {
        if (have.has(c.username)) {
          already++;
          continue;
        }
        const name = personDisplayName(c);
        if (
          !q ||
          c.username.toLowerCase().includes(q) ||
          name.toLowerCase().includes(q) ||
          (c.alias || "").toLowerCase().includes(q)
        ) {
          hits.push({ id: c.username, name, kind: "private", sub: c.alias || "" });
        }
      }
      hits.sort((a, b) => (a.kind === b.kind ? 0 : a.kind === "group" ? -1 : 1));
      const shown = hits.slice(0, 80);
      const more = hits.length - shown.length;
      box.innerHTML = shown.length
        ? shown
            .map(
              (h) => `<div class="item">
            <span class="tag ${h.kind}">${h.kind === "group" ? "群" : "私聊"}</span>
            <div class="grow"><strong>${esc(h.name)}</strong><div class="muted">${esc(h.id)}${h.sub ? " · " + esc(h.sub) : ""}</div></div>
            <button type="button" class="primary btn-add-hit" data-id="${esc(h.id)}" data-name="${esc(h.name)}">加入</button>
          </div>`,
            )
            .join("") +
          (more > 0
            ? `<div class="empty">还有 ${more} 个，输入关键词缩小范围</div>`
            : "")
        : `<div class="empty">${
            q
              ? "无匹配"
              : already
                ? `没有可添加的（${already} 个已在白名单，未列出）`
                : "缓存里没有群/联系人"
          }</div>`;
      box.querySelectorAll(".btn-add-hit").forEach((btn) => {
        btn.addEventListener("click", async () => {
          try {
            await api("/admin/targets", {
              method: "POST",
              body: JSON.stringify({
                id: btn.dataset.id,
                name: btn.dataset.name,
              }),
            });
            toastOk("已加入");
            refreshTargetsBoth();
          } catch (e) {
            toastErr(e.message);
          }
        });
      });
      if (pane) pane.scrollTop = keepScroll;
    } catch (e) {
      handleLoadError("search-results", e);
    }
  }

  // ---- gate ----
  let selectedTargetID = "";
  let selectedTarget = null;
  let gateTargetList = [];

  // 门闩全局表单的未保存保护：与人格页同一套思路——
  // baseline 记录载入时的表单值，有差异就亮「未保存」并在离开前确认。
  const GATE_FIELDS = ["f-triggers", "f-pushall", "f-debounce", "f-bubble", "f-bubble-cd", "f-ctx", "f-burst", "f-burst-win", "f-burst-cd", "f-text", "f-rate"];
  let gateBaseline = null; // null = 尚未加载过，不参与脏检查

  function gateFormValues() {
    return GATE_FIELDS.map((id) => {
      const el = $(id);
      return el && el.type === "checkbox" ? el.checked : el ? el.value : "";
    }).join("|");
  }

  function gateIsDirty() {
    return gateBaseline !== null && gateFormValues() !== gateBaseline;
  }

  function gateSyncBaseline() {
    gateBaseline = gateFormValues();
    updateGateDirtyPill();
  }

  // 放弃草稿：把 baseline 对齐到当前表单。之后 loadGate 会重新拉值覆盖。
  function gateDropDraft() {
    gateBaseline = gateFormValues();
    updateGateDirtyPill();
  }

  function updateGateDirtyPill() {
    const el = $("gate-dirty");
    if (el) el.classList.toggle("hidden", !gateIsDirty());
  }

  function optNum(el) {
    const raw = (el.value || "").trim();
    if (!raw) return null;
    const n = Number(raw);
    return Number.isFinite(n) ? n : null;
  }

  function fillTargetGate(t) {
    const form = $("target-gate-form");
    const status = $("tg-status");
    const clearBtn = $("btnGateClear");
    if (!form || !status) return;
    if (!t || t.kind !== "group") {
      form.classList.add("hidden");
      selectedTargetID = "";
      selectedTarget = null;
      if (clearBtn) clearBtn.classList.add("hidden");
      status.textContent = t
        ? "私聊没有群门闩。"
        : "未选择群。空白字段沿用上面的全局。";
      return;
    }
    selectedTargetID = t.id;
    selectedTarget = t;
    form.classList.remove("hidden");
    if (clearBtn) clearBtn.classList.toggle("hidden", !t.has_override);
    status.textContent = t.has_override
      ? "本群有覆盖 · " + (t.name || t.id)
      : "本群沿用全局 · " + (t.name || t.id);
    $("tg-triggers").value = t.trigger_names ? t.trigger_names.join(", ") : "";
    $("tg-pushall-set").checked = t.group_push_all != null;
    $("tg-pushall").checked = !!t.group_push_all;
    $("tg-debounce").value = t.debounce_seconds != null ? t.debounce_seconds : "";
    $("tg-bubble").value = t.bubble_rate != null ? t.bubble_rate : "";
    $("tg-bubble-cd").value =
      t.bubble_cooldown_minutes != null ? t.bubble_cooldown_minutes : "";
    $("tg-ctx").value =
      t.max_context_messages != null ? t.max_context_messages : "";
    $("tg-burst").value = t.emoji_burst_count != null ? t.emoji_burst_count : "";
    $("tg-burst-win").value =
      t.emoji_burst_window_seconds != null ? t.emoji_burst_window_seconds : "";
    $("tg-burst-cd").value =
      t.emoji_burst_cooldown_minutes != null
        ? t.emoji_burst_cooldown_minutes
        : "";
  }

  async function loadGateTargets() {
    const sel = $("tg-chat");
    if (!sel) return;
    try {
      // 只列白名单里的群（门闩本来就是对白名单群做覆盖），名字优先用缓存。
      const [data] = await Promise.all([api("/admin/targets"), loadRecipients()]);
      gateTargetList = (data.targets || []).filter((t) => t.kind === "group");
      const keep = selectedTargetID;
      sel.innerHTML =
        `<option value="">选择群…</option>` +
        gateTargetList
          .map((t) => {
            const mark = t.has_override ? " · 已覆盖" : "";
            return `<option value="${esc(t.id)}">${esc(selectLabel(t))}${mark}</option>`;
          })
          .join("");
      if (keep && gateTargetList.some((t) => t.id === keep)) {
        sel.value = keep;
        fillTargetGate(gateTargetList.find((t) => t.id === keep));
      } else {
        sel.value = "";
        fillTargetGate(null);
      }
    } catch (e) {
      toastErr(e.message);
    }
  }

  async function saveTargetGate() {
    if (!selectedTargetID) return toastErr("先选一个群");
    const namesRaw = $("tg-triggers").value.trim();
    const body = {
      debounce_seconds: optNum($("tg-debounce")),
      bubble_rate: optNum($("tg-bubble")),
      bubble_cooldown_minutes: optNum($("tg-bubble-cd")),
      max_context_messages: optNum($("tg-ctx")),
      emoji_burst_count: optNum($("tg-burst")),
      emoji_burst_window_seconds: optNum($("tg-burst-win")),
      emoji_burst_cooldown_minutes: optNum($("tg-burst-cd")),
      group_push_all: $("tg-pushall-set").checked
        ? $("tg-pushall").checked
        : null,
    };
    if (namesRaw) {
      body.trigger_names = namesRaw
        .split(/[,，]/)
        .map((s) => s.trim())
        .filter(Boolean);
    } else if (selectedTarget && selectedTarget.trigger_names != null) {
      body.trigger_names = [];
    }
    try {
      await api("/admin/targets/" + encodeURIComponent(selectedTargetID), {
        method: "PATCH",
        body: JSON.stringify(body),
      });
      toastOk("本群覆盖已保存");
      loadGate();
      loadGateTargets();
    } catch (e) {
      toastErr(e.message);
    }
  }

  if ($("tg-chat")) {
    $("tg-chat").addEventListener("change", () => {
      const id = $("tg-chat").value;
      fillTargetGate(gateTargetList.find((t) => t.id === id) || null);
    });
  }
  if ($("target-gate-form")) {
    $("target-gate-form").addEventListener("submit", (ev) => {
      ev.preventDefault();
      saveTargetGate();
    });
  }
  if ($("btnGateClear")) {
    $("btnGateClear").addEventListener("click", async () => {
      if (!selectedTargetID) return toastErr("先选一个群");
      if (!(await askConfirm("清除本群门闩覆盖，改回全局？", "清除覆盖")))
        return;
      try {
        await api("/admin/targets/" + encodeURIComponent(selectedTargetID), {
          method: "PATCH",
          body: JSON.stringify({
            trigger_names: null,
            bubble_rate: null,
            bubble_cooldown_minutes: null,
            debounce_seconds: null,
            max_context_messages: null,
            group_push_all: null,
            emoji_burst_count: null,
            emoji_burst_window_seconds: null,
            emoji_burst_cooldown_minutes: null,
          }),
        });
        toastOk("已清除覆盖");
        loadGate();
        loadGateTargets();
      } catch (e) {
        toastErr(e.message);
      }
    });
  }

  async function loadGate() {
    $("gate-preview").textContent = "加载门闩配置…";
    $("btnGateSave").disabled = true;
    try {
      const c = await api("/admin/config");
      $("gate-preview").textContent = c.gate_summary || "";
      $("f-triggers").value = (c.trigger_names || []).join(", ");
      $("f-pushall").checked = !!c.group_push_all;
      $("f-debounce").value = c.debounce_seconds;
      $("f-bubble").value = c.bubble_rate;
      $("f-bubble-cd").value = c.bubble_cooldown_minutes;
      $("f-ctx").value = c.max_context_messages;
      $("f-burst").value = c.emoji_burst_count;
      $("f-burst-win").value = c.emoji_burst_window_seconds;
      $("f-burst-cd").value = c.emoji_burst_cooldown_minutes;
      $("f-text").value = c.max_text_len;
      $("f-rate").value = c.send_rate_per_min;
      gateSyncBaseline();
    } catch (e) {
      $("gate-preview").textContent = "加载失败: " + e.message;
      toastErr(e.message);
    } finally {
      $("btnGateSave").disabled = false;
    }
  }

  $("gate-form").addEventListener("submit", async (ev) => {
    ev.preventDefault();
    // 数字字段留空 = 不修改（后端 PATCH 用 map 区分「未传」与零值，
    // 跳过就不会把清空的字段误存成 0）。trigger_names 例外：总是提交，空 = 清空点名词。
    const body = {
      trigger_names: $("f-triggers")
        .value.split(/[,，]/)
        .map((s) => s.trim())
        .filter(Boolean),
      group_push_all: $("f-pushall").checked,
    };
    const optFields = [
      ["debounce_seconds", "f-debounce"],
      ["bubble_rate", "f-bubble"],
      ["bubble_cooldown_minutes", "f-bubble-cd"],
      ["max_context_messages", "f-ctx"],
      ["emoji_burst_count", "f-burst"],
      ["emoji_burst_window_seconds", "f-burst-win"],
      ["emoji_burst_cooldown_minutes", "f-burst-cd"],
      ["max_text_len", "f-text"],
      ["send_rate_per_min", "f-rate"],
    ];
    for (const [key, id] of optFields) {
      const v = optNum($(id));
      if (v != null) body[key] = v;
    }
    try {
      const c = await api("/admin/config", {
        method: "PATCH",
        body: JSON.stringify(body),
      });
      $("gate-preview").textContent = c.gate_summary || "";
      gateSyncBaseline();
      toastOk("已保存");
    } catch (e) {
      toastErr(e.message);
    }
  });
  // 表单任意改动 → 刷新「未保存」标记（checkbox 走 change，其余走 input）
  $("gate-form").addEventListener("input", updateGateDirtyPill);
  $("gate-form").addEventListener("change", updateGateDirtyPill);

  // ---- inbound ----
  let es = null;
  const seenTrace = new Set();

  // 实时流可能挂一整晚：DOM 与去重集合都封顶 300 条 FIFO 淘汰，避免越滚越卡
  const TRACE_MAX = 300;

  // 图例变筛选器：选中若干类 = 只看这几类；全不选 = 全部显示
  const traceKindFilter = new Set();

  function kindOfItem(el) {
    // className 形如 "trace-item pushed"，第一段是基类
    return (el.className || "").split(/\s+/).find((c) => c && c !== "trace-item") || "";
  }

  function applyTraceFilterTo(el) {
    el.style.display =
      !traceKindFilter.size || traceKindFilter.has(kindOfItem(el)) ? "" : "none";
  }

  function applyTraceFilter() {
    const listEl = $("trace-list");
    if (!listEl) return;
    listEl.querySelectorAll(".trace-item").forEach(applyTraceFilterTo);
  }

  function trimTrace(listEl, prepend) {
    while (listEl.children.length > TRACE_MAX) {
      const victim = prepend ? listEl.lastElementChild : listEl.firstElementChild;
      if (!victim) break;
      if (victim.dataset && victim.dataset.tid) seenTrace.delete(victim.dataset.tid);
      victim.remove();
    }
  }

  function fmtTs(ts) {
    if (!ts) return "";
    const d = new Date(ts * 1000);
    const today = new Date();
    const sameDay = d.toDateString() === today.toDateString();
    const t = d.toLocaleTimeString();
    return sameDay ? t : d.getMonth() + 1 + "-" + d.getDate() + " " + t;
  }

  function renderTrace(ev, prepend) {
    // 统一存字符串：裁剪时从 dataset.tid 取回的是字符串，类型一致才能删得中
    if (!ev || ev.id == null || seenTrace.has(String(ev.id))) return;
    seenTrace.add(String(ev.id));
    // 首个事件进来时清掉空态占位
    const listEl = $("trace-list");
    const empty = listEl.querySelector(".empty");
    if (empty) empty.remove();
    const el = document.createElement("div");
    el.className = "trace-item " + (ev.kind || "");
    const tr = cleanLabel(ev.trigger_reason);
    const rs = cleanLabel(ev.reason);
    const bits = [
      ev.chat_name || ev.chat_id,
      ev.user_name,
      tr ? TRIGGER_CN[tr] || tr : null,
      rs ? REASON_CN[rs] || rs : null,
      ev.text ? truncate(ev.text, 80) : null,
    ]
      .filter(Boolean)
      .join(" · ");
    const kindTitle =
      {
        pushed: "已推 SSE",
        scheduled: "去抖中",
        context_only: "只记不推",
        dropped: "丢弃",
        cancelled: "打断作废",
        revoked: "撤回捷径（桥内处理，未推 SSE）",
      }[ev.kind] || ev.kind;
    el.innerHTML = `<div class="k" title="${esc(kindTitle)}">${esc(ev.kind || "?")}</div>
      <div><span class="muted">${esc(fmtTs(ev.ts))} #${ev.id}</span>
      ${ev.msg_count ? " ×" + ev.msg_count : ""}
      <div title="${esc(ev.text || "")}">${esc(bits)}</div></div>`;
    el.dataset.tid = String(ev.id);
    if (prepend) listEl.prepend(el);
    else listEl.appendChild(el);
    applyTraceFilterTo(el);
    trimTrace(listEl, prepend);
  }

  async function loadTraceRecent() {
    try {
      const data = await api("/admin/inbound/recent?n=80");
      const events = data.events || [];
      $("trace-list").innerHTML = events.length
        ? ""
        : `<div class="empty">暂无入站事件 — 点「实时」订阅旁路，或群消息经门闩后落在这里</div>`;
      seenTrace.clear();
      events.forEach((ev) => renderTrace(ev, false));
    } catch (e) {
      toastErr(e.message);
    }
  }

  function startTraceLive() {
    stopTraceLive();
    const token = getToken();
    const el = $("trace-status");
    if (!token) {
      el.textContent = "未登录";
      el.className = "pill err";
      return;
    }
    es = new EventSource(
      "/admin/inbound/stream?token=" + encodeURIComponent(token),
    );
    el.textContent = "连接中";
    el.className = "pill";
    es.addEventListener("trace", (msg) => {
      try {
        renderTrace(JSON.parse(msg.data), true);
      } catch (_) {}
    });
    es.onopen = () => {
      el.textContent = "实时";
      el.className = "pill live";
    };
    es.onerror = () => {
      // EventSource 断线后会自动重连：readyState CLOSED 才是真断，
      // 其余显示「重连中」，别让用户误以为要手动再点「实时」
      if (!es || es.readyState === 2) {
        el.textContent = "已断开";
        el.className = "pill err";
      } else {
        el.textContent = "重连中";
        el.className = "pill warn";
      }
    };
  }

  function stopTraceLive() {
    if (es) {
      es.close();
      es = null;
    }
    const el = $("trace-status");
    if (el) {
      el.textContent = "未连接";
      el.className = "pill";
    }
  }

  $("btnTraceLive").addEventListener("click", () =>
    loadTraceRecent().then(startTraceLive),
  );
  $("btnTraceStop").addEventListener("click", stopTraceLive);
  $("btnTraceRefresh").addEventListener("click", loadTraceRecent);
  $("btnTraceClear").addEventListener("click", () => {
    $("trace-list").innerHTML = "";
    seenTrace.clear();
  });
  // 图例 chip 点击 = 筛选出该类型（可叠加多选），再点一次取消
  document
    .querySelectorAll(".legend-always .legend-grid > div[data-kind]")
    .forEach((div) => {
      div.addEventListener("click", () => {
        const k = div.dataset.kind;
        if (traceKindFilter.has(k)) traceKindFilter.delete(k);
        else traceKindFilter.add(k);
        div.classList.toggle("on", traceKindFilter.has(k));
        applyTraceFilter();
      });
    });

  // ---- sessions ----
  // silent：自动刷新走静默拉取，保留旧列表，不闪骨架屏
  async function loadSessions(silent) {
    try {
      if (!silent) showSkeleton("session-list", 5);
      const data = await api("/admin/sessions");
      const list = data.sessions || [];
      $("session-list").innerHTML = list.length
        ? list
            .map((s) => {
              const flags = [];
              if (s.pending_debounce) flags.push("去抖中");
              if (s.unflushed_count) flags.push("未推 " + s.unflushed_count);
              if (s.bubble_cool_remain_sec)
                flags.push("冒泡 " + s.bubble_cool_remain_sec + "s");
              if (s.burst_cool_remain_sec)
                flags.push("斗图 " + s.burst_cool_remain_sec + "s");
              if (!s.in_whitelist) flags.push("非白名单");
              const trig = s.last_trigger
                ? " · " + (TRIGGER_CN[s.last_trigger] || esc(s.last_trigger))
                : "";
              const who = s.last_user_name ? " · " + esc(s.last_user_name) : "";
              return `<div class="item">
              <span class="tag ${s.chat_type === "group" ? "group" : "private"}">${s.chat_type === "group" ? "群" : "私聊"}</span>
              <div class="grow">
                <strong>${esc(s.chat_name || s.chat_id || s.session_key)}</strong>
                <div class="muted">${esc(s.chat_id || s.session_key)} · 上下文 ${s.context_count}${trig}${who}</div>
                ${flags.length ? `<div class="muted">${esc(flags.join(" · "))}</div>` : ""}
              </div></div>`;
            })
            .join("")
        : `<div class="empty">暂无本地会话</div>`;
    } catch (e) {
      handleLoadError("session-list", e);
    }
  }

  // ---- hermes ----
  function renderHermesTools(data) {
    const box = $("hermes-tools");
    const count = $("hermes-tools-count");
    const regs = data.registered_samples || [];
    const crashes = data.crash_samples || [];
    count.textContent = regs.length + " 注册 / " + crashes.length + " 崩溃";
    const parts = [];
    if (data.ok) {
      parts.push(
        '<div class="alerts ok"><ul><li>工具注册看起来正常（日志尾扫描）</li></ul></div>',
      );
    } else {
      parts.push(
        `<div class="alerts"><ul><li>${esc(data.hint || "工具注册异常")}</li></ul></div>`,
      );
    }
    if (crashes.length) {
      parts.push('<div class="section-head"><h3>崩溃样本</h3></div>');
      parts.push(
        crashes
          .map(
            (c) =>
              `<div class="item reg-item" title="${esc(c)}"><span class="tag bad">crash</span><div class="grow mono">${esc(c)}</div></div>`,
          )
          .join(""),
      );
    }
    if (regs.length) {
      parts.push('<div class="section-head"><h3>已注册（日志尾）</h3></div>');
      parts.push(
        regs
          .map(
            (r) =>
              `<div class="item reg-item" title="${esc(r)}"><span class="tag ok">ok</span><div class="grow mono">${esc(r)}</div></div>`,
          )
          .join(""),
      );
    }
    box.innerHTML = parts.join("") || '<div class="empty">无工具注册日志</div>';
  }

  function renderHermesSessions(data) {
    const box = $("hermes-sessions");
    const count = $("hermes-sessions-count");
    const list = data.sessions || [];
    count.textContent = list.length ? list.length + " 条" : "0";
    if (!list.length) {
      const note = data.note ? esc(data.note) : "无 Hermes gateway session";
      box.innerHTML =
        '<div class="empty">' +
        note +
        "</div>" +
        (data.raw ? `<pre class="code-block">${esc(data.raw)}</pre>` : "");
      return;
    }
    const KV = (k, v) => {
      if (v == null || v === "" || v === 0 || (Array.isArray(v) && !v.length))
        return "";
      const s = typeof v === "object" ? JSON.stringify(v) : String(v);
      if (s.length > 60) return "";
      return `<span class="kv"><b>${esc(k)}</b>${esc(s)}</span>`;
    };
    box.innerHTML = list
      .map((s) => {
        const bits = Object.entries(s || {})
          .map(([k, v]) => KV(k, v))
          .join("");
        return `<div class="item session-item"><div class="kv-wrap">${bits || "<span class='muted'>—</span>"}</div></div>`;
      })
      .join("");
  }

  async function loadHermes() {
    const stEl = $("hermes-ops-status");
    stEl.textContent = "…";
    stEl.className = "pill";
    showSkeleton("hermes-cards", 3, "card");
    $("hermes-tools").innerHTML = '<div class="empty">加载中…</div>';
    $("hermes-sessions").innerHTML = '<div class="empty">加载中…</div>';
    $("hermes-tools-count").textContent = "";
    $("hermes-sessions-count").textContent = "";
    try {
      const meta = await api("/admin/hermes/meta");
      if (!meta.configured) {
        stEl.textContent = "未配置";
        stEl.className = "pill err";
        $("hermes-alerts").innerHTML =
          "<ul><li>配置 hermes_ops_url；脚本放 <code>~/.hermes/ops/</code></li></ul>";
        $("hermes-cards").innerHTML = "";
        renderHermesTools({
          ok: false,
          hint: "未配置 hermes_ops_url",
          registered_samples: [],
          crash_samples: [],
        });
        renderHermesSessions({ sessions: [] });
        return;
      }
      let line = meta.ops_url || "ops";
      if (meta.ops_version) line += " · v" + meta.ops_version;
      stEl.textContent = line;
      stEl.className =
        "pill" +
        (meta.ops_reachable === false
          ? " err"
          : meta.ops_reachable
            ? " live"
            : "");
      const tips = [];
      if (meta.ops_error) tips.push(meta.ops_error);
      if (meta.ops_warn) tips.push(meta.ops_warn);
      const ov = await api("/admin/hermes/overview");
      (ov.alerts || []).forEach((a) => tips.push(a));
      $("hermes-alerts").innerHTML = tips.length
        ? "<ul>" + tips.map((a) => `<li>${esc(a)}</li>`).join("") + "</ul>"
        : "";
      const st = ov.systemd || {};
      $("hermes-cards").innerHTML = [
        card("网关状态", esc(st.is_active) || "?", st.ok ? "ok" : "bad"),
        card("工具注册", ov.tools_ok ? "正常" : "异常", ov.tools_ok ? "ok" : "bad"),
        card("运行目录", esc(ov.hermes_home) || "—"),
      ].join("");
      renderHermesTools(await api("/admin/hermes/tools/check"));
      renderHermesSessions(await api("/admin/hermes/sessions?n=30"));
    } catch (e) {
      stEl.textContent = "失败";
      stEl.className = "pill err";
      $("hermes-alerts").innerHTML = `<ul><li>${esc(e.message)}</li></ul>`;
      renderHermesTools({
        ok: false,
        hint: e.message,
        registered_samples: [],
        crash_samples: [],
      });
      renderHermesSessions({ sessions: [] });
      toastErr(e.message);
    }
  }

  // 15s 自动拉取当前日志文件（简易 tail）。离开运维页时由 setPage 关掉。
  let logTailTimer = 0;

  function stopLogTail() {
    if (logTailTimer) {
      clearInterval(logTailTimer);
      logTailTimer = 0;
      const cb = $("hermes-log-tail");
      if (cb) cb.checked = false;
    }
  }

  $("hermes-log-tail").addEventListener("change", (ev) => {
    if (ev.target.checked) {
      logTailTimer = setInterval(() => $("btnHermesLog").click(), 15000);
    } else {
      clearInterval(logTailTimer);
      logTailTimer = 0;
    }
  });

  $("btnHermesLog").addEventListener("click", async () => {
    const file = $("hermes-log-file").value;
    const grep = $("hermes-log-grep").value.trim();
    let path = "/admin/hermes/logs?file=" + encodeURIComponent(file) + "&n=200";
    if (grep) path += "&grep=" + encodeURIComponent(grep);
    const logEl = $("hermes-log");
    const infoEl = $("hermes-log-info");
    logEl.innerHTML =
      '<span class="log-line"><span class="log-ln">…</span>加载中…</span>';
    if (infoEl) infoEl.textContent = "";
    try {
      const data = await api(path);
      if (!data.exists) {
        logEl.textContent = "不存在: " + (data.path || file);
        return;
      }
      const lines = data.lines || [];
      const total = lines.length;
      if (infoEl)
        infoEl.textContent = (data.path || file) + " · " + total + " 行";
      if (!total) {
        logEl.innerHTML =
          '<span class="log-line"><span class="log-ln">0</span>（空）</span>';
        return;
      }
      // 构建带行号和高亮的 HTML
      const grepLower = grep ? grep.toLowerCase() : "";
      const frag = [];
      for (let i = 0; i < total; i++) {
        const raw = lines[i];
        const ln = i + 1;
        let content = esc(raw);
        // 高亮匹配词（大小写不敏感，行内全部命中处）
        if (grepLower && content) {
          const low = content.toLowerCase();
          let out = "";
          let pos = 0;
          for (;;) {
            const idx = low.indexOf(grepLower, pos);
            if (idx < 0) {
              out += content.slice(pos);
              break;
            }
            out +=
              content.slice(pos, idx) +
              '<span class="log-hl">' +
              content.slice(idx, idx + grep.length) +
              "</span>";
            pos = idx + grep.length;
          }
          content = out;
        }
        frag.push(
          '<span class="log-line"><span class="log-ln">' +
            ln +
            "</span>" +
            content +
            "</span>",
        );
      }
      logEl.innerHTML = frag.join("");
      // 自动滚底
      const autoScroll = $("hermes-log-auto");
      if (autoScroll && autoScroll.checked) {
        requestAnimationFrame(() => {
          logEl.scrollTop = logEl.scrollHeight;
        });
      }
    } catch (e) {
      logEl.textContent = e.message;
      toastErr(e.message);
    }
  });

  // ---- stickers: facets first ----
  let selectedMood = null; // null = 未选，不自动加载
  let selectedTag = "";
  let selectedStickerMd5 = "";

  function stickerFileURL(md5) {
    const tok = getToken();
    let u = "/admin/hermes/stickers/" + encodeURIComponent(md5) + "/file";
    if (tok) u += "?token=" + encodeURIComponent(tok);
    return u;
  }

  async function fillStickerChats() {
    try {
      // 出站有硬校验（guardOutbound），这里仍只列白名单，名字用缓存补。
      const [data] = await Promise.all([api("/admin/targets"), loadRecipients()]);
      $("sticker-chats").innerHTML = (data.targets || [])
        .map((t) => `<option value="${esc(t.id)}">${esc(datalistLabel(t))}</option>`)
        .join("");
    } catch (_) {}
  }

  function openStickerDrawer() {
    const drawer = $("sticker-drawer");
    if (!drawer) return;
    drawer.hidden = false;
    drawer.classList.remove("hidden");
  }

  function closeStickerDrawer() {
    const drawer = $("sticker-drawer");
    if (!drawer) return;
    drawer.classList.add("hidden");
    drawer.hidden = true;
  }

  function clearStickerDetail() {
    document
      .querySelectorAll(".sticker-card.selected")
      .forEach((c) => c.classList.remove("selected"));
    selectedStickerMd5 = "";
    const sel = $("sticker-selected");
    if (sel) {
      sel.textContent = "未选";
      sel.classList.add("muted");
      sel.title = "";
    }
    const detail = $("sticker-detail");
    if (detail) {
      detail.classList.remove("muted", "empty");
      detail.innerHTML = "";
    }
    closeStickerDrawer();
  }

  async function loadStickerFacets() {
    await fillStickerChats();
    $("sticker-list").innerHTML = "";
    $("sticker-pager").classList.add("hidden");
    $("sticker-pager").innerHTML = "";
    clearStickerDetail();
    $("sticker-filter-hint").classList.remove("hidden");
    $("sticker-filter-hint").innerHTML =
      "先在左侧选择情绪/题材，或在上方输入关键词后点「加载」。";
    selectedMood = null;
    selectedTag = "";
    selectedStickerMd5 = "";
    stickerPage = 1;
    try {
      const data = await api("/admin/hermes/stickers/facets");
      $("sticker-lib-stats").textContent = (data.total || 0) + " 张";
      const moods = data.moods || [];
      $("mood-list").innerHTML = moods
        .map(
          (m) =>
            `<button type="button" class="mood-item" data-mood="${esc(m.name)}"><span>${esc(m.name)}</span><span class="cnt">${m.count}</span></button>`,
        )
        .join("");
      if (data.no_mood) {
        $("mood-list").insertAdjacentHTML(
          "beforeend",
          `<button type="button" class="mood-item" data-mood="__none__"><span>无情绪</span><span class="cnt">${data.no_mood}</span></button>`,
        );
      }
      $("mood-all").classList.add("active");
      $("mood-list")
        .querySelectorAll(".mood-item")
        .forEach((btn) => {
          btn.addEventListener("click", () => {
            $("mood-all").classList.remove("active");
            $("mood-list")
              .querySelectorAll(".mood-item")
              .forEach((b) => b.classList.remove("active"));
            $("tag-list")
              .querySelectorAll("button")
              .forEach((b) => b.classList.remove("active"));
            btn.classList.add("active");
            selectedTag = "";
            if (btn.dataset.mood === "__none__") {
              // 「无情绪」走专门的查漏模式：后端 no_mood=1 列出所有没有 mood 的表情
              selectedMood = "__none__";
            } else {
              selectedMood = btn.dataset.mood;
            }
            $("sticker-q").value = "";
            stickerPage = 1;
            loadStickers();
          });
        });
      $("mood-all").onclick = () => {
        $("mood-list")
          .querySelectorAll(".mood-item")
          .forEach((b) => b.classList.remove("active"));
        $("mood-all").classList.add("active");
        selectedMood = null;
        selectedTag = "";
        $("sticker-list").innerHTML = "";
        $("sticker-pager").classList.add("hidden");
        $("sticker-filter-hint").classList.remove("hidden");
        $("sticker-filter-hint").innerHTML =
          "已取消情绪筛选。输入关键词后点「加载」，或点选情绪。";
        stickerPage = 1;
        loadStickers();
      };

      const tags = data.tags || [];
      $("tag-list").innerHTML = tags
        .slice(0, 24)
        .map(
          (t) =>
            `<button type="button" data-tag="${esc(t.name)}">${esc(t.name)} ${t.count}</button>`,
        )
        .join("");
      $("tag-list")
        .querySelectorAll("button")
        .forEach((btn) => {
          btn.addEventListener("click", () => {
            $("tag-list")
              .querySelectorAll("button")
              .forEach((b) => b.classList.remove("active"));
            btn.classList.add("active");
            selectedTag = btn.dataset.tag;
            // 题材可与情绪叠加
            stickerPage = 1;
            loadStickers();
          });
        });
    } catch (e) {
      $("mood-list").innerHTML = `<div class="empty">${esc(e.message)}</div>`;
      toastErr(e.message);
    }
  }

  async function sendStickerMd5(md5) {
    const chat = $("sticker-chat").value.trim();
    if (!chat) {
      toastErr("请填写试发目标 chat_id");
      $("sticker-chat").focus();
      return;
    }
    if (!md5 || md5.length !== 32) return toastErr("md5 无效");
    if (!(await askConfirm("试发到\n" + chat + "\n" + md5, "试发表情"))) return;
    try {
      const res = await fetch("/admin/diagnose", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: "Bearer " + getToken(),
          "X-Admin-Token": getToken(),
        },
        body: JSON.stringify({ kind: "emoji", chat_id: chat, md5 }),
      });
      const data = await res.json();
      $("sticker-detail").innerHTML =
        `<pre class="code-block">${esc(JSON.stringify(data, null, 2))}</pre>`;
      if (!res.ok) toastErr(data.error || "失败");
      else toastOk("试发 outcome=" + data.outcome);
    } catch (e) {
      toastErr(e.message || String(e));
    }
  }

  async function loadStickers() {
    const q = $("sticker-q").value.trim();
    const pager = $("sticker-pager");
    const hasFilter = selectedMood != null || !!selectedTag || !!q;
    // 没有筛选时只能看第 1 页：让用户先去选情绪/题材/关键词
    if (!hasFilter) {
      $("sticker-filter-hint").classList.remove("hidden");
      $("sticker-filter-hint").innerHTML =
        `💡 先在左侧选择情绪，或输入关键词后点「加载」，库大避免一次拉全库。` +
        ` <button type="button" class="ghost" id="btnLoadFirstMood">加载第一个情绪</button>`;
      const b = $("btnLoadFirstMood");
      if (b)
        b.onclick = () => {
          const first = document.querySelector("#mood-list .mood-item");
          if (first) first.click();
          else $("sticker-q").focus();
        };
      $("sticker-list").innerHTML = "";
      pager.classList.add("hidden");
      pager.innerHTML = "";
      stickerPage = 1;
      return;
    }
    $("sticker-filter-hint").classList.add("hidden");
    showSkeleton("sticker-list", 6, "card");
    let path =
      "/admin/hermes/stickers?n=" + stickerPageSize + "&page=" + stickerPage;
    if (selectedMood === "__none__") {
      // 查漏：只列没有任何 mood 的表情，后端走专门分支
      path += "&no_mood=1";
    } else if (selectedMood) {
      path += "&mood=" + encodeURIComponent(selectedMood);
    }
    if (selectedTag) path += "&tag=" + encodeURIComponent(selectedTag);
    if (q) path += "&q=" + encodeURIComponent(q);

    try {
      const data = await api(path);
      const list = data.stickers || [];
      $("sticker-list").innerHTML = list.length
        ? list
            .map((s) => {
              const src = stickerFileURL(s.md5);
              const title = s.desc || s.md5 || "";
              const moods = (s.moods || []).join(" ") || "—";
              const use = s.use_count || 0;
              // 没有 file_size（旧 ops 版本）时按 0 处理，不显示大小提示
              const sizeKB = s.file_size
                ? Math.round(s.file_size / 1024)
                : null;
              const sizeHint =
                sizeKB && sizeKB > 0
                  ? sizeKB >= 1024
                    ? `${(sizeKB / 1024).toFixed(1)}MB`
                    : `${sizeKB}KB`
                  : "";
              const big = s.file_size && s.file_size > 2 * 1024 * 1024;
              return `<div class="sticker-card${big ? " sticker-big" : ""}" data-md5="${esc(s.md5)}" title="${esc(title)} · ${esc(s.md5)}${sizeHint ? " · " + sizeHint : ""}">
                <div class="thumb-wrap">
                  <img class="sticker-thumb" src="${esc(src)}" alt="" loading="lazy" />
                  <div class="thumb-fallback" hidden>⚠ 加载失败</div>
                </div>
                <div class="meta">
                  <strong>${esc(title)}</strong>
                  <span class="moods">${esc(moods)}</span>
                  <span class="use-count">使用 ${use} 次${sizeHint ? " · " + sizeHint : ""}</span>
                </div>
                <div class="card-actions">
                  <button type="button" class="ghost btn-copy" data-md5="${esc(s.md5)}" title="复制该表情的 md5 指纹">复制 md5</button>
                  <button type="button" class="primary btn-send" data-md5="${esc(s.md5)}" title="试发到 chat_id 输入框中的目标">试发</button>
                </div>
              </div>`;
            })
            .join("")
        : `<div class="empty">无结果（匹配 ${data.total_matched || 0}）</div>`;

      // 缩略图加载失败兜底：⚠ 覆盖层
      $("sticker-list")
        .querySelectorAll(".sticker-thumb")
        .forEach((img) => {
          img.addEventListener(
            "error",
            () => {
              const wrap = img.closest(".thumb-wrap");
              if (!wrap) return;
              img.hidden = true;
              const fb = wrap.querySelector(".thumb-fallback");
              if (fb) fb.hidden = false;
              wrap.classList.add("thumb-err");
            },
            { once: true },
          );
        });

      // 点击视觉反馈：键盘 ripple + 长按 lift
      $("sticker-list")
        .querySelectorAll(".sticker-card")
        .forEach((card) => {
          card.addEventListener("pointerdown", () =>
            card.classList.add("pressing"),
          );
          const release = () => card.classList.remove("pressing");
          card.addEventListener("pointerup", release);
          card.addEventListener("pointerleave", release);
          card.addEventListener("pointercancel", release);
        });

      // pagination controls
      const pager = $("sticker-pager");
      const page = data.page || 1;
      const pgSize = data.page_size || stickerPageSize;
      const total = data.total_matched || 0;
      const hasMore = !!data.has_more;
      const hasPrev = page > 1;
      const lastPage = Math.max(1, Math.ceil(total / Math.max(1, pgSize)));
      const buildBtn = (id, label, enabled) =>
        `<button type="button" class="ghost" id="${id}"${enabled ? "" : " disabled"}>${label}</button>`;
      const start = total === 0 ? 0 : (page - 1) * pgSize + 1;
      const end = start + list.length - 1;
      pager.classList.remove("hidden");
      pager.innerHTML =
        `<span class="pager-info">显示 <span class="pager-count">${start}-${end}</span> / 共 <span class="pager-count">${total}</span> 张</span>` +
        `<span class="pager-nav">` +
        buildBtn("pgFirst", "首页", page > 1) +
        buildBtn("pgPrev", "上一页", hasPrev) +
        `<span class="pager-count">第 ${page} / ${lastPage} 页</span>` +
        buildBtn("pgNext", "下一页", hasMore) +
        buildBtn("pgLast", "末页", page < lastPage) +
        `<input type="number" class="pager-jump" min="1" max="${lastPage}" value="${page}" title="回车跳转" />` +
        `</span>`;
      const on = (id, n) => {
        const b = $(id);
        if (b && !b.disabled)
          b.onclick = () => {
            stickerPage = n;
            loadStickers();
          };
      };
      on("pgFirst", 1);
      on("pgPrev", page - 1);
      on("pgNext", page + 1);
      on("pgLast", lastPage);

      $("sticker-list")
        .querySelectorAll(".sticker-card")
        .forEach((card) => {
          card.addEventListener("click", async (ev) => {
            if (ev.target.closest("button")) return;
            const md5 = card.dataset.md5;
            selectedStickerMd5 = md5;
            document
              .querySelectorAll(".sticker-card")
              .forEach((c) =>
                c.classList.toggle("selected", c.dataset.md5 === md5),
              );
            $("sticker-selected").textContent =
              "已选: " + md5.slice(0, 8) + "…";
            $("sticker-selected").title = md5;
            $("sticker-selected").classList.remove("muted");
            const detail = $("sticker-detail");
            openStickerDrawer();
            detail.classList.remove("muted", "empty");
            detail.innerHTML = `<div class="detail-loading"><span class="spinner"></span>加载详情…</div>`;
            try {
              const d = await api(
                "/admin/hermes/stickers/" + encodeURIComponent(md5),
              );
              const metaBits = [
                `<strong>${esc(d.desc || "(无描述)")}</strong>`,
                d.moods && d.moods.length
                  ? `<span class="muted">情绪: ${esc(d.moods.join(" / "))}</span>`
                  : "",
                d.tags && d.tags.length
                  ? `<span class="muted">题材: ${esc(d.tags.join(" / "))}</span>`
                  : "",
                `<span class="muted mono">md5: ${esc(d.md5)}</span>`,
              ]
                .filter(Boolean)
                .join("<br>");
              detail.innerHTML = `<div class="detail-head">
                <div class="preview-frame">
                  <img class="sticker-preview" src="${esc(stickerFileURL(md5))}" alt="" onerror="this.classList.add('preview-err')" />
                </div>
                <div class="detail-info">${metaBits}</div>
                <div class="detail-actions">
                  <button type="button" class="primary" id="dSend" title="试发到 chat_id 输入框中的目标">试发</button>
                  <button type="button" class="ghost" id="dCopy" title="复制 md5">复制 md5</button>
                </div>
              </div>`;
              $("dSend").onclick = () => sendStickerMd5(md5);
              $("dCopy").onclick = async () => {
                try {
                  await navigator.clipboard.writeText(md5);
                  toastOk("已复制 md5");
                } catch (_) {
                  toastErr(md5);
                }
              };
            } catch (e) {
              toastErr(e.message);
              clearStickerDetail();
            }
          });
        });
      $("sticker-list")
        .querySelectorAll(".btn-send")
        .forEach((b) => {
          b.addEventListener("click", (ev) => {
            ev.stopPropagation();
            sendStickerMd5(b.dataset.md5);
          });
        });
      $("sticker-list")
        .querySelectorAll(".btn-copy")
        .forEach((b) => {
          b.addEventListener("click", async (ev) => {
            ev.stopPropagation();
            try {
              await navigator.clipboard.writeText(b.dataset.md5);
              toastOk("已复制");
            } catch (_) {
              toastErr(b.dataset.md5);
            }
          });
        });
    } catch (e) {
      $("sticker-list").innerHTML = "";
      pager.classList.add("hidden");
      toastErr(e.message);
    }
  }

  $("btnStickers").addEventListener("click", () => {
    stickerPage = 1;
    loadStickers();
  });
  $("sticker-q").addEventListener("keydown", (ev) => {
    if (ev.key === "Enter") {
      stickerPage = 1;
      loadStickers();
    }
  });
  $("sticker-page-size").addEventListener("change", () => {
    stickerPageSize = parseInt($("sticker-page-size").value, 10);
    stickerPage = 1;
    loadStickers();
  });

  // 跳转：分页条里的页码输入框回车
  $("sticker-pager").addEventListener("keydown", (ev) => {
    if (ev.key !== "Enter") return;
    const t = ev.target;
    if (!t || !t.classList || !t.classList.contains("pager-jump")) return;
    const v = parseInt(t.value, 10);
    if (!Number.isFinite(v) || v < 1) return;
    stickerPage = v;
    loadStickers();
  });

  // ---- profiles ----
  // wxid → [{id, name, member_count}]，来自 /admin/data/member-groups（chatroom.json 成员表反查）。
  // 只做展示辅助：查不到（缓存里没有该群的成员表）就显示「群信息未知」，不影响档案本身。
  let profileGroupsCache = new Map();

  // 档案表单未保存保护：人格页有完整的一套，档案页此前是裸奔——
  // 改了字段直接点另一个人，改动静默丢失。baseline 记录 fillProfile 载入的值。
  const PROFILE_FIELDS = ["pf-name", "pf-aliases", "pf-personality", "pf-prefs", "pf-notes"];
  let profileBaseline = null; // null = 右栏还没载入过档案

  function profileFormValues() {
    return PROFILE_FIELDS.map((id) => ($(id) ? $(id).value : "")).join("|");
  }

  function profileIsDirty() {
    return profileBaseline !== null && profileFormValues() !== profileBaseline;
  }

  function profileSyncBaseline() {
    profileBaseline = profileFormValues();
    updateProfileDirtyPill();
  }

  function profileDropDraft() {
    profileBaseline = profileFormValues();
    updateProfileDirtyPill();
  }

  function updateProfileDirtyPill() {
    const el = $("profile-dirty");
    if (el) el.classList.toggle("hidden", !profileIsDirty());
  }

  // 切换档案对象前的确认：右栏有未保存改动且目标是另一个人时才拦。
  async function confirmProfileSwitch(wxid) {
    if (!profileIsDirty() || $("pf-wxid").value.trim() === wxid) return true;
    return askConfirm("档案有未保存改动，切换后将丢弃。继续吗？", "放弃未保存内容");
  }

  async function loadProfileGroups(wxids) {
    if (!wxids.length) return;
    try {
      const d = await api(
        "/admin/data/member-groups?wxids=" +
          encodeURIComponent(wxids.slice(0, 200).join(",")),
      );
      if (!d.ok) return;
      profileGroupsCache = new Map(Object.entries(d.groups || {}));
      for (const [wxid, list] of profileGroupsCache) {
        const el = $("profile-list").querySelector(
          `.btn-profile[data-wxid="${CSS.escape(wxid)}"]`,
        );
        if (!el) continue;
        const box = el.querySelector(".pf-meta");
        if (box) box.innerHTML = profileMetaHTML(wxid);
      }
      if (profileSelectedWXID) renderProfileGroups(profileSelectedWXID);
      bindGroupTags($("profile-list"));
    } catch {
      // 群信息是锦上添花，失败不打断档案主流程
    }
  }

  // 左栏群标签也可点：同一份抽屉，点了先选中该人再打开群成员。
  function bindGroupTags(root) {
    if (!root) return;
    root.querySelectorAll(".pf-tag.grp[data-gid]").forEach((el) => {
      el.addEventListener("click", async (ev) => {
        ev.stopPropagation(); // 别让它顺带触发「选中这个人」
        const item = el.closest(".btn-profile");
        const wxid = item && item.dataset.wxid ? item.dataset.wxid : "";
        if (item && wxid && wxid !== profileSelectedWXID) {
          if (!(await confirmProfileSwitch(wxid))) return;
          root.querySelectorAll(".btn-profile").forEach((n) => {
            n.classList.remove("selected");
            n.removeAttribute("aria-current");
          });
          item.classList.add("selected");
          item.setAttribute("aria-current", "true");
          profileSelectedWXID = wxid;
          fillProfile(wxid);
        }
        openGroupDrawer(el.dataset.gid, wxid);
      });
    });
  }

  function profileMetaHTML(wxid) {
    const list = profileGroupsCache.get(wxid);
    if (!list) return "";
    if (!list.length) return "";
    const tags = list
      .slice(0, 2)
      .map(
        (g) =>
          `<span class="pf-tag grp" data-gid="${esc(g.id)}" title="${esc(g.id)} · 点开看群成员">${esc(g.name || g.id)}</span>`,
      )
      .join("");
    const more =
      list.length > 2 ? `<span class="pf-tag">+${list.length - 2}</span>` : "";
    return tags + more;
  }

  function renderProfileGroups(wxid) {
    const box = $("pf-groups");
    if (!box) return;
    const list = profileGroupsCache.get(wxid);
    if (!list) {
      box.innerHTML = `<span class="pf-group none">群信息未加载</span>`;
      return;
    }
    if (!list.length) {
      box.innerHTML = `<span class="pf-group none">缓存里没有含此人的群成员表</span>`;
      return;
    }
    box.innerHTML = list
      .map(
        (g) =>
          `<button type="button" class="pf-group" data-gid="${esc(g.id)}" title="${esc(g.id)} · 点开看群成员">${esc(g.name || g.id)}<span class="gmeta">${g.member_count} 人</span></button>`,
      )
      .join("");
    box.querySelectorAll(".pf-group[data-gid]").forEach((el) => {
      el.addEventListener("click", () => openGroupDrawer(el.dataset.gid, wxid));
    });
  }

  // 群成员抽屉：从档案页的「所在群」点进来，标出当前这个人在群里。
  // 群名/人数优先用 member-groups 已有的缓存，详情靠 /admin/data/chatrooms/<id> 补。
  async function openGroupDrawer(gid, wxid) {
    const drawer = $("group-drawer");
    const body = $("group-detail");
    if (!drawer || !body) return;
    const known = (profileGroupsCache.get(wxid) || []).find((g) => g.id === gid);
    $("gd-title").textContent = known ? known.name || gid : gid;
    body.innerHTML = `<div class="gd-empty">载入成员…</div>`;
    drawer.hidden = false;
    drawer.classList.remove("hidden");
    try {
      const d = await api("/admin/data/chatrooms/" + encodeURIComponent(gid));
      if (!d.ok) throw new Error(d.error || "读取失败");
      const members = d.members || [];
      $("gd-title").textContent = d.name || (known && known.name) || gid;
      const head = `<div class="gd-id">${esc(gid)}</div>
        <div class="gd-stats">
          <span class="gd-stat">成员 <b>${members.length}</b></span>
          ${d.owner ? `<span class="gd-stat">群主 <b>${esc(memberName(d.owner, members))}</b></span>` : ""}
          <span class="gd-stat">缓存 <b>${d.in_chatroom_cache ? "有成员表" : "无成员表"}</b></span>
        </div>`;
      if (!members.length) {
        body.innerHTML = head + `<div class="gd-empty">该群不在 chatroom.json 的成员表里，只有联系人缓存。</div>`;
        return;
      }
      const list = members
        .map((m) => {
          const name = m.remark || m.display_name || m.nickname || m.username;
          const me = m.username === wxid;
          return `<div class="gd-member${me ? " me" : ""}"${me ? ' id="gd-me"' : ""}>
            <span class="mname">${esc(name)}</span>
            ${me ? `<span class="mbadge self">当前档案</span>` : ""}
            ${m.is_owner ? `<span class="mbadge owner">群主</span>` : ""}
            ${m.in_contact_cache ? `<span class="mbadge">好友</span>` : ""}
            <span class="mmeta">${esc(m.username)}</span>
          </div>`;
        })
        .join("");
      body.innerHTML = `${head}<div class="gd-member-list">${list}</div>`;
      const me = body.querySelector("#gd-me");
      if (me) me.scrollIntoView({ block: "center" });
    } catch (e) {
      body.innerHTML = `<div class="gd-empty">${esc(e.message)}</div>`;
    }
  }

  function closeGroupDrawer() {
    const drawer = $("group-drawer");
    if (!drawer) return;
    drawer.classList.add("hidden");
    drawer.hidden = true;
  }

  function memberName(wxid, members) {
    const m = (members || []).find((x) => x.username === wxid);
    if (!m) return wxid;
    return m.remark || m.display_name || m.nickname || wxid;
  }

  async function loadProfiles() {
    const q = $("profile-q").value.trim();
    let path = "/admin/hermes/member_profiles";
    if (q) path += "?q=" + encodeURIComponent(q);
    // 先记滚动位置再 showLoading：showLoading 会重写列表，scrollTop 立刻归零。
    const keepScroll = $("profile-list").parentElement
      ? $("profile-list").parentElement.scrollTop
      : 0;
    showLoading("profile-list", "加载档案…");
    try {
      const data = await api(path);
      const list = data.profiles || [];
      // 刷新列表时保住选中态与左栏滚动位置：左栏是独立滚动区，
      // 重绘会把 scrollTop 清零，用户会「跳回顶部」。
      // 左栏只回答「这是谁、在哪些群」：名字 + 唯一标识 + 群标签。
      // 性格/偏好/备注这些可编辑字段一律只放右栏，避免两边重复让人对不上。
      $("profile-list").innerHTML = list.length
        ? list
            .map((p) => {
              const sel = p.wxid && p.wxid === profileSelectedWXID ? " selected" : "";
              const name = p.display_name || p.wxid || "";
              const ident = name !== p.wxid && p.wxid ? p.wxid : "";
              const tags = profileMetaHTML(p.wxid);
              return `<div class="item btn-profile${sel}" data-wxid="${esc(p.wxid)}"${
                sel ? ' aria-current="true"' : ""
              }>
            <div class="grow">
              <strong>${esc(name)}</strong>
              ${ident ? `<div class="pf-line">${esc(ident)}</div>` : ""}
              <div class="pf-meta">${tags}</div>
            </div></div>`;
            })
            .join("")
        : `<div class="empty">无档案</div>`;
      $("profile-list")
        .querySelectorAll(".btn-profile")
        .forEach((el) => {
          el.addEventListener("click", async () => {
            const wxid = el.dataset.wxid || "";
            if (wxid !== profileSelectedWXID && !(await confirmProfileSwitch(wxid))) return;
            $("profile-list")
              .querySelectorAll(".btn-profile")
              .forEach((n) => {
                n.classList.remove("selected");
                n.removeAttribute("aria-current");
              });
            el.classList.add("selected");
            el.setAttribute("aria-current", "true");
            profileSelectedWXID = wxid;
            fillProfile(wxid);
          });
        });
      if (keepScroll && $("profile-list").parentElement) {
        $("profile-list").parentElement.scrollTop = keepScroll;
      }
      bindGroupTags($("profile-list"));
      // 重进页面 / 刷新后右栏要跟选中项对上，否则右侧一片空白
      const sel = $("profile-list").querySelector(".btn-profile.selected");
      if (sel && sel.dataset.wxid) fillProfile(sel.dataset.wxid);
      loadProfileGroups(list.map((p) => p.wxid).filter(Boolean));
    } catch (e) {
      handleLoadError("profile-list", e);
    }
  }

  // 多行字段随内容长高：性格/备注这类可能写很长，固定 3 行会让内容缩在
  // 小框里靠内部滚动看，不如把框撑开——整段和「所在群」一起在同一栏里滚。
  function autoGrow(el) {
    if (!el || el.tagName !== "TEXTAREA") return;
    el.style.height = "auto";
    // 留 2px 余量，避免正好卡在 scrollHeight 时出现内滚动条
    el.style.height = el.scrollHeight + 2 + "px";
  }

  function autosizeProfileFields() {
    for (const id of ["pf-personality", "pf-prefs", "pf-notes"]) autoGrow($(id));
  }

  async function fillProfile(wxid) {
    try {
      const p = await api(
        "/admin/hermes/member_profiles/" + encodeURIComponent(wxid),
      );
      // 换人后右栏从头显示；左栏位置不受影响。
      const pane = document.querySelector(".split-detail");
      if (pane) pane.scrollTop = 0;
      $("profile-empty").classList.add("hidden");
      $("profile-pane").classList.remove("hidden");
      $("pf-wxid").value = p.wxid || wxid;
      $("pf-name").value = p.display_name || "";
      $("pf-personality").value = p.personality || "";
      $("pf-prefs").value = (p.preferences || []).join(", ");
      $("pf-notes").value = p.notes || "";
      $("pf-aliases").value = (p.aliases || []).join(", ");
      // 头部：显示名 + wxid；标签区显示更新时间与「群数」
      $("pf-title").textContent = p.display_name || wxid;
      $("pf-sub").textContent = p.wxid || wxid;
      const badges = [];
      if (p.updated_at) badges.push(`<span class="pf-tag">更新 ${esc(fmtDate(p.updated_at))}</span>`);
      const groups = profileGroupsCache.get(p.wxid || wxid) || [];
      if (groups.length) badges.push(`<span class="pf-tag grp">在 ${groups.length} 个群</span>`);
      $("pf-badges").innerHTML = badges.join("");
      renderProfileGroups(p.wxid || wxid);
      $("profile-result").textContent = JSON.stringify(p, null, 2);
      autosizeProfileFields();
      profileSyncBaseline();
    } catch (e) {
      toastErr(e.message);
    }
  }

  $("btnProfiles").addEventListener("click", loadProfiles);
  for (const id of ["pf-personality", "pf-prefs", "pf-notes"]) {
    const el = $(id);
    if (el) el.addEventListener("input", () => autoGrow(el));
  }
  $("profile-q").addEventListener("keydown", (ev) => {
    if (ev.key === "Enter") loadProfiles();
  });
  $("profile-form").addEventListener("submit", async (ev) => {
    ev.preventDefault();
    const wxid = $("pf-wxid").value.trim();
    if (!wxid) return toastErr("wxid 必填");
    const body = {
      display_name: $("pf-name").value.trim(),
      personality: $("pf-personality").value.trim(),
      preferences: $("pf-prefs")
        .value.split(/[,，\n]/)
        .map((s) => s.trim())
        .filter(Boolean),
      notes: $("pf-notes").value.trim(),
      aliases: $("pf-aliases")
        .value.split(/[,，]/)
        .map((s) => s.trim())
        .filter(Boolean),
    };
    try {
      const res = await api(
        "/admin/hermes/member_profiles/" + encodeURIComponent(wxid),
        {
          method: "PUT",
          body: JSON.stringify(body),
        },
      );
      $("profile-result").textContent = JSON.stringify(res, null, 2);
      profileSyncBaseline();
      toastOk("已保存");
      loadProfiles();
    } catch (e) {
      toastErr(e.message);
    }
  });
  // 任意字段改动 → 刷新「未保存」标记
  $("profile-form").addEventListener("input", updateProfileDirtyPill);
  $("btnProfileDel").addEventListener("click", async () => {
    const wxid = $("pf-wxid").value.trim();
    if (!wxid || !(await askConfirm("删除 " + wxid + "？", "删除档案"))) return;
    try {
      $("profile-result").textContent = JSON.stringify(
        await api("/admin/hermes/member_profiles/" + encodeURIComponent(wxid), {
          method: "DELETE",
        }),
        null,
        2,
      );
      // 已删除：右栏回到空态，不再参与脏检查
      profileBaseline = null;
      updateProfileDirtyPill();
      $("profile-empty").classList.remove("hidden");
      $("profile-pane").classList.add("hidden");
      toastOk("已删除");
      loadProfiles();
    } catch (e) {
      toastErr(e.message);
    }
  });

  // ---- personas ----
  function personaBytes(value) {
    if (window.TextEncoder) return new TextEncoder().encode(value || "").length;
    return unescape(encodeURIComponent(value || "")).length;
  }

  function personaIsDirty() {
    const content = $("persona-content");
    const id = $("persona-id");
    return !!content &&
      (content.value !== personaBaseline || (personaCreating && id.value !== personaBaselineID));
  }

  function personaSetBaseline(value, id) {
    personaBaseline = value || "";
    personaBaselineID = id == null ? personaBaselineID : id;
    const dirty = $("persona-dirty");
    if (dirty) dirty.classList.add("hidden");
  }

  function updatePersonaBytes() {
    const input = $("persona-content");
    if (!input) return;
    const size = personaBytes(input.value);
    const counter = $("persona-bytes");
    counter.textContent = `${size} / ${PERSONA_MAX_BYTES} 字节`;
    counter.classList.toggle("over-limit", size > PERSONA_MAX_BYTES);
    $("persona-dirty").classList.toggle("hidden", !personaIsDirty());
    const canSave = personaCreating
      ? personaCapabilities && personaCapabilities.create
      : personaCapabilities && personaCapabilities.write;
    $("btnPersonaSave").disabled = !canSave || size > PERSONA_MAX_BYTES;
  }

  function personaCapability(raw, names, fallback) {
    if (!raw) return fallback;
    if (Array.isArray(raw)) return names.some((name) => raw.includes(name));
    for (const name of names) {
      if (Object.prototype.hasOwnProperty.call(raw, name)) return !!raw[name];
    }
    return fallback;
  }

  function applyPersonaCapabilities(raw) {
    personaCapabilities = {
      read: personaCapability(raw, ["personas.read", "read", "personas_read"], false),
      write: personaCapability(raw, ["personas.write", "write", "personas_write", "update"], false),
      create: personaCapability(raw, ["personas.write", "create", "personas_create"], false),
      bindings: personaCapability(raw, ["persona_bindings.read", "bindings", "bindings_read"], false),
      unbind: personaCapability(raw, ["persona_bindings.unbind", "unbind", "bindings_unbind"], false),
    };
    const notes = [];
    if (!personaCapabilities.read) notes.push("ops 尚未提供人格读取能力");
    if (personaCapabilities.read && !personaCapabilities.write)
      notes.push("后端未开放人格编辑，当前降级为只读");
    if (personaCapabilities.read && !personaCapabilities.create)
      notes.push("后端未开放人格新建");
    if (!personaCapabilities.bindings) notes.push("后端未提供会话绑定读取");
    if (personaCapabilities.bindings && !personaCapabilities.unbind)
      notes.push("后端未开放解除绑定");
    const box = $("persona-capability");
    box.innerHTML = notes.length
      ? `<ul>${notes.map((note) => `<li>${esc(note)}</li>`).join("")}</ul>`
      : "";
    $("btnPersonaNew").disabled = !personaCapabilities.create;
    updatePersonaBytes();
  }

  function personaUpdatedAt(ns) {
    const value = Number(ns || 0);
    if (!value) return "";
    const millis = value > 1e15 ? Math.floor(value / 1e6) : value * 1000;
    return new Date(millis).toLocaleString();
  }

  function renderPersonaList() {
    const q = $("persona-q").value.trim().toLowerCase();
    const list = personaListCache.filter((item) =>
      String(item.persona_id || item.id || "").toLowerCase().includes(q),
    );
    // 左栏是独立滚动区，重绘时要自己把滚动位置放回去，否则每次点选都跳回顶部
    const libPane = document.querySelector(".persona-library");
    const keepScroll = libPane ? libPane.scrollTop : 0;
    $("persona-list").innerHTML = list.length
      ? list.map((item) => {
          const id = item.persona_id || item.id || "";
          const size = Number(item.size_bytes || 0);
          const bindings = Number(item.binding_count || 0);
          const badges = [
            item.is_default ? '<span class="tag ok">默认</span>' : "",
            item.available === false ? '<span class="tag bad">不可用</span>' : "",
          ].join("");
          return `<button type="button" class="item persona-item${id === personaSelectedID ? " selected" : ""}" data-persona-id="${esc(id)}">
            <div class="grow"><strong>${esc(id)}</strong>
              <div class="muted">${size} 字节 · ${bindings} 个绑定</div>
              <div class="muted">${esc(personaUpdatedAt(item.updated_at_ns))}</div>
            </div>${badges}</button>`;
        }).join("")
      : '<div class="empty">没有匹配的人格</div>';
    $("persona-list").querySelectorAll(".persona-item").forEach((item) => {
      item.addEventListener("click", () => selectPersona(item.dataset.personaId));
    });
    if (libPane) libPane.scrollTop = keepScroll;
  }

  async function selectPersona(id, force) {
    if (!id) return;
    if (!force && personaIsDirty()) {
      if (!(await askConfirm("当前草稿尚未保存，仍要切换人格吗？", "放弃未保存内容")))
        return;
    }
    showLoading("persona-bindings", "加载绑定…");
    try {
      const detail = await apiResponse(
        "/admin/hermes/personas/" + encodeURIComponent(id),
      );
      const p = detail.data.persona || detail.data;
      personaSelectedID = p.persona_id || p.id || id;
      personaBindingCount = Number(p.binding_count || 0);
      personaCreating = false;
      personaETag = detail.headers.get("etag") || p.etag || p.revision || "";
      $("persona-id").value = personaSelectedID;
      $("persona-id").readOnly = true;
      $("persona-content").value = p.content || p.markdown || "";
      $("persona-editor-title").textContent = "编辑 " + personaSelectedID;
      $("btnPersonaDelete").disabled =
        personaSelectedID === "default" ||
        personaBindingCount > 0 ||
        !personaCapabilities.write;
      personaSetBaseline($("persona-content").value, personaSelectedID);
      updatePersonaBytes();
      // 右栏换人后从头显示（左栏滚动位置由 renderPersonaList 保住）
      const detailPane = document.querySelector(".persona-detail");
      if (detailPane) detailPane.scrollTop = 0;
      renderPersonaList();
      renderPersonaBindings(personaSelectedID);
    } catch (e) {
      showError("persona-bindings", e.message);
      toastErr(e.message);
    }
  }

  function renderPersonaBindings(id) {
    if (!personaCapabilities.bindings) {
      $("persona-bindings-count").textContent = "";
      showEmpty("persona-bindings", "当前后端不支持绑定状态读取");
      return;
    }
    const list = personaBindingsCache.filter(
      (item) => String(item.persona_id || "") === id,
    );
    $("persona-bindings-count").textContent = list.length + " 个";
    $("persona-bindings").innerHTML = list.length
      ? list.map((item) => `<div class="item persona-binding-item">
          <span class="tag">${personaCapabilities.unbind ? "显式" : "只读"}</span><div class="grow">
            <strong>${esc(item.session_key || "")}</strong>
            <div class="muted">更新 ${esc(item.updated_at || "—")}</div>
          </div><button type="button" class="ghost danger btn-persona-unbind" data-binding-id="${esc(item.binding_id || "")}" data-session-key="${esc(item.session_key || "")}"${personaCapabilities.unbind && item.binding_id ? "" : " disabled"}>解除绑定</button>
        </div>`).join("")
      : '<div class="empty">没有显式绑定；会话将继承 default</div>';
    $("persona-bindings").querySelectorAll(".btn-persona-unbind").forEach((btn) => {
      btn.addEventListener("click", async () => {
        if (!btn.dataset.bindingId || !personaCapabilities.unbind) return;
        if (!(await askConfirm(
          `解除 ${btn.dataset.sessionKey} 的显式绑定？\n下一批消息将继承 default；聊天历史和当前 run 不受影响。`,
          "解除人格绑定",
        ))) return;
        try {
          await api(
            "/admin/hermes/persona_bindings/" +
              encodeURIComponent(btn.dataset.bindingId),
            { method: "DELETE" },
          );
          toastOk("已解除绑定，从下一批消息生效");
          await loadPersonas();
          if (personaSelectedID) await selectPersona(personaSelectedID, true);
        } catch (e) {
          toastErr(e.message);
        }
      });
    });
  }

  async function loadPersonas() {
    showSkeleton("persona-list", 4);
    try {
      const listData = await api("/admin/hermes/personas");
      const capabilityData = listData.capabilities || null;
      applyPersonaCapabilities(capabilityData);
      // 列表接口已通，至少具备读取；capabilities 字段可后补。
      if (!capabilityData) personaCapabilities.read = true;
      if (!personaCapabilities.read) {
        personaListCache = [];
        personaBindingsCache = [];
        showEmpty("persona-list", "ops 尚未支持人格管理，请先更新 Hermes 侧服务");
        showEmpty("persona-bindings", "无可用的人格绑定接口");
        return;
      }
      personaListCache = listData.personas || listData.items || [];
      renderPersonaList();
      if (personaCapabilities.bindings) {
        try {
          const bindingData = await api("/admin/hermes/persona_bindings");
          personaBindingsCache = bindingData.bindings || bindingData.items || [];
          if (bindingData.capabilities)
            applyPersonaCapabilities(Object.assign({}, capabilityData, bindingData.capabilities));
          if (bindingData.bindings_error) {
            const box = $("persona-capability");
            const extra = `<li>${esc("绑定文件异常：" + bindingData.bindings_error)}</li>`;
            if ((box.innerHTML || "").includes("</ul>"))
              box.innerHTML = box.innerHTML.replace("</ul>", extra + "</ul>");
            else box.innerHTML = `<ul>${extra}</ul>`;
          }
        } catch (e) {
          if (e.status === 404 || e.status === 405 || e.status === 501) {
            personaCapabilities.bindings = false;
            personaCapabilities.unbind = false;
          } else throw e;
        }
      }
      if (personaSelectedID && !personaCreating)
        renderPersonaBindings(personaSelectedID);
      else if (!personaSelectedID && personaListCache.length)
        await selectPersona(
          personaListCache[0].persona_id || personaListCache[0].id,
          true,
        );
      else if (!personaListCache.length && !personaCreating) newPersonaDraft();
    } catch (e) {
      applyPersonaCapabilities({ write: false, create: false, bindings: false, unbind: false });
      if (e.status === 404 || e.status === 405 || e.status === 501) {
        showEmpty("persona-list", "ops 尚未支持人格管理，请先更新 Hermes 侧服务");
        showEmpty("persona-bindings", "无可用的人格绑定接口");
        return;
      }
      showError("persona-list", e.message);
      toastErr(e.message);
    }
  }

  async function newPersonaDraft() {
    if (personaIsDirty()) {
      if (!(await askConfirm("当前草稿尚未保存，仍要新建人格吗？", "放弃未保存内容")))
        return;
    }
    personaSelectedID = "";
    personaBindingCount = 0;
    personaETag = "";
    personaCreating = true;
    $("persona-id").value = "";
    $("persona-id").readOnly = false;
    $("persona-content").value = "";
    $("persona-editor-title").textContent = "新建人格";
    $("btnPersonaDelete").disabled = true;
    $("persona-bindings-count").textContent = "";
    showEmpty("persona-bindings", "新建人格尚无绑定");
    personaSetBaseline("", "");
    updatePersonaBytes();
    renderPersonaList();
    $("persona-id").focus();
  }

  $("btnPersonas").addEventListener("click", loadPersonas);
  $("persona-q").addEventListener("input", renderPersonaList);
  $("btnPersonaNew").addEventListener("click", newPersonaDraft);
  $("persona-content").addEventListener("input", updatePersonaBytes);
  $("persona-id").addEventListener("input", updatePersonaBytes);
  $("btnPersonaReload").addEventListener("click", async () => {
    if (!personaIsDirty()) return;
    if (!(await askConfirm("放弃当前未保存草稿？", "放弃草稿"))) return;
    if (personaCreating) newPersonaDraft();
    else selectPersona(personaSelectedID, true);
  });

  $("persona-form").addEventListener("submit", async (ev) => {
    ev.preventDefault();
    const id = $("persona-id").value.trim();
    const content = $("persona-content").value;
    const size = personaBytes(content);
    if (!id) return toastErr("人格 ID 必填");
    if (!content.trim()) return toastErr("Markdown 内容不能为空");
    if (size > PERSONA_MAX_BYTES) return toastErr("内容超过 64 KiB UTF-8 上限");
    const headers = {};
    if (!personaCreating) {
      if (!personaETag) return toastErr("缺少版本标识，请刷新详情后再保存");
      headers["If-Match"] = personaETag;
    }
    $("btnPersonaSave").disabled = true;
    try {
      const result = await apiResponse(
        personaCreating
          ? "/admin/hermes/personas"
          : "/admin/hermes/personas/" + encodeURIComponent(personaSelectedID),
        {
          method: personaCreating ? "POST" : "PUT",
          headers,
          body: JSON.stringify({ id, persona_id: id, content }),
        },
      );
      const p = result.data.persona || result.data;
      personaSelectedID = p.persona_id || p.id || id;
      personaCreating = false;
      personaETag = result.headers.get("etag") || p.etag || p.revision || personaETag;
      personaSetBaseline(content);
      toastOk("人格已保存");
      await loadPersonas();
      await selectPersona(personaSelectedID, true);
    } catch (e) {
      if (e.status === 412) {
        toastWarn("人格已被其他操作更新；草稿仍保留，请复制后刷新对比");
        $("persona-dirty").classList.remove("hidden");
      } else toastErr(e.message);
    } finally {
      updatePersonaBytes();
    }
  });

  $("btnPersonaDelete").addEventListener("click", async () => {
    if (!personaSelectedID || personaSelectedID === "default") return;
    if (personaBindingCount > 0)
      return toastWarn("该人格仍有显式绑定，请先逐条解除绑定");
    if (!(await askConfirm(
      `删除人格 ${personaSelectedID}？\n此操作不可撤销。`,
      "删除人格",
    ))) return;
    try {
      await api(
        "/admin/hermes/personas/" + encodeURIComponent(personaSelectedID),
        { method: "DELETE" },
      );
      toastOk("人格已删除");
      personaSelectedID = "";
      personaBindingCount = 0;
      personaETag = "";
      personaCreating = false;
      personaSetBaseline("", "");
      await loadPersonas();
    } catch (e) {
      toastErr(e.message);
    }
  });

  window.addEventListener("beforeunload", (ev) => {
    if (!personaIsDirty() && !gateIsDirty() && !profileIsDirty()) return;
    ev.preventDefault();
    ev.returnValue = "";
  });

  // ---- diagnose ----
  async function loadDiagChats() {
    // 每次进诊断页都同步一次字段显隐：md5 只在 emoji 类型下显示
    syncDiagFileUI();
    try {
      const [data] = await Promise.all([api("/admin/targets"), loadRecipients()]);
      $("diag-chats").innerHTML = (data.targets || [])
        .map((t) => `<option value="${esc(t.id)}">${esc(datalistLabel(t))}</option>`)
        .join("");
    } catch (_) {}
  }

  const DIAG_LIMITS = {
    image: 45 * 1024 * 1024,
    video: 45 * 1024 * 1024,
    voice: 10 * 1024 * 1024,
    emoji: 45 * 1024 * 1024,
  };
  const DIAG_ACCEPT = {
    image: "image/*",
    video: "video/*",
    voice: "audio/*,.amr,.silk,.mp3,.wav,.m4a,.ogg",
    emoji: "image/gif,image/png,image/webp,image/*",
  };

  function formatBytes(n) {
    if (n < 1024) return n + " B";
    if (n < 1024 * 1024) return (n / 1024).toFixed(1) + " KB";
    return (n / (1024 * 1024)).toFixed(1) + " MB";
  }

  function syncDiagFileUI() {
    const kind = $("diag-kind").value;
    const input = $("diag-file");
    const nameEl = $("diag-file-name");
    if (input) input.accept = DIAG_ACCEPT[kind] || "*/*";
    // md5 只有试发表情时才有意义，其余类型收起，少一排无关系的名字
    const md5Label = $("diag-md5") && $("diag-md5").closest("label");
    if (md5Label) md5Label.style.display = kind === "emoji" ? "" : "none";
    const file = input && input.files && input.files[0];
    if (!nameEl) return;
    if (!file) {
      nameEl.textContent = "未选择文件";
      return;
    }
    nameEl.textContent = file.name + " · " + formatBytes(file.size);
  }

  $("diag-kind").addEventListener("change", syncDiagFileUI);
  $("diag-file").addEventListener("change", syncDiagFileUI);

  $("diag-form").addEventListener("submit", async (ev) => {
    ev.preventDefault();
    $("diag-result").textContent = "…";
    const kind = $("diag-kind").value;
    const chat = $("diag-chat").value.trim();
    const url = $("diag-url").value.trim();
    const md5 = $("diag-md5").value.trim();
    const file = $("diag-file").files && $("diag-file").files[0];
    if (file) {
      const limit = DIAG_LIMITS[kind] || DIAG_LIMITS.image;
      if (file.size > limit) {
        const msg = kind + " 文件超过 " + (limit >> 20) + "MB 上限";
        $("diag-result").textContent = msg;
        toastErr(msg);
        return;
      }
    }
    try {
      const headers = {
        Authorization: "Bearer " + getToken(),
        "X-Admin-Token": getToken(),
      };
      let body;
      if (file) {
        const fd = new FormData();
        fd.append("kind", kind);
        fd.append("chat_id", chat);
        fd.append("file", file, file.name);
        body = fd;
      } else {
        headers["Content-Type"] = "application/json";
        body = JSON.stringify({ kind, chat_id: chat, url, md5 });
      }
      const res = await fetch("/admin/diagnose", {
        method: "POST",
        headers,
        body,
      });
      const data = await res.json();
      $("diag-result").textContent = JSON.stringify(data, null, 2);
      if (!res.ok) toastErr(data.error || "失败");
      else toastOk("outcome=" + data.outcome);
    } catch (e) {
      $("diag-result").textContent = String(e);
      toastErr(e.message || String(e));
    }
  });

  // ---- command palette ----
  // key 是单键跳转快捷键（焦点不在输入框时生效），在面板里可见可查
  const PAGES = [
    { tab: "overview", title: "总览", hint: "桥运行态与红灯", key: "g" },
    { tab: "targets", title: "白名单", hint: "会话路由", key: "b" },
    { tab: "gate", title: "门闩", hint: "触发策略", key: "m" },
    { tab: "inbound", title: "入站", hint: "旁路 trace", key: "i" },
    { tab: "sessions", title: "会话", hint: "桥本地 session", key: "s" },
    { tab: "hermes", title: "运维", hint: "Hermes ops", key: "h" },
    { tab: "stickers", title: "表情", hint: "情绪库", key: "e" },
    { tab: "profiles", title: "档案", hint: "群友偏好", key: "p" },
    { tab: "personas", title: "人格", hint: "Markdown 与绑定状态", key: "r" },
    { tab: "golem", title: "Golem 插件设置台", hint: "全部插件 · 新标签打开", key: "c" },
    { tab: "diagnose", title: "诊断", hint: "媒体试发", key: "d" },
  ];
  let cmdkIndex = 0;

  function cmdkVisible() {
    const el = $("cmdk");
    return el && !el.classList.contains("hidden");
  }

  function filteredPages() {
    const q = (($("cmdk-q") && $("cmdk-q").value) || "").trim().toLowerCase();
    if (!q) return PAGES;
    return PAGES.filter((p) =>
      (p.title + p.hint + p.tab).toLowerCase().includes(q),
    );
  }

  function renderCmdk() {
    const list = $("cmdk-list");
    if (!list) return;
    const items = filteredPages();
    if (!items.length) {
      list.innerHTML = `<div class="empty">无匹配页面</div>`;
      return;
    }
    if (cmdkIndex >= items.length) cmdkIndex = 0;
    list.innerHTML = items
      .map(
        (p, i) =>
          `<button type="button" class="cmdk-item${i === cmdkIndex ? " active" : ""}" data-tab="${esc(p.tab)}"><strong>${esc(p.title)}</strong><span class="muted">${esc(p.hint)}</span>${p.key ? `<kbd>${esc(p.key)}</kbd>` : ""}</button>`,
      )
      .join("");
    list.querySelectorAll(".cmdk-item").forEach((btn) => {
      btn.addEventListener("click", () => {
        closeCmdk();
        if (btn.dataset.tab === "golem") openGolem();
        else setPage(btn.dataset.tab);
      });
    });
  }

  // 插件设置台是独立页面（同服务、独立版式），新标签打开。
  // 用命名窗口复用：连按 c 不再开一排标签，而是聚焦已打开的那个。
  function openGolem() {
    if (openGolem._w && !openGolem._w.closed) {
      openGolem._w.focus();
      return;
    }
    openGolem._w = window.open("golem.html", "golem_settings");
  }

  function openCmdk() {
    const el = $("cmdk");
    if (!el || $("app-shell").classList.contains("hidden")) return;
    el.hidden = false;
    el.classList.remove("hidden");
    cmdkIndex = 0;
    if ($("cmdk-q")) $("cmdk-q").value = "";
    renderCmdk();
    setTimeout(() => $("cmdk-q") && $("cmdk-q").focus(), 20);
  }

  function closeCmdk() {
    const el = $("cmdk");
    if (!el) return;
    el.classList.add("hidden");
    el.hidden = true;
  }

  if ($("btnCommand")) $("btnCommand").addEventListener("click", openCmdk);
  if ($("cmdk-q"))
    $("cmdk-q").addEventListener("input", () => {
      cmdkIndex = 0;
      renderCmdk();
    });
  if ($("cmdk")) {
    $("cmdk").addEventListener("click", (ev) => {
      if (ev.target === $("cmdk")) closeCmdk();
    });
  }
  if ($("confirm")) {
    $("confirm").addEventListener("click", (ev) => {
      if (ev.target === $("confirm") && $("confirm-cancel"))
        $("confirm-cancel").click();
    });
  }

  document.addEventListener("keydown", (ev) => {
    const inField = /^(INPUT|TEXTAREA|SELECT)$/.test(
      (ev.target && ev.target.tagName) || "",
    );
    if ((ev.ctrlKey || ev.metaKey) && ev.key.toLowerCase() === "k") {
      ev.preventDefault();
      if (cmdkVisible()) closeCmdk();
      else openCmdk();
      return;
    }
    if (ev.key === "Escape") {
      if (cmdkVisible()) {
        ev.preventDefault();
        closeCmdk();
        return;
      }
      if ($("app-shell").classList.contains("nav-open")) {
        ev.preventDefault();
        setNavOpen(false);
        return;
      }
      if (
        $("group-drawer") &&
        !$("group-drawer").classList.contains("hidden")
      ) {
        ev.preventDefault();
        closeGroupDrawer();
        return;
      }
      if (
        $("sticker-drawer") &&
        !$("sticker-drawer").classList.contains("hidden")
      ) {
        ev.preventDefault();
        clearStickerDetail();
        return;
      }
      if (
        $("confirm") &&
        !$("confirm").classList.contains("hidden") &&
        $("confirm-cancel")
      ) {
        $("confirm-cancel").click();
      }
      return;
    }
    if (cmdkVisible()) {
      const items = filteredPages();
      if (ev.key === "ArrowDown") {
        ev.preventDefault();
        cmdkIndex = (cmdkIndex + 1) % Math.max(items.length, 1);
        renderCmdk();
      } else if (ev.key === "ArrowUp") {
        ev.preventDefault();
        cmdkIndex =
          (cmdkIndex - 1 + Math.max(items.length, 1)) %
          Math.max(items.length, 1);
        renderCmdk();
      } else if (ev.key === "Enter") {
        ev.preventDefault();
        const pick = items[cmdkIndex];
        if (pick) {
          closeCmdk();
          if (pick.tab === "golem") openGolem();
          else setPage(pick.tab);
        }
      }
      return;
    }
    if (inField || ev.ctrlKey || ev.metaKey || ev.altKey) return;
    // 单键跳转快捷键：与命令面板里展示的 key 一致，统一从 PAGES 生成
    const keyMap = Object.fromEntries(
      PAGES.filter((p) => p.key && p.tab !== "golem").map((p) => [p.key, p.tab]),
    );
    if (ev.key === "c" && !$("app-shell").classList.contains("hidden")) {
      openGolem();
      return;
    }
    if (keyMap[ev.key] && !$("app-shell").classList.contains("hidden"))
      setPage(keyMap[ev.key]);
  });

  if ($("btnDrawerClose"))
    $("btnDrawerClose").addEventListener("click", clearStickerDetail);
  if ($("sticker-drawer")) {
    $("sticker-drawer").addEventListener("click", (ev) => {
      if (ev.target === $("sticker-drawer")) clearStickerDetail();
    });
  }
  if ($("btnGroupClose"))
    $("btnGroupClose").addEventListener("click", closeGroupDrawer);
  if ($("group-drawer")) {
    $("group-drawer").addEventListener("click", (ev) => {
      if (ev.target === $("group-drawer")) closeGroupDrawer();
    });
  }

  // ---- 窄屏抽屉导航 ----
  function setNavOpen(open) {
    $("app-shell").classList.toggle("nav-open", open);
    const scrim = $("nav-scrim");
    if (scrim) {
      scrim.classList.toggle("hidden", !open);
      scrim.hidden = !open;
    }
  }

  if ($("btnNav")) $("btnNav").addEventListener("click", () => setNavOpen(true));
  if ($("nav-scrim"))
    $("nav-scrim").addEventListener("click", () => setNavOpen(false));

  // hash 恢复：F5 / 从历史进来时落在 # 指定的页。登录成功后也会调一次。
  function enterHashPage() {
    if ($("app-shell").classList.contains("hidden")) return;
    const h = location.hash.slice(1);
    if (h && PAGES.some((p) => p.tab === h)) setPage(h);
  }

  // boot
  (async () => {
    await tryEnter();
    enterHashPage();
  })();
})();
