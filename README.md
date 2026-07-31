# Dify2API

Dify2API 将每位用户自己的 **Dify Workflow App** 暴露为统一的
**OpenAI 兼容 API**，并内嵌用户控制台与管理员控制台。终端用户通过 Discord OAuth
注册、绑定 Dify 地址与密钥，再使用个人 `d2a_` 调用密钥访问模型。

- 单二进制：Go ≥1.25、SQLite、`go:embed` 原生 SPA
- 多用户隔离：`调用密钥 → 用户 → 模型全名 → 用户 App → Dify`
- 双站点：用户站 + 独立管理员 Host
- 凭据保护：AES-256-GCM 加密存储，调用密钥以 SHA-256 查询
- 公益资源：按模型定价、捐赠路由、积分与事务化结算

> 本项目是独立的非官方第三方工具，与 LangGenius, Inc. 无隶属、背书或赞助关系。
> Dify、OpenAI、Claude、Discord、SillyTavern、Gemini 等名称归各自权利人所有，
> 此处仅用于描述兼容性。

## 架构

```text
客户端 ──HTTPS──▶ Nginx ──HTTP──▶ Dify2API ──HTTP(S)──▶ 用户绑定的 Dify Apps
浏览器 ──HTTPS──▶ Nginx ──HTTP──▶ 内嵌用户/管理员 SPA
```

网关根据模型名中的 `[服务]后端` 前缀选择严格消息契约。未知服务或不符合契约的
消息会在出站前拒绝。

## 部署

### 1. 准备与构建

1. 安装 Go ≥1.25。
2. 复制 `admin.env.example` 为私有 `admin.env`，权限设为 `0600`。
3. 配置 Discord Application 回调：`https://<主域名>/auth/discord/callback`。
4. 构建并启动：

```bash
go build -o dify2api .
./dify2api -admin /path/to/admin.env -force-https
```

必填配置：

```dotenv
ADMIN_USERNAME=admin
ADMIN_PASSWORD=<明文或 bcrypt 哈希>
DISCORD_CLIENT_ID=...
DISCORD_CLIENT_SECRET=...
SITE_BASE_URL=https://dify2api.example.com
```

完整配置、默认值与注释以 [`admin.env.example`](admin.env.example) 为唯一模板。
以下项目最影响安全边界：

| 配置 | 默认值 | 用途 |
|---|---|---|
| `ADMIN_HOST` | `admin.<主域名>` | 管理站 Host |
| `TRUSTED_PROXY_CIDRS` | loopback | 哪些 TCP peer 可提供转发头；无代理填 `none` |
| `DIFY_EGRESS_ALLOWLIST` | 空 | 显式允许私有 Dify 的精确 origin（拒绝 CIDR/裸 IP） |
| `REMOTE_CONTENT_ORIGIN_ALLOWLIST` | 空 | 允许 Dify 抓取的 website/image 精确 origin |
| `DIFY_MAX_RESPONSE_MB` | `32` | 解压后 JSON / 累计 SSE 上限 |
| `MAX_CHAT_IN_FLIGHT` | `32` | 全局聊天并发上限 |
| `MAX_REQUEST_BODY_MB` | `10` | chat 请求体上限 |
| `MAX_WEB_REQUEST_BODY_KB` | `256` | 状态变更 Web API 请求体上限 |
| `SSE_BUFFER_MB` | `1` | 每条 SSE 的初始缓冲 |

### 2. 反向代理

参考 `nginx/` 中的示例。必须注意：

- 应用仅接受 `SITE_BASE_URL` 和 `ADMIN_HOST` 对应 Host；其他 Host 返回 421，
  `/health` 探测也应发送合法 Host。
- 仅信任 `TRUSTED_PROXY_CIDRS` 中 peer 的 `X-Forwarded-*`；单层 nginx 应覆盖
  XFF 为 `$remote_addr`，不要保留客户端伪造的 XFF。
- SSE 路径关闭代理缓冲，读取超时应不低于 `DIFY_HTTP_TIMEOUT_MS`。
- 静态资源必须遵守 origin revalidation：不要在 CDN 强制长期 immutable 缓存。

### 3. 首次使用

1. 登录管理员站，设置 Discord guild/role 注册条件及业务开关。
2. 用户从主站完成 Discord OAuth。
3. 用户添加 Dify App、选择服务与后端模型名，通过参数/连通性校验。
4. 用户创建调用密钥，通过 `GET /v1/models` 与 `POST /v1/chat/completions` 调用。

```bash
curl https://dify2api.example.com/v1/models \
  -H 'Authorization: Bearer d2a_xxx'
```

### 升级现有部署

从 v1.1.0 或更早版本升级到 v1.2.0 前：

1. 停机备份 `dify2api.db` 与 `dify2api.key`；不停机时使用 SQLite online backup，
   不要在 WAL 活跃时只复制主 `.db`。
2. 与最新 `admin.env.example` 合并新增配置，并采用最新 nginx 示例。
3. Docker、远端或多级反代必须填写实际代理来源 CIDR，禁止使用 `0.0.0.0/0` 或 `::/0`。
4. 私网 Dify 默认被阻断；`DIFY_EGRESS_ALLOWLIST` 只接受精确
   `http(s)://host[:port]` origin（拒绝 CIDR 与裸 IP），按最小范围配置。
5. 任意 website-summary URL 与远程图片默认被阻断；按需配置精确的
   `REMOTE_CONTENT_ORIGIN_ALLOWLIST`。data URI 图片不受影响。
6. 新表和索引会在启动时幂等创建，无需手工 SQL；启动后检查 `[RECOVERY]`、
   `[CLEANUP]` 日志并做普通/公益调用冒烟。

完整版本变更与兼容性说明见 [CHANGELOG.md](CHANGELOG.md)。

#### 降级说明（不建议）

v1.2.0 的公益记账在**预留时**即扣减捐赠 `remaining_count` 与消费者积分（v1.1.0 是成功
时扣减）。若存在未结算的 reservation（`reserved`/`dispatched` 状态）时降级回 v1.1.0，
旧二进制无法识别这些记录（启动恢复只在 v1.2.0 中执行），可能造成账务不一致。
确需降级时，请先用 v1.2.0 启动一次完成恢复（所有 reservation 变为 `committed`/`released`）
再切换；新表/新索引对 v1.1.0 无影响。

## 服务与消息契约

| 服务 | 消息布局 | Dify App 变量 |
|---|---|---|
| `general` | 恰好 1 条 `user` | `user_0` |
| `custom` | `user` 必填，`system` 可选 | `user_0`、`system_prompt` |
| `website-summary` | `user` URL 必填，`system` 要求可选 | `request_url`、`request_instruction` |
| `image-processing` | `user` + 1–10 张图片，`system` 可选 | `user_request`、`system_prompt`、`input_image_list` |
| `sillytavern-main-trimmed` | 可选 system + 最多 10 组 assistant/user | `system_prompt`、`user_0`、`assistant_1..10`、`user_1..10` |
| `sillytavern-main-200` | 可选 system + 最多 200 组 assistant/user | `system_prompt`、`user_0`、`assistant_1..200`、`user_1..200`、`assistant_prefill` |
| `sillytavern-SP·数据库-填表` | system 必填，之后严格交替 | `system_prompt`、`user_0..3`、`assistant_0..2`、`assistant_prefill` |

契约必选变量必须存在于 App；App 的必选变量也必须被契约覆盖。data URI 图片由网关
先上传至 Dify；远程 URL 仅在精确 origin allowlist 中时放行。

新增服务时，以 `translator` 服务注册表为唯一事实来源：注册服务、实现
`TranslateForService`/`ContractVarsFor`、再创建匹配的 Dify Workflow。

## 运维与安全

- **备份与权限**：数据库和 master key 缺一不可。建议独立低权限账号、`umask 077`、
  目录 `0700`、配置/数据库/key `0600`，并定期做恢复演练。
- **出站防护**：Dify 默认只能解析到公网地址；阻断 loopback、私网、link-local、metadata、
  DNS rebinding、跨 origin 重定向和环境代理。私网访问只能由部署者以精确 origin 显式允许；
  网关自身的 `SITE_BASE_URL`/`ADMIN_HOST` 与 loopback 监听端口即使在 allowlist 中也永久拒绝，
  且阻断无文本/时序侧信道。
- **请求生命周期**：下游断开会取消上游请求，并用独立短 context 尝试 Dify Stop；
  请求体、解压后响应、SSE 累计字节、探测并发和总聊天并发均有硬上限。
- **公益结算**：出站前在单个 SQLite 事务中预留捐赠次数并扣消费者积分；成功幂等结算
  捐赠者贡献/奖励，明确前置失败退款，进程重启恢复未完成 reservation。
- **限流**：用户三类 RPM、公益条目 RPM、Web IP 和无效调用密钥分别限流；具体阈值可在
  管理台或启动文件调整。
- **日志留存**：request log 仅存元数据；普通、公益、活跃 donation 和 orphan 日志都按
  自身时间滚动保留 30 天，绑定告警同事务清理。
- **数据权利**：用户可导出完整个人数据（包括解密后的自有凭据和公益结算记录）或删除账号；
  导出文件响应使用 `no-store`，应视为高敏感文件。
- **调试**：管理员 `-debug` 会把原始请求落盘，禁止在公网常开；用户自助 debug 使用内存 SSE，
  捕获内容按 64/256 KiB 截断且不含认证类 Header，「发送到 Dify」开关直接表示是否真实转发
  （默认演习模式），断开后有清理宽限期。
- **法律页面**：`/privacy`、`/terms` 支持 `?lang=en|zh` 或用户语言偏好；未登录默认中文，
  不直接按页面请求的 `Accept-Language` 切换。部署前必须审核模板并配置真实举报邮箱。

## SMTP 提醒

配置 `SMTP_HOST` 后启用；为空时静默关闭。支持 587 STARTTLS 与 465 implicit TLS。
核心变量为 `SMTP_HOST`、`SMTP_PORT`、`SMTP_USER`、`SMTP_PASS`、`SMTP_FROM`、
`SMTP_TO`、`SMTP_TLS`，详见 `admin.env.example`。

通知包括：自动封禁、捐赠连续失败停用、管理员登录锁定、公益定价缺失和用户 Debug 滥用；同类事件按
10 分钟窗口聚合。生产部署应使用应用专用密码并验证发件人与 TLS 模式。

## API 错误

统一格式：

```json
{"error":{"code":"invalid_request","type":"invalid_request","message":"[Dify2API] ..."}}
```

| HTTP | code | 说明 |
|---|---|---|
| 400 | `invalid_request`, `invalid_message_sequence`, `invalid_role`, `remote_url_not_allowed` | 请求、契约、角色或远程 URL 不合法 |
| 400 | `already_checked_in`, `checkin_disabled`, `credits_capped`, `content_too_short` | 签到、防滥用或积分门禁 |
| 400 | `debug_not_active`, `too_many_pending`, `confirmation_required` | Debug/申请状态；删除账号需 `?confirm=DELETE` |
| 401 | `unauthorized`, `invalid_credentials` | 调用密钥、会话或管理员凭据无效 |
| 403 | `forbidden`, `charity_disabled`, `donation_disabled`, `insufficient_credits`, `login_locked`, `rpm_exceeded` | 权限或业务门禁 |
| 404 | `not_found`, `model_not_found`, `debug_intercept` | 路径/模型不存在或 debug 拦截 |
| 409 | `conflict` | 模型名等唯一约束冲突 |
| 413/415 | `request_too_large`, `invalid_request` | 请求体过大或 Content-Type 错误 |
| 429 | `charity_overloaded`, `rate_limited`, `server_busy` | 公益、IP 或并发限流；可能附 `Retry-After` |
| 4xx | 上游 code | Dify 4xx 原状态码透传，消息带 `[Dify]` |
| 500 | `internal` | 网关内部错误 |
| 502 | `upstream_blocked`, `upstream_error` | 出站策略、Dify 5xx、响应超限或网络错误 |
| 503 | `maintenance`, `service_unavailable` | 维护或无可用公益资源 |
| 504 | `upstream_timeout` | Dify 超时，建议使用流式传输 |

SSE 已开始后发生错误时，会发送 OpenAI 风格错误帧且不再发送 `[DONE]`。

## 客户端集成与源码结构

- Pi 插件位于 `integrations/pi-dify-subagent/`；安装、参数和 preset 格式见该目录 README。
- 主要包：`config`、`db`、`auth`、`translator`、`dify`、`handler`、`openai`。
- 内嵌前端位于 `web/static/`；参考 Dify Workflow 位于 `dify_app/`。
- 安全报告见 [SECURITY.md](SECURITY.md)，贡献规则见 [CONTRIBUTING.md](CONTRIBUTING.md)。

## 合规与许可证

部署者须遵守 Dify、模型供应商和所在地法律法规，并提供适用的隐私政策及内容治理机制。
内嵌法律页面是通用模板，不构成法律意见；非美国部署者尤其应审查其中 DMCA 等条款。
严禁涉及未成年人的性内容及真人非自愿内容。

本项目按 [AGPL-3.0](LICENSE) 发布。通过网络提供修改版服务时须履行对应源码义务；
第三方浏览器依赖许可见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。

本软件包含 AES-256-GCM 等加密功能。公开源码/目标代码的出口适用性可能因法域和分发方式而异，
本说明不构成法律建议；有疑问请咨询专业人士。

---

Copyright (C) 2026 Dify2API contributors.
