// Golem 插件设置台：表单渲染器 + 脏检查 + 键级保存。
//
// 数据流：
//   /admin/plugin-config              → 段清单（哪些插件、版本/只读/exe）
//   /admin/plugin-config/keys?root=.. → 该段全部叶子键 + 值片段 + etag
//   /admin/plugin-config/section?..   → 整段原文（兜底页签）
//   /admin/data/recipients            → 群/好友缓存（接收者选择器、群信息浮层）
//
// 写盘只发改动过的键；expect_raw 用载入时的值片段做单键乐观锁。
(() => {
  const TOKEN_KEY = "hermes_bridge_admin_token";
  const SCHEMA = window.GOLEM_SCHEMA || {};
  const COMMON = SCHEMA.COMMON || [];
  const SCHEMAS = SCHEMA.SCHEMAS || {};
  const PLUGIN_ORDER = SCHEMA.PLUGIN_ORDER || [];
  const DEPRECATED = new Set(SCHEMA.DEPRECATED_PLUGINS || []);

  const $ = (id) => document.getElementById(id);

  const S = {
    sections: [],
    recipients: null,
    recipientsAt: 0,
    root: "",
    run: 1,
    keys: [],
    tables: [],
    etag: "",
    readonly: false,
    reason: "",
    path: "",
    sites: new Map(),
    base: new Map(),
    work: new Map(),
    covered: new Set(),
    tab: "form",
    rawText: "",
    rawBase: "",
    rawEtag: "",
    rawRO: false,
    rawLoaded: false,
    saving: false,
    groupID: "",
    // 左栏两组筛选互相独立、可叠加：
    //   filterState = all | on | off | miss
    //   filterMode  = all | black | white
    filterState: "all",
    filterMode: "all",
  };

  // ---------- 小工具 ----------

  function esc(s) {
    return String(s == null ? "" : s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  // stable 用键排序后的序列化做深比较：inline table 的键顺序不该算改动。
  function stable(v) {
    if (v === null || typeof v !== "object") return JSON.stringify(v);
    if (Array.isArray(v)) return "[" + v.map(stable).join(",") + "]";
    const keys = Object.keys(v).sort();
    return "{" + keys.map((k) => JSON.stringify(k) + ":" + stable(v[k])).join(",") + "}";
  }
  const same = (a, b) => stable(a) === stable(b);

  function getToken() {
    return (localStorage.getItem(TOKEN_KEY) || "").trim();
  }
  function setToken(t) {
    t = (t || "").trim();
    if (t) localStorage.setItem(TOKEN_KEY, t);
    else localStorage.removeItem(TOKEN_KEY);
  }

  async function api(method, path, body) {
    const headers = {};
    const token = getToken();
    if (token) {
      headers["Authorization"] = "Bearer " + token;
      headers["X-Admin-Token"] = token;
    }
    const opts = { method, headers };
    if (body !== undefined) {
      headers["Content-Type"] = "application/json";
      opts.body = JSON.stringify(body);
    }
    const res = await fetch(path, opts);
    const ct = res.headers.get("content-type") || "";
    const data = ct.includes("application/json") ? await res.json() : { error: await res.text() };
    if (!res.ok) {
      const e = new Error((data && (data.error || data.code)) || res.statusText || "请求失败");
      e.status = res.status;
      e.data = data;
      throw e;
    }
    return data;
  }

  let toastTimer = 0;
  function toast(msg, kind) {
    const el = $("toast");
    el.textContent = msg;
    el.className = "toast" + (kind ? " " + kind : "");
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => el.classList.add("hidden"), kind === "err" ? 5200 : 2600);
  }

  function fmtClock(iso) {
    if (!iso) return "未知";
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return iso;
    const p = (n) => String(n).padStart(2, "0");
    return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
  }

  function banner(text, kind, btnLabel, onClick) {
    const box = $("banner");
    $("banner-text").textContent = text;
    box.className = "banner" + (kind ? " " + kind : "");
    box.classList.remove("hidden");
    const btn = $("banner-btn");
    if (btnLabel) {
      btn.textContent = btnLabel;
      btn.hidden = false;
      btn.onclick = onClick;
    } else {
      btn.hidden = true;
      btn.onclick = null;
    }
  }
  function hideBanner() {
    $("banner").classList.add("hidden");
  }

  // ---------- 键身份 ----------

  // 去掉根段：ai.config.reply_rate → [config, reply_rate]
  function relSegs(site) {
    return (site.segs || []).slice(1);
  }

  function colPathSegs(key) {
    // schema 里的 key 是不带引号的点分路径（本项目所有 collection 键都是裸键）
    return key.split(".");
  }

  // 前缀匹配；allowIdx 允许最后一段是 [[数组表]] 实例序号。
  function matchPrefix(segs, path, allowIdx) {
    if (segs.length <= path.length) return null;
    for (let i = 0; i < path.length; i++) {
      if (segs[i].k !== path[i]) return null;
      if (segs[i].i >= 0 && !(allowIdx && i === path.length - 1)) return null;
    }
    return segs.slice(path.length);
  }

  function siteOfKey(fieldKey) {
    return S.sites.get(S.root + "." + fieldKey);
  }

  // ---------- 值与控件 ----------

  function baseType(t) {
    return t.indexOf("tri-") === 0 ? t.slice(4) : t;
  }
  const isTri = (t) => t.indexOf("tri-") === 0;

  function coerce(type, raw) {
    switch (type) {
      case "int": {
        const n = Number(raw);
        return Number.isFinite(n) ? Math.trunc(n) : NaN;
      }
      case "number": {
        const n = Number(raw);
        return Number.isFinite(n) ? n : NaN;
      }
      case "bool":
        return !!raw;
      default:
        return raw;
    }
  }

  function fmtVal(v) {
    if (v === null || v === undefined) return "";
    if (typeof v === "string") return v;
    return String(v);
  }

  function switchHTML(id, on, disabled) {
    return `<label class="switch"><input type="checkbox" data-kid="${esc(id)}" data-ctype="bool"${
      on ? " checked" : ""
    }${disabled ? " disabled" : ""}/><span class="track"></span><span class="lbl">${
      on ? "开" : "关"
    }</span></label>`;
  }

  function textInput(id, type, val, disabled, extra) {
    const t = type === "password" ? "password" : type === "url" ? "url" : "text";
    const ac = type === "password" ? ' autocomplete="new-password"' : "";
    return `<input type="${t}" data-kid="${esc(id)}" data-ctype="${esc(type)}" value="${esc(
      fmtVal(val),
    )}"${disabled ? " disabled" : ""}${ac}${extra || ""} spellcheck="false" />`;
  }

  function numInput(id, type, val, f, disabled) {
    const step = f.step != null ? f.step : type === "int" ? 1 : "any";
    const extra =
      (f.min != null ? ` min="${f.min}"` : "") +
      (f.max != null ? ` max="${f.max}"` : "") +
      ` step="${step}"`;
    return `<div class="inline-num"><input type="number" data-kid="${esc(id)}" data-ctype="${esc(
      type,
    )}" value="${esc(fmtVal(val))}"${disabled ? " disabled" : ""}${extra} />${
      f.unit ? `<span class="unit">${esc(f.unit)}</span>` : ""
    }</div>`;
  }

  function selectHTML(id, options, val, disabled, ctype) {
    const cur = fmtVal(val);
    const known = options.some((o) => o[0] === cur);
    let html = `<select data-kid="${esc(id)}" data-ctype="${esc(ctype || "enum")}"${
      disabled ? " disabled" : ""
    }>`;
    if (!known && cur !== "") html += `<option value="${esc(cur)}" selected>${esc(cur)}（当前值，不在预设里）</option>`;
    for (const o of options) {
      html += `<option value="${esc(o[0])}"${o[0] === cur ? " selected" : ""}>${esc(o[1] || o[0])}</option>`;
    }
    html += "</select>";
    return html;
  }

  function textareaHTML(id, val, disabled, rows) {
    return `<textarea data-kid="${esc(id)}" data-ctype="textarea" rows="${
      rows || 5
    }"${disabled ? " disabled" : ""} spellcheck="false">${esc(fmtVal(val))}</textarea>`;
  }

  // ---------- 接收者 ----------

  function recipientIndex() {
    const idx = new Map();
    const r = S.recipients;
    if (!r) return idx;
    for (const g of r.groups || []) idx.set(g.id, { name: g.name || g.id, kind: "群", id: g.id, meta: `${g.member_count} 人` });
    for (const c of r.contacts || []) {
      const nm = c.remark || c.nickname || c.alias || c.username;
      idx.set(c.username, { name: nm, kind: "好友", id: c.username, meta: c.alias || "" });
    }
    return idx;
  }

  function tagHTML(list, ctype, disabled, id) {
    const idx = recipientIndex();
    let html = `<div class="tagbox" data-ctype="${ctype || "tags"}"${
      id ? ` data-kid="${esc(id)}"` : ""
    }${disabled ? ' data-ro="1"' : ""}>`;
    list.forEach((v, i) => {
      const info = idx.get(v);
      html += `<span class="tag"><span class="tname">${esc(info ? info.name : v)}</span>${
        info ? `<span class="tid">${esc(v)}</span>` : ""
      }<button type="button" data-tagrm="${i}" aria-label="移除"${disabled ? " disabled" : ""}>×</button></span>`;
    });
    html += `<input type="text" class="tag-in" placeholder="${
      disabled ? "" : "输入或搜索后回车添加"
    }"${disabled ? " disabled" : ""} spellcheck="false" />`;
    if (ctype === "recipients") html += `<div class="sug hidden"></div>`;
    html += "</div>";
    return html;
  }

  // ---------- 字段 ----------

  function fieldHTML(field, opts) {
    const id = opts.id;
    const site = opts.site;
    const val = opts.value;
    const ro = opts.ro;
    const tri = isTri(field.type);
    const type = baseType(field.type);
    const disabled = ro || !!opts.missing;
    let ctl = "";

    if (opts.missing) {
      ctl = `<div class="field-help">未设置（键不存在）。本页只能改已有值，新增请用「原文」页签。</div>`;
    } else if (type === "bool") {
      ctl = switchHTML(id, !!val, disabled);
    } else if (type === "enum") {
      ctl = selectHTML(id, field.options || [], val, disabled);
    } else if (type === "int" || type === "number") {
      ctl = numInput(id, type, val, field, disabled);
    } else if (type === "textarea") {
      ctl = textareaHTML(id, val, disabled, field.rows);
    } else if (type === "tags" || type === "recipients") {
      ctl = tagHTML(Array.isArray(val) ? val : [], type, disabled, id);
    } else {
      ctl = textInput(id, type, val, disabled);
    }

    const note = [];
    if (site && site.reason) note.push(site.reason);
    if (tri && !opts.missing) note.push("键缺失时按默认值处理；本页不能新增该键。");

    return `<div class="field${disabled ? " ro" : ""}" data-kid="${esc(id)}">
      <div class="field-label"><span class="lab">${esc(field.label || field.key)}</span><code class="k">${esc(
        field.key,
      )}</code></div>
      <div class="field-ctl">${ctl}${
        field.help ? `<div class="field-help">${esc(field.help)}</div>` : ""
      }${note.length ? `<div class="field-help">${esc(note.join(" "))}</div>` : ""}
      <div class="field-err hidden"></div></div>
    </div>`;
  }

  // 渲染一个 schema 字段；把 id 记进 covered。
  function renderField(field, idOverride, valueOverride, siteOverride, roOverride) {
    const id = idOverride || S.root + "." + field.key;
    const site = siteOverride || S.sites.get(id);
    const missing = !site;
    const ro = roOverride != null ? roOverride : missing || !site.editable || S.readonly;
    const val = valueOverride !== undefined ? valueOverride : missing ? undefined : site.value;
    S.covered.add(id);
    return fieldHTML(field, { id, site, value: val, ro, missing });
  }

  // ---------- 表单渲染 ----------

  function renderForm() {
    const sc = SCHEMAS[S.root] || { title: S.root, groups: [] };
    const sec = S.sections.find((s) => s.root === S.root && s.run === S.run) || {};
    const head = $("form-head");
    const body = $("form-body");

    head.innerHTML = `<div class="htext">
        <h2>${esc(sc.title || S.root)}${sc.version ? `<span class="ver">依据 ${esc(sc.version)}</span>` : ""}</h2>
        <div class="desc">${esc(sc.desc || "")}</div>
        <div class="meta">段 <code>${esc(S.root)}</code> · 第 ${sec.start_line || "?"}–${sec.end_line || "?"} 行 · ${
      (S.keys.length || 0)
    } 个键${S.readonly ? ` · <b>只读</b>：${esc(S.reason || "")}` : ""}</div>
      </div>
      <div class="tabs"><button type="button" data-tab="form"${
        S.tab === "form" ? ' class="active"' : ""
      }>表单</button><button type="button" data-tab="raw"${
        S.tab === "raw" ? ' class="active"' : ""
      }>原文</button></div>`;

    const parts = [];

    // 公共段
    parts.push(`<section class="card"><div class="card-h"><h3>公共设置</h3><span class="hint">所有插件通用（host 的 plugin.Config）</span></div><div class="card-b">${COMMON.map(
      (f) => renderField(f),
    ).join("")}</div></section>`);

    for (const g of sc.groups || []) {
      parts.push(`<section class="card"><div class="card-h"><h3>${esc(g.title || "设置")}</h3>${
        g.hint ? `<span class="hint">${esc(g.hint)}</span>` : ""
      }</div><div class="card-b">${g.fields.map((f) => renderField(f)).join("")}</div></section>`);
    }

    for (const col of sc.collections || []) {
      parts.push(renderCollection(col));
    }

    // 未识别键
    const unknown = S.keys.filter((k) => !S.covered.has(k.id));
    if (unknown.length) {
      parts.push(`<section class="card"><div class="card-h"><h3>未识别键</h3>
        <span class="hint">表单没有覆盖的键，只读展示</span><span class="count">${unknown.length}</span></div>
        <div class="card-b"><div class="unknown-list">${unknown
          .map(
            (k) => `<div class="unknown-row"><span class="uid">${esc(k.id)}</span>
              <span class="uraw" title="${esc(k.raw)}">${esc(k.raw)}</span>
              <button type="button" data-goraw="${k.line}">去原文编辑</button></div>`,
          )
          .join("")}</div></div></section>`);
    }

    body.innerHTML = parts.join("");
    head.querySelectorAll(".tabs button").forEach((b) => {
      b.onclick = () => switchTab(b.dataset.tab);
    });
    attachFormEvents(body);
    renderRawHead();
    updateSavebar();
  }

  function renderCollection(col) {
    const path = colPathSegs(col.key);
    const wantArray = col.kind === "array-of-table";
    const buckets = new Map(); // 实例名/序号 → 字段名 → site

    for (const k of S.keys) {
      const rest = matchPrefix(relSegs(k), path, wantArray);
      if (!rest || !rest.length) continue;
      if (wantArray) {
        const idx = relSegs(k)[path.length - 1].i;
        if (idx < 0) continue;
        if (rest.length !== 1) continue;
        const key = String(idx);
        if (!buckets.has(key)) buckets.set(key, { idx, fields: {} });
        buckets.get(key).fields[rest[0].k] = k;
      } else if (col.kind === "map-of-scalar") {
        // 名字 → 标量：整条只有一段
        if (rest.length !== 1 || rest[0].i >= 0) continue;
        const name = rest[0].k;
        if (!buckets.has(name)) buckets.set(name, { name, fields: {} });
        buckets.get(name).fields[""] = k;
      } else {
        if (rest[0].i >= 0 || rest.length !== 2) continue;
        const name = rest[0].k;
        if (!buckets.has(name)) buckets.set(name, { name, fields: {} });
        buckets.get(name).fields[rest[1].k] = k;
      }
    }

    const items = [];
    if (wantArray) {
      const arr = [...buckets.values()].sort((a, b) => a.idx - b.idx);
      arr.forEach((b, n) => {
        const labelField = col.labelField && b.fields[col.labelField];
        const title =
          col.labelField === null
            ? `第 ${n + 1} 条`
            : `第 ${n + 1} 条${
                labelField && labelField.value ? " · " + fmtVal(labelField.value) : ""
              }`;
        items.push(itemHTML(b, col, title, `#${b.idx}`, arr.length > 1));
      });
    } else {
      const list = [...buckets.values()].sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
      list.forEach((b) => {
        items.push(itemHTML(b, col, b.name, "", list.length > 1));
      });
    }

    let body = items.join("");
    if (!items.length) {
      // 空数组（如 cron.config.jobs = []）本身是一条可编辑的叶子键；
      // 这里只说明「本页不加元素」，不把它算成未识别键。
      const leaf = S.sites.get(S.root + "." + col.key);
      if (leaf) {
        S.covered.add(leaf.id);
        body = `<div class="empty-item">当前是空数组 <code>${esc(leaf.raw)}</code>；本页不提供新增条目，请用「原文」页签添加。</div>`;
      } else {
        body = `<div class="empty-item">这段里没有 ${esc(col.title || col.key)} 的任何条目（本页不提供新增）。</div>`;
      }
    }

    return `<section class="card"><div class="card-h"><h3>${esc(col.title || col.key)}</h3>
      <span class="hint">${esc(col.help || "")}</span><span class="count">${items.length}</span></div>
      <div class="card-b">${body}</div></section>`;
  }

  function itemHTML(bucket, col, title, sub, collapsible) {
    let inner = "";
    if (col.kind === "map-of-scalar") {
      const site = bucket.fields[""];
      inner = `<div class="field" data-kid="${esc(site.id)}">
        <div class="field-label"><span class="lab">正文</span></div>
        <div class="field-ctl">${textareaHTML(
          site.id,
          site.value,
          S.readonly || !site.editable,
          6,
        )}<div class="field-err hidden"></div></div></div>`;
      S.covered.add(site.id);
    } else {
      for (const f of col.fields || []) {
        const site = bucket.fields[f.key];
        if (!site) {
          // 该条目缺这个键：只提示，不提供新增
          inner += `<div class="field ro"><div class="field-label"><span class="lab">${esc(
            f.label || f.key,
          )}</span><code class="k">${esc(f.key)}</code></div>
            <div class="field-ctl"><div class="field-help">该条没有这个键（缺省值生效）</div></div></div>`;
          continue;
        }
        inner += renderField(f, site.id, site.value, site, S.readonly || !site.editable);
      }
    }
    return `<div class="item${collapsible ? " collapsed" : ""}"><div class="item-h"><span class="name">${esc(
      title,
    )}</span>${sub ? `<span class="sub">${esc(sub)}</span>` : ""}<span class="caret">▾</span></div>
      <div class="item-b">${inner}</div></div>`;
  }

  function renderRawHead() {
    const sc = SCHEMAS[S.root] || { title: S.root };
    const sec = S.sections.find((s) => s.root === S.root && s.run === S.run) || {};
    $("raw-head").innerHTML = `<div class="htext">
        <h2>${esc(sc.title || S.root)} · 原文</h2>
        <div class="desc">整段文本编辑：增删键、增删数组元素、改表头都只能用这里。</div>
        <div class="meta">段 <code>${esc(S.root)}</code> · 第 ${sec.start_line || "?"}–${sec.end_line || "?"} 行${
      S.readonly ? ` · <b>只读</b>：${esc(S.reason || "")}` : ""
    }</div></div>
      <div class="tabs"><button type="button" data-tab="form"${
        S.tab === "form" ? ' class="active"' : ""
      }>表单</button><button type="button" data-tab="raw"${
        S.tab === "raw" ? ' class="active"' : ""
      }>原文</button></div>`;
    $("raw-head").querySelectorAll(".tabs button").forEach((b) => {
      b.onclick = () => switchTab(b.dataset.tab);
    });
  }

  // ---------- 事件 ----------

  function attachFormEvents(root) {
    root.querySelectorAll(".tabs button").forEach((b) => {
      b.onclick = () => switchTab(b.dataset.tab);
    });

    root.querySelectorAll(".item-h").forEach((h) => {
      h.onclick = () => h.parentElement.classList.toggle("collapsed");
    });

    root.querySelectorAll("[data-goraw]").forEach((b) => {
      b.onclick = () => {
        switchTab("raw", Number(b.dataset.goraw));
      };
    });

    // 标量控件：input 时只更新 work，不重渲染（否则丢焦点）
    root.querySelectorAll("[data-kid]").forEach((ctl) => {
      const t = ctl.dataset.ctype;
      if (!t || t === "tags" || t === "recipients") return;
      const id = ctl.dataset.kid;
      const handler = () => {
        let v;
        if (t === "bool") {
          v = ctl.checked;
          const lbl = ctl.parentElement.querySelector(".lbl");
          if (lbl) lbl.textContent = v ? "开" : "关";
        } else if (t === "int" || t === "number") {
          const n = Number(ctl.value);
          v = Number.isFinite(n) && ctl.value !== "" ? (t === "int" ? Math.trunc(n) : n) : null;
        } else {
          v = ctl.value;
        }
        S.work.set(id, v);
        markDirty(ctl);
        updateSavebar();
      };
      ctl.addEventListener(t === "bool" || t === "enum" ? "change" : "input", handler);
    });

    // 标签 / 接收者
    root.querySelectorAll(".tagbox").forEach((box) => attachTagbox(box));
  }

  function markDirty(ctl) {
    const id = ctl.dataset ? ctl.dataset.kid : "";
    const field = ctl.closest ? ctl.closest(".field") : null;
    ctl.classList.toggle("dirty", !same(S.work.get(id), S.base.get(id)));
    if (field) {
      field.querySelectorAll(".field-err").forEach((e) => e.classList.add("hidden"));
    }
  }

  function attachTagbox(box) {
    const field = box.closest(".field");
    const id = field.dataset.kid;
    const isRec = box.dataset.ctype === "recipients";
    const ro = box.dataset.ro === "1";
    const input = box.querySelector(".tag-in");
    const sug = box.querySelector(".sug");
    let highlight = -1;

    const refreshDirty = () => {
      const changed = !same(S.work.get(id), S.base.get(id));
      box.classList.toggle("dirty", changed);
    };
    const commit = (list) => {
      S.work.set(id, list.slice());
      rerenderTagbox(field, list, id, isRec, ro);
    };

    box.querySelectorAll("[data-tagrm]").forEach((b) => {
      b.onclick = (ev) => {
        ev.stopPropagation();
        if (ro) return;
        const list = (S.work.get(id) || []).slice();
        list.splice(Number(b.dataset.tagrm), 1);
        commit(list);
      };
    });

    if (ro || !input) return;

    const add = (v) => {
      v = String(v || "").trim();
      if (!v) return;
      const list = (S.work.get(id) || []).slice();
      if (list.indexOf(v) >= 0) {
        toast("已经在列表里了", "warn");
        return;
      }
      list.push(v);
      commit(list);
      input.value = "";
      if (sug) sug.classList.add("hidden");
    };

    input.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter") {
        ev.preventDefault();
        const rows = sug && !sug.classList.contains("hidden") ? [...sug.querySelectorAll(".sug-row")] : [];
        if (highlight >= 0 && rows[highlight]) add(rows[highlight].dataset.v);
        else add(input.value);
      } else if (ev.key === "ArrowDown" || ev.key === "ArrowUp") {
        const rows = sug ? [...sug.querySelectorAll(".sug-row")] : [];
        if (!rows.length) return;
        ev.preventDefault();
        highlight = ev.key === "ArrowDown"
          ? Math.min(rows.length - 1, highlight + 1)
          : Math.max(0, highlight - 1);
        rows.forEach((r, i) => r.classList.toggle("on", i === highlight));
      } else if (ev.key === "Escape") {
        if (sug) sug.classList.add("hidden");
      }
    });

    if (!sug) return;

    const renderSug = () => {
      const q = input.value.trim().toLowerCase();
      const idx = recipientIndex();
      const picked = new Set(S.work.get(id) || []);
      const rows = [];
      for (const info of idx.values()) {
        if (picked.has(info.id)) continue;
        const hay = (info.name + " " + info.id).toLowerCase();
        if (q && hay.indexOf(q) < 0) continue;
        rows.push(info);
        if (rows.length >= 40) break;
      }
      highlight = -1;
      if (!rows.length) {
        sug.innerHTML = `<div class="sug-row none">没有匹配（可直接回车添加自定义 ID）</div>`;
      } else {
        sug.innerHTML = rows
          .map(
            (r) => `<div class="sug-row" data-v="${esc(r.id)}"><span class="sname">${esc(r.name)}</span>
              <span class="skind">${esc(r.kind)}</span><span class="sid">${esc(r.id)}</span></div>`,
          )
          .join("");
      }
      sug.classList.remove("hidden");
      sug.querySelectorAll(".sug-row[data-v]").forEach((r) => {
        r.onmousedown = (ev) => {
          ev.preventDefault();
          add(r.dataset.v);
        };
      });
    };

    input.addEventListener("input", renderSug);
    input.addEventListener("focus", renderSug);
    input.addEventListener("blur", () => setTimeout(() => sug.classList.add("hidden"), 140));
    refreshDirty();
  }

  function rerenderTagbox(field, list, id, isRec, ro) {
    const old = field.querySelector(".tagbox");
    if (!old) return;
    const html = tagHTML(list, isRec ? "recipients" : "tags", ro, id);
    old.outerHTML = html;
    const nb = field.querySelector(".tagbox");
    if (nb) attachTagbox(nb);
    if (nb) nb.classList.toggle("dirty", !same(list, S.base.get(id)));
    updateSavebar();
  }

  // ---------- 页签 ----------

  async function switchTab(tab, line) {
    if (tab === "raw") {
      if (formDirty() && !S.rawLoaded) {
        if (!confirm("表单里有未保存改动，切到原文会丢掉它们。继续吗？")) return;
        resetWork();
      }
      S.tab = "raw";
      $("form-view").classList.add("hidden");
      $("raw-view").classList.remove("hidden");
      await ensureRaw();
      if (line) scrollRawTo(line);
    } else {
      S.tab = "form";
      $("raw-view").classList.add("hidden");
      $("form-view").classList.remove("hidden");
    }
    syncTabs();
    updateSavebar();
  }

  function syncTabs() {
    document.querySelectorAll(".tabs button").forEach((b) => {
      b.classList.toggle("active", b.dataset.tab === S.tab);
    });
  }

  function scrollRawTo(line) {
    const ta = $("raw-text");
    const sec = S.sections.find((s) => s.root === S.root && s.run === S.run) || {};
    const rel = Math.max(0, line - (sec.start_line || 1));
    const lines = ta.value.split("\n");
    let off = 0;
    for (let i = 0; i < Math.min(rel, lines.length); i++) off += lines[i].length + 1;
    ta.focus();
    ta.setSelectionRange(off, off + (lines[rel] || "").length);
    const lh = parseFloat(getComputedStyle(ta).lineHeight) || 20;
    ta.scrollTop = Math.max(0, rel * lh - ta.clientHeight / 3);
  }

  async function ensureRaw(force) {
    if (S.rawLoaded && !force) return;
    const q = `root=${encodeURIComponent(S.root)}&run=${S.run}`;
    const d = await api("GET", "/admin/plugin-config/section?" + q);
    if (!d.ok) throw new Error(d.error || "读取段失败");
    S.rawText = d.text || "";
    S.rawBase = S.rawText;
    S.rawEtag = d.etag || "";
    S.rawRO = !!d.readonly;
    S.rawLoaded = true;
    const ta = $("raw-text");
    ta.value = S.rawText;
    ta.disabled = S.rawRO;
    renderRawHead();
    $("raw-head").querySelectorAll(".tabs button").forEach((b) => {
      b.onclick = () => switchTab(b.dataset.tab);
    });
    ta.classList.remove("dirty");
  }

  // ---------- 脏检查 ----------

  function formDirty() {
    for (const [id, v] of S.work) if (!same(v, S.base.get(id))) return true;
    return false;
  }
  function rawDirty() {
    return S.rawLoaded && $("raw-text").value !== S.rawBase;
  }
  function dirtyCount() {
    let n = 0;
    for (const [id, v] of S.work) if (!same(v, S.base.get(id))) n++;
    return n;
  }
  function resetWork() {
    S.work = new Map(S.base);
  }

  function updateSavebar() {
    const btn = $("btn-save");
    const n = S.tab === "raw" ? (rawDirty() ? 1 : 0) : dirtyCount();
    const readOnly = S.tab === "raw" ? S.rawRO || S.readonly : S.readonly;
    $("sb-plugin").textContent = S.root || "—";
    const dEl = $("sb-dirty");
    if (readOnly) {
      dEl.textContent = "只读";
      dEl.className = "sb-dirty";
    } else if (n) {
      dEl.textContent = S.tab === "raw" ? "原文有改动" : `${n} 项改动未保存`;
      dEl.className = "sb-dirty on";
    } else {
      dEl.textContent = "无改动";
      dEl.className = "sb-dirty";
    }
    btn.disabled = readOnly || !n || S.saving;
    btn.textContent = S.saving ? "保存中…" : "保存";
    $("btn-discard").disabled = !n || S.saving;
    $("sb-note").textContent = S.path ? `写入 ${S.path}` : "";
    $("sb-note").title = S.path || "";
  }

  // ---------- 载入 ----------

  async function loadSections() {
    const d = await api("GET", "/admin/plugin-config");
    if (!d.ok) {
      banner(d.error || "读不到配置文件", "err");
      return;
    }
    S.path = d.path || "";
    $("cfg-path").textContent = d.path || "";
    $("cfg-path").title = d.path || "";
    // 自身段（hermes_bridge）不在本页出现：它有自己的管理页。
    // 已弃用的插件（DEPRECATED_PLUGINS，目前是旧版 hermes）一并剔除：
    // 它们仍在 config.toml 里，但列出来会引导用户去启用，不如彻底不展示。
    S.sections = (d.sections || []).filter((s) => !s.self && !DEPRECATED.has(s.root));
    if (!S.sections.length) {
      banner("配置文件里没有可编辑的插件段", "warn");
      return;
    }
    renderChips();
    // 列表里只展示 run===1 的段（同根多段不连续时是只读的），默认选中也要对齐
    const ordered = orderSections(S.sections);
    const keep = S.sections.find((s) => s.root === S.root && s.run === S.run);
    const first = keep || ordered.find((s) => s.run === 1) || ordered[0];
    await selectPlugin(first.root, first.run);
  }

  function orderSections(list) {
    const rank = new Map(PLUGIN_ORDER.map((n, i) => [n, i]));
    return list.slice().sort((a, b) => {
      const ra = rank.has(a.root) ? rank.get(a.root) : 999;
      const rb = rank.has(b.root) ? rank.get(b.root) : 999;
      if (ra !== rb) return ra - rb;
      return a.root < b.root ? -1 : a.root > b.root ? 1 : 0;
    });
  }

  // 左侧插件列表：只列可编辑的段；弃用插件已在 loadSections 里剔除。
  //
  // 版式是两行：第一行「圆点 + 名字 + 版本」，第二行小字状态
  // （白名单/黑名单 · 已停用 · 缺 exe · 只读）。单行塞不下——
  // 实测 232px 侧栏里加两个标签就会把 reread 的名字挤到只剩一个字母。
  function renderChips() {
    const nav = $("chips");
    const q = (($("plugin-q") && $("plugin-q").value) || "").trim().toLowerCase();
    // 重绘会把左栏 scrollTop 清零，先存后还；否则点一个靠下的插件、
    // 或保存后刷新状态，列表就跳回顶部。
    const keepScroll = nav.scrollTop;
    nav.innerHTML = "";
    let shown = 0;
    for (const s of orderSections(S.sections)) {
      if (s.run !== 1) continue; // 同根多段不连续（只读），只列第一段作提示
      const sc = SCHEMAS[s.root] || {};
      const label = sc.title ? sc.title.split(" · ")[0] : s.root;
      if (q && !(label + " " + s.root).toLowerCase().includes(q)) continue;
      if (!matchFilter(s)) continue;
      shown++;
      const noCfg = sc.groups && sc.groups.length === 0 && !(sc.collections && sc.collections.length);
      const b = document.createElement("button");
      b.type = "button";
      applyChipState(b, s, { label, version: sc.version, noCfg });
      b.onclick = () => selectPlugin(s.root, s.run);
      b.dataset.root = s.root;
      b.dataset.run = String(s.run);
      nav.appendChild(b);
    }
    renderFilterCounts();
    nav.scrollTop = keepScroll;
    if (!shown) {
      const p = document.createElement("div");
      p.className = "chip-empty";
      p.textContent =
        q || S.filterState !== "all" || S.filterMode !== "all"
          ? "没有匹配的插件"
          : "没有可编辑的插件段";
      nav.appendChild(p);
    }
  }

  // 两组筛选各自独立、结果取交集：
  //   filterState：全部 / 启用 / 停用 / 缺 exe
  //   filterMode ：全部 / 黑名单 / 白名单
  //
  // 「启用」就是配置里 enable = true，跟有没有 exe 无关（缺 exe 的插件照样可以是
  // 启用状态，只是加载不起来）——两者是正交维度，所以启用 15 + 停用 2 = 17，
  // 而缺 exe 4 与它们重叠。计数必须和筛选用**同一个判断**，否则会出现
  // 「按钮写 11、点开 15 条」这种自相矛盾；不要去改「启用」的定义来凑数。
  //
  // 段里没写 enable 时按启用算（host 会沿用插件 main() 里的默认值，通常是 true），
  // 与第二行状态里「没写就不标停用」保持一致。
  function isEnabled(s) {
    return s.enable_known ? !!s.enable : true;
  }

  function modeOf(s) {
    if (!s.mode_known) return "";
    if (s.mode === "whitelist") return "white";
    if (s.mode === "blacklist") return "black";
    return "";
  }

  function matchFilter(s) {
    if (S.filterState === "on" && !isEnabled(s)) return false;
    if (S.filterState === "off" && isEnabled(s)) return false;
    if (S.filterState === "miss" && s.has_exe) return false;
    if (S.filterMode !== "all" && modeOf(s) !== S.filterMode) return false;
    return true;
  }

  // 筛选按钮上的计数：让「有几个停用 / 几个白名单」不用点进去才知道。
  // 计数是「在本组另一维筛选的结果里，这一项会有几条」，所以两组叠加时数字依然对得上。
  function renderFilterCounts() {
    const rows = S.sections.filter((s) => s.run === 1);
    const byMode = rows.filter((s) =>
      S.filterMode === "all" ? true : modeOf(s) === S.filterMode,
    );
    const byState = rows.filter((s) =>
      S.filterState === "all" ? true : matchFilter(s),
    );
    const counts = {
      state: {
        all: byMode.length,
        on: byMode.filter(isEnabled).length,
        off: byMode.filter((s) => !isEnabled(s)).length,
        miss: byMode.filter((s) => !s.has_exe).length,
      },
      mode: {
        all: byState.length,
        black: byState.filter((s) => modeOf(s) === "black").length,
        white: byState.filter((s) => modeOf(s) === "white").length,
      },
    };

    const box = $("chips-filter");
    if (!box) return;
    for (const b of box.querySelectorAll(".fchip")) {
      const group = b.dataset.group === "mode" ? "mode" : "state";
      const k = b.dataset.filter;
      const cur = group === "mode" ? S.filterMode : S.filterState;
      const n = counts[group][k] === undefined ? 0 : counts[group][k];
      b.classList.toggle("active", k === cur);
      b.setAttribute("aria-pressed", k === cur ? "true" : "false");
      b.innerHTML = `${b.dataset.label || (b.dataset.label = b.textContent)} <span class="n">${n}</span>`;
      b.classList.toggle("zero", n === 0);
    }
  }

  // 画一个 chip 的状态部分。单独抽出来是为了「保存 enable 之后原地刷新」，
  // 不必整份重建列表（重建会把左栏 scrollTop 清零）。
  //
  // 两行：第一行 圆点 + 名字 + 版本；第二行 小字状态。
  // 圆点三态：有 exe 且启用=绿；没 exe=黄；配置里 enable=false=空心灰。
  // 颜色只是辅助，状态一律写字，不让人靠颜色猜。
  function applyChipState(b, s, opt) {
    const o = opt || {};
    const label = o.label || s.root;
    const disabled = s.enable_known && !s.enable;
    const dotCls = !s.has_exe ? " miss" : disabled ? " off" : "";
    const active = s.root === S.root && Number(s.run) === S.run;
    b.className = "chip" + (active ? " active" : "") + (disabled ? " is-off" : "");
    if (active) b.setAttribute("aria-current", "true");
    else b.removeAttribute("aria-current");
    const exeName = `golem_plugin_${s.root}.exe`;
    b.title = !s.has_exe
      ? `没找到 ${exeName}`
      : disabled
        ? `${exeName} · 配置里 enable = false，host 不向它派发消息`
        : `${exeName} · 配置里 enable = true`;

    // 第二行状态片段。模式先说（它决定谁能用），再叠加异常状态。
    const bits = [];
    if (s.mode_known) {
      const wl = s.mode === "whitelist";
      bits.push(
        `<span class="st mode${wl ? " wl" : ""}">${wl ? "白名单" : "黑名单"}</span>`,
      );
      // whitelist + limits 为空 = 除主人外谁都用不了，这是最容易踩的配置
      if (wl && !s.limits) {
        bits.push(
          `<span class="st bad" title="白名单模式下 limits 为空：除机器人主人外，没有任何会话能调用它">仅主人</span>`,
        );
      }
    }
    if (!s.has_exe) bits.push(`<span class="st bad">缺 exe</span>`);
    if (disabled) bits.push(`<span class="st off">已停用</span>`);
    if (s.readonly) bits.push(`<span class="st">只读</span>`);
    if (o.noCfg) bits.push(`<span class="st">无 config</span>`);

    b.innerHTML = `<span class="dot${dotCls}"></span>
      <span class="nm">${esc(label)}</span>${
        o.version ? `<span class="v">${esc(o.version)}</span>` : ""
      }
      <span class="sub">${bits.join('<span class="sep">/</span>')}</span>`;
  }

  // 保存 enable 之后刷新左栏状态：只重取段清单并原地改 chip，不重建列表。
  // 保存 enable / mode 之后刷新左栏状态。
  //
  // 必须整表重绘而不是只原地改 chip：筛选结果与按钮计数都依赖这些字段，
  // 开了「停用」筛选却刚把某人启用时，原地改 chip 会让它留在错误的筛选里。
  // renderChips 内部已保存 / 恢复 scrollTop，重绘不会让列表跳回顶部。
  async function refreshSectionStates() {
    try {
      const d = await api("GET", "/admin/plugin-config");
      if (!d.ok) return;
      const byKey = new Map(
        (d.sections || []).map((x) => [x.root + "#" + x.run, x]),
      );
      for (const s of S.sections) {
        const fresh = byKey.get(s.root + "#" + s.run);
        if (!fresh) continue;
        s.enable = fresh.enable;
        s.enable_known = fresh.enable_known;
        s.has_exe = fresh.has_exe;
        s.mode = fresh.mode;
        s.mode_known = fresh.mode_known;
        s.limits = fresh.limits;
      }
      renderChips();
    } catch {
      // 状态刷新失败不影响本次保存的结果
    }
  }

  // 只更新选中态，不重建列表：重建会把左栏 scrollTop 清零。
  function markChips() {
    const nav = $("chips");
    if (!nav) return;
    for (const b of nav.children) {
      if (!b.dataset || b.dataset.root === undefined) continue;
      const on = b.dataset.root === S.root && Number(b.dataset.run) === S.run;
      b.classList.toggle("active", on);
      if (on) b.setAttribute("aria-current", "true");
      else b.removeAttribute("aria-current");
    }
  }

  async function selectPlugin(root, run) {
    if ((root !== S.root || run !== S.run) && (formDirty() || rawDirty())) {
      if (!confirm("当前插件有未保存改动，切换会丢掉它们。继续吗？")) return;
    }
    S.root = root;
    S.run = run;
    S.rawLoaded = false;
    hideBanner();
    $("raw-view").classList.add("hidden");
    $("form-view").classList.remove("hidden");
    S.tab = "form";
    // 右侧内容换插件后从头开始；左侧列表的滚动位置保持不变，
    // 也不重绘列表——只挪一下选中态，避免长列表被拉回顶部。
    $("main").scrollTop = 0;
    $("form-body").innerHTML = `<div class="card"><div class="card-b"><div class="empty-item">载入中…</div></div></div>`;
    markChips();
    try {
      await loadKeys();
    } catch (e) {
      $("form-body").innerHTML = `<div class="card"><div class="card-b"><div class="empty-item">${esc(
        e.message,
      )}</div></div></div>`;
      banner("载入「" + root + "」失败：" + e.message, "err", "重试", () => selectPlugin(root, run));
      return;
    }
    renderForm();
    $("main").scrollTop = 0;
  }

  async function loadKeys() {
    const q = `root=${encodeURIComponent(S.root)}&run=${S.run}`;
    const d = await api("GET", "/admin/plugin-config/keys?" + q);
    if (!d.ok) throw new Error(d.error || "读取失败");
    S.keys = d.keys || [];
    S.tables = d.tables || [];
    S.etag = d.etag || "";
    S.readonly = !!d.readonly;
    S.reason = d.reason || "";
    S.path = d.path || S.path;
    S.sites = new Map();
    S.base = new Map();
    for (const k of S.keys) {
      S.sites.set(k.id, k);
      S.base.set(k.id, k.value);
    }
    S.work = new Map(S.base);
    S.covered = new Set();
    // 接收者选择器要用群/好友名；取不到就退化成裸 ID，不阻塞表单。
    loadRecipients().catch(() => {});
    updateSavebar();
  }

  // ---------- 保存 ----------

  async function save() {
    if (S.tab === "raw") return saveRaw();
    const edits = [];
    for (const [id, v] of S.work) {
      if (same(v, S.base.get(id))) continue;
      const site = S.sites.get(id);
      if (!site) continue;
      if (!site.editable) {
        toast(`「${id}」不可编辑：${site.reason || "该键只读"}`, "err");
        return;
      }
      if (!validateOne(id, v)) return;
      edits.push({ id, expect_raw: site.raw, value: v });
    }
    if (!edits.length) {
      toast("没有改动");
      return;
    }
    S.saving = true;
    updateSavebar();
    try {
      await putKeys(edits);
    } catch (e) {
      if (e.data && e.data.code === "comment_loss") {
        if (confirm(`${e.message}\n\n确认继续会丢掉该值里的注释，仍然保存吗？`)) {
          const retry = edits.map((x) => (x.id === e.data.id ? Object.assign({}, x, { allow_comment_loss: true }) : x));
          try {
            await putKeys(retry);
            return;
          } catch (e2) {
            handleSaveError(e2);
            return;
          }
        }
      } else {
        handleSaveError(e);
      }
    } finally {
      S.saving = false;
      updateSavebar();
    }
  }

  async function putKeys(edits) {
    const q = `root=${encodeURIComponent(S.root)}&run=${S.run}`;
    const d = await api("PUT", "/admin/plugin-config/keys?" + q, { etag: S.etag, edits });
    const warn = (d.warnings || []).filter(Boolean);
    toast(`已保存 ${d.count} 项${d.backup ? "，备份 " + d.backup : ""}`, warn.length ? "warn" : "");
    if (warn.length) banner(warn.join("；"), "warn");
    else hideBanner();
    await loadKeys();
    renderForm();
    refreshSectionStates();
  }

  function handleSaveError(e) {
    const code = (e.data && e.data.code) || "";
    if (code === "etag_mismatch") {
      banner("配置文件已被其它途径修改（host /pm、手动编辑或另一个页面）。重新载入后才能保存。", "err", "重新载入", () => selectPlugin(S.root, S.run));
      return;
    }
    if (code === "key_changed") {
      const id = e.data.id;
      const field = $("form-body").querySelector(`.field[data-kid="${cssEscape(id)}"]`);
      if (field) {
        const err = field.querySelector(".field-err");
        if (err) {
          err.textContent = "这个键已被外部修改（当前值：" + (e.data.current_raw || "?") + "），请重新载入";
          err.classList.remove("hidden");
        }
      }
      banner(e.message || "键已被外部修改", "err", "重新载入", () => selectPlugin(S.root, S.run));
      return;
    }
    banner(e.message || "保存失败", "err");
    toast(e.message || "保存失败", "err");
  }

  function cssEscape(s) {
    return String(s).replace(/["\\]/g, "\\$&");
  }

  function validateOne(id, v) {
    const site = S.sites.get(id);
    const kind = site ? site.kind : "";
    const field = findFieldDef(id);
    if (v === null || v === undefined) {
      showFieldErr(id, "不能为空");
      return false;
    }
    if (kind === "integer" && !Number.isInteger(v)) {
      showFieldErr(id, "必须是整数");
      return false;
    }
    if (field) {
      if (typeof v === "number") {
        if (field.min != null && v < field.min) {
          showFieldErr(id, `不能小于 ${field.min}`);
          return false;
        }
        if (field.max != null && v > field.max) {
          showFieldErr(id, `不能大于 ${field.max}`);
          return false;
        }
      }
      if (field.type === "tags" || field.type === "recipients") {
        if (!Array.isArray(v)) {
          showFieldErr(id, "必须是列表");
          return false;
        }
        if (field.type === "recipients" && v.some((x) => !String(x).trim())) {
          showFieldErr(id, "列表里有空项");
          return false;
        }
      }
    }
    return true;
  }

  function showFieldErr(id, msg) {
    const field = $("form-body").querySelector(`.field[data-kid="${cssEscape(id)}"]`);
    if (!field) return;
    const err = field.querySelector(".field-err");
    if (err) {
      err.textContent = msg;
      err.classList.remove("hidden");
    }
    field.scrollIntoView({ block: "center", behavior: "smooth" });
  }

  // 在 schema 里反查字段定义（校验用）
  function findFieldDef(id) {
    const rel = id.indexOf(S.root + ".") === 0 ? id.slice(S.root.length + 1) : id;
    for (const f of COMMON) if (f.key === rel) return f;
    const sc = SCHEMAS[S.root];
    if (!sc) return null;
    for (const g of sc.groups || []) for (const f of g.fields || []) if (f.key === rel) return f;
    for (const c of sc.collections || []) {
      if (c.kind === "map-of-scalar") continue;
      for (const f of c.fields || []) if (rel.endsWith("." + f.key)) return f;
    }
    return null;
  }

  async function saveRaw() {
    const ta = $("raw-text");
    const text = ta.value;
    if (text === S.rawBase) {
      toast("没有改动");
      return;
    }
    S.saving = true;
    updateSavebar();
    try {
      const q = `root=${encodeURIComponent(S.root)}&run=${S.run}`;
      const d = await api("PUT", "/admin/plugin-config/section?" + q, { etag: S.rawEtag || S.etag, text });
      toast(`已保存整段（${d.bytes} 字节）`, (d.warnings || []).length ? "warn" : "");
      if ((d.warnings || []).length) banner(d.warnings.join("；"), "warn");
      else hideBanner();
      await loadKeys();
      await ensureRaw(true);
      renderForm();
      refreshSectionStates();
      if (S.tab === "raw") {
        $("form-view").classList.add("hidden");
        $("raw-view").classList.remove("hidden");
      }
      updateSavebar();
    } catch (e) {
      handleSaveError(e);
    } finally {
      S.saving = false;
      updateSavebar();
    }
  }

  // ---------- 群与联系人 ----------

  async function loadRecipients(force) {
    if (S.recipients && !force) return S.recipients;
    const d = await api("GET", "/admin/data/recipients");
    if (!d.ok) throw new Error(d.error || "读不到群/联系人缓存");
    S.recipients = d;
    S.recipientsAt = Date.now();
    return d;
  }

  async function openGroups() {
    $("groups").classList.remove("hidden");
    const list = $("glist");
    list.innerHTML = `<div class="gsec">载入中…</div>`;
    $("gdetail").innerHTML = "";
    try {
      await loadRecipients();
    } catch (e) {
      list.innerHTML = `<div class="gsec">${esc(e.message)}</div>`;
      return;
    }
    renderGroupList();
    const first = (S.recipients.groups || [])[0];
    if (first) openGroup(first.id);
    const f = (S.recipients.files && S.recipients.files.chatroom) || {};
    const c = (S.recipients.files && S.recipients.files.contact) || {};
    $("groups-meta").textContent = `缓存快照 · chatroom.json ${fmtClock(f.mtime)} / contact.json ${fmtClock(
      c.mtime,
    )} · 只读`;
  }

  function renderGroupList() {
    const q = ($("gsearch").value || "").trim().toLowerCase();
    const list = $("glist");
    const groups = (S.recipients.groups || []).filter(
      (g) => !q || (g.name + " " + g.id).toLowerCase().indexOf(q) >= 0,
    );
    const friends = (S.recipients.contacts || []).filter(
      (c) => !q || ((c.remark || "") + " " + (c.nickname || "") + " " + c.username).toLowerCase().indexOf(q) >= 0,
    );
    let html = `<div class="gsec">群聊 · ${groups.length}</div>`;
    html += groups.length
      ? groups
          .map(
            (g) => `<div class="grow${g.id === S.groupID ? " active" : ""}" data-gid="${esc(g.id)}">
              <span class="gname">${esc(g.name || g.id)}</span><span class="gnum">${g.member_count || 0} 人</span></div>`,
          )
          .join("")
      : `<div class="grow"><span class="gname">没有匹配的群</span></div>`;
    html += `<div class="gsec">好友 · ${friends.length}</div>`;
    html += friends.length
      ? friends
          .map(
            (c) => `<div class="grow${c.username === S.groupID ? " active" : ""}" data-gid="${esc(c.username)}">
              <span class="gname">${esc(c.remark || c.nickname || c.alias || c.username)}</span>
              <span class="gnum">${esc(c.alias || "好友")}</span></div>`,
          )
          .join("")
      : `<div class="grow"><span class="gname">没有匹配的好友</span></div>`;
    list.innerHTML = html;
    list.querySelectorAll("[data-gid]").forEach((r) => {
      r.onclick = () => openGroup(r.dataset.gid);
    });
  }

  async function openGroup(id) {
    S.groupID = id;
    $("glist").querySelectorAll(".grow").forEach((r) => r.classList.toggle("active", r.dataset.gid === id));
    const box = $("gdetail");
    const g = (S.recipients.groups || []).find((x) => x.id === id);
    const c = (S.recipients.contacts || []).find((x) => x.username === id);

    if (!g && c) {
      box.innerHTML = `<div class="gd-head"><h3>${esc(c.remark || c.nickname || c.alias || id)}</h3>
        <div class="gid">${esc(id)}</div></div>
        <div class="gd-stats"><span class="gd-stat">类型 <b>好友</b></span>
        ${c.alias ? `<span class="gd-stat">微信号 <b>${esc(c.alias)}</b></span>` : ""}
        ${c.nickname ? `<span class="gd-stat">昵称 <b>${esc(c.nickname)}</b></span>` : ""}</div>`;
      return;
    }

    box.innerHTML = `<div class="gd-head"><h3>${esc(g.name || id)}</h3><div class="gid">${esc(id)}</div></div>
      <div class="gd-stats">
        <span class="gd-stat">成员 <b>${g.member_count || 0}</b></span>
        ${g.owner_name ? `<span class="gd-stat">群主 <b>${esc(g.owner_name)}</b></span>` : ""}
        ${g.owner ? `<span class="gd-stat">群主 wxid <b>${esc(g.owner)}</b></span>` : ""}
        <span class="gd-stat">成员明细 <b>${g.in_chatroom_cache ? "有" : "无（只出现在联系人缓存）"}</b></span>
      </div><div class="member-list"><div class="gd-empty">载入成员…</div></div>`;

    try {
      const d = await api("GET", "/admin/data/chatrooms/" + encodeURIComponent(id));
      if (!d.ok) throw new Error(d.error || "读取失败");
      const members = d.members || [];
      const listEl = box.querySelector(".member-list");
      if (!members.length) {
        listEl.innerHTML = `<div class="gd-empty">没有成员明细（该群不在 chatroom.json 里）。</div>`;
        return;
      }
      const q = ($("gsearch").value || "").trim().toLowerCase();
      const rows = members.filter(
        (m) =>
          !q ||
          ((m.remark || "") + " " + (m.display_name || "") + " " + (m.nickname || "") + " " + m.username)
            .toLowerCase()
            .indexOf(q) >= 0,
      );
      listEl.innerHTML = rows
        .map(
          (m) => `<div class="member">
            <span class="mname">${esc(m.remark || m.display_name || m.nickname || m.username)}</span>
            ${m.is_owner ? `<span class="mbadge owner">群主</span>` : ""}
            ${m.in_contact_cache ? `<span class="mbadge">好友</span>` : ""}
            <span class="mright">${esc(m.alias ? "@" + m.alias : "")}${
            m.city || m.province ? " " + esc([m.province, m.city].filter(Boolean).join(" ")) : ""
          }</span>
          <span class="mmeta">${esc(m.username)}</span></div>`,
        )
        .join("");
    } catch (e) {
      const listEl = box.querySelector(".member-list");
      if (listEl) listEl.innerHTML = `<div class="gd-empty">${esc(e.message)}</div>`;
    }
  }

  // ---------- 启动 ----------

  function showLogin(msg) {
    $("app").classList.add("hidden");
    $("login").classList.remove("hidden");
    $("login-token").value = "";
    const err = $("login-error");
    if (msg) {
      err.textContent = msg;
      err.classList.remove("hidden");
    } else {
      err.classList.add("hidden");
    }
    setTimeout(() => $("login-token").focus(), 50);
    fillListen();
  }

  async function fillListen() {
    try {
      const res = await fetch("/admin/meta");
      if (!res.ok) return;
      const meta = await res.json();
      const addr = (meta && meta.admin_listen) || "";
      if (!addr) return;
      const local = /^(127\.0\.0\.1|localhost|\[::1\]):/.test(addr);
      $("login-listen").textContent = (local ? "本机管理台 · " : "管理台 · ") + addr;
    } catch (_) {
      /* 保留默认文案 */
    }
  }

  function showApp() {
    $("login").classList.add("hidden");
    $("app").classList.remove("hidden");
  }

  async function boot() {
    const tok = getToken();
    if (!tok) {
      showLogin();
      return;
    }
    try {
      await loadSections();
      showApp();
    } catch (e) {
      if (e.status === 401) {
        setToken("");
        showLogin("token 无效或已变更");
      } else {
        showLogin(e.message || "无法连接管理台");
      }
    }
  }

  $("login-form").addEventListener("submit", async (ev) => {
    ev.preventDefault();
    const t = $("login-token").value.trim();
    if (!t) return;
    setToken(t);
    $("btn-login").disabled = true;
    try {
      await loadSections();
      showApp();
      toast("已登录");
    } catch (e) {
      setToken("");
      showLogin(e.status === 401 ? "admin_token 不正确" : e.message || "无法连接管理台");
    } finally {
      $("btn-login").disabled = false;
    }
  });

  $("btn-logout").addEventListener("click", () => {
    setToken("");
    S.root = "";
    showLogin();
  });

  $("btn-save").addEventListener("click", save);
  if ($("plugin-q")) {
    $("plugin-q").addEventListener("input", renderChips);
    $("plugin-q").addEventListener("keydown", (ev) => {
      // 回车直接跳到第一个匹配项
      if (ev.key !== "Enter") return;
      ev.preventDefault();
      const first = $("chips").querySelector(".chip");
      if (first) first.click();
    });
  }
  // 筛选行：两组各自单选，结果取交集。再点一次当前项 = 该组回到「全部」。
  if ($("chips-filter")) {
    $("chips-filter").addEventListener("click", (ev) => {
      const btn = ev.target.closest(".fchip");
      if (!btn) return;
      const group = btn.dataset.group === "mode" ? "mode" : "state";
      const key = group === "mode" ? "filterMode" : "filterState";
      const next = btn.dataset.filter || "all";
      S[key] = next === S[key] && next !== "all" ? "all" : next;
      renderChips();
    });
  }
  $("btn-discard").addEventListener("click", () => {
    if (S.tab === "raw") {
      $("raw-text").value = S.rawBase;
      $("raw-text").classList.remove("dirty");
    } else {
      resetWork();
      renderForm();
    }
    toast("已放弃改动");
    updateSavebar();
  });

  $("raw-text").addEventListener("input", () => {
    $("raw-text").classList.toggle("dirty", $("raw-text").value !== S.rawBase);
    updateSavebar();
  });

  $("banner-close").addEventListener("click", hideBanner);
  $("btn-groups").addEventListener("click", openGroups);
  $("groups-close").addEventListener("click", () => $("groups").classList.add("hidden"));
  $("gsearch").addEventListener("input", () => {
    renderGroupList();
    if (S.groupID) openGroup(S.groupID);
  });

  document.addEventListener("keydown", (ev) => {
    if (ev.key === "Escape" && !$("groups").classList.contains("hidden")) {
      $("groups").classList.add("hidden");
    }
    if ((ev.ctrlKey || ev.metaKey) && ev.key === "s") {
      ev.preventDefault();
      if (!$("btn-save").disabled) save();
    }
  });

  window.addEventListener("beforeunload", (ev) => {
    if (formDirty() || rawDirty()) {
      ev.preventDefault();
      ev.returnValue = "";
    }
  });

  boot();
})();
