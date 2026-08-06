/* Dify2API SPA — shared helpers, state, routing, pagination, batch
 * selection, and bulletin board display.  Loaded after i18n.js.
 * All UI strings live in the i18n dictionary (zh + en). */
"use strict";

/* ---------------- helpers ---------------- */
const $ = (sel) => document.querySelector(sel);
const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const fmtT = (ts) => (ts ? new Date(ts * 1000).toLocaleString() : "—");
const fmtDate = (ts) => (ts ? new Date(ts * 1000).toLocaleDateString() : "—");
const fmtLocalDT = (ts) => {
  if (!ts) return "";
  const d = new Date(ts * 1000);
  const pad = (n) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
};
/* True on phone/tablet widths. Used by inline-style grids in admin.js/user.js
 * (CSS @media cannot override inline grid-template-columns). Forms re-render
 * on tab switch / dialog open, so the value is fresh at render time. */
const isNarrowScreen = () => window.innerWidth <= 768;

async function api(path, opts = {}) {
  const res = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    credentials: "same-origin",
    ...opts,
    body: opts.body ? JSON.stringify(opts.body) : undefined,
  });
  let data = null;
  try { data = await res.json(); } catch { /* ignore */ }
  if (!res.ok) {
    const msg = data?.error?.message || `${res.status}`;
    const e = new Error(msg);
    e.status = res.status;
    throw e;
  }
  return data;
}

function toast(msg, ms = 2200) {
  const t = $("#toast");
  t.textContent = msg;
  t.style.opacity = "1";
  setTimeout(() => (t.style.opacity = "0"), ms);
}

/* ---------------- state & init ---------------- */
const state = { site: null, me: null, mode: "user" };

/* ---------------- theme ---------------- */
(function initTheme() {
  const theme = localStorage.getItem("d2a-theme");
  applyTheme(theme);
})();

function getTheme() {
  return localStorage.getItem("d2a-theme"); // null="auto", "light", "dark"
}

function applyTheme(theme) {
  if (theme === "light" || theme === "dark") {
    document.documentElement.setAttribute("data-theme", theme);
  } else {
    document.documentElement.removeAttribute("data-theme");
  }
}

function cycleTheme() {
  const cur = getTheme();
  let next;
  if (!cur) next = "light";
  else if (cur === "light") next = "dark";
  else next = null; // dark → auto
  if (next) {
    localStorage.setItem("d2a-theme", next);
  } else {
    localStorage.removeItem("d2a-theme");
  }
  applyTheme(next);
  updateThemeButton();
  if (typeof onThemeChanged === "function") onThemeChanged();
}

function themeIcon(theme) {
  if (theme === "light") return "☀";
  if (theme === "dark") return "🌙";
  return "🖥";
}

function themeTitle(theme) {
  if (theme === "light") return T("themeLight");
  if (theme === "dark") return T("themeDark");
  return T("themeAuto");
}

function updateThemeButton() {
  const btn = $("#theme-switch");
  if (!btn) return;
  const theme = getTheme();
  btn.textContent = themeIcon(theme);
  btn.title = themeTitle(theme);
}

async function init() {
  try {
    state.site = await api("/api/site-info");
  } catch {
    state.site = { site_host: "", admin_host: "", site_name: "Dify2API", report_email: "", site_base_url: "" };
  }
  // Apply site name to the nav logo and document title.
  const siteName = (state.site["site_name_" + currentLang]) || state.site.site_name || "Dify2API";
  if (siteName) {
    document.title = siteName;
    const logo = $("#nav-logo");
    if (logo) logo.textContent = siteName;
  }
  updateThemeButton();
  state.mode = location.hostname === state.site.admin_host ? "admin" : "user";
  try {
    state.me = await api("/api/me");
    syncLangFromServer();
  } catch {
    state.me = null;
  }
  // Bind language switcher
  const langBtn = $("#lang-switch");
  if (langBtn) {
    langBtn.textContent = T('langSwitch');
    langBtn.onclick = switchLang;
  }
  const themeBtn = $("#theme-switch");
  if (themeBtn) {
    themeBtn.onclick = cycleTheme;
  }
  applyFooterLang();
  route();
}

// common.js is loaded before user.js/admin.js. Wait until all parser-blocking
// scripts have executed before the async bootstrap can call their renderers;
// otherwise a fast cached /api response can make route() win the load race.
function bootApp() {
  init().catch((err) => {
    console.error("Dify2API frontend initialization failed", err);
    const app = $("#app");
    if (app) app.textContent = "页面加载失败，请刷新重试。";
  });
}
if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", bootApp, { once: true });
} else {
  bootApp();
}

// Footer legal links are plain HTML shared by both sites (index.html);
// update their labels to follow the current UI language.
function applyFooterLang() {
  const p = $("#footer-privacy");
  if (p) p.textContent = T('footerPrivacy');
  const t = $("#footer-terms");
  if (t) t.textContent = T('footerTerms');
}

function route() {
  if (state.mode === "admin") {
    state.me?.is_admin ? renderAdminDashboard() : renderAdminLogin();
  } else {
    if (!state.me) renderUserLogin();
    else if (state.me.is_admin) renderAdminNotice();
    else renderUserDashboard();
  }
}

function bindLogout(sel) {
  const btn = $(sel);
  if (btn) btn.onclick = async () => {
    // Clean up debug session before logout.
    closeDebugSSE();
    try { await api("/api/me/debug/stop", { method: "POST" }); } catch (_) {}
    await api("/api/auth/logout", { method: "POST" }).catch(() => {});
    state.me = null;
    route();
  };
}

/* ---------------- pagination (shared) ---------------- */
function newPager(rowFn) {
  return { data: [], page: 1, size: 10, rowFn, afterRender: null };
}
function renderPaged(p, rowsSel, ctrlsSel, emptyCols) {
  const total = p.data.length;
  const pages = p.size === Infinity ? 1 : Math.max(1, Math.ceil(total / p.size));
  p.page = Math.min(Math.max(1, p.page), pages);
  const start = p.size === Infinity ? 0 : (p.page - 1) * p.size;
  const items = p.size === Infinity ? p.data : p.data.slice(start, start + p.size);
  $(rowsSel).innerHTML = items.length ? items.map(p.rowFn).join("") : `<tr><td colspan="${emptyCols}" class="muted">${T('empty')}</td></tr>`;
  $(ctrlsSel).innerHTML = `
    <select class="pg-size">
      ${[5, 10, 20, 50].map((n) => `<option value="${n}" ${p.size === n ? "selected" : ""}>${n} ${T('paginationPerPage')}</option>`).join("")}
      <option value="inf" ${p.size === Infinity ? "selected" : ""}>${T('paginationAll')}</option>
    </select>
    <button class="pg-prev secondary" ${p.page <= 1 ? "disabled" : ""}>‹</button>
    <span class="muted">${T('paginationInfo').replace('{page}', String(p.page)).replace('{pages}', String(pages)).replace('{total}', String(total))}</span>
    <button class="pg-next secondary" ${p.page >= pages ? "disabled" : ""}>›</button>`;
  const c = $(ctrlsSel);
  c.querySelector(".pg-size").onchange = (e) => {
    p.size = e.target.value === "inf" ? Infinity : parseInt(e.target.value, 10);
    p.page = 1;
    renderPaged(p, rowsSel, ctrlsSel, emptyCols);
  };
  c.querySelector(".pg-prev").onclick = () => { p.page--; renderPaged(p, rowsSel, ctrlsSel, emptyCols); };
  c.querySelector(".pg-next").onclick = () => { p.page++; renderPaged(p, rowsSel, ctrlsSel, emptyCols); };
  // afterRender runs exactly once per render (initial, page change, size
  // change). Re-binding inside it must therefore be idempotent-safe: row
  // elements are recreated by the innerHTML writes above, and
  // bindBatchSelectAll guards its listeners (see below).
  if (p.afterRender) p.afterRender();
}

// Server-paginated lists (admin request logs, alerts) cap the "全部" mode at
// this many rows. Callers must surface the truncation in the UI — see
// truncatedListNote below — never silently hide it.
const MAX_SERVER_ROWS = 10000;

// HTML snippet (or empty string) shown next to a server-paginated pager when
// "全部" is selected and the total exceeds the server-side row cap.
function truncatedListNote(total, size) {
  return size === Infinity && total > MAX_SERVER_ROWS
    ? `<span class="muted trunc-note">${T('listTruncated').replace("{n}", String(MAX_SERVER_ROWS))}</span>`
    : "";
}

/* ---------------- batch selection helpers ---------------- */
// Wire select-all toggle for a table. onSelectionChange is called after any checkbox change.
function bindBatchSelectAll(selectAllSel, chkSel, onSelectionChange) {
  const selectAll = document.querySelector(selectAllSel);
  if (!selectAll) return;
  selectAll.onclick = () => {
    document.querySelectorAll(chkSel).forEach(c => { c.checked = selectAll.checked; });
    if (onSelectionChange) onSelectionChange();
  };
  // Guard per element: afterRender may re-run on the same freshly rendered
  // rows (e.g. duplicated invocation paths), and addEventListener would
  // accumulate duplicate change listeners on the checkboxes.
  document.querySelectorAll(chkSel).forEach(c => {
    if (c._batchBound) return;
    c._batchBound = true;
    c.addEventListener("change", () => {
      if (!c.checked) {
        const sa = document.querySelector(selectAllSel);
        if (sa) sa.checked = false;
      }
      if (onSelectionChange) onSelectionChange();
    });
  });
}

// Get IDs from checked batch row checkboxes.
function getBatchIds(chkSel) {
  return Array.from(document.querySelectorAll(chkSel + ":checked")).map(c => parseInt(c.dataset.id, 10));
}

// Get pricing pairs (service/model) from checked batch row checkboxes.
function getBatchPairs(chkSel) {
  return Array.from(document.querySelectorAll(chkSel + ":checked")).map(c => ({
    service: c.dataset.service,
    model: c.dataset.model,
  }));
}

// Show/hide a batch action bar based on current selection.
function refBatchBar(batchBarId, chkSel) {
  const bar = document.getElementById(batchBarId);
  if (!bar) return;
  const chks = document.querySelectorAll(chkSel + ":checked");
  bar.style.display = chks.length > 0 ? "flex" : "none";
}

// Clear all batch checkboxes and hide bar (used after page changes or batch completion).
function clearBatchSelection(selectAllSel, chkSel, batchBarId) {
  const sa = document.querySelector(selectAllSel);
  if (sa) sa.checked = false;
  document.querySelectorAll(chkSel).forEach(c => { c.checked = false; });
  const bar = document.getElementById(batchBarId);
  if (bar) bar.style.display = "none";
}

/* ---------------- bulletin board ---------------- */

// State for user bulletin board.
const bulletinPager = newPager(bulletinRow);
let _bulletinDismissed = null; // Set of dismissed bulletin IDs (from localStorage).

function _dismissedSet() {
  if (_bulletinDismissed !== null) return _bulletinDismissed;
  try {
    _bulletinDismissed = new Set(JSON.parse(localStorage.getItem("d2a_dismissed_bulletins") || "[]"));
  } catch {
    _bulletinDismissed = new Set();
  }
  return _bulletinDismissed;
}

function dismissBulletin(id) {
  const s = _dismissedSet();
  s.add(id);
  localStorage.setItem("d2a_dismissed_bulletins", JSON.stringify([...s]));
  loadBulletins();
}

// Type badge colour for bulletin types.
function bulletinTypeBadge(t) {
  switch (t) {
    case "warning": return `<span class="badge warn">${T('bulletinTypeWarning')}</span>`;
    case "important": return `<span class="badge err">${T('bulletinTypeImportant')}</span>`;
    default: return `<span class="badge ok">${T('bulletinTypeInfo')}</span>`;
  }
}

// Left border colour class for bulletin card.
function bulletinBorderClass(t) {
  switch (t) {
    case "warning": return "bull-warn";
    case "important": return "bull-imp";
    default: return "bull-info";
  }
}

function bulletinRow(b) {
  const preview = (b.content || "").replace(/<[^>]*>/g, "").substring(0, 100);
  const closable = b.closable && !b.is_system;
  const dismissed = _dismissedSet().has(b.id);
  if (dismissed) return ""; // skip dismissed

  return `
    <article class="bulletin-item ${bulletinBorderClass(b.type)}" data-id="${b.id}" style="cursor:pointer">
      <div style="display:flex;justify-content:space-between;align-items:flex-start;gap:.5rem">
        <div style="flex:1" class="bull-click">
          <h4 style="margin:0 0 .25rem">${esc(b.title)} ${bulletinTypeBadge(b.type)}</h4>
          <p class="muted" style="margin:0">${esc(preview)}${preview.length >= 100 ? "…" : ""}</p>
          <small class="muted">${fmtT(b.created_at)}</small>
        </div>
        ${closable ? `<button class="bull-dismiss secondary outline" style="padding:.2rem .5rem;font-size:.8rem;flex-shrink:0">✕</button>` : ""}
      </div>
    </article>`;
}

function showBulletinDialog(b) {
  let dialog = $("#bulletin-dialog");
  if (!dialog) {
    dialog = document.createElement("dialog");
    dialog.id = "bulletin-dialog";
    document.body.appendChild(dialog);
  }
  dialog.innerHTML = `
    <article>
      <header>
        <h3>${esc(b.title)} ${bulletinTypeBadge(b.type)}</h3>
      </header>
      <div class="bulletin-content" style="max-height:60vh;overflow-y:auto">${b.content || ""}</div>
      <footer style="margin-top:1rem;text-align:right">
        <button class="secondary bull-dlg-close">${T('bulletinClose')}</button>
      </footer>
    </article>`;
  dialog.showModal();
  dialog.querySelector(".bull-dlg-close").onclick = () => dialog.close();
  dialog.addEventListener("click", (e) => { if (e.target === dialog) dialog.close(); });
}

async function loadBulletins() {
  try {
    const data = await api("/api/bulletins");
    const list = data.bulletins || [];
    // Filter out dismissed bulletins.
    const ds = _dismissedSet();
    bulletinPager.data = list.filter((b) => !ds.has(b.id));
    bulletinPager.size = 5; // 5 per page for bulletin cards
    renderPaged(bulletinPager, "#bulletin-rows", "#bulletin-pager", 1);
    // Bind click handlers: click on card body opens dialog.
    document.querySelectorAll(".bull-click").forEach((el) => {
      el.onclick = () => {
        const id = parseInt(el.closest(".bulletin-item").dataset.id, 10);
        const b = list.find((x) => x.id === id);
        if (b) showBulletinDialog(b);
      };
    });
    // Bind dismiss buttons.
    document.querySelectorAll(".bull-dismiss").forEach((btn) => {
      btn.onclick = (e) => {
        e.stopPropagation();
        const id = parseInt(btn.closest(".bulletin-item").dataset.id, 10);
        dismissBulletin(id);
      };
    });
  } catch {
    // Graceful: bulletins might not be available yet.
  }
}

// Renders the bulletin board above tabbed content on the user dashboard.
function renderBulletinBoard() {
  const container = $("#bulletin-board");
  if (!container) return;
  container.innerHTML = `
    <h3>${T('bulletinTitle')}</h3>
    <div id="bulletin-rows"></div>
    <div class="row-actions" id="bulletin-pager" style="margin-top:.5rem"></div>`;
  loadBulletins();
}

// ---- click-to-copy ID badge (shared by admin.js) ----
function idBadge(id) {
  return `<span class="id-badge mono" data-copy-id="${id}" title="${T('clickToCopy')}: ${id}" style="font-size:.8em;cursor:pointer;background:var(--pico-muted-border-color);padding:0 .25rem;border-radius:3px">#${id}</span>`;
}

function bindIdBadgeClicks() {
  document.querySelectorAll(".id-badge").forEach((el) => {
    if (el._bound) return;
    el._bound = true;
    el.onclick = async () => {
      try {
        await navigator.clipboard.writeText(el.dataset.copyId);
        toast(T('copied'));
      } catch {
        toast(T('copyFail'));
      }
    };
  });
}
