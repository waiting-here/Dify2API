"use strict";

/* ---------------- key management ---------------- */
async function copyKey() {
  let key;
  try {
    const resp = await api("/api/caller-key");
    key = resp.key;
    if (!key) {
      const r = await api("/api/caller-key/reset", { method: "POST" });
      key = r.key;
    }
  } catch (err) {
    toast(T('error').replace("{msg}", err.message), 3000);
    return;
  }

  // Try Clipboard API first, fall back to execCommand for non-HTTPS contexts.
  if (navigator.clipboard && navigator.clipboard.writeText) {
    try {
      await navigator.clipboard.writeText(key);
      toast(T('copied'));
      return;
    } catch { /* Clipboard API rejected — try fallback */ }
  }

  // Fallback: use a temporary textarea (works on HTTP and older browsers).
  const ta = document.createElement("textarea");
  ta.value = key;
  ta.style.position = "fixed";
  ta.style.opacity = "0";
  document.body.appendChild(ta);
  ta.select();
  try {
    document.execCommand("copy");
    toast(T('copied'));
  } catch {
    toast(T('copyFail'), 3200);
  }
  document.body.removeChild(ta);
}

/* ---------------- user site: login ---------------- */
function renderUserLogin() {
  $("#nav-user").textContent = "";
  $("#app").innerHTML = `
    <article class="card" style="max-width:28rem;margin:4rem auto;text-align:center">
      <h3>${T('userLoginTitle')} · ${esc(state.site.site_name || T('siteName'))}</h3>
      <p class="muted">${T('userLoginHint')}</p>
      <a role="button" href="/auth/discord/login">${T('loginWithDiscord')}</a>
    </article>`;
}

function renderAdminNotice() {
  $("#nav-user").textContent = state.me.username;
  $("#app").innerHTML = `
    <article class="card" style="max-width:32rem;margin:4rem auto;text-align:center">
      <p>${T('adminNotice')}</p>
      <button id="logout">${T('logout')}</button>
    </article>`;
  bindLogout("#logout");
}

/* ---------------- user site: dashboard ---------------- */
async function renderUserDashboard() {
  $("#nav-user").innerHTML = `${esc(T('welcome').replace("{name}", state.me.username))} · <a href="#" id="logout">${T('logout')}</a>`;
  bindLogout("#logout");

  const tabs = ["configs", "credits", "charity", "logs", "debug"];
  const tabLabels = {
    configs: T('userTabConfigs'), credits: T('userTabCredits'), charity: T('userTabCharity'),
    logs: T('userTabLogs'), debug: T('userTabDebug'),
  };
  const tabNav = tabs.map((t, i) =>
    `<button class="user-tab${i === 0 ? " active" : ""}" data-tab="${t}">${tabLabels[t]}</button>`
  ).join("");

  $("#app").innerHTML = `
    <!-- Above-tab area: bulletin, key, data cards (always visible) -->
    <section class="card" id="bulletin-board"></section>
    <div class="user-top-cards">
      <section class="card" id="key-card">
        <h3>${T('keyTitle')}</h3>
        <p class="muted">${T('keyHint')}</p>
        <div class="row-actions">
          <span class="mono badge off">d2a_•••••••••••••••</span>
          <button id="copy-key" class="secondary">${T('copy')}</button>
          <button id="reset-key" class="contrast outline">${T('resetKey')}</button>
        </div>
      </section>
      <section class="card" id="data-card">
        <h3>${T('dataManagement')}</h3>
        <div class="row-actions">
          <button id="export-data" class="secondary">${T('exportData')}</button>
          <button id="delete-account" class="contrast outline">${T('deleteAccount')}</button>
        </div>
      </section>
    </div>

    <!-- Tab navigation -->
    <nav class="tab-nav">${tabNav}</nav>

    <!-- Configs tab (default active) -->
    <div id="utab-configs" class="user-tab-content">
      <section class="card">
        <h3>${T('configsTitle')}</h3>
        <div id="check-note"></div>
        <div class="table-wrap"><table><thead><tr><th>${T('thModel')}</th><th>${T('thNote')}</th><th>${T('thEnabled')}</th><th>${T('thActions')}</th></tr></thead><tbody id="cfg-rows"></tbody></table></div>
        <div class="row-actions" id="cfg-pager" style="margin:.5rem 0 1rem"></div>
        <form id="cfg-form">
          <div style="display:grid;grid-template-columns:${isNarrowScreen()?'1fr':'auto 1fr'};gap:.5rem;align-items:end">
            <label>${T('thService')}<select name="service" id="cfg-service"></select></label>
            <label>${T('thModel')}<input name="backend" placeholder="${T('fieldBackend')}${T('fieldBackendHint')}" required></label>
          </div>
          <label>${T('thBaseURL')}<input name="dify_base_url" placeholder="${T('fieldBaseURL')}" required></label>
          <label>API Key<input name="dify_api_key" placeholder="${T('fieldAPIKey')}" required></label>
          <label>${T('thNote')}<input name="note" placeholder="${T('fieldNote')}"></label>
          <button type="submit" id="cfg-submit">${T('addConfig')}</button>
        </form>
      </section>
    </div>

    <!-- Credits tab -->
    <div id="utab-credits" class="user-tab-content" style="display:none">
      <section class="card" id="credits-card">
        <h3>${T('creditsTitle')}</h3>
        <div id="credits-info"><p class="muted">${T('loading')}</p></div>
        <div class="row-actions" style="margin-top:.5rem">
          <button id="checkin-btn" class="secondary">${T('creditsCheckin')}</button>
        </div>
      </section>
    </div>

    <!-- Charity tab -->
    <div id="utab-charity" class="user-tab-content" style="display:none">
      <section class="card" id="charity-card"></section>
      <section class="card" id="my-donations-section">
        <h3>${T('myDonations')}</h3>
        <div id="my-donations-content"></div>
      </section>
    </div>

    <!-- Logs tab -->
    <div id="utab-logs" class="user-tab-content" style="display:none">
      <section class="card">
        <h3>${T('logsTitle')}</h3>
        <div class="table-wrap"><table><thead><tr><th>${T('thTime')}</th><th>${T('thDuration')}</th><th>${T('thModel')}</th><th>${T('thStatus')}</th><th>${T('thErrorCode')}</th><th>${T('thErrorDetail')}</th><th>${T('thCreditsConsumed')}</th><th>${T('thAntiAbuse')}</th></tr></thead><tbody id="log-rows"></tbody></table></div>
        <div class="row-actions" id="log-pager" style="margin-top:.5rem"></div>
      </section>
    </div>

    <!-- Debug tab -->
    <div id="utab-debug" class="user-tab-content" style="display:none">
      <section class="card" id="debug-section">
        <h3>${T('userTabDebug')}</h3>
        <p class="muted">${T('debugComingSoon')}</p>
      </section>
    </div>`;

  // Bind tab switching.
  document.querySelectorAll(".user-tab").forEach((btn) => {
    btn.onclick = () => switchUserTab(btn.dataset.tab);
  });

  // Reset tab lazy-load state for fresh render.
  for (const k of Object.keys(_userTabLoaded)) delete _userTabLoaded[k];
  _userTabLoaded.configs = true;

  // Bind key-card events.
  $("#copy-key").onclick = copyKey;
  $("#reset-key").onclick = async () => {
    if (!confirm(T('resetKeyConfirm'))) return;
    await api("/api/caller-key/reset", { method: "POST" });
    toast(T('keyResetDone'));
  };
  $("#export-data").onclick = async () => {
    // Trigger a file download via a temporary link.
    try {
      const resp = await fetch("/api/me/export", { credentials: "same-origin" });
      if (!resp.ok) {
        const data = await resp.json().catch(() => ({}));
        throw new Error(data?.error?.message || `HTTP ${resp.status}`);
      }
      const blob = await resp.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = resp.headers.get("Content-Disposition")?.match(/filename="?([^"]+)"?/)?.[1] || "export.json";
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
      toast(T('exportDone'));
    } catch (err) {
      toast(T('error').replace("{msg}", err.message), 4000);
    }
  };
  $("#delete-account").onclick = async () => {
    if (!confirm(T('deleteAccountWarn1'))) return;
    const input = prompt(T('deleteAccountWarn2'));
    if (input !== T('deleteAccountConfirm')) {
      if (input !== null) toast(T('deleteAccountFailed'), 3000);
      return;
    }
    try {
      const data = await api("/api/me?confirm=DELETE", { method: "DELETE" });
      toast(data.message || T('deleteAccountDone'), 5000);
      state.me = null;
      route();
    } catch (err) {
      toast(T('error').replace("{msg}", err.message), 4000);
    }
  };

  // Render bulletin board (above tabs, always visible).
  renderBulletinBoard();

  // Load first tab (configs, always pre-loaded).
  await initUserConfigsTab();
}

/* ---------------- user site: tab switching ---------------- */
const _userTabLoaded = {};

function switchUserTab(tab) {
  document.querySelectorAll(".user-tab").forEach((b) => b.classList.remove("active"));
  document.querySelectorAll(".user-tab-content").forEach((c) => (c.style.display = "none"));
  const btn = document.querySelector(`.user-tab[data-tab="${tab}"]`);
  if (btn) btn.classList.add("active");
  const content = $(`#utab-${tab}`);
  if (content) content.style.display = "";
  // Lazy load on first activation.
  if (!_userTabLoaded[tab]) {
    _userTabLoaded[tab] = true;
    switch (tab) {
      case "credits": renderCreditsCard(); break;
      case "charity": renderCharityCard(); renderMyDonations(); break;
      case "logs": initUserLogsTab(); break;
      case "debug": initUserDebugTab(); break;
    }
  }
}

async function initUserConfigsTab() {
  $("#cfg-form").onsubmit = onConfigSubmit;
  const { services } = await api("/api/services");
  $("#cfg-service").innerHTML = services
    .map((s) => `<option value="${esc(s.name)}" title="${esc(s.label)}">${esc(s.name)}</option>`)
    .join("");
  await loadConfigs();
}

function initUserLogsTab() {
  loadLogs();
}

// ---- Debug tab state ----
let _debugActive = false;
let _debugDryRun = true;
let _debugEventSource = null;
let _debugEvents = [];
const _debugMaxEvents = 50;

function initUserDebugTab() {
  const section = $("#debug-section");
  if (!section) return;

  // Render immediately based on localStorage (avoids showing placeholder).
  renderDebugUI();

  // Then check server-side status asynchronously and update if needed.
  api("/api/me/debug/status").then((st) => {
    _debugActive = st.active;
    _debugDryRun = st.dry_run;
    renderDebugUI();
    if (_debugActive) connectDebugSSE();
  }).catch(() => {
    // Server unreachable — keep the immediate render as-is.
  });
}

function renderDebugUI() {
  const section = $("#debug-section");
  if (!section) return;

  const consented = _debugConsented();

  let html = `<h3>${T('userTabDebug')}</h3>`;

  if (!consented) {
    // Disclaimer.
    html += `
      <div class="debug-warning card" style="border-left:4px solid var(--pico-del-color, #c62828);margin-bottom:1rem">
        <pre style="white-space:pre-wrap;font-size:.85rem;margin:0">${esc(T('debugWarning'))}</pre>
        <button id="debug-consent-btn" class="secondary" style="margin-top:.75rem">${T('debugConsent')}</button>
      </div>`;
  } else if (_debugActive) {
    // Active debug: controls + event log.
    html += `
      <div class="row-actions" style="gap:.5rem;flex-wrap:wrap">
        <button id="debug-stop-btn" class="contrast">${T('debugStop')}</button>
        <label style="display:flex;align-items:center;gap:.4rem;white-space:nowrap">
          <input type="checkbox" id="debug-dryrun-toggle" role="switch" ${!_debugDryRun ? "checked" : ""}>
          ${T('debugDryRunLabel')}
        </label>
        <span class="badge ok">${T('debugConnected')}</span>
        <span class="muted" style="font-size:.85rem">${T('debugEventCount').replace("{n}", String(_debugEvents.length))}</span>
      </div>
      <p class="muted" style="font-size:.8rem;margin:.3rem 0">${_debugDryRun ? T('debugDryRunHintOn') : T('debugDryRunHintOff')}</p>
      <p class="muted" style="font-size:.8rem;margin:.3rem 0">${T('debugStreamNote')}</p>
      <div style="margin-top:.5rem">
        <button id="debug-collapse-all" class="secondary outline" style="font-size:.75rem">${T('debugCollapseAll')}</button>
        <button id="debug-expand-all" class="secondary outline" style="font-size:.75rem;margin-left:.25rem">${T('debugExpandAll')}</button>
      </div>
      <div id="debug-events" style="max-height:60vh;overflow-y:auto;margin-top:.5rem">
        ${renderDebugEvents()}
      </div>`;
  } else {
    // Consented but inactive.
    html += `
      <div class="debug-warning card" style="border-left:4px solid var(--pico-del-color, #c62828);margin-bottom:1rem">
        <pre style="white-space:pre-wrap;font-size:.85rem;margin:0">${esc(T('debugWarning'))}</pre>
      </div>
      <button id="debug-start-btn" class="secondary">${T('debugStart')}</button>`;
  }

  section.innerHTML = html;

  // Bind events.
  if (!consented) {
    $("#debug-consent-btn").onclick = () => {
      _debugSetConsented(true);
      renderDebugUI();
    };
  } else if (_debugActive) {
    $("#debug-stop-btn").onclick = stopDebug;
    const dryToggle = $("#debug-dryrun-toggle");
    if (dryToggle) {
      // The switch means "Send to Dify": checked = real forwarding,
      // unchecked = dry-run (the default).
      dryToggle.onchange = () => toggleDebugDryRun(!dryToggle.checked);
    }
    $("#debug-collapse-all").onclick = () => {
      document.querySelectorAll("#debug-events details[open]").forEach((d) => d.removeAttribute("open"));
    };
    $("#debug-expand-all").onclick = () => {
      document.querySelectorAll("#debug-events details").forEach((d) => d.setAttribute("open", ""));
    };
  } else {
    $("#debug-start-btn").onclick = () => {
      if (!confirm(T('debugStartConfirm'))) return;
      startDebug(true);
    };
  }
}

function renderDebugEvents() {
  if (_debugEvents.length === 0) {
    return `<p class="muted">${T('debugNoEvents')}</p>`;
  }
  return _debugEvents.map((evt, i) => renderOneDebugEvent(evt, i)).join("");
}

function renderOneDebugEvent(evt, idx) {
  // Terminal / lifecycle events (rendered as compact cards).
  if (evt.event === "replaced") {
    return `<div class="card" style="padding:.5rem 1rem;margin-bottom:.5rem;border-left:4px solid var(--pico-warn-color,#f9a825)">
      <strong>⚠ ${T('debugStreamReplaced')}</strong>
    </div>`;
  }
  if (evt.event === "dropped") {
    return `<div class="card" style="padding:.5rem 1rem;margin-bottom:.5rem;border-left:4px solid var(--pico-warn-color,#f9a825)">
      <strong>⚠ ${T('debugDropped')}</strong>
    </div>`;
  }
  if (evt.event === "idle_timeout" || evt.event === "session_expired" || evt.event === "no_attach_timeout") {
    const label = evt.event === "idle_timeout" ? T('debugIdleTimeout') :
                  evt.event === "session_expired" ? T('debugSessionExpired') : T('debugNoAttachTimeout');
    return `<div class="card" style="padding:.5rem 1rem;margin-bottom:.5rem;border-left:4px solid var(--pico-del-color, #c62828)">
      <strong>⏱ ${esc(label)}</strong>
    </div>`;
  }

  const ts = new Date(evt.timestamp * 1000).toLocaleTimeString();
  const req = evt.request || {};
  const resp = evt.response;
  const hasError = !!evt.error;

  const reqBodyStr = typeof req.body === "object" ? JSON.stringify(req.body, null, 2) : String(req.body ?? "");
  const inputsStr = evt.dify_inputs ? JSON.stringify(evt.dify_inputs, null, 2) : T('debugNone');
  const respBodyStr = resp ? (resp.body || "") : T('debugNone');

  const statusBadge = resp
    ? `<span class="badge ${resp.status < 400 ? "ok" : "err"}">${resp.status}</span>`
    : `<span class="badge off">dry-run</span>`;

  return `
    <details class="debug-event card" style="padding:.5rem 1rem;margin-bottom:.5rem;border-left:4px solid ${hasError ? "var(--pico-del-color, #c62828)" : "var(--pico-primary, #1095c1)"}">
      <summary style="cursor:pointer;display:flex;gap:.75rem;align-items:center">
        <strong>${esc(req.method || "?")} ${esc(req.path || "?")}</strong>
        <span class="muted" style="font-size:.85rem">${ts}</span>
        ${statusBadge}
        ${hasError ? `<span class="badge err">${esc(evt.error.substring(0, 40))}</span>` : ""}
      </summary>
      <div style="margin-top:.5rem">
        <p style="margin:0 0 .25rem"><strong>${T('debugRawBody')}:</strong>${req.truncated ? ` <span class="badge warn" title="${esc(T('debugTruncatedHint').replace('{size}', '64 KiB'))}">${esc(T('debugTruncated'))}</span>` : ""}</p>
        <pre style="max-height:16em;overflow:auto;font-size:.8rem;background:var(--pico-code-bg,#1a1a2e);color:var(--pico-code-color,#e0e0e0);padding:.5rem;border-radius:4px">${esc(reqBodyStr)}</pre>
        ${renderMessageLayout(evt.message_layout)}
        <p style="margin:.5rem 0 .25rem"><strong>${T('debugDifyInputsLabel')}:</strong>${evt.inputs_truncated ? ` <span class="badge warn" title="${esc(T('debugTruncatedHint').replace('{size}', '64 KiB'))}">${esc(T('debugTruncated'))}</span>` : ""}</p>
        <pre style="max-height:16em;overflow:auto;font-size:.8rem;background:var(--pico-code-bg,#1a1a2e);color:var(--pico-code-color,#e0e0e0);padding:.5rem;border-radius:4px">${esc(inputsStr)}</pre>
        <p style="margin:.5rem 0 .25rem"><strong>${T('debugResponseBodyLabel')}:</strong>${resp && resp.truncated ? ` <span class="badge warn" title="${esc(T('debugTruncatedHint').replace('{size}', '256 KiB'))}">${esc(T('debugTruncated'))}</span>` : ""}</p>
        <pre style="max-height:16em;overflow:auto;font-size:.8rem;background:var(--pico-code-bg,#1a1a2e);color:var(--pico-code-color,#e0e0e0);padding:.5rem;border-radius:4px">${esc(respBodyStr)}</pre>
      </div>
    </details>`;
}

function renderMessageLayout(layout) {
  if (!layout || !layout.length) return "";
  const rows = layout.map(function(s) {
    const content = s.content ? ` <span class="muted">${esc(s.content.substring(0, 60))}${s.content.length > 60 ? "…" : ""}</span>` : "";
    return `<tr><td class="mono muted">[${s.index}]</td><td class="mono">${esc(s.role)}</td><td style="font-size:.8rem">${content}</td></tr>`;
  }).join("");
  return `
    <p style="margin:.5rem 0 .25rem"><strong>${T('debugMessageLayout')}:</strong></p>
    <div class="table-wrap" style="margin-bottom:.5rem"><table style="font-size:.8rem"><thead><tr><th>#</th><th>${T('debugRole')}</th><th>${T('debugContentPreview')}</th></tr></thead><tbody>${rows}</tbody></table></div>`;
}

// ---- Debug actions ----

async function startDebug(dryRun) {
  try {
    await api("/api/me/debug/start", { method: "POST", body: { dry_run: dryRun } });
    _debugActive = true;
    _debugDryRun = dryRun;
    _debugEvents = [];
    renderDebugUI();
    connectDebugSSE();
  } catch (err) {
    toast(T('error').replace("{msg}", err.message), 3000);
  }
}

async function stopDebug() {
  closeDebugSSE();
  try {
    await api("/api/me/debug/stop", { method: "POST" });
  } catch (_) { /* ignore — may already be closed */ }
  _debugActive = false;
  _debugEvents = [];
  renderDebugUI();
}

async function toggleDebugDryRun(wantDryRun) {
  if (!wantDryRun) {
    // Turning dry-run OFF (the switch goes ON → real sending) — require
    // secondary confirmation.
    if (!confirm(T('debugDryRunOffConfirm'))) {
      // Revert toggle to unchecked (dry-run).
      const t = $("#debug-dryrun-toggle");
      if (t) t.checked = false;
      return;
    }
  }
  try {
    await api("/api/me/debug/dry-run", { method: "POST", body: { dry_run: wantDryRun } });
    _debugDryRun = wantDryRun;
    renderDebugUI();
  } catch (err) {
    toast(T('error').replace("{msg}", err.message), 3000);
    // Revert toggle.
    const t = $("#debug-dryrun-toggle");
    if (t) t.checked = !wantDryRun;
  }
}

// ---- SSE connection ----

function connectDebugSSE() {
  closeDebugSSE();

  const es = new EventSource("/api/me/debug/stream");
  _debugEventSource = es;

  es.addEventListener("connected", () => {
    // Stream is live.
  });

  es.addEventListener("request", (e) => {
    try {
      const evt = JSON.parse(e.data);
      // Handle terminal lifecycle events: close SSE and re-render.
      if (evt.event === "replaced" || evt.event === "idle_timeout" ||
          evt.event === "session_expired" || evt.event === "no_attach_timeout") {
        closeDebugSSE();
        _debugActive = false;
        var toastMsg = T('debugStreamDone');
        if (evt.event === "replaced") toastMsg = T('debugStreamReplaced');
        else if (evt.event === "idle_timeout") toastMsg = T('debugIdleTimeout');
        else if (evt.event === "session_expired") toastMsg = T('debugSessionExpired');
        else if (evt.event === "no_attach_timeout") toastMsg = T('debugNoAttachTimeout');
        toast(toastMsg, 5000);
        renderDebugUI();
        return;
      }
      _debugEvents.unshift(evt);
      if (_debugEvents.length > _debugMaxEvents) _debugEvents.length = _debugMaxEvents;
      // Update the event list if visible.
      const container = $("#debug-events");
      if (container) {
        container.innerHTML = renderDebugEvents();
      }
      // Update event count.
      const countSpan = document.querySelector("#debug-section .muted");
      if (countSpan && countSpan.textContent.includes(T('debugEventCount').split("{")[0])) {
        countSpan.textContent = T('debugEventCount').replace("{n}", String(_debugEvents.length));
      }
    } catch (_) { /* ignore malformed event */ }
  });

  es.addEventListener("done", () => {
    closeDebugSSE();
    _debugActive = false;
    renderDebugUI();
    toast(T('debugStreamDone'), 3000);
  });

  es.onerror = () => {
    // EventSource will auto-reconnect. Update badge.
    const badge = document.querySelector("#debug-section .badge.ok");
    if (badge) {
      badge.className = "badge warn";
      badge.textContent = T('debugDisconnected');
    }
  };
}

function closeDebugSSE() {
  if (_debugEventSource) {
    _debugEventSource.close();
    _debugEventSource = null;
  }
}

// ---- Disclaimer consent (localStorage) ----

function _debugConsented() {
  try { return localStorage.getItem("d2a_debug_consent") === "1"; } catch (_) { return false; }
}

function _debugSetConsented(v) {
  try { localStorage.setItem("d2a_debug_consent", v ? "1" : "0"); } catch (_) {}
}

// Timer handle for auto-restoring the check-in button at midnight.
let _checkinTimer = null;

async function renderCreditsCard() {
  const card = $("#credits-card");
  if (!card) return;
  try {
    // Fetch user credits and check-in status in parallel.
    const [me, status] = await Promise.all([
      api("/api/me"),
      api("/api/me/checkin/status"),
    ]);
    // Use i18n'd name if available, fall back to the single-value env var.
    const creditsName = (state.site["credits_name_" + currentLang]) || state.site.credits_name || T('creditsTitle');
    const logoText = state.site.credits_logo_text || "";
    const logoImg = `<img src="/credits-logo" alt="" style="height:2rem;vertical-align:middle;margin-right:.5rem" onerror="var t=this.parentElement.querySelector('.cr-text');if(t)t.style.display='';this.style.display='none'">`;
    $("#credits-info").innerHTML = `
      ${logoImg}<span class="cr-text" style="${logoText ? 'display:none' : ''};font-size:1.5rem;margin-right:.5rem">${esc(logoText)}</span>
      <strong>${esc(creditsName)}</strong>
      <span class="badge ok" style="margin-left:.75rem">${T('creditsBalance').replace("{n}", String(me.credits || 0))}</span>`;

    const btn = $("#checkin-btn");
    if (!btn) return;

    // Clear any existing restore timer.
    if (_checkinTimer) { clearTimeout(_checkinTimer); _checkinTimer = null; }

    // Determine button state from status endpoint.
    if (status.checked_in_today && status.next_checkin_at >= 9999999990) {
      // credits_cap == 0: check-in is globally disabled.
      btn.textContent = T('creditsCheckinDisabled');
      btn.disabled = true;
    } else if (status.checked_in_today) {
      // Already checked in today; auto-restore at next midnight.
      btn.textContent = T('creditsCheckedIn');
      btn.disabled = true;
      const waitMs = Math.max(0, (status.next_checkin_at - Math.floor(Date.now() / 1000)) * 1000);
      _checkinTimer = setTimeout(() => renderCreditsCard(), waitMs);
    } else {
      btn.textContent = T('creditsCheckin');
      btn.disabled = false;
    }

    btn.onclick = async () => {
      btn.disabled = true;
      btn.textContent = T('loading');
      try {
        const resp = await api("/api/me/checkin", { method: "POST" });
        toast(T('creditsCheckinDone')
          .replace("{name}", creditsName)
          .replace("{bonus}", String(resp.bonus))
          .replace("{total}", String(resp.credits)));
        // After successful check-in, set button to "checked in" + auto-restore.
        // Re-fetch status to get the correct next_checkin_at.
        const newStatus = await api("/api/me/checkin/status");
        btn.textContent = T('creditsCheckedIn');
        btn.disabled = true;
        if (newStatus.next_checkin_at && newStatus.next_checkin_at < 9999999990) {
          const waitMs = Math.max(0, (newStatus.next_checkin_at - Math.floor(Date.now() / 1000)) * 1000);
          if (_checkinTimer) clearTimeout(_checkinTimer);
          _checkinTimer = setTimeout(() => renderCreditsCard(), waitMs);
        }
        // Refresh balance display.
        const me2 = await api("/api/me");
        $("#credits-info").innerHTML = `
          ${logoImg}<span class="cr-text" style="${logoText ? 'display:none' : ''};font-size:1.5rem;margin-right:.5rem">${esc(logoText)}</span>
          <strong>${esc(creditsName)}</strong>
          <span class="badge ok" style="margin-left:.75rem">${T('creditsBalance').replace("{n}", String(me2.credits || 0))}</span>`;
      } catch (err) {
        if (err.message && err.message.includes("今日已签到")) {
          toast(T('creditsCheckedIn'));
          btn.textContent = T('creditsCheckedIn');
          btn.disabled = true;
        } else {
          toast(T('creditsCheckinFailed') + "：" + err.message, 3000);
          btn.disabled = false;
          btn.textContent = T('creditsCheckin');
        }
        return;
      }
    };
  } catch {
    $("#credits-info").innerHTML = `<p class="muted">${T('error').replace("{msg}", T('creditsLoadFailed'))}</p>`;
  }
}

async function renderCharityCard() {
  const card = $("#charity-card");
  if (!card) return;
  // When the global switch is off, show a persistent banner AND the
  // personal toggle (users can freely flip the switch, but every flip
  // shows an informational toast).
  const globalOff = !state.site.charity_enabled;
  let enabled = false;
  let pricingList = [];
  if (!globalOff) {
    try {
      const data = await api("/api/me/charity");
      enabled = data.charity_enabled;
      pricingList = data.pricing || [];
    } catch { /* use default false */ }
  }
  const statusText = enabled ? T('userCharityOn') : T('userCharityOff');
  // Build pricing table when enabled and pricing data exists.
  let pricingTableHtml = "";
  if (enabled && pricingList.length > 0) {
    pricingTableHtml = `
      <h4 style="margin-top:1.25rem">${T('pricingTitle')}</h4>
      <div class="table-wrap"><table><thead><tr>
        <th>${T('pricingThService')}</th><th>${T('pricingThModel')}</th><th>${T('pricingThPrice')}</th>
      </tr></thead><tbody>
        ${pricingList.map((p) => `<tr><td>${esc(p.service)}</td><td class="mono">${esc(p.model)}</td><td class="mono">${esc(String(p.price))}</td></tr>`).join("")}
      </tbody></table></div>`;
  }
  card.innerHTML = `
    ${globalOff ? `<article class="note warn" style="margin-bottom:.75rem">${T('userCharityBanner')}</article>` : ""}
    <h3>${T('userCharityToggle')}</h3>
    <p class="muted">${esc(statusText)}</p>
    <p class="muted">${T('userCharityContribution').replace('{n}', String(state.me.donation_credit || 0))}</p>
    ${enabled ? `<article class="note warn">${T('userCharityConfirm').replace(/\n\n[^\n]+$/, '')}</article>` : ""}
    <label style="display:flex;align-items:center;gap:.75rem">
      <input type="checkbox" id="charity-toggle" role="switch" ${enabled ? "checked" : ""}>
      <span>${T('userCharityToggle')}</span>
    </label>
    ${pricingTableHtml}`;
  const toggle = $("#charity-toggle");
  toggle.onchange = async () => {
    const wantOn = toggle.checked;
    // If the global switch is off, allow the toggle but show an
    // informational toast and revert the visual state.
    if (globalOff) {
      toggle.checked = !wantOn;
      toast(T('userCharityBanner'), 3000);
      return;
    }
    if (wantOn && !confirm(T('userCharityConfirm'))) {
      toggle.checked = false;
      return;
    }
    try {
      await api("/api/me/charity", {
        method: "PUT",
        body: { enabled: wantOn, confirmed: wantOn },
      });
      toast(wantOn ? T('userCharityOn') : T('userCharityOff'));
      renderCharityCard();
    } catch (err) {
      toggle.checked = !wantOn;
      toast(T('error').replace("{msg}", err.message), 3000);
    }
  };
}

async function renderMyDonations() {
  const container = $("#my-donations-content");
  if (!container) return;

  const donationEnabled = state.site.donation_enabled;

  // Fetch pending count and existing applications.
  let apps = [];
  let pendingCount = 0;
  try {
    const resp = await api("/api/me/donations");
    apps = resp.applications || [];
    pendingCount = apps.filter((a) => a.status === "pending").length;
  } catch { /* will show error below */ }

  const limit = state.site.donation_review_limit || 3;
  const canSubmit = donationEnabled && pendingCount < limit;

  let html = "";

  // Apply button / disabled notice.
  if (donationEnabled) {
    html += `<div style="margin-bottom:.75rem">`;
    if (canSubmit) {
      html += `<button id="donation-apply-btn" class="secondary">${T('donationApplyBtn')}</button>`;
    } else {
      html += `<button id="donation-apply-btn" class="secondary" disabled>${T('donationApplyBtn')}</button>`;
      html += ` <span class="muted" style="font-size:.85em">${T('donationApplyDisabled').replace("{n}", String(pendingCount)).replace("{limit}", String(limit))}</span>`;
    }
    html += `</div>`;

    // Donation description + hidden form.
    html += `<article class="note info" style="margin-bottom:1rem">${T('userDonationDescription')}</article>`;
    html += `<form id="donation-apply-form" style="display:none">
      <div style="display:grid;grid-template-columns:${isNarrowScreen()?'1fr':'auto 1fr'};gap:.5rem;align-items:end">
        <label>${T('thService')}<select name="service" id="don-apply-service"></select></label>
        <label>${T('thModel')}<input name="model" placeholder="${T('fieldBackend')}" required></label>
      </div>
      <label>${T('fieldBaseURL')}<input name="dify_base_url" placeholder="https://api.dify.ai/v1" required></label>
      <label>${T('fieldAPIKey')}<input name="dify_api_key" placeholder="app-…" required></label>
      <div style="display:grid;grid-template-columns:${isNarrowScreen()?'1fr':'1fr 1fr 1fr'};gap:.5rem">
        <label>${T('donationApplyDeadline')}<input name="deadline" type="datetime-local" required></label>
        <label>${T('donationApplyTotalCount')}<input name="total_count" type="number" min="1" value="100" required></label>
        <label>${T('rpmLimitLabel')}<input name="rpm_limit" type="number" min="1" value="10" placeholder="${T('rpmLimitUserHint')}"></label>
      </div>
      <label>${T('fieldNote')}<textarea name="note" rows="2"></textarea></label>
      <button type="submit">${T('donationApplySubmit')}</button>
      <span id="donation-apply-msg" class="muted" style="margin-left:.75rem"></span>
    </form>`;
  } else {
    html += `<article class="note warn">${T('userDonationBanner')}</article>`;
  }

  // Application status table.
  if (apps.length > 0) {
    html += `<div class="table-wrap"><table><thead><tr>
      <th>${T('donationAppThService')}</th><th>${T('donationAppThModel')}</th>
      <th>${T('donationAppThStatus')}</th><th>${T('donationAppThCreated')}</th>
      <th>${T('donationAppThNote')}</th><th>${T('adminReviewNote')}</th>
      <th>${T('donationAppThDonation')}</th>
    </tr></thead><tbody>`;
    for (const a of apps) {
      const statusBadge = a.status === "pending" ? `<span class="badge warn">${T('donationAppStatusPending')}</span>`
        : a.status === "approved" ? `<span class="badge ok">${T('donationAppStatusApproved')}</span>`
        : `<span class="badge err">${T('donationAppStatusRejected')}</span>`;
      let donationCell = "—";
      if (a.status === "approved") {
        if (a.donation_status === "inactive") donationCell = `<span class="muted">${T('donationAppDonationInactive')}</span>`;
        else if (a.donation_status === "active") donationCell = `<span class="badge ok">${T('donationAppDonationActive')}</span> ${a.donation_remaining}/${a.donation_total}`;
        else if (a.donation_status === "expired") donationCell = `<span class="badge off">${T('donationAppDonationExpired')}</span>`;
        if (a.donation_deadline) donationCell += ` <small class="muted">(${fmtT(a.donation_deadline)})</small>`;
      } else if (a.status === "rejected") {
        donationCell = `<button class="secondary donation-resubmit-btn" data-id="${a.id}" style="width:auto;margin:0">${T('donationResubmit')}</button>`;
      }
      const adminNote = a.review_note ? esc(a.review_note) : "—";
      html += `<tr>
        <td>${esc(a.service)}</td><td class="mono">${esc(a.model)}</td>
        <td>${statusBadge}</td><td class="muted">${fmtT(a.created_at)}</td>
        <td class="muted"><div class="wrap" style="max-width:50rem">${esc(a.note || "—")}</div></td>
        <td class="muted"><div class="wrap" style="max-width:50rem">${adminNote}</div></td>
        <td>${donationCell}</td>
      </tr>`;
    }
    html += `</tbody></table></div>`;
  } else {
    html += `<p class="muted">${T('empty')}</p>`;
  }

  container.innerHTML = html;

  // Bind apply button.
  const applyBtn = $("#donation-apply-btn");
  const form = $("#donation-apply-form");
  if (applyBtn && form && canSubmit) {
    applyBtn.onclick = () => { form.style.display = form.style.display === "none" ? "" : "none"; };

    // Populate service dropdown.
    try {
      const { services } = await api("/api/services");
      $("#don-apply-service").innerHTML = (services || []).map((s) => `<option value="${esc(s.name)}">${esc(s.name)}</option>`).join("");
    } catch { /* silently ignore */ }

    form.onsubmit = async (e) => {
      e.preventDefault();
      if (!confirm(T('userDonationConfirm'))) return;
      const f = e.target;
      const deadline = f.deadline.value ? Math.floor(new Date(f.deadline.value).getTime() / 1000) : 0;
      const msg = $("#donation-apply-msg");
      msg.textContent = T('loading');
      try {
        const rpmLimit = parseInt(f.rpm_limit.value, 10) || 0;
        const resp = await api("/api/me/donations", {
          method: "POST",
          body: {
            service: f.service.value,
            model: f.model.value.trim(),
            dify_base_url: f.dify_base_url.value.trim(),
            dify_api_key: f.dify_api_key.value.trim(),
            deadline,
            total_count: parseInt(f.total_count.value, 10),
            rpm_limit: rpmLimit,
            note: f.note.value.trim(),
          },
        });
        f.reset();
        if (resp.notice) {
          // Keep the form visible so the user can fix the address.
          msg.innerHTML = `<div class="note warn">⚠ ${esc(resp.notice)}</div>`;
          await renderMyDonations();
          return;
        }
        msg.textContent = "";
        toast(T('donationApplySubmitted'));
        form.style.display = "none";
        await renderMyDonations();
      } catch (err) {
        msg.textContent = T('error').replace("{msg}", err.message);
      }
    };
  }

  // Bind resubmit buttons for rejected applications.
  document.querySelectorAll(".donation-resubmit-btn").forEach((btn) => {
    btn.onclick = async () => {
      const id = parseInt(btn.dataset.id, 10);
      const a = apps.find((x) => x.id === id);
      if (!a) return;
      // Show the apply form.
      const form2 = $("#donation-apply-form");
      if (form2) {
        form2.style.display = "";
        // Populate service dropdown if not already populated.
        if (!$("#don-apply-service").children.length) {
          try {
            const { services } = await api("/api/services");
            $("#don-apply-service").innerHTML = (services || []).map((s) => `<option value="${esc(s.name)}">${esc(s.name)}</option>`).join("");
          } catch { /* silently ignore */ }
        }
        // Pre-fill fields from the rejected application.
        $("#don-apply-service").value = a.service || "";
        form2.querySelector("[name=model]").value = a.model || "";
        form2.querySelector("[name=dify_base_url]").value = a.dify_base_url || "";
        form2.querySelector("[name=deadline]").value = a.deadline ? fmtLocalDT(a.deadline) : "";
        form2.querySelector("[name=total_count]").value = a.total_count || "";
        form2.querySelector("[name=rpm_limit]").value = a.rpm_limit || "";
        form2.querySelector("[name=note]").value = a.note || "";
        // Do NOT pre-fill API key (security: only has_key is returned).
        form2.querySelector("[name=dify_api_key]").value = "";
        form2.querySelector("[name=dify_api_key]").placeholder = T('donationResubmitKeyHint');
      }
    };
  });
}

const cfgPager = newPager(cfgRow);
const logPager = newPager(logRow);

function cfgRow(c) {
  return `
    <tr data-id="${c.id}">
      <td class="mono">${esc(c.model)}</td>
      <td class="muted wrap">${esc(c.note || "—")}</td>
      <td><input type="checkbox" class="cfg-toggle" ${c.enabled ? "checked" : ""} role="switch"></td>
      <td><div class="row-actions">
        <button class="secondary cfg-edit">${T('editConfig')}</button>
        <button class="contrast outline cfg-del">${T('deleteConfig')}</button>
      </div></td>
    </tr>`;
}

async function loadConfigs() {
  const { configs } = await api("/api/configs");
  cfgPager.data = configs || [];
  renderPaged(cfgPager, "#cfg-rows", "#cfg-pager", 4);

  document.querySelectorAll(".cfg-toggle").forEach((cb) => (cb.onchange = async (e) => {
    const id = e.target.closest("tr").dataset.id;
    await api(`/api/configs/${id}/toggle`, { method: "POST", body: { enabled: e.target.checked } });
    toast(T('settingsSaved'));
  }));
  document.querySelectorAll(".cfg-del").forEach((b) => (b.onclick = async (e) => {
    if (!confirm(T('deleteConfirm'))) return;
    const id = e.target.closest("tr").dataset.id;
    await api(`/api/configs/${id}`, { method: "DELETE" });
    await loadConfigs();
  }));
  document.querySelectorAll(".cfg-edit").forEach((b) => (b.onclick = (e) => {
    const id = e.target.closest("tr").dataset.id;
    const c = cfgPager.data.find((x) => String(x.id) === id);
    if (!c) return;
    showConfigEditDialog(c);
  }));
}

async function showConfigEditDialog(c) {
  const old = $("#cfg-edit-dialog");
  if (old) old.remove();

  const dialog = document.createElement("dialog");
  dialog.id = "cfg-edit-dialog";
  document.body.appendChild(dialog);

  // Parse [service]backend format
  let svc = "", backend = c.model;
  const m = c.model.match(/^\[([^\]]+)\](.*)$/);
  if (m) { svc = m[1]; backend = m[2]; }

  // Populate services dropdown
  let svcOpts = "";
  try {
    const { services } = await api("/api/services");
    svcOpts = (services || []).map((s) => `<option value="${esc(s.name)}" ${s.name === svc ? "selected" : ""}>${esc(s.name)}</option>`).join("");
  } catch (_) { /* keep empty */ }

  dialog.innerHTML = `
    <article>
      <header><h3>${T('editConfig')}</h3></header>
      <form id="cfg-edit-form">
        <label>${T('thService')}<select name="service">${svcOpts}</select></label>
        <label>${T('thModel')}<input name="backend" value="${esc(backend)}" placeholder="${T('fieldBackend')}${T('fieldBackendHint')}" required></label>
        <label>${T('thBaseURL')}<input name="dify_base_url" value="${esc(c.dify_base_url)}" placeholder="${T('fieldBaseURL')}" required></label>
        <label>API Key<input name="dify_api_key" placeholder="${T('fieldAPIKey')}"></label>
        <label>${T('thNote')}<input name="note" value="${esc(c.note || '')}" placeholder="${T('fieldNote')}"></label>
        <div id="cfg-edit-msg" style="margin-bottom:.5rem"></div>
        <footer style="display:flex;gap:.5rem;justify-content:flex-end">
          <button type="button" id="cfg-edit-save">${T('save')}</button>
          <button type="button" id="cfg-edit-cancel">${T('cancelEdit')}</button>
        </footer>
      </form>
    </article>`;
  dialog.showModal();

  const close = () => { dialog.close(); dialog.remove(); };
  $("#cfg-edit-cancel").onclick = close;
  dialog.addEventListener("click", (e) => { if (e.target === dialog) close(); });

  $("#cfg-edit-save").onclick = async () => {
    const f = $("#cfg-edit-form");
    const body = {
      model: `[${f.service.value}]${f.backend.value.trim()}`,
      dify_base_url: f.dify_base_url.value,
      dify_api_key: f.dify_api_key.value,
      note: f.note.value,
    };
    const msg = $("#cfg-edit-msg");
    msg.innerHTML = `<p class="muted">${T('loading')}</p>`;
    try {
      const resp = await api(`/api/configs/${c.id}`, { method: "PUT", body });
      const check = resp.app_check || {};
      let cls = "ok", html = "";
      if (check.error) {
        cls = "warn"; html = `${T('checkError')}: ${esc(check.error)}`;
      } else if ("compatible" in check) {
        if (check.compatible) {
          html = `${T('checkCompatible')}`;
          if (check.extra_app_optional?.length) html += `<br><span class="muted">${T('checkExtra').replace("{list}", esc(check.extra_app_optional.join(", ")))}</span>`;
        } else {
          cls = "err";
          html = `${T('checkIncompatible')}`;
          if (check.missing_contract_vars?.length) html += `<br>${T('checkMissing').replace("{list}", esc(check.missing_contract_vars.join(", ")))}`;
          if (check.uncovered_app_required?.length) html += `<br>${T('checkUncovered').replace("{list}", esc(check.uncovered_app_required.join(", ")))}`;
        }
      } else {
        // Probe skipped: the binding (model / base URL / key) did not change.
        html = `${T('checkSkipped')}`;
      }
      msg.innerHTML = `<div class="note ${cls}">${html}</div>`;
      if (resp.notice) {
        // Keep the dialog open so the user reads the hint before closing.
        msg.innerHTML += `<div class="note warn" style="margin-top:.4rem">⚠ ${esc(resp.notice)}</div>`;
        await loadConfigs();
        return;
      }
      await loadConfigs();
      close();
    } catch (err) {
      msg.innerHTML = `<div class="note err">${T('error').replace("{msg}", esc(err.message))}</div>`;
    }
  };
}

async function onConfigSubmit(e) {
  e.preventDefault();
  const f = e.target;
  const body = {
    model: `[${f.service.value}]${f.backend.value.trim()}`,
    dify_base_url: f.dify_base_url.value,
    dify_api_key: f.dify_api_key.value,
    note: f.note.value,
  };
  const note = $("#check-note");
  note.innerHTML = `<p class="muted">${T('loading')}</p>`;
  try {
    const resp = await api("/api/configs", { method: "POST", body });
    f.reset();
    const c = resp.app_check || {};
    let cls = "ok", html = "";
    if (c.error) {
      cls = "warn"; html = `${T('checkError')}: ${esc(c.error)}`;
    } else if ("compatible" in c) {
      if (c.compatible) {
        html = `${T('checkCompatible')}`;
        if (c.extra_app_optional?.length) html += `<br><span class="muted">${T('checkExtra').replace("{list}", esc(c.extra_app_optional.join(", ")))}</span>`;
      } else {
        cls = "err";
        html = `${T('checkIncompatible')}`;
        if (c.missing_contract_vars?.length) html += `<br>${T('checkMissing').replace("{list}", esc(c.missing_contract_vars.join(", ")))}`;
        if (c.uncovered_app_required?.length) html += `<br>${T('checkUncovered').replace("{list}", esc(c.uncovered_app_required.join(", ")))}`;
      }
    } else {
      // Probe skipped: the binding (model / base URL / key) did not change.
      html = `${T('checkSkipped')}`;
    }
    note.innerHTML = `<div class="note ${cls}">${html}</div>`;
    if (resp.notice) {
      note.innerHTML += `<div class="note warn" style="margin-top:.4rem">⚠ ${esc(resp.notice)}</div>`;
    }
    await loadConfigs();
  } catch (err) {
    note.innerHTML = `<div class="note err">${T('error').replace("{msg}", esc(err.message))}</div>`;
  }
}

function logRow(l) {
  // Parse anti-abuse info into user-friendly text.
  let aaText = "";
  if (l.anti_abuse_info) {
    try {
      const obj = JSON.parse(l.anti_abuse_info);
      if (obj.penalties && obj.penalties.length > 0) {
        let deducted = 0, banned = 0;
        for (const p of obj.penalties) {
          const dm = p.match(/^credits_deducted:(\d+)$/);
          if (dm) deducted = parseInt(dm[1], 10);
          const bm = p.match(/^banned:(\d+)h$/);
          if (bm) banned = parseInt(bm[1], 10);
        }
        if (deducted > 0 && banned > 0) {
          aaText = T('antiAbusePenaltyFormat').replace('{credits}', String(deducted)).replace('{hours}', String(banned));
        } else if (deducted > 0) {
          aaText = T('antiAbusePenaltyDeduct').replace('{credits}', String(deducted));
        } else if (banned > 0) {
          aaText = T('antiAbusePenaltyBan').replace('{hours}', String(banned));
        }
      } else if (obj.triggered) {
        aaText = T('antiAbuseNone');
      }
    } catch { aaText = ""; }
  }
  return `
    <tr>
      <td class="muted">${fmtT(l.started_at)}</td>
      <td class="muted">${l.ended_at > l.started_at ? (l.ended_at - l.started_at) + "s" : "—"}</td>
      <td class="mono">${esc(l.model)}</td>
      <td><span class="badge ${l.status === "success" ? "ok" : "err"}">${esc(l.status)}</span></td>
      <td class="mono muted">${esc(l.error_code || "")}</td>
      <td class="muted wrap" style="max-width:48rem">${esc(l.error_detail || "")}</td>
      <td class="mono muted">${l.credits_consumed ? esc(String(l.credits_consumed)) : "0"}</td>
      <td class="muted">${esc(aaText)}</td>
    </tr>`;
}

async function loadLogs() {
  const { logs } = await api("/api/logs");
  logPager.data = logs || [];
  renderPaged(logPager, "#log-rows", "#log-pager", 8);
}

