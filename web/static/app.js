/* Dify2API SPA (vanilla JS). Host-aware: admin.<site> renders the admin
 * console; everything else renders the user console. All UI strings live in
 * the i18n dictionary (zh + en). */
"use strict";

const i18n = {
  zh: {
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
  copyFail: "复制失败，请刷新页面后重试",
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
  rpmA: "A",
  rpmB: "B",
  rpmC: "C",
  rpmSave: "保存",
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
  settingsTitle: "系统设置",
  maintenanceMode: "全站维护模式",
  maintenanceModeHint: "开启后用户站点显示维护页面，管理员站点不受影响",
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
  userCharityConfirm: "警告：使用公益资源时，您的请求将被转发至捐赠者配置的 Dify App。捐赠者可通过其 Dify App 后台日志查看完整请求内容。平台不保证捐赠 App 的可靠性，对捐赠者可能的恶意行为免除平台责任。\n\n确定开启？",
  userCharityOn: "公益资源已启用",
  userCharityOff: "公益资源已关闭",
  userCharityBanner: "捐赠/公益系统尚未被管理员启用",
  insufficientCredits: "积分不足",
  // Credits & check-in (alpha.3 F2)
  creditsTitle: "公益积分",
  creditsBalance: "当前余额：{n}",
  creditsCheckin: "签到",
  creditsCheckinDone: "签到成功！获得 {name} +{bonus}，当前余额 {total}",
  creditsCheckedIn: "已签到",
  creditsCheckinDisabled: "签到未开放",
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

  // Bulletin board (alpha.4 B3)
  bulletinTitle: "公告栏",
  bulletinAdminTitle: "公告管理",
  bulletinAdd: "发布公告",
  bulletinEdit: "编辑",
  bulletinDelete: "删除",
  bulletinDeleteConfirm: "确定删除该公告？",
  bulletinSave: "保存",
  bulletinThTitle: "标题",
  bulletinThType: "类型",
  bulletinThCreated: "发布时间",
  bulletinThExpires: "过期时间",
  bulletinThClosable: "可关闭",
  bulletinThActions: "操作",
  bulletinTypeInfo: "信息",
  bulletinTypeWarning: "警告",
  bulletinTypeImportant: "重要",
  bulletinClosableYes: "是",
  bulletinClosableNo: "否",
  bulletinNeverExpires: "永不过期",
  bulletinFieldTitle: "公告标题",
  bulletinFieldContent: "正文（HTML）",
  bulletinFieldType: "类型",
  bulletinFieldSortOrder: "排序权重（数字越大越靠前）",
  bulletinFieldClosable: "允许用户关闭",
  bulletinFieldExpiresAt: "过期时间（可选，留空表示永不过期）",
  bulletinCreated: "公告已发布",
  bulletinUpdated: "公告已更新",
  bulletinDeleted: "公告已删除",
  bulletinSystemNote: "（系统公告，不可编辑/删除）",
  bulletinPreview: "预览",
  bulletinClose: "关闭",
  bulletinNoContent: "暂无公告",

  // --- User tab navigation (alpha.4 Tab-U) ---
  userTabConfigs: "模型配置",
  userTabCredits: "积分签到",
  userTabCharity: "公益",
  userTabLogs: "请求日志",
  userTabDebug: "调试",

  // --- Admin tab navigation (alpha.4 Tab-A) ---
  adminTabSettings: "系统设置",
  adminTabUsers: "用户管理",
  adminTabLogs: "请求日志",
  adminTabDonations: "公益资源",
  adminTabAlerts: "告警中心",
  adminTabBulletins: "公告管理",

  // --- Settings fieldset legends ---
  settingsLegendMaintenance: "站点维护",
  settingsLegendDiscord: "Discord 认证",
  settingsLegendRPM: "速率限制",
  settingsLegendCheckin: "积分签到",
  settingsLegendCharity: "公益与捐赠",
  settingsLegendMailer: "邮件通知",

  // --- New settings fields ---
  donationEnabled: "允许用户提交捐赠",
  donationEnabledHint: "开启后用户可在控制台中提交捐赠申请",
  charityEnabledLabel: "启用公益路由",
  charityEnabledHint: "开启后公益资源可用，用户可在 /v1/models 中看到公益模型",
  donationReviewLimit: "待审核上限",
  donationReviewLimitHint: "每个用户同时最多待审核的捐赠申请数，0 表示不限制",
  creditsGate: "公益门槛积分",
  creditsGateHint: "用户积分需大于此值才能调用公益模型",
  charityCost: "公益每次消耗",
  charityCostHint: "每成功调用一次公益模型消耗的积分",
  donationFailLimitLabel: "连续失败上限",
  donationFailLimitHint: "同类服务连续失败次数达上限后自动停用",
  mailerCoolMinutesLabel: "邮件冷却时间（分钟）",
  mailerCoolMinutesHint: "同一类型邮件的最小发送间隔",

  // --- User management enhancements ---
  userSearch: "搜索用户",
  userSearchPlaceholder: "输入用户名或 Discord ID 搜索…",
  userMoreActions: "更多操作",

  // --- Donation application ---
  donationReviewSection: "待审核申请",
  donationReviewPending: "暂无待审核申请",
  donationApplyBtn: "申请加入公益资源池",
  donationApplyDisabled: "您有 {n} 条待审核申请（上限 {limit}），请等待审核完成后再提交",
  donationApplyTitle: "提交捐赠申请",
  donationApplySubmit: "提交申请",
  donationApplySubmitted: "申请已提交，等待管理员审核",
  donationApplyService: "服务类型",
  donationApplyModel: "模型名（不含方括号）",
  donationApplyBaseURL: "Dify Base URL",
  donationApplyAPIKey: "Dify API Key",
  donationApplyDeadline: "截止时间",
  donationApplyTotalCount: "捐赠次数",
  donationApplyNote: "备注（可选）",
  donationAppStatusPending: "待审核",
  donationAppStatusApproved: "已通过",
  donationAppStatusRejected: "已驳回",
  donationAppThService: "服务",
  donationAppThModel: "模型",
  donationAppThStatus: "状态",
  donationAppThCreated: "提交时间",
  donationAppThNote: "备注",
  donationAppThDonation: "捐赠条目状态",
  donationAppDonationInactive: "未激活（管理员可启用）",
  donationAppDonationActive: "使用中",
  donationAppDonationExpired: "已失效",
  donationReviewBtn: "审核",
  donationReviewTitle: "审核捐赠申请",
  donationReviewApprove: "通过",
  donationReviewReject: "驳回",
  donationReviewNote: "审核备注",
  donationReviewModifyHint: "修改以下字段将覆盖申请人的原始值（留空则沿用原值）",
  donationReviewApproved: "已通过审核",
  donationReviewRejected: "已驳回",
  donationReviewNoPending: "暂无待审核申请",
  donationAppThPendingCount: "待审核申请（{n}）",
  donationAppThApplicant: "申请人",
  donationAppThDeadline: "截止时间",
  donationAppThCount: "次数",

  // Debug tab
  debugWarning: "⚠️ 调试模式警告\n\n开启后，您的每一次 API 请求的完整内容（包括 Dify App 密钥、\n对话消息、请求参数与服务端响应）将在您的浏览器中直接展示。\nDify2API 服务端不会将这些数据写入磁盘或数据库，调试数据仅\n存在于浏览器的当前页面中。\n\n- 请勿在公共设备或他人可查看您屏幕的环境中使用此功能。\n- 关闭当前页面或手动关闭调试模式后，所有未保存的数据将\n  立即丢失且无法恢复。\n- 您对调试过程中暴露的敏感信息承担全部责任。",
  debugWarningEn: "⚠️ Debug Mode Warning\n\nWhen enabled, the full content of every API request you make\n(including Dify App keys, conversation messages, request\nparameters, and server responses) will be displayed directly\nin your browser. Dify2API does NOT write this data to disk or\ndatabase — debug data exists only within the current browser tab.\n\n- Do not use this feature on public devices or where others\n  can see your screen.\n- Closing this page or manually disabling debug mode will\n  cause all unsaved data to be lost immediately and\n  irrecoverably.\n- You assume full responsibility for any sensitive information\n  exposed during debugging.",
  debugConsent: "我已知晓并同意",
  debugStart: "开启调试",
  debugStop: "关闭调试",
  debugDryRunLabel: "发送到 Dify",
  debugDryRunHintOn: "当前为演习模式：请求经过解析校验但不发往 Dify",
  debugDryRunHintOff: "请求将实际发往 Dify，可能消耗额度或产生费用",
  debugStartConfirm: "开启调试后，您的 API 请求数据将在此页面展示。\n注意：流式（streaming）请求会在服务端完整接收后一次性推送，不会逐帧实时显示。确定开启？",
  debugStreamNote: "提示：流式（streaming）请求将在服务端完整接收后一次性展示，不会逐帧实时推送。",
  debugDryRunOffConfirm: "关闭演习模式后，请求将实际发往 Dify 后端——Dify 可看到您的对话数据，且可能消耗消息额度或产生费用。确定继续？",
  debugNoEvents: "暂无拦截数据。发起 API 请求后，请求原文、Dify 输入映射与服务端响应将在此展示。",
  debugEventCount: "已拦截 {n} 次请求",
  debugConnected: "已连接到调试流",
  debugDisconnected: "调试流已断开，正在重连……",
  debugStreamDone: "调试会话已结束",
  debugStreamReplaced: "调试会话已在其他标签页中被替代",
  debugBtnDisabled: "请先阅读并同意免责声明",
  debugCollapseAll: "全部折叠",
  debugExpandAll: "全部展开",
  debugClose: "关闭",
  debugRawBody: "请求原文 (JSON)",
  debugDifyInputsLabel: "Dify 输入映射",
  debugResponseBodyLabel: "响应",
  langSwitch: "中/EN",
  },
  en: {
  adminTabAlerts: "Alert Center",
  adminTabBulletins: "Bulletins",
  adminTabDonations: "Charity Resources",
  adminTabLogs: "Request Logs",
  adminTabSettings: "System Settings",
  adminTabUsers: "User Management",
  bulletinAdd: "Post Bulletin",
  bulletinAdminTitle: "Bulletin Management",
  bulletinClosableNo: "No",
  bulletinClosableYes: "Yes",
  bulletinClose: "Close",
  bulletinCreated: "Bulletin posted",
  bulletinDelete: "Delete",
  bulletinDeleteConfirm: "Delete this bulletin?",
  bulletinDeleted: "Bulletin deleted",
  bulletinEdit: "Edit",
  bulletinFieldClosable: "Allow users to dismiss",
  bulletinFieldContent: "Content (HTML)",
  bulletinFieldExpiresAt: "Expiry time (optional, leave empty for no expiry)",
  bulletinFieldSortOrder: "Sort order (higher = first)",
  bulletinFieldTitle: "Bulletin title",
  bulletinFieldType: "Type",
  bulletinNeverExpires: "Never expires",
  bulletinNoContent: "No bulletins",
  bulletinSave: "Save",
  bulletinSystemNote: "(System bulletin, cannot be edited/deleted)",
  bulletinThActions: "Actions",
  bulletinThClosable: "Closable",
  bulletinThCreated: "Published",
  bulletinThExpires: "Expires",
  bulletinThTitle: "Title",
  bulletinThType: "Type",
  bulletinTitle: "Bulletin Board",
  bulletinTypeImportant: "Important",
  bulletinTypeInfo: "Info",
  bulletinTypeWarning: "Warning",
  bulletinUpdated: "Bulletin updated",
  charityCost: "Charity cost per call",
  charityCostHint: "Credits consumed per successful charity model call",
  charityEnabledHint: "When enabled, charity resources become available and users can see charity models in /v1/models",
  charityEnabledLabel: "Enable charity routing",
  creditsCheckedIn: "Checked In",
  creditsCheckinDisabled: "Check-in Unavailable",
  creditsCheckinDone: "Check-in successful! Received {name} +{bonus}, current balance {total}",
  creditsCheckinFailed: "Check-in failed",
  creditsGate: "Charity credits gate",
  creditsGateHint: "Users must have more credits than this to use charity models",
  debugBtnDisabled: "Please read and accept the disclaimer first",
  debugClose: "Close",
  debugCollapseAll: "Collapse All",
  debugConnected: "Connected to debug stream",
  debugConsent: "I have read and agree",
  debugDifyInputsLabel: "Dify Input Mapping",
  debugDisconnected: "Debug stream disconnected, reconnecting…",
  debugDryRunHintOff: "Requests will be sent to Dify and may consume quota or incur charges",
  debugDryRunHintOn: "Dry-run mode: requests are parsed and validated but NOT sent to Dify",
  debugDryRunLabel: "Send to Dify",
  debugDryRunOffConfirm: "After disabling dry-run mode, requests will be sent to Dify — Dify can see your data and may consume quota or incur charges. Continue?",
  debugEventCount: "{n} request(s) intercepted",
  debugExpandAll: "Expand All",
  debugNoEvents: "No intercepted data yet. Make an API request and its raw data, Dify input mapping, and server response will appear here.",
  debugRawBody: "Request Body (JSON)",
  debugResponseBodyLabel: "Response",
  debugStart: "Start Debug",
  debugStartConfirm: `After starting debug, your API request data will be displayed on this page.
Note: streaming requests will be displayed all at once after completion. Continue?`,
  debugStop: "Stop Debug",
  debugStreamDone: "Debug session ended",
  debugStreamNote: "Note: streaming requests will be displayed all at once after completion, not frame by frame.",
  debugStreamReplaced: "Debug session was taken over in another tab",
  debugWarning: `⚠️ Debug Mode Warning

When enabled, the full content of every API request you make
(including Dify App keys, conversation messages, request
parameters, and server responses) will be displayed directly
in your browser. Dify2API does NOT write this data to disk or
database — debug data exists only within the current browser tab.

- Do not use this feature on public devices or where others
  can see your screen.
- Closing this page or manually disabling debug mode will
  cause all unsaved data to be lost immediately and
  irrecoverably.
- You assume full responsibility for any sensitive information
  exposed during debugging.`,
  debugWarningEn: "⚠️ Debug Mode Warning (English — see collapsed section above)",
  donationAppDonationActive: "Active",
  donationAppDonationExpired: "Expired",
  donationAppDonationInactive: "Inactive (admin can activate)",
  donationAppStatusApproved: "Approved",
  donationAppStatusPending: "Pending",
  donationAppStatusRejected: "Rejected",
  donationAppThApplicant: "Applicant",
  donationAppThCount: "Count",
  donationAppThCreated: "Submitted",
  donationAppThDeadline: "Deadline",
  donationAppThDonation: "Donation Status",
  donationAppThModel: "Model",
  donationAppThNote: "Note",
  donationAppThPendingCount: "Pending Applications ({n})",
  donationAppThService: "Service",
  donationAppThStatus: "Status",
  donationApplyAPIKey: "Dify API Key",
  donationApplyBaseURL: "Dify Base URL",
  donationApplyBtn: "Apply to Join Charity Pool",
  donationApplyDeadline: "Deadline",
  donationApplyDisabled: "You have {n} pending applications (limit {limit}). Please wait for review before submitting more.",
  donationApplyModel: "Model name (no brackets)",
  donationApplyNote: "Note (optional)",
  donationApplyService: "Service type",
  donationApplySubmit: "Submit",
  donationApplySubmitted: "Application submitted, awaiting admin review",
  donationApplyTitle: "Submit Donation Application",
  donationApplyTotalCount: "Donation count",
  donationEnabled: "Allow users to submit donations",
  donationEnabledHint: "When enabled, users can submit donation applications from their dashboard",
  donationFailLimitHint: "Auto-deactivate when consecutive failures for the same service reach this limit",
  donationFailLimitLabel: "Consecutive failure limit",
  donationReviewApprove: "Approve",
  donationReviewApproved: "Approved",
  donationReviewBtn: "Review",
  donationReviewLimit: "Pending review limit",
  donationReviewLimitHint: "Max pending donation applications per user. 0 means no limit.",
  donationReviewModifyHint: "Modifying fields below will override the applicant's original values (leave empty to keep original)",
  donationReviewNoPending: "No pending applications",
  donationReviewNote: "Review note",
  donationReviewPending: "No pending applications",
  donationReviewReject: "Reject",
  donationReviewRejected: "Rejected",
  donationReviewSection: "Pending Applications",
  donationReviewTitle: "Review Donation Application",
  langSwitch: "EN/中",
  mailerCoolMinutesHint: "Minimum interval between emails of the same type",
  mailerCoolMinutesLabel: "Email cooldown (minutes)",
  maintenanceMode: "Site Maintenance Mode",
  maintenanceModeHint: "When enabled, the user site displays a maintenance page. The admin site is unaffected.",
  settingsLegendCharity: "Charity & Donations",
  settingsLegendCheckin: "Credits & Check-in",
  settingsLegendDiscord: "Discord Authentication",
  settingsLegendMailer: "Email Notifications",
  settingsLegendMaintenance: "Site Maintenance",
  settingsLegendRPM: "Rate Limiting",
  userMoreActions: "More actions",
  userSearch: "Search users",
  userSearchPlaceholder: "Enter username or Discord ID to search…",
  userTabCharity: "Charity",
  userTabConfigs: "Model Configs",
  userTabCredits: "Credits & Check-in",
  userTabDebug: "Debug",
  userTabLogs: "Request Logs",
  }
};

let currentLang = (() => { try { return localStorage.getItem('lang') || 'en'; } catch { return 'en'; } })();

// syncLangFromServer applies the user's persisted language preference from /api/me.
// Call after state.me is populated.  Falls back to localStorage, then "en".
function syncLangFromServer() {
  if (state.me && state.me.lang) {
    currentLang = state.me.lang;
    try { localStorage.setItem('lang', currentLang); } catch {}
  }
}

function T(key) {
  const dict = i18n[currentLang];
  if (dict && dict[key] !== undefined) return dict[key];
  return i18n.zh[key] || key;
}

async function switchLang() {
  currentLang = currentLang === 'zh' ? 'en' : 'zh';
  try { localStorage.setItem('lang', currentLang); } catch {}
  // Persist to server if logged in.
  if (state.me && !state.me.is_admin) {
    api("/api/me/lang", { method: "PUT", body: { lang: currentLang } }).catch(() => {});
  }
  // Update the lang switch button text.
  const btn = $("#lang-switch");
  if (btn) btn.textContent = T('langSwitch');
  route();
}

/* ---------------- helpers ---------------- */
const $ = (sel) => document.querySelector(sel);
const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const fmtT = (ts) => (ts ? new Date(ts * 1000).toLocaleString() : "—");
const fmtDate = (ts) => (ts ? new Date(ts * 1000).toLocaleDateString() : "—");

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

/* ---------------- state & init ---------------- */
const state = { site: null, me: null, mode: "user" };

(async function init() {
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
  if (btn) btn.onclick = async () => {
    // Clean up debug session before logout.
    closeDebugSSE();
    try { await api("/api/me/debug/stop", { method: "POST" }); } catch (_) {}
    await api("/api/auth/logout", { method: "POST" }).catch(() => {});
    state.me = null;
    route();
  };
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
  $(rowsSel).innerHTML = items.length ? items.map(p.rowFn).join("") : `<tr><td colspan="${emptyCols}" class="muted">${T('empty')}</td></tr>`;
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
        <h3>数据管理</h3>
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
          <div style="display:grid;grid-template-columns:auto 1fr;gap:.5rem;align-items:end">
            <label>${T('thService')}<select name="service" id="cfg-service"></select></label>
            <label>${T('thModel')}<input name="backend" placeholder="${T('fieldBackend')}（不得含方括号 [ ] 或保留前缀）" required></label>
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
        <h3>我的捐赠</h3>
        <div id="my-donations-content"></div>
      </section>
    </div>

    <!-- Logs tab -->
    <div id="utab-logs" class="user-tab-content" style="display:none">
      <section class="card">
        <h3>${T('logsTitle')}</h3>
        <div class="table-wrap"><table><thead><tr><th>${T('thTime')}</th><th>${T('thDuration')}</th><th>${T('thModel')}</th><th>${T('thStatus')}</th><th>${T('thErrorCode')}</th></tr></thead><tbody id="log-rows"></tbody></table></div>
        <div class="row-actions" id="log-pager" style="margin-top:.5rem"></div>
      </section>
    </div>

    <!-- Debug tab -->
    <div id="utab-debug" class="user-tab-content" style="display:none">
      <section class="card" id="debug-section">
        <h3>${T('userTabDebug')}</h3>
        <p class="muted">调试功能将在后续版本中提供</p>
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
        <pre style="white-space:pre-wrap;font-size:.85rem;margin:0">${esc(currentLang === 'zh' ? i18n.zh.debugWarning : i18n.en.debugWarning)}</pre>
        <details style="margin-top:.5rem"><summary>${currentLang === 'zh' ? 'English' : '中文'}</summary>
          <pre style="white-space:pre-wrap;font-size:.85rem;margin:0">${esc(currentLang === 'zh' ? i18n.en.debugWarning : i18n.zh.debugWarning)}</pre>
        </details>
        <button id="debug-consent-btn" class="secondary" style="margin-top:.75rem">${T('debugConsent')}</button>
      </div>`;
  } else if (_debugActive) {
    // Active debug: controls + event log.
    html += `
      <div class="row-actions" style="gap:.5rem;flex-wrap:wrap">
        <button id="debug-stop-btn" class="contrast">${T('debugStop')}</button>
        <label style="display:flex;align-items:center;gap:.4rem;white-space:nowrap">
          <input type="checkbox" id="debug-dryrun-toggle" role="switch" ${_debugDryRun ? "checked" : ""}>
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
        <pre style="white-space:pre-wrap;font-size:.85rem;margin:0">${esc(currentLang === 'zh' ? i18n.zh.debugWarning : i18n.en.debugWarning)}</pre>
        <details style="margin-top:.5rem"><summary>${currentLang === 'zh' ? 'English' : '中文'}</summary>
          <pre style="white-space:pre-wrap;font-size:.85rem;margin:0">${esc(currentLang === 'zh' ? i18n.en.debugWarning : i18n.zh.debugWarning)}</pre>
        </details>
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
      dryToggle.onchange = () => toggleDebugDryRun(dryToggle.checked);
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
  if (evt.event === "replaced") {
    return `<div class="card" style="padding:.5rem 1rem;margin-bottom:.5rem;border-left:4px solid var(--pico-warn-color,#f9a825)">
      <strong>⚠ ${T('debugStreamReplaced')}</strong>
    </div>`;
  }

  const ts = new Date(evt.timestamp * 1000).toLocaleTimeString();
  const req = evt.request || {};
  const resp = evt.response;
  const hasError = !!evt.error;

  const reqBodyStr = typeof req.body === "object" ? JSON.stringify(req.body, null, 2) : String(req.body ?? "");
  const inputsStr = evt.dify_inputs ? JSON.stringify(evt.dify_inputs, null, 2) : "（无）";
  const respBodyStr = resp ? (resp.body || "") : "（无）";

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
        <p style="margin:0 0 .25rem"><strong>${T('debugRawBody')}:</strong></p>
        <pre style="max-height:16em;overflow:auto;font-size:.8rem;background:var(--pico-code-bg,#1a1a2e);color:var(--pico-code-color,#e0e0e0);padding:.5rem;border-radius:4px">${esc(reqBodyStr)}</pre>
        <p style="margin:.5rem 0 .25rem"><strong>${T('debugDifyInputsLabel')}:</strong></p>
        <pre style="max-height:16em;overflow:auto;font-size:.8rem;background:var(--pico-code-bg,#1a1a2e);color:var(--pico-code-color,#e0e0e0);padding:.5rem;border-radius:4px">${esc(inputsStr)}</pre>
        <p style="margin:.5rem 0 .25rem"><strong>${T('debugResponseBodyLabel')}:</strong></p>
        <pre style="max-height:16em;overflow:auto;font-size:.8rem;background:var(--pico-code-bg,#1a1a2e);color:var(--pico-code-color,#e0e0e0);padding:.5rem;border-radius:4px">${esc(respBodyStr)}</pre>
      </div>
    </details>`;
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
    // Turning dry-run OFF (sending to Dify) — require secondary confirmation.
    if (!confirm(T('debugDryRunOffConfirm'))) {
      // Revert toggle.
      const t = $("#debug-dryrun-toggle");
      if (t) t.checked = true;
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
      // Handle replaced event: another tab took over the debug session.
      if (evt.event === "replaced") {
        closeDebugSSE();
        _debugActive = false;
        toast(T('debugStreamReplaced'), 5000);
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
    $("#credits-info").innerHTML = `<p class="muted">${T('error').replace("{msg}", "无法加载积分信息")}</p>`;
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
  if (!globalOff) {
    try {
      const data = await api("/api/me/charity");
      enabled = data.charity_enabled;
    } catch { /* use default false */ }
  }
  const statusText = enabled ? T('userCharityOn') : T('userCharityOff');
  card.innerHTML = `
    ${globalOff ? `<article class="note warn" style="margin-bottom:.75rem">${T('userCharityBanner')}</article>` : ""}
    <h3>${T('userCharityToggle')}</h3>
    <p class="muted">${esc(statusText)}</p>
    <label style="display:flex;align-items:center;gap:.75rem">
      <input type="checkbox" id="charity-toggle" role="switch" ${enabled ? "checked" : ""}>
      <span>${T('userCharityToggle')}</span>
    </label>`;
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

    // Hidden form.
    html += `<form id="donation-apply-form" style="display:none;margin-bottom:1rem;padding:.75rem;border:1px solid var(--pico-muted-border-color);border-radius:4px">
      <h4>${T('donationApplyTitle')}</h4>
      <div style="display:grid;grid-template-columns:auto 1fr;gap:.5rem;align-items:end">
        <label>${T('donationApplyService')}<select name="service" id="don-apply-service"></select></label>
        <label>${T('donationApplyModel')}<input name="model" placeholder="${T('donationApplyModel')}" required></label>
      </div>
      <label>${T('donationApplyBaseURL')}<input name="dify_base_url" placeholder="https://api.dify.ai/v1" required></label>
      <label>${T('donationApplyAPIKey')}<input name="dify_api_key" placeholder="app-…" required></label>
      <label>${T('donationApplyDeadline')}<input name="deadline" type="datetime-local" required></label>
      <label>${T('donationApplyTotalCount')}<input name="total_count" type="number" min="1" value="100" required></label>
      <label>${T('donationApplyNote')}<textarea name="note" rows="2"></textarea></label>
      <button type="submit">${T('donationApplySubmit')}</button>
      <span id="donation-apply-msg" class="muted" style="margin-left:.75rem"></span>
    </form>`;
  } else {
    html += `<article class="note warn">${esc(T('userCharityBanner')) || "捐赠系统当前未开放"}</article>`;
  }

  // Application status table.
  if (apps.length > 0) {
    html += `<div class="table-wrap"><table><thead><tr>
      <th>${T('donationAppThService')}</th><th>${T('donationAppThModel')}</th>
      <th>${T('donationAppThStatus')}</th><th>${T('donationAppThCreated')}</th>
      <th>${T('donationAppThNote')}</th><th>${T('donationAppThDonation')}</th>
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
      }
      if (a.status === "rejected" && a.review_note) {
        donationCell = `<span class="muted">${esc(a.review_note)}</span>`;
      }
      html += `<tr>
        <td>${esc(a.service)}</td><td class="mono">${esc(a.model)}</td>
        <td>${statusBadge}</td><td class="muted">${fmtT(a.created_at)}</td>
        <td class="muted wrap">${esc(a.note || "—")}</td>
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
      $("#don-apply-service").innerHTML = (services || []).map((s) => `<option value="${esc(s.name)}">${esc(s.label)}</option>`).join("");
    } catch { /* silently ignore */ }

    form.onsubmit = async (e) => {
      e.preventDefault();
      const f = e.target;
      const deadline = f.deadline.value ? Math.floor(new Date(f.deadline.value).getTime() / 1000) : 0;
      const msg = $("#donation-apply-msg");
      msg.textContent = T('loading');
      try {
        await api("/api/me/donations", {
          method: "POST",
          body: {
            service: f.service.value,
            model: f.model.value.trim(),
            dify_base_url: f.dify_base_url.value.trim(),
            dify_api_key: f.dify_api_key.value.trim(),
            deadline,
            total_count: parseInt(f.total_count.value, 10),
            note: f.note.value.trim(),
          },
        });
        msg.textContent = "";
        toast(T('donationApplySubmitted'));
        form.style.display = "none";
        f.reset();
        await renderMyDonations();
      } catch (err) {
        msg.textContent = T('error').replace("{msg}", err.message);
      }
    };
  }
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
        <button class="secondary cfg-edit">${T('editConfig')}</button>
        <button class="contrast outline cfg-del">${T('deleteConfig')}</button>
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
    f.dify_api_key.placeholder = T('fieldAPIKey');
    f.note.value = c.note || "";
    $("#cfg-submit").textContent = T('save');
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
  note.innerHTML = `<p class="muted">${T('loading')}</p>`;
  try {
    const resp = editingId
      ? await api(`/api/configs/${editingId}`, { method: "PUT", body })
      : await api("/api/configs", { method: "POST", body });
    editingId = null;
    $("#cfg-submit").textContent = T('addConfig');
    f.reset();
    const c = resp.app_check || {};
    let cls = "ok", html = "";
    if (c.error) {
      cls = "warn"; html = `${T('checkError')}: ${esc(c.error)}`;
    } else if (c.compatible) {
      html = `${T('checkCompatible')}`;
      if (c.extra_app_optional?.length) html += `<br><span class="muted">${T('checkExtra').replace("{list}", esc(c.extra_app_optional.join(", ")))}</span>`;
    } else {
      cls = "err";
      html = `${T('checkIncompatible')}`;
      if (c.missing_contract_vars?.length) html += `<br>${T('checkMissing').replace("{list}", esc(c.missing_contract_vars.join(", ")))}`;
      if (c.uncovered_app_required?.length) html += `<br>${T('checkUncovered').replace("{list}", esc(c.uncovered_app_required.join(", ")))}`;
    }
    note.innerHTML = `<div class="note ${cls}">${html}</div>`;
    await loadConfigs();
  } catch (err) {
    note.innerHTML = `<div class="note err">${T('error').replace("{msg}", esc(err.message))}</div>`;
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
  await renderAdminDonationReview();
  await loadAdminDonations();
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
  const tabs = ["settings", "users", "logs", "donations", "alerts", "bulletins"];
  const tabLabels = {
    settings: T('adminTabSettings'), users: T('adminTabUsers'), logs: T('adminTabLogs'),
    donations: T('adminTabDonations'), alerts: T('adminTabAlerts'), bulletins: T('adminTabBulletins'),
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
              <label style="flex:1 1 10rem">签到最低积分<input name="checkin_min" type="number" min="1" required></label>
              <label style="flex:1 1 10rem">签到最高积分<input name="checkin_max" type="number" min="1" required></label>
              <label style="flex:1 1 10rem">积分上限<input name="credits_cap" type="number" min="0" required></label>
            </div>
            <div style="display:flex;flex-wrap:wrap;gap:.75rem;margin-top:.25rem">
              <label style="flex:1 1 10rem">${T('creditsGate')}<input name="credits_gate" type="number" min="0" required></label>
              <label style="flex:1 1 10rem">${T('charityCost')}<input name="charity_cost" type="number" min="1" required></label>
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
        <div class="table-wrap"><table><thead><tr><th><input type="checkbox" id="select-all" title="全选"></th><th>${T('thUser')}</th><th>${T('thCredits')}</th><th>${T('thDonationCredit')}</th><th>${T('thRPM')}</th><th>${T('thCreated')}</th><th>${T('thStatus')}</th><th>${T('thActions')}</th></tr></thead><tbody id="user-rows"></tbody></table></div>
        <div class="row-actions" id="user-pager" style="margin-top:.5rem"></div>
      </section>
    </div>

    <!-- Logs tab -->
    <div id="tab-logs" class="admin-tab-content" style="display:none">
      <section class="card">
        <h3>${T('adminLogsTitle')}</h3>
        <div id="admin-logs-filter" style="display:flex;flex-wrap:wrap;gap:.75rem;align-items:flex-end;margin-bottom:.8rem">
          <label style="flex:1 1 16rem;min-width:14rem;margin-bottom:0">${T('thUser')}
            <input id="alf-user" list="alf-user-list" placeholder="${T('adminLogsUserSearch')}" autocomplete="off">
            <datalist id="alf-user-list"></datalist>
          </label>
          <label style="flex:0 1 12rem;min-width:10rem;margin-bottom:0">${T('thService')}<select id="alf-service"><option value="">${T('adminLogsAllServices')}</option></select></label>
          <label style="flex:0 1 12rem;min-width:10rem;margin-bottom:0">${T('adminLogsModel')}<input id="alf-model" placeholder="[公益][general]x" style="margin-bottom:0"></label>
          <label style="flex:0 1 8rem;min-width:7rem;margin-bottom:0">${T('thStatus')}<select id="alf-status"><option value="">${T('adminLogsAllStatus')}</option><option value="success">${T('adminLogsSuccess')}</option><option value="error">${T('adminLogsError')}</option></select></label>
          <label style="flex:0 1 13rem;min-width:11rem;margin-bottom:0">${T('adminLogsSince')}<input id="alf-since" type="datetime-local"></label>
          <label style="flex:0 1 13rem;min-width:11rem;margin-bottom:0">${T('adminLogsUntil')}<input id="alf-until" type="datetime-local"></label>
          <button id="alf-query" style="flex:0 0 auto;width:auto;margin-bottom:0">${T('adminLogsQuery')}</button>
        </div>
        <div class="table-wrap"><table><thead><tr><th>${T('thTime')}</th><th>${T('thUser')}</th><th>${T('thModel')}</th><th>${T('thService')}</th><th>${T('thDuration')}</th><th>${T('thStatus')}</th><th>${T('thHTTPStatus')}</th><th>${T('thErrorCode')}</th><th>${T('thErrorDetail')}</th><th>${T('thDonationSource')}</th></tr></thead><tbody id="alf-rows"></tbody></table></div>
        <div class="row-actions" id="alf-pager" style="margin-top:.5rem"></div>
      </section>
    </div>

    <!-- Donations tab -->
    <div id="tab-donations" class="admin-tab-content" style="display:none">
      <section class="card">
        <h3>${T('charityTitle')}</h3>
        <!-- Donation review section -->
        <div id="donation-review-section" style="margin-bottom:1.5rem;padding:.75rem;border:1px solid var(--pico-muted-border-color);border-radius:4px">
          <h4>${T('donationReviewSection')}</h4>
          <div id="donation-review-content"></div>
        </div>
        <form id="donation-form">
          <div style="display:grid;grid-template-columns:auto 1fr;gap:.5rem;align-items:end">
            <label>${T('charityService')}<select name="service" id="don-service"></select></label>
            <label>${T('charityModel')}<input name="model" placeholder="${T('charityModel')}" required></label>
          </div>
          <label>${T('charityBaseURL')}<input name="dify_base_url" placeholder="https://api.dify.ai/v1" required></label>
          <label>${T('charityAPIKey')}<input name="dify_api_key" placeholder="app-…" required></label>
          <label>${T('charitySourceUser')}
            <input id="don-source-user" list="don-user-list" placeholder="${T('charitySourceUserHint')}" autocomplete="off">
            <datalist id="don-user-list"></datalist>
          </label>
          <label>${T('charitySourceText')}<input name="source_text" placeholder="当未选来源用户时填写此项"></label>
          <label>${T('charityDeadline')}<input name="deadline" type="datetime-local" required></label>
          <label>${T('charityTotalCount')}<input name="total_count" type="number" min="1" required></label>
          <label>${T('charityNote')}<input name="note" placeholder="${T('charityNote')}"></label>
          <div id="don-note"></div>
          <button type="submit">${T('charitySubmit')}</button>
        </form>
        <div class="table-wrap" style="margin-top:1.5rem"><table><thead><tr>
          <th>${T('charityThService')}</th><th>${T('charityThModel')}</th><th>${T('charityThSource')}</th>
          <th>${T('charityThStatus')}</th><th>${T('charityThRemaining')}</th><th>${T('charityThDeadline')}</th>
          <th>${T('thActions')}</th>
        </tr></thead><tbody id="don-rows"></tbody></table></div>
        <div class="row-actions" id="don-pager" style="margin-top:.5rem"></div>
      </section>
    </div>

    <!-- Alerts tab -->
    <div id="tab-alerts" class="admin-tab-content" style="display:none">
      <section class="card">
        <h3>${T('alertTitle')}</h3>
        <div class="table-wrap"><table><thead><tr><th><input type="checkbox" id="alert-select-all" title="全选"></th><th>${T('thTime')}</th><th>${T('alertType')}</th><th>${T('alertMessage')}</th><th>${T('thActions')}</th></tr></thead><tbody id="alert-rows"></tbody></table></div>
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
  sf.credits_gate.value = s.credits_gate;
  sf.charity_cost.value = s.charity_cost;
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
      credits_gate: parseInt(sf.credits_gate.value, 10) || 0,
      charity_cost: parseInt(sf.charity_cost.value, 10),
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
    let txt = (u.auto_banned ? "RPM 自动" : "") + T('statusBannedUntil').replace("{time}", fmtT(u.banned_until));
    if (u.ban_reason) txt += `<br><span class="muted">原因：${esc(u.ban_reason)}</span>`;
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

function userRow(u) {
  const fmtLim = (v) => (v == null ? "" : String(v));
  const rpm = `
    <span style="display:inline-flex;align-items:center;gap:2px">
      <input class="u-rpm" data-id="${u.id}" data-class="a" type="number" min="1" value="${fmtLim(u.rpm_limit_a)}" placeholder="${T('rpmA')}" style="width:3.5rem;padding:0 .25rem;font-size:.75rem">
      <input class="u-rpm" data-id="${u.id}" data-class="b" type="number" min="1" value="${fmtLim(u.rpm_limit_b)}" placeholder="${T('rpmB')}" style="width:3.5rem;padding:0 .25rem;font-size:.75rem">
      <input class="u-rpm" data-id="${u.id}" data-class="c" type="number" min="1" value="${fmtLim(u.rpm_limit_c)}" placeholder="${T('rpmC')}" style="width:3.5rem;padding:0 .25rem;font-size:.75rem">
      <button class="secondary u-rpm-save" data-id="${u.id}" style="width:auto">${T('rpmSave')}</button>
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
      <td class="row-actions">
        <button class="secondary u-ban">${T('ban')}</button>
        <button class="secondary u-unban">${T('unban')}</button>
        <div class="dropdown-wrapper" style="position:relative;display:inline-block">
          <button class="secondary dropdown-trigger" style="width:auto;margin:0;padding:.3rem .45rem">…</button>
          <div class="dropdown-menu" style="display:none;position:absolute;right:0;top:100%;z-index:100;background:var(--pico-card-background-color);border:1px solid var(--pico-muted-border-color);border-radius:6px;box-shadow:0 4px 12px rgba(0,0,0,.2);min-width:130px;padding:.25rem 0">
            <button class="dropdown-item u-key" data-id="${u.id}" style="display:block;width:100%;text-align:left;background:none;border:none;border-radius:0;padding:.4rem .75rem;cursor:pointer;color:var(--pico-color);margin:0;font-size:.85rem">${T('resetUserKey')}</button>
            <button class="dropdown-item u-export" data-id="${u.id}" style="display:block;width:100%;text-align:left;background:none;border:none;border-radius:0;padding:.4rem .75rem;cursor:pointer;color:var(--pico-color);margin:0;font-size:.85rem">${T('adminExport')}</button>
            <button class="dropdown-item u-del" data-id="${u.id}" style="display:block;width:100%;text-align:left;background:none;border:none;border-radius:0;padding:.4rem .75rem;cursor:pointer;color:var(--pico-color);margin:0;font-size:.85rem">${T('deleteUser')}</button>
          </div>
        </div>
      </td>
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
      document.querySelectorAll(".dropdown-menu").forEach((m) => (m.style.display = "none"));
      menu.style.display = isOpen ? "none" : "block";
    };
  });
  // Close menu after dropdown item click
  document.querySelectorAll(".dropdown-item").forEach((item) => {
    item.addEventListener("click", () => {
      setTimeout(() => {
        document.querySelectorAll(".dropdown-menu").forEach((m) => (m.style.display = "none"));
      }, 50);
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
      if (isNaN(amount) || amount < 0) { toast("请输入有效数值"); return; }
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
  renderPaged(userPager, "#user-rows", "#user-pager", 8);
  bindUserRowActions();
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
  // Server-side pagination: "全部" uses a large value (no server cap).
  const size = adminLogPager.size === Infinity ? 10000 : adminLogPager.size;
  params.set("limit", String(size));
  params.set("offset", String((adminLogPager.page - 1) * size));

  try {
    const data = await api(`/api/admin/logs?${params.toString()}`);
    renderAdminLogs(data);
  } catch (err) {
    $("#alf-rows").innerHTML = `<tr><td colspan="10" class="muted">${T('error').replace("{msg}", err.message)}</td></tr>`;
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
    : `<tr><td colspan="10" class="muted">${T('empty')}</td></tr>`;

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
  blocking_failed_200: T('alertTypeBlockingFailed200'),
  donation_exhausted_race: "公益资源竞争耗尽",
};

function alertRow(a) {
  const typeLabel = alertTypeLabels[a.type] || esc(a.type);
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
      <td class="row-actions">${actionsHtml}</td>
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
  const statusMap = { active: T('charityStatusActive'), inactive: T('charityStatusInactive'), expired: T('charityStatusExpired') };
  const statusBadge = `<span class="badge ${d.status === "active" ? "ok" : d.status === "expired" ? "off" : "warn"}">${esc(statusMap[d.status] || d.status)}</span>`;
  const remaining = `${d.remaining_count}/${d.total_count}`;
  const deadline = fmtT(d.deadline);
  const source = esc(d.source_display || "—");
  let actions = "";
  if (d.status === "active") {
    actions += `<button class="secondary don-toggle" data-id="${d.id}" data-status="inactive" style="width:auto;margin:0">${T('charityBtnToggleOff')}</button> `;
  } else if (d.status === "inactive") {
    actions += `<button class="secondary don-toggle" data-id="${d.id}" data-status="active" style="width:auto;margin:0">${T('charityBtnToggleOn')}</button> `;
  }
  if (d.status !== "expired") {
    actions += `<button class="contrast outline don-delete" data-id="${d.id}" style="width:auto;margin:0">${T('charityBtnDelete')}</button>`;
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
    $("#don-rows").innerHTML = `<tr><td colspan="7" class="muted">${T('error').replace("{msg}", err.message)}</td></tr>`;
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

    let html = `<div class="table-wrap"><table><thead><tr>
      <th>${T('donationAppThApplicant')}</th><th>${T('donationAppThService')}</th><th>${T('donationAppThModel')}</th>
      <th>${T('donationAppThDeadline')}</th><th>${T('donationAppThCount')}</th><th>${T('donationAppThNote')}</th>
      <th>${T('donationAppThCreated')}</th><th>${T('thActions')}</th>
    </tr></thead><tbody>`;
    for (const a of apps) {
      const applicant = a.username ? `${esc(a.username)} <span class="muted mono">(${esc(a.discord_id || "")})</span>` : esc(String(a.user_id));
      html += `<tr>
        <td>${applicant}</td>
        <td>${esc(a.service)}</td><td class="mono">${esc(a.model)}</td>
        <td class="muted">${fmtT(a.deadline)}</td>
        <td class="mono">${esc(String(a.total_count))}</td>
        <td class="muted wrap" style="max-width:12rem">${esc(a.note || "—")}</td>
        <td class="muted">${fmtT(a.created_at)}</td>
        <td class="row-actions">
          <button class="secondary don-review-btn" data-id="${a.id}">${T('donationReviewBtn')}</button>
        </td>
      </tr>`;
    }
    html += `</tbody></table></div>`;
    container.innerHTML = html;

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
        <label>${T('donationApplyBaseURL')}<input name="dify_base_url" value="${esc(app.dify_base_url)}"></label>
        <label>${T('donationApplyAPIKey')}<input name="dify_api_key" placeholder="留空则沿用原密钥"></label>
        <div style="display:grid;grid-template-columns:1fr 1fr;gap:.5rem">
          <label>${T('donationAppThDeadline')}<input name="deadline" type="datetime-local" value="${new Date(app.deadline * 1000).toISOString().slice(0, 16)}"></label>
          <label>${T('donationAppThCount')}<input name="total_count" type="number" min="1" value="${esc(String(app.total_count))}"></label>
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
    const body = {
      service: f.querySelector("[name=service]").value.trim(),
      model: f.querySelector("[name=model]").value.trim(),
      dify_base_url: f.querySelector("[name=dify_base_url]").value.trim(),
      dify_api_key: f.querySelector("[name=dify_api_key]").value.trim(),
      total_count: parseInt(f.querySelector("[name=total_count]").value, 10),
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

// renderBulletinBoard: renders the bulletin board container for the user site.
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
      <td>${esc(b.title)}</td>
      <td>${typeCell}</td>
      <td class="muted">${created}</td>
      <td class="muted">${expires}</td>
      <td>${closable}</td>
      <td class="row-actions">${actions}</td>
    </tr>`;
}

async function loadAdminBulletins() {
  try {
    const data = await api("/api/admin/bulletins");
    const list = data.bulletins || [];
    adminBulletinPager.data = list;
    renderPaged(adminBulletinPager, "#admin-bulletin-rows", "#admin-bulletin-pager", 6);
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
        f.querySelector('[name="expires_at"]').value = b.expires_at
          ? new Date(b.expires_at * 1000).toISOString().slice(0, 16)
          : "";
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
    $("#admin-bulletin-rows").innerHTML = `<tr><td colspan="6" class="muted">${T('error').replace("{msg}", err.message)}</td></tr>`;
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
    <div class="table-wrap"><table><thead><tr>
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

// Delegate click events for donation actions (toggle/delete) and RPM save.
document.addEventListener("click", async (ev) => {
  // Close all dropdown menus when clicking outside a dropdown wrapper.
  if (!ev.target.closest(".dropdown-wrapper")) {
    document.querySelectorAll(".dropdown-menu").forEach((m) => (m.style.display = "none"));
  }

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
});
