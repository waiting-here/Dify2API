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
SillyTavern / Pi ──HTTPS──▶ Nginx ──HTTP──▶ Dify2API ──HTTP(S)──▶ Dify Apps（每用户各自绑定）
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
├── integrations/         # 客户端集成（pi-dify-subagent）
├── CHANGELOG.md          # 已发布版本与 Unreleased 变更
├── SECURITY.md           # 安全漏洞报告指引
├── CONTRIBUTING.md       # 贡献指南（含 DCO 要求）
├── THIRD_PARTY_NOTICES.md  # 内嵌浏览器依赖许可清单
├── dify_app/             # 参考 Dify App DSL 导出
├── nginx/                # Nginx 示例（HTTPS 跳转版 / 纯 HTTP 版）
└── admin.env.example     # 启动配置模板（唯一配置来源）
```

## 部署

### 最小部署流程

1. 获取源码：`git clone` 本仓库
2. 安装 Go **≥1.25**
3. 复制 `admin.env.example` 为 `admin.env`，填写必填项（Discord OAuth、管理员密码等）
4. 构建：`go build -o dify2api .`
5. 配置反代：参考 `nginx/` 目录下的示例配置（详见下方 §3.启动）
6. 后台常驻：使用 systemd、screen 或 Supervisor 保持服务运行

### 升级现有部署

从 v1.1.0 或更早版本升级至当前代码时，请先阅读以下兼容性提示：

1. **先做一致性备份**：停止服务后备份 `dify2api.db` 与 `dify2api.key`；若不能停机，
   使用 SQLite online backup 或先正确 checkpoint。不要在 WAL 活跃时只热复制主 `.db` 文件。
2. **合并新增配置项**：以最新 `admin.env.example` 为准，重点检查
   `TRUSTED_PROXY_CIDRS`、`DIFY_EGRESS_ALLOWLIST`、`REMOTE_CONTENT_ORIGIN_ALLOWLIST`、
   `DIFY_MAX_RESPONSE_MB`、`DIFY_PROBE_IN_FLIGHT` 和 `MAX_WEB_REQUEST_BODY_KB`。
3. **核对代理链**：loopback nginx 使用默认可信 CIDR 即可；Docker、远端或多级反代必须填入
   实际代理来源 CIDR。不要填写 `0.0.0.0/0` 或 `::/0`。同步采用最新 `nginx/` 示例，
   并确保代理覆盖 XFF，而不是保留客户端提交的 XFF。
4. **核对 Dify 地址**：公网 Dify 无需额外配置；解析到私网、loopback 或 link-local 的既有 App
   会被默认阻断，必须在 `DIFY_EGRESS_ALLOWLIST` 中按最小范围允许精确 origin 或 CIDR。
   Dify 请求不再使用 `HTTP_PROXY`/`HTTPS_PROXY` 环境代理。
5. **核对远程内容功能**：任意 website-summary URL 和远程图片现在默认拒绝。确有需要时，
   只把可信目标的精确 origin 加入 `REMOTE_CONTENT_ORIGIN_ALLOWLIST`；data URI 图片不受影响。
6. **数据库自动升级**：新增表和索引会在启动时幂等创建，无需手工 SQL。首次启动后检查
   `[RECOVERY]`、`[CLEANUP]` 和出站安全配置日志，再执行普通/公益模型冒烟测试。
7. **资源默认值**：未显式配置时，`SSE_BUFFER_MB` 的默认值由 10 MiB 降为 1 MiB；
   已在旧启动文件中明确写为 10 的部署不会自动改变，可按内存预算调整。

当前静态资源策略要求浏览器/CDN 每次重新验证：`index.html` 使用 `no-cache`，其余内嵌静态文件
使用 `max-age=0, must-revalidate`。请勿在 CDN 规则中强制覆盖为长期 immutable 缓存。

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
I18N_FILE=i18n.json           # 多语言字典文件（可选；站点名称通过字典中 site_name 键配置，参考 i18n.json.example）
REPORT_EMAIL=report@example.com  # DMCA/CSAM 举报联络邮箱（展示于服务协议与隐私政策页）
FAVICON_PATH=                  # 浏览器标签页图标文件路径（可选,支持 .ico/.png/.svg/.webp）
LISTEN_ADDR=localhost:10086
TRUSTED_PROXY_CIDRS=127.0.0.0/8,::1/128  # 仅这些 TCP 来源可提供 X-Forwarded-*；无代理填 none
DIFY2API_DB_PATH=dify2api.db
DIFY2API_MASTER_KEY_PATH=dify2api.key
DIFY_HTTP_TIMEOUT_MS=900000
DIFY_MAX_RESPONSE_MB=32       # 解压后 JSON / 累计 SSE 响应上限 MiB
DIFY_PROBE_IN_FLIGHT=8        # App /parameters 探测全局并发上限
DIFY_EGRESS_ALLOWLIST=        # 私有 Dify 精确 origin 或 CIDR；公网地址无需配置
REMOTE_CONTENT_ORIGIN_ALLOWLIST=  # Dify 可代用户抓取的精确可信 origin；默认禁用
MAX_CHAT_IN_FLIGHT=32        # 全局并发聊天上限（超出 429）
MAX_REQUEST_BODY_MB=10       # chat 请求体上限 MiB（超出 413）
MAX_WEB_REQUEST_BODY_KB=256  # 状态变更类 /api/* 请求体上限 KiB（超出 413）
SSE_BUFFER_MB=1              # 每流 SSE 初始缓冲 MiB（单行 10MiB，累计见上）
LOGIN_MAX_FAILURES=5         # 登录失败锁定阈值
LOGIN_WINDOW_MIN=10          # 失败计数窗口（分钟）
LOGIN_LOCK_MIN=60            # 锁定时长（分钟）
LOGIN_MIN_LATENCY_MS=300     # 登录恒定时延（毫秒）
```

### 3. 启动

```bash
./dify2api -admin /path/to/admin.env
# -force-https  公网部署：HTTP → 301 HTTPS（需前置 TLS 终止）
# -debug        调试拦截模式（见下）
```

- 双域名（`<域名>` 与 `admin.<域名>`）均需解析与证书；应用只接受 `SITE_BASE_URL`
  与 `ADMIN_HOST` 对应 Host，未知 Host 返回 421；直接探测 `/health` 时也须发送其中一个合法 Host；
- 公网部署建议置于 nginx 后。网关仅对 `TRUSTED_PROXY_CIDRS` 中的 TCP peer 读取
  `X-Forwarded-*`，并从右向左剥离可信代理；单层 nginx 必须覆盖 XFF 为 `$remote_addr`，
  不得使用会保留客户端伪造值的 `$proxy_add_x_forwarded_for`；
- SSE 流式必须 `proxy_buffering off`；超时建议 ≥900s（与 `DIFY_HTTP_TIMEOUT_MS` 默认值一致）；
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
| `sillytavern-main-trimmed` | `system`（可选）后接 0–10 组 `assistant, user`（1–22 条） | `system_prompt`（可选）、`user_0`、`assistant_1..10`、`user_1..10` |
| `sillytavern-main-200` | `system`（可选）后接 0–200 组 `assistant, user`（1–403 条） | `system_prompt`（可选）、`user_0`、`assistant_1..200`、`user_1..200`、`assistant_prefill` |
| `sillytavern-SP·数据库-填表` | `system` 必填 + `user` 可选 + assistant 打头严格交替 `A U A U A U A`（1–8 条） | `system_prompt`、`user_0`（可选）、`assistant_0`、`user_1`、`assistant_1`、`user_2`、`assistant_2`、`user_3`、`assistant_prefill` |

- 未知服务一律拒绝（严格模式）；多模态图片经 data URI 预上传（`/v1/files/upload`）。
  `website-summary` URL 和图片 `remote_url` 只有 origin 位于部署者配置的
  `REMOTE_CONTENT_ORIGIN_ALLOWLIST` 时才会交给 Dify 抓取；默认列表为空，避免消费者借捐赠者 Dify 访问内网。
  该名单表示部署者同时信任目标 origin 的重定向行为；Dify 侧仍须启用其 SSRF/出站防火墙。
  参考 Website Summary DSL 已设置 10s connect / 30s read / 10s write timeout 且只重试 1 次；
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
  推荐停机备份或使用 SQLite online backup，WAL 活跃时不要只热复制主 `.db` 文件；定期做恢复演练；
- **文件权限**：使用独立低权限 OS 账号和 `umask 077` 运行；配置/数据库/master key 建议 `0600`、
  所在目录建议 `0700`；公网环境不要启用 `-debug`，调试 dump 按敏感数据管理和清理；
- **限流（三类 RPM）**：每用户三个独立滑动窗口（60 秒）——
  A 传输完成（默认 6 次/分，不含失败）、B 请求成功（默认 12）、
  C 请求接收（默认 18，鉴权后计）；门禁在请求开始时检查三窗口，
  任一超限返回 403 `rpm_exceeded`（文案含类别与阈值）；管理台可调
  三个全局上限、每用户三个覆盖值、违规累计阈值（默认 5）与
  自动封禁时长（默认 24h）；三类违规合并计数；
  **公益资源路由**中每条捐赠条目有独立 RPM 上限（默认 10 次/分，60 秒窗口），
  acquire 在内存锁内立即占位；契约错误、未允许的远程 URL 与 debug dry-run 均发生在 acquire 前。
  全部条目超限返回 429 `charity_overloaded`；管理员可为每条捐赠单独调整上限；
- **IP 限流**：`/api/*` 网页接口与 Discord 登录入口按经过可信代理链解析的源 IP 限流（默认 120 次/分，超限
  60 秒内 429，不封禁、不影响 `/v1/*`；`WEB_RPM_PER_IP=0` 关闭）；
  `/v1/*` 无效密钥请求按源 IP 节流（默认 30 次/分，防无效密钥
  洪泛；有效密钥不受影响）；
- **封禁 vs 删除**：封禁（定时/永久）保留记录且禁止再注册；删除清空记录并允许再注册；
- **内置加固**：HTTP 服务超时（Slowloris）、chat 10 MiB 与 Web API 256 KiB 两级请求体上限、
  Dify 解压后响应上限、出站 DNS/IP 钉扎（默认拒绝私网/metadata/重定向/环境代理）、
  并发背压（429）、可信代理/固定 Host 门禁、无状态 HMAC OAuth state、管理员登录爆破锁定
  （IP+用户名滑窗，5 次/10 分钟 → 锁 1h）、密钥 AES-GCM 加密存储；
- **日志**：`request_logs` 仅存元数据（时间/模型/状态/错误码/服务/HTTP状态/详情/扣分）；防滥用触发时
  额外记录 `anti_abuse_info` JSON（触发类型与惩罚内容）；所有普通、公益及 donation 已删除后留下的
  日志都按自身时间逐条执行滚动 30 天留存，绑定告警与日志在同一事务清理。服务启动时及
  此后每 24 小时运行清理并删除过期会话（启动日志含 `[CLEANUP]` 行）；用户台自查；
  管理台“请求日志”面板可按用户（关键字搜索）/服务/状态/时间范围筛选全部用户的请求记录，支持分页，
  并在首次进入该面板时按需加载内嵌 Chart.js 展示每日成功/错误和按服务统计（普通用户端不加载）；
- **调试模式**：`-debug` 拦截请求落盘（`request.json` + `dify_inputs.json`）不转发；
  普通用户可在控制台"调试"标签页自主开启调试（SSE 流式推送至浏览器，
  服务端零磁盘留存，支持演习模式，需确认免责声明）。
- **流式断开保护**：所有 Dify 请求绑定下游 request context；客户端中途断开时立即取消
  上游 HTTP，并以独立 3 秒 context 尝试 Dify Stop API。SSE channel 背压发送同样可取消，
  不会因无人消费而永久泄漏 goroutine。
- **公告栏**：管理员可发布/编辑/删除公告（HTML 正文），用户端以卡片列表展示，
  点击弹出详情；系统公告（签到未启用/捐赠未启用/公益未启用）自动生成。
- **维护模式**：管理员在设置页开启后，用户端显示友好维护页面（503），
  管理员端不受影响。
- **用户自助捐赠**：用户可提交 Dify App 凭据申请加入公益资源池（提交时二次确认 +
  App 连通性+契约校验），管理员审核（可修改字段）后入池；用户可查看自己捐赠的审核状态。
- **公益原子结算**：出站前在一个 SQLite 事务内同时预留捐赠次数并扣除消费者积分，写入
  `reserved → dispatched → committed/released` 持久状态；成功时原子增加 success、捐赠贡献与奖励，
  明确的上游失败原子退款并归还次数。进程重启会释放未 dispatched 项，并保守提交已 dispatched 项，
  防止并发超额、负余额和已消费调用变成免费；结算价格/奖励按预留时快照。
- **捐赠积分回报**：公益调用结算成功时，捐赠者获得 `charity_pricing.reward` 积分配置的积分回报（新建定价时默认按价格的 50% 向上取整，管理员可独立编辑）。
- **按模型定价表**：公益资源消耗从全局统一 `charity_cost` 改为按 `(service, model)` 组合单独定价（`charity_pricing` 表）；每条定价含价格、捐赠奖励、启用开关；未定价组合禁止激活名下捐赠条目；用户端可查看已启用定价。
- **捐赠安全层**：三层保险——① 捐赠条目需管理员手动激活 ② 需配置定价表 ③ 需手动开启定价的 enabled 开关。管理员创建捐赠默认未激活。
- **批量管理**：捐赠审批/公益资源库/价格表/公告管理四处表格支持多选与批量操作（通过/拒绝/激活/停用/删除），采用原子全拒策略——任一项不满足条件即整体拒绝。
- **多语言**：前端支持中文/英文切换，首次登录自动检测浏览器语言；
  语言偏好保存至服务端（跨设备同步）。站点名称、积分名称等可通过
  `i18n.json` 字典文件提供多语言版本。
- **数据权利**：用户可在网页控制台自助导出全部个人数据（JSON 下载，含解密凭据、公益结算记录），
  或自助删除账号（二次确认，清空全部记录）；管理员可从后台为单个用户导出数据。
- **法律页面**：内嵌 `/privacy`（隐私政策）和 `/terms`（服务协议，含 DMCA/NCMEC 条款）。
  服务端按 `?lang=en|zh` 或已登录用户语言偏好选择中英文；未登录且未指定参数时默认中文，
  当前不会在页面请求时直接依据 HTTP `Accept-Language` 自动切换。部署者通过 `I18N_FILE` 字典中的
  `site_name` 与 `REPORT_EMAIL` 配置后自动填充。
  **⚠️ 部署前必须审核并修改**：模板中的 DMCA 程序等条款基于美国法律；
  非美国部署者需按当地法律替换相关内容（详见 HTML 注释与 §合规提示）。
- **网站图标**：通过 `FAVICON_PATH` 配置浏览器标签页图标文件路径（支持常见图片格式）。
- **邮件提醒**：配置 SMTP 后，以下四类事件自动发送邮件通知——
  ① 用户因 RPM 违规被自动封禁、② 捐赠条目连续 10 次失败自动转为未激活、
  ③ 管理员登录被爆破锁定、④ 公益定价缺失（无可用定价条目）。未配置 `SMTP_HOST` 时邮件功能静默关闭（启动日志
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
| 400 | `already_checked_in` | 今日已签到 |
| 400 | `checkin_disabled` | 签到系统已被管理员关闭（积分上限设为 0） |
| 400 | `confirmation_required` | 数据导出/删除操作需在请求体中提供确认字符串（`confirm_delete` 或 `confirm_export`） |
| 400 | `content_too_short` | 请求内容过短（不暴露阈值） |
| 400 | `credits_capped` | 签到积分已达上限 |
| 400 | `debug_not_active` | 用户自助调试模式未开启 |
| 400 | `invalid_message_sequence` | 消息布局不符该服务契约 |
| 400 | `invalid_request` | 请求体/参数非法（含未注册服务名或不安全的 Dify Base URL） |
| 400 | `remote_url_not_allowed` | website-summary/远程图片 origin 未经部署者允许 |
| 400 | `invalid_role` | 消息包含不支持的角色类型（仅限 system/user/assistant） |
| 400 | `too_many_pending` | 待审核捐赠申请已达上限 |
| 401 | `invalid_credentials` | 管理员登录用户名或密码错误 |
| 401 | `unauthorized` | 调用方密钥缺失/无效，或网页会话失效 |
| 403 | `charity_disabled` | 全局公益开关已被管理员关闭 |
| 403 | `donation_disabled` | 捐赠系统已被管理员关闭 |
| 403 | `forbidden` | 管理接口非管理员 |
| 403 | `insufficient_credits` | 调用公益模型时积分不足（含可配置积分名） |
| 403 | `login_locked` | 管理员登录失败过多，锁定中 |
| 403 | `rpm_exceeded` | 超出三类 RPM 任一上限（文案含类别、阈值与封禁提示） |
| 404 | `debug_intercept` | 调试拦截（非真实错误） |
| 404 | `model_not_found` | 模型未配置或已停用 |
| 404 | `not_found` | 路径不存在或跨站点访问 |
| 409 | `conflict` | 模型名已存在 |
| 413 | `request_too_large` | 请求体超限 |
| 415 | `invalid_request` | Content-Type 不是 application/json |
| 429 | `charity_overloaded` | 当前该公益模型所有捐赠条目均已达速率上限，请稍后重试 |
| 429 | `rate_limited` | 源 IP 被限流（Web 接口超频或无效密钥过多，附 Retry-After） |
| 429 | `server_busy` | 全局并发已满（附 Retry-After） |
| 4xx | （透传上游 code） | Dify 返回 4xx 时原状态码与错误码透传（如 400 `invalid_param`），消息带 `[Dify]` 前缀 |
| 500 | `internal` | 网关内部错误 |
| 502 | `upstream_blocked` | 旧配置中的 Dify origin 被当前出站安全策略阻断 |
| 502 | `upstream_error` 等 | Dify 5xx、响应超限或网络错误 |
| 503 | `maintenance` | 站点处于维护模式 |
| 503 | `service_unavailable` | 当前该公益模型无可用捐赠条目，或定价缺失/停用 |
| 504 | `upstream_timeout` | 上游 Dify 响应超时（可能因 Cloudflare 100 秒限制被截断），消息带 `[Dify2API]` 前缀，建议使用流式传输（`stream: true`） |

流式传输中途失败（SSE 已开始后）：流内发送 OpenAI 风格错误帧
`data: {"error":{"code":"upstream_error",...}}` 且**不发** `data: [DONE]`，
与 OpenAI 官方行为一致（SDK 据此抛错）。

## 合规提示与使用红线

本项目按"现状"提供，仅为中立的协议适配工具。部署运营者须：遵守 [Dify 服务条款](https://dify.ai/legal/terms-of-service)与所绑模型供应商使用政策（如 [Anthropic AUP](https://www.anthropic.com/legal/aup)）；面向终端用户提供服务时提供合规隐私政策并遵守 GDPR、《个人信息保护法》等法规。严禁任何涉及未成年人的性内容、真人非自愿内容；在中国法域须注意《治安管理处罚法》第 68 条、《刑法》第 363–367 条、《网络安全法》第 12 条；向公众提供生成式 AI 服务另须遵守《生成式人工智能服务管理暂行办法》。因违规使用产生的一切后果由部署者/使用者自行承担。

## 许可证

本项目以 [GNU Affero General Public License v3.0](LICENSE)（AGPL-3.0）发布。你可以自由使用、修改和再分发，但基于本项目的衍生作品（包括通过网络提供服务的修改版）必须以相同协议开源。商业闭源使用请联系作者另行授权。内嵌浏览器依赖的版权与许可证见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。

## 加密功能与出口声明

本软件包含加密功能（AES-256-GCM，用于凭据加密存储），依赖 Go 标准库 `crypto/aes` 和 `golang.org/x/crypto`。

**美国出口管制 （EAR）**：作为以 AGPL-3.0 协议公开发布的开源软件，本项目的源代码及对应目标代码符合 EAR § 734.3(b)(2) 对"publicly available"的定义，依据 License Exception TSU (15 CFR § 740.13(e))，不受 EAR ECCN 5D002 类别的主要出口管制限制。然而，若以非公开方式提供修改版或将其作为闭源商业产品分发，可能需要单独进行出口合规评估。

本声明不构成法律建议。如有疑问，请咨询合格的出口管制律师。

---

Copyright (C) 2026 Dify2API contributors.
