---
name: website-summary
description: 网页总结。给定页面 URL(url 参数,必填)与可选总结要求(system_prompt),返回页面内容总结。
model: "[website-summary]claude-opus-4-6"
service: website-summary
system_prompt_policy: optional
timeout_ms: 300000
result_limit: 8000
---

请用要点式中文总结该页面的核心内容。
