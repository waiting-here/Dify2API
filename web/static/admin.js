"use strict";

/* ---------------- admin log charts (lazy Chart.js lifecycle) ---------------- */
let _chartJSLoadPromise = null;
let _adminLogCharts = [];
let _adminLogStats = null;
let _adminLogChartGeneration = 0;

/* R-A (v1.3.0): the donation/pricing/review panel code is shared between
 * the admin site (/api/admin/...) and the level-4/5 user site
 * (/api/me/...). _coAdminMode selects the endpoint prefix; the user site
 * sets it to "me" when its co-admin tab renders. The all-logs tab is
 * similarly shared with the level-5 user view (/api/me/all-logs), which
 * has no export. */
let _coAdminMode = "admin";
function coAdminPath(p) {
  if (_coAdminMode !== "me") return p;
  if (p === "/api/admin/donations/pending") return "/api/me/review/pending";
  if (p.startsWith("/api/admin/donations/") && (p.includes("/approve") || p.includes("/reject"))) {
    // /api/admin/donations/{id}/approve|reject and */approve|reject/batch
    return "/api/me/review/" + p.slice("/api/admin/donations/".length);
  }
  return p.replace("/api/admin/", "/api/me/charity-admin/");
}
let _allLogsMode = "admin";
function allLogsPath(p) {
  return _allLogsMode === "me" ? p.replace("/api/admin/logs", "/api/me/all-logs") : p;
}

// R-A level settings cache: thresholds/names/banner. Loaded once with the
// common admin data so the users tab can render level names; kept fresh by
// the levels tab after saving.
let _levelSettings = null;

function loadChartJS() {
  if (typeof window.Chart === "function") return Promise.resolve(window.Chart);
  if (_chartJSLoadPromise) return _chartJSLoadPromise;

  _chartJSLoadPromise = new Promise((resolve, reject) => {
    const script = document.createElement("script");
    script.src = "/static/chart.min.js";
    script.async = true;
    script.dataset.d2aChartjs = "4.5.1";
    script.onload = () => {
      if (typeof window.Chart === "function") {
        resolve(window.Chart);
        return;
      }
      script.remove();
      _chartJSLoadPromise = null;
      reject(new Error("Chart.js did not initialize"));
    };
    script.onerror = () => {
      script.remove();
      _chartJSLoadPromise = null;
      reject(new Error("Chart.js failed to load"));
    };
    document.head.appendChild(script);
  });
  return _chartJSLoadPromise;
}

function destroyAdminLogCharts() {
  _adminLogChartGeneration++;
  for (const chart of _adminLogCharts) {
    try { chart.destroy(); } catch (_) { /* already detached */ }
  }
  _adminLogCharts = [];
}

function hideAdminLogCharts() {
  destroyAdminLogCharts();
  const area = $("#alf-chart-area");
  if (area) area.style.display = "none";
}

function adminLogsTabVisible() {
  const adminTab = $("#tab-logs");
  if (adminTab) return adminTab.style.display !== "none";
  const userTab = $("#utab-alllogs");
  return !!userTab && userTab.style.display !== "none";
}

function resizeAdminLogCharts() {
  if (_adminLogCharts.length === 0) return;
  requestAnimationFrame(() => {
    for (const chart of _adminLogCharts) chart.resize();
  });
}

function reportAdminLogChartFailure() {
  hideAdminLogCharts();
  toast(T('adminLogsChartLoadFailed'), 3500);
}

// Called by common.js after a manual theme switch. Recreate rather than
// mutating instances so every computed CSS colour is refreshed consistently.
function onThemeChanged() {
  if (_adminLogStats && adminLogsTabVisible()) {
    renderAdminLogCharts(_adminLogStats).catch(reportAdminLogChartFailure);
  }
}

/* ---------------- admin site: login ---------------- */
function renderAdminLogin() {
  destroyAdminLogCharts();
  _adminLogStats = null;
  $("#nav-user").textContent = "";
  $("#app").innerHTML = `
    <article class="card" style="max-width:24rem;margin:4rem auto">
      <h3>${T('adminLoginTitle')}</h3>
      <form id="admin-login-form">
        <label>${T('username')}<input name="username" required autocomplete="username"></label>
        <label>${T('password')}<input name="password" type="password" required autocomplete="current-password"></label>
        <div id="login-err" class="note err" style="display:none"></div>
        <button type="submit">${T('login')}</button>
      </form>
    </article>`;
  $("#admin-login-form").onsubmit = async (e) => {
    e.preventDefault();
    const f = e.target;
    try {
      await api("/api/auth/admin/login", { method: "POST", body: { username: f.username.value, password: f.password.value } });
      state.me = await api("/api/me");
      route();
    } catch (err) {
      const el = $("#login-err");
      el.style.display = "";
      el.textContent = T('error').replace("{msg}", err.message);
    }
  };
}

/* ---------------- admin site: common data cache ---------------- */
let _adminCommonData = null;
async function loadAdminCommonData() {
  if (_adminCommonData) return _adminCommonData;
  const [{ users }, { services }, levelSettings] = await Promise.all([
    api("/api/admin/users"),
    api("/api/services"),
    api("/api/admin/level-settings"),
  ]);
  _adminCommonData = { users: users || [], services: services || [] };
  _levelSettings = levelSettings || {};
  adminLogUsers = _adminCommonData.users;
  return _adminCommonData;
}

/* ---------------- admin site: tab switching ---------------- */
const _adminTabLoaded = {};
function switchAdminTab(tab) {
  document.querySelectorAll(".admin-tab").forEach((b) => b.classList.remove("active"));
  document.querySelectorAll(".admin-tab-content").forEach((c) => (c.style.display = "none"));
  const btn = document.querySelector(`.admin-tab[data-tab="${tab}"]`);
  if (btn) btn.classList.add("active");
  const content = $(`#tab-${tab}`);
  if (content) content.style.display = "";
  // Lazy load on first activation. Mark the tab loaded only after its async
  // init succeeds, so a failed load can be retried by leaving and re-entering.
  if (!_adminTabLoaded[tab]) {
    const inits = {
      users: initAdminUsersTab,
      levels: initAdminLevelsTab,
      logs: initAdminLogsTab,
      donations: initAdminDonationsTab,
      alerts: initAdminAlertsTab,
      bulletins: initAdminBulletinsTab,
      antiabuse: initAdminAntiAbuseTab,
    };
    const fn = inits[tab];
    if (fn) {
      Promise.resolve(fn()).then(
        () => { _adminTabLoaded[tab] = true; },
        () => toast(T('tabLoadFailed'), 3000)
      );
    } else {
      _adminTabLoaded[tab] = true;
    }
  } else if (tab === "logs") {
    // A chart hidden by another tab can have a stale zero-sized layout.
    if (_adminLogCharts.length > 0) resizeAdminLogCharts();
    else if (_adminLogStats) renderAdminLogCharts(_adminLogStats).catch(reportAdminLogChartFailure);
  }
}

// adminUserOption renders a datalist option value for a user, including the
// numeric DB id so filters can be picked and searched by id too.
function adminUserOption(u) {
  return `${esc(u.username)}（${esc(u.discord_id)}） [${u.id}]`;
}

async function initAdminUsersTab() {
  await loadAdminCommonData();
  $("#user-search-list").innerHTML = _adminCommonData.users.map(adminUserOption).join("");
  await loadAdminUsers();
}

// R-A levels tab: loads the nine level settings into the form (names,
// thresholds, banner). Save sends the full field set through the dedicated
// atomic endpoint; validation errors surface the backend message verbatim.
async function initAdminLevelsTab() {
  const data = await api("/api/admin/level-settings");
  _levelSettings = data || {};
  const f = $("#level-settings-form");
  f.threshold_2.value = data.threshold_2 != null ? data.threshold_2 : 1;
  f.threshold_3.value = data.threshold_3 != null ? data.threshold_3 : 100;
  f.threshold_4.value = data.threshold_4 != null ? data.threshold_4 : 500;
  for (let i = 1; i <= 5; i++) f["name_" + i].value = data["name_" + i] || "";
  f.banner_text.value = data.banner_text || "";
  f.onsubmit = async (e) => {
    e.preventDefault();
    const msg = $("#level-settings-msg");
    msg.innerHTML = `<p class="muted">${T('loading')}</p>`;
    const body = {
      threshold_2: parseInt(f.threshold_2.value, 10),
      threshold_3: parseInt(f.threshold_3.value, 10),
      threshold_4: parseInt(f.threshold_4.value, 10),
      name_1: f.name_1.value.trim(),
      name_2: f.name_2.value.trim(),
      name_3: f.name_3.value.trim(),
      name_4: f.name_4.value.trim(),
      name_5: f.name_5.value.trim(),
      banner_text: f.banner_text.value.trim(),
    };
    try {
      await api("/api/admin/level-settings", { method: "PUT", body });
      _levelSettings = { ...body };
      msg.innerHTML = `<div class="note ok">${T('settingsSaved')}</div>`;
      toast(T('settingsSaved'));
    } catch (err) {
      msg.innerHTML = `<div class="note err">${T('error').replace("{msg}", esc(err.message))}</div>`;
    }
  };
}

async function initAdminLogsTab() {
  const data = await loadAdminCommonData();
  const userList = $("#alf-user-list");
  if (userList) userList.innerHTML = data.users.map(adminUserOption).join("");
  let svcOpts = `<option value="">${T('adminLogsAllServices')}</option>`;
  data.services.forEach((s) => { svcOpts += `<option value="${esc(s.name)}">${esc(s.name)}</option>`; });
  $("#alf-service").innerHTML = svcOpts;
  $("#alf-query").onclick = () => { adminLogPager.page = 1; loadAdminLogs(); loadAdminLogStats(); };
  const exportBtn = $("#alf-export");
  if (exportBtn) exportBtn.onclick = onExportLogs;
  await loadAdminLogStats();
  await loadAdminLogs();
}

async function initAdminDonationsTab() {
  const data = await loadAdminCommonData();
  $("#don-service").innerHTML = data.services
    .map((s) => `<option value="${esc(s.name)}">${esc(s.name)}</option>`)
    .join("");
  $("#don-user-list").innerHTML = data.users.map(adminUserOption).join("");
  // Populate application history filters.
  $("#dah-service").innerHTML = `<option value="">${T('donationAppStatusAll')}</option>` +
    data.services.map((s) => `<option value="${esc(s.name)}">${esc(s.name)}</option>`).join("");
  $("#dah-user-list").innerHTML = data.users.map(adminUserOption).join("");
  $("#dah-query").onclick = () => { donAppHistoryPager.page = 1; loadDonationAppHistory(); };
  $("#donation-form").onsubmit = onDonationSubmit;
  $("#pricing-form").onsubmit = onPricingSubmit;
  await renderAdminDonationReview();
  await loadDonationAppHistory();
  await loadAdminDonations();
  await loadPricing();
}

async function initAdminAlertsTab() {
  await Promise.all([loadAdminAlerts(), loadAlertPrefs()]);
  $("#alert-delete-btn").onclick = async () => {
    const chks = document.querySelectorAll(".alert-chk:checked");
    if (chks.length === 0) return;
    if (!confirm(T('alertDeleteConfirm').replace("{n}", String(chks.length)))) return;
    const ids = Array.from(chks).map((c) => parseInt(c.dataset.id, 10));
    try {
      const resp = await api("/api/admin/alerts", { method: "DELETE", body: { ids } });
      toast(T('alertDeleted').replace("{n}", String(resp.deleted || 0)));
      alertPager.page = 1;
      await loadAdminAlerts();
    } catch (err) {
      toast(T('error').replace("{msg}", err.message), 3000);
    }
  };
}

function initAdminBulletinsTab() {
  renderAdminBulletins();
}

/* ---------------- admin site: dashboard ---------------- */
async function renderAdminDashboard() {
  destroyAdminLogCharts();
  _adminLogStats = null;
  _coAdminMode = "admin";
  _allLogsMode = "admin";
  $("#nav-user").innerHTML = `${esc(state.me.username)} · <a href="#" id="logout">${T('logout')}</a>`;
  bindLogout("#logout");

  // Tab navigation bar
  const tabs = ["settings", "antiabuse", "users", "levels", "logs", "donations", "alerts", "bulletins"];
  const tabLabels = {
    settings: T('adminTabSettings'), antiabuse: T('adminTabAntiAbuse'), users: T('adminTabUsers'),
    levels: T('adminTabLevels'),
    logs: T('adminTabLogs'), donations: T('adminTabDonations'), alerts: T('adminTabAlerts'),
    bulletins: T('adminTabBulletins'),
  };
  const tabNav = tabs.map((t, i) =>
    `<button class="admin-tab${i === 0 ? " active" : ""}" data-tab="${t}">${tabLabels[t]}</button>`
  ).join("");

  $("#app").innerHTML = `
    <nav class="tab-nav">${tabNav}</nav>

    <!-- Settings tab -->
    <div id="tab-settings" class="admin-tab-content">
      <section class="card">
        <h3>${T('settingsTitle')}</h3>
        <form id="settings-form">
          <fieldset>
            <legend>${T('settingsLegendMaintenance')}</legend>
            <label style="display:flex;align-items:center;gap:.5rem">
              <input name="maintenance_mode" type="checkbox" role="switch">
              <span>${T('maintenanceMode')}</span>
              <span class="muted" style="font-size:.85em">${T('maintenanceModeHint')}</span>
            </label>
          </fieldset>
          <fieldset>
            <legend>${T('settingsLegendDiscord')}</legend>
            <div style="display:flex;flex-wrap:wrap;gap:.75rem">
              <label style="flex:1 1 16rem">${T('guildID')}<input name="guild_id"></label>
              <label style="flex:1 1 16rem">${T('roleID')}<input name="role_id"></label>
            </div>
          </fieldset>
          <fieldset>
            <legend>${T('settingsLegendRPM')}</legend>
            <div style="display:flex;flex-wrap:wrap;gap:.75rem">
              <label style="flex:1 1 12rem">${T('rpmLimitA')}<input name="rpm_limit_a" type="number" min="1" required></label>
              <label style="flex:1 1 12rem">${T('rpmLimitB')}<input name="rpm_limit_b" type="number" min="1" required></label>
              <label style="flex:1 1 12rem">${T('rpmLimitC')}<input name="rpm_limit_c" type="number" min="1" required></label>
            </div>
            <div style="display:flex;flex-wrap:wrap;gap:.75rem;margin-top:.25rem">
              <label style="flex:1 1 12rem">${T('rpmViolationLimit')}<input name="rpm_violation_limit" type="number" min="1" required></label>
              <label style="flex:1 1 12rem">${T('rpmBanHours')}<input name="rpm_ban_hours" type="number" min="1" required></label>
              <label style="flex:1 1 12rem">${T('probeLimitPerUser')}<input name="probe_limit_per_user" type="number" min="1" required></label>
            </div>
          </fieldset>
          <fieldset>
            <legend>${T('settingsLegendCheckin')}</legend>
            <div style="display:flex;flex-wrap:wrap;gap:.75rem">
              <label style="flex:1 1 10rem">${T('checkinMinLabel')}<input name="checkin_min" type="number" min="1" required></label>
              <label style="flex:1 1 10rem">${T('checkinMaxLabel')}<input name="checkin_max" type="number" min="1" required></label>
              <label style="flex:1 1 10rem">${T('creditsCapLabel')}<input name="credits_cap" type="number" min="0" required></label>
            </div>
          </fieldset>
          <fieldset>
            <legend>${T('settingsLegendCharity')}</legend>
            <label style="display:flex;align-items:center;gap:.5rem;margin-bottom:.25rem">
              <input name="donation_enabled" type="checkbox" role="switch">
              <span>${T('donationEnabled')}</span>
              <span class="muted" style="font-size:.85em">${T('donationEnabledHint')}</span>
            </label>
            <label style="display:flex;align-items:center;gap:.5rem;margin-bottom:.5rem">
              <input name="charity_enabled" type="checkbox" role="switch">
              <span>${T('charityEnabledLabel')}</span>
              <span class="muted" style="font-size:.85em">${T('charityEnabledHint')}</span>
            </label>
            <div style="display:flex;flex-wrap:wrap;gap:.75rem">
              <label style="flex:1 1 12rem">${T('donationReviewLimit')}<input name="donation_review_limit" type="number" min="0" required></label>
              <label style="flex:1 1 12rem">${T('donationFailLimitLabel')}<input name="donation_fail_limit" type="number" min="1" required></label>
            </div>
          </fieldset>
          <fieldset>
            <legend>${T('settingsLegendMailer')}</legend>
            <label style="flex:1 1 14rem">${T('mailerCoolMinutesLabel')}<input name="mailer_cool_minutes" type="number" min="1" required></label>
          </fieldset>
          <button type="submit">${T('save')}</button>
        </form>
      </section>
    </div>

    <!-- Anti-Abuse tab -->
    <div id="tab-antiabuse" class="admin-tab-content" style="display:none">
      <section class="card">
        <h3>${T('adminTabAntiAbuse')}</h3>
        <div class="table-wrap"><table><thead><tr>
          <th>${T('antiAbuseThService')}</th>
          <th>${T('antiAbuseThMode')}</th>
          <th>${T('antiAbuseThMinChars')}</th>
          <th>${T('antiAbuseThPenaltyCredits')}</th>
          <th>${T('antiAbuseThPenaltyBan')}</th>
          <th>${T('antiAbuseThDonationSelect')}</th>
        </tr></thead><tbody id="antiabuse-rows"></tbody></table></div>
        <div class="row-actions" style="margin-top:.5rem">
          <button id="antiabuse-save-all">${T('antiAbuseSave')}</button>
        </div>
      </section>
    </div>

    <!-- Users tab -->
    <div id="tab-users" class="admin-tab-content" style="display:none">
      <section class="card">
        <h3>${T('usersTitle')}</h3>
        <label style="margin-bottom:.75rem">${T('userSearch')}
          <input id="user-search" list="user-search-list" placeholder="${T('userSearchPlaceholder')}" autocomplete="off" style="max-width:24rem">
          <datalist id="user-search-list"></datalist>
        </label>
        <div style="display:flex;flex-wrap:wrap;gap:.5rem;align-items:center;margin-bottom:.75rem" id="batch-ops">
          <select id="batch-action" style="width:auto;margin-bottom:0">
            <option value="">— ${T('batchAction')} —</option>
            <option value="credits-set">${T('batchSet')} ${T('creditsTitle')}</option>
            <option value="credits-add">${T('batchAdd')} ${T('creditsTitle')}</option>
            <option value="credits-sub">${T('batchSub')} ${T('creditsTitle')}</option>
            <option value="dc-set">${T('batchSet')} ${T('thDonationCredit')}</option>
            <option value="dc-add">${T('batchAdd')} ${T('thDonationCredit')}</option>
            <option value="dc-sub">${T('batchSub')} ${T('thDonationCredit')}</option>
          </select>
          <input id="batch-amount" type="number" min="0" placeholder="${T('batchAmount')}" style="width:6rem;margin-bottom:0">
          <button id="batch-submit" class="secondary" style="width:auto;margin-bottom:0">${T('batchSubmit')}</button>
        </div>
        <div class="table-wrap"><table><thead><tr><th><input type="checkbox" id="select-all" title="${T('selectAll')}"></th><th>${T('thUserId')}</th><th>${T('thUser')}</th><th>${T('levelTh')}</th><th>${T('thCredits')}</th><th>${T('thDonationCredit')}</th><th>${T('thRPM')}</th><th>${T('thCreated')}</th><th>${T('thStatus')}</th><th>${T('thActions')}</th></tr></thead><tbody id="user-rows"></tbody></table></div>
        <div class="row-actions" id="user-pager" style="margin-top:.5rem"></div>
      </section>
    </div>

    <!-- Levels tab (R-A) -->
    <div id="tab-levels" class="admin-tab-content" style="display:none">
      <section class="card">
        <h3>${T('adminTabLevels')}</h3>
        <form id="level-settings-form">
          <fieldset>
            <legend>${T('levelThresholds')}</legend>
            <div style="display:flex;flex-wrap:wrap;gap:.75rem">
              <label style="flex:1 1 10rem">${T('levelThresholdTpl').replace('{n}', '2')}<input name="threshold_2" type="number" min="0" required></label>
              <label style="flex:1 1 10rem">${T('levelThresholdTpl').replace('{n}', '3')}<input name="threshold_3" type="number" min="0" required></label>
              <label style="flex:1 1 10rem">${T('levelThresholdTpl').replace('{n}', '4')}<input name="threshold_4" type="number" min="0" required></label>
            </div>
            <p class="muted" style="font-size:.85em;margin:.5rem 0 0">${T('levelThresholdsHint')}</p>
          </fieldset>
          <fieldset>
            <legend>${T('levelNames')}</legend>
            <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(11rem,1fr));gap:.75rem">
              ${[1, 2, 3, 4, 5].map((n) => `<label>Lv.${n}<input name="name_${n}" maxlength="20" placeholder="${T('levelNames')}"></label>`).join("")}
            </div>
          </fieldset>
          <fieldset>
            <legend>${T('levelBanner')}</legend>
            <label>${T('levelBannerHint')}<textarea name="banner_text" rows="2" maxlength="200"></textarea></label>
          </fieldset>
          <div id="level-settings-msg"></div>
          <button type="submit">${T('save')}</button>
        </form>
      </section>
    </div>

    <!-- Logs tab -->
    <div id="tab-logs" class="admin-tab-content" style="display:none">
      <section class="card">
        <h3>${T('adminLogsTitle')}</h3>
        <div id="admin-logs-filter" style="margin-bottom:.8rem">
          <label class="afl-user">${T('thUser')}
            <input id="alf-user" list="alf-user-list" placeholder="${T('adminLogsUserSearch')}" autocomplete="off">
            <datalist id="alf-user-list"></datalist>
          </label>
          <label class="afl-svc">${T('thService')}<select id="alf-service"><option value="">${T('adminLogsAllServices')}</option></select></label>
          <label class="afl-model">${T('adminLogsModel')}<input id="alf-model" placeholder="[公益][general]x"></label>
          <label class="afl-status">${T('thStatus')}<select id="alf-status"><option value="">${T('adminLogsAllStatus')}</option><option value="success">${T('adminLogsSuccess')}</option><option value="error">${T('adminLogsError')}</option></select></label>
          <label class="afl-since">${T('adminLogsSince')}<input id="alf-since" type="datetime-local"></label>
          <label class="afl-until">${T('adminLogsUntil')}<input id="alf-until" type="datetime-local"></label>
          <div class="afl-actions">
            <button id="alf-query">${T('adminLogsQuery')}</button>
            <button id="alf-export" class="secondary outline">${T('adminLogsExport')}</button>
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

    <!-- Donations tab -->
    <div id="tab-donations" class="admin-tab-content" style="display:none">
      <!-- Pricing panel (beta.2) -->
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
        <!-- Donation review section -->
        <div id="donation-review-section" style="margin-bottom:1.5rem;padding:.75rem;border:1px solid var(--pico-muted-border-color);border-radius:4px">
          <h4>${T('donationReviewSection')}</h4>
          <div id="donation-review-content"></div>
        </div>
        <!-- Application history -->
        <div id="donation-app-history-section" style="margin-bottom:1.5rem;padding:.75rem;border:1px solid var(--pico-muted-border-color);border-radius:4px">
          <h4>${T('donationAppHistory')}</h4>
          <div id="don-app-history-filter" style="display:flex;flex-wrap:wrap;gap:.5rem;align-items:end;margin-bottom:.75rem">
            <label style="min-width:8rem">${T('donationAppFilterUser')}<input id="dah-user" list="dah-user-list" placeholder="${T('adminLogsUserSearch')}" autocomplete="off"><datalist id="dah-user-list"></datalist></label>
            <label style="min-width:8rem">${T('donationAppFilterService')}<select id="dah-service"><option value="">${T('donationAppStatusAll')}</option></select></label>
            <label style="min-width:8rem">${T('donationAppFilterStatus')}<select id="dah-status"><option value="">${T('donationAppStatusAll')}</option><option value="pending">${T('donationAppStatusPending')}</option><option value="approved">${T('donationAppStatusApproved')}</option><option value="rejected">${T('donationAppStatusRejected')}</option></select></label>
            <label style="min-width:8rem">${T('donationAppFilterTime')}<input id="dah-since" type="datetime-local" style="min-width:10rem"></label>
            <label style="min-width:8rem"><span style="visibility:hidden">.</span><input id="dah-until" type="datetime-local" style="min-width:10rem"></label>
            <button id="dah-query" class="secondary" style="width:auto;margin:0">${T('adminLogsQuery')}</button>
          </div>
          <div id="donation-app-history-content"></div>
          <div class="row-actions" id="don-app-history-pager" style="margin-top:.5rem"></div>
        </div>
        <form id="donation-form">
          <div style="display:grid;grid-template-columns:${isNarrowScreen()?'1fr':'auto 1fr'};gap:.5rem;align-items:end">
            <label>${T('thService')}<select name="service" id="don-service"></select></label>
            <label>${T('thModel')}<input name="model" placeholder="${T('fieldBackend')}" required></label>
          </div>
          <label>${T('fieldBaseURL')}<input name="dify_base_url" placeholder="https://api.dify.ai/v1" required></label>
          <label>${T('fieldAPIKey')}<input name="dify_api_key" placeholder="app-…" required></label>
          <label>${T('charitySourceUser')}
            <input id="don-source-user" list="don-user-list" placeholder="${T('charitySourceUserHint')}" autocomplete="off">
            <datalist id="don-user-list"></datalist>
          </label>
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
          <label>${T('thUser')}<input id="don-filter-user" list="don-filter-user-list" placeholder="${T('adminLogsUserSearch')}" autocomplete="off"><datalist id="don-filter-user-list"></datalist></label>
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

    <!-- Alerts tab -->
    <div id="tab-alerts" class="admin-tab-content" style="display:none">
      <section class="card">
        <h3>${T('alertTitle')}</h3>
        <div class="table-wrap"><table><thead><tr><th><input type="checkbox" id="alert-select-all" title="${T('selectAll')}"></th><th>${T('thTime')}</th><th>${T('alertType')}</th><th>${T('alertMessage')}</th><th>${T('thActions')}</th></tr></thead><tbody id="alert-rows"></tbody></table></div>
        <div class="row-actions" style="margin:.5rem 0">
          <button id="alert-delete-btn" class="contrast outline">${T('alertDeleteSelected')}</button>
        </div>
        <div class="row-actions" id="alert-pager" style="margin-top:.5rem"></div>
      </section>
      <section class="card">
        <h3>${T('alertPrefsTitle')}</h3>
        <p class="muted" style="margin-bottom:.75rem">${T('alertPrefsHint')}</p>
        <div class="table-wrap"><table><thead><tr>
          <th>${T('alertPrefsThCategory')}</th><th>${T('alertPrefsThTrigger')}</th>
          <th>${T('alertPrefsThCenter')}</th><th>${T('alertPrefsThEmail')}</th>
        </tr></thead><tbody id="alert-prefs-rows"></tbody></table></div>
      </section>
    </div>

    <!-- Bulletins tab -->
    <div id="tab-bulletins" class="admin-tab-content" style="display:none">
      <section class="card">
        <div id="admin-bulletins"></div>
      </section>
    </div>`;

  // Bind tab switching
  document.querySelectorAll(".admin-tab").forEach((btn) => {
    btn.onclick = () => switchAdminTab(btn.dataset.tab);
  });

  // Reset tab state for fresh render
  for (const k of Object.keys(_adminTabLoaded)) delete _adminTabLoaded[k];
  _adminTabLoaded.settings = true;
  _adminCommonData = null;
  _userSearchBound = false;
  _allAdminUsers = [];

  // Load settings tab (always visible first, no lazy loading needed)
  const s = await api("/api/admin/settings");
  const sf = $("#settings-form");
  sf.guild_id.value = s.guild_id || "";
  sf.role_id.value = s.role_id || "";
  sf.rpm_limit_a.value = s.rpm_limit_a;
  sf.rpm_limit_b.value = s.rpm_limit_b;
  sf.rpm_limit_c.value = s.rpm_limit_c;
  sf.rpm_violation_limit.value = s.rpm_violation_limit;
  sf.rpm_ban_hours.value = s.rpm_ban_hours;
  sf.probe_limit_per_user.value = s.probe_limit_per_user;
  sf.checkin_min.value = s.checkin_min;
  sf.checkin_max.value = s.checkin_max;
  sf.credits_cap.value = s.credits_cap;
  sf.donation_fail_limit.value = s.donation_fail_limit;
  sf.donation_review_limit.value = s.donation_review_limit;
  sf.mailer_cool_minutes.value = s.mailer_cool_minutes;
  sf.donation_enabled.checked = s.donation_enabled;
  sf.charity_enabled.checked = s.charity_enabled;
  sf.maintenance_mode.checked = s.maintenance_mode;
  sf.onsubmit = async (e) => {
    e.preventDefault();
    await api("/api/admin/settings", { method: "PUT", body: {
      guild_id: sf.guild_id.value.trim(),
      role_id: sf.role_id.value.trim(),
      rpm_limit_a: parseInt(sf.rpm_limit_a.value, 10),
      rpm_limit_b: parseInt(sf.rpm_limit_b.value, 10),
      rpm_limit_c: parseInt(sf.rpm_limit_c.value, 10),
      rpm_violation_limit: parseInt(sf.rpm_violation_limit.value, 10),
      rpm_ban_hours: parseInt(sf.rpm_ban_hours.value, 10),
      probe_limit_per_user: parseInt(sf.probe_limit_per_user.value, 10),
      checkin_min: parseInt(sf.checkin_min.value, 10),
      checkin_max: parseInt(sf.checkin_max.value, 10),
      credits_cap: parseInt(sf.credits_cap.value, 10) || 0,
      donation_fail_limit: parseInt(sf.donation_fail_limit.value, 10),
      donation_review_limit: parseInt(sf.donation_review_limit.value, 10) || 0,
      mailer_cool_minutes: parseInt(sf.mailer_cool_minutes.value, 10),
      donation_enabled: sf.donation_enabled.checked,
      charity_enabled: sf.charity_enabled.checked,
      maintenance_mode: sf.maintenance_mode.checked,
    } });
    toast(T('settingsSaved'));
  };
}

function userStatusBadges(u) {
  if (u.disabled) {
    let txt = T('statusBannedPerm');
    if (u.ban_reason) txt += ` (${esc(u.ban_reason)})`;
    return `<span class="badge err">${txt}</span>`;
  }
  if (u.banned_until > 0 && u.banned_until * 1000 > Date.now()) {
    let txt = (u.auto_banned ? T('rpmAutoBanPrefix') : "") + T('statusBannedUntil').replace("{time}", fmtT(u.banned_until));
    if (u.ban_reason) txt += `<br><span class="muted">${T('banReasonLabel').replace('{reason}', esc(u.ban_reason))}</span>`;
    return `<span class="badge warn">${txt}</span>`;
  }
  return `<span class="badge ok">${T('statusNormal')}</span>`;
}

const userPager = newPager(userRow);
const adminLogPager = newPager(adminLogRow);
const alertPager = newPager(alertRow);
// Users fetched for the admin-log filter (username → id resolution).
let adminLogUsers = [];
// Jump target set by the alert center's "view linked request" action:
// { logId, userId } — consumed by loadAdminLogs/renderAdminLogs.
let alertJump = null;

// resolveLogUserFilter maps the free-text user filter to a user id.
// Returns { id } on success, { id: null } for "all" (empty input), or
// { error } when the text matches no user.
function resolveLogUserFilter(text) {
  const q = text.trim();
  if (!q) return { id: null };
  // Datalist form: "username（discord_id） [id]". The trailing [id] is
  // optional so the older "username（discord_id）" format still parses.
  const m = q.match(/^(.*)（([^（）]*)）(?: \[(\d+)\])?$/);
  if (m) {
    const hit = adminLogUsers.find((u) => u.username === m[1] && u.discord_id === m[2]);
    if (hit && (!m[3] || String(hit.id) === m[3])) return { id: hit.id };
  }
  // Fallbacks: exact username, exact discord id, numeric user id.
  const byName = adminLogUsers.filter((u) => u.username === q);
  if (byName.length === 1) return { id: byName[0].id };
  const byDiscord = adminLogUsers.find((u) => u.discord_id === q);
  if (byDiscord) return { id: byDiscord.id };
  if (/^\d+$/.test(q)) {
    const byId = adminLogUsers.find((u) => String(u.id) === q);
    if (byId) return { id: byId.id };
  }
  return { error: T('adminLogsUserNotFound').replace("{name}", q) };
}

function closeDropdownMenus() {
  document.querySelectorAll(".dropdown-menu").forEach((menu) => {
    menu.style.display = "none";
    menu.style.visibility = "";
    menu.style.left = "";
    menu.style.top = "";
  });
}

function positionDropdownMenu(trigger, menu) {
  const viewportGap = 8;
  const triggerGap = 4;

  // Render invisibly first so the menu can be measured without flashing at
  // its previous position. position:fixed keeps it outside table-wrap's clip.
  menu.style.visibility = "hidden";
  menu.style.display = "block";

  const triggerRect = trigger.getBoundingClientRect();
  const menuRect = menu.getBoundingClientRect();
  const maxLeft = Math.max(viewportGap, window.innerWidth - menuRect.width - viewportGap);
  const left = Math.min(Math.max(triggerRect.right - menuRect.width, viewportGap), maxLeft);

  const below = triggerRect.bottom + triggerGap;
  const above = triggerRect.top - menuRect.height - triggerGap;
  let top = below;
  if (below + menuRect.height > window.innerHeight - viewportGap && above >= viewportGap) {
    top = above;
  }
  const maxTop = Math.max(viewportGap, window.innerHeight - menuRect.height - viewportGap);
  top = Math.min(Math.max(top, viewportGap), maxTop);

  menu.style.left = `${Math.round(left)}px`;
  menu.style.top = `${Math.round(top)}px`;
  menu.style.visibility = "";
}

function repositionOpenDropdownMenus() {
  document.querySelectorAll(".dropdown-menu").forEach((menu) => {
    if (menu.style.display !== "block") return;
    const trigger = menu.closest(".dropdown-wrapper")?.querySelector(".dropdown-trigger");
    if (trigger) positionDropdownMenu(trigger, menu);
  });
}

// R-A: resolve a level name from the cached settings (fallback: number).
function levelNameFor(n) {
  return (_levelSettings && _levelSettings["name_" + n]) || String(n);
}

function userRow(u) {
  const fmtLim = (v) => (v == null ? "" : String(v));
  const rpm = `
    <span style="display:inline-flex;align-items:center;gap:2px">
      <input class="u-rpm" data-id="${u.id}" data-class="a" type="number" min="1" value="${fmtLim(u.rpm_limit_a)}" placeholder="${T('rpmA')}" style="width:3.5rem;padding:0 .25rem;font-size:.75rem;margin-bottom:0">
      <input class="u-rpm" data-id="${u.id}" data-class="b" type="number" min="1" value="${fmtLim(u.rpm_limit_b)}" placeholder="${T('rpmB')}" style="width:3.5rem;padding:0 .25rem;font-size:.75rem;margin-bottom:0">
      <input class="u-rpm" data-id="${u.id}" data-class="c" type="number" min="1" value="${fmtLim(u.rpm_limit_c)}" placeholder="${T('rpmC')}" style="width:3.5rem;padding:0 .25rem;font-size:.75rem;margin-bottom:0">
      <button class="secondary u-rpm-save" data-id="${u.id}" style="padding:.3rem .5rem;font-size:.8rem">${T('rpmSave')}</button>
    </span>`;
  const titleTxt = esc(`${u.username}（${u.discord_id}）`);
  // R-A level column: default/auto shows "default · name" (tooltip explains
  // auto mode), manual shows the name plus a manual marker. Inline controls
  // set a manual override (1-5) or restore automatic (null).
  const lvlName = levelNameFor(u.level);
  const lvlText = u.level_manual
    ? `${esc(lvlName)} <span class="badge warn">${T('levelManual')}</span>`
    : `<span title="${T('levelAuto')}">${T('levelDefault')} · ${esc(lvlName)}</span>`;
  const lvlOpts = [1, 2, 3, 4, 5].map((n) => `<option value="${n}">${esc(levelNameFor(n))}</option>`).join("");
  return `
    <tr data-id="${u.id}">
      <td><input type="checkbox" class="user-chk" data-id="${u.id}"></td>
      <td class="mono muted">${u.id}</td>
      <td style="max-width:10rem;white-space:nowrap;overflow:hidden;text-overflow:ellipsis" title="${titleTxt}">${esc(u.username)} <span class="id-badge mono" data-copy-id="${esc(u.discord_id)}" title="${T('clickToCopy')}: ${esc(u.discord_id)}" style="cursor:pointer">(${esc(u.discord_id)})</span></td>
      <td class="wrap"><div>${lvlText}</div>
        <div class="row-actions" style="margin-top:.15rem">
          <select class="u-level-set" data-id="${u.id}" title="${T('levelSet')}" style="width:auto;margin:0;padding:.15rem .3rem;font-size:.75rem"><option value="">—</option>${lvlOpts}</select>
          <button class="secondary outline u-level-reset" data-id="${u.id}" title="${T('levelReset')}" style="padding:.15rem .5rem;font-size:.75rem;width:auto;margin:0">${T('levelReset')}</button>
        </div>
      </td>
      <td class="mono">${u.credits != null ? String(u.credits) : "0"}</td>
      <td class="mono">${u.donation_credit != null ? String(u.donation_credit) : "0"}</td>
      <td>${rpm}</td>
      <td class="muted" title="${fmtT(u.created_at)}" style="white-space:nowrap">${fmtDate(u.created_at)}</td>
      <td class="wrap">${userStatusBadges(u)}</td>
      <td><div class="row-actions">
        <button class="secondary u-ban">${T('ban')}</button>
        <button class="secondary u-unban">${T('unban')}</button>
        <div class="dropdown-wrapper">
          <button class="secondary dropdown-trigger" style="width:auto;margin:0;padding:.4rem .5rem">…</button>
          <div class="dropdown-menu">
            <button class="dropdown-item u-key" data-id="${u.id}" style="display:block;width:100%;text-align:left;background:none;border:none;border-radius:0;padding:.4rem .75rem;cursor:pointer;color:var(--pico-color);margin:0;font-size:.85rem">${T('resetUserKey')}</button>
            <button class="dropdown-item u-export" data-id="${u.id}" style="display:block;width:100%;text-align:left;background:none;border:none;border-radius:0;padding:.4rem .75rem;cursor:pointer;color:var(--pico-color);margin:0;font-size:.85rem">${T('adminExport')}</button>
            <button class="dropdown-item u-del" data-id="${u.id}" style="display:block;width:100%;text-align:left;background:none;border:none;border-radius:0;padding:.4rem .75rem;cursor:pointer;color:var(--pico-color);margin:0;font-size:.85rem">${T('deleteUser')}</button>
          </div>
        </div>
      </div></td>
    </tr>`;
}

let _allAdminUsers = [];
let _userSearchBound = false;

function bindUserRowActions() {
  document.querySelectorAll(".u-ban").forEach((b) => (b.onclick = async (e) => {
    const id = e.target.closest("tr").dataset.id;
    const hours = prompt(T('banTimedPrompt'), "");
    if (hours === null) return;
    const raw = hours.trim();
    let body;
    if (raw === "") {
      body = { permanent: true };
    } else {
      const h = parseFloat(raw);
      if (isNaN(h) || h <= 0) { toast(T('banInvalid')); return; }
      body = { until: Math.floor(Date.now() / 1000 + h * 3600) };
    }
    const reason = prompt(T('banReasonPrompt'), "");
    if (reason !== null) body.reason = reason.trim();
    await api(`/api/admin/users/${id}/ban`, { method: "POST", body });
    await loadAdminUsers();
  }));
  document.querySelectorAll(".u-unban").forEach((b) => (b.onclick = async (e) => {
    const id = e.target.closest("tr").dataset.id;
    await api(`/api/admin/users/${id}/unban`, { method: "POST" });
    await loadAdminUsers();
  }));
  // R-A: manual level override (select 1-5) / restore automatic (null).
  document.querySelectorAll(".u-level-set").forEach((sel) => (sel.onchange = async () => {
    const id = sel.dataset.id;
    const v = sel.value;
    try {
      await api(`/api/admin/users/${id}/level`, { method: "PUT", body: { level: v ? parseInt(v, 10) : null } });
      toast(T('settingsSaved'));
      await loadAdminUsers();
    } catch (err) {
      toast(T('error').replace("{msg}", err.message), 3000);
    }
  }));
  document.querySelectorAll(".u-level-reset").forEach((b) => (b.onclick = async () => {
    const id = b.dataset.id;
    try {
      await api(`/api/admin/users/${id}/level`, { method: "PUT", body: { level: null } });
      toast(T('settingsSaved'));
      await loadAdminUsers();
    } catch (err) {
      toast(T('error').replace("{msg}", err.message), 3000);
    }
  }));
  document.querySelectorAll(".u-key").forEach((b) => (b.onclick = async (e) => {
    if (!confirm(T('resetUserKeyConfirm'))) return;
    const id = e.target.closest("tr").dataset.id;
    await api(`/api/admin/users/${id}/reset-key`, { method: "POST" });
    toast(T('settingsSaved'));
  }));
  document.querySelectorAll(".u-del").forEach((b) => (b.onclick = async (e) => {
    if (!confirm(T('deleteUserConfirm'))) return;
    const id = e.target.closest("tr").dataset.id;
    await api(`/api/admin/users/${id}`, { method: "DELETE" });
    await loadAdminUsers();
  }));
  document.querySelectorAll(".u-export").forEach((b) => (b.onclick = async (e) => {
    const id = e.target.closest("tr").dataset.id;
    try {
      const resp = await fetch(`/api/admin/users/${id}/export`, { credentials: "same-origin" });
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
  }));

  // Dropdown toggle binding
  document.querySelectorAll(".dropdown-trigger").forEach((btn) => {
    btn.onclick = (ev) => {
      ev.stopPropagation();
      const menu = btn.closest(".dropdown-wrapper").querySelector(".dropdown-menu");
      const isOpen = menu.style.display === "block";
      closeDropdownMenus();
      if (!isOpen) positionDropdownMenu(btn, menu);
    };
  });
  // Close menu after dropdown item click
  document.querySelectorAll(".dropdown-item").forEach((item) => {
    item.addEventListener("click", () => {
      setTimeout(closeDropdownMenus, 50);
    });
  });

  // Select-all checkbox.
  const selectAll = $("#select-all");
  if (selectAll) {
    selectAll.onclick = () => {
      const chks = document.querySelectorAll(".user-chk");
      chks.forEach((c) => (c.checked = selectAll.checked));
    };
  }

  // Batch operation submit.
  const batchSubmit = $("#batch-submit");
  if (batchSubmit) {
    batchSubmit.onclick = async () => {
      const action = $("#batch-action").value;
      const amount = parseInt($("#batch-amount").value, 10);
      if (!action) { toast(T('batchNoSelection')); return; }
      if (isNaN(amount) || amount < 0) { toast(T('batchInvalidAmount')); return; }
      const chks = document.querySelectorAll(".user-chk:checked");
      if (chks.length === 0) { toast(T('batchNoSelection')); return; }
      const actionLabels = {
        "credits-set": T('batchSet') + " " + T('creditsTitle'),
        "credits-add": T('batchAdd') + " " + T('creditsTitle'),
        "credits-sub": T('batchSub') + " " + T('creditsTitle'),
        "dc-set": T('batchSet') + " " + T('thDonationCredit'),
        "dc-add": T('batchAdd') + " " + T('thDonationCredit'),
        "dc-sub": T('batchSub') + " " + T('thDonationCredit'),
      };
      const label = actionLabels[action] || action;
      if (!confirm(T('batchConfirm').replace("{n}", String(chks.length)).replace("{action}", label).replace("{amount}", String(amount)))) return;
      const userIDs = Array.from(chks).map((c) => parseInt(c.dataset.id, 10));
      let endpoint, bodyAction;
      if (action.startsWith("credits-")) {
        endpoint = "/api/admin/users/credits";
        bodyAction = action.replace("credits-", "");
      } else {
        endpoint = "/api/admin/users/donation_credit";
        bodyAction = action.replace("dc-", "");
      }
      try {
        const resp = await api(endpoint, { method: "POST", body: { user_ids: userIDs, action: bodyAction, amount } });
        toast(T('batchDone').replace("{n}", String(resp.updated || 0)));
        await loadAdminUsers();
      } catch (err) {
        toast(T('error').replace("{msg}", err.message), 3000);
      }
    };
  }
}

function applyUserFilter() {
  const searchEl = $("#user-search");
  const q = searchEl ? searchEl.value.trim().toLowerCase() : "";
  let list = _allAdminUsers;
  if (q) {
    if (/^\d+$/.test(q)) {
      // Pure digits: match the numeric DB id (exact) OR a Discord id
      // (also all digits, exact). Discord-id is matched exactly to stay
      // consistent with resolveLogUserFilter and avoid fragment ambiguity
      // with numeric db ids.
      list = _allAdminUsers.filter((u) =>
        String(u.id) === q ||
        (u.discord_id || "") === q
      );
    } else {
      list = _allAdminUsers.filter((u) =>
        (u.username || "").toLowerCase().includes(q) ||
        (u.discord_id || "").toLowerCase().includes(q)
      );
    }
  }
  userPager.data = list;
  userPager.page = 1;
  userPager.afterRender = () => { bindUserRowActions(); bindIdBadgeClicks(); };
  renderPaged(userPager, "#user-rows", "#user-pager", 10);
}

async function loadAdminUsers() {
  const { users } = await api("/api/admin/users");
  _allAdminUsers = users || [];
  // Bind search input on first call
  if (!_userSearchBound) {
    _userSearchBound = true;
    const searchEl = $("#user-search");
    if (searchEl) searchEl.oninput = () => applyUserFilter();
  }
  applyUserFilter();
}

/* ---------------- admin site: request logs ---------------- */
function adminLogRow(l) {
  const userCell = l.username
    ? esc(l.username)
    : esc(String(l.user_id)) + ` <span class="muted">${T('adminLogsDeletedUser')}</span>`;
  const dur = l.ended_at && l.started_at ? ((l.ended_at - l.started_at) * 1000).toFixed(0) + "ms" : "—";
  const statusClass = l.status === "success" ? "ok" : "err";
  const statusText = l.status === "success" ? T('adminLogsSuccess') : T('adminLogsError');
  const donationSrc = l.source_display || (l.donation_id ? "—" : "");
  return `
    <tr data-id="${l.id}">
      <td class="muted">${fmtT(l.started_at)}</td>
      <td>${userCell}</td>
      <td class="mono">${esc(l.model)}</td>
      <td>${esc(l.service)}</td>
      <td class="muted">${esc(dur)}</td>
      <td><span class="badge ${statusClass}">${statusText}</span></td>
      <td class="mono muted">${l.http_status ? esc(String(l.http_status)) : "—"}</td>
      <td class="mono muted">${esc(l.error_code)}</td>
      <td class="muted wrap" style="max-width:48rem">${esc(l.error_detail || "")}</td>
      <td class="mono muted">${l.credits_consumed ? esc(String(l.credits_consumed)) : "0"}</td>
      <td class="mono muted" style="max-width:16rem;overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="${esc(l.anti_abuse_info || "")}">${esc(l.anti_abuse_info || "")}</td>
      <td class="muted">${esc(donationSrc)}</td>
    </tr>`;
}

async function loadAdminLogs() {
  const params = new URLSearchParams();
  // The level-5 all-logs tab (user site) has no user filter input.
  const userEl = $("#alf-user");
  // When jumping from an alert, use the alert's user id directly: the
  // server-side user_id filter works for deleted users too, while the
  // client-side resolver only knows live users.
  const resolved = alertJump && alertJump.userId
    ? { id: Number(alertJump.userId) }
    : userEl ? resolveLogUserFilter(userEl.value) : { id: null };
  if (resolved.error) {
    $("#alf-rows").innerHTML = `<tr><td colspan="12" class="muted">${esc(resolved.error)}</td></tr>`;
    $("#alf-pager").innerHTML = "";
    return;
  }
  if (resolved.id !== null) params.set("user_id", String(resolved.id));
  const svc = $("#alf-service").value;
  if (svc) params.set("service", svc);
  const model = $("#alf-model").value.trim();
  if (model) params.set("model", model);
  const st = $("#alf-status").value;
  if (st) params.set("status", st);
  const since = $("#alf-since").value;
  if (since) params.set("since", String(Math.floor(new Date(since).getTime() / 1000)));
  const until = $("#alf-until").value;
  if (until) params.set("until", String(Math.floor(new Date(until).getTime() / 1000)));
  // Server-side pagination: "全部" uses a large value (no server cap).
  // If the total exceeds it, truncatedListNote surfaces the truncation.
  const size = adminLogPager.size === Infinity ? MAX_SERVER_ROWS : adminLogPager.size;
  params.set("limit", String(size));
  params.set("offset", String((adminLogPager.page - 1) * size));

  try {
    const data = await api(`${allLogsPath("/api/admin/logs")}?${params.toString()}`);
    renderAdminLogs(data);
  } catch (err) {
    $("#alf-rows").innerHTML = `<tr><td colspan="12" class="muted">${T('error').replace("{msg}", err.message)}</td></tr>`;
    $("#alf-pager").innerHTML = "";
  }
}

function renderAdminLogs(data) {
  const { logs, total } = data;
  if (!logs) return;

  const size = adminLogPager.size;
  const pages = size === Infinity ? 1 : Math.max(1, Math.ceil(total / size));
  adminLogPager.page = Math.min(Math.max(1, adminLogPager.page), pages);

  $("#alf-rows").innerHTML = logs.length
    ? logs.map(adminLogRow).join("")
    : `<tr><td colspan="12" class="muted">${T('empty')}</td></tr>`;

  $("#alf-pager").innerHTML = `
    <select class="pg-size">
      ${[5, 10, 20, 50].map((n) => `<option value="${n}" ${size === n ? "selected" : ""}>${n} ${T('paginationPerPage')}</option>`).join("")}
      <option value="inf" ${size === Infinity ? "selected" : ""}>${T('paginationAll')}</option>
    </select>
    <button class="pg-prev secondary" ${adminLogPager.page <= 1 ? "disabled" : ""}>‹</button>
    <span class="muted">${T('paginationInfo').replace('{page}', String(adminLogPager.page)).replace('{pages}', String(pages)).replace('{total}', String(total))}</span>
    <button class="pg-next secondary" ${adminLogPager.page >= pages ? "disabled" : ""}>›</button>
    ${truncatedListNote(total, size)}`;

  const c = $("#alf-pager");
  c.querySelector(".pg-size").onchange = (e) => {
    adminLogPager.size = e.target.value === "inf" ? Infinity : parseInt(e.target.value, 10);
    adminLogPager.page = 1;
    loadAdminLogs();
  };
  c.querySelector(".pg-prev").onclick = () => { adminLogPager.page--; loadAdminLogs(); };
  c.querySelector(".pg-next").onclick = () => { adminLogPager.page++; loadAdminLogs(); };

  // When jumping from an alert, highlight the linked request row (if it is
  // within the loaded range) and then forget the jump target.
  if (alertJump) {
    const row = document.querySelector(`#alf-rows tr[data-id="${alertJump.logId}"]`);
    if (row) {
      row.scrollIntoView({ behavior: "smooth", block: "center" });
      row.style.outline = "2px solid var(--pico-primary, #1095c1)";
      row.style.background = "var(--pico-mark-background-color, #fff3bf)";
      setTimeout(() => { row.style.outline = ""; row.style.background = ""; }, 3000);
    } else {
      toast(T('alertLinkedRequestNotFound'));
    }
    alertJump = null;
  }
}

async function onExportLogs() {
  const params = new URLSearchParams();
  const userEl = $("#alf-user");
  const resolved = userEl ? resolveLogUserFilter(userEl.value) : { id: null };
  if (resolved.id !== null) params.set("user_id", String(resolved.id));
  const svc = $("#alf-service").value; if (svc) params.set("service", svc);
  const model = $("#alf-model").value.trim(); if (model) params.set("model", model);
  const st = $("#alf-status").value; if (st) params.set("status", st);
  const since = $("#alf-since").value; if (since) params.set("since", String(Math.floor(new Date(since).getTime() / 1000)));
  const until = $("#alf-until").value; if (until) params.set("until", String(Math.floor(new Date(until).getTime() / 1000)));
  // Prompt for format.
  const fmt = confirm(T('adminLogsExportCSVPrompt')) ? "csv" : "json";
  params.set("format", fmt);
  try {
    const resp = await fetch(`/api/admin/logs/export?${params.toString()}`, { credentials: "same-origin" });
    if (!resp.ok) {
      const err = await resp.json().catch(() => ({ error: { message: resp.statusText } }));
      throw new Error((err.error && err.error.message) || resp.statusText);
    }
    const blob = await resp.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    const disp = resp.headers.get("Content-Disposition") || "";
    const m = disp.match(/filename="?([^"]+)"?/);
    a.download = m ? m[1] : `dify2api-logs.${fmt}`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
    toast(T('adminLogsExported'));
  } catch (err) {
    toast(T('error').replace("{msg}", err.message), 3000);
  }
}

let _adminLogStatsRequest = 0;

// Merge UTC hour buckets into local-timezone days. Missing days are filled by
// walking the local calendar (setDate), which is DST-safe: a fixed 86400s step
// would land on the wrong local day across a DST transition.
function mergeHourStatsToLocalDays(byHour) {
  const pad = (n) => String(n).padStart(2, "0");
  const dayKey = (d) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
  const days = new Map();
  let minMs = Infinity;
  let maxMs = -Infinity;
  for (const h of byHour) {
    const ms = Number(h.hour_unix) * 1000;
    if (!Number.isFinite(ms) || ms <= 0) continue;
    const d = new Date(ms);
    const key = dayKey(d);
    const cur = days.get(key) || { date: key, success: 0, error: 0 };
    cur.success += Number(h.success) || 0;
    cur.error += Number(h.error) || 0;
    days.set(key, cur);
    if (ms < minMs) minMs = ms;
    if (ms > maxMs) maxMs = ms;
  }
  if (days.size === 0) return [];
  const out = [];
  const cursor = new Date(minMs);
  cursor.setHours(0, 0, 0, 0); // local midnight of the first day
  const last = new Date(maxMs);
  last.setHours(23, 59, 59, 999);
  while (cursor <= last) {
    const key = dayKey(cursor);
    out.push(days.get(key) || { date: key, success: 0, error: 0 });
    cursor.setDate(cursor.getDate() + 1); // local calendar day increment
  }
  return out;
}

async function loadAdminLogStats() {
  if (!$("#alf-chart-area")) return;
  // Mirror loadAdminLogs' filter parsing (no pagination): the chart must
  // follow the exact same filter semantics as the table, and a bad filter
  // must not silently fall back to the full dataset.
  const params = new URLSearchParams();
  const userEl = $("#alf-user");
  const resolved = userEl ? resolveLogUserFilter(userEl.value) : { id: null };
  if (resolved.error) {
    _adminLogStats = null;
    hideAdminLogCharts();
    return;
  }
  if (resolved.id !== null) params.set("user_id", String(resolved.id));
  const svc = $("#alf-service").value;
  if (svc) params.set("service", svc);
  const model = $("#alf-model").value.trim();
  if (model) params.set("model", model);
  const st = $("#alf-status").value;
  if (st) params.set("status", st);
  const since = $("#alf-since").value;
  if (since) params.set("since", String(Math.floor(new Date(since).getTime() / 1000)));
  const until = $("#alf-until").value;
  if (until) params.set("until", String(Math.floor(new Date(until).getTime() / 1000)));

  // Stale-response guard: rapid consecutive queries must never let an older
  // in-flight response overwrite a newer one (or its chart state).
  const request = ++_adminLogStatsRequest;
  let data;
  try {
    data = await api(`${allLogsPath("/api/admin/logs/stats")}?${params.toString()}`);
  } catch {
    if (request !== _adminLogStatsRequest) return;
    _adminLogStats = null;
    hideAdminLogCharts();
    return;
  }
  if (request !== _adminLogStatsRequest) return;

  _adminLogStats = { by_hour: Array.isArray(data.by_hour) ? data.by_hour : [] };
  if (_adminLogStats.by_hour.length === 0) {
    hideAdminLogCharts();
    return;
  }
  try {
    await renderAdminLogCharts(_adminLogStats);
  } catch {
    reportAdminLogChartFailure();
  }
}

function buildAdminLogChartSummary(byDay) {
  const daily = byDay.map((d) =>
    `${d.date}: ${T('adminLogsSuccess')} ${Number(d.success) || 0}, ${T('adminLogsError')} ${Number(d.error) || 0}`
  ).join("; ");
  return `${T('adminLogsChartSummary')}: ${daily}.`;
}

async function renderAdminLogCharts(stats) {
  destroyAdminLogCharts();
  const generation = _adminLogChartGeneration;
  const ChartJS = await loadChartJS();

  const area = $("#alf-chart-area");
  const dayCanvas = $("#alf-day-chart");
  const summary = $("#alf-chart-summary");
  if (generation !== _adminLogChartGeneration || !area || !dayCanvas || !adminLogsTabVisible()) return;

  const byDay = mergeHourStatsToLocalDays(stats.by_hour || []);
  if (byDay.length === 0) {
    area.style.display = "none";
    return;
  }

  const rootStyle = getComputedStyle(document.documentElement);
  const textColor = rootStyle.getPropertyValue("--pico-muted-color").trim() || "#666";
  const gridColor = rootStyle.getPropertyValue("--pico-muted-border-color").trim() || "#ccc";
  const tickSize = window.innerWidth <= 480 ? 10 : 12;
  const tickStyle = { color: textColor, font: { size: tickSize } };
  const gridStyle = { color: gridColor };

  dayCanvas.setAttribute("aria-label", T('adminLogsDailyChartAria'));
  summary.textContent = buildAdminLogChartSummary(byDay);
  area.style.display = "";

  const dayChart = new ChartJS(dayCanvas, {
    type: "bar",
    data: {
      labels: byDay.map((d) => d.date && d.date.length >= 10 ? d.date.substring(5) : d.date),
      datasets: [
        { label: T('adminLogsSuccess'), data: byDay.map((d) => Number(d.success) || 0), backgroundColor: "#1a7f1a" },
        { label: T('adminLogsError'), data: byDay.map((d) => Number(d.error) || 0), backgroundColor: "#b02020" },
      ],
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: "index", intersect: false },
      scales: {
        x: { stacked: true, ticks: tickStyle, grid: { display: false } },
        y: { stacked: true, beginAtZero: true, ticks: { ...tickStyle, precision: 0 }, grid: gridStyle, title: { display: true, text: T('adminLogsRequests'), color: textColor } },
      },
      plugins: {
        tooltip: { mode: "index", intersect: false },
        legend: { position: "bottom", labels: { color: textColor } },
      },
    },
  });
  _adminLogCharts.push(dayChart);
}

/* ---------------- admin site: alert center ---------------- */
function getAlertTypeLabels() {
  return {
    blocking_failed_200: T('alertTypeBlockingFailed200'),
    user_auto_banned: T('alertPrefsCatUserAutoBanned'),
    donation_inactive: T('alertPrefsCatDonationInactive'),
    admin_login_locked: T('alertPrefsCatAdminLoginLocked'),
    pricing_missing: T('alertPrefsCatPricingMissing'),
    debug_abuse: T('alertPrefsCatDebugAbuse'),
  };
}

// Per-category alert prefs: labels + trigger descriptions for the switch table.
function alertPrefMeta() {
  return {
    user_auto_banned:    { label: T('alertPrefsCatUserAutoBanned'),    desc: T('alertPrefsDescUserAutoBanned') },
    donation_inactive:   { label: T('alertPrefsCatDonationInactive'),   desc: T('alertPrefsDescDonationInactive') },
    admin_login_locked:  { label: T('alertPrefsCatAdminLoginLocked'),  desc: T('alertPrefsDescAdminLoginLocked') },
    pricing_missing:     { label: T('alertPrefsCatPricingMissing'),     desc: T('alertPrefsDescPricingMissing') },
    debug_abuse:         { label: T('alertPrefsCatDebugAbuse'),         desc: T('alertPrefsDescDebugAbuse') },
    blocking_failed_200: { label: T('alertPrefsCatBlockingFailed200'), desc: T('alertPrefsDescBlockingFailed200') },
  };
}

async function loadAlertPrefs() {
  try {
    const data = await api("/api/admin/alert-prefs");
    renderAlertPrefs(data.prefs || []);
  } catch (err) {
    $("#alert-prefs-rows").innerHTML = `<tr><td colspan="4" class="muted">${T('error').replace("{msg}", err.message)}</td></tr>`;
  }
}

function renderAlertPrefs(prefs) {
  const tbody = $("#alert-prefs-rows");
  if (!tbody) return;
  const meta = alertPrefMeta();
  if (prefs.length === 0) {
    tbody.innerHTML = `<tr><td colspan="4" class="muted">${T('empty')}</td></tr>`;
    return;
  }
  tbody.innerHTML = prefs.map((p) => {
    const m = meta[p.event_type] || { label: esc(p.event_type), desc: "" };
    return `<tr data-type="${esc(p.event_type)}">
      <td>${m.label}</td>
      <td class="muted wrap" style="max-width:28rem">${m.desc}</td>
      <td><input type="checkbox" role="switch" class="ap-center" ${p.show_in_center ? "checked" : ""} title="${T('alertPrefsThCenter')}"></td>
      <td><input type="checkbox" role="switch" class="ap-email" ${p.email_enabled ? "checked" : ""} title="${T('alertPrefsThEmail')}"></td>
    </tr>`;
  }).join("");

  tbody.querySelectorAll(".ap-center, .ap-email").forEach((tgl) => {
    tgl.onchange = async () => {
      const row = tgl.closest("tr");
      const eventType = row.dataset.type;
      const center = row.querySelector(".ap-center").checked;
      const email = row.querySelector(".ap-email").checked;
      try {
        await api("/api/admin/alert-prefs", {
          method: "PUT",
          body: { prefs: [{ event_type: eventType, show_in_center: center, email_enabled: email }] },
        });
        toast(T('alertPrefsSaved'), 1800);
      } catch (err) {
        tgl.checked = !tgl.checked;
        toast(T('error').replace("{msg}", err.message), 3000);
      }
    };
  });
}

function alertRow(a) {
  const typeLabel = getAlertTypeLabels()[a.type] || esc(a.type);
  let actionsHtml = "";
  if (a.request_log_id) {
    actionsHtml = `<button class="secondary alert-goto" data-log-id="${a.request_log_id}" data-user-id="${a.user_id || ""}" style="width:auto;margin:0">${T('alertLinkedRequest')}</button>`;
  }
  return `
    <tr data-id="${a.id}">
      <td><input type="checkbox" class="alert-chk" data-id="${a.id}"></td>
      <td class="muted">${fmtT(a.created_at)}</td>
      <td><span class="badge warn">${typeLabel}</span></td>
      <td class="wrap" style="max-width:24rem">${esc(a.message)}</td>
      <td><div class="row-actions">${actionsHtml}</div></td>
    </tr>`;
}

async function loadAdminAlerts() {
  const size = alertPager.size === Infinity ? MAX_SERVER_ROWS : alertPager.size;
  const params = new URLSearchParams({
    limit: String(size),
    offset: String((alertPager.page - 1) * size),
  });
  try {
    const data = await api(`/api/admin/alerts?${params.toString()}`);
    renderAdminAlerts(data);
  } catch (err) {
    $("#alert-rows").innerHTML = `<tr><td colspan="5" class="muted">${T('error').replace("{msg}", err.message)}</td></tr>`;
    $("#alert-pager").innerHTML = "";
  }
}

function renderAdminAlerts(data) {
  const { alerts, total } = data;
  if (!alerts) return;

  const size = alertPager.size;
  const pages = size === Infinity ? 1 : Math.max(1, Math.ceil(total / size));
  alertPager.page = Math.min(Math.max(1, alertPager.page), pages);

  $("#alert-rows").innerHTML = alerts.length
    ? alerts.map(alertRow).join("")
    : `<tr><td colspan="5" class="muted">${T('empty')}</td></tr>`;

  $("#alert-pager").innerHTML = `
    <select class="pg-size">
      ${[5, 10, 20, 50].map((n) => `<option value="${n}" ${size === n ? "selected" : ""}>${n} ${T('paginationPerPage')}</option>`).join("")}
      <option value="inf" ${size === Infinity ? "selected" : ""}>${T('paginationAll')}</option>
    </select>
    <button class="pg-prev secondary" ${alertPager.page <= 1 ? "disabled" : ""}>‹</button>
    <span class="muted">${T('paginationInfo').replace('{page}', String(alertPager.page)).replace('{pages}', String(pages)).replace('{total}', String(total))}</span>
    <button class="pg-next secondary" ${alertPager.page >= pages ? "disabled" : ""}>›</button>
    ${truncatedListNote(total, size)}`;

  const c = $("#alert-pager");
  c.querySelector(".pg-size").onchange = (e) => {
    alertPager.size = e.target.value === "inf" ? Infinity : parseInt(e.target.value, 10);
    alertPager.page = 1;
    loadAdminAlerts();
  };
  c.querySelector(".pg-prev").onclick = () => { alertPager.page--; loadAdminAlerts(); };
  c.querySelector(".pg-next").onclick = () => { alertPager.page++; loadAdminAlerts(); };

  // Select-all checkbox.
  const selectAll = $("#alert-select-all");
  if (selectAll) {
    selectAll.onclick = () => {
      const chks = document.querySelectorAll(".alert-chk");
      chks.forEach((c2) => (c2.checked = selectAll.checked));
    };
  }

  // Link-to-request buttons.
  document.querySelectorAll(".alert-goto").forEach((btn) => {
    btn.onclick = () => {
      // Jump to the linked request: switch to the logs tab (it is hidden
      // until activated), pre-fill the user filter (when the user still
      // exists), load the full range so the row is present, then
      // renderAdminLogs highlights it and scrolls it into view.
      const logId = btn.dataset.logId;
      const userId = btn.dataset.userId || "";
      if (!logId) return;
      alertJump = { logId, userId };
      const userExists = userId && adminLogUsers.some((u) => String(u.id) === userId);
      $("#alf-user").value = userExists ? userId : "";
      $("#alf-service").value = "";
      $("#alf-model").value = "";
      $("#alf-status").value = "";
      adminLogPager.size = Infinity; // "全部" — maximise the chance the row is in range
      adminLogPager.page = 1;
      switchAdminTab("logs");
      // First activation: initAdminLogsTab's own loadAdminLogs() consumes
      // alertJump. Already initialised: trigger the load ourselves (a
      // second load here would race with the tab's auto-load otherwise).
      if (_adminTabLoaded.logs) {
        loadAdminLogs();
      }
      $("#admin-logs-filter").scrollIntoView({ behavior: "smooth" });
    };
  });
}

/* ---------------- admin site: anti-abuse ---------------- */

let antiAbuseConfigs = [];

async function initAdminAntiAbuseTab() {
  await loadAntiAbuseConfigs();
}

async function loadAntiAbuseConfigs() {
  try {
    const data = await api("/api/admin/anti-abuse");
    antiAbuseConfigs = data.configs || [];
    renderAntiAbuseRows();
  } catch (err) {
    $("#antiabuse-rows").innerHTML = `<tr><td colspan="6" class="muted">${T('error').replace("{msg}", err.message)}</td></tr>`;
  }
}

function renderAntiAbuseRows() {
  const tbody = $("#antiabuse-rows");
  if (!tbody) return;
  if (antiAbuseConfigs.length === 0) {
    tbody.innerHTML = `<tr><td colspan="6" class="muted">${T('empty')}</td></tr>`;
    return;
  }
  const modeLabels = [
    T('antiAbuseModeOff'),
    T('antiAbuseModeCharity'),
    T('antiAbuseModeAll'),
  ];
  tbody.innerHTML = antiAbuseConfigs.map((c) => {
    const modeOpts = modeLabels.map((label, i) =>
      `<option value="${i}" ${c.mode === i ? "selected" : ""}>${esc(label)}</option>`
    ).join("");
    return `<tr data-service="${esc(c.service)}">
      <td>${esc(c.service)}</td>
      <td><select class="aa-mode">${modeOpts}</select></td>
      <td><input type="number" class="aa-min-chars" value="${c.min_chars}" min="0" style="width:5rem;margin-bottom:0"></td>
      <td><input type="number" class="aa-penalty-credits" value="${c.penalty_deduct_credits}" min="0" style="width:5rem;margin-bottom:0"></td>
      <td><input type="number" class="aa-penalty-ban" value="${c.penalty_ban_hours}" min="0" style="width:5rem;margin-bottom:0"></td>
      <td><input type="checkbox" role="switch" class="aa-donation" ${c.donation_selectable ? "checked" : ""} title="${esc(T('antiAbuseDonationSelectHint'))}"></td>
    </tr>`;
  }).join("");

  const saveBtn = $("#antiabuse-save-all");
  if (saveBtn) {
    saveBtn.onclick = async () => {
      const configs = [];
      document.querySelectorAll("#antiabuse-rows tr").forEach((row) => {
        const svc = row.dataset.service;
        const mode = parseInt(row.querySelector(".aa-mode").value, 10);
        const minChars = parseInt(row.querySelector(".aa-min-chars").value, 10) || 0;
        const penaltyCredits = parseInt(row.querySelector(".aa-penalty-credits").value, 10) || 0;
        const penaltyBan = parseInt(row.querySelector(".aa-penalty-ban").value, 10) || 0;
        const donationSelectable = row.querySelector(".aa-donation").checked ? 1 : 0;
        configs.push({ service: svc, mode, min_chars: minChars, penalty_deduct_credits: penaltyCredits, penalty_ban_hours: penaltyBan, donation_selectable: donationSelectable });
      });
      try {
        const resp = await api("/api/admin/anti-abuse", { method: "PUT", body: { configs } });
        antiAbuseConfigs = resp.configs || [];
        renderAntiAbuseRows();
        toast(T('antiAbuseSaved'));
      } catch (err) {
        toast(T('error').replace("{msg}", err.message), 3000);
      }
    };
  }
}

/* ---------------- admin site: donations (公益资源) ---------------- */
const donPager = newPager(donationRow);

let pricingData = []; // populated by loadPricing

function donationRow(d) {
  const statusMap = { active: T('charityStatusActive'), inactive: T('charityStatusInactive'), expired: T('charityStatusExpired') };
  const statusBadge = `<span class="badge ${d.status === "active" ? "ok" : d.status === "expired" ? "off" : "warn"}">${esc(statusMap[d.status] || d.status)}</span>`;
  const remaining = `${d.remaining_count}/${d.total_count}`;
  const deadline = fmtT(d.deadline);
  let sourceCell = esc(d.source_display || "—");
  if (d.source_discord_id) {
    sourceCell += ` <span class="id-badge mono" data-copy-id="${esc(d.source_discord_id)}" title="${T('clickToCopy')}: ${esc(d.source_discord_id)}" style="cursor:pointer">(${esc(d.source_discord_id)})</span>`;
  }
  const rpmDisplay = esc(String(d.rpm_limit != null ? d.rpm_limit : 10));
  // Key column: show ⚠ when the same API key is shared across multiple donations.
  const keyCell = d.is_dup_key
    ? `<span title="${esc(T('dupKeyWarning'))}" style="cursor:help">⚠</span>`
    : `<span class="muted">—</span>`;
  let actions = "";
  if (d.status !== "expired") {
    actions += `<button class="secondary don-edit" data-id="${d.id}" style="width:auto;margin:0" title="${T('donationEdit')}">✎</button> `;
  }
  if (d.status === "active") {
    actions += `<button class="secondary don-toggle" data-id="${d.id}" data-status="inactive" style="width:auto;margin:0">${T('charityBtnToggleOff')}</button> `;
  } else if (d.status === "inactive") {
    actions += `<button class="secondary don-toggle" data-id="${d.id}" data-status="active" style="width:auto;margin:0">${T('charityBtnToggleOn')}</button> `;
  }
  if (d.status !== "expired") {
    actions += `<button class="contrast outline don-delete" data-id="${d.id}" style="width:auto;margin:0">${T('charityBtnDelete')}</button>`;
  }
  return `<tr><td><input type="checkbox" class="don-chk" data-id="${d.id}"></td><td>${esc(d.service)}</td><td class="mono">${esc(d.model)}</td><td>${sourceCell}</td><td>${keyCell}</td><td>${statusBadge}</td><td>${remaining}</td><td class="mono">${rpmDisplay}</td><td class="muted">${deadline}</td><td class="muted"><div class="wrap" style="max-width:50rem">${esc(d.note || "—")}</div></td><td class="muted"><div class="wrap" style="max-width:50rem">${esc(d.review_note || "—")}</div></td><td><div class="row-actions">${actions}</div></td></tr>`;
}

let _allDonations = [];
let _donFilterBound = false;

async function loadAdminDonations() {
  try {
    const data = await api(coAdminPath("/api/admin/donations"));
    _allDonations = data.donations || [];
    bindDonationFilters();
    applyDonationFilters();
  } catch (err) {
    $("#don-rows").innerHTML = `<tr><td colspan="12" class="muted">${T('error').replace("{msg}", err.message)}</td></tr>`;
    $("#don-pager").innerHTML = "";
  }
}

function bindDonationFilters() {
  if (_donFilterBound) return;
  _donFilterBound = true;
  const svcSet = new Set(_allDonations.map((d) => d.service));
  $("#don-filter-service").innerHTML =
    `<option value="">${T('adminLogsAllServices')}</option>` +
    [...svcSet].map((s) => `<option value="${esc(s)}">${esc(s)}</option>`).join("");
  if (_adminCommonData && _adminCommonData.users) {
    $("#don-filter-user-list").innerHTML = _adminCommonData.users.map(adminUserOption).join("");
  }
  ["#don-filter-status", "#don-filter-service"].forEach((sel) => {
    $(sel).onchange = applyDonationFilters;
  });
  $("#don-filter-q").oninput = applyDonationFilters;
  const userFilter = $("#don-filter-user");
  if (userFilter) userFilter.oninput = applyDonationFilters;
}

// Client-side filter over the fully loaded donation list (same mechanism as
// the users tab search). Status/service are exact; the keyword matches model,
// source text/name and note; the user field resolves via resolveLogUserFilter
// (username / discord id / numeric id) against source_user_id.
function applyDonationFilters() {
  const status = $("#don-filter-status").value;
  const svc = $("#don-filter-service").value;
  const q = ($("#don-filter-q").value || "").trim().toLowerCase();
  const userFilter = $("#don-filter-user");
  const userText = (userFilter ? userFilter.value : "").trim();
  let list = _allDonations;
  if (status) list = list.filter((d) => d.status === status);
  if (svc) list = list.filter((d) => d.service === svc);
  if (q) {
    list = list.filter((d) =>
      (d.model || "").toLowerCase().includes(q) ||
      (d.source_text || "").toLowerCase().includes(q) ||
      (d.source_username || "").toLowerCase().includes(q) ||
      (d.source_discord_id || "").toLowerCase().includes(q) ||
      (d.note || "").toLowerCase().includes(q)
    );
  }
  if (userText) {
    const resolved = resolveLogUserFilter(userText);
    if (!resolved.error && resolved.id !== null) {
      const uid = resolved.id;
      list = list.filter((d) => d.source_user_id === uid);
    }
  }
  donPager.data = list;
  donPager.page = 1;
  donPager.afterRender = () => {
    clearBatchSelection("#don-select-all", ".don-chk", "don-batch-bar");
    bindBatchSelectAll("#don-select-all", ".don-chk", () => refBatchBar("don-batch-bar", ".don-chk"));
    bindIdBadgeClicks();
  };
  renderPaged(donPager, "#don-rows", "#don-pager", 12);
}

async function loadPricing() {
  try {
    const data = await api(coAdminPath("/api/admin/pricing"));
    pricingData = data.pricing || [];
    renderPricingRows();
    // Populate pricing service dropdown from existing donations' services.
    if (donPager.data && donPager.data.length > 0) {
      const svcSet = new Set(donPager.data.map((d) => d.service));
      const sel = $("#pricing-service");
      if (sel) {
        sel.innerHTML = [...svcSet].map((s) => `<option value="${esc(s)}">${esc(s)}</option>`).join("");
      }
    }
  } catch (err) {
    $("#pricing-rows").innerHTML = `<tr><td colspan="7" class="muted">${T('error').replace("{msg}", err.message)}</td></tr>`;
  }
}

function renderPricingRows() {
  const tbody = $("#pricing-rows");
  if (!tbody) return;
  if (pricingData.length === 0) {
    tbody.innerHTML = `<tr><td colspan="7" class="muted">${T('empty')}</td></tr>`;
    return;
  }
  tbody.innerHTML = pricingData.map((p) => `
    <tr>
      <td><input type="checkbox" class="pricing-chk" data-service="${esc(p.service)}" data-model="${esc(p.model)}"></td>
      <td>${esc(p.service)}</td>
      <td class="mono">${esc(p.model)}</td>
      <td class="mono">${esc(String(p.price))}</td>
      <td class="mono">${esc(String(p.reward))}</td>
      <td>
        <input type="checkbox" role="switch" class="pricing-toggle" data-service="${esc(p.service)}" data-model="${esc(p.model)}" ${p.enabled ? "checked" : ""} title="${T('pricingEnabledHint')}">
      </td>
      <td><div class="row-actions">
        <button class="secondary pricing-edit-btn" data-service="${esc(p.service)}" data-model="${esc(p.model)}" data-price="${p.price}" data-reward="${p.reward}">${T('pricingEdit')}</button>
        <button class="contrast outline pricing-delete-btn" data-service="${esc(p.service)}" data-model="${esc(p.model)}">${T('pricingDelete')}</button>
      </div></td>
    </tr>`).join("");

  // Bind toggle switches.
  tbody.querySelectorAll(".pricing-toggle").forEach((tgl) => {
    tgl.onchange = async () => {
      const svc = tgl.dataset.service;
      const mdl = tgl.dataset.model;
      const wantOn = tgl.checked;
      try {
        await api(coAdminPath("/api/admin/pricing"), { method: "PATCH", body: { service: svc, model: mdl, enabled: wantOn } });
        // Update local state.
        const p = pricingData.find((x) => x.service === svc && x.model === mdl);
        if (p) p.enabled = wantOn;
        toast(wantOn ? T('pricingEnabledHint') : T('charityBtnToggleOff'), 1800);
      } catch (err) {
        tgl.checked = !wantOn;
        toast(T('error').replace("{msg}", err.message), 3000);
      }
    };
  });

  // Bind edit buttons.
  tbody.querySelectorAll(".pricing-edit-btn").forEach((btn) => {
    btn.onclick = () => showPricingEditDialog(btn.dataset.service, btn.dataset.model, parseInt(btn.dataset.price, 10), parseInt(btn.dataset.reward, 10));
  });

  // Bind delete buttons.
  tbody.querySelectorAll(".pricing-delete-btn").forEach((btn) => {
    btn.onclick = async () => {
      const svc = btn.dataset.service;
      const mdl = btn.dataset.model;
      if (!confirm(T('deleteConfirm'))) return;
      try {
        await api(coAdminPath("/api/admin/pricing"), { method: "DELETE", body: { service: svc, model: mdl } });
        pricingData = pricingData.filter((x) => !(x.service === svc && x.model === mdl));
        renderPricingRows();
        toast(T('charityDeleted'), 2000);
      } catch (err) {
        toast(T('error').replace("{msg}", err.message), 3000);
      }
    };
  });

  // Batch select-all.
  bindBatchSelectAll("#pricing-select-all", ".pricing-chk", () => refBatchBar("pricing-batch-bar", ".pricing-chk"));
  refBatchBar("pricing-batch-bar", ".pricing-chk");
}

async function showPricingEditDialog(svc, mdl, curPrice, curReward) {
  const old = $("#pricing-edit-dialog");
  if (old) old.remove();

  const dialog = document.createElement("dialog");
  dialog.id = "pricing-edit-dialog";
  document.body.appendChild(dialog);
  dialog.innerHTML = `
    <article>
      <header><h3>${T('pricingEdit')}: ${esc(svc)} / ${esc(mdl)}</h3></header>
      <form id="pricing-edit-form">
        <div style="display:grid;grid-template-columns:${isNarrowScreen()?'1fr':'1fr 1fr'};gap:.5rem">
          <label>${T('pricingThPrice')}<input name="price" type="number" min="0" value="${curPrice}" required></label>
          <label>${T('pricingThReward')}<input name="reward" type="number" min="0" value="${curReward || ""}" placeholder="自动"></label>
        </div>
        <small class="muted">${T('pricingRewardHint')}</small>
        <div id="pricing-edit-msg" style="margin-bottom:.5rem"></div>
        <footer style="display:flex;gap:.5rem;justify-content:flex-end">
          <button type="button" id="pricing-edit-save">${T('save')}</button>
          <button type="button" id="pricing-edit-cancel">${T('cancelEdit')}</button>
        </footer>
      </form>
    </article>`;
  dialog.showModal();

  const close = () => { dialog.close(); dialog.remove(); };
  $("#pricing-edit-cancel").onclick = close;
  dialog.addEventListener("click", (e) => { if (e.target === dialog) close(); });

  $("#pricing-edit-save").onclick = async () => {
    const price = parseInt($("#pricing-edit-form [name=price]").value, 10);
    const rewardRaw = $("#pricing-edit-form [name=reward]").value.trim();
    const reward = rewardRaw !== "" ? parseInt(rewardRaw, 10) : null;
    const msg = $("#pricing-edit-msg");
    msg.innerHTML = `<span class="muted">${T('loading')}</span>`;
    try {
      const resp = await api(coAdminPath("/api/admin/pricing"), { method: "PUT", body: { service: svc, model: mdl, price, reward } });
      // Update local data.
      const idx = pricingData.findIndex((x) => x.service === svc && x.model === mdl);
      if (idx >= 0) {
        pricingData[idx] = resp.pricing;
      } else {
        pricingData.push(resp.pricing);
      }
      renderPricingRows();
      toast(T('settingsSaved'), 2000);
      close();
    } catch (err) {
      msg.innerHTML = `<span class="note err">${T('error').replace("{msg}", err.message)}</span>`;
    }
  };
}

async function onPricingSubmit(e) {
  e.preventDefault();
  const f = e.target;
  const svc = f.service.value.trim();
  const mdl = f.model.value.trim();
  const price = parseInt(f.price.value, 10) || 0;
  const rewardRaw = f.reward.value.trim();
  const reward = rewardRaw !== "" ? parseInt(rewardRaw, 10) : null;
  const note = $("#pricing-note");
  note.innerHTML = `<span class="muted">${T('loading')}</span>`;
  try {
    const resp = await api(coAdminPath("/api/admin/pricing"), { method: "PUT", body: { service: svc, model: mdl, price, reward } });
    // Update local data (upsert).
    const idx = pricingData.findIndex((x) => x.service === svc && x.model === mdl);
    if (idx >= 0) {
      pricingData[idx] = resp.pricing;
    } else {
      pricingData.push(resp.pricing);
    }
    renderPricingRows();
    f.reset();
    note.innerHTML = "";
    toast(T('settingsSaved'), 2000);
  } catch (err) {
    note.innerHTML = `<span class="note err">${T('error').replace("{msg}", esc(err.message))}</span>`;
  }
}

async function onDonationSubmit(e) {
  e.preventDefault();
  const f = e.target;
  // Resolve source user from text+<datalist> (admin site only; the user
  // site co-admin form has no source-user picker).
  const srcEl = $("#don-source-user");
  const userText = srcEl ? srcEl.value.trim() : "";
  let sourceUserId = null;
  if (userText) {
    const m = userText.match(/^(.*)（([^（）]*)）$/);
    if (m) {
      const hit = adminLogUsers.find((u) => u.username === m[1] && u.discord_id === m[2]);
      if (hit) sourceUserId = hit.id;
    }
  }
  // Convert datetime-local to unix seconds.
  const deadline = f.deadline.value ? Math.floor(new Date(f.deadline.value).getTime() / 1000) : 0;
  const rpmLimit = parseInt(f.rpm_limit.value, 10) || 0;
  const body = {
    service: f.service.value,
    model: f.model.value.trim(),
    dify_base_url: f.dify_base_url.value.trim(),
    dify_api_key: f.dify_api_key.value.trim(),
    source_user_id: sourceUserId,
    source_text: f.source_text.value.trim(),
    deadline,
    total_count: parseInt(f.total_count.value, 10),
    rpm_limit: rpmLimit,
    note: f.note.value.trim(),
  };
  const note = $("#don-note");
  note.innerHTML = `<p class="muted">${T('loading')}</p>`;
  try {
    await api(coAdminPath("/api/admin/donations"), { method: "POST", body });
    note.innerHTML = `<div class="note ok">${T('charityCreated')}</div>`;
    f.reset();
    if (srcEl) srcEl.value = "";
    await loadAdminDonations();
  } catch (err) {
    note.innerHTML = `<div class="note err">${T('error').replace("{msg}", esc(err.message))}</div>`;
  }
}

/* ---------------- admin donation review ---------------- */

// Refresh the donation list after review actions. Level-4 co-admins have
// no donations panel, so the list endpoint is out of reach for them; the
// admin site and level-5 co-admins always reload.
function coAdminReloadDonations() {
  if (_coAdminMode === "me" && (state.me?.level || 0) < 5) return Promise.resolve();
  return loadAdminDonations();
}

async function renderAdminDonationReview() {
  const container = $("#donation-review-content");
  if (!container) return;

  try {
    const resp = await api(coAdminPath("/api/admin/donations/pending"));
    const apps = resp.applications || [];

    if (apps.length === 0) {
      container.innerHTML = `<p class="muted">${T('donationReviewNoPending')}</p>`;
      // Also update the section header.
      const h4 = document.querySelector("#donation-review-section h4");
      if (h4) h4.textContent = T('donationReviewSection');
      return;
    }

    const h4 = document.querySelector("#donation-review-section h4");
    if (h4) h4.textContent = T('donationAppThPendingCount').replace("{n}", String(apps.length));

    // Batch action bar.
    let html = `<div id="don-review-batch-bar" style="display:none;gap:.5rem;align-items:center;margin-bottom:.5rem">
      <button class="secondary batch-review-approve" style="width:auto;margin:0">${T('batchApprove')}</button>
      <button class="contrast outline batch-review-reject" style="width:auto;margin:0">${T('batchReject')}</button>
    </div>
    <div class="table-wrap"><table><thead><tr>
      <th><input type="checkbox" id="review-select-all" title="${T('batchSelectAll')}"></th>
      <th>${T('donationAppThApplicant')}</th><th>${T('donationAppThService')}</th><th>${T('donationAppThModel')}</th>
      <th>${T('donationAppThDeadline')}</th><th>${T('donationAppThCount')}</th><th>${T('donationAppThNote')}</th>
      <th>${T('adminReviewNote')}</th><th>${T('donationAppThCreated')}</th><th>${T('thActions')}</th>
    </tr></thead><tbody>`;
    for (const a of apps) {
      const applicant = `${esc(a.username || String(a.user_id))} <span class="id-badge mono" data-copy-id="${esc(a.discord_id || "")}" title="${T('clickToCopy')}: ${esc(a.discord_id || "")}" style="cursor:pointer">(${esc(a.discord_id || "")})</span>`;
      html += `<tr>
        <td><input type="checkbox" class="review-chk" data-id="${a.id}"></td>
        <td>${applicant}</td>
        <td>${esc(a.service)}</td><td class="mono">${esc(a.model)}</td>
        <td class="muted">${fmtT(a.deadline)}</td>
        <td class="mono">${esc(String(a.total_count))}</td>
        <td class="muted"><div class="wrap" style="max-width:50rem">${esc(a.note || "—")}</div></td>
        <td class="muted"><div class="wrap" style="max-width:50rem">${esc(a.review_note || "—")}</div></td>
        <td class="muted">${fmtT(a.created_at)}</td>
        <td><div class="row-actions">
          <button class="secondary don-review-btn" data-id="${a.id}">${T('donationReviewBtn')}</button>
        </div></td>
      </tr>`;
    }
    html += `</tbody></table></div>`;
    container.innerHTML = html;

    // Bind batch select-all.
    bindBatchSelectAll("#review-select-all", ".review-chk", () => refBatchBar("don-review-batch-bar", ".review-chk"));

    // Bind review buttons.
    document.querySelectorAll(".don-review-btn").forEach((btn) => {
      btn.onclick = () => {
        const id = parseInt(btn.dataset.id, 10);
        const a = apps.find((x) => x.id === id);
        if (a) showReviewDialog(a);
      };
    });
    bindIdBadgeClicks();
  } catch (err) {
    container.innerHTML = `<p class="note err">${T('error').replace("{msg}", err.message)}</p>`;
  }
}

function showReviewDialog(app) {
  // Remove existing dialog if any.
  const old = $("#review-dialog");
  if (old) old.remove();

  const dialog = document.createElement("dialog");
  dialog.id = "review-dialog";
  document.body.appendChild(dialog);

  dialog.innerHTML = `
    <article>
      <header><h3>${T('donationReviewTitle')}</h3></header>
      <div style="margin-bottom:.5rem">
        <strong>${T('donationAppThApplicant')}:</strong> ${esc(app.username || String(app.user_id))}
        ${app.discord_id ? `<span class="muted mono"> (${esc(app.discord_id)})</span>` : ""}
      </div>
      <p class="muted" style="font-size:.85em">${T('donationReviewModifyHint')}</p>
      <form id="review-form">
        <div style="display:grid;grid-template-columns:${isNarrowScreen()?'1fr':'auto 1fr'};gap:.5rem;align-items:end">
          <label>${T('donationAppThService')}<input name="service" value="${esc(app.service)}"></label>
          <label>${T('donationAppThModel')}<input name="model" value="${esc(app.model)}"></label>
        </div>
        <label>${T('fieldBaseURL')}<input name="dify_base_url" value="${esc(app.dify_base_url)}"></label>
        <label>${T('fieldAPIKey')}<input name="dify_api_key" placeholder="${T('reviewKeepOriginalKey')}"></label>
        <div style="display:grid;grid-template-columns:${isNarrowScreen()?'1fr':'1fr 1fr 1fr'};gap:.5rem">
          <label>${T('donationAppThDeadline')}<input name="deadline" type="datetime-local" value="${fmtLocalDT(app.deadline)}"></label>
          <label>${T('donationAppThCount')}<input name="total_count" type="number" min="1" value="${esc(String(app.total_count))}"></label>
          <label>${T('rpmLimitLabel')}<input name="rpm_limit" type="number" min="1" value="${esc(String(app.rpm_limit || 10))}" placeholder="${T('rpmLimitHint')}"></label>
        </div>
        <label>${T('donationReviewNote')}<textarea name="review_note" rows="2"></textarea></label>
        <div id="review-msg" style="margin-bottom:.5rem"></div>
        <footer style="display:flex;gap:.5rem;justify-content:flex-end">
          <button type="button" id="review-approve-btn" class="secondary">${T('donationReviewApprove')}</button>
          <button type="button" id="review-reject-btn" class="contrast outline">${T('donationReviewReject')}</button>
          <button type="button" id="review-cancel-btn">${T('bulletinClose')}</button>
        </footer>
      </form>
    </article>`;
  dialog.showModal();

  const close = () => { dialog.close(); dialog.remove(); };
  $("#review-cancel-btn").onclick = close;
  dialog.addEventListener("click", (e) => { if (e.target === dialog) close(); });

  $("#review-reject-btn").onclick = async () => {
    const note = $("#review-form [name=review_note]").value.trim();
    const msg = $("#review-msg");
    msg.innerHTML = `<span class="muted">${T('loading')}</span>`;
    try {
      await api(coAdminPath(`/api/admin/donations/${app.id}/reject`), { method: "POST", body: { review_note: note } });
      toast(T('donationReviewRejected'));
      close();
      // Refresh both the pending list, history and the donation table.
      await renderAdminDonationReview();
      await loadDonationAppHistory();
      await coAdminReloadDonations();
    } catch (err) {
      msg.innerHTML = `<span class="note err">${T('error').replace("{msg}", err.message)}</span>`;
    }
  };

  $("#review-approve-btn").onclick = async () => {
    const f = $("#review-form");
    const deadline = f.querySelector("[name=deadline]").value ? Math.floor(new Date(f.querySelector("[name=deadline]").value).getTime() / 1000) : 0;
    const rpmLimit = parseInt(f.querySelector("[name=rpm_limit]").value, 10) || 0;
    const body = {
      service: f.querySelector("[name=service]").value.trim(),
      model: f.querySelector("[name=model]").value.trim(),
      dify_base_url: f.querySelector("[name=dify_base_url]").value.trim(),
      dify_api_key: f.querySelector("[name=dify_api_key]").value.trim(),
      total_count: parseInt(f.querySelector("[name=total_count]").value, 10),
      rpm_limit: rpmLimit,
      deadline,
      review_note: f.querySelector("[name=review_note]").value.trim(),
    };
    const msg = $("#review-msg");
    msg.innerHTML = `<span class="muted">${T('loading')}</span>`;
    try {
      await api(coAdminPath(`/api/admin/donations/${app.id}/approve`), { method: "POST", body });
      toast(T('donationReviewApproved'));
      close();
      await renderAdminDonationReview();
      await loadDonationAppHistory();
      await coAdminReloadDonations();
    } catch (err) {
      msg.innerHTML = `<span class="note err">${T('error').replace("{msg}", err.message)}</span>`;
    }
  };
}

// --- Application history ---
const donAppHistoryPager = { page: 1, perPage: 10, total: 0 };

async function loadDonationAppHistory() {
  const container = $("#donation-app-history-content");
  const pagerContainer = $("#don-app-history-pager");
  if (!container) return;

  const userText = $("#dah-user").value.trim();
  let userId = null;
  if (userText) {
    const resolved = resolveLogUserFilter(userText);
    if (resolved.error) {
      container.innerHTML = `<p class="note err">${esc(resolved.error)}</p>`;
      if (pagerContainer) pagerContainer.innerHTML = "";
      return;
    }
    userId = resolved.id;
  }

  const params = new URLSearchParams();
  const statusVal = $("#dah-status").value;
  const serviceVal = $("#dah-service").value;
  const sinceVal = $("#dah-since").value;
  const untilVal = $("#dah-until").value;
  if (statusVal) params.set("status", statusVal);
  if (userId) params.set("user_id", String(userId));
  if (serviceVal) params.set("service", serviceVal);
  if (sinceVal) params.set("since", String(Math.floor(new Date(sinceVal).getTime() / 1000)));
  if (untilVal) params.set("until", String(Math.floor(new Date(untilVal).getTime() / 1000)));
  params.set("limit", String(donAppHistoryPager.perPage));
  params.set("offset", String((donAppHistoryPager.page - 1) * donAppHistoryPager.perPage));

  try {
    const resp = await api(`/api/admin/donations/applications?${params.toString()}`);
    const apps = resp.applications || [];
    donAppHistoryPager.total = resp.total || 0;
    const total = donAppHistoryPager.total;
    const pages = Math.max(1, Math.ceil(total / donAppHistoryPager.perPage));
    donAppHistoryPager.page = Math.min(Math.max(1, donAppHistoryPager.page), pages);

    if (apps.length === 0) {
      container.innerHTML = `<p class="muted">${T('empty')}</p>`;
      if (pagerContainer) pagerContainer.innerHTML = "";
      return;
    }

    const statusBadge = (s) => s === "pending" ? `<span class="badge warn">${T('donationAppStatusPending')}</span>`
      : s === "approved" ? `<span class="badge ok">${T('donationAppStatusApproved')}</span>`
      : `<span class="badge err">${T('donationAppStatusRejected')}</span>`;

    const rows = apps.map((a) => {
      const applicant = `${esc(a.username || String(a.user_id))} <span class="id-badge mono" data-copy-id="${esc(a.discord_id || "")}" title="${T('clickToCopy')}: ${esc(a.discord_id || "")}" style="cursor:pointer">(${esc(a.discord_id || "")})</span>`;
      return `<tr>
        <td>${applicant}</td>
        <td>${esc(a.service)}</td><td class="mono">${esc(a.model)}</td>
        <td>${statusBadge(a.status)}</td>
        <td class="muted"><div class="wrap" style="max-width:50rem">${esc(a.note || "—")}</div></td>
        <td class="muted"><div class="wrap" style="max-width:50rem">${esc(a.review_note || "—")}</div></td>
        <td class="muted">${fmtT(a.created_at)}</td>
      </tr>`;
    }).join("");

    container.innerHTML = `<div class="table-wrap"><table><thead><tr>
      <th>${T('donationAppThUser')}</th><th>${T('donationAppThService')}</th><th>${T('donationAppThModel')}</th>
      <th>${T('donationAppThStatus')}</th><th>${T('donationAppThNote')}</th><th>${T('adminReviewNote')}</th>
      <th>${T('donationAppThCreated')}</th>
    </tr></thead><tbody>${rows}</tbody></table></div>`;

    if (pagerContainer) {
      pagerContainer.innerHTML = `
        <select class="pg-size">
          ${[5, 10, 20, 50].map((n) => `<option value="${n}" ${donAppHistoryPager.perPage === n ? "selected" : ""}>${n} ${T('paginationPerPage')}</option>`).join("")}
        </select>
        <button class="pg-prev secondary" ${donAppHistoryPager.page <= 1 ? "disabled" : ""}>‹</button>
        <span class="muted">${T('paginationInfo').replace('{page}', String(donAppHistoryPager.page)).replace('{pages}', String(pages)).replace('{total}', String(total))}</span>
        <button class="pg-next secondary" ${donAppHistoryPager.page >= pages ? "disabled" : ""}>›</button>`;
      pagerContainer.querySelector(".pg-size").onchange = (e) => {
        donAppHistoryPager.perPage = parseInt(e.target.value, 10);
        donAppHistoryPager.page = 1;
        loadDonationAppHistory();
      };
      pagerContainer.querySelector(".pg-prev").onclick = () => {
        if (donAppHistoryPager.page > 1) { donAppHistoryPager.page--; loadDonationAppHistory(); }
      };
      pagerContainer.querySelector(".pg-next").onclick = () => {
        if (donAppHistoryPager.page < pages) { donAppHistoryPager.page++; loadDonationAppHistory(); }
      };
    }
    bindIdBadgeClicks();
  } catch (err) {
    container.innerHTML = `<p class="note err">${T('error').replace("{msg}", err.message)}</p>`;
    if (pagerContainer) pagerContainer.innerHTML = "";
  }
}

async function showDonationEditDialog(d) {
  // Remove existing dialog if any.
  const old = $("#don-edit-dialog");
  if (old) old.remove();

  const dialog = document.createElement("dialog");
  dialog.id = "don-edit-dialog";
  document.body.appendChild(dialog);

  dialog.innerHTML = `
    <article>
      <header><h3>${T('donationEditTitle').replace("{id}", String(d.id))}</h3></header>
      <p class="muted" style="font-size:.85em">${T('donationReviewModifyHint')}</p>
      <form id="don-edit-form">
        <div style="display:grid;grid-template-columns:${isNarrowScreen()?'1fr':'auto 1fr'};gap:.5rem;align-items:end">
          <label>${T('thService')}<input name="service" value="${esc(d.service)}"></label>
          <label>${T('thModel')}<input name="model" value="${esc(d.model)}"></label>
        </div>
        <label>${T('fieldBaseURL')}<input name="dify_base_url" value="${esc(d.dify_base_url)}"></label>
        <label>${T('fieldAPIKey')}<input name="dify_api_key" placeholder="${T('reviewKeepOriginalKey')}"></label>
        <div style="display:grid;grid-template-columns:${isNarrowScreen()?'1fr':'1fr 1fr 1fr'};gap:.5rem">
          <label>${T('charityDeadline')}<input name="deadline" type="datetime-local" value="${fmtLocalDT(d.deadline)}"></label>
          <label>${T('charityTotalCount')}<input name="total_count" type="number" min="1" value="${esc(String(d.total_count))}"></label>
          <label>${T('rpmLimitLabel')}<input name="rpm_limit" type="number" min="1" value="${esc(String(d.rpm_limit || 10))}" placeholder="${T('rpmLimitHint')}"></label>
        </div>
        <label>${T('adminReviewNote')}<textarea name="review_note" rows="2">${esc(d.review_note || "")}</textarea></label>
        <div id="don-edit-msg" style="margin-bottom:.5rem"></div>
        <footer style="display:flex;gap:.5rem;justify-content:flex-end">
          <button type="button" id="don-edit-save-btn">${T('save')}</button>
          <button type="button" id="don-edit-cancel-btn">${T('cancelEdit')}</button>
        </footer>
      </form>
    </article>`;
  dialog.showModal();

  const close = () => { dialog.close(); dialog.remove(); };
  $("#don-edit-cancel-btn").onclick = close;
  dialog.addEventListener("click", (e) => { if (e.target === dialog) close(); });

  $("#don-edit-save-btn").onclick = async () => {
    const f = $("#don-edit-form");
    const deadline = f.querySelector("[name=deadline]").value ? Math.floor(new Date(f.querySelector("[name=deadline]").value).getTime() / 1000) : 0;
    const rpmLimit = parseInt(f.querySelector("[name=rpm_limit]").value, 10) || 0;
    const totalCount = parseInt(f.querySelector("[name=total_count]").value, 10) || 0;
    const body = {
      service: f.querySelector("[name=service]").value.trim(),
      model: f.querySelector("[name=model]").value.trim(),
      dify_base_url: f.querySelector("[name=dify_base_url]").value.trim(),
      dify_api_key: f.querySelector("[name=dify_api_key]").value.trim(),
      review_note: f.querySelector("[name=review_note]").value.trim(),
    };
    if (deadline) body.deadline = deadline;
    if (totalCount > 0) body.total_count = totalCount;
    if (rpmLimit > 0) body.rpm_limit = rpmLimit;
    const msg = $("#don-edit-msg");
    msg.innerHTML = `<span class="muted">${T('loading')}</span>`;
    try {
      const resp = await api(coAdminPath(`/api/admin/donations/${d.id}`), { method: "PATCH", body });
      if (resp.validation && !resp.validation.compatible) {
        toast(T('donationSavedInvalid').replace("{msg}", esc(resp.validation.message || "")), 3000);
      } else {
        toast(T('donationSavedValid'));
      }
      close();
      await loadAdminDonations();
    } catch (err) {
      msg.innerHTML = `<span class="note err">${T('error').replace("{msg}", err.message)}</span>`;
    }
  };
}

/* ---------------- admin bulletin management ---------------- */

const adminBulletinPager = newPager(adminBulletinRow);

async function showBulletinEditDialog(b) {
  const old = $("#bulletin-edit-dialog");
  if (old) old.remove();

  const dialog = document.createElement("dialog");
  dialog.id = "bulletin-edit-dialog";
  document.body.appendChild(dialog);

  dialog.innerHTML = `
    <article>
      <header><h3>${T('bulletinEditTitle').replace("{id}", String(b.id))}</h3></header>
      <form id="bulletin-edit-form">
        <label>${T('bulletinFieldTitle')}<input name="title" value="${esc(b.title)}" required></label>
        <label>${T('bulletinFieldContent')}<textarea name="content" rows="6" style="font-family:monospace" required>${esc(b.content)}</textarea></label>
        <div style="display:grid;grid-template-columns:${isNarrowScreen()?'1fr':'1fr 1fr'};gap:.75rem">
          <label>${T('bulletinFieldType')}
            <select name="type">
              <option value="info" ${b.type==='info'?'selected':''}>${T('bulletinTypeInfo')}</option>
              <option value="warning" ${b.type==='warning'?'selected':''}>${T('bulletinTypeWarning')}</option>
              <option value="important" ${b.type==='important'?'selected':''}>${T('bulletinTypeImportant')}</option>
            </select>
          </label>
          <label>${T('bulletinFieldContentType')}
            <select name="content_type">
              <option value="html" ${(b.content_type||'html')==='html'?'selected':''}>${T('bulletinContentTypeHTML')}</option>
              <option value="markdown" ${b.content_type==='markdown'?'selected':''}>${T('bulletinContentTypeMarkdown')}</option>
            </select>
          </label>
        </div>
        <div style="display:grid;grid-template-columns:${isNarrowScreen()?'1fr':'1fr 1fr'};gap:.75rem">
          <label>${T('bulletinFieldSortOrder')}<input name="sort_order" type="number" value="${esc(String(b.sort_order))}"></label>
          <label>${T('bulletinFieldLang')}
            <select name="lang">
              <option value="zh" ${(b.lang||'zh')==='zh'?'selected':''}>中文</option>
              <option value="en" ${b.lang==='en'?'selected':''}>English</option>
            </select>
          </label>
        </div>
        <div style="display:flex;flex-wrap:wrap;gap:.75rem;align-items:center">
          <label style="display:flex;align-items:center;gap:.5rem;margin-bottom:0">
            <input name="closable" type="checkbox" role="switch" ${b.closable?'checked':''}>
            <span>${T('bulletinFieldClosable')}</span>
          </label>
          <label style="flex:1">${T('bulletinFieldExpiresAt')}<input name="expires_at" type="datetime-local" value="${b.expires_at ? fmtLocalDT(b.expires_at) : ''}"></label>
        </div>
        <div id="bulletin-edit-msg" style="margin-bottom:.5rem"></div>
        <footer style="display:flex;gap:.5rem;justify-content:flex-end">
          <button type="button" id="bulletin-edit-save">${T('save')}</button>
          <button type="button" id="bulletin-edit-cancel">${T('cancelEdit')}</button>
        </footer>
      </form>
    </article>`;
  dialog.showModal();

  const close = () => { dialog.close(); dialog.remove(); };
  $("#bulletin-edit-cancel").onclick = close;
  dialog.addEventListener("click", (e) => { if (e.target === dialog) close(); });

  $("#bulletin-edit-save").onclick = async () => {
    const f = $("#bulletin-edit-form");
    const expiresVal = f.querySelector('[name="expires_at"]').value;
    const body = {
      title: f.querySelector('[name="title"]').value.trim(),
      content: f.querySelector('[name="content"]').value,
      content_type: f.querySelector('[name="content_type"]').value,
      type: f.querySelector('[name="type"]').value,
      sort_order: parseInt(f.querySelector('[name="sort_order"]').value, 10) || 0,
      closable: f.querySelector('[name="closable"]').checked,
      expires_at: expiresVal ? Math.floor(new Date(expiresVal).getTime() / 1000) : null,
      lang: f.querySelector('[name="lang"]').value,
    };
    const msg = $("#bulletin-edit-msg");
    msg.innerHTML = `<p class="muted">${T('loading')}</p>`;
    try {
      await api(`/api/admin/bulletins/${b.id}`, { method: "PUT", body });
      toast(T('bulletinUpdated'));
      close();
      await loadAdminBulletins();
    } catch (err) {
      msg.innerHTML = `<div class="note err">${T('error').replace("{msg}", esc(err.message))}</div>`;
    }
  };
}

function adminBulletinRow(b) {
  const typeCell = bulletinTypeBadge(b.type);
  const fmtBadge = (b.content_type === 'markdown')
    ? `<span class="badge" style="background:var(--pico-primary);color:var(--pico-primary-inverse)">${T('bulletinContentTypeMarkdown')}</span>`
    : `<span class="badge">${T('bulletinContentTypeHTML')}</span>`;
  const langBadge = `<span class="badge">${esc(b.lang || 'zh').toUpperCase()}</span>`;
  const created = fmtT(b.created_at);
  const expires = b.expires_at ? fmtT(b.expires_at) : T('bulletinNeverExpires');
  const closable = b.closable ? T('bulletinClosableYes') : T('bulletinClosableNo');
  const isSys = b.is_system;
  const actions = isSys
    ? `<span class="muted">${T('bulletinSystemNote')}</span>`
    : `<button class="secondary bull-edit" data-id="${b.id}" style="width:auto;margin:0">${T('bulletinEdit')}</button>
       <button class="contrast outline bull-del" data-id="${b.id}" style="width:auto;margin:0">${T('bulletinDelete')}</button>`;
  return `
    <tr data-id="${b.id}" class="${isSys ? 'muted' : ''}">
      <td><input type="checkbox" class="bulletin-chk" data-id="${b.id}" ${isSys ? 'disabled' : ''}></td>
      <td>${esc(b.title)}</td>
      <td>${typeCell}</td>
      <td>${fmtBadge}</td>
      <td>${langBadge}</td>
      <td class="muted">${created}</td>
      <td class="muted">${expires}</td>
      <td>${closable}</td>
      <td><div class="row-actions">${actions}</div></td>
    </tr>`;
}

async function loadAdminBulletins() {
  try {
    const data = await api("/api/admin/bulletins");
    const list = data.bulletins || [];
    adminBulletinPager.data = list;
    adminBulletinPager.afterRender = () => {
      clearBatchSelection("#bulletin-select-all", ".bulletin-chk", "bulletin-batch-bar");
      bindBatchSelectAll("#bulletin-select-all", ".bulletin-chk:not([disabled])", () => refBatchBar("bulletin-batch-bar", ".bulletin-chk:checked"));
      // Bind edit buttons. Bound here (inside afterRender) so paging the
      // table re-binds the freshly rendered rows instead of losing the
      // handlers after a page change.
      document.querySelectorAll(".bull-edit").forEach((btn) => {
        btn.onclick = () => {
          const id = parseInt(btn.dataset.id, 10);
          const b = adminBulletinPager.data.find((x) => x.id === id);
          if (!b) return;
          showBulletinEditDialog(b);
        };
      });
      // Bind delete buttons.
      document.querySelectorAll(".bull-del").forEach((btn) => {
        btn.onclick = async () => {
          if (!confirm(T('bulletinDeleteConfirm'))) return;
          const id = parseInt(btn.dataset.id, 10);
          try {
            await api(`/api/admin/bulletins/${id}`, { method: "DELETE" });
            toast(T('bulletinDeleted'));
            await loadAdminBulletins();
          } catch (err) {
            toast(T('error').replace("{msg}", err.message), 3000);
          }
        };
      });
    };
    renderPaged(adminBulletinPager, "#admin-bulletin-rows", "#admin-bulletin-pager", 7);
  } catch (err) {
    $("#admin-bulletin-rows").innerHTML = `<tr><td colspan="9" class="muted">${T('error').replace("{msg}", err.message)}</td></tr>`;
    $("#admin-bulletin-pager").innerHTML = "";
  }
}

async function onBulletinSubmit(e) {
  e.preventDefault();
  const f = e.target;
  const expiresVal = f.querySelector('[name="expires_at"]').value;
  const body = {
    title: f.querySelector('[name="title"]').value.trim(),
    content: f.querySelector('[name="content"]').value,
    content_type: f.querySelector('[name="content_type"]').value,
    type: f.querySelector('[name="type"]').value,
    sort_order: parseInt(f.querySelector('[name="sort_order"]').value, 10) || 0,
    closable: f.querySelector('[name="closable"]').checked,
    expires_at: expiresVal ? Math.floor(new Date(expiresVal).getTime() / 1000) : null,
    lang: f.querySelector('[name="lang"]').value,
  };
  const note = $("#admin-bulletin-note");
  note.innerHTML = `<p class="muted">${T('loading')}</p>`;
  try {
    await api("/api/admin/bulletins", { method: "POST", body });
    toast(T('bulletinCreated'));
    f.reset();
    note.innerHTML = "";
    await loadAdminBulletins();
  } catch (err) {
    note.innerHTML = `<div class="note err">${T('error').replace("{msg}", esc(err.message))}</div>`;
  }
}

// renderAdminBulletins: renders the bulletin management page for the admin site.
// Called within the admin "公告管理" tab.
function renderAdminBulletins() {
  const container = $("#admin-bulletins");
  if (!container) return;
  container.innerHTML = `
    <h3>${T('bulletinAdminTitle')}</h3>
    <div id="bulletin-batch-bar" style="display:none;gap:.5rem;align-items:center;margin-bottom:.5rem">
      <button class="contrast outline batch-bulletin-del" style="width:auto;margin:0">${T('batchDelete')}</button>
    </div>
    <div class="table-wrap"><table><thead><tr>
      <th><input type="checkbox" id="bulletin-select-all" title="${T('batchSelectAll')}"></th>
      <th>${T('bulletinThTitle')}</th><th>${T('bulletinThType')}</th><th>${T('bulletinThFormat')}</th><th>${T('bulletinThLang')}</th><th>${T('bulletinThCreated')}</th>
      <th>${T('bulletinThExpires')}</th><th>${T('bulletinThClosable')}</th><th>${T('bulletinThActions')}</th>
    </tr></thead><tbody id="admin-bulletin-rows"></tbody></table></div>
    <div class="row-actions" id="admin-bulletin-pager" style="margin:.5rem 0 1rem"></div>
    <form id="admin-bulletin-form">
      <label>${T('bulletinFieldTitle')}<input name="title" required></label>
      <label>${T('bulletinFieldContent')}<textarea name="content" rows="6" style="font-family:monospace" required></textarea></label>
      <div style="display:grid;grid-template-columns:${isNarrowScreen()?'1fr':'1fr 1fr'};gap:.75rem">
        <label>${T('bulletinFieldType')}
          <select name="type">
            <option value="info">${T('bulletinTypeInfo')}</option>
            <option value="warning">${T('bulletinTypeWarning')}</option>
            <option value="important">${T('bulletinTypeImportant')}</option>
          </select>
        </label>
        <label>${T('bulletinFieldContentType')}
          <select name="content_type">
            <option value="html">${T('bulletinContentTypeHTML')}</option>
            <option value="markdown">${T('bulletinContentTypeMarkdown')}</option>
          </select>
        </label>
      </div>
      <div style="display:grid;grid-template-columns:${isNarrowScreen()?'1fr':'1fr 1fr'};gap:.75rem">
        <label>${T('bulletinFieldSortOrder')}<input name="sort_order" type="number" value="0"></label>
        <label>${T('bulletinFieldLang')}
          <select name="lang">
            <option value="zh">中文</option>
            <option value="en">English</option>
          </select>
        </label>
      </div>
      <div style="display:flex;flex-wrap:wrap;gap:.75rem;align-items:center">
        <label style="display:flex;align-items:center;gap:.5rem;margin-bottom:0">
          <input name="closable" type="checkbox" role="switch" checked>
          <span>${T('bulletinFieldClosable')}</span>
        </label>
        <label style="flex:1">${T('bulletinFieldExpiresAt')}<input name="expires_at" type="datetime-local"></label>
      </div>
      <div id="admin-bulletin-note"></div>
      <button type="submit">${T('bulletinAdd')}</button>
    </form>`;
  const f = $("#admin-bulletin-form");
  f.onsubmit = onBulletinSubmit;
  loadAdminBulletins();
}

window.addEventListener("resize", repositionOpenDropdownMenus);
document.addEventListener("scroll", repositionOpenDropdownMenus, true);

// Delegate click events for donation actions (toggle/delete) and RPM save.
document.addEventListener("click", async (ev) => {
  // Close all dropdown menus when clicking outside a dropdown wrapper.
  if (!ev.target.closest(".dropdown-wrapper")) closeDropdownMenus();

  const rpmBtn = ev.target.closest(".u-rpm-save");
  if (rpmBtn) {
    const id = rpmBtn.dataset.id;
    const aVal = document.querySelector(`.u-rpm[data-id="${id}"][data-class="a"]`);
    const bVal = document.querySelector(`.u-rpm[data-id="${id}"][data-class="b"]`);
    const cVal = document.querySelector(`.u-rpm[data-id="${id}"][data-class="c"]`);
    const clean = (el) => {
      const v = el?.value?.trim();
      if (!v) return null;
      const n = parseInt(v, 10);
      return (isNaN(n) || n < 1) ? null : n;
    };
    try {
      await api(`/api/admin/users/${id}/rpm`, { method: "POST", body: {
        rpm_limit_a: clean(aVal), rpm_limit_b: clean(bVal), rpm_limit_c: clean(cVal),
      } });
      toast(T('settingsSaved'));
      await loadAdminUsers();
    } catch (err) {
      toast(T('error').replace("{msg}", err.message), 3000);
    }
    return;
  }
  const btn = ev.target.closest(".don-toggle");
  if (btn) {
    const id = btn.dataset.id;
    const status = btn.dataset.status;
    try {
      await api(coAdminPath(`/api/admin/donations/${id}/status`), { method: "POST", body: { status } });
      toast(T('charityStatusChanged'));
      await loadAdminDonations();
    } catch (err) {
      toast(T('error').replace("{msg}", err.message), 3000);
    }
    return;
  }
  const delBtn = ev.target.closest(".don-delete");
  if (delBtn) {
    if (!confirm(T('charityDeleteWarn'))) return;
    const id = delBtn.dataset.id;
    try {
      await api(coAdminPath(`/api/admin/donations/${id}`), { method: "DELETE" });
      toast(T('charityDeleted'));
      await loadAdminDonations();
    } catch (err) {
      toast(T('error').replace("{msg}", err.message), 3000);
    }
    return;
  }
  const editBtn = ev.target.closest(".don-edit");
  if (editBtn) {
    const id = parseInt(editBtn.dataset.id, 10);
    const d = donPager.data.find((x) => x.id === id);
    if (d) showDonationEditDialog(d);
    return;
  }

  // --- Batch action handlers ---

  // Donation review batch approve.
  const revApprove = ev.target.closest(".batch-review-approve");
  if (revApprove) {
    const ids = getBatchIds(".review-chk");
    if (ids.length === 0) { toast(T('batchNoSelection'), 2200); return; }
    try {
      await api(coAdminPath("/api/admin/donations/approve/batch"), { method: "POST", body: { ids } });
      toast(T('donationReviewApproved'), 2200);
      clearBatchSelection("#review-select-all", ".review-chk", "don-review-batch-bar");
      await renderAdminDonationReview();
      await loadDonationAppHistory();
      await coAdminReloadDonations();
    } catch (err) { toast(T('error').replace("{msg}", err.message), 3000); }
    return;
  }

  // Donation review batch reject.
  const revReject = ev.target.closest(".batch-review-reject");
  if (revReject) {
    const ids = getBatchIds(".review-chk");
    if (ids.length === 0) { toast(T('batchNoSelection'), 2200); return; }
    try {
      await api(coAdminPath("/api/admin/donations/reject/batch"), { method: "POST", body: { ids } });
      toast(T('donationReviewRejected'), 2200);
      clearBatchSelection("#review-select-all", ".review-chk", "don-review-batch-bar");
      await renderAdminDonationReview();
      await loadDonationAppHistory();
      await coAdminReloadDonations();
    } catch (err) { toast(T('error').replace("{msg}", err.message), 3000); }
    return;
  }

  // Donation batch activate.
  const donActivate = ev.target.closest(".batch-don-activate");
  if (donActivate) {
    const ids = getBatchIds(".don-chk");
    if (ids.length === 0) { toast(T('batchNoSelection'), 2200); return; }
    try {
      await api(coAdminPath("/api/admin/donations/status/batch"), { method: "POST", body: { ids, status: "active" } });
      toast(T('charityStatusChanged'), 2200);
      clearBatchSelection("#don-select-all", ".don-chk", "don-batch-bar");
      await loadAdminDonations();
    } catch (err) { toast(T('error').replace("{msg}", err.message), 3000); }
    return;
  }

  // Donation batch deactivate.
  const donDeactivate = ev.target.closest(".batch-don-deactivate");
  if (donDeactivate) {
    const ids = getBatchIds(".don-chk");
    if (ids.length === 0) { toast(T('batchNoSelection'), 2200); return; }
    try {
      await api(coAdminPath("/api/admin/donations/status/batch"), { method: "POST", body: { ids, status: "inactive" } });
      toast(T('charityStatusChanged'), 2200);
      clearBatchSelection("#don-select-all", ".don-chk", "don-batch-bar");
      await loadAdminDonations();
    } catch (err) { toast(T('error').replace("{msg}", err.message), 3000); }
    return;
  }

  // Donation batch delete.
  const donDelete = ev.target.closest(".batch-don-delete");
  if (donDelete) {
    const ids = getBatchIds(".don-chk");
    if (ids.length === 0) { toast(T('batchNoSelection'), 2200); return; }
    if (!confirm(T('batchConfirmDelete').replace("{n}", String(ids.length)))) return;
    try {
      await api(coAdminPath("/api/admin/donations/delete/batch"), { method: "POST", body: { ids } });
      toast(T('charityDeleted'), 2200);
      clearBatchSelection("#don-select-all", ".don-chk", "don-batch-bar");
      await loadAdminDonations();
    } catch (err) { toast(T('error').replace("{msg}", err.message), 3000); }
    return;
  }

  // Pricing batch delete.
  const pricingDel = ev.target.closest(".batch-pricing-del");
  if (pricingDel) {
    const pairs = getBatchPairs(".pricing-chk");
    if (pairs.length === 0) { toast(T('batchNoSelection'), 2200); return; }
    if (!confirm(T('batchConfirmDelete').replace("{n}", String(pairs.length)))) return;
    try {
      await api(coAdminPath("/api/admin/pricing/delete/batch"), { method: "POST", body: { pairs } });
      toast(T('charityDeleted'), 2200);
      clearBatchSelection("#pricing-select-all", ".pricing-chk", "pricing-batch-bar");
      // Reload pricing data.
      const data = await api(coAdminPath("/api/admin/pricing"));
      pricingData = data.pricing || [];
      renderPricingRows();
    } catch (err) { toast(T('error').replace("{msg}", err.message), 3000); }
    return;
  }

  // Bulletin batch delete.
  const bulletinDel = ev.target.closest(".batch-bulletin-del");
  if (bulletinDel) {
    const ids = getBatchIds(".bulletin-chk");
    if (ids.length === 0) { toast(T('batchNoSelection'), 2200); return; }
    if (!confirm(T('batchConfirmDelete').replace("{n}", String(ids.length)))) return;
    try {
      await api("/api/admin/bulletins/delete/batch", { method: "POST", body: { ids } });
      toast(T('bulletinDeleted'), 2200);
      clearBatchSelection("#bulletin-select-all", ".bulletin-chk", "bulletin-batch-bar");
      await loadAdminBulletins();
    } catch (err) { toast(T('error').replace("{msg}", err.message), 3000); }
    return;
  }
});
