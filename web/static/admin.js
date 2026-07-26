"use strict";

/* ---------------- admin site: login ---------------- */
function renderAdminLogin() {
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
  const [{ users }, { services }] = await Promise.all([
    api("/api/admin/users"),
    api("/api/services"),
  ]);
  _adminCommonData = { users: users || [], services: services || [] };
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
  // Lazy load on first activation
  if (!_adminTabLoaded[tab]) {
    _adminTabLoaded[tab] = true;
    switch (tab) {
      case "users": initAdminUsersTab(); break;
      case "logs": initAdminLogsTab(); break;
      case "donations": initAdminDonationsTab(); break;
      case "alerts": initAdminAlertsTab(); break;
      case "bulletins": initAdminBulletinsTab(); break;
      case "antiabuse": initAdminAntiAbuseTab(); break;
    }
  }
}

async function initAdminUsersTab() {
  await loadAdminCommonData();
  $("#user-search-list").innerHTML = _adminCommonData.users
    .map((u) => `<option value="${esc(u.username)}（${esc(u.discord_id)}）"></option>`)
    .join("");
  await loadAdminUsers();
}

async function initAdminLogsTab() {
  const data = await loadAdminCommonData();
  $("#alf-user-list").innerHTML = data.users
    .map((u) => `<option value="${esc(u.username)}（${esc(u.discord_id)}）"></option>`)
    .join("");
  let svcOpts = `<option value="">${T('adminLogsAllServices')}</option>`;
  data.services.forEach((s) => { svcOpts += `<option value="${esc(s.name)}">${esc(s.name)}</option>`; });
  $("#alf-service").innerHTML = svcOpts;
  $("#alf-query").onclick = () => { adminLogPager.page = 1; loadAdminLogs(); };
  await loadAdminLogs();
}

async function initAdminDonationsTab() {
  const data = await loadAdminCommonData();
  $("#don-service").innerHTML = data.services
    .map((s) => `<option value="${esc(s.name)}">${esc(s.name)}</option>`)
    .join("");
  $("#don-user-list").innerHTML = data.users
    .map((u) => `<option value="${esc(u.username)}（${esc(u.discord_id)}）"></option>`)
    .join("");
  $("#donation-form").onsubmit = onDonationSubmit;
  $("#pricing-form").onsubmit = onPricingSubmit;
  await renderAdminDonationReview();
  await loadAdminDonations();
  await loadPricing();
}

async function initAdminAlertsTab() {
  await loadAdminAlerts();
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
  $("#nav-user").innerHTML = `${esc(state.me.username)} · <a href="#" id="logout">${T('logout')}</a>`;
  bindLogout("#logout");

  // Tab navigation bar
  const tabs = ["settings", "antiabuse", "users", "logs", "donations", "alerts", "bulletins"];
  const tabLabels = {
    settings: T('adminTabSettings'), antiabuse: T('adminTabAntiAbuse'), users: T('adminTabUsers'),
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
          <th>${T('thActions')}</th>
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
        <div class="table-wrap"><table><thead><tr><th><input type="checkbox" id="select-all" title="${T('selectAll')}"></th><th>${T('thUser')}</th><th>${T('thCredits')}</th><th>${T('thDonationCredit')}</th><th>${T('thRPM')}</th><th>${T('thCreated')}</th><th>${T('thStatus')}</th><th>${T('thActions')}</th></tr></thead><tbody id="user-rows"></tbody></table></div>
        <div class="row-actions" id="user-pager" style="margin-top:.5rem"></div>
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
          <button id="alf-query" class="afl-btn">${T('adminLogsQuery')}</button>
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
          <div style="display:grid;grid-template-columns:1fr 1fr 1fr 1fr;gap:.5rem;align-items:end">
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
        <form id="donation-form">
          <div style="display:grid;grid-template-columns:auto 1fr;gap:.5rem;align-items:end">
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
          <div style="display:grid;grid-template-columns:1fr 1fr 1fr;gap:.5rem">
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
        <div class="table-wrap"><table><thead><tr>
          <th><input type="checkbox" id="don-select-all" title="${T('batchSelectAll')}"></th>
          <th>${T('charityThService')}</th><th>${T('charityThModel')}</th><th>${T('charityThSource')}</th>
          <th>Key</th>
          <th>${T('charityThStatus')}</th><th>${T('charityThRemaining')}</th><th>RPM</th><th>${T('charityThDeadline')}</th>
          <th>${T('thNote')}</th><th>${T('thActions')}</th>
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

// resolveLogUserFilter maps the free-text user filter to a user id.
// Returns { id } on success, { id: null } for "all" (empty input), or
// { error } when the text matches no user.
function resolveLogUserFilter(text) {
  const q = text.trim();
  if (!q) return { id: null };
  // Exact datalist form: "username（discord_id）".
  const m = q.match(/^(.*)（([^（）]*)）$/);
  if (m) {
    const hit = adminLogUsers.find((u) => u.username === m[1] && u.discord_id === m[2]);
    if (hit) return { id: hit.id };
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
  return `
    <tr data-id="${u.id}">
      <td><input type="checkbox" class="user-chk" data-id="${u.id}"></td>
      <td style="max-width:10rem;white-space:nowrap;overflow:hidden;text-overflow:ellipsis" title="${titleTxt}">${esc(u.username)} <span class="muted mono">(${esc(u.discord_id)})</span></td>
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
  userPager.data = q ? _allAdminUsers.filter((u) =>
    (u.username || "").toLowerCase().includes(q) ||
    (u.discord_id || "").toLowerCase().includes(q)
  ) : _allAdminUsers;
  userPager.page = 1;
  userPager.afterRender = bindUserRowActions;
  renderPaged(userPager, "#user-rows", "#user-pager", 8);
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
    <tr>
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
  const resolved = resolveLogUserFilter($("#alf-user").value);
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
  const size = adminLogPager.size === Infinity ? 10000 : adminLogPager.size;
  params.set("limit", String(size));
  params.set("offset", String((adminLogPager.page - 1) * size));

  try {
    const data = await api(`/api/admin/logs?${params.toString()}`);
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
    <button class="pg-next secondary" ${adminLogPager.page >= pages ? "disabled" : ""}>›</button>`;

  const c = $("#alf-pager");
  c.querySelector(".pg-size").onchange = (e) => {
    adminLogPager.size = e.target.value === "inf" ? Infinity : parseInt(e.target.value, 10);
    adminLogPager.page = 1;
    loadAdminLogs();
  };
  c.querySelector(".pg-prev").onclick = () => { adminLogPager.page--; loadAdminLogs(); };
  c.querySelector(".pg-next").onclick = () => { adminLogPager.page++; loadAdminLogs(); };
}

/* ---------------- admin site: alert centre ---------------- */
function getAlertTypeLabels() {
  return {
    blocking_failed_200: T('alertTypeBlockingFailed200'),
    donation_exhausted_race: T('alertTypeDonationExhausted'),
  };
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
  const size = alertPager.size === Infinity ? 10000 : alertPager.size;
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
    <button class="pg-next secondary" ${alertPager.page >= pages ? "disabled" : ""}>›</button>`;

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
      // Switch to the log tab and pre-fill the model/service/user filters.
      if (btn.dataset.logId) {
        $("#alf-user").value = "";
        $("#alf-service").value = "";
        $("#alf-model").value = "";
        // We scroll to the log section and trigger a query.
      }
      // Fallback: just trigger a fresh query on the log section.
      $("#admin-logs-filter").scrollIntoView({ behavior: "smooth" });
      adminLogPager.page = 1;
      loadAdminLogs();
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
      <td></td>
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
        configs.push({ service: svc, mode, min_chars: minChars, penalty_deduct_credits: penaltyCredits, penalty_ban_hours: penaltyBan });
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
  const source = esc(d.source_display || "—");
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
  return `<tr><td><input type="checkbox" class="don-chk" data-id="${d.id}"></td><td>${esc(d.service)}</td><td class="mono">${esc(d.model)}</td><td>${source}</td><td>${keyCell}</td><td>${statusBadge}</td><td>${remaining}</td><td class="mono">${rpmDisplay}</td><td class="muted">${deadline}</td><td class="muted wrap">${esc(d.note || "—")}</td><td><div class="row-actions">${actions}</div></td></tr>`;
}

async function loadAdminDonations() {
  try {
    const data = await api("/api/admin/donations");
    const list = data.donations || [];
    donPager.data = list;
    donPager.afterRender = () => {
      clearBatchSelection("#don-select-all", ".don-chk", "don-batch-bar");
      bindBatchSelectAll("#don-select-all", ".don-chk", () => refBatchBar("don-batch-bar", ".don-chk"));
    };
    renderPaged(donPager, "#don-rows", "#don-pager", 11);
  } catch (err) {
    $("#don-rows").innerHTML = `<tr><td colspan="11" class="muted">${T('error').replace("{msg}", err.message)}</td></tr>`;
    $("#don-pager").innerHTML = "";
  }
}

async function loadPricing() {
  try {
    const data = await api("/api/admin/pricing");
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
        await api("/api/admin/pricing", { method: "PATCH", body: { service: svc, model: mdl, enabled: wantOn } });
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
        await api("/api/admin/pricing", { method: "DELETE", body: { service: svc, model: mdl } });
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
        <div style="display:grid;grid-template-columns:1fr 1fr;gap:.5rem">
          <label>${T('pricingThPrice')}<input name="price" type="number" min="0" value="${curPrice}" required></label>
          <label>${T('pricingThReward')}<input name="reward" type="number" min="0" value="${curReward || ""}" placeholder="自动"></label>
        </div>
        <small class="muted">${T('pricingRewardHint')}</small>
        <div id="pricing-edit-msg" style="margin-bottom:.5rem"></div>
        <footer style="display:flex;gap:.5rem;justify-content:flex-end">
          <button type="button" id="pricing-edit-save">${T('save')}</button>
          <button type="button" id="pricing-edit-cancel">${T('bulletinClose')}</button>
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
      const resp = await api("/api/admin/pricing", { method: "PUT", body: { service: svc, model: mdl, price, reward } });
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
    const resp = await api("/api/admin/pricing", { method: "PUT", body: { service: svc, model: mdl, price, reward } });
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
  // Resolve source user from text+<datalist>.
  const userText = $("#don-source-user").value.trim();
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
    await api("/api/admin/donations", { method: "POST", body });
    note.innerHTML = `<div class="note ok">${T('charityCreated')}</div>`;
    f.reset();
    $("#don-source-user").value = "";
    await loadAdminDonations();
  } catch (err) {
    note.innerHTML = `<div class="note err">${T('error').replace("{msg}", esc(err.message))}</div>`;
  }
}

/* ---------------- admin donation review ---------------- */

async function renderAdminDonationReview() {
  const container = $("#donation-review-content");
  if (!container) return;

  try {
    const resp = await api("/api/admin/donations/pending");
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
      <th>${T('donationAppThCreated')}</th><th>${T('thActions')}</th>
    </tr></thead><tbody>`;
    for (const a of apps) {
      const applicant = a.username ? `${esc(a.username)} <span class="muted mono">(${esc(a.discord_id || "")})</span>` : esc(String(a.user_id));
      html += `<tr>
        <td><input type="checkbox" class="review-chk" data-id="${a.id}"></td>
        <td>${applicant}</td>
        <td>${esc(a.service)}</td><td class="mono">${esc(a.model)}</td>
        <td class="muted">${fmtT(a.deadline)}</td>
        <td class="mono">${esc(String(a.total_count))}</td>
        <td class="muted wrap" style="max-width:12rem">${esc(a.note || "—")}</td>
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
        <div style="display:grid;grid-template-columns:auto 1fr;gap:.5rem;align-items:end">
          <label>${T('donationAppThService')}<input name="service" value="${esc(app.service)}"></label>
          <label>${T('donationAppThModel')}<input name="model" value="${esc(app.model)}"></label>
        </div>
        <label>${T('fieldBaseURL')}<input name="dify_base_url" value="${esc(app.dify_base_url)}"></label>
        <label>${T('fieldAPIKey')}<input name="dify_api_key" placeholder="${T('reviewKeepOriginalKey')}"></label>
        <div style="display:grid;grid-template-columns:1fr 1fr 1fr;gap:.5rem">
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
      await api(`/api/admin/donations/${app.id}/reject`, { method: "POST", body: { review_note: note } });
      toast(T('donationReviewRejected'));
      close();
      // Refresh both the pending list and the donation table.
      await renderAdminDonationReview();
      await loadAdminDonations();
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
      await api(`/api/admin/donations/${app.id}/approve`, { method: "POST", body });
      toast(T('donationReviewApproved'));
      close();
      await renderAdminDonationReview();
      await loadAdminDonations();
    } catch (err) {
      msg.innerHTML = `<span class="note err">${T('error').replace("{msg}", err.message)}</span>`;
    }
  };
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
        <div style="display:grid;grid-template-columns:auto 1fr;gap:.5rem;align-items:end">
          <label>${T('thService')}<input name="service" value="${esc(d.service)}"></label>
          <label>${T('thModel')}<input name="model" value="${esc(d.model)}"></label>
        </div>
        <label>${T('fieldBaseURL')}<input name="dify_base_url" value="${esc(d.dify_base_url)}"></label>
        <label>${T('fieldAPIKey')}<input name="dify_api_key" placeholder="${T('reviewKeepOriginalKey')}"></label>
        <div style="display:grid;grid-template-columns:1fr 1fr 1fr;gap:.5rem">
          <label>${T('charityDeadline')}<input name="deadline" type="datetime-local" value="${fmtLocalDT(d.deadline)}"></label>
          <label>${T('charityTotalCount')}<input name="total_count" type="number" min="1" value="${esc(String(d.total_count))}"></label>
          <label>${T('rpmLimitLabel')}<input name="rpm_limit" type="number" min="1" value="${esc(String(d.rpm_limit || 10))}" placeholder="${T('rpmLimitHint')}"></label>
        </div>
        <label>${T('charityNote')}<input name="note" value="${esc(d.note || "")}" placeholder="${T('charityNote')}"></label>
        <div id="don-edit-msg" style="margin-bottom:.5rem"></div>
        <footer style="display:flex;gap:.5rem;justify-content:flex-end">
          <button type="button" id="don-edit-save-btn">${T('save')}</button>
          <button type="button" id="don-edit-cancel-btn">${T('bulletinClose')}</button>
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
      note: f.querySelector("[name=note]").value.trim(),
    };
    if (deadline) body.deadline = deadline;
    if (totalCount > 0) body.total_count = totalCount;
    if (rpmLimit > 0) body.rpm_limit = rpmLimit;
    const msg = $("#don-edit-msg");
    msg.innerHTML = `<span class="muted">${T('loading')}</span>`;
    try {
      const resp = await api(`/api/admin/donations/${d.id}`, { method: "PATCH", body });
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
let _bulletinEditingId = null;

function adminBulletinRow(b) {
  const typeCell = bulletinTypeBadge(b.type);
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
    };
    renderPaged(adminBulletinPager, "#admin-bulletin-rows", "#admin-bulletin-pager", 7);
    // Bind edit buttons.
    document.querySelectorAll(".bull-edit").forEach((btn) => {
      btn.onclick = () => {
        const id = parseInt(btn.dataset.id, 10);
        const b = adminBulletinPager.data.find((x) => x.id === id);
        if (!b) return;
        _bulletinEditingId = id;
        const f = $("#admin-bulletin-form");
        f.querySelector('[name="title"]').value = b.title || "";
        f.querySelector('[name="content"]').value = b.content || "";
        f.querySelector('[name="type"]').value = b.type || "info";
        f.querySelector('[name="sort_order"]').value = b.sort_order || 0;
        f.querySelector('[name="closable"]').checked = !!b.closable;
        f.querySelector('[name="expires_at"]').value = b.expires_at ? fmtLocalDT(b.expires_at) : "";
        f.querySelector("button[type=submit]").textContent = T('bulletinSave');
        f.scrollIntoView({ behavior: "smooth" });
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
  } catch (err) {
    $("#admin-bulletin-rows").innerHTML = `<tr><td colspan="7" class="muted">${T('error').replace("{msg}", err.message)}</td></tr>`;
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
    type: f.querySelector('[name="type"]').value,
    sort_order: parseInt(f.querySelector('[name="sort_order"]').value, 10) || 0,
    closable: f.querySelector('[name="closable"]').checked,
    expires_at: expiresVal ? Math.floor(new Date(expiresVal).getTime() / 1000) : null,
  };
  const note = $("#admin-bulletin-note");
  note.innerHTML = `<p class="muted">${T('loading')}</p>`;
  try {
    if (_bulletinEditingId) {
      await api(`/api/admin/bulletins/${_bulletinEditingId}`, { method: "PUT", body });
      toast(T('bulletinUpdated'));
    } else {
      await api("/api/admin/bulletins", { method: "POST", body });
      toast(T('bulletinCreated'));
    }
    _bulletinEditingId = null;
    f.reset();
    f.querySelector("button[type=submit]").textContent = T('bulletinAdd');
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
      <th>${T('bulletinThTitle')}</th><th>${T('bulletinThType')}</th><th>${T('bulletinThCreated')}</th>
      <th>${T('bulletinThExpires')}</th><th>${T('bulletinThClosable')}</th><th>${T('bulletinThActions')}</th>
    </tr></thead><tbody id="admin-bulletin-rows"></tbody></table></div>
    <div class="row-actions" id="admin-bulletin-pager" style="margin:.5rem 0 1rem"></div>
    <form id="admin-bulletin-form">
      <label>${T('bulletinFieldTitle')}<input name="title" required></label>
      <label>${T('bulletinFieldContent')}<textarea name="content" rows="6" style="font-family:monospace" required></textarea></label>
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:.75rem">
        <label>${T('bulletinFieldType')}
          <select name="type">
            <option value="info">${T('bulletinTypeInfo')}</option>
            <option value="warning">${T('bulletinTypeWarning')}</option>
            <option value="important">${T('bulletinTypeImportant')}</option>
          </select>
        </label>
        <label>${T('bulletinFieldSortOrder')}<input name="sort_order" type="number" value="0"></label>
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
  // Cancel editing state when re-rendering.
  _bulletinEditingId = null;
  const f = $("#admin-bulletin-form");
  f.querySelector("button[type=submit]").textContent = T('bulletinAdd');
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
      await api(`/api/admin/donations/${id}/status`, { method: "POST", body: { status } });
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
      await api(`/api/admin/donations/${id}`, { method: "DELETE" });
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
      await api("/api/admin/donations/approve/batch", { method: "POST", body: { ids } });
      toast(T('donationReviewApproved'), 2200);
      clearBatchSelection("#review-select-all", ".review-chk", "don-review-batch-bar");
      await renderAdminDonationReview();
      await loadAdminDonations();
    } catch (err) { toast(T('error').replace("{msg}", err.message), 3000); }
    return;
  }

  // Donation review batch reject.
  const revReject = ev.target.closest(".batch-review-reject");
  if (revReject) {
    const ids = getBatchIds(".review-chk");
    if (ids.length === 0) { toast(T('batchNoSelection'), 2200); return; }
    try {
      await api("/api/admin/donations/reject/batch", { method: "POST", body: { ids } });
      toast(T('donationReviewRejected'), 2200);
      clearBatchSelection("#review-select-all", ".review-chk", "don-review-batch-bar");
      await renderAdminDonationReview();
      await loadAdminDonations();
    } catch (err) { toast(T('error').replace("{msg}", err.message), 3000); }
    return;
  }

  // Donation batch activate.
  const donActivate = ev.target.closest(".batch-don-activate");
  if (donActivate) {
    const ids = getBatchIds(".don-chk");
    if (ids.length === 0) { toast(T('batchNoSelection'), 2200); return; }
    try {
      await api("/api/admin/donations/status/batch", { method: "POST", body: { ids, status: "active" } });
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
      await api("/api/admin/donations/status/batch", { method: "POST", body: { ids, status: "inactive" } });
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
      await api("/api/admin/donations/delete/batch", { method: "POST", body: { ids } });
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
      await api("/api/admin/pricing/delete/batch", { method: "POST", body: { pairs } });
      toast(T('charityDeleted'), 2200);
      clearBatchSelection("#pricing-select-all", ".pricing-chk", "pricing-batch-bar");
      // Reload pricing data.
      const data = await api("/api/admin/pricing");
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
