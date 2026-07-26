# Changelog

Dify2API 的所有重要变更均记录在此文件中。

格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
每个版本以 GitHub Releases 风格的叙述性摘要呈现。

## [v1.0.0] — 2026-07-27

> 首个正式稳定版（GA）。经过 8 个预发布版本（4 个 alpha + 2 个 beta + 2 个 rc）
> 的迭代，Dify2API 现已具备完整的多用户 API 网关能力、内嵌管理台、
> 公益积分体系、防滥用保护，以及丰富的客户端生态集成。

### 核心能力

- **OpenAI 兼容 API 网关**：`/v1/chat/completions`（流式/非流式）、`/v1/models`
- **多用户隔离**：Discord OAuth 登录（guild + role 门槛），每用户自行绑定 Dify App 凭据
- **双站点架构**：主站（用户 Discord 登录）+ 管理站（admin 子域，密码登录），严格 host 隔离
- **凭据安全**：AES-GCM 加密存储 API 密钥，SHA-256 索引防重复检测

### 服务生态

| 服务 | 说明 |
|------|------|
| `sillytavern-main` | SillyTavern 主对话（1–22 槽位，S 可选） |
| `sillytavern-main-200` | SillyTavern 大容量对话（1–403 槽位，200 对） |
| `pi-coding-agent` | Pi Coding Agent 子智能体 |
| `openai-translator` | 通用 OpenAI 翻译 |

### 管理台功能

- 用户管理（密钥重置、数据导出、封禁）
- 捐赠审批（三态：pending/active/inactive）+ 批量操作
- 公益资源库（per-donation RPM 限制、加权路由）
- 按模型定价表（独立于全局设置，含积分回报）
- 公告栏（多语言、维护模式切换）
- 防滥用配置（三档开关：off/charity/all，按服务定制）
- 请求日志（含防滥用触发详情、积分消耗记录）
- 告警中心（邮件聚合：连续失败、定价缺失、用户封禁）
- 管理员单用户数据导出（合规）

### 用户端功能

- API 密钥管理（创建/删除/调用统计）
- Dify App 配置绑定（连通性测试）
- 公益捐赠提交（Key 重复检测、撤回警告）
- 积分签到（随机奖励、日上限）
- 请求日志查看（含防滥用惩罚友好展示）
- 自助 Debug（SSE 流式推送至浏览器，dry-run 模式）
- 自助数据导出/账号删除（合规）

### 安全与防护

- 三类 RPM 限流（A/B/C：传输完成/请求成功/请求接收）
- Web IP 限流 + 无效密钥节流
- 登录爆破防护（滑动窗口 + 恒定时延）
- 防滥用机制（角色校验 + 内容长度门禁，扣分/封禁惩罚）
- 并发信号量（`MAX_CHAT_IN_FLIGHT`）
- 客户端断开时自动停止上游 Workflow
- 维护模式（API 返回 503，管理台仍可访问）
- Force HTTPS（301 重定向）

### 技术栈

- **后端**：Go ≥1.25 + SQLite（modernc.org/sqlite，免 CGO）
- **前端**：原生 JS + Pico.css，go:embed 零构建链
- **插件**：Pi Coding Agent 子智能体（TypeScript + esbuild）
- **部署**：单二进制，无外部服务依赖

---

## [v1.0.0-rc.2] — 2026-07-27

### 新增
- **sillytavern-main-200 服务**：支持 1–403 条消息（200 对 user/assistant），配套 Dify App DSL YAML
- **防滥用日志列**：管理员端 JSON 原文（tooltip）、用户端 i18n 友好格式（扣分/封禁展示）
- **用户捐赠申请备注列**：无论审核状态均展示管理员备注

### 修复
- 管理员用户表格行操作下拉菜单被表格容器裁剪 → `position: fixed` + 视口坐标计算
- 捐赠申请二次确认增加撤回警告文案（中/英双语）
- 定价编辑和捐赠编辑弹窗取消按钮统一为"取消编辑"
- 用户配置编辑弹窗化（`<dialog>` + `showModal()`，预填字段）
- 公告编辑弹窗化（同模式，新建/编辑分离）

### 变更
- `request_logs` 新增 `anti_abuse_info` 列
- 移除 `db/db.go` 中幂等 ALTER TABLE 迁移块，迁移统一由手动 SQL 管理

---

## [v1.0.0-rc.1] — 2026-07-27

### 新增
- **防滥用机制**：`service_anti_abuse` 表 + 三档开关（off/charity/all），角色合法性检查（`invalid_role`）和内容长度门禁（`content_too_short`），扣积分/封禁惩罚可配置
- **防滥用管理前端**：admin 新增 antiabuse 标签页，表格行内编辑 + 一键保存
- **sillytavern-main-trimmed 扩展**：槽位从 10 增至 22，最小消息 2→1，system 改为可选
- **捐赠 Key 重复检测**：`donations` 表新增 `dify_api_key_sha256`，前端独立 Key 列 + 重复警告

### 修复
- 请求日志错误详情列 `max-width` 48rem（可容纳更长错误信息）
- `workflow_finished` 失败路径补 `evt.Data.Text` fallback
- `app.js`（2849 行）拆分为 `common.js` / `user.js` / `admin.js`，便于维护
- `alertTypeLabels` 热切换修复（模块级 const → 函数）

---

## [v1.0.0-beta.2] — 2026-07-26

### 新增
- **按模型定价表**（`charity_pricing`）：取代全局 `charity_cost`，每条定价含价格、捐赠奖励、启用开关
- **定价管理前端**：admin 捐赠标签页内嵌 CRUD 面板；用户端展示定价表格
- **批量管理**：捐赠审批/公益资源库/价格表/公告管理 4 处表格支持多选批量操作（激活/停用/删除/通过/拒绝），原子全拒原则
- **公益启用持续提醒**：用户端横幅警告 + 捐赠说明区域

### 修复
- 捐赠积分回报确认（审批通过时自动发放奖励）
- 捐赠来源显示修复（`source_display` 字段）
- 捐赠默认状态改为 inactive（需管理员批准后激活）
- Tab 导航 CSS（flex 均分填满宽屏）
- 时间显示改为本地时区（`fmtLocalDT`）
- i18n 去重（删除 32 个孤儿 key，新增 `check-i18n.cjs` 自动校验）

### 变更
- 签到默认值调整（min 100→45, max 200→55, cap 500→250）
- `credits_gate` / `charity_cost` 标记为废弃
- DB 初始化不再维护 ALTER TABLE 迁移块

---

## [v1.0.0-beta.1] — 2026-07-25

### 新增
- **公益 per-donation RPM 限制**：`donations` 表 rpm_limit 列 + 内存 60s 滑窗限流器
- **通用 PATCH 字段更新端点**：捐赠的 rpm_limit 等字段可单独修改
- **i18n 全量覆盖**：~40 个新 key + 169 个英文翻译补齐

### 修复
- 管理员端表格行对齐、日志查询区 Grid 重排、标签页滚动条消除
- `row-actions` flex→div 包裹修复

---

## [v1.0.0-alpha.4] — 2026-07-25

### 新增
- **双端标签页化**：用户端和管理端均采用 tab 切换布局
- **公告栏**：管理员发布/编辑/删除公告，用户端展示
- **维护模式**：一键切换，API 返回 503，管理台仍可访问
- **用户自助捐赠**：提交 Dify App 凭据 + 备注，管理员审核
- **用户自助 Debug**：SSE 流式推送至浏览器，dry-run 模式
- **签到增强**：随机奖励 + 积分上限 + 时区偏移
- **公益开关拆分**：`donation_enabled` / `charity_enabled` 独立控制
- **多语言基建**：i18n 字典独立文件 + 系统公告多语言

---

## [v1.0.0-alpha.3] — 2026-07-24

### 新增
- **公益资源子系统**：捐赠库三态（pending/active/inactive）、加权路由、积分签到
- **三类 RPM 限流**：A（传输完成）/ B（请求成功）/ C（请求接收），滑动窗口 + 自动封禁
- **告警中心**：邮件聚合（连续失败、异常检测），冷却窗口
- **管理员日志增强**：条件筛选、分页、详情展开
- **条件清理**：日志/会话定期自动清理

---

## [v1.0.0-alpha.2] — 2026-07-22

### 新增
- 管理员请求日志查看
- 日志/会话自动清理
- 流内错误帧（SSE error 事件透传）
- OAuth state 绑 cookie（防 CSRF）
- Dify 错误码透传（区分网关错误与上游错误）
- Discord OAuth 失败重定向

### 修复
- 测试漂移修复
- 中文文案全角标点统一

---

## [v1.0.0-alpha.1] — 2026-07-20

### 新增
- OpenAI 兼容 `/v1/chat/completions`（流式 + 非流式）
- `/v1/models` 模型列表端点
- Discord OAuth 认证（guild + role 门槛）
- 用户 API 密钥管理
- Dify App 凭据绑定（AES-GCM 加密）
- Admin 子域隔离
- 内嵌 SPA（Pico.css + 原生 JS）
- Pi Coding Agent 插件
- 隐私政策 / 服务协议页面
