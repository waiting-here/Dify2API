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
  stopFishingParticles();
  $("#nav-user").textContent = "";
  $("#app").innerHTML = `
    <article class="card login-card" style="max-width:28rem;margin:4rem auto">
      <span class="login-mark" aria-hidden="true"></span>
      <h3>${T('userLoginTitle')} · ${esc(state.site.site_name || T('siteName'))}</h3>
      <p class="muted">${T('userLoginHint')}</p>
      <a role="button" href="/auth/discord/login">${T('loginWithDiscord')}</a>
    </article>`;
}

function renderAdminNotice() {
  stopFishingParticles();
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
  stopFishingParticles();
  $("#nav-user").innerHTML = `${esc(T('welcome').replace("{name}", state.me.username))} · <a href="#" id="logout">${T('logout')}</a>`;
  bindLogout("#logout");

  const me = state.me;
  // R-A: level-gated tabs. 4+ users get the co-admin panel (review at 4,
  // + resources/pricing at 5); 5-only users get the all-site logs tab.
  const tabs = ["configs", "credits", "games", "charity", "logs", "debug"];
  if (me.level >= 4) tabs.splice(tabs.indexOf("charity") + 1, 0, "coadmin");
  if (me.level >= 5) tabs.splice(tabs.indexOf("logs") + 1, 0, "alllogs");
  const tabLabels = {
    configs: T('userTabConfigs'), credits: T('userTabCredits'), games: T('userTabGames'),
    charity: T('userTabCharity'), logs: T('userTabLogs'), debug: T('userTabDebug'),
    coadmin: T('tabCharityCoAdmin'), alllogs: T('tabAllLogs'),
  };
  const tabNav = tabs.map((t, i) =>
    `<button class="user-tab${i === 0 ? " active" : ""}" data-tab="${t}">${tabLabels[t]}</button>`
  ).join("");

  // R-A: level badge for everyone + level-2+ banner (plain text, not
  // closable). The badge name falls back to the numeric level server-side.
  const levelBadge = `<span class="level-badge" title="${T('levelBadge')}: ${esc(me.level_name)}" aria-label="${T('levelBadge')}: ${esc(me.level_name)}">Lv.${me.level} · ${esc(me.level_name)}</span>`;
  const levelBanner = (me.level >= 2 && me.banner_text)
    ? `<div class="level-banner" role="note" aria-label="${T('levelBanner')}">${esc(me.banner_text)}</div>`
    : "";

  $("#app").innerHTML = `
    <!-- Level badge + banner (above everything; banner only for level 2+) -->
    <div id="level-area">${levelBadge}${levelBanner}</div>
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
        <div class="table-wrap"><table><thead><tr><th>${T('thModel')}</th><th>${T('thNote')}</th><th>${T('thEnabled')}</th><th>${T('thActions')}</th></tr></thead><tbody id="cfg-rows">${skeletonRows(4, 5)}</tbody></table></div>
        <div class="row-actions" id="cfg-pager" style="margin:.5rem 0 1rem"></div>
        <form id="cfg-form">
          <div style="display:grid;grid-template-columns:${isNarrowScreen()?'1fr':'auto 1fr'};gap:.5rem;align-items:end">
            <label>${T('thService')}<select name="service" id="cfg-service"></select></label>
            <label>${T('thModel')}<input name="backend" placeholder="${T('fieldBackend')}${T('fieldBackendHint')}" required></label>
          </div>
          <div id="cfg-service-extra" style="margin:.25rem 0 .5rem">
            <span id="cfg-service-deprecated" class="badge warn" style="display:none;vertical-align:middle">${T('serviceDeprecated')}</span>
            <button type="button" id="cfg-getapp-btn" class="secondary" style="display:none;margin-left:.5rem">${T('serviceGetMyApp')}</button>
          </div>
          <label>${T('thBaseURL')}<input name="dify_base_url" placeholder="${T('fieldBaseURL')}" required></label>
          <div id="cfg-base-url-warn"></div>
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
        <div id="credits-info">${skeletonBlock(2)}</div>
        <div class="row-actions" style="margin-top:.5rem">
          <button id="checkin-btn" class="secondary">${T('creditsCheckin')}</button>
        </div>
      </section>
    </div>

    <!-- Games tab -->
    <div id="utab-games" class="user-tab-content" style="display:none">
      <section id="games-root" aria-live="polite">
        <article class="card skel-card">${skeletonBlock(3)}</article>
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

    <!-- Charity co-admin tab (level >= 4; R-A). Reuses the admin
         donation/pricing panel ids and interaction patterns — the shared
         handlers in admin.js map /api/admin/* to /api/me/* via coAdminPath. -->
    <div id="utab-coadmin" class="user-tab-content" style="display:none">
      <div id="donation-review-section" style="margin-bottom:1.5rem;padding:.75rem;border:1px solid var(--pico-muted-border-color);border-radius:4px">
        <h4>${T('donationReviewSection')}</h4>
        <div id="donation-review-content"></div>
      </div>
      <div id="coadmin-level5" style="display:none">
        <section class="card" id="pricing-panel">
          <h3>${T('pricingTitle')}</h3>
          <form id="pricing-form">
            <div style="display:grid;grid-template-columns:${isNarrowScreen()?'1fr':'1fr 1fr 1fr 1fr'};gap:.5rem;align-items:end">
              <label>${T('pricingThService')}<select name="service" id="pricing-service"></select></label>
              <label>${T('pricingThModel')}<input name="model" placeholder="${T('fieldBackend')}" required></label>
              <label>${T('pricingThPrice')}<input name="price" type="number" min="0" value="0" required></label>
              <label>${T('pricingThReward')}<input name="reward" type="number" min="0" placeholder="自动"></label>
            </div>
            <small class="muted" style="margin-top:.25rem">${T('pricingRewardHint')}</small>
            <div id="pricing-note"></div>
            <button type="submit">${T('pricingAdd')}</button>
          </form>
          <div id="pricing-batch-bar" style="display:none;gap:.5rem;align-items:center;margin-bottom:.5rem;margin-top:1rem">
            <button class="secondary batch-pricing-del" style="width:auto;margin:0">${T('batchDelete')}</button>
          </div>
          <div class="table-wrap"><table><thead><tr>
            <th><input type="checkbox" id="pricing-select-all" title="${T('batchSelectAll')}"></th>
            <th>${T('pricingThService')}</th><th>${T('pricingThModel')}</th>
            <th>${T('pricingThPrice')}</th><th>${T('pricingThReward')}</th>
            <th>${T('pricingThEnabled')}</th><th>${T('thActions')}</th>
          </tr></thead><tbody id="pricing-rows"></tbody></table></div>
        </section>
        <section class="card">
          <h3>${T('charityTitle')}</h3>
          <form id="donation-form">
            <div style="display:grid;grid-template-columns:${isNarrowScreen()?'1fr':'auto 1fr'};gap:.5rem;align-items:end">
              <label>${T('thService')}<select name="service" id="don-service"></select></label>
              <label>${T('thModel')}<input name="model" placeholder="${T('fieldBackend')}" required></label>
            </div>
            <label>${T('fieldBaseURL')}<input name="dify_base_url" placeholder="https://api.dify.ai/v1" required></label>
            <label>${T('fieldAPIKey')}<input name="dify_api_key" placeholder="app-…" required></label>
            <label>${T('charitySourceText')}<input name="source_text" placeholder="${T('charitySourceTextPlaceholder')}"></label>
            <div style="display:grid;grid-template-columns:${isNarrowScreen()?'1fr':'1fr 1fr 1fr'};gap:.5rem">
              <label>${T('charityDeadline')}<input name="deadline" type="datetime-local" required></label>
              <label>${T('charityTotalCount')}<input name="total_count" type="number" min="1" required></label>
              <label>${T('rpmLimitLabel')}<input name="rpm_limit" type="number" min="1" value="10" placeholder="${T('rpmLimitHint')}"></label>
            </div>
            <label>${T('charityNote')}<input name="note" placeholder="${T('charityNote')}"></label>
            <div id="don-note"></div>
            <button type="submit">${T('charitySubmit')}</button>
          </form>
          <div id="don-batch-bar" style="display:none;gap:.5rem;align-items:center;margin-bottom:.5rem;margin-top:1.5rem">
            <button class="secondary batch-don-activate" style="width:auto;margin:0">${T('batchActivate')}</button>
            <button class="secondary batch-don-deactivate" style="width:auto;margin:0">${T('batchDeactivate')}</button>
            <button class="contrast outline batch-don-delete" style="width:auto;margin:0">${T('batchDelete')}</button>
          </div>
          <div id="don-filter">
            <label>${T('charityThStatus')}<select id="don-filter-status"><option value="">${T('donationAppStatusAll')}</option><option value="active">${T('charityStatusActive')}</option><option value="inactive">${T('charityStatusInactive')}</option><option value="expired">${T('charityStatusExpired')}</option></select></label>
            <label>${T('thService')}<select id="don-filter-service"><option value="">${T('adminLogsAllServices')}</option></select></label>
            <label>${T('donFilterKeyword')}<input id="don-filter-q" placeholder="${T('donFilterKeywordPlaceholder')}" autocomplete="off"></label>
          </div>
          <div class="table-wrap"><table><thead><tr>
            <th><input type="checkbox" id="don-select-all" title="${T('batchSelectAll')}"></th>
            <th>${T('charityThService')}</th><th>${T('charityThModel')}</th><th>${T('charityThSource')}</th>
            <th>Key</th>
            <th>${T('charityThStatus')}</th><th>${T('charityThRemaining')}</th><th>RPM</th><th>${T('charityThDeadline')}</th>
            <th>${T('thNote')}</th><th>${T('adminReviewNote')}</th><th>${T('thActions')}</th>
          </tr></thead><tbody id="don-rows"></tbody></table></div>
          <div class="row-actions" id="don-pager" style="margin-top:.5rem"></div>
        </section>
      </div>
    </div>

    <!-- All-site logs tab (level 5; R-A). List + stats, no export. -->
    <div id="utab-alllogs" class="user-tab-content" style="display:none">
      <section class="card">
        <h3>${T('tabAllLogs')}</h3>
        <div id="admin-logs-filter" class="alf-no-user" style="margin-bottom:.8rem">
          <label class="afl-svc">${T('thService')}<select id="alf-service"><option value="">${T('adminLogsAllServices')}</option></select></label>
          <label class="afl-status">${T('thStatus')}<select id="alf-status"><option value="">${T('adminLogsAllStatus')}</option><option value="success">${T('adminLogsSuccess')}</option><option value="error">${T('adminLogsError')}</option></select></label>
          <label class="afl-model">${T('adminLogsModel')}<input id="alf-model" placeholder="[公益][general]x"></label>
          <label class="afl-since">${T('adminLogsSince')}<input id="alf-since" type="datetime-local"></label>
          <label class="afl-until">${T('adminLogsUntil')}<input id="alf-until" type="datetime-local"></label>
          <div class="afl-actions">
            <button id="alf-query">${T('adminLogsQuery')}</button>
          </div>
        </div>
        <div id="alf-chart-area" style="display:none;margin-bottom:.8rem">
          <div class="admin-log-chart" id="alf-day-chart-wrap">
            <canvas id="alf-day-chart" role="img" aria-label="${esc(T('adminLogsDailyChartAria'))}" aria-describedby="alf-chart-summary">${esc(T('adminLogsDailyChartAria'))}</canvas>
          </div>
          <p id="alf-chart-summary" class="sr-only" aria-live="polite"></p>
        </div>
        <div class="table-wrap"><table><thead><tr><th>${T('thTime')}</th><th>${T('thUser')}</th><th>${T('thModel')}</th><th>${T('thService')}</th><th>${T('thDuration')}</th><th>${T('thStatus')}</th><th>${T('thHTTPStatus')}</th><th>${T('thErrorCode')}</th><th>${T('thErrorDetail')}</th><th>${T('thCreditsConsumed')}</th><th>${T('thAntiAbuse')}</th><th>${T('thDonationSource')}</th></tr></thead><tbody id="alf-rows"></tbody></table></div>
        <div class="row-actions" id="alf-pager" style="margin-top:.5rem"></div>
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
  initTabScroll(document.querySelector(".tab-nav"));
  bindCellToggles(document);

  // Reset tab lazy-load state for fresh render.
  for (const k of Object.keys(_userTabLoaded)) delete _userTabLoaded[k];
  _userTabLoaded.configs = true;
  // Defensive: the shared co-admin/all-logs endpoint mappers in admin.js
  // always point at the admin site unless the user-site tabs set them.
  if (typeof _coAdminMode !== "undefined") _coAdminMode = "admin";
  if (typeof _allLogsMode !== "undefined") _allLogsMode = "admin";

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
  scrollActiveTabIntoView(document.querySelector(".tab-nav"));
  // Lazy load on first activation.
  if (!_userTabLoaded[tab]) {
    _userTabLoaded[tab] = true;
    switch (tab) {
      case "credits": renderCreditsCard(); break;
      case "games": renderGamesTab(); break;
      case "charity": renderCharityCard(); renderMyDonations(); break;
      case "logs": initUserLogsTab(); break;
      case "debug": initUserDebugTab(); break;
      case "coadmin": initUserCoAdminTab(); break;
      case "alllogs": initUserAllLogsTab(); break;
    }
  }
}

async function initUserConfigsTab() {
  // A stale pending id from a previous render must not survive: the rebuilt
  // form is empty and a leftover id would silently route a brand-new config
  // submit to PUT, overwriting the old row (see onConfigSubmit).
  _cfgSelfSitePending = null;
  $("#cfg-form").onsubmit = onConfigSubmit;
  const { services } = await api("/api/services");
  _cfgServices = services || [];
  $("#cfg-service").innerHTML = _cfgServices
    .map((s) => `<option value="${esc(s.name)}" title="${esc(s.label)}">${esc(s.name)}</option>`)
    .join("");
  // Deprecation badge + "Get My App" button follow the selected service.
  updateCfgServiceExtras();
  $("#cfg-service").onchange = updateCfgServiceExtras;
  const _getappBtn = $("#cfg-getapp-btn");
  if (_getappBtn) _getappBtn.onclick = showGetMyAppDialog;
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

function bindCreditsLogoFallback(container) {
  const img = container && container.querySelector(".credits-logo-img");
  if (!img) return;
  img.addEventListener("error", () => {
    const text = img.parentElement && img.parentElement.querySelector(".cr-text");
    if (text) text.style.display = "";
    img.style.display = "none";
  }, { once: true });
}

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
    const logoImg = `<img class="credits-logo-img" src="/credits-logo" alt="" style="height:2rem;vertical-align:middle;margin-right:.5rem">`;
    $("#credits-info").innerHTML = `
      ${logoImg}<span class="cr-text" style="${logoText ? 'display:none' : ''};font-size:1.5rem;margin-right:.5rem">${esc(logoText)}</span>
      <strong>${esc(creditsName)}</strong>
      <span class="badge ok" style="margin-left:.75rem">${T('creditsBalance').replace("{n}", String(me.credits || 0))}</span>`;
    bindCreditsLogoFallback($("#credits-info"));

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
    } else if (status.capped) {
      // Balance already at/above the cap: check-in would be refused server-side.
      btn.textContent = T('creditsCheckinCapped');
      btn.disabled = true;
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
        bindCreditsLogoFallback($("#credits-info"));
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

/* ---------------- user site: mini-games ---------------- */
function newFishingUIState() {
  return {
    entered: false,
    credits: 0,
    selectedBait: "",
    pendingRoundID: "",
    busy: false,
    phase: "idle",
    result: null,
    errorCode: "",
    creditBlockedBait: "",
    unavailable: false,
    board: "single",
    leaderboardAnon: false,
  };
}

let _gamesOwnerID = null;
let _gamesCatalog = null;
let _fishingState = newFishingUIState();
let _fishingParticleStop = null;

function stopFishingParticles() {
  if (_fishingParticleStop) _fishingParticleStop();
  _fishingParticleStop = null;
}

// Scheme C water atmosphere: a small, lifecycle-bound canvas layer for
// drifting highlights and bubbles. It is deliberately independent of the
// fishing phase; phase motion belongs to the CSS rig below.
function initFishingParticles() {
  stopFishingParticles();
  const canvas = $("#fishing-particles");
  const stage = $("#fishing-stage");
  if (!canvas || !stage) return;
  const ctx = canvas.getContext("2d");
  if (!ctx) return;
  const dpr = Math.min(window.devicePixelRatio || 1, 2);
  let width = 1;
  let height = 1;
  let frameID = 0;
  let tick = 0;
  const reduced = window.matchMedia?.("(prefers-reduced-motion: reduce)")?.matches;
  const bubbles = Array.from({ length: 14 }, () => ({
    x: Math.random(), y: .55 + Math.random() * .45,
    r: 1 + Math.random() * 2.4, speed: .0006 + Math.random() * .0012,
  }));
  const sparkles = Array.from({ length: 26 }, () => ({
    x: Math.random(), y: .52 + Math.random() * .46, phase: Math.random() * Math.PI * 2,
  }));
  const isDark = () => document.documentElement.dataset.theme === "dark" ||
    (!document.documentElement.dataset.theme && window.matchMedia?.("(prefers-color-scheme: dark)")?.matches);
  const resize = () => {
    const rect = stage.getBoundingClientRect();
    width = Math.max(1, rect.width);
    height = Math.max(1, rect.height);
    canvas.width = Math.round(width * dpr);
    canvas.height = Math.round(height * dpr);
    canvas.style.width = `${width}px`;
    canvas.style.height = `${height}px`;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  };
  const draw = () => {
    ctx.clearRect(0, 0, width, height);
    const waterline = height * .52;
    const dark = isDark();
    for (const sparkle of sparkles) {
      const alpha = .06 + .1 * (.5 + .5 * Math.sin(tick * .05 + sparkle.phase));
      ctx.fillStyle = dark ? `rgba(180,220,255,${alpha})` : `rgba(255,255,255,${alpha})`;
      const x = sparkle.x * width;
      const y = waterline + (sparkle.y - .5) * height * .9;
      ctx.beginPath();
      ctx.ellipse(x, y, 14, 1.6, 0, 0, Math.PI * 2);
      ctx.fill();
    }
    for (const bubble of bubbles) {
      if (!reduced) {
        bubble.y -= bubble.speed;
        if (bubble.y < .53) {
          bubble.y = 1;
          bubble.x = Math.random();
        }
      }
      const x = bubble.x * width + Math.sin(tick * .03 + bubble.x * 10) * 6;
      const y = bubble.y * height;
      ctx.strokeStyle = dark ? "rgba(190,225,255,.5)" : "rgba(255,255,255,.6)";
      ctx.lineWidth = 1;
      ctx.beginPath();
      ctx.arc(x, y, bubble.r, 0, Math.PI * 2);
      ctx.stroke();
    }
  };
  const onResize = () => { resize(); draw(); };
  const stop = () => {
    if (frameID) cancelAnimationFrame(frameID);
    window.removeEventListener("resize", onResize);
    ctx.clearRect(0, 0, width, height);
  };
  _fishingParticleStop = stop;
  resize();
  if (reduced) {
    draw();
    return;
  }
  const frame = () => {
    tick += 1;
    draw();
    frameID = requestAnimationFrame(frame);
  };
  window.addEventListener("resize", onResize, { passive: true });
  frame();
}

function finiteGameNumber(value, fallback = 0) {
  const n = Number(value);
  return Number.isFinite(n) ? n : fallback;
}

async function renderGamesTab() {
  const root = $("#games-root");
  if (!root || !state.me) return;
  stopFishingParticles();

  const ownerID = state.me.id;
  if (_gamesOwnerID !== ownerID) {
    _gamesOwnerID = ownerID;
    _gamesCatalog = null;
    _fishingState = newFishingUIState();
  }
  root.innerHTML = `<article class="card skel-card">${skeletonBlock(3)}</article>`;

  try {
    const data = await api("/api/me/games");
    if (!state.me || state.me.id !== ownerID || !$("#games-root")) return;
    _gamesCatalog = data || {};
    _fishingState.credits = finiteGameNumber(data?.credits);
    _fishingState.leaderboardAnon = !!data?.leaderboard_anon;
    const fishing = getFishingGame();
    _fishingState.unavailable = !data?.master_enabled || !fishing?.enabled;
    renderGamesCatalog();
    if (_fishingState.entered && !_fishingState.unavailable) {
      loadFishingLeaderboard(_fishingState.board);
    }
  } catch (_) {
    root.innerHTML = `
      <article class="card">
        <h3>${T('gamesTitle')}</h3>
        <div class="note err">${T('gamesNetworkError')}</div>
        <button type="button" id="games-retry" class="secondary">${T('retry')}</button>
      </article>`;
    const retry = $("#games-retry");
    if (retry) retry.onclick = renderGamesTab;
  }
}

function getFishingGame() {
  const games = Array.isArray(_gamesCatalog?.games) ? _gamesCatalog.games : [];
  return games.find((game) => game && game.id === "fishing") || null;
}

function renderGamesCatalog() {
  const root = $("#games-root");
  if (!root || !_gamesCatalog) return;
  stopFishingParticles();

  const games = Array.isArray(_gamesCatalog.games) ? _gamesCatalog.games : [];
  let cards = "";
  if (!_gamesCatalog.master_enabled) {
    cards = `<article class="card games-unavailable"><div class="note info">${T('gamesNotAvailable')}</div></article>`;
  } else if (games.length === 0) {
    cards = `<article class="card"><p class="muted games-empty">${T('gamesEmpty')}</p></article>`;
  } else {
    cards = games.map(renderUserGameCard).join("");
  }

  root.innerHTML = `
    <div class="games-heading">
      <div>
        <h2 id="games-title">${T('gamesTitle')}</h2>
        <p class="muted">${T('gamesIntro')}</p>
      </div>
      <span id="games-balance" class="badge ok">${T('creditsBalance').replace("{n}", String(_fishingState.credits))}</span>
    </div>
    <div class="games-list">${cards}</div>`;

  const entry = root.querySelector('.games-entry-btn[data-game-id="fishing"]');
  if (entry) {
    entry.onclick = () => {
      _fishingState.entered = true;
      renderGamesCatalog();
      bindFishingUI();
      loadFishingLeaderboard(_fishingState.board);
      const bait = $("#fishing-baits");
      if (bait) bait.focus({ preventScroll: false });
    };
  }
  if (_fishingState.entered && !_fishingState.unavailable) bindFishingUI();
}

function renderUserGameCard(game) {
  if (!game || typeof game.id !== "string") return "";
  if (game.id === "fishing") return renderFishingGameCard(game);
  return `
    <article class="card game-card" data-game-id="${esc(game.id)}">
      <div class="game-card-head">
        <div><h3>${esc(game.id)}</h3></div>
        <button type="button" class="secondary games-entry-btn" disabled>${T('gamesNotAvailable')}</button>
      </div>
    </article>`;
}

function renderFishingGameCard(game) {
  const unavailable = _fishingState.unavailable || !game.enabled;
  const body = _fishingState.entered && !unavailable
    ? fishingGameBodyHTML(game)
    : unavailable
      ? `<div class="note info">${T('gamesNotAvailable')}</div>`
      : `<button type="button" class="games-entry-btn" data-game-id="fishing">${T('gamesEnter')}</button>`;
  return `
    <article class="card game-card fishing-card" data-game-id="fishing">
      <div class="game-card-head">
        <div class="game-card-title">
          <span class="game-card-mark" aria-hidden="true">≈</span>
          <div>
            <h3>${T('gamesFishingName')}</h3>
            <p class="muted">${T('gamesFishingHint')}</p>
          </div>
        </div>
      </div>
      ${body}
    </article>`;
}

function fishingBaitName(key) {
  const names = {
    worm: T('gamesBaitWorm'),
    lure: T('gamesBaitLure'),
    premium: T('gamesBaitPremium'),
  };
  return names[key] || String(key || "");
}

function gameSpeciesName(key) {
  const names = {
    whitebait: T('gameSpeciesWhitebait'),
    gudgeon: T('gameSpeciesGudgeon'),
    horse_mouth: T('gameSpeciesHorseMouth'),
    smelt: T('gameSpeciesSmelt'),
    loach: T('gameSpeciesLoach'),
    crucian: T('gameSpeciesCrucian'),
    tilapia: T('gameSpeciesTilapia'),
    yellow_catfish: T('gameSpeciesYellowCatfish'),
    ayu: T('gameSpeciesAyu'),
    stream_carp: T('gameSpeciesStreamCarp'),
    common_carp: T('gameSpeciesCommonCarp'),
    snakehead: T('gameSpeciesSnakehead'),
    catfish: T('gameSpeciesCatfish'),
    mandarin_fish: T('gameSpeciesMandarinFish'),
    rainbow_trout: T('gameSpeciesRainbowTrout'),
    grass_carp: T('gameSpeciesGrassCarp'),
    silver_carp: T('gameSpeciesSilverCarp'),
    bighead_carp: T('gameSpeciesBigheadCarp'),
    black_carp: T('gameSpeciesBlackCarp'),
    japanese_eel: T('gameSpeciesJapaneseEel'),
    yellowcheek: T('gameSpeciesYellowcheek'),
    taimen: T('gameSpeciesTaimen'),
    koi: T('gameSpeciesKoi'),
    boot: T('gameSpeciesBoot'),
    seaweed: T('gameSpeciesSeaweed'),
    plastic_bag: T('gameSpeciesPlasticBag'),
    branch: T('gameSpeciesBranch'),
    old_tire: T('gameSpeciesOldTire'),
    glasses: T('gameSpeciesGlasses'),
    phone_case: T('gameSpeciesPhoneCase'),
    fry: T('gameSpeciesFry'),
    bottle: T('gameSpeciesBottle'),
    clover: T('gameSpeciesClover'),
    shell: T('gameSpeciesShell'),
  };
  return names[key] || String(key || "");
}

function fishingTierName(tier) {
  const names = {
    small: T('gamesTierSmall'),
    regular: T('gamesTierRegular'),
    big: T('gamesTierBig'),
    giant: T('gamesTierGiant'),
    legend: T('gamesTierLegend'),
  };
  return names[tier] || "";
}

function fishingTreasureText(key) {
  const messages = {
    bottle: T('gamesTreasureBottle'),
    clover: T('gamesTreasureClover'),
    shell: T('gamesTreasureShell'),
  };
  return messages[key] || "";
}

function fishingBaits(game) {
  return Array.isArray(game?.params?.baits)
    ? game.params.baits.filter((b) => b && typeof b.bait === "string")
    : [];
}

function selectedFishingBait() {
  const game = getFishingGame();
  return fishingBaits(game).find((b) => b.bait === _fishingState.selectedBait) || null;
}

function fishingGameBodyHTML(game) {
  const baits = fishingBaits(game);
  const baitButtons = baits.map((item) => {
    const selected = item.bait === _fishingState.selectedBait;
    const price = finiteGameNumber(item.price);
    return `
      <button type="button" class="bait-choice secondary${selected ? " selected" : ""}"
              data-bait="${esc(item.bait)}" aria-pressed="${selected ? "true" : "false"}">
        <span class="bait-name">${esc(fishingBaitName(item.bait))}</span>
        <small>${esc(T('gamesBaitPrice').replace("{n}", String(price)))}</small>
      </button>`;
  }).join("");
  const allowedPhases = new Set(["casting", "waiting", "reeling", "result"]);
  const phase = allowedPhases.has(_fishingState.phase) ? _fishingState.phase : "idle";

  return `
    <div class="fishing-game-body">
      <div class="fishing-layout">
        <section class="fishing-play-panel" aria-labelledby="fishing-bait-title">
          <h4 id="fishing-bait-title">${T('gamesChooseBait')}</h4>
          <div id="fishing-baits" class="fishing-baits" tabindex="-1">${baitButtons}</div>
          <div id="fishing-stage" class="fishing-stage phase-${phase}" aria-label="${esc(fishingStatusText())}">
            <span class="fishing-celestial" aria-hidden="true"></span>
            <span class="fishing-cloud cloud-one" aria-hidden="true"></span>
            <span class="fishing-cloud cloud-two" aria-hidden="true"></span>
            <span class="fishing-bank" aria-hidden="true"></span>
            <canvas id="fishing-particles" class="fishing-particles" aria-hidden="true"></canvas>
            <span class="fishing-rig" aria-hidden="true"><span class="fishing-rod"><span class="fishing-line"><span class="fishing-float"></span><span class="fishing-catch"><span class="fishing-hook"></span><span class="fishing-leash"></span><span class="fishing-fish"></span></span></span></span></span>
            <span class="fishing-ripple ripple-one" aria-hidden="true"></span>
            <span class="fishing-ripple ripple-two" aria-hidden="true"></span>
          </div>
          <div class="fishing-cast-row">
            <button type="button" id="fishing-cast">${fishingCastButtonText()}</button>
            <p id="fishing-status" class="muted" role="status">${esc(fishingStatusText())}</p>
          </div>
        </section>
        <aside id="fishing-result" class="fishing-result-panel" aria-live="polite">
          ${fishingResultHTML()}
        </aside>
      </div>
      ${fishingLeaderboardShellHTML()}
    </div>`;
}

function fishingCastButtonText() {
  if (_fishingState.busy) {
    if (_fishingState.phase === "waiting") return T('gamesWaiting');
    if (_fishingState.phase === "reeling") return T('gamesReeling');
    return T('gamesCasting');
  }
  if (_fishingState.pendingRoundID) return T('retry');
  return T('gamesCast');
}

function fishingErrorText(code) {
  switch (code) {
    case "no_bait": return T('gamesNoBait');
    case "insufficient_credits": return T('gamesInsufficientCredits');
    case "rate_limited": return T('gamesRateLimited');
    case "unavailable": return T('gamesNotAvailable');
    default: return T('gamesNetworkError');
  }
}

function normalizeGameError(err) {
  if (err?.code === "insufficient_credits") return "insufficient_credits";
  if (err?.code === "rate_limited") return "rate_limited";
  if (err?.code === "games_disabled" || err?.code === "game_disabled") return "unavailable";
  return "network";
}

function fishingStatusText() {
  if (_fishingState.errorCode) return fishingErrorText(_fishingState.errorCode);
  if (_fishingState.busy) {
    if (_fishingState.phase === "waiting") return T('gamesWaiting');
    if (_fishingState.phase === "reeling") return T('gamesReeling');
    return T('gamesCasting');
  }
  const bait = selectedFishingBait();
  if (!bait) return T('gamesNoBait');
  if (_fishingState.creditBlockedBait === bait.bait || _fishingState.credits < finiteGameNumber(bait.price)) {
    return T('gamesInsufficientCredits');
  }
  return T('gamesReady');
}

function fishingResultHTML() {
  if (_fishingState.result) return fishingOutcomeHTML(_fishingState.result);
  if (_fishingState.errorCode) {
    return `<h4>${T('gamesResultTitle')}</h4><div class="note err">${esc(fishingErrorText(_fishingState.errorCode))}</div>`;
  }
  if (_fishingState.busy || _fishingState.pendingRoundID) {
    return `<h4>${T('gamesResultTitle')}</h4><p class="muted">${T('loading')}</p>`;
  }
  return `<h4>${T('gamesResultTitle')}</h4><p class="muted games-empty">${T('gamesResultEmpty')}</p>`;
}

function fishingOutcomeHTML(result) {
  const species = gameSpeciesName(result.species_key);
  const isJunk = !!result.is_junk;
  const isTreasure = !!result.is_treasure;
  const isFish = !isJunk && !isTreasure;
  const tier = fishingTierName(result.tier);
  const size = finiteGameNumber(result.size_cm);
  const glyph = isTreasure ? "✦" : isJunk ? "≈" : "◈";
  let badges = "";
  if (isFish && tier) {
    badges += `<span class="badge game-tier tier-${esc(result.tier)}">${esc(tier)}</span>`;
  }
  if (result.meter) badges += `<span class="badge game-meter">${T('gamesMeter')}</span>`;

  let special = "";
  if (isJunk) {
    special = result.species_key === "fry"
      ? T('gamesReleaseFry')
      : T('gamesJunkX').replace("{item}", species);
  } else if (isTreasure) {
    special = fishingTreasureText(result.species_key);
  }
  const sizeHTML = isFish ? `<span class="catch-size">× ${size} cm</span>` : "";
  const specialHTML = special ? `<p class="catch-special">${esc(special)}</p>` : "";
  return `
    <h4>${T('gamesResultTitle')}</h4>
    <div class="game-catch${isTreasure ? " treasure" : ""}${isJunk ? " junk" : ""}">
      <span class="catch-glyph" aria-hidden="true">${glyph}</span>
      <div class="catch-main">
        <div class="catch-name">${esc(species)} ${sizeHTML}</div>
        <div class="catch-badges">${badges}</div>
      </div>
      ${specialHTML}
      <p class="game-reward"><strong>${esc(T('gamesCreditsWon').replace("{n}", String(finiteGameNumber(result.credits_won))))}</strong></p>
      <p class="muted game-balance-result">${esc(T('creditsBalance').replace("{n}", String(finiteGameNumber(result.credits))))}</p>
    </div>`;
}

function fishingLeaderboardShellHTML() {
  const single = _fishingState.board === "single";
  return `
    <section class="games-leaderboard" aria-labelledby="games-leaderboard-title">
      <div class="games-leaderboard-head">
        <div>
          <h4 id="games-leaderboard-title">${T('gamesLeaderboard')}</h4>
          <div class="row-actions games-board-switch" role="group" aria-label="${esc(T('gamesLeaderboard'))}">
            <button type="button" class="secondary games-board-btn${single ? " active" : ""}" data-board="single" aria-pressed="${single ? "true" : "false"}">${T('gamesBoardSingle')}</button>
            <button type="button" class="secondary games-board-btn${single ? "" : " active"}" data-board="total" aria-pressed="${single ? "false" : "true"}">${T('gamesBoardTotal')}</button>
          </div>
        </div>
        <label class="games-anon-control">
          <input type="checkbox" id="games-anon-toggle" role="switch" ${_fishingState.leaderboardAnon ? "checked" : ""}>
          <span>${T('gamesAnonToggle')}</span>
        </label>
      </div>
      <div id="games-leaderboard-content"><p class="muted">${T('loading')}</p></div>
    </section>`;
}

function bindFishingUI() {
  document.querySelectorAll("#utab-games .bait-choice").forEach((button) => {
    button.onclick = () => {
      if (_fishingState.busy || _fishingState.pendingRoundID) return;
      _fishingState.selectedBait = button.dataset.bait || "";
      _fishingState.errorCode = "";
      if (_fishingState.creditBlockedBait !== _fishingState.selectedBait) {
        _fishingState.creditBlockedBait = "";
      }
      renderFishingResultPanel();
      updateFishingControls();
    };
  });
  const cast = $("#fishing-cast");
  if (cast) cast.onclick = onFishingCast;
  document.querySelectorAll("#utab-games .games-board-btn").forEach((button) => {
    button.onclick = () => loadFishingLeaderboard(button.dataset.board);
  });
  const anon = $("#games-anon-toggle");
  if (anon) {
    anon.onchange = async () => {
      const wanted = anon.checked;
      anon.disabled = true;
      try {
        const saved = await api("/api/me/games/anonymous", { method: "PUT", body: { anonymous: wanted } });
        _fishingState.leaderboardAnon = !!saved?.anonymous;
        anon.checked = _fishingState.leaderboardAnon;
        toast(T('gamesAnonSaved'));
        await loadFishingLeaderboard(_fishingState.board);
      } catch (err) {
        anon.checked = _fishingState.leaderboardAnon;
        toast(fishingErrorText(normalizeGameError(err)), 3200);
      } finally {
        if (document.body.contains(anon)) anon.disabled = false;
      }
    };
  }
  initFishingParticles();
  updateFishingControls();
}

function updateFishingControls() {
  const game = getFishingGame();
  if (!game) return;
  const pending = !!_fishingState.pendingRoundID;
  const unavailable = _fishingState.unavailable || !game.enabled;
  document.querySelectorAll("#utab-games .bait-choice").forEach((button) => {
    const selected = button.dataset.bait === _fishingState.selectedBait;
    button.classList.toggle("selected", selected);
    button.setAttribute("aria-pressed", selected ? "true" : "false");
    button.disabled = unavailable || _fishingState.busy || pending;
  });

  const bait = selectedFishingBait();
  const insufficient = !!bait && (_fishingState.creditBlockedBait === bait.bait || _fishingState.credits < finiteGameNumber(bait.price));
  const cast = $("#fishing-cast");
  if (cast) {
    cast.textContent = fishingCastButtonText();
    cast.disabled = _fishingState.busy || unavailable || (!pending && (!bait || insufficient));
  }
  const status = $("#fishing-status");
  if (status) {
    status.textContent = fishingStatusText();
    status.classList.toggle("game-error-text", !!_fishingState.errorCode || insufficient || unavailable);
  }
  const stage = $("#fishing-stage");
  if (stage) stage.setAttribute("aria-label", fishingStatusText());
  updateGamesBalance();
}

function updateGamesBalance() {
  const balance = $("#games-balance");
  if (balance) balance.textContent = T('creditsBalance').replace("{n}", String(_fishingState.credits));
}

function renderFishingResultPanel() {
  const panel = $("#fishing-result");
  if (panel) panel.innerHTML = fishingResultHTML();
}

function setFishingPhase(phase) {
  _fishingState.phase = phase;
  const stage = $("#fishing-stage");
  if (stage) {
    ["idle", "casting", "waiting", "reeling", "result"].forEach((name) => stage.classList.remove(`phase-${name}`));
    void stage.offsetWidth;
    stage.classList.add(`phase-${phase}`);
  }
  updateFishingControls();
}

function gameDelay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function fishingAnimationDelay(ms) {
  return window.matchMedia?.("(prefers-reduced-motion: reduce)")?.matches ? Math.min(ms, 80) : ms;
}

async function onFishingCast() {
  if (_fishingState.busy) return;
  if (_fishingState.pendingRoundID) {
    await retryFishingSettle();
    return;
  }
  const bait = selectedFishingBait();
  if (!bait) {
    _fishingState.errorCode = "no_bait";
    renderFishingResultPanel();
    updateFishingControls();
    return;
  }
  if (_fishingState.credits < finiteGameNumber(bait.price)) {
    _fishingState.errorCode = "insufficient_credits";
    _fishingState.creditBlockedBait = bait.bait;
    renderFishingResultPanel();
    updateFishingControls();
    return;
  }

  _fishingState.busy = true;
  _fishingState.phase = "idle";
  _fishingState.errorCode = "";
  _fishingState.result = null;
  renderFishingResultPanel();
  updateFishingControls();
  try {
    const started = await api("/api/me/games/fishing/start", {
      method: "POST",
      body: { bait: bait.bait },
    });
    if (!started?.round_id) throw new Error("missing round_id");
    _fishingState.pendingRoundID = started.round_id;
    _fishingState.credits = finiteGameNumber(started.credits, _fishingState.credits);
    _fishingState.creditBlockedBait = "";
    if (state.me) state.me.credits = _fishingState.credits;
    updateGamesBalance();
  } catch (err) {
    _fishingState.busy = false;
    _fishingState.errorCode = normalizeGameError(err);
    if (_fishingState.errorCode === "insufficient_credits") {
      _fishingState.creditBlockedBait = bait.bait;
      refreshFishingBalance();
    }
    if (_fishingState.errorCode === "unavailable") _fishingState.unavailable = true;
    setFishingPhase("idle");
    renderFishingResultPanel();
    return;
  }

  await playFishingAnimation();
  await settleFishingPending();
}

async function playFishingAnimation() {
  setFishingPhase("casting");
  await gameDelay(fishingAnimationDelay(700));
  setFishingPhase("waiting");
  await gameDelay(fishingAnimationDelay(1100));
  setFishingPhase("reeling");
  await gameDelay(fishingAnimationDelay(700));
}

async function retryFishingSettle() {
  if (!_fishingState.pendingRoundID || _fishingState.busy) return;
  _fishingState.busy = true;
  _fishingState.errorCode = "";
  renderFishingResultPanel();
  setFishingPhase("reeling");
  await gameDelay(fishingAnimationDelay(500));
  await settleFishingPending();
}

async function settleFishingPending() {
  const roundID = _fishingState.pendingRoundID;
  if (!roundID) return;
  try {
    const result = await api("/api/me/games/fishing/settle", {
      method: "POST",
      body: { round_id: roundID },
    });
    _fishingState.pendingRoundID = "";
    _fishingState.busy = false;
    _fishingState.errorCode = "";
    _fishingState.result = result;
    _fishingState.credits = finiteGameNumber(result?.credits, _fishingState.credits);
    _fishingState.creditBlockedBait = "";
    if (state.me) state.me.credits = _fishingState.credits;
    setFishingPhase("result");
    renderFishingResultPanel();
    loadFishingLeaderboard(_fishingState.board);
  } catch (err) {
    _fishingState.busy = false;
    _fishingState.errorCode = normalizeGameError(err);
    setFishingPhase("idle");
    renderFishingResultPanel();
  }
}

async function refreshFishingBalance() {
  try {
    const latest = await api("/api/me/games");
    _fishingState.credits = finiteGameNumber(latest?.credits, _fishingState.credits);
    _fishingState.leaderboardAnon = !!latest?.leaderboard_anon;
    if (state.me) state.me.credits = _fishingState.credits;
    const bait = selectedFishingBait();
    if (bait && _fishingState.credits >= finiteGameNumber(bait.price)) {
      _fishingState.creditBlockedBait = "";
    }
    updateFishingControls();
  } catch (_) { /* keep the safe, disabled state until a later refresh */ }
}

function updateFishingBoardButtons() {
  document.querySelectorAll("#utab-games .games-board-btn").forEach((button) => {
    const active = button.dataset.board === _fishingState.board;
    button.classList.toggle("active", active);
    button.setAttribute("aria-pressed", active ? "true" : "false");
  });
}

async function loadFishingLeaderboard(board) {
  if (board !== "single" && board !== "total") board = "single";
  _fishingState.board = board;
  updateFishingBoardButtons();
  const content = $("#games-leaderboard-content");
  if (!content) return;
  content.innerHTML = `<p class="muted">${T('loading')}</p>`;
  const requestedBoard = board;
  try {
    const data = await api(`/api/me/games/fishing/leaderboard?board=${requestedBoard}`);
    if (_fishingState.board !== requestedBoard || !$("#games-leaderboard-content")) return;
    renderFishingLeaderboard(data || {}, requestedBoard);
  } catch (_) {
    if (_fishingState.board !== requestedBoard || !$("#games-leaderboard-content")) return;
    content.innerHTML = `
      <div class="note err">${T('gamesNetworkError')}</div>
      <button type="button" id="games-lb-retry" class="secondary">${T('retry')}</button>`;
    const retry = $("#games-lb-retry");
    if (retry) retry.onclick = () => loadFishingLeaderboard(requestedBoard);
  }
}

function isMyLeaderboardEntry(entry, me, board) {
  if (!entry || !me || entry.rank !== me.rank) return false;
  const sameValue = board === "single"
    ? entry.size_cm === me.size_cm && entry.species_key === me.species_key
    : entry.total === me.total;
  if (!sameValue) return false;
  return me.anonymous ? !!entry.anonymous : entry.username === me.username;
}

function fishingLeaderboardRow(entry, board, isMe) {
  const rank = finiteGameNumber(entry?.rank);
  const anonymous = !!entry?.anonymous || !entry?.username;
  const username = anonymous ? T('gamesAnonymousUser') : esc(entry.username);
  const meBadge = isMe ? `<span class="badge game-my-rank">${T('gamesMyRank')}</span>` : "";
  let value = "—";
  if (board === "single") {
    const species = entry?.species_key ? esc(gameSpeciesName(entry.species_key)) : "";
    const size = finiteGameNumber(entry?.size_cm);
    value = `${species ? `<span class="game-lb-species">${species}</span> · ` : ""}<strong>${size} cm</strong>`;
  } else {
    value = `<strong>${finiteGameNumber(entry?.total)}</strong>`;
  }
  return `
    <tr${isMe ? ' class="game-lb-me"' : ""}>
      <td class="mono">#${rank}</td>
      <td>${username} ${meBadge}</td>
      <td>${value}</td>
    </tr>`;
}

function renderFishingLeaderboard(data, board) {
  const content = $("#games-leaderboard-content");
  if (!content) return;
  const entries = Array.isArray(data?.entries) ? data.entries.slice(0, 20) : [];
  // The list endpoint is authoritative for the user's current toggle. Apply
  // it to `me` as well so both boards render consistently even if an older
  // backend omits the flag from the personal total-board row.
  const me = data?.me ? { ...data.me, anonymous: _fishingState.leaderboardAnon } : null;
  let markedMe = false;
  const rows = entries.map((entry) => {
    const mine = isMyLeaderboardEntry(entry, me, board);
    if (mine) markedMe = true;
    return fishingLeaderboardRow(entry, board, mine);
  });
  if (me && !markedMe) rows.push(fishingLeaderboardRow(me, board, true));
  const valueHeading = board === "single" ? T('gamesSize') : T('gamesTotal');
  content.innerHTML = `
    <div class="table-wrap">
      <table aria-label="${esc(T('gamesLeaderboard'))}">
        <thead><tr><th>${T('gamesRank')}</th><th>${T('username')}</th><th>${valueHeading}</th></tr></thead>
        <tbody>${rows.length ? rows.join("") : `<tr><td colspan="3" class="empty-state games-empty">${T('gamesEmpty')}</td></tr>`}</tbody>
      </table>
    </div>`;
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
        <th>${T('pricingThService')}</th><th>${T('pricingThModel')}</th><th>${T('pricingThPrice')}</th><th>${T('pricingThAvailable')}</th>
      </tr></thead><tbody>
        ${pricingList.map((p) => `<tr><td>${esc(p.service)}</td><td class="mono">${esc(p.model)}</td><td class="mono">${esc(String(p.price))}</td><td>${p.available ? `<span class="badge ok">${T('pricingAvailable')}</span>` : `<span class="badge off">${T('pricingUnavailable')}</span>`}</td></tr>`).join("")}
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
    html += `<p class="empty-state">${T('empty')}</p>`;
  }

  container.innerHTML = html;

  // Bind apply button.
  const applyBtn = $("#donation-apply-btn");
  const form = $("#donation-apply-form");
  if (applyBtn && form && canSubmit) {
    applyBtn.onclick = () => { form.style.display = form.style.display === "none" ? "" : "none"; };

    // Populate service dropdown (only services the admin allows for
    // self-service donations).
    try {
      const { services } = await api("/api/services?donation=1");
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
        if (resp.notice) {
          // Keep the form (values intact) so the user can fix the address.
          msg.innerHTML = `<div class="note warn">⚠ ${esc(resp.notice)}</div>`;
          await renderMyDonations();
          return;
        }
        f.reset();
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
            const { services } = await api("/api/services?donation=1");
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
  const svcName = (String(c.model || "").match(/^\[([^\]]+)\]/) || [])[1] || "";
  const svc = _cfgServices.find((item) => item.name === svcName);
  const deprecated = svc?.deprecated ? ` <span class="badge warn">${T('serviceDeprecated')}</span>` : "";
  return `
    <tr data-id="${c.id}">
      <td class="mono">${esc(c.model)}${deprecated}</td>
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
  // Bind row actions inside afterRender so they are re-bound after every
  // render (initial load, page change, page-size change). Binding once after
  // renderPaged left paged-away rows dead after flipping pages.
  cfgPager.afterRender = () => {
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
  };
  renderPaged(cfgPager, "#cfg-rows", "#cfg-pager", 4);
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
  let svcList = [];
  let svcOpts = "";
  try {
    const { services } = await api("/api/services");
    svcList = services || [];
    svcOpts = svcList.map((s) => `<option value="${esc(s.name)}" ${s.name === svc ? "selected" : ""}>${esc(s.name)}</option>`).join("");
  } catch (_) { /* keep empty */ }

  dialog.innerHTML = `
    <article>
      <header><h3>${T('editConfig')}</h3></header>
      <form id="cfg-edit-form">
        <label>${T('thService')}<select name="service" id="cfg-edit-service">${svcOpts}</select><span id="cfg-edit-deprecated" class="badge warn" style="display:none;margin-left:.5rem;vertical-align:middle">${T('serviceDeprecated')}</span></label>
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

  // Deprecation badge follows the edited service selection.
  const updateEditDeprecated = () => {
    const sel = $("#cfg-edit-service");
    const name = sel ? sel.value : "";
    const s = svcList.find((x) => x.name === name);
    const badge = $("#cfg-edit-deprecated");
    if (badge) badge.style.display = s && s.deprecated ? "" : "none";
  };
  updateEditDeprecated();
  const _editSvcSel = $("#cfg-edit-service");
  if (_editSvcSel) _editSvcSel.onchange = updateEditDeprecated;

  const close = () => { dialog.close(); dialog.remove(); };
  $("#cfg-edit-cancel").onclick = close;
  dialog.addEventListener("click", (e) => { if (e.target === dialog) close(); });

  $("#cfg-edit-save").onclick = async () => {
    const f = $("#cfg-edit-form");
    const _editSvc = svcList.find((x) => x.name === f.service.value);
    if (_editSvc && _editSvc.deprecated && !confirm(T('serviceDeprecated') + ' · ' + f.service.value)) return;
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

// Services cached for the configs form: drives the deprecation badge and
// the "Get My App" button. Refreshed in initUserConfigsTab.
let _cfgServices = [];

function selectedCfgService() {
  const sel = $("#cfg-service");
  if (!sel) return null;
  const name = sel.value || "";
  return _cfgServices.find((s) => s.name === name) || null;
}

// Show/hide the deprecation badge and the "Get My App" button based on the
// service currently selected in the configs form.
function updateCfgServiceExtras() {
  const svc = selectedCfgService();
  const badge = $("#cfg-service-deprecated");
  const btn = $("#cfg-getapp-btn");
  if (badge) badge.style.display = svc && svc.deprecated ? "" : "none";
  if (btn) btn.style.display = svc && svc.downloadable ? "" : "none";
}

// Build the label for a model option in the Get-My-App dialog:
// "Display Name · provider · v1.2.3".
function getMyAppModelLabel(m) {
  const parts = [m.display_name || m.model_key];
  if (m.provider) parts.push(m.provider);
  if (m.dependency_version) parts.push("v" + m.dependency_version);
  return parts.join(" · ");
}

// "Get My App" dialog: list downloadable models for sillytavern-main-v1 and
// fetch a fresh obfuscated DSL (personal or donation purpose). Reuses the
// same <dialog> pattern as the config edit dialog.
async function showGetMyAppDialog() {
  const old = $("#cfg-getapp-dialog");
  if (old) old.remove();

  const dialog = document.createElement("dialog");
  dialog.id = "cfg-getapp-dialog";
  document.body.appendChild(dialog);
  dialog.innerHTML = `
    <article>
      <header><h3>${T('serviceGetMyApp')}</h3></header>
      <p class="muted">${T('loading')}</p>
      <footer style="display:flex;gap:.5rem;justify-content:flex-end;margin-top:.5rem">
        <button type="button" id="getapp-close" class="secondary">${T('bulletinClose')}</button>
      </footer>
    </article>`;
  dialog.showModal();

  const close = () => { dialog.close(); dialog.remove(); };
  $("#getapp-close").onclick = close;
  dialog.addEventListener("click", (e) => { if (e.target === dialog) close(); });

  let models = null;
  let fetchErr = null;
  try {
    const service = selectedCfgService()?.name || "";
    if (!service) throw new Error("service is required");
    dialog.dataset.service = service;
    const data = await api(`/api/me/services/${encodeURIComponent(service)}/models`);
    models = data.models || [];
  } catch (err) {
    fetchErr = err;
  }
  // The user may have closed the dialog while the fetch was in flight.
  if (!document.body.contains(dialog)) return;

  let bodyHtml;
  if (fetchErr) {
    bodyHtml = `<div class="note err">${T('serviceDownloadError').replace("{msg}", esc(fetchErr.message))}</div>`;
  } else if (!models.length) {
    bodyHtml = `<p class="empty-state">${T('empty')}</p>`;
  } else {
    const opts = models.map((m) => {
      const disabled = m.enabled === false;
      return `<option value="${esc(m.model_key)}"${disabled ? " disabled" : ""}>${esc(getMyAppModelLabel(m))}</option>`;
    }).join("");
    bodyHtml = `
        <label>${T('serviceDownloadModel')}<select id="getapp-model">${opts}</select></label>
        <div class="note warn" style="margin:.5rem 0">${T('serviceRedownloadWarning')}</div>
        <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(11rem,1fr));gap:.6rem;margin:.75rem 0">
          <div style="display:flex;flex-direction:column;align-items:stretch;gap:.2rem">
            <button type="button" id="getapp-download" style="margin:0">${T('serviceDownloadDsl')}</button>
            <small class="muted" style="line-height:1.35">${T('serviceDownloadDslHint')}</small>
          </div>
          <div style="display:flex;flex-direction:column;align-items:stretch;gap:.2rem">
            <button type="button" id="getapp-donate" class="secondary" style="margin:0">${T('serviceDonate')}</button>
            <small class="muted" style="line-height:1.35">${T('serviceDonateHint')}</small>
          </div>
        </div>
        <div id="getapp-msg" aria-live="polite"></div>`;
  }
  dialog.innerHTML = `
    <article>
      <header><h3>${T('serviceGetMyApp')}</h3></header>
      ${bodyHtml}
      <footer style="display:flex;gap:.5rem;justify-content:flex-end;margin-top:.5rem">
        <button type="button" id="getapp-close" class="secondary">${T('bulletinClose')}</button>
      </footer>
    </article>`;
  // innerHTML reset cleared the close-button handler; rebind it. The backdrop
  // listener lives on `dialog` itself and survives the child replacement.
  $("#getapp-close").onclick = close;
  if (models && models.length) {
    $("#getapp-download").onclick = () => downloadMyApp("personal");
    $("#getapp-donate").onclick = () => downloadMyApp("donation");
  }
}

// Toggle download buttons + aria-busy while a download is in flight.
function setGetAppBusy(busy) {
  const dl = $("#getapp-download");
  const don = $("#getapp-donate");
  [dl, don].forEach((b) => {
    if (!b) return;
    b.disabled = busy;
    if (busy) b.setAttribute("aria-busy", "true"); else b.removeAttribute("aria-busy");
  });
}

// Download a DSL template for the selected model. `purpose` is "personal" or
// "donation". Donation downloads gate on a 409 confirm_required second step.
async function downloadMyApp(purpose) {
  const modelSel = $("#getapp-model");
  const modelKey = modelSel ? modelSel.value : "";
  if (!modelKey) return;
  const msg = $("#getapp-msg");
  setGetAppBusy(true);
  if (msg) msg.innerHTML = `<p class="muted">${T('loading')}</p>`;

  const run = async (confirmFlag) => {
    const service = $("#cfg-getapp-dialog")?.dataset.service || selectedCfgService()?.name || "";
    let url = `/api/me/services/${encodeURIComponent(service)}/download?model=${encodeURIComponent(modelKey)}&purpose=${encodeURIComponent(purpose)}`;
    if (confirmFlag) url += "&confirm=true";
    const resp = await fetch(url, { credentials: "same-origin" });
    if (resp.ok) {
      const blob = await resp.blob();
      const m = (resp.headers.get("Content-Disposition") || "").match(/filename="?([^"]+)"?/);
      return { ok: true, blob: blob, filename: m ? m[1] : `sillytavern-main-v1-${modelKey}.yml` };
    }
    let body = null;
    try { body = await resp.json(); } catch (_) { /* non-JSON error body */ }
    return { ok: false, status: resp.status, code: (body && body.error && body.error.code) || "", message: (body && body.error && body.error.message) || `HTTP ${resp.status}` };
  };

  let result;
  try {
    result = await run(false);
  } catch (err) {
    setGetAppBusy(false);
    if (msg) msg.innerHTML = `<div class="note err">${T('serviceDownloadError').replace("{msg}", esc(err.message))}</div>`;
    return;
  }

  // Donation second confirmation: 409 confirm_required → re-run with confirm.
  if (!result.ok && result.status === 409 && result.code === "confirm_required" && purpose === "donation") {
    if (confirm(T('serviceConfirmRequired'))) {
      try {
        result = await run(true);
      } catch (err) {
        setGetAppBusy(false);
        if (msg) msg.innerHTML = `<div class="note err">${T('serviceDownloadError').replace("{msg}", esc(err.message))}</div>`;
        return;
      }
    } else {
      setGetAppBusy(false);
      if (msg) msg.innerHTML = "";
      return;
    }
  }

  if (!result.ok) {
    setGetAppBusy(false);
    if (result.status === 403 && result.code === "donation_locked") {
      if (msg) msg.innerHTML = `<div class="note err">${T('serviceDonationLocked')}</div>`;
    } else {
      if (msg) msg.innerHTML = `<div class="note err">${T('serviceDownloadError').replace("{msg}", esc(result.message))}</div>`;
    }
    return;
  }

  // Success: trigger a browser download of the YAML attachment.
  try {
    const url = URL.createObjectURL(result.blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = result.filename;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
    if (msg) msg.innerHTML = `<div class="note ok">${T('serviceDownloadedHint')}</div><div class="note info">${T('serviceImportGuide')}</div>`;
    toast(T('serviceDownloadedHint'));
  } catch (err) {
    setGetAppBusy(false);
    if (msg) msg.innerHTML = `<div class="note err">${T('serviceDownloadError').replace("{msg}", esc(err.message))}</div>`;
    return;
  }
  setGetAppBusy(false);
}

// Self-site hint state for the create form. Set when the create API answers
// with a `notice` (base URL looks like this console). The config is already
// saved; we keep the form values, pin a persistent warning next to the field,
// and route the next submit to PUT so a corrected address updates the saved
// row in place (re-POSTing the same model name would 409).
let _cfgSelfSitePending = null; // {id: number} | null

async function onConfigSubmit(e) {
  e.preventDefault();
  const f = e.target;
  const _selSvc = _cfgServices.find((s) => s.name === f.service.value);
  if (_selSvc && _selSvc.deprecated && !confirm(T('serviceDeprecated') + ' · ' + f.service.value)) return;
  const body = {
    model: `[${f.service.value}]${f.backend.value.trim()}`,
    dify_base_url: f.dify_base_url.value,
    dify_api_key: f.dify_api_key.value,
    note: f.note.value,
  };
  const note = $("#check-note");
  const warn = $("#cfg-base-url-warn");
  note.innerHTML = `<p class="muted">${T('loading')}</p>`;
  const pending = _cfgSelfSitePending;
  try {
    const resp = pending
      ? await api(`/api/configs/${pending.id}`, { method: "PUT", body })
      : await api("/api/configs", { method: "POST", body });
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
      // Same stance as the edit dialog: the config is saved, but the base URL
      // looks like this console. Keep the form (values intact) and pin a
      // prominent warning next to the field — the user must fix the address
      // or explicitly confirm before the flow completes.
      _cfgSelfSitePending = { id: resp.config.id };
      warn.innerHTML = `
        <div class="note err">
          <strong>⚠ ${esc(resp.notice)}</strong><br>
          <span class="muted">${T('cfgSelfSiteFixHint')}</span><br>
          <button type="button" class="secondary" id="cfg-selfsite-confirm" style="margin-top:.4rem">${T('cfgSelfSiteConfirm')}</button>
        </div>`;
      $("#cfg-selfsite-confirm").onclick = () => {
        _cfgSelfSitePending = null;
        warn.innerHTML = "";
        note.innerHTML = "";
        f.reset();
        loadConfigs();
      };
      await loadConfigs();
      return;
    }
    _cfgSelfSitePending = null;
    warn.innerHTML = "";
    f.reset();
    await loadConfigs();
  } catch (err) {
    if (pending && err.status === 404) {
      // The row created in the previous attempt was deleted from the list
      // while the form stayed open; the next submit creates it fresh again.
      _cfgSelfSitePending = null;
      note.innerHTML = `<div class="note err">${T('cfgSelfSiteGone')}</div>`;
      return;
    }
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
      <td class="muted wrap-clamp">${cellClamp(l.error_detail)}</td>
      <td class="mono muted">${l.credits_consumed ? esc(String(l.credits_consumed)) : "0"}</td>
      <td class="muted">${esc(aaText)}</td>
    </tr>`;
}

async function loadLogs() {
  const { logs } = await api("/api/logs");
  logPager.data = logs || [];
  renderPaged(logPager, "#log-rows", "#log-pager", 8);
}

/* ---------------- R-A: charity co-admin tab (level >= 4) ---------------- */
// Level 4: donation-application review panel only. Level 5 additionally
// gets charity resource + pricing management. All data flows through
// /api/me/* endpoints (the admin.js panel code maps them via coAdminPath);
// /api/admin/* is never called from the user site.
async function initUserCoAdminTab() {
  _coAdminMode = "me";
  const level5 = (state.me.level || 0) >= 5;
  const five = $("#coadmin-level5");
  if (five) five.style.display = level5 ? "" : "none";
  if (level5) {
    try {
      const { services } = await api("/api/services");
      const svcOpts = (services || []).map((s) => `<option value="${esc(s.name)}">${esc(s.name)}</option>`).join("");
      const donSvc = $("#don-service");
      if (donSvc) donSvc.innerHTML = svcOpts;
      const pricingSvc = $("#pricing-service");
      if (pricingSvc) pricingSvc.innerHTML = svcOpts;
    } catch { /* keep dropdowns empty; forms still fail server-side */ }
    $("#donation-form").onsubmit = onDonationSubmit;
    $("#pricing-form").onsubmit = onPricingSubmit;
  }
  await renderAdminDonationReview();
  if (level5) {
    await loadAdminDonations();
    await loadPricing();
  }
}

/* ---------------- R-A: all-site logs tab (level 5, no export) ---------------- */
async function initUserAllLogsTab() {
  _allLogsMode = "me";
  const alfRows = $("#alf-rows");
  if (alfRows) alfRows.innerHTML = skeletonRows(12, 6);
  try {
    const { services } = await api("/api/services");
    $("#alf-service").innerHTML = `<option value="">${T('adminLogsAllServices')}</option>` +
      (services || []).map((s) => `<option value="${esc(s.name)}">${esc(s.name)}</option>`).join("");
  } catch { /* keep "all services" */ }
  $("#alf-query").onclick = () => { adminLogPager.page = 1; loadAdminLogs(); loadAdminLogStats(); };
  await loadAdminLogStats();
  await loadAdminLogs();
}
