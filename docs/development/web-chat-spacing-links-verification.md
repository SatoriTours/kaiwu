# 聊天气泡间距与网页结果链接

## 范围与结论（2026-10-31）

本批本地限定范围实现、自测与独立只读功能／桌面手机浅色读图验收通过；主控另目视手机深色。**用户已要求提交代码，交付进行中；实际提交、推送、部署结果见外部证据索引。未调用真实模型。** 本地验收本身不代表现用网页已生效。

- 用户气泡垂直内边距收至 6px；修正 CSS 特异性导致的首尾段落各 12px 外边距叠加，保留段间距与 Markdown。
- 回复中的结果链接直接显示在摘要下，不再藏在“执行详情”；从正文及报告 summary/results/next 提取完整 HTTP(S) URL，去重、最多六条、拒绝带用户名／密码地址，不扫描工具日志。
- 每轮提示要求网页交付返回真实 URL 或说明受控预览入口，区分服务器本机与用户设备、未启动／停止／未部署状态；静态网页优先声明实际静态 manifest。保持预览显式启动，不猜端口、不扩大服务与部署授权。
- 已有历史如果没有返回 URL，不能凭空补出；提示词改进不保证真实模型遵循。

## 改动与证据

基线 HEAD：`fc05f40d0ac115660814587a23620e2325ed537c`，main 共享普通 checkout，保留其他未提交成果。功能／测试七路径：
`apps/web/src/{chat-focus.css,chat-messages.ts,AssistantReply.vue}`、
`apps/web/tests/{workbench.test.ts,chat-turn.browser.spec.ts}`、
`server/internal/agent/{launch.go,testdata/fakepi/main.go}`。

原始证据：`~/.local/state/agent-artifacts/kaiwu/2026-10-31-chat-spacing-links/`。

| 检查 | 实际结果／证据 |
| --- | --- |
| 新增 URL 单元红绿 | 红态返回空数组；绿态 workbench 38 项通过，unit-red.log / unit-green.log |
| 气泡浏览器红绿 | 红态桌面／手机气泡额外高度 48／46px；绿态 ≤16px、首尾 margin 0，browser-red.log / browser-green.log |
| 默认前端测试 | `npm --prefix apps/web test`，100 项通过，frontend-tests.log |
| 构建 | `npm --prefix apps/web run build`，通过，build.log |
| 受影响 Go | `GOTOOLCHAIN=local go -C server test ./internal/agent ./internal/workbench`，两包通过，go-tests.log |
| 浏览器回归 | `npx playwright test --config playwright.chat-turn.config.ts -g 'Compact user bubbles\|V5'`，5 项通过，含1440浅色、390浅／深色与两项V5聊天／开发结果保留，browser-regression.log |
| 独立验收 | independent-review.md：源码／证据／两项URL定向测试、边界内存断言及两张浅色截图实际读图通过 |
| 视觉 | delivery-1440.png / delivery-390.png / delivery-390-dark.png；主控三图目视，通过本批间距与链接展示检查 |

浏览器使用真实 Go＋隔离假 pi，URL 为合成 example.com 数据；不证明真实求和网页部署或模型交付成功。开发期未执行提交交付全量门禁，未更改现用服务生命周期。用户随后已授权提交代码，沿用既有干净归档门禁及配置部署，最终 SHA、全量结果和实际入口验证记录于同一证据目录的 delivery-status.md；不另把开发期测试当交付全量通过。当前协调主目录仅一个 worktree（main，.git/index），唯一只读子 Agent 已结束且报告位于外部证据目录；无待整合的本任务其他空间，主目录因混合未完成果保留，不归档。
