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
首次启动会原子创建 `DIFY2API_MASTER_KEY_PATH`；Linux 上已有密钥若不是仅属主可读写
（0600）、或内容不是严格的 32 字节 base64 key，启动会明确失败且绝不会覆盖原文件。
以下项目最影响安全边界：

| 配置 | 默认值 | 用途 |
|---|---|---|
| `ADMIN_HOST` | `admin.<主域名>` | 管理站 Host |
| `TRUSTED_PROXY_CIDRS` | loopback | 哪些 TCP peer 可提供转发头；无代理填 `none` |
| `DIFY_EGRESS_ALLOWLIST` | 空 | 显式允许私有 Dify 的精确 origin（拒绝 CIDR/裸 IP）；留空仅阻断非公网目标，公网域名（含 CDN 代理）无需条目 |
| `REMOTE_CONTENT_ORIGIN_ALLOWLIST` | 空 | 允许 Dify 抓取的 website/image 精确 origin；留空则两个功能禁用（data URI 图片不受影响） |
| `DIFY_MAX_RESPONSE_MB` | `32` | 解压后 JSON / 累计 SSE 上限 |
| `CHARITY_SETTLEMENT_ATTEMPT_TIMEOUT_SEC` | `5` | 公益结算同步尝试时限；失败后转入在线恢复 |
| `CHARITY_SETTLEMENT_RETRY_DELAY_MS` | `100` | SQLite BUSY/LOCKED 同步重试退避；不改变总尝试时限 |
| `CHARITY_SETTLEMENT_RESERVED_STALE_SEC` | `300` | 周期扫描可释放未派发 reservation 的陈旧阈值 |
| `CHARITY_SETTLEMENT_DISPATCH_GRACE_SEC` | `60` | 已派发 reservation 陈旧阈值在 Dify 超时上的额外宽限 |
| `CHARITY_SETTLEMENT_SCAN_INTERVAL_SEC` | `60` | 公益结算在线重试与陈旧状态扫描周期 |
| `CHARITY_SETTLEMENT_QUEUE_SIZE` | `256` | 公益结算内存唤醒队列/去重集合上限；满载由 DB 扫描兜底 |
| `REQUEST_LOG_CLEANUP_INTERVAL_SEC` | `60` | 过期请求日志物理清理周期；API 可见范围始终严格滚动 30 天 |
| `MAX_CHAT_IN_FLIGHT` | `32` | 全局聊天并发上限 |
| `MAX_REQUEST_BODY_MB` | `10` | chat 请求体上限 |
| `MAX_WEB_REQUEST_BODY_KB` | `256` | 状态变更 Web API 请求体上限 |
| `SSE_BUFFER_MB` | `1` | 每条 SSE 的初始缓冲，按需增长（单行上限 10 MiB） |
| `SHUTDOWN_TIMEOUT_SEC` | `30` | 退出时等待在途请求和后台任务的总时限，超时后强制关闭 |

### 2. 反向代理

参考 `nginx/` 中的示例。必须注意：

- 应用仅接受 `SITE_BASE_URL` 和 `ADMIN_HOST` 对应 Host；其他 Host 返回 421，
  `/health` 探测也应发送合法 Host。
- 仅信任 `TRUSTED_PROXY_CIDRS` 中 peer 的 `X-Forwarded-*`；单层 nginx 应覆盖
  XFF 为 `$remote_addr`，不要保留客户端伪造的 XFF。
- SSE 路径关闭代理缓冲，读取超时应不低于 `DIFY_HTTP_TIMEOUT_MS`。
- 静态资源必须遵守 origin revalidation：不要在 CDN 强制长期 immutable 缓存。

**Cloudflare 代理（橙色云）部署**：源站收到的连接来自 Cloudflare 边缘 IP。
推荐保持 `TRUSTED_PROXY_CIDRS` 为 loopback 默认值，在 nginx 中启用 `real_ip` 模块：
用 Cloudflare 官方网段（https://www.cloudflare.com/ips-v4 与 ips-v6，建议定期更新）
配置 `set_real_ip_from`，`real_ip_header CF-Connecting-IP;`，此后 `$remote_addr`
即真实客户端，继续用 `X-Forwarded-For $remote_addr` 覆盖转发。不要把客户端可伪造的
XFF/CF-Connecting-IP 直接透传给网关。另注意 Cloudflare 对非流式响应存在约 100 秒
超时（免费套餐），长任务建议客户端使用流式传输（`stream: true`）。

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
| `sillytavern-main-trimmed`（弃用，v2 移除） | 可选 system + 最多 10 组 assistant/user | `system_prompt`、`user_0`、`assistant_1..10`、`user_1..10` |
| `sillytavern-main-200`（弃用，v2 移除） | 可选 system + 最多 200 组 assistant/user | `system_prompt`、`user_0`、`assistant_1..200`、`user_1..200`、`assistant_prefill` |
| `sillytavern-main-v1` | 同 `sillytavern-main-200`（可选 system + 最多 200 组 assistant/user） | 同 200 契约变量；变量经服务端混淆映射重命名并含不定量 dummy（下载说明见下） |
| `sillytavern-SP·数据库-填表` | system 必填，之后严格交替 | `system_prompt`、`user_0..3`、`assistant_0..2`、`assistant_prefill` |

**`sillytavern-main-v1` 模板下载与弃用说明**：该服务的 Dify App 模板由服务端按每用户随机种子
生成混淆版本（变量 key / prompt 引用 / 描述备注 / app 名 / 节点标题 / 节点 ID 同步替换，并随机
添加不定量仅混淆用 dummy 变量），生成后自校验 YAML 可解析与导入结构合法再提供下载。在
「我的配置」对应服务卡片点「获取我的 App」、选择模型后下载 DSL 文件，自行导入 Dify 即可；每次
重新下载会生成新种子并使上一次下载的 Dify App 立即失效。管理员可在「模型配置管理」tab 维护
model/provider、依赖版本@hash、参数 JSON、启用状态与排序；非手动依赖 pin 每日 UTC 03:00 从
Dify marketplace manifest 同步，失败会进入告警中心并保留旧值。`sillytavern-main-200` 与
`sillytavern-main-trimmed` 已标记弃用，在 v1.x.x 期间继续支持，计划于 v2 移除，建议新用户
直接使用 `sillytavern-main-v1`。

契约必选变量必须存在于 App；App 的必选变量也必须被契约覆盖。data URI 图片由网关
先上传至 Dify；远程 URL 仅在精确 origin allowlist 中时放行。

新增服务时，以 `translator` 服务注册表为唯一事实来源：注册服务、实现
`TranslateForService`/`ContractVarsFor`、再创建匹配的 Dify Workflow。

## 运维与安全

- **备份与权限**：数据库和 master key 缺一不可。建议独立低权限账号、`umask 077`、
  目录 `0700`、配置/数据库/key `0600`，并定期做恢复演练。
- **出站防护**：Dify 默认只能解析到公网地址；阻断 loopback、私网、link-local、metadata、
  DNS rebinding、跨 origin 重定向和环境代理。解析到公网地址的 Dify（含 Cloudflare 等
  CDN 代理的域名）无需 allowlist 条目；私网目标只能由部署者以精确 origin 显式允许；
  网关自身的 `SITE_BASE_URL`/`ADMIN_HOST` 与 loopback 监听端口即使在 allowlist 中也永久拒绝，
  且阻断无文本/时序侧信道。
- **请求生命周期**：下游断开会取消上游请求，并用独立短 context 尝试 Dify Stop；
  请求体、解压后响应、SSE 累计字节、探测并发和总聊天并发均有硬上限。
- **公益结算**：出站前在单个 SQLite 事务中预留捐赠次数并扣消费者积分；成功幂等结算
  捐赠者贡献/奖励，明确前置失败退款，进程重启恢复未完成 reservation。
- **限流**：用户三类 RPM、公益条目 RPM、Web IP 和无效调用密钥分别限流；具体阈值可在
  管理台或启动文件调整。
- **日志留存**：request log 仅存元数据；普通、公益、活跃 donation 和 orphan 日志都按
  自身时间严格滚动保留 30 天，读取、统计和导出也按请求时刻限窗，绑定告警同事务清理。
- **活跃度聚合**：第一方 SQLite 按 UTC 日记录用户的 API 尝试/成功次数、控制台业务写操作、
  签到次数和小游戏对局数（game_rounds，v1.4.0 起计入产品活跃口径），用于管理员站汇总统计；
  用户级日聚合最多保留 400 天，仅管理员可查看全站汇总，
  随个人导出提供并在删号时删除。已结束 UTC 日另存 k=5 抑制的不可逆全站匿名快照（不含用户
  标识或可筛选维度），最多保留 400 天，删号后可继续保留且不能回连个人。
- **数据权利**：用户可导出完整个人数据（包括解密后的自有凭据、公益结算记录和用户级活跃度
  日聚合）或删除账号；不可逆全站匿名统计快照不属于可回连个人的数据，可能在删号后继续保留。
  导出文件响应使用 `no-store`，应视为高敏感文件。
- **调试**：管理员 `-debug` 会把原始请求落盘，禁止在公网常开；用户自助 debug 使用内存 SSE，
  捕获内容按 64/256 KiB 截断且不含认证类 Header，「发送到 Dify」开关直接表示是否真实转发
  （默认演习模式），断开后有清理宽限期。
- **法律页面**：`/privacy`、`/terms` 支持 `?lang=en|zh` 或用户语言偏好；未登录默认中文，
  不直接按页面请求的 `Accept-Language` 切换。新增活跃度数据类型和 400 天期限属于实质性政策
  更新；生产启用采集前应先发布政策更新和公告并至少等待 7 天。部署前必须按当地法律审核模板
  并配置真实举报邮箱。

## 用户分级与协管（R-A）

普通用户按 1–5 级提供差异化能力；等级在读取时惰性计算，门槛或公益积分（`donation_credit`）
的调整即时生效。

- **自动等级**：`donation_credit ≥ level_threshold_2/3/4`（默认 1/100/500）分别达到
  2/3/4 级，低于 t2 为 1 级；**5 级只能手动设定**（自动判定永不返回 5）。
- **手动设定**：管理员在「用户管理」tab 为单个用户设定 1–5 级或恢复自动（`default`）；
  被手动设定的用户不再自动升降级。
- **特权**（高等级包含所有低等级特权）：2 级用户站顶部横幅（纯文本，不可关闭）；3 级签到
  无视 `credits_cap` 上限（`credits_cap=0` 的全局关闭仍优先）；4 级用户站公益审批；5 级
  公益资源/定价管理 + 全站请求日志（列表与统计，**无导出**，`error_detail` 按用户视角脱敏）。
- **等级设置**（管理台「用户分级」tab，共 9 个键，专用原子接口）：
  `level_threshold_2..4`（校验 `0 ≤ t2 < t3 < t4`）、`level_name_1..5`（留空回退数字等级，
  名称 ≤ 20 字）、`level_banner_text`（2 级及以上顶部横幅，≤ 200 字，拒绝控制字符）；
  PUT 全 9 字段必填，单事务写入。
- **新端点**（仅用户站可达，管理员站 404；`/api/admin/*` 仍仅管理员）：

  | 等级 | 端点 | 说明 |
  |---|---|---|
  | 4 | `GET /api/me/review/pending` | 待审核捐赠申请列表 |
  | 4 | `POST /api/me/review/{id}/approve` / `reject` | 单条审批/驳回 |
  | 4 | `POST /api/me/review/approve|reject/batch` | 批量审批/驳回 |
  | 5 | `GET\|POST /api/me/charity-admin/donations`、`PATCH …/{id}`、`POST …/{id}/status`、`DELETE …/{id}`、`POST …/status|delete/batch` | 公益资源 CRUD/状态/批量 |
  | 5 | `GET\|PUT\|PATCH\|DELETE /api/me/charity-admin/pricing`、`POST …/delete/batch` | 定价管理 |
  | 5 | `GET /api/me/all-logs`、`GET /api/me/all-logs/stats` | 全站日志列表 + 统计（同 R-C 小时聚合契约） |

- **升级说明**：无手工迁移 —— `users.level` 列（NULL=自动，1–5=手动）在启动时幂等创建，
  旧数据默认按自动模式计算等级。

## 小游戏（池塘垂钓）

用户站「积分签到」与「公益与捐赠」之间新增「小游戏」标签页，v1.4.0 首期上线「池塘垂钓」并预留
多游戏扩展接口（按 `game_id` 维度建模）。

- **玩法**：门票制三档鱼饵（蚯蚓 5 / 商品饵 10 / 拟饵 15 积分，全部负期望；回收率 90%，拟饵 88%），
  下注后服务端签发随机种子决定渔获（品种 × 尺寸，倍率 0x–40x），客户端纯演出动画；极小概率钓上
  宝物（漂流瓶 / 幸运四叶草 / 神秘贝壳，固定 2x/3x/5x 倍率），输局触发「永不空军」彩蛋（0 回收）。
  共 23 鱼种 + 8 彩蛋 + 3 宝物。
- **积分与防刷**：游戏只动用普通积分，输赢直接增减，与签到积分上限 `credits_cap` 无额外交互；
  积分 0 封底，输到 0 需签到回血。防刷为限流 + 幂等 + 随机种子校验 + 0 封底，不设每日局数/亏损上限。
- **排行榜**：单鱼榜（单条鱼尺寸，长期累计）+ 总渔获榜（近 30 天滚动窗口），各 Top 20 并展示
  我的排名；用户名默认展示，可切换匿名上榜。
- **管理端**：管理员站「小游戏管理」标签页提供总开关、单游戏开关、全部经济参数（饵价/回收率/
  宝物倍率）可调与一键恢复默认；v1.4.0 暂不引入专用管理员操作审计。

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
| 405 | `method_not_allowed` | HTTP 方法不受该接口支持 |
| 409 | `conflict` | 模型名等唯一约束冲突 |
| 413/415 | `request_too_large`, `invalid_request` | 请求体过大或 Content-Type 错误 |
| 429 | `charity_overloaded`, `rate_limited`, `server_busy` | 公益、IP 或并发限流；可能附 `Retry-After` |
| 4xx | 上游 code | Dify 4xx 原状态码透传，消息带 `[Dify]` |
| 500 | `internal` | 网关内部错误 |
| 501 | `not_implemented` | 接口存在但未实现（如 `by_service=1` 服务统计） |
| 502 | `upstream_blocked`, `upstream_error` | 出站策略、Dify 5xx、响应超限或网络错误 |
| 503 | `maintenance`, `service_unavailable` | 维护或无可用公益资源 |
| 504 | `upstream_timeout` | Dify 超时，建议使用流式传输 |

SSE 已开始后发生错误时，会发送 OpenAI 风格错误帧且不再发送 `[DONE]`。

## 客户端集成与源码结构

- Pi 插件位于 `integrations/pi-dify-subagent/`；安装、参数和 preset 格式见该目录 README。
- 主要包：`config`、`db`、`auth`、`translator`、`dify`、`handler`、`openai`。
- 内嵌前端位于 `web/static/`；v1.4.0 起参考 Dify Workflow 模板不再随仓库分发（`dify_app/` 已移出版本库），`sillytavern-main-v1` 等可下载服务的 Dify App 由服务端生成混淆模板。
- 安全报告见 [SECURITY.md](SECURITY.md)，贡献规则见 [CONTRIBUTING.md](CONTRIBUTING.md)。

## 合规与许可证

部署者须遵守 Dify、模型供应商和所在地法律法规，并提供适用的隐私政策及内容治理机制。
内嵌法律页面是通用模板，不构成法律意见；非美国部署者尤其应审查其中 DMCA 等条款。
严禁涉及未成年人的性内容及真人非自愿内容。

本项目按 [AGPL-3.0](LICENSE) 发布。通过网络提供修改版服务时须履行对应源码义务：
服务协议页会展示 `SOURCE_URL` 指向的源码仓库链接（默认上游仓库），部署修改版时必须改为自己公开发布的地址。
第三方浏览器依赖许可见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。

本软件包含 AES-256-GCM 等加密功能。公开源码/目标代码的出口适用性可能因法域和分发方式而异，
本说明不构成法律建议；有疑问请咨询专业人士。

---

Copyright (C) 2026 Dify2API contributors.
