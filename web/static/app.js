/* Dify2API SPA (vanilla JS). Host-aware: admin.<site> renders the admin
 * console; everything else renders the user console. All UI strings live in
 * the T dictionary (zh-CN in V1.0.0; i18n-ready). */
"use strict";

const T = {
  loading: "加载中……",
  siteName: "Dify2API",
  userLoginTitle: "登录",
  userLoginHint: "请使用 Discord 账号登录。首次登录将自动尝试注册（需满足服务器身份组条件）。",
  loginWithDiscord: "使用 Discord 登录",
  adminLoginTitle: "管理员登录",
  username: "用户名",
  password: "密码",
  login: "登录",
  logout: "退出登录",
  welcome: "你好，{name}",
  adminNotice: "当前为管理员账号。管理员无服务配置界面——如需使用服务，请退出后以 Discord 注册普通用户。",
  keyTitle: "调用方密钥（API Key）",
  keyHint: "此密钥用于 OpenAI 兼容接口（/v1）鉴权。界面不显示完整密钥，点击按钮复制。",
  copy: "复制",
  copied: "已复制",
  copyFail: "复制失败（需要 HTTPS 或 localhost 环境）",
  resetKey: "重置密钥",
  resetKeyConfirm: "重置后旧密钥立即失效，确定重置？",
  keyResetDone: "密钥已重置，请重新复制。",
  configsTitle: "App 配置（模型 → Dify App）",
  thModel: "模型名",
  thBaseURL: "App 地址",
  thNote: "备注",
  fieldNote: "可选备注，仅自己可见，如：Claude 主用",
  thEnabled: "启用",
  thActions: "操作",
  addConfig: "添加配置",
  editConfig: "编辑",
  deleteConfig: "删除",
  deleteConfirm: "确定删除该配置？",
  save: "保存",
  fieldModel: "模型名，如 [general]claude-opus-4-6",
  fieldBackend: "后端模型名，如 claude-opus-4-6",
  thService: "服务",
  fieldBaseURL: "Dify App API 地址，如 https://api.dify.ai/v1",
  fieldAPIKey: "Dify App API Key (app-…)",
  checkCompatible: "参数校验：兼容 ✓",
  checkIncompatible: "参数校验：不兼容",
  checkError: "参数校验：无法获取 App 参数",
  checkMissing: "App 缺少契约必选变量：{list}",
  checkUncovered: "App 必选变量契约不发送：{list}",
  checkExtra: "App 多余可选变量（将闲置，不影响使用）：{list}",
  logsTitle: "请求日志",
  thTime: "时间",
  thDuration: "耗时",
  thStatus: "状态",
  thErrorCode: "错误码",
  empty: "（暂无）",
  usersTitle: "用户管理",
  thUser: "用户",
  thRPM: "RPM 上限（A/B/C）",
  rpmLimitA: "全局 RPM 上限 A（传输完成，次/分）",
  rpmLimitB: "全局 RPM 上限 B（请求成功，次/分）",
  rpmLimitC: "全局 RPM 上限 C（请求接收，次/分）",
  rpmViolationLimit: "自动封禁阈值（24 小时内超限次数）",
  rpmBanHours: "自动封禁时长（小时）",
  rpmPrompt: "该用户的三类 RPM 覆盖值（当前：{cur}）。\n输入三个数字，用逗号分隔（例：6,12,18）；某项留空表示跟随全局（例：,12, 仅覆盖 B）；\n全部留空或输入 default 恢复全部跟随全局：",
  rpmInvalid: "格式无效：需为三个以逗号分隔的正整数（可留空）",
  thCreated: "注册时间",
  ban: "封禁",
  unban: "解封",
  banTimedPrompt: "封禁时长（小时），留空或 0 表示永久封禁：",
  banReasonPrompt: "封禁原因（可选，将显示给被封禁用户）：",
  banInvalid: "请输入有效小时数",
  resetUserKey: "重置密钥",
  resetUserKeyConfirm: "确定重置该用户的调用方密钥？（新密钥由用户自行查看）",
  deleteUser: "删除",
  deleteUserConfirm: "删除将清空该用户全部数据（可重新注册），确定？",
  statusNormal: "正常",
  statusBannedPerm: "永久封禁",
  statusBannedUntil: "封禁至 {time}",
  settingsTitle: "注册条件（Discord）",
  guildID: "服务器 ID (guild_id)",
  roleID: "身份组 ID (role_id)",
  settingsSaved: "已保存",
  error: "错误：{msg}",
  unauthorized: "登录已失效，请重新登录",
  exportData: "导出我的数据",
  exportDone: "数据已导出，请查看下载文件",
  deleteAccount: "删除我的账号",
  deleteAccountWarn1: "此操作不可撤销。将永久删除您的账号及全部数据（包括 App 配置、调用密钥和请求日志）。",
  deleteAccountWarn2: "确认删除？请在此输入 DELETE 进行最终确认：",
  deleteAccountConfirm: "DELETE",
  deleteAccountFailed: "请输入 DELETE 以确认删除",
  deleteAccountDone: "您的账号及全部数据已永久删除。感谢使用 Dify2API。",
  adminExport: "导出数据",
  adminLogsTitle: "请求日志",
  adminLogsAllUsers: "全部",
  adminLogsAllServices: "全部",
  adminLogsAllStatus: "全部",
  adminLogsSuccess: "成功",
  adminLogsError: "错误",
  adminLogsQuery: "查询",
  adminLogsDeletedUser: "（已删除）",
  adminLogsUserSearch: "输入用户名搜索，留空表示全部",
  adminLogsUserNotFound: "未找到匹配的用户：{name}",
  adminLogsSince: "开始时间",
  adminLogsUntil: "结束时间",
  thHTTPStatus: "HTTP",
  thErrorDetail: "错误详情",
  // Admin alert centre
  alertTitle: "告警中心",
  alertType: "类型",
  alertMessage: "消息",
  alertTypeBlockingFailed200: "阻塞调用 200 但失败",
  alertDeleteSelected: "删除选中",
  alertDeleteConfirm: "确定删除选中的 {n} 条告警？",
  alertDeleted: "已删除 {n} 条告警",
  alertLinkedRequest: "查看关联请求",

  // Charity / donations
  charityTitle: "公益资源",
  charityGlobalLabel: "全局公益开关",
  charityGlobalHint: "开启后用户可在控制台中启用公益资源，并在 /v1/models 中看到公益模型",
  charityFormTitle: "添加捐赠条目",
  charityService: "服务",
  charityModel: "模型名（不得含方括号）",
  charityBaseURL: "Dify Base URL",
  charityAPIKey: "Dify API Key",
  charitySourceUser: "来源用户",
  charitySourceUserHint: "从已注册用户中搜索（输入用户名或 Discord ID）",
  charitySourceText: "来源文本（未选用户时填写）",
  charityDeadline: "截止时间",
  charityTotalCount: "捐赠次数",
  charityNote: "备注（来源为管理员时必填）",
  charitySubmit: "创建",
  charityTableTitle: "捐赠条目列表",
  charityThService: "服务",
  charityThModel: "模型",
  charityThSource: "来源",
  charityThStatus: "状态",
  charityThRemaining: "剩余/总数",
  charityThDeadline: "截止时间",
  charityThActions: "操作",
  charityStatusActive: "有效",
  charityStatusInactive: "未激活",
  charityStatusExpired: "失效",
  charityBtnToggleOn: "激活",
  charityBtnToggleOff: "停用",
  charityBtnDelete: "删除",
  charityDeleteWarn: "关联的请求日志与告警将保留但不再能回溯到条目详情。确定删除？",
  charityCreated: "捐赠条目已创建",
  charityStatusChanged: "状态已更新",
  charityDeleted: "捐赠条目已删除",
  // User charity toggle
  userCharityToggle: "启用公益资源",
  userCharityConfirm: "警告：开启公益资源后，您的请求将被转发至捐赠者配置的 Dify App。捐赠者可通过其 Dify App 后台日志查看完整请求内容。平台不保证捐赠 App 的可靠性，对捐赠者可能的恶意行为免除平台责任。\n\n确定开启？",
  userCharityOn: "公益资源已启用",
  userCharityOff: "公益资源已关闭",
  userCharityBanner: "捐赠与公益系统尚未被管理员启用",
  insufficientCredits: "积分不足",
  // Credits & check-in (alpha.3 F2)
  creditsTitle: "公益积分",
  creditsBalance: "当前余额：{n}",
  creditsCheckin: "签到",
  creditsCheckinDone: "签到成功！获得 {name} +{bonus}，当前余额 {total}",
  creditsCheckedIn: "今日已签到",
  creditsCheckinFailed: "签到失败",
  creditsNameLabel: "积分名称",
  thCredits: "积分",
  thDonationCredit: "捐赠有效值",
  batchCredits: "批量积分操作",
  batchDonationCredit: "批量捐赠有效值",
  batchAction: "操作",
  batchAmount: "数值",
  batchSubmit: "执行",
  batchSet: "设定",
  batchAdd: "增加",
  batchSub: "减少",
  batchConfirm: "确定要对 {n} 个用户执行 {action} {amount} 吗？",
  batchDone: "操作完成，已更新 {n} 个用户",
  batchNoSelection: "请先选择用户",

  // Admin log model filter + donation source column
  adminLogsModel: "模型名",
  thDonationSource: "捐赠来源",
};

/* ---------------- helpers ---------------- */
const $ = (sel) => document.querySelector(sel);
const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const fmtT = (ts) => (ts ? new Date(ts * 1000).toLocaleString() : "—");

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

async function copyKey() {
  try {
    let { key } = await api("/api/caller-key");
    if (!key) {
      // No key yet (e.g. freshly registered) — generate one on the spot.
      const r = await api("/api/caller-key/reset", { method: "POST" });
      key = r.key;
    }
    await navigator.clipboard.writeText(key);
    toast(T.copied);
  } catch {
    toast(T.copyFail, 3200);
  }
}

/* ---------------- state & init ---------------- */
const state = { site: null, me: null, mode: "user" };

(async function init() {
  try {
    state.site = await api("/api/site-info");
  } catch {
    state.site = { site_host: "", admin_host: "", site_name: "Dify2API", report_email: "", site_base_url: "" };
  }
  // Apply site name to the nav logo and document title.
  if (state.site.site_name) {
    document.title = state.site.site_name;
    const logo = $("#nav-logo");
    if (logo) logo.textContent = state.site.site_name;
  }
  state.mode = location.hostname === state.site.admin_host ? "admin" : "user";
  try {
    state.me = await api("/api/me");
  } catch {
    state.me = null;
  }
  route();
})();

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
  if (btn) btn.onclick = async () => { await api("/api/auth/logout", { method: "POST" }).catch(() => {}); state.me = null; route(); };
}

/* ---------------- user site: login ---------------- */
function renderUserLogin() {
  $("#nav-user").textContent = "";
  $("#app").innerHTML = `
    <article class="card" style="max-width:28rem;margin:4rem auto;text-align:center">
      <h3>${T.userLoginTitle} · ${esc(state.site.site_name || T.siteName)}</h3>
      <p class="muted">${T.userLoginHint}</p>
      <a role="button" href="/auth/discord/login">${T.loginWithDiscord}</a>
    </article>`;
}

function renderAdminNotice() {
  $("#nav-user").textContent = state.me.username;
  $("#app").innerHTML = `
    <article class="card" style="max-width:32rem;margin:4rem auto;text-align:center">
      <p>${T.adminNotice}</p>
      <button id="logout">${T.logout}</button>
    </article>`;
  bindLogout("#logout");
}

/* ---------------- pagination (shared) ---------------- */
function newPager(rowFn) {
  return { data: [], page: 1, size: 10, rowFn };
}
function renderPaged(p, rowsSel, ctrlsSel, emptyCols) {
  const total = p.data.length;
  const pages = p.size === Infinity ? 1 : Math.max(1, Math.ceil(total / p.size));
  p.page = Math.min(Math.max(1, p.page), pages);
  const start = p.size === Infinity ? 0 : (p.page - 1) * p.size;
  const items = p.size === Infinity ? p.data : p.data.slice(start, start + p.size);
  $(rowsSel).innerHTML = items.length ? items.map(p.rowFn).join("") : `<tr><td colspan="${emptyCols}" class="muted">${T.empty}</td></tr>`;
  $(ctrlsSel).innerHTML = `
    <select class="pg-size">
      ${[5, 10, 20, 50].map((n) => `<option value="${n}" ${p.size === n ? "selected" : ""}>${n} 条/页</option>`).join("")}
      <option value="inf" ${p.size === Infinity ? "selected" : ""}>全部</option>
    </select>
    <button class="pg-prev secondary" ${p.page <= 1 ? "disabled" : ""}>‹</button>
    <span class="muted">${p.page} / ${pages} 页 · 共 ${total} 条</span>
    <button class="pg-next secondary" ${p.page >= pages ? "disabled" : ""}>›</button>`;
  const c = $(ctrlsSel);
  c.querySelector(".pg-size").onchange = (e) => {
    p.size = e.target.value === "inf" ? Infinity : parseInt(e.target.value, 10);
    p.page = 1;
    renderPaged(p, rowsSel, ctrlsSel, emptyCols);
  };
  c.querySelector(".pg-prev").onclick = () => { p.page--; renderPaged(p, rowsSel, ctrlsSel, emptyCols); };
  c.querySelector(".pg-next").onclick = () => { p.page++; renderPaged(p, rowsSel, ctrlsSel, emptyCols); };
}

/* ---------------- user site: dashboard ---------------- */
async function renderUserDashboard() {
  $("#nav-user").innerHTML = `${esc(T.welcome.replace("{name}", state.me.username))} · <a href="#" id="logout">${T.logout}</a>`;
  bindLogout("#logout");
  $("#app").innerHTML = `
    <section class="card" id="key-card">
      <h3>${T.keyTitle}</h3>
      <p class="muted">${T.keyHint}</p>
      <div class="row-actions">
        <span class="mono badge off">d2a_•••••••••••••••</span>
        <button id="copy-key" class="secondary">${T.copy}</button>
        <button id="reset-key" class="contrast outline">${T.resetKey}</button>
      </div>
    </section>
    <section class="card" id="data-card">
      <h3>数据管理</h3>
      <div class="row-actions">
        <button id="export-data" class="secondary">${T.exportData}</button>
        <button id="delete-account" class="contrast outline">${T.deleteAccount}</button>
      </div>
    </section>
    <section class="card" id="charity-card"></section>
    <section class="card" id="credits-card">
      <h3>${T.creditsTitle}</h3>
      <div id="credits-info"><p class="muted">${T.loading}</p></div>
      <div class="row-actions" style="margin-top:.5rem">
        <button id="checkin-btn" class="secondary">${T.creditsCheckin}</button>
      </div>
    </section>
    <section class="card" id="configs">
      <h3>${T.configsTitle}</h3>
      <div id="check-note"></div>
      <div class="table-wrap"><table><thead><tr><th>${T.thModel}</th><th>${T.thNote}</th><th>${T.thEnabled}</th><th>${T.thActions}</th></tr></thead><tbody id="cfg-rows"></tbody></table></div>
      <div class="row-actions" id="cfg-pager" style="margin:.5rem 0 1rem"></div>
      <form id="cfg-form">
        <div style="display:grid;grid-template-columns:auto 1fr;gap:.5rem;align-items:end">
          <label>${T.thService}<select name="service" id="cfg-service"></select></label>
          <label>${T.thModel}<input name="backend" placeholder="${T.fieldBackend}（不得含方括号 [ ] 或保留前缀）" required></label>
        </div>
        <label>${T.thBaseURL}<input name="dify_base_url" placeholder="${T.fieldBaseURL}" required></label>
        <label>API Key<input name="dify_api_key" placeholder="${T.fieldAPIKey}" required></label>
        <label>${T.thNote}<input name="note" placeholder="${T.fieldNote}"></label>
        <button type="submit" id="cfg-submit">${T.addConfig}</button>
      </form>
    </section>
    <section class="card" id="logs">
      <h3>${T.logsTitle}</h3>
      <div class="table-wrap"><table><thead><tr><th>${T.thTime}</th><th>${T.thDuration}</th><th>${T.thModel}</th><th>${T.thStatus}</th><th>${T.thErrorCode}</th></tr></thead><tbody id="log-rows"></tbody></table></div>
      <div class="row-actions" id="log-pager" style="margin-top:.5rem"></div>
    </section>`;

  $("#copy-key").onclick = copyKey;
  $("#reset-key").onclick = async () => {
    if (!confirm(T.resetKeyConfirm)) return;
    await api("/api/caller-key/reset", { method: "POST" });
    toast(T.keyResetDone);
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
      toast(T.exportDone);
    } catch (err) {
      toast(T.error.replace("{msg}", err.message), 4000);
    }
  };
  $("#delete-account").onclick = async () => {
    if (!confirm(T.deleteAccountWarn1)) return;
    const input = prompt(T.deleteAccountWarn2);
    if (input !== T.deleteAccountConfirm) {
      if (input !== null) toast(T.deleteAccountFailed, 3000);
      return;
    }
    try {
      const data = await api("/api/me?confirm=DELETE", { method: "DELETE" });
      toast(data.message || T.deleteAccountDone, 5000);
      state.me = null;
      route();
    } catch (err) {
      toast(T.error.replace("{msg}", err.message), 4000);
    }
  };
  $("#cfg-form").onsubmit = onConfigSubmit;
  // Populate the service dropdown from the server-side registry.
  const { services } = await api("/api/services");
  $("#cfg-service").innerHTML = services
    .map((s) => `<option value="${esc(s.name)}" title="${esc(s.label)}">${esc(s.name)}</option>`)
    .join("");
  await Promise.all([loadConfigs(), loadLogs()]);

  // Charity toggle and banner.
  renderCharityCard();
  // Credits card and check-in.
  renderCreditsCard();
}

async function renderCreditsCard() {
  const card = $("#credits-card");
  if (!card) return;
  try {
    // Fetch latest user data to get credits.
    const me = await api("/api/me");
    const creditsName = state.site.credits_name || T.creditsTitle;
    // Logo: CREDITS_LOGO_PATH (image) takes precedence over
    // CREDITS_LOGO_TEXT (emoji/text). If neither is configured the
    // image falls back to text, or text is empty, and onerror hides it.
    const logoText = state.site.credits_logo_text || "";
    const logoHTML = logoText
      ? `<span style="font-size:1.5rem;margin-right:.5rem">${esc(logoText)}</span>`
      : "";
    // Image as primary source (PATH priority): always render.  When
    // PATH is not configured the endpoint returns 204 → onerror fires
    // → the text span is shown (via a separate onerror callback).
    const hasPath = true; // unknown — we try and let onerror deal
    const logoImg = `<img src="/credits-logo" alt="" style="height:2rem;vertical-align:middle;margin-right:.5rem" onerror="var t=this.parentElement.querySelector('.cr-text');if(t)t.style.display='';this.style.display='none'">`;
    $("#credits-info").innerHTML = `
      ${logoImg}<span class="cr-text" style="${logoText ? 'display:none' : ''};font-size:1.5rem;margin-right:.5rem">${esc(logoText)}</span>
      <strong>${esc(creditsName)}</strong>
      <span class="badge ok" style="margin-left:.75rem">${T.creditsBalance.replace("{n}", String(me.credits || 0))}</span>`;

    const btn = $("#checkin-btn");
    if (!btn) return;
    // Check if already checked in today: we only know after the first attempt.
    // For UI hint, we could track via state, but the simplest approach:
    // the server returns 400 "今日已签到" — we handle it in the click handler.
    btn.onclick = async () => {
      btn.disabled = true;
      btn.textContent = T.loading;
      try {
        const resp = await api("/api/me/checkin", { method: "POST" });
        toast(T.creditsCheckinDone
          .replace("{name}", creditsName)
          .replace("{bonus}", String(resp.bonus))
          .replace("{total}", String(resp.credits)));
        // Refresh the card to show new balance.
        renderCreditsCard();
      } catch (err) {
        if (err.message && err.message.includes("今日已签到")) {
          toast(T.creditsCheckedIn);
          btn.textContent = T.creditsCheckedIn;
          btn.disabled = true;
        } else {
          toast(T.creditsCheckinFailed + "：" + err.message, 3000);
          btn.disabled = false;
          btn.textContent = T.creditsCheckin;
        }
        return;
      }
    };
  } catch {
    $("#credits-info").innerHTML = `<p class="muted">${T.error.replace("{msg}", "无法加载积分信息")}</p>`;
  }
}

async function renderCharityCard() {
  const card = $("#charity-card");
  if (!card) return;
  // When the global switch is off, show a persistent banner AND the
  // personal toggle (users can freely flip the switch, but every flip
  // shows an informational toast).
  const globalOff = !state.site.charity_global_enabled;
  let enabled = false;
  if (!globalOff) {
    try {
      const data = await api("/api/me/charity");
      enabled = data.charity_enabled;
    } catch { /* use default false */ }
  }
  const statusText = enabled ? T.userCharityOn : T.userCharityOff;
  card.innerHTML = `
    ${globalOff ? `<article class="note warn" style="margin-bottom:.75rem">${T.userCharityBanner}</article>` : ""}
    <h3>${T.userCharityToggle}</h3>
    <p class="muted">${esc(statusText)}</p>
    <label style="display:flex;align-items:center;gap:.75rem">
      <input type="checkbox" id="charity-toggle" role="switch" ${enabled ? "checked" : ""}>
      <span>${T.userCharityToggle}</span>
    </label>`;
  const toggle = $("#charity-toggle");
  toggle.onchange = async () => {
    const wantOn = toggle.checked;
    // If the global switch is off, allow the toggle but show an
    // informational toast and revert the visual state.
    if (globalOff) {
      toggle.checked = !wantOn;
      toast(T.userCharityBanner, 3000);
      return;
    }
    if (wantOn && !confirm(T.userCharityConfirm)) {
      toggle.checked = false;
      return;
    }
    try {
      await api("/api/me/charity", {
        method: "PUT",
        body: { enabled: wantOn, confirmed: wantOn },
      });
      toast(wantOn ? T.userCharityOn : T.userCharityOff);
      renderCharityCard();
    } catch (err) {
      toggle.checked = !wantOn;
      toast(T.error.replace("{msg}", err.message), 3000);
    }
  };
}

let editingId = null;
const cfgPager = newPager(cfgRow);
const logPager = newPager(logRow);

function cfgRow(c) {
  return `
    <tr data-id="${c.id}">
      <td class="mono">${esc(c.model)}</td>
      <td class="muted wrap">${esc(c.note || "—")}</td>
      <td><input type="checkbox" class="cfg-toggle" ${c.enabled ? "checked" : ""} role="switch"></td>
      <td class="row-actions">
        <button class="secondary cfg-edit">${T.editConfig}</button>
        <button class="contrast outline cfg-del">${T.deleteConfig}</button>
      </td>
    </tr>`;
}

async function loadConfigs() {
  const { configs } = await api("/api/configs");
  cfgPager.data = configs || [];
  renderPaged(cfgPager, "#cfg-rows", "#cfg-pager", 4);

  document.querySelectorAll(".cfg-toggle").forEach((cb) => (cb.onchange = async (e) => {
    const id = e.target.closest("tr").dataset.id;
    await api(`/api/configs/${id}/toggle`, { method: "POST", body: { enabled: e.target.checked } });
    toast(T.settingsSaved);
  }));
  document.querySelectorAll(".cfg-del").forEach((b) => (b.onclick = async (e) => {
    if (!confirm(T.deleteConfirm)) return;
    const id = e.target.closest("tr").dataset.id;
    await api(`/api/configs/${id}`, { method: "DELETE" });
    await loadConfigs();
  }));
  document.querySelectorAll(".cfg-edit").forEach((b) => (b.onclick = (e) => {
    const id = e.target.closest("tr").dataset.id;
    const c = cfgPager.data.find((x) => String(x.id) === id);
    if (!c) return;
    editingId = id;
    const f = $("#cfg-form");
    const m = c.model.match(/^\[([^\]]+)\](.*)$/);
    if (m) {
      f.service.value = m[1];
      f.backend.value = m[2];
    } else {
      f.backend.value = c.model;
    }
    f.dify_base_url.value = c.dify_base_url;
    f.dify_api_key.value = "";
    f.dify_api_key.placeholder = T.fieldAPIKey;
    f.note.value = c.note || "";
    $("#cfg-submit").textContent = T.save;
    f.scrollIntoView({ behavior: "smooth" });
  }));
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
  note.innerHTML = `<p class="muted">${T.loading}</p>`;
  try {
    const resp = editingId
      ? await api(`/api/configs/${editingId}`, { method: "PUT", body })
      : await api("/api/configs", { method: "POST", body });
    editingId = null;
    $("#cfg-submit").textContent = T.addConfig;
    f.reset();
    const c = resp.app_check || {};
    let cls = "ok", html = "";
    if (c.error) {
      cls = "warn"; html = `${T.checkError}: ${esc(c.error)}`;
    } else if (c.compatible) {
      html = `${T.checkCompatible}`;
      if (c.extra_app_optional?.length) html += `<br><span class="muted">${T.checkExtra.replace("{list}", esc(c.extra_app_optional.join(", ")))}</span>`;
    } else {
      cls = "err";
      html = `${T.checkIncompatible}`;
      if (c.missing_contract_vars?.length) html += `<br>${T.checkMissing.replace("{list}", esc(c.missing_contract_vars.join(", ")))}`;
      if (c.uncovered_app_required?.length) html += `<br>${T.checkUncovered.replace("{list}", esc(c.uncovered_app_required.join(", ")))}`;
    }
    note.innerHTML = `<div class="note ${cls}">${html}</div>`;
    await loadConfigs();
  } catch (err) {
    note.innerHTML = `<div class="note err">${T.error.replace("{msg}", esc(err.message))}</div>`;
  }
}

function logRow(l) {
  return `
    <tr>
      <td class="muted">${fmtT(l.started_at)}</td>
      <td class="muted">${l.ended_at > l.started_at ? (l.ended_at - l.started_at) + "s" : "—"}</td>
      <td class="mono">${esc(l.model)}</td>
      <td><span class="badge ${l.status === "success" ? "ok" : "err"}">${esc(l.status)}</span></td>
      <td class="mono muted">${esc(l.error_code || "")}</td>
    </tr>`;
}

async function loadLogs() {
  const { logs } = await api("/api/logs");
  logPager.data = logs || [];
  renderPaged(logPager, "#log-rows", "#log-pager", 5);
}

/* ---------------- admin site: login ---------------- */
function renderAdminLogin() {
  $("#nav-user").textContent = "";
  $("#app").innerHTML = `
    <article class="card" style="max-width:24rem;margin:4rem auto">
      <h3>${T.adminLoginTitle}</h3>
      <form id="admin-login-form">
        <label>${T.username}<input name="username" required autocomplete="username"></label>
        <label>${T.password}<input name="password" type="password" required autocomplete="current-password"></label>
        <div id="login-err" class="note err" style="display:none"></div>
        <button type="submit">${T.login}</button>
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
      el.textContent = T.error.replace("{msg}", err.message);
    }
  };
}

/* ---------------- admin site: dashboard ---------------- */
async function renderAdminDashboard() {
  $("#nav-user").innerHTML = `${esc(state.me.username)} · <a href="#" id="logout">${T.logout}</a>`;
  bindLogout("#logout");
  $("#app").innerHTML = `
    <section class="card">
      <h3>${T.settingsTitle}</h3>
      <form id="settings-form">
        <label>${T.guildID}<input name="guild_id"></label>
        <label>${T.roleID}<input name="role_id"></label>
        <div style="display:flex;flex-wrap:wrap;gap:.75rem">
          <label style="flex:1 1 12rem">${T.rpmLimitA}<input name="rpm_limit_a" type="number" min="1" required></label>
          <label style="flex:1 1 12rem">${T.rpmLimitB}<input name="rpm_limit_b" type="number" min="1" required></label>
          <label style="flex:1 1 12rem">${T.rpmLimitC}<input name="rpm_limit_c" type="number" min="1" required></label>
        </div>
        <div style="display:flex;flex-wrap:wrap;gap:.75rem">
          <label style="flex:1 1 12rem">${T.rpmViolationLimit}<input name="rpm_violation_limit" type="number" min="1" required></label>
          <label style="flex:1 1 12rem">${T.rpmBanHours}<input name="rpm_ban_hours" type="number" min="1" required></label>
        </div>
        <h4 style="margin-top:1rem">${T.creditsTitle}（签到）</h4>
        <div style="display:flex;flex-wrap:wrap;gap:.75rem">
          <label style="flex:1 1 10rem">签到最低积分<input name="checkin_min" type="number" min="1" required></label>
          <label style="flex:1 1 10rem">签到最高积分<input name="checkin_max" type="number" min="1" required></label>
          <label style="flex:1 1 10rem">积分上限<input name="credits_cap" type="number" min="1" required></label>
        </div>
        <label style="display:flex;align-items:center;gap:.5rem;margin-top:.5rem">
          <input name="charity_global_enabled" type="checkbox" role="switch">
          <span>${T.charityGlobalLabel}</span>
          <span class="muted" style="font-size:.85em">${T.charityGlobalHint}</span>
        </label>
        <button type="submit">${T.save}</button>
      </form>
    </section>
    <section class="card">
      <h3>${T.usersTitle}</h3>
      <div style="display:flex;flex-wrap:wrap;gap:.5rem;align-items:center;margin-bottom:.75rem" id="batch-ops">
        <select id="batch-action" style="width:auto;margin-bottom:0">
          <option value="">— ${T.batchAction} —</option>
          <option value="credits-set">${T.batchSet} ${T.creditsTitle}</option>
          <option value="credits-add">${T.batchAdd} ${T.creditsTitle}</option>
          <option value="credits-sub">${T.batchSub} ${T.creditsTitle}</option>
          <option value="dc-set">${T.batchSet} ${T.thDonationCredit}</option>
          <option value="dc-add">${T.batchAdd} ${T.thDonationCredit}</option>
          <option value="dc-sub">${T.batchSub} ${T.thDonationCredit}</option>
        </select>
        <input id="batch-amount" type="number" min="0" placeholder="${T.batchAmount}" style="width:6rem;margin-bottom:0">
        <button id="batch-submit" class="secondary" style="width:auto;margin-bottom:0">${T.batchSubmit}</button>
      </div>
      <div class="table-wrap"><table><thead><tr><th><input type="checkbox" id="select-all" title="全选"></th><th>${T.thUser}</th><th>${T.thCredits}</th><th>${T.thDonationCredit}</th><th>${T.thRPM}</th><th>${T.thCreated}</th><th>${T.thStatus}</th><th>${T.thActions}</th></tr></thead><tbody id="user-rows"></tbody></table></div>
      <div class="row-actions" id="user-pager" style="margin-top:.5rem"></div>
    </section>
    <section class="card">
      <h3>${T.adminLogsTitle}</h3>
      <div id="admin-logs-filter" style="display:flex;flex-wrap:wrap;gap:.75rem;align-items:flex-end;margin-bottom:.8rem">
        <label style="flex:1 1 16rem;min-width:14rem;margin-bottom:0">${T.thUser}
          <input id="alf-user" list="alf-user-list" placeholder="${T.adminLogsUserSearch}" autocomplete="off">
          <datalist id="alf-user-list"></datalist>
        </label>
        <label style="flex:0 1 12rem;min-width:10rem;margin-bottom:0">${T.thService}<select id="alf-service"><option value="">${T.adminLogsAllServices}</option></select></label>
        <label style="flex:0 1 12rem;min-width:10rem;margin-bottom:0">${T.adminLogsModel}<input id="alf-model" placeholder="[公益][general]x" style="margin-bottom:0"></label>
        <label style="flex:0 1 8rem;min-width:7rem;margin-bottom:0">${T.thStatus}<select id="alf-status"><option value="">${T.adminLogsAllStatus}</option><option value="success">${T.adminLogsSuccess}</option><option value="error">${T.adminLogsError}</option></select></label>
        <label style="flex:0 1 13rem;min-width:11rem;margin-bottom:0">${T.adminLogsSince}<input id="alf-since" type="datetime-local"></label>
        <label style="flex:0 1 13rem;min-width:11rem;margin-bottom:0">${T.adminLogsUntil}<input id="alf-until" type="datetime-local"></label>
        <button id="alf-query" style="flex:0 0 auto;width:auto;margin-bottom:0">${T.adminLogsQuery}</button>
      </div>
      <div class="table-wrap"><table><thead><tr><th>${T.thTime}</th><th>${T.thUser}</th><th>${T.thModel}</th><th>${T.thService}</th><th>${T.thDuration}</th><th>${T.thStatus}</th><th>${T.thHTTPStatus}</th><th>${T.thErrorCode}</th><th>${T.thErrorDetail}</th><th>${T.thDonationSource}</th></tr></thead><tbody id="alf-rows"></tbody></table></div>
      <div class="row-actions" id="alf-pager" style="margin-top:.5rem"></div>
    </section>
    <section class="card" id="alerts-card">
      <h3>${T.alertTitle}</h3>
      <div class="table-wrap"><table><thead><tr><th><input type="checkbox" id="alert-select-all" title="全选"></th><th>${T.thTime}</th><th>${T.alertType}</th><th>${T.alertMessage}</th><th>${T.thActions}</th></tr></thead><tbody id="alert-rows"></tbody></table></div>
      <div class="row-actions" style="margin:.5rem 0">
        <button id="alert-delete-btn" class="contrast outline">${T.alertDeleteSelected}</button>
      </div>
      <div class="row-actions" id="alert-pager" style="margin-top:.5rem"></div>
    </section>
    <section class="card" id="donations-card">
      <h3>${T.charityTitle}</h3>
      <form id="donation-form">
        <div style="display:grid;grid-template-columns:auto 1fr;gap:.5rem;align-items:end">
          <label>${T.charityService}<select name="service" id="don-service"></select></label>
          <label>${T.charityModel}<input name="model" placeholder="${T.charityModel}" required></label>
        </div>
        <label>${T.charityBaseURL}<input name="dify_base_url" placeholder="https://api.dify.ai/v1" required></label>
        <label>${T.charityAPIKey}<input name="dify_api_key" placeholder="app-…" required></label>
        <label>${T.charitySourceUser}
          <input id="don-source-user" list="don-user-list" placeholder="${T.charitySourceUserHint}" autocomplete="off">
          <datalist id="don-user-list"></datalist>
        </label>
        <label>${T.charitySourceText}<input name="source_text" placeholder="当未选来源用户时填写此项"></label>
        <label>${T.charityDeadline}<input name="deadline" type="datetime-local" required></label>
        <label>${T.charityTotalCount}<input name="total_count" type="number" min="1" required></label>
        <label>${T.charityNote}<input name="note" placeholder="${T.charityNote}"></label>
        <div id="don-note"></div>
        <button type="submit">${T.charitySubmit}</button>
      </form>
      <div class="table-wrap" style="margin-top:1.5rem"><table><thead><tr>
        <th>${T.charityThService}</th><th>${T.charityThModel}</th><th>${T.charityThSource}</th>
        <th>${T.charityThStatus}</th><th>${T.charityThRemaining}</th><th>${T.charityThDeadline}</th>
        <th>${T.thActions}</th>
      </tr></thead><tbody id="don-rows"></tbody></table></div>
      <div class="row-actions" id="don-pager" style="margin-top:.5rem"></div>
    </section>`;

  const s = await api("/api/admin/settings");
  const sf = $("#settings-form");
  sf.guild_id.value = s.guild_id || "";
  sf.role_id.value = s.role_id || "";
  sf.rpm_limit_a.value = s.rpm_limit_a;
  sf.rpm_limit_b.value = s.rpm_limit_b;
  sf.rpm_limit_c.value = s.rpm_limit_c;
  sf.rpm_violation_limit.value = s.rpm_violation_limit;
  sf.rpm_ban_hours.value = s.rpm_ban_hours;
  sf.charity_global_enabled.checked = s.charity_global_enabled;
  sf.checkin_min.value = s.checkin_min;
  sf.checkin_max.value = s.checkin_max;
  sf.credits_cap.value = s.credits_cap;
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
      credits_cap: parseInt(sf.credits_cap.value, 10),
      charity_global_enabled: sf.charity_global_enabled.checked,
    } });
    toast(T.settingsSaved);
  };

  await loadAdminUsers();

  // Populate the admin-logs filter widgets.  The user filter is a
  // searchable text input backed by a <datalist> (dropdowns are unusable
  // with 100+ users); typing filters natively in the browser, and the
  // entered text is resolved to a user id in loadAdminLogs().
  const { users } = await api("/api/admin/users");
  adminLogUsers = users || [];
  $("#alf-user-list").innerHTML = adminLogUsers
    .map((u) => `<option value="${esc(u.username)}（${esc(u.discord_id)}）"></option>`)
    .join("");

  const { services } = await api("/api/services");
  let svcOpts = `<option value="">${T.adminLogsAllServices}</option>`;
  services.forEach((s) => { svcOpts += `<option value="${esc(s.name)}">${esc(s.name)}</option>`; });
  $("#alf-service").innerHTML = svcOpts;

  $("#alf-query").onclick = () => { adminLogPager.page = 1; loadAdminLogs(); };
  await loadAdminLogs();

  // Charity / donations card initialization.
  const donSvc = $("#don-service");
  donSvc.innerHTML = services
    .map((s) => `<option value="${esc(s.name)}">${esc(s.name)}</option>`)
    .join("");
  // Source user datalist from the same users array.
  $("#don-user-list").innerHTML = adminLogUsers
    .map((u) => `<option value="${esc(u.username)}（${esc(u.discord_id)}）"></option>`)
    .join("");
  $("#donation-form").onsubmit = onDonationSubmit;
  await loadAdminDonations();

  // Alert centre init.
  await loadAdminAlerts();
  $("#alert-delete-btn").onclick = async () => {
    const chks = document.querySelectorAll(".alert-chk:checked");
    if (chks.length === 0) return;
    if (!confirm(T.alertDeleteConfirm.replace("{n}", String(chks.length)))) return;
    const ids = Array.from(chks).map((c) => parseInt(c.dataset.id, 10));
    try {
      const resp = await api("/api/admin/alerts", { method: "DELETE", body: { ids } });
      toast(T.alertDeleted.replace("{n}", String(resp.deleted || 0)));
      alertPager.page = 1;
      await loadAdminAlerts();
    } catch (err) {
      toast(T.error.replace("{msg}", err.message), 3000);
    }
  };
}

function userStatusBadges(u) {
  if (u.disabled) {
    let txt = T.statusBannedPerm;
    if (u.ban_reason) txt += ` (${esc(u.ban_reason)})`;
    return `<span class="badge err">${txt}</span>`;
  }
  if (u.banned_until > 0 && u.banned_until * 1000 > Date.now()) {
    let txt = (u.auto_banned ? "RPM 自动" : "") + T.statusBannedUntil.replace("{time}", fmtT(u.banned_until));
    if (u.ban_reason) txt += `<br><span class="muted">原因：${esc(u.ban_reason)}</span>`;
    return `<span class="badge warn">${txt}</span>`;
  }
  return `<span class="badge ok">${T.statusNormal}</span>`;
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
  return { error: T.adminLogsUserNotFound.replace("{name}", q) };
}

function userRow(u) {
  const fmtLim = (v) => (v == null ? "–" : String(v));
  const hasOverride = u.rpm_limit_a != null || u.rpm_limit_b != null || u.rpm_limit_c != null;
  const rpm = hasOverride
    ? esc(`${fmtLim(u.rpm_limit_a)}/${fmtLim(u.rpm_limit_b)}/${fmtLim(u.rpm_limit_c)}`)
    : `<span class="muted">default</span>`;
  return `
    <tr data-id="${u.id}">
      <td><input type="checkbox" class="user-chk" data-id="${u.id}"></td>
      <td>${esc(u.username)} <span class="muted mono">(${esc(u.discord_id)})</span></td>
      <td class="mono">${u.credits != null ? String(u.credits) : "0"}</td>
      <td class="mono">${u.donation_credit != null ? String(u.donation_credit) : "0"}</td>
      <td>${rpm} <button class="secondary u-rpm" style="padding:.1rem .5rem;font-size:.8rem">✎</button></td>
      <td class="muted">${fmtT(u.created_at)}</td>
      <td class="wrap">${userStatusBadges(u)}</td>
      <td class="row-actions">
        <button class="secondary u-ban">${T.ban}</button>
        <button class="secondary u-unban">${T.unban}</button>
        <button class="contrast outline u-key">${T.resetUserKey}</button>
        <button class="secondary u-export">${T.adminExport}</button>
        <button class="contrast outline u-del">${T.deleteUser}</button>
      </td>
    </tr>`;
}

async function loadAdminUsers() {
  const { users } = await api("/api/admin/users");
  userPager.data = users || [];
  renderPaged(userPager, "#user-rows", "#user-pager", 8);

  document.querySelectorAll(".u-ban").forEach((b) => (b.onclick = async (e) => {
    const id = e.target.closest("tr").dataset.id;
    const hours = prompt(T.banTimedPrompt, "");
    if (hours === null) return;
    const raw = hours.trim();
    let body;
    if (raw === "") {
      body = { permanent: true };
    } else {
      const h = parseFloat(raw);
      if (isNaN(h) || h <= 0) { toast(T.banInvalid); return; }
      body = { until: Math.floor(Date.now() / 1000 + h * 3600) };
    }
    const reason = prompt(T.banReasonPrompt, "");
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
    if (!confirm(T.resetUserKeyConfirm)) return;
    const id = e.target.closest("tr").dataset.id;
    await api(`/api/admin/users/${id}/reset-key`, { method: "POST" });
    toast(T.settingsSaved);
  }));
  document.querySelectorAll(".u-del").forEach((b) => (b.onclick = async (e) => {
    if (!confirm(T.deleteUserConfirm)) return;
    const id = e.target.closest("tr").dataset.id;
    await api(`/api/admin/users/${id}`, { method: "DELETE" });
    await loadAdminUsers();
  }));
  document.querySelectorAll(".u-rpm").forEach((b) => (b.onclick = async (e) => {
    const id = e.target.closest("tr").dataset.id;
    const u = userPager.data.find((x) => String(x.id) === id);
    const curParts = [u?.rpm_limit_a, u?.rpm_limit_b, u?.rpm_limit_c].map((x) => (x == null ? "" : String(x)));
    const cur = curParts.some((x) => x !== "") ? curParts.join(",") : "";
    const v = prompt(T.rpmPrompt.replace("{cur}", cur || "default"), cur);
    if (v === null) return;
    const raw = v.trim().toLowerCase();
    let body = { rpm_limit_a: null, rpm_limit_b: null, rpm_limit_c: null };
    if (raw !== "" && raw !== "default") {
      const parts = raw.split(",").map((x) => x.trim());
      if (parts.length !== 3) { toast(T.rpmInvalid); return; }
      const keys = ["rpm_limit_a", "rpm_limit_b", "rpm_limit_c"];
      for (let i = 0; i < 3; i++) {
        if (parts[i] === "") continue; // 留空 = 跟随全局
        const n = parseInt(parts[i], 10);
        if (isNaN(n) || n < 1) { toast(T.rpmInvalid); return; }
        body[keys[i]] = n;
      }
    }
    await api(`/api/admin/users/${id}/rpm`, { method: "POST", body });
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
      toast(T.exportDone);
    } catch (err) {
      toast(T.error.replace("{msg}", err.message), 4000);
    }
  }));

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
      if (!action) { toast(T.batchNoSelection); return; }
      if (isNaN(amount) || amount < 0) { toast("请输入有效数值"); return; }
      const chks = document.querySelectorAll(".user-chk:checked");
      if (chks.length === 0) { toast(T.batchNoSelection); return; }
      const actionLabels = {
        "credits-set": T.batchSet + " " + T.creditsTitle,
        "credits-add": T.batchAdd + " " + T.creditsTitle,
        "credits-sub": T.batchSub + " " + T.creditsTitle,
        "dc-set": T.batchSet + " " + T.thDonationCredit,
        "dc-add": T.batchAdd + " " + T.thDonationCredit,
        "dc-sub": T.batchSub + " " + T.thDonationCredit,
      };
      const label = actionLabels[action] || action;
      if (!confirm(T.batchConfirm.replace("{n}", String(chks.length)).replace("{action}", label).replace("{amount}", String(amount)))) return;
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
        toast(T.batchDone.replace("{n}", String(resp.updated || 0)));
        await loadAdminUsers();
      } catch (err) {
        toast(T.error.replace("{msg}", err.message), 3000);
      }
    };
  }
}

/* ---------------- admin site: request logs ---------------- */
function adminLogRow(l) {
  const userCell = l.username
    ? esc(l.username)
    : esc(String(l.user_id)) + ` <span class="muted">${T.adminLogsDeletedUser}</span>`;
  const dur = l.ended_at && l.started_at ? ((l.ended_at - l.started_at) * 1000).toFixed(0) + "ms" : "—";
  const statusClass = l.status === "success" ? "ok" : "err";
  const statusText = l.status === "success" ? T.adminLogsSuccess : T.adminLogsError;
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
      <td class="muted wrap" style="max-width:24rem">${esc(l.error_detail || "")}</td>
      <td class="muted">${esc(donationSrc)}</td>
    </tr>`;
}

async function loadAdminLogs() {
  const params = new URLSearchParams();
  const resolved = resolveLogUserFilter($("#alf-user").value);
  if (resolved.error) {
    $("#alf-rows").innerHTML = `<tr><td colspan="10" class="muted">${esc(resolved.error)}</td></tr>`;
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
  // Server-side pagination: "全部" maps to the server's max page size (500).
  const size = adminLogPager.size === Infinity ? 500 : adminLogPager.size;
  params.set("limit", String(size));
  params.set("offset", String((adminLogPager.page - 1) * size));

  try {
    const data = await api(`/api/admin/logs?${params.toString()}`);
    renderAdminLogs(data);
  } catch (err) {
    $("#alf-rows").innerHTML = `<tr><td colspan="10" class="muted">${T.error.replace("{msg}", err.message)}</td></tr>`;
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
    : `<tr><td colspan="10" class="muted">${T.empty}</td></tr>`;

  $("#alf-pager").innerHTML = `
    <select class="pg-size">
      ${[5, 10, 20, 50].map((n) => `<option value="${n}" ${size === n ? "selected" : ""}>${n} 条/页</option>`).join("")}
      <option value="inf" ${size === Infinity ? "selected" : ""}>全部</option>
    </select>
    <button class="pg-prev secondary" ${adminLogPager.page <= 1 ? "disabled" : ""}>‹</button>
    <span class="muted">${adminLogPager.page} / ${pages} 页 · 共 ${total} 条</span>
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
const alertTypeLabels = {
  blocking_failed_200: T.alertTypeBlockingFailed200,
  donation_exhausted_race: "公益资源竞争耗尽",
};

function alertRow(a) {
  const typeLabel = alertTypeLabels[a.type] || esc(a.type);
  let actionsHtml = "";
  if (a.request_log_id) {
    actionsHtml = `<button class="secondary alert-goto" data-log-id="${a.request_log_id}" data-user-id="${a.user_id || ""}" style="width:auto;margin:0">${T.alertLinkedRequest}</button>`;
  }
  return `
    <tr data-id="${a.id}">
      <td><input type="checkbox" class="alert-chk" data-id="${a.id}"></td>
      <td class="muted">${fmtT(a.created_at)}</td>
      <td><span class="badge warn">${typeLabel}</span></td>
      <td class="wrap" style="max-width:24rem">${esc(a.message)}</td>
      <td class="row-actions">${actionsHtml}</td>
    </tr>`;
}

async function loadAdminAlerts() {
  const size = alertPager.size === Infinity ? 500 : alertPager.size;
  const params = new URLSearchParams({
    limit: String(size),
    offset: String((alertPager.page - 1) * size),
  });
  try {
    const data = await api(`/api/admin/alerts?${params.toString()}`);
    renderAdminAlerts(data);
  } catch (err) {
    $("#alert-rows").innerHTML = `<tr><td colspan="5" class="muted">${T.error.replace("{msg}", err.message)}</td></tr>`;
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
    : `<tr><td colspan="5" class="muted">${T.empty}</td></tr>`;

  $("#alert-pager").innerHTML = `
    <select class="pg-size">
      ${[5, 10, 20, 50].map((n) => `<option value="${n}" ${size === n ? "selected" : ""}>${n} 条/页</option>`).join("")}
      <option value="inf" ${size === Infinity ? "selected" : ""}>全部</option>
    </select>
    <button class="pg-prev secondary" ${alertPager.page <= 1 ? "disabled" : ""}>‹</button>
    <span class="muted">${alertPager.page} / ${pages} 页 · 共 ${total} 条</span>
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

/* ---------------- admin site: donations (公益资源) ---------------- */
const donPager = newPager(donationRow);

function donationRow(d) {
  const statusMap = { active: T.charityStatusActive, inactive: T.charityStatusInactive, expired: T.charityStatusExpired };
  const statusBadge = `<span class="badge ${d.status === "active" ? "ok" : d.status === "expired" ? "off" : "warn"}">${esc(statusMap[d.status] || d.status)}</span>`;
  const remaining = `${d.remaining_count}/${d.total_count}`;
  const deadline = fmtT(d.deadline);
  const source = esc(d.source_display || "—");
  let actions = "";
  if (d.status === "active") {
    actions += `<button class="secondary don-toggle" data-id="${d.id}" data-status="inactive" style="width:auto;margin:0">${T.charityBtnToggleOff}</button> `;
  } else if (d.status === "inactive") {
    actions += `<button class="secondary don-toggle" data-id="${d.id}" data-status="active" style="width:auto;margin:0">${T.charityBtnToggleOn}</button> `;
  }
  if (d.status !== "expired") {
    actions += `<button class="contrast outline don-delete" data-id="${d.id}" style="width:auto;margin:0">${T.charityBtnDelete}</button>`;
  }
  return `<tr><td>${esc(d.service)}</td><td class="mono">${esc(d.model)}</td><td>${source}</td><td>${statusBadge}</td><td>${remaining}</td><td class="muted">${deadline}</td><td class="row-actions">${actions}</td></tr>`;
}

async function loadAdminDonations() {
  try {
    const data = await api("/api/admin/donations");
    const list = data.donations || [];
    donPager.data = list;
    renderPaged(donPager, "#don-rows", "#don-pager", 7);
  } catch (err) {
    $("#don-rows").innerHTML = `<tr><td colspan="7" class="muted">${T.error.replace("{msg}", err.message)}</td></tr>`;
    $("#don-pager").innerHTML = "";
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
  const body = {
    service: f.service.value,
    model: f.model.value.trim(),
    dify_base_url: f.dify_base_url.value.trim(),
    dify_api_key: f.dify_api_key.value.trim(),
    source_user_id: sourceUserId,
    source_text: f.source_text.value.trim(),
    deadline,
    total_count: parseInt(f.total_count.value, 10),
    note: f.note.value.trim(),
  };
  const note = $("#don-note");
  note.innerHTML = `<p class="muted">${T.loading}</p>`;
  try {
    await api("/api/admin/donations", { method: "POST", body });
    note.innerHTML = `<div class="note ok">${T.charityCreated}</div>`;
    f.reset();
    $("#don-source-user").value = "";
    await loadAdminDonations();
  } catch (err) {
    note.innerHTML = `<div class="note err">${T.error.replace("{msg}", esc(err.message))}</div>`;
  }
}

// Delegate click events for donation actions (toggle/delete).
document.addEventListener("click", async (ev) => {
  const btn = ev.target.closest(".don-toggle");
  if (btn) {
    const id = btn.dataset.id;
    const status = btn.dataset.status;
    try {
      await api(`/api/admin/donations/${id}/status`, { method: "POST", body: { status } });
      toast(T.charityStatusChanged);
      await loadAdminDonations();
    } catch (err) {
      toast(T.error.replace("{msg}", err.message), 3000);
    }
    return;
  }
  const delBtn = ev.target.closest(".don-delete");
  if (delBtn) {
    if (!confirm(T.charityDeleteWarn)) return;
    const id = delBtn.dataset.id;
    try {
      await api(`/api/admin/donations/${id}`, { method: "DELETE" });
      toast(T.charityDeleted);
      await loadAdminDonations();
    } catch (err) {
      toast(T.error.replace("{msg}", err.message), 3000);
    }
    return;
  }
});
