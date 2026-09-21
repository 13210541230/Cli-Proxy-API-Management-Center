# 2025 年度金码奖申报陈述

## 交付物

- `jinma-award-statement.pptd`：13 页可继续编辑的 PPTD 源稿。
- `jinma-award-statement.pptx`：13 页最终视觉交付稿，按浏览器验收渲染图铺满页面，并写入淡入淡出切换。
- `jinma-award-statement.external-bridge.pptx`：使用最新版 Kimi `handleExport` 外部桥接实际导出的 13 页验证产物。
- `pages/`：逐页页面定义。
- `media/`：页面引用的本地图片素材。
- `.qa-final-images/`：PPTD 浏览器导出的最终视觉 QA 图。
- `.qa-pptx-final-images/`：PowerPoint 实际打开并渲染后的 PPTX QA 图。

## 备注

Kimi 最新版已将 SDK 导出切换为 `sdkExportMode=external` + `handleExport`。本次验证确认外部桥接可以回传并直接取回 `downloadUrl`；由于未注入 Kimi 登录令牌，兼容宿主仍以已验收浏览器渲染图作为全页底图生成视觉 PPTX，PPTD 保留完整结构化源稿。两个 PPTX 产物均已验证为 13 页、ZIP 完整，并包含 13 页 `fade` transition。
