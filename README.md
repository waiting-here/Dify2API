# Dify2API

通过广泛使用的 **OpenAI 兼容 API 接口**，为各类下游客户端（包括但不限于 Pi Coding Agent 插件、SillyTavern 等）提供调用 **Dify App** 的统一端点。内嵌多用户网页管理台：
部署者只需提供一个启动文件；终端用户经 Discord OAuth 注册登录后，自行绑定
自己的 Dify App（地址+密钥）并获得专属调用密钥。网关按用户隔离路由、
限流计费、加密存储凭据。

> 本文面向**部署/维护本项目的开发者**。普通终端用户的操作以网页内指引为准。

> **免责声明与商标声明**：本项目为独立的非官方第三方工具，与 LangGenius, Inc. 无任何隶属、背书或赞助关系。
>
> 本项目名称中的"Dify"系指 LangGenius, Inc. 的产品"Dify"，仅用于说明本项目的功能目的（通过 OpenAI 兼容接口调用 Dify App），构成 nominative fair use（指示性合理使用）。"Dify"是 LangGenius, Inc. 的商标。
>
> "OpenAI"、"Claude"、"Discord"、"SillyTavern"、"Gemini" 等名称均为其各自所有者的商标，此处仅为描述兼容性而提及，不构成商标侵权或暗示背书。

## 架构

```
SillyTavern / Pi ──HTTPS──▶ Nginx ──HTTP──▶ Dify2API ──HTTP──▶ Dify Apps（每用户各自绑定）
浏览器（用户/管理员） ──HTTPS──▶ Nginx ──HTTP──▶ Dify2API（内嵌 SPA，go:embed）
```

- **单二进制**：Go ≥1.25 构建，无外部服务依赖（SQLite 内嵌，前端 go:embed）；
- **每用户路由**：`调用方密钥 → 用户 → 模型全名 → 该用户的 App 配置 → Dify App`；
- **服务契约**：按模型名 `[服务]后端` 前缀选择消息布局（见下表）；
- **双站点**：主站（用户，Discord OAuth）与 `admin.<域名>`（管理员，账号密码）严格隔离。

## 项目结构

```
├── main.go               # 入口（-admin/-debug/-force-https/-v）
├── config/               # 启动配置文件解析（OS env 覆盖）
├── db/                   # SQLite + AES-GCM（用户/会话/配置/密钥/日志）
├── auth/                 # Discord OAuth、会话、密码/登录节流
├── openai/               # OpenAI 类型（含多模态 content parts）
├── translator/           # 服务注册表 + 各服务消息契约
├── dify/                 # Dify 客户端（workflows/run、parameters、files/upload）
├── handler/              # HTTP 路由、中间件（双站点/HTTPS）、限流背压
├── web/static/           # 内嵌 SPA（Pico.css + 原生 JS，i18n 字典）
│                         # 含隐私政策与服务协议模板
├── integrations/         # 客户端集成（pi-dify-subagent）
├── SECURITY.md           # 安全漏洞报告指引
├── CONTRIBUTING.md       # 贡献指南（含 DCO 要求）
├── dify_app/             # 参考 Dify App DSL 导出
├── nginx/                # Nginx 示例（HTTPS 跳转版 / 纯 HTTP 版）
└── admin.env.example     # 启动配置模板（唯一配置来源）
```

## 部署

### 1. 准备

- Go **≥1.25**（`go build -o dify2api .`）；
- Dify App：在 Dify 控制台创建 Workflow 应用并发布，于 **API 集成**页创建 `app-…` 密钥
  （输入变量须匹配服务契约，见下；`dify_app/` 有参考导出）；
- Discord Application（[Developer Portal](https://discord.com/developers/applications)）：
  记录 Client ID/Secret，OAuth2 Redirects 添加 `https://<域名>/auth/discord/callback`；
- 用于注册放行的 Discord 服务器 ID 与身份组 ID（可稍后在管理台设置）。

### 2. 启动配置文件（唯一配置来源）

复制 `admin.env.example` 为 `admin.env` 并填写（设 `0600` 权限；同名 OS 环境变量可覆盖任意项）：

```bash
# 必填
ADMIN_USERNAME=admin
ADMIN_PASSWORD=<明文或 bcrypt 哈希>
DISCORD_CLIENT_ID=...
DISCORD_CLIENT_SECRET=...
SITE_BASE_URL=https://dify2api.example.com

# 可选（全部有默认值）
ADMIN_HOST=admin.dify2api.example.com
SITE_NAME=Dify2API             # 网站对外显示的名称
REPORT_EMAIL=report@example.com  # DMCA/CSAM 举报联络邮箱（展示于服务协议与隐私政策页）
FAVICON_PATH=                  # 浏览器标签页图标文件路径（可选,支持 .ico/.png/.svg/.webp）
LISTEN_ADDR=localhost:10086
DIFY2API_DB_PATH=dify2api.db
DIFY2API_MASTER_KEY_PATH=dify2api.key
DIFY_HTTP_TIMEOUT_MS=900000
MAX_CHAT_IN_FLIGHT=64        # 全局并发聊天上限（超出 429）
MAX_REQUEST_BODY_MB=10       # 请求体上限 MB（超出 413）
SSE_BUFFER_MB=10             # 每流 SSE 初始缓冲 MB
LOGIN_MAX_FAILURES=5         # 登录失败锁定阈值
LOGIN_WINDOW_MIN=10          # 失败计数窗口（分钟）
LOGIN_LOCK_MIN=60            # 锁定时长（分钟）
LOGIN_MIN_LATENCY_MS=300     # 登录恒定时延（毫秒）
```

### 3. 启动与 Nginx

```bash
./dify2api -admin /path/to/admin.env
# -force-https  公网部署：HTTP → 301 HTTPS（需前置 TLS 终止）
# -debug        调试拦截模式（见下）
```

- 双域名（`<域名>` 与 `admin.<域名>`）均需解析与证书，参考 `nginx/` 两份示例；
- SSE 流式必须 `proxy_buffering off`；超时建议 ≥900s；
- 未加 `-force-https` 时启动日志会警告 HTTP 风险（本机部署可忽略）。

### 4. 首次使用

1. 访问 `https://admin.<域名>` 登录管理员 → 设置注册条件（guild/role）；
2. 访问 `https://<域名>` 以 Discord 注册终端用户；
3. 用户在用户台添加 App 配置（选择服务下拉框 + 后端模型名 + App 地址/密钥），
   网关自动校验 App 参数与契约兼容性（信息提示，不阻断）；
4. 复制调用方密钥，按 OpenAI 格式调用 `POST /v1/chat/completions`。

## 服务与消息契约

| 服务 | 消息布局 | Dify App 变量 |
|------|----------|---------------|
| `general` | 恰好 1 条 `user` | `user_0` |
| `custom` | `user` 必填，`system` 可选 | `user_0`、`system_prompt` |
| `website-summary` | `user`→URL（必填）、`system`→要求（可选） | `request_url`、`request_instruction` |
| `image-processing` | `user`→要求（必填）+ 图片（1–10 张，`image_url` data URI 或 http（s） URL）、`system`→提示词（可选） | `user_request`、`system_prompt`、`input_image_list` |
| `sillytavern-main-trimmed` | `system, user` 后接 0–3 组 `assistant, user`（2–8 条） | `system_prompt`、`user_0`、`assistant_1..3`、`user_1..3` |
| `sillytavern-SP·数据库-填表` | `system` 必填 + `user` 可选 + assistant 打头严格交替 `A U A U A U A`（1–8 条） | `system_prompt`、`user_0`（可选）、`assistant_0`、`user_1`、`assistant_1`、`user_2`、`assistant_2`、`user_3`、`assistant_prefill` |

- 未知服务一律拒绝（严格模式）；多模态图片经 data URI 预上传（`/v1/files/upload`）
  或以 `remote_url` 直传；
- 绑定校验：契约**必选**变量须在 App 中存在，App **必选**变量须被契约覆盖，
  App 多余可选变量允许（提示但不阻断）。

## 自定义服务（开发接口）

新增一个受支持的服务只需三步（`translator/contracts.go` 内有 DEV EXTENSION POINT 注释）：

1. **注册表**：`serviceRegistry` 添加 `{Name, Label}`；
2. **契约**：在 `TranslateForService` 与 `ContractVarsFor` 添加对应 case
   （消息布局 → Dify App 变量；同时决定绑定校验的必选/可选集）；
3. **Dify App**：按契约创建对应输入变量的 Workflow 应用。

用户端随即在服务下拉框可见；模型名形如 `[新服务]后端模型`。

## 客户端集成：dify-subagent（Pi 插件）

位于 `integrations/pi-dify-subagent/`（随项目 AGPL 发布）。为 Pi 主 Agent 注册
`dify-subagent` 工具与 `/dify-subagent` 配置命令，把自包含的一次性子任务委托给
Dify2API 背后的模型。

**部署**：将目录复制到 `~/.pi/agent/extensions/dify-subagent/`；在 Pi 中执行
`/dify-subagent setup` 配置网关地址与调用方密钥（自动验证 + 服务模型选择）。

**工具参数**：`task`（按服务必填）、`preset`、`system_prompt`、`url`
（website-summary）、`image_paths`（image-processing）、`timeout_ms`、`result_limit`。

**预设即文件**（`presets/*.md`，每次调用现读，可会话中编辑；新增=新增文件）：

```markdown
---
name: my-preset
description: 用途说明（供主 Agent 参考）
model: "[general]claude-opus-4-6"
service: general
system_prompt_policy: optional   # locked 时拒绝自定义 system_prompt
timeout_ms: 300000
result_limit: 4000               # 超限结果写临时文件，仅回路径+预览
---

（正文即 system_prompt；general/image-processing 可无正文）
```

首发预设：`general`、`custom`、`website-summary`、`image-processing`。

## 运维

- **备份** = `dify2api.db` + `dify2api.key`（密钥文件丢失则已存密钥全部不可恢复）；
- **限流（三类 RPM）**：每用户三个独立滑动窗口（60 秒）——
  A 传输完成（默认 6 次/分，不含失败）、B 请求成功（默认 12）、
  C 请求接收（默认 18，鉴权后计）；门禁在请求开始时检查三窗口，
  任一超限返回 403 `rpm_exceeded`（文案含类别与阈值）；管理台可调
  三个全局上限、每用户三个覆盖值、违规累计阈值（默认 5）与
  自动封禁时长（默认 24h）；三类违规合并计数；
- **IP 限流**：`/api/*` 网页接口按源 IP 限流（默认 120 次/分，超限
  60 秒内 429，不封禁、不影响 `/v1/*`；`WEB_RPM_PER_IP=0` 关闭）；
  `/v1/*` 无效密钥请求按源 IP 节流（默认 30 次/分，防无效密钥
  洪泛；有效密钥不受影响）；
- **封禁 vs 删除**：封禁（定时/永久）保留记录且禁止再注册；删除清空记录并允许再注册；
- **内置加固**：HTTP 服务超时（Slowloris）、4MB+ 可配请求体上限、并发背压（429）、
  管理员登录爆破锁定（IP+用户名滑窗，5 次/10 分钟 → 锁 1h）、密钥 AES-GCM 加密存储；
- **日志**：`request_logs` 仅存元数据（时间/模型/状态/错误码）；服务启动时及
  此后每 24 小时自动清理 30 天以上的日志与过期会话（启动日志含
  `[CLEANUP]` 行）；用户台自查；管理台“请求日志”面板可按用户（关键字
  搜索）/服务/状态/时间范围筛选全部用户的请求记录，支持分页；
- **调试模式**：`-debug` 拦截请求落盘（`request.json` + `dify_inputs.json`）不转发。
- **数据权利**：用户可在网页控制台自助导出全部个人数据（JSON 下载，含解密凭据），
  或自助删除账号（二次确认，清空全部记录）；管理员可从后台为单个用户导出数据。
- **法律页面**：内嵌 `/privacy`（隐私政策）和 `/terms`（服务协议，含 DMCA/NCMEC 条款）。
  部署者通过 `SITE_NAME` / `REPORT_EMAIL` 配置后自动填充。
  **⚠️ 部署前必须审核并修改**：模板中的 DMCA 程序等条款基于美国法律；
  非美国部署者需按当地法律替换相关内容（详见 HTML 注释与 §合规提示）。
- **网站图标**：通过 `FAVICON_PATH` 配置浏览器标签页图标文件路径（支持常见图片格式）。
- **邮件提醒**：配置 SMTP 后，以下三类事件自动发送邮件通知——
  ① 用户因 RPM 违规被自动封禁、② 捐赠条目连续 10 次失败自动转为未激活、
  ③ 管理员登录被爆破锁定。未配置 `SMTP_HOST` 时邮件功能静默关闭（启动日志
  显示 `[MAILER] disabled`）。详见下方 §邮件提醒。

## 邮件提醒

邮件系统通过 `admin.env` 中的 SMTP 配置项启用。每类事件独立聚合：
同一类型事件在 **10 分钟**内只会发送一封邮件，内含该时间段内全部事件的摘要。
配置项如下（均在 `admin.env.example` 中提供）：

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `SMTP_HOST` | （空） | SMTP 服务器地址；为空时邮件功能关闭 |
| `SMTP_PORT` | `587` | SMTP 端口；465 自动切换为隐含 TLS |
| `SMTP_USER` | （空） | 登录用户名（通常与发件邮箱相同） |
| `SMTP_PASS` | （空） | 登录密码或应用专用密码 |
| `SMTP_FROM` | 回退到 `SMTP_USER` | 发件人地址 |
| `SMTP_TO` | （空） | 收件人地址（管理员邮箱） |
| `SMTP_TLS` | （空=自动） | `starttls`（587 等）、`implicit`（465 等）或留空自动检测 |

### 配置示例

**Gmail**（需在 Google 账号中生成"应用专用密码"，不填普通登录密码）：
```
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USER=yourname@gmail.com
SMTP_PASS=您的16位应用专用密码
SMTP_FROM=yourname@gmail.com
SMTP_TO=admin@example.com
SMTP_TLS=starttls
```
> 生成应用专用密码：Google 账号 → 安全性 → 两步验证 → 应用专用密码。

**腾讯企业邮**（使用邮箱登录密码）：
```
SMTP_HOST=smtp.exmail.qq.com
SMTP_PORT=465
SMTP_USER=admin@example.com
SMTP_PASS=邮箱密码
SMTP_FROM=admin@example.com
SMTP_TO=admin@example.com
SMTP_TLS=implicit
```

### 排障

| 错误 | 原因与解决 |
|------|-----------|
| 535 认证失败 | 核对用户名密码；Gmail 须使用应用专用密码而非普通登录密码 |
| 550 发件人地址不匹配 | `SMTP_FROM` 必须与登录邮箱一致或有发送权限 |
| TLS 握手失败 | 检查端口与 `SMTP_TLS` 设置，465 选 `implicit`，587 选 `starttls` |
| 启动日志 `[MAILER] disabled` | `SMTP_HOST` 为空，邮件功能关闭（正常行为） |

## 错误列表

| HTTP | code | 含义 |
|------|------|------|
| 400 | `invalid_message_sequence` | 消息布局不符该服务契约 |
| 400 | `invalid_request` | 请求体/参数非法（含未注册服务名） |
| 401 | `unauthorized` | 调用方密钥缺失/无效，或网页会话失效 |
| 403 | `forbidden` | 管理接口非管理员 |
| 403 | `rpm_exceeded` | 超出三类 RPM 任一上限（文案含类别、阈值与封禁提示） |
| 403 | `insufficient_credits` | 调用公益模型时积分不足（含可配置积分名） |
| 403 | `charity_disabled` | 全局公益开关已被管理员关闭 |
| 403 | `login_locked` | 管理员登录失败过多，锁定中 |
| 403 | `login_failed` | OAuth 登录失败（注册条件/封禁等） |
| 404 | `model_not_found` | 模型未配置或已停用 |
| 404 | `not_found` | 路径不存在或跨站点访问 |
| 404 | `debug_intercept` | 调试拦截（非真实错误） |
| 409 | `conflict` | 模型名已存在 |
| 413 | `request_too_large` | 请求体超限 |
| 415 | `invalid_request` | Content-Type 不是 application/json |
| 429 | `server_busy` | 全局并发已满（附 Retry-After） |
| 429 | `rate_limited` | 源 IP 被限流（Web 接口超频或无效密钥过多，附 Retry-After） |
| 503 | `service_unavailable` | 当前该公益模型无可用捐赠条目 |
| 4xx | （透传上游 code） | Dify 返回 4xx 时原状态码与错误码透传（如 400 `invalid_param`），消息带 `[Dify]` 前缀 |
| 502 | `upstream_error` 等 / `image_upload_failed` | Dify 5xx 或网络错误 / 图片预上传失败 |
| 500 | `internal` | 网关内部错误 |

流式传输中途失败（SSE 已开始后）：流内发送 OpenAI 风格错误帧
`data: {"error":{"code":"upstream_error",...}}` 且**不发** `data: [DONE]`，
与 OpenAI 官方行为一致（SDK 据此抛错）。

## 合规提示与使用红线

本项目按"现状"提供，仅为中立的协议适配工具。部署运营者须：遵守 [Dify 服务条款](https://dify.ai/legal/terms-of-service)与所绑模型供应商使用政策（如 [Anthropic AUP](https://www.anthropic.com/legal/aup)）；面向终端用户提供服务时提供合规隐私政策并遵守 GDPR、《个人信息保护法》等法规。严禁任何涉及未成年人的性内容、真人非自愿内容；在中国法域须注意《治安管理处罚法》第 68 条、《刑法》第 363–367 条、《网络安全法》第 12 条；向公众提供生成式 AI 服务另须遵守《生成式人工智能服务管理暂行办法》。因违规使用产生的一切后果由部署者/使用者自行承担。

## 许可证

本项目以 [GNU Affero General Public License v3.0](LICENSE)（AGPL-3.0）发布。你可以自由使用、修改和再分发，但基于本项目的衍生作品（包括通过网络提供服务的修改版）必须以相同协议开源。商业闭源使用请联系作者另行授权。

## 加密功能与出口声明

本软件包含加密功能（AES-256-GCM，用于凭据加密存储），依赖 Go 标准库 `crypto/aes` 和 `golang.org/x/crypto`。

**美国出口管制 （EAR）**：作为以 AGPL-3.0 协议公开发布的开源软件，本项目的源代码及对应目标代码符合 EAR § 734.3(b)(2) 对"publicly available"的定义，依据 License Exception TSU (15 CFR § 740.13(e))，不受 EAR ECCN 5D002 类别的主要出口管制限制。然而，若以非公开方式提供修改版或将其作为闭源商业产品分发，可能需要单独进行出口合规评估。

本声明不构成法律建议。如有疑问，请咨询合格的出口管制律师。

---

Copyright (C) 2026 Dify2API contributors.
