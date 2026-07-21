# dify-subagent(Pi 插件)

搭配 [Dify2API](../../README.md) 使用的 [Pi Coding Agent](https://github.com/earendil-works/pi-coding-agent) 扩展。
注册一个 `dify-subagent` 工具,让主 Agent 把**自包含、一次性、无需工具**的子任务
(摘要、翻译、起草、代码片段评审、第二意见等)委托给 Dify2API 背后的模型,并回收文本结果。

每次调用恰好发送一个 `system` 消息和一个 `user` 消息(Dify2API 的最小合法布局),
阻塞式返回。无工具、无历史、无自动重试——错误信息原样返回,由主 Agent 自行决策。

## 为什么用它

如果你的主 Agent 按 token 计费、而 Dify 侧按次计费(如 Dify AI Credit),
可以把高 token 消耗的工作(长输入摘要/翻译/评审)卸载到按次计费的一侧。
主 Agent 只需付出:工具 schema + 任务参数 + 精简后的结果。

## 安装

要求:已部署并运行 Dify2API(默认 `http://localhost:10086`)。

将本目录复制(或符号链接)到 Pi 的用户扩展目录:

```bash
# Linux/macOS
mkdir -p ~/.pi/agent/extensions
cp -r integrations/pi-dify-subagent ~/.pi/agent/extensions/dify-subagent

# Windows(PowerShell)
Copy-Item -Recurse integrations\pi-dify-subagent $env:USERPROFILE\.pi\agent\extensions\dify-subagent
```

目录结构(安装后):

```
~/.pi/agent/extensions/dify-subagent/
├── index.ts
├── README.md
└── presets/
    ├── general-preview.md
    └── custom-preview.md
```

重启 Pi 或执行 `/reload` 后,主 Agent 即可调用 `dify-subagent` 工具。

## 配置

### `/dify-subagent` 命令(推荐)

在 Pi 会话中直接配置并**自动验证**:

| 命令 | 说明 |
|------|------|
| `/dify-subagent setup` | 交互式输入网关地址与 API Key → 自动拉取 `/v1/models` 验证 → 展示可用服务与模型;若某服务有多个模型(如 `[general-preview]claude-opus-4-6` 与 `[general-preview]gpt-5.5`),逐个提示选择并保存 |
| `/dify-subagent url <baseUrl>` | 直接设置网关地址(同样自动验证) |
| `/dify-subagent key` | 交互式输入 API Key(经输入框,**不会留在会话记录里**;留空则清除) |
| `/dify-subagent model <服务名>` | 重新拉取模型列表,为指定服务选择发往的模型 |
| `/dify-subagent` 或 `show` | 查看当前配置(Key 脱敏显示) |

配置保存于 `~/.pi/agent/extensions/dify-subagent/config.json`(权限 0600),结构:

```json
{
  "baseUrl": "https://dify2api.example.com/v1",
  "apiKey": "...",
  "serviceModels": { "general-preview": "[general-preview]claude-opus-4-6" }
}
```

地址支持域名(`https://域名/v1`)、IPv4(`http://192.0.2.1:10086/v1`)、IPv6(`http://[2001:db8::1]:10086/v1`,需方括号)。验证失败时会询问是否仍保存。

**优先级**:`config.json` > 环境变量 > 内置默认。

### 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `DIFY2API_BASE_URL` | `http://localhost:10086/v1` | Dify2API 网关地址 |
| `DIFY2API_API_KEY` | (空) | 网关闭合 `API_KEY` 时必填,发送 `Authorization: Bearer` |
| `DIFY2API_TIMEOUT_MS` | `300000` | 兜底超时(预设文件与调用参数均可覆盖) |
| `DIFY2API_MAX_TASK_CHARS` | `100000` | task 长度预检上限(对齐 Dify App 变量上限) |
| `DIFY_SUBAGENT_PRESETS_DIR` | (空) | 额外的预设目录(优先级最高) |

### 模型选择行为

**每次调用都会重新拉取模型列表**(如有变动则更新本地记录),然后按以下顺序决定发往的模型:

1. **用户配置**:该服务已在 `serviceModels` 中指定 → 直接使用;
   - 若**此前选定的模型已从列表消失**:提示用户,并优先随机选用同服务的其他模型继续(结果附注告知主 Agent;配置中的映射不会被自动改写,可用 `/dify-subagent model <服务>` 重新选定);同服务已无其他模型 → 同第 4 条;
2. **预设默认**:预设文件的 `model` 在网关模型列表中 → 使用之;
3. **随机兜底**:服务在列表中有其他模型 → 随机选一个,并在结果中附注提示主 Agent;
4. **无可用模型**:服务前缀不在列表中 → **先提示用户**(无论最终是否发出请求),再返回错误给主 Agent,不发出对话请求。

模型列表获取失败时:若预设定义了 `model` 则按其继续(V0.1.0 网关对该字段仅回声),否则报错。

## 工具参数

| 参数 | 必填 | 说明 |
|------|------|------|
| `task` | 按服务 | 自包含任务内容(general/custom 必填;website-summary 不需要)。按次计费,建议多个子任务打包一次调用 |
| `url` | 按服务 | 目标页面地址(website-summary 必填,需 http(s)://) |
| `image_paths` | 按服务 | 图片路径数组,本地文件路径或 http(s) URL(image-processing 必填,≤10 张,单张 ≤10MB) |
| `preset` | 否 | 预设名,缺省 `general`;传错名字会返回实时可用清单 |
| `system_prompt` | 否 | 自定义系统提示词(website-summary 中作为总结要求);`general` 预设及 `locked` 预设会拒绝 |
| `timeout_ms` | 否 | 单次超时(毫秒),缺省取预设文件值 |
| `result_limit` | 否 | 结果内联上限(字符);超限结果写入临时文件,仅返回路径+预览 |

## 预设

预设是 `presets/*.md` 文件(frontmatter + 正文),**每次调用现读**,可会话中途编辑;
新增预设 = 新增文件,无需改代码。同名优先级:`DIFY_SUBAGENT_PRESETS_DIR` >
项目级 `.pi/extensions/dify-subagent/presets` > 用户级。

```markdown
---
name: my-preset
description: 用途说明(供主 Agent 选择时参考)
model: "[my-preset]claude-opus-4-6"
system_prompt_policy: optional
timeout_ms: 300000
result_limit: 4000
---

(正文即 system_prompt)
```

| 字段 | 缺省 | 说明 |
|------|------|------|
| `name` | 文件名 | 预设名 |
| `description` | (无) | 用途说明 |
| `model` | `name` | 随请求发送的模型名(需与你在网关网页端配置的模型名一致) |
| `service` | model 前缀 | 服务契约:`general`(仅 task)/`custom`(task+可选提示词)/`website-summary`(url+可选要求) |
| `system_prompt_policy` | `optional` | `locked`:提示词固定,传 `system_prompt` 会被预检拒绝;`optional`:可自定义,缺省用正文 |
| `timeout_ms` | `300000` | 默认超时 |
| `result_limit` | `4000` | 默认结果内联上限(字符) |

### 首发预设

| 预设 | service | 说明 |
|------|---------|------|
| `general` | general | 通用单轮问答;无系统提示词(提示词内置于 Dify App),只填 `task` |
| `custom` | custom | 自定义单轮问答;`task` + 可选 `system_prompt`(缺省用文件正文) |
| `website-summary` | website-summary | 网页总结;`url` 必填 + 可选 `system_prompt`(总结要求) |
| `image-processing` | image-processing | 图片理解;`task`(处理要求)+ `image_paths` 必填;系统提示词缺省用 App 内置版本 |

## 行为说明

- **超长结果**:超过有效 `result_limit` 的回答全文写入 `os.tmpdir()/dify-subagent/*.md`
  (权限 0600,不自动清理),工具结果只返回「路径 + 总字符数 + 前 500 字预览」,
  主 Agent 可用 read 工具按需取片段;
- **预检不发请求**:task 超长、locked 预设收到 system_prompt、未知 preset,
  均在发送前报错(不消耗 Dify 额度);
- **超时**:默认 300s(预设可调),网关侧上限见其 `DIFY_HTTP_TIMEOUT_MS`(默认 600s);
- **错误处理**:超时/网络/HTTP 错误/格式异常均原样返回诊断,由主 Agent 决定
  直接重试、修改输入重试或放弃。
