# Changelog

Dify2API 的所有重要变更均记录在此文件中。

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
