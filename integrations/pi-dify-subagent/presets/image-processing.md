---
name: image-processing
description: 图片理解。给定本地图片路径或 URL(image_paths)与处理要求(task),返回图片内容分析。视觉由 Dify App 的多模态模型完成;系统提示词缺省使用 App 内置版本,可用 system_prompt 覆盖。
model: "[image-processing]claude-sonnet-4-6"
service: image-processing
system_prompt_policy: optional
timeout_ms: 300000
result_limit: 8000
---
