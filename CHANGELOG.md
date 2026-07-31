# Changelog

Dify2API 的所有重要变更均记录在此文件中。

## [Unreleased]

> 此批变更的最终版本号尚待决定（`v1.1.1` 或 `v1.2.0`）。相较 v1.1.0，
> 除缺陷修复外，还包含新的安全配置、公益持久化结算机制和管理端图表能力。

### 新增

- **公益持久化预留账本**：新增 `charity_reservations`，以
  `reserved → dispatched → committed/released` 状态机记录每次公益调用；价格、奖励和捐赠者均在预留时快照，启动时自动恢复中断记录
- **管理员日志图表升级**：内嵌并按需加载经完整性校验的 Chart.js 4.5.1，展示最近七天成功/错误堆叠图和按服务统计；支持主题、双语、响应式及无障碍文本摘要
- **出站与代理安全配置**：新增 `TRUSTED_PROXY_CIDRS`、`DIFY_EGRESS_ALLOWLIST`、
  `REMOTE_CONTENT_ORIGIN_ALLOWLIST`、`DIFY_MAX_RESPONSE_MB`、`DIFY_PROBE_IN_FLIGHT` 和 `MAX_WEB_REQUEST_BODY_KB`
- **第三方许可清单**：新增 `THIRD_PARTY_NOTICES.md`，记录 Chart.js、`@kurkle/color` 与 Pico CSS 的 MIT notice

### 变更

- **Dify 出站默认拒绝私网**：阻断 loopback、私网、link-local、metadata、DNS rebinding、跨 origin 重定向和环境代理；私有 Dify 必须由部署者显式允许
- **远程内容默认禁用**：`website-summary` URL 与远程图片只有精确 origin 位于允许列表时才会交给 Dify 抓取；参考 Website Summary DSL 同步缩短网络超时和重试
- **请求生命周期与资源上限**：所有 Dify 请求绑定下游 context；客户端断开会取消上游并使用独立短 context 尝试 Stop；限制解压后 JSON、累计 SSE 和 App 探测并发
- **公益结算事务化**：捐赠次数、消费者积分和 reservation 在同一 SQLite 事务内预留；明确前置/上游失败原子退款，已 dispatched 的不确定结果保守结算；捐赠者贡献和奖励幂等提交
- **公益 RPM 原子占位**：契约、远程 URL 和 debug dry-run 校验提前到占位前；失败可释放 lease，避免并发穿透或无效请求消耗额度
- **日志严格逐条留存 30 天**：普通、公益、活跃 donation 和 orphan 日志均按自身 `started_at` 清理；绑定告警同事务删除，未绑定告警保留人工处理
- **OAuth state 无状态化**：改为 HMAC 认证、时限约束且绑定 cookie 的 token，不再维护可被匿名请求放大的内存 map
- **SSE 默认内存降低**：`SSE_BUFFER_MB` 默认值由 10 MiB 降至 1 MiB；单行上限 10 MiB，单次累计响应默认 32 MiB

### 修复

- 修复管理员日志筛选区查询/导出按钮在桌面及移动端重叠
- 修复公益模型早期失败日志将 service 记为“公益”而非实际服务的问题
- 修复并发公益调用可能超出捐赠次数、产生负积分、重复奖励或错误 `credits_consumed` 的账务竞争
- 修复签到更新可能以旧快照覆盖并发积分扣减的问题
- 修复用户 Debug session 的状态读写、channel send/close、替换与延迟清理竞争；重连不再被旧 timer 关闭
- 修复 anti-abuse 配置刷新与请求/admin 读取之间的 map 竞争

### 安全加固

- 仅信任 `TRUSTED_PROXY_CIDRS` 中 TCP peer 提供的转发头，从右向左解析代理链；未知 Host 在重定向前返回 421，HTTPS 重定向使用固定配置 origin
- 状态变更类 Web API 增加统一 256 KiB 请求体上限；chat 继续使用独立 10 MiB 上限
- Dify 响应体、SSE 背压发送和 connectivity probe 均增加硬上限或并发门禁
- nginx 示例改为覆盖 XFF，并同步 Host、body 上限和流式代理安全设置

### 升级提示

- 数据库表和索引由启动过程幂等创建，无需手工 SQL；升级前仍必须备份 SQLite 数据库及 master key
- 使用 Docker、远端或多级反代时，必须把实际代理来源 CIDR 加入 `TRUSTED_PROXY_CIDRS`；不要使用 `0.0.0.0/0` 或 `::/0`
- 现有 Dify Base URL 若解析到私网，升级后会返回 `upstream_blocked`，须先配置最小范围的 `DIFY_EGRESS_ALLOWLIST`
- 依赖任意 website-summary URL 或远程图片的部署，须按业务需要配置精确的 `REMOTE_CONTENT_ORIGIN_ALLOWLIST`；默认空列表会拒绝这些请求
- 静态资源要求 CDN 遵守 origin revalidation；`index.html` 最终保持 `Cache-Control: no-cache`，并未改为 `no-store`

## [v1.1.0] — 2026-07-29

> 功能增强与打磨版本。公告系统 Markdown 支持、全站 i18n 错误消息、
> 法律页面英文版、日志导出与图表、移动端响应式适配。

### 新增

- **公告系统增强**：`bulletins` 新增 `content_type` 列（html/markdown），集成 goldmark 服务端 Markdown 渲染，管理端加语言/格式选择器
- **邮件系统增强**：mailer 发送成功日志；userDebugHub 滥用检测（5 次/10 分钟阈值），新增 `EventDebugAbuse` 告警事件
- **法律页面英文版**：新建 `privacy/terms/maintenance.en.html`；服务端按 `?lang=en|zh` 或已登录用户语言偏好选择版本，未登录且未指定参数时默认中文
- **用户端增强**：`/api/me` 新增 `donation_credit` 统计；驳回的捐赠申请支持复制信息重新提交；管理端新增申请历史筛选列表（按状态/用户）
- **API 错误消息 i18n**：新建 `handler/i18n.go`（canonical 语言检测 + `t` 辅助函数），主要网关错误消息实现中英双语
- **管理员日志导出与图表**：`/api/admin/logs/export`（CSV/JSON 格式）、`/api/admin/logs/stats`（每日统计 + 按服务统计）、Canvas 柱状图
- **移动端响应式适配**：768px/480px 两级断点，dialog 全屏化，nav 折叠，table 次列隐藏，44px 触控目标最小尺寸

### 修复

- 修复管理员请求日志 Canvas 图表颜色与高度
- 修复管理端申请历史默认分页大小与下拉选项不一致
- 曾尝试将 `index.html` 改为 `no-store`，发布前已回滚；v1.1.0 最终行为保持 `no-cache`

## [v1.0.1] — 2026-07-29

> 缺陷修复与小改进。

### 修复

- **P0**：管理员编辑捐赠弹窗的备注字段改 `note` → `review_note`
- `ApproveApplication` 创建捐赠时填入 `source_discord_id` 与 `source_username`

### 新增

- 三处 Discord ID 可点击复制（用户管理、待审核、捐赠列表）
- 手动主题切换（亮色/暗色/跟随系统），`localStorage` 持久化

## [v1.0.0] — 2026-07-27

> 首个正式稳定版（GA）。经过 8 个预发布版本的迭代，Dify2API 现已具备完整的
> 多用户 API 网关能力、内嵌管理台、公益积分体系和防滥用保护。

### 核心能力

- **OpenAI 兼容 API 网关**：`/v1/chat/completions`（流式/非流式）、`/v1/models`
- **多用户隔离**：Discord OAuth 登录（guild + role 门槛），每用户自行绑定 Dify App 凭据
- **双站点架构**：主站（用户 Discord 登录）+ 管理站（admin 子域，密码登录），严格 host 隔离
- **凭据安全**：AES-GCM 加密存储 API 密钥，SHA-256 索引防重复检测

### 支持的服务

| 服务 | 说明 |
|------|------|
| `general` | 通用单轮问答（仅 user_0） |
| `custom` | 自定义单轮问答（user_0 + 可选 system_prompt） |
| `website-summary` | 网页总结（request_url + 可选 instruction） |
| `image-processing` | 图片理解（system_prompt 可选 + user_request + 图片） |
| `sillytavern-main-trimmed` | SillyTavern 主对话（1–22 条宽松角色槽位布局） |
| `sillytavern-main-200` | SillyTavern 大容量对话（1–403 条，200 对 user/assistant） |
| `sillytavern-SP·数据库-填表` | SillyTavern 数据库填表（system + 可选 user + 严格交替 assistant/user + 预填充） |

### 管理台功能

- 用户管理（密钥重置、数据导出、封禁/解封）
- 捐赠审批（三态：pending/active/inactive）+ 批量操作
- 公益资源库（per-donation RPM 限制、加权路由）
- 按模型定价表（独立于全局设置，含积分回报）
- 公告栏（多语言、维护模式切换）
- 防滥用配置（三档开关：off/charity/all，按服务定制惩罚）
- 请求日志（含防滥用触发详情、积分消耗记录）
- 告警中心（邮件聚合：连续失败、定价缺失、用户封禁）
- 管理员单用户数据导出（合规）

### 用户端功能

- API 密钥管理（创建/删除/调用统计）
- Dify App 配置绑定（连通性测试）
- 公益捐赠提交（Key 重复检测、撤回警告）
- 积分签到（随机奖励、日上限、时区偏移）
- 请求日志查看（含防滥用惩罚友好展示）
- 自助 Debug（SSE 流式推送至浏览器，dry-run 模式）
- 自助数据导出/账号删除（合规）

### 安全与防护

- 三类 RPM 限流（A/B/C：传输完成/请求成功/请求接收）
- Web IP 限流 + 无效密钥节流
- 登录爆破防护（滑动窗口 + 恒定时延）
- 防滥用机制（角色校验 + 内容长度门禁，扣分/封禁惩罚可配置）
- 并发信号量（`MAX_CHAT_IN_FLIGHT`，默认 32）
- 客户端断开时自动停止上游 Workflow
- 维护模式（API 返回 503，管理台仍可访问）
- Force HTTPS（301 重定向）

### 性能基线

- Chat completions 网关自身处理：~1,900 RPS（非流式）、~1,475 RPS（流式）
- `/v1/models`：~37,000 RPS
- 测试环境：Go in-process httptest（消除网络延迟），stub Dify 0ms 响应
- 内存模型：每流式连接预分配 `SSE_BUFFER_MB` 字节（默认 10MB），`MAX_CHAT_IN_FLIGHT=32` 安全适配 1GB VPS

### 技术栈

- **后端**：Go ≥1.25 + SQLite（modernc.org/sqlite，免 CGO）
- **前端**：原生 JS + Pico.css，go:embed 零构建链
- **插件**：Pi Coding Agent 子智能体（TypeScript + esbuild）
- **部署**：单二进制，无外部服务依赖
