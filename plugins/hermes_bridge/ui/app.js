(() => {
  const TOKEN_KEY = "hermes_bridge_admin_token";
  const $ = (id) => document.getElementById(id);
  const toastEl = $("toast");
  let stickerPage = 1;
  let stickerPageSize = 20;

  function toast(msg, opts) {
    opts = typeof opts === "boolean" ? { err: opts } : (opts || {});
    const dur = opts.dur || 3000;
    toastEl.textContent = msg;
    toastEl.className = "toast";
    if (opts.err) toastEl.classList.add("err");
    if (opts.warn) toastEl.classList.add("warn");
    toastEl.classList.remove("hidden");
    clearTimeout(toastEl._t);
    toastEl._t = setTimeout(() => toastEl.classList.add("hidden"), dur);
  }

  function toastOk(msg) { toast(msg, { dur: 2000 }); }
  function toastWarn(msg) { toast(msg, { warn: true, dur: 3500 }); }
  function toastErr(msg) { toast(msg, { err: true, dur: 4000 }); }

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
    el.innerHTML = Array.from({ length: count }, () => `<div class="skeleton ${cls}"></div>`).join("");
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

  async function api(path, opts = {}) {
    const headers = Object.assign({}, opts.headers || {});
    const token = getToken();
    if (token) {
      headers["Authorization"] = "Bearer " + token;
      headers["X-Admin-Token"] = token;
    }
    if (opts.body && !headers["Content-Type"]) {
      headers["Content-Type"] = "application/json";
    }
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
    return data;
  }

  // ---- auth gate ----
  function showLogin(errMsg) {
    stopTraceLive();
    $("app-shell").classList.add("hidden");
    $("login-screen").classList.remove("hidden");
    $("login-token").value = "";
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
      showLogin(e.status === 401 ? "token 无效" : e.message || "无法连接管理台");
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
  const pageActions = {
    overview: () => {
      const b = document.createElement("button");
      b.className = "primary";
      b.textContent = "刷新";
      b.onclick = () => loadOverview();
      return [b];
    },
    sessions: () => {
      const b = document.createElement("button");
      b.className = "primary";
      b.textContent = "刷新";
      b.onclick = () => loadSessions();
      return [b];
    },
    hermes: () => {
      const b = document.createElement("button");
      b.className = "primary";
      b.textContent = "刷新";
      b.onclick = () => loadHermes();
      return [b];
    },
  };

  function setPage(tab) {
    document.querySelectorAll("#nav button").forEach((b) => b.classList.toggle("active", b.dataset.tab === tab));
    document.querySelectorAll(".panel").forEach((p) => p.classList.remove("active"));
    const panel = $("panel-" + tab);
    if (!panel) return;
    panel.classList.add("active");
    $("page-title").textContent = panel.dataset.title || tab;
    $("page-sub").textContent = panel.dataset.sub || "";
    const box = $("topbar-actions");
    box.innerHTML = "";
    const makers = pageActions[tab];
    if (makers) makers().forEach((el) => box.appendChild(el));

    if (tab !== "inbound") stopTraceLive();
    if (tab === "overview") loadOverview();
    if (tab === "targets") loadTargets();
    if (tab === "gate") loadGate();
    if (tab === "inbound") loadTraceRecent();
    if (tab === "sessions") loadSessions();
    if (tab === "hermes") loadHermes();
    if (tab === "stickers") {
      // 切到表情面板：清空旧详情，避免从其它页进来时还残留选中态
      clearStickerDetail();
      loadStickerFacets();
    }
    if (tab === "profiles") loadProfiles();
    if (tab === "diagnose") loadDiagChats();
  }

  document.querySelectorAll("#nav button").forEach((btn) => {
    btn.addEventListener("click", () => setPage(btn.dataset.tab));
  });

  function card(label, value, cls) {
    return `<div class="card ${cls || ""}"><div class="label">${label}</div><div class="value">${value}</div></div>`;
  }

  function applyOverview(o) {
    $("ver").textContent = "v" + (o.version || "");
    const subsCls = o.subscribers > 0 ? "ok" : "bad";
    $("overview-cards").innerHTML = [
      card("SSE 订阅者", o.subscribers, subsCls),
      card("白名单", o.targets),
      card("本地会话", o.local_sessions),
      card("去抖 pending", o.pending_debounce),
      card("未推缓冲", o.buffered_unflushed),
      card("media_ref", o.media_refs),
      card("业务 token", o.token_masked || "—"),
      card("主人", o.owner_ok ? o.owner_name || "已识别" : "未识别", o.owner_ok ? "ok" : "bad"),
      card("机器人", o.self_name || o.self_id || "—"),
      card("管理监听", o.admin_listen || "—"),
      card("业务监听", o.listen || "—"),
      card("限流", `${o.send_rate_per_min}/分`),
      card("Hermes ops", o.hermes_ops_configured ? "已配置" : "未配置", o.hermes_ops_configured ? "ok" : ""),
    ].join("");
    $("gate-summary").textContent = o.gate_summary || "";
    const alerts = o.alerts || [];
    $("alerts").innerHTML = alerts.length
      ? "<ul>" + alerts.map((a) => `<li>${esc(a)}</li>`).join("") + "</ul>"
      : "";
  }

  async function loadOverview() {
    try {
      showSkeleton("overview-cards", 12, "card");
      applyOverview(await api("/admin/overview"));
    } catch (e) {
      if (e.status === 401) {
        setToken("");
        showLogin("登录已失效");
        return;
      }
      showError("overview-cards", e.message);
      $("alerts").innerHTML = `<ul><li>${esc(e.message)}</li></ul>`;
      toastErr(e.message);
    }
  }

  // ---- targets ----
  async function loadTargets() {
    try {
      showSkeleton("target-list", 4);
      const data = await api("/admin/targets");
      const list = data.targets || [];
      $("target-count").textContent = list.length + " 个";
      $("target-list").innerHTML = list.length
        ? list
            .map(
              (t) => `<div class="item" data-id="${esc(t.id)}">
            <span class="tag ${t.kind}">${t.kind === "group" ? "群" : "私聊"}</span>
            <div class="grow"><strong>${esc(t.name || t.id)}</strong><div class="muted">${esc(t.id)}</div></div>
            <button type="button" class="ghost danger btn-del">移除</button>
          </div>`
            )
            .join("")
        : `<div class="empty">白名单为空</div>`;
      $("target-list").querySelectorAll(".btn-del").forEach((btn) => {
        btn.addEventListener("click", async () => {
          const id = btn.closest(".item").dataset.id;
          if (!confirm("移出白名单？\n" + id)) return;
          try {
            await api("/admin/targets/" + encodeURIComponent(id), { method: "DELETE" });
            toastOk("已移除");
            loadTargets();
          } catch (e) {
            toastErr(e.message);
          }
        });
      });
    } catch (e) {
      toastErr(e.message);
    }
  }

  $("btnAddTarget").addEventListener("click", async () => {
    const id = $("target-id").value.trim();
    const name = $("target-name").value.trim();
    if (!id) toastErr("请填写 id");
    try {
      await api("/admin/targets", { method: "POST", body: JSON.stringify({ id, name }) });
      toastOk("已加入");
      $("target-id").value = "";
      $("target-name").value = "";
      loadTargets();
    } catch (e) {
      toastErr(e.message);
    }
  });

  $("btnSearch").addEventListener("click", doSearch);
  $("search-q").addEventListener("keydown", (ev) => {
    if (ev.key === "Enter") doSearch();
  });

  async function doSearch() {
    const q = $("search-q").value.trim();
    if (!q) return toastErr("输入关键词");
    showLoading("search-results", "搜索中…");
    try {
      const data = await api("/admin/contacts/search?q=" + encodeURIComponent(q));
      const results = data.results || [];
      $("search-results").innerHTML = results.length
        ? results
            .map(
              (h) => `<div class="item">
            <span class="tag ${h.kind}">${h.kind === "group" ? "群" : "私聊"}</span>
            <div class="grow"><strong>${esc(h.name)}</strong><div class="muted">${esc(h.id)}</div></div>
            <button type="button" class="primary btn-add-hit" data-id="${esc(h.id)}" data-name="${esc(h.name)}">加入</button>
          </div>`
            )
            .join("")
        : `<div class="empty">无匹配</div>`;
      $("search-results").querySelectorAll(".btn-add-hit").forEach((btn) => {
        btn.addEventListener("click", async () => {
          try {
            await api("/admin/targets", {
              method: "POST",
              body: JSON.stringify({ id: btn.dataset.id, name: btn.dataset.name }),
            });
            toastOk("已加入");
            loadTargets();
          } catch (e) {
            toastErr(e.message);
          }
        });
      });
    } catch (e) {
      toastErr(e.message);
    }
  }

  // ---- gate ----
  async function loadGate() {
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
    } catch (e) {
      toastErr(e.message);
    }
  }

  $("gate-form").addEventListener("submit", async (ev) => {
    ev.preventDefault();
    const body = {
      trigger_names: $("f-triggers")
        .value.split(/[,，]/)
        .map((s) => s.trim())
        .filter(Boolean),
      group_push_all: $("f-pushall").checked,
      debounce_seconds: num($("f-debounce").value),
      bubble_rate: num($("f-bubble").value),
      bubble_cooldown_minutes: num($("f-bubble-cd").value),
      max_context_messages: num($("f-ctx").value),
      emoji_burst_count: num($("f-burst").value),
      emoji_burst_window_seconds: num($("f-burst-win").value),
      emoji_burst_cooldown_minutes: num($("f-burst-cd").value),
      max_text_len: num($("f-text").value),
      send_rate_per_min: num($("f-rate").value),
    };
    try {
      const c = await api("/admin/config", { method: "PATCH", body: JSON.stringify(body) });
      $("gate-preview").textContent = c.gate_summary || "";
      toastOk("已保存");
    } catch (e) {
      toastErr(e.message);
    }
  });

  // ---- inbound ----
  let es = null;
  const seenTrace = new Set();

  function fmtTs(ts) {
    if (!ts) return "";
    return new Date(ts * 1000).toLocaleTimeString();
  }

  function renderTrace(ev, prepend) {
    if (!ev || ev.id == null || seenTrace.has(ev.id)) return;
    seenTrace.add(ev.id);
    const el = document.createElement("div");
    el.className = "trace-item " + (ev.kind || "");
    const bits = [ev.chat_name || ev.chat_id, ev.user_name, ev.trigger_reason, ev.reason, ev.text]
      .filter(Boolean)
      .join(" · ");
    el.innerHTML = `<div class="k">${esc(ev.kind || "?")}</div>
      <div><span class="muted">${esc(fmtTs(ev.ts))} #${ev.id}</span>
      ${ev.msg_count ? " ×" + ev.msg_count : ""}
      <div>${esc(bits)}</div></div>`;
    if (prepend) $("trace-list").prepend(el);
    else $("trace-list").appendChild(el);
  }

  async function loadTraceRecent() {
    try {
      const data = await api("/admin/inbound/recent?n=80");
      $("trace-list").innerHTML = "";
      seenTrace.clear();
      (data.events || []).forEach((ev) => renderTrace(ev, false));
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
    es = new EventSource("/admin/inbound/stream?token=" + encodeURIComponent(token));
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
      el.textContent = "断开";
      el.className = "pill err";
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

  $("btnTraceLive").addEventListener("click", () => loadTraceRecent().then(startTraceLive));
  $("btnTraceStop").addEventListener("click", stopTraceLive);
  $("btnTraceRefresh").addEventListener("click", loadTraceRecent);
  $("btnTraceClear").addEventListener("click", () => {
    $("trace-list").innerHTML = "";
    seenTrace.clear();
  });

  // ---- sessions ----
  async function loadSessions() {
    try {
      showSkeleton("session-list", 5);
      const data = await api("/admin/sessions");
      const list = data.sessions || [];
      $("session-list").innerHTML = list.length
        ? list
            .map((s) => {
              const flags = [];
              if (s.pending_debounce) flags.push("去抖中");
              if (s.unflushed_count) flags.push("未推 " + s.unflushed_count);
              if (s.bubble_cool_remain_sec) flags.push("冒泡 " + s.bubble_cool_remain_sec + "s");
              if (s.burst_cool_remain_sec) flags.push("斗图 " + s.burst_cool_remain_sec + "s");
              if (!s.in_whitelist) flags.push("非白名单");
              return `<div class="item">
              <span class="tag ${s.chat_type === "group" ? "group" : "private"}">${s.chat_type === "group" ? "群" : "私聊"}</span>
              <div class="grow">
                <strong>${esc(s.chat_name || s.chat_id || s.session_key)}</strong>
                <div class="muted">${esc(s.session_key)} · 上下文 ${s.context_count}${s.last_trigger ? " · " + esc(s.last_trigger) : ""}</div>
                ${flags.length ? `<div class="muted">${esc(flags.join(" · "))}</div>` : ""}
              </div></div>`;
            })
            .join("")
        : `<div class="empty">暂无本地会话</div>`;
    } catch (e) {
      toastErr(e.message);
    }
  }

  // ---- hermes ----
  async function loadHermes() {
    const stEl = $("hermes-ops-status");
    stEl.textContent = "…";
    stEl.className = "pill";
    showSkeleton("hermes-cards", 3, "card");
    try {
      const meta = await api("/admin/hermes/meta");
      if (!meta.configured) {
        stEl.textContent = "未配置";
        stEl.className = "pill err";
        $("hermes-alerts").innerHTML =
          "<ul><li>配置 hermes_ops_url；脚本放 <code>~/.hermes/ops/</code></li></ul>";
        $("hermes-cards").innerHTML = "";
        return;
      }
      let line = meta.ops_url || "ops";
      if (meta.ops_version) line += " · v" + meta.ops_version;
      stEl.textContent = line;
      stEl.className = "pill" + (meta.ops_reachable === false ? " err" : meta.ops_reachable ? " live" : "");
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
        card("gateway", st.is_active || "?", st.ok ? "ok" : "bad"),
        card("tools", ov.tools_ok ? "ok" : "检查", ov.tools_ok ? "ok" : "bad"),
        card("HOME", ov.hermes_home || "—"),
      ].join("");
      const tools = await api("/admin/hermes/tools/check");
      $("hermes-tools").textContent = JSON.stringify(
        { ok: tools.ok, hint: tools.hint, registered_samples: tools.registered_samples, crash_samples: tools.crash_samples },
        null,
        2
      );
      $("hermes-sessions").textContent = JSON.stringify(await api("/admin/hermes/sessions?n=30"), null, 2);
    } catch (e) {
      stEl.textContent = "失败";
      stEl.className = "pill err";
      $("hermes-alerts").innerHTML = `<ul><li>${esc(e.message)}</li></ul>`;
      toastErr(e.message);
    }
  }

  $("btnHermesLog").addEventListener("click", async () => {
    const file = $("hermes-log-file").value;
    const grep = $("hermes-log-grep").value.trim();
    let path = "/admin/hermes/logs?file=" + encodeURIComponent(file) + "&n=100";
    if (grep) path += "&grep=" + encodeURIComponent(grep);
    $("hermes-log").textContent = "…";
    try {
      const data = await api(path);
      if (!data.exists) {
        $("hermes-log").textContent = "不存在: " + (data.path || file);
        return;
      }
      $("hermes-log").textContent = (data.path || "") + "\n---\n" + ((data.lines || []).join("\n") || "空");
    } catch (e) {
      $("hermes-log").textContent = e.message;
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
      const data = await api("/admin/targets");
      $("sticker-chats").innerHTML = (data.targets || [])
        .map((t) => `<option value="${esc(t.id)}">${esc(t.name || t.id)}</option>`)
        .join("");
    } catch (_) {}
  }

  function clearStickerDetail() {
    document.querySelectorAll(".sticker-card.selected").forEach((c) => c.classList.remove("selected"));
    selectedStickerMd5 = "";
    const sel = $("sticker-selected");
    if (sel) {
      sel.textContent = "未选";
      sel.classList.add("muted");
      sel.title = "";
    }
    const detail = $("sticker-detail");
    if (detail) {
      detail.classList.add("hidden");
      detail.classList.remove("muted", "empty");
      detail.innerHTML = "";
    }
  }

  async function loadStickerFacets() {
    await fillStickerChats();
    $("sticker-list").innerHTML = "";
    $("sticker-pager").classList.add("hidden");
    $("sticker-pager").innerHTML = "";
    clearStickerDetail();
    $("sticker-filter-hint").classList.remove("hidden");
    $("sticker-filter-hint").innerHTML = "先在左侧选择情绪/题材，或在上方输入关键词后点「加载」。";
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
            `<button type="button" class="mood-item" data-mood="${esc(m.name)}"><span>${esc(m.name)}</span><span class="cnt">${m.count}</span></button>`
        )
        .join("");
      if (data.no_mood) {
        $("mood-list").insertAdjacentHTML(
          "beforeend",
          `<button type="button" class="mood-item" data-mood="__none__"><span>无情绪</span><span class="cnt">${data.no_mood}</span></button>`
        );
      }
      $("mood-all").classList.add("active");
      $("mood-list").querySelectorAll(".mood-item").forEach((btn) => {
        btn.addEventListener("click", () => {
          $("mood-all").classList.remove("active");
          $("mood-list").querySelectorAll(".mood-item").forEach((b) => b.classList.remove("active"));
          $("tag-list").querySelectorAll("button").forEach((b) => b.classList.remove("active"));
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
        $("mood-list").querySelectorAll(".mood-item").forEach((b) => b.classList.remove("active"));
        $("mood-all").classList.add("active");
        selectedMood = null;
        selectedTag = "";
        $("sticker-list").innerHTML = "";
        $("sticker-pager").classList.add("hidden");
        $("sticker-filter-hint").classList.remove("hidden");
        $("sticker-filter-hint").innerHTML = "已取消情绪筛选。输入关键词后点「加载」，或点选情绪。";
        stickerPage = 1;
        loadStickers();
      };

      const tags = data.tags || [];
      $("tag-list").innerHTML = tags
        .slice(0, 24)
        .map((t) => `<button type="button" data-tag="${esc(t.name)}">${esc(t.name)} ${t.count}</button>`)
        .join("");
      $("tag-list").querySelectorAll("button").forEach((btn) => {
        btn.addEventListener("click", () => {
          $("tag-list").querySelectorAll("button").forEach((b) => b.classList.remove("active"));
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
    if (!confirm("试发到\n" + chat + "\n" + md5)) return;
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
      $("sticker-detail").innerHTML = `<pre class="code-block">${esc(JSON.stringify(data, null, 2))}</pre>`;
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
      if (b) b.onclick = () => {
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
    let path = "/admin/hermes/stickers?n=" + stickerPageSize + "&page=" + stickerPage;
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
              const sizeKB = s.file_size ? Math.round(s.file_size / 1024) : null;
              const sizeHint = sizeKB && sizeKB > 0 ? (sizeKB >= 1024 ? `${(sizeKB/1024).toFixed(1)}MB` : `${sizeKB}KB`) : "";
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
                  <button type="button" class="ghost btn-copy" data-md5="${esc(s.md5)}" title="复制 md5">md5</button>
                  <button type="button" class="primary btn-send" data-md5="${esc(s.md5)}" title="试发到 chat_id 输入框中的目标">试发</button>
                </div>
              </div>`;
            })
            .join("")
        : `<div class="empty">无结果（匹配 ${data.total_matched || 0}）</div>`;

      // 缩略图加载失败兜底：⚠ 覆盖层
      $("sticker-list").querySelectorAll(".sticker-thumb").forEach((img) => {
        img.addEventListener("error", () => {
          const wrap = img.closest(".thumb-wrap");
          if (!wrap) return;
          img.hidden = true;
          const fb = wrap.querySelector(".thumb-fallback");
          if (fb) fb.hidden = false;
          wrap.classList.add("thumb-err");
        }, { once: true });
      });

      // 点击视觉反馈：键盘 ripple + 长按 lift
      $("sticker-list").querySelectorAll(".sticker-card").forEach((card) => {
        card.addEventListener("pointerdown", () => card.classList.add("pressing"));
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
      const on = (id, n) => { const b = $(id); if (b && !b.disabled) b.onclick = () => { stickerPage = n; loadStickers(); }; };
      on("pgFirst", 1);
      on("pgPrev", page - 1);
      on("pgNext", page + 1);
      on("pgLast", lastPage);

      $("sticker-list").querySelectorAll(".sticker-card").forEach((card) => {
        card.addEventListener("click", async (ev) => {
          if (ev.target.closest("button")) return;
          const md5 = card.dataset.md5;
          selectedStickerMd5 = md5;
          document.querySelectorAll(".sticker-card").forEach((c) => c.classList.toggle("selected", c.dataset.md5 === md5));
          $("sticker-selected").textContent = "已选: " + md5.slice(0, 8) + "…";
          $("sticker-selected").title = md5;
          $("sticker-selected").classList.remove("muted");
          const detail = $("sticker-detail");
          detail.classList.remove("hidden", "muted", "empty");
          detail.innerHTML = `<div class="detail-loading"><span class="spinner"></span>加载详情…</div>`;
          try {
            const d = await api("/admin/hermes/stickers/" + encodeURIComponent(md5));
            const metaBits = [
              `<strong>${esc(d.desc || "(无描述)")}</strong>`,
              d.moods && d.moods.length ? `<span class="muted">情绪: ${esc(d.moods.join(" / "))}</span>` : "",
              d.tags && d.tags.length ? `<span class="muted">题材: ${esc(d.tags.join(" / "))}</span>` : "",
              `<span class="muted mono">md5: ${esc(d.md5)}</span>`,
            ].filter(Boolean).join("<br>");
            detail.innerHTML =
              `<div class="detail-head">
                <div class="preview-frame">
                  <img class="sticker-preview" src="${esc(stickerFileURL(md5))}" alt="" onerror="this.classList.add('preview-err')" />
                </div>
                <div class="detail-info">${metaBits}</div>
                <div class="detail-actions">
                  <button type="button" class="primary" id="dSend" title="试发到 chat_id 输入框中的目标">↑ 试发</button>
                  <button type="button" class="ghost" id="dCopy" title="复制 md5">复制</button>
                  <button type="button" class="ghost close-x" id="dClose" title="关闭详情" aria-label="关闭">×</button>
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
            $("dClose").onclick = () => clearStickerDetail();
          } catch (e) {
            toastErr(e.message);
            clearStickerDetail();
          }
        });
      });
      $("sticker-list").querySelectorAll(".btn-send").forEach((b) => {
        b.addEventListener("click", (ev) => {
          ev.stopPropagation();
          sendStickerMd5(b.dataset.md5);
        });
      });
      $("sticker-list").querySelectorAll(".btn-copy").forEach((b) => {
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

  $("btnStickers").addEventListener("click", () => { stickerPage = 1; loadStickers(); });
  $("sticker-q").addEventListener("keydown", (ev) => {
    if (ev.key === "Enter") { stickerPage = 1; loadStickers(); }
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
  async function loadProfiles() {
    const q = $("profile-q").value.trim();
    let path = "/admin/hermes/member_profiles";
    if (q) path += "?q=" + encodeURIComponent(q);
    showLoading("profile-list", "加载档案…");
    try {
      const data = await api(path);
      const list = data.profiles || [];
      $("profile-list").innerHTML = list.length
        ? list
            .map(
              (p) => `<div class="item btn-profile" data-wxid="${esc(p.wxid)}" style="cursor:pointer">
            <div class="grow">
              <strong>${esc(p.display_name || p.wxid || "")}</strong>
              <div class="muted">${esc(p.wxid || "")}</div>
              <div class="muted">${esc(p.personality || "")}</div>
            </div></div>`
            )
            .join("")
        : `<div class="empty">无档案</div>`;
      $("profile-list").querySelectorAll(".btn-profile").forEach((el) => {
        el.addEventListener("click", () => fillProfile(el.dataset.wxid));
      });
    } catch (e) {
      toastErr(e.message);
    }
  }

  async function fillProfile(wxid) {
    try {
      const p = await api("/admin/hermes/member_profiles/" + encodeURIComponent(wxid));
      $("pf-wxid").value = p.wxid || wxid;
      $("pf-name").value = p.display_name || "";
      $("pf-personality").value = p.personality || "";
      $("pf-prefs").value = (p.preferences || []).join(", ");
      $("pf-notes").value = p.notes || "";
      $("pf-aliases").value = (p.aliases || []).join(", ");
      $("profile-result").textContent = JSON.stringify(p, null, 2);
    } catch (e) {
      toastErr(e.message);
    }
  }

  $("btnProfiles").addEventListener("click", loadProfiles);
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
        .value.split(/[,，]/)
        .map((s) => s.trim())
        .filter(Boolean),
      notes: $("pf-notes").value.trim(),
      aliases: $("pf-aliases")
        .value.split(/[,，]/)
        .map((s) => s.trim())
        .filter(Boolean),
    };
    try {
      const res = await api("/admin/hermes/member_profiles/" + encodeURIComponent(wxid), {
        method: "PUT",
        body: JSON.stringify(body),
      });
      $("profile-result").textContent = JSON.stringify(res, null, 2);
      toastOk("已保存");
      loadProfiles();
    } catch (e) {
      toastErr(e.message);
    }
  });
  $("btnProfileDel").addEventListener("click", async () => {
    const wxid = $("pf-wxid").value.trim();
    if (!wxid || !confirm("删除 " + wxid + "？")) return;
    try {
      $("profile-result").textContent = JSON.stringify(
        await api("/admin/hermes/member_profiles/" + encodeURIComponent(wxid), { method: "DELETE" }),
        null,
        2
      );
      toastOk("已删除");
      loadProfiles();
    } catch (e) {
      toastErr(e.message);
    }
  });

  // ---- diagnose ----
  async function loadDiagChats() {
    try {
      const data = await api("/admin/targets");
      $("diag-chats").innerHTML = (data.targets || [])
        .map((t) => `<option value="${esc(t.id)}">${esc(t.name || t.id)}</option>`)
        .join("");
    } catch (_) {}
  }

  $("diag-form").addEventListener("submit", async (ev) => {
    ev.preventDefault();
    $("diag-result").textContent = "…";
    try {
      const res = await fetch("/admin/diagnose", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: "Bearer " + getToken(),
          "X-Admin-Token": getToken(),
        },
        body: JSON.stringify({
          kind: $("diag-kind").value,
          chat_id: $("diag-chat").value.trim(),
          url: $("diag-url").value.trim(),
          md5: $("diag-md5").value.trim(),
        }),
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

  // boot
  tryEnter();
})();
