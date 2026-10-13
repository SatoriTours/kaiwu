# 聊天 Markdown 与输入预览验证（2026-10-12）

用户已确认完整方案：消息支持 Markdown、表格、代码高亮和复制，输入框支持预览；沿用已有 WebSocket 和会话隔离。这是现有聊天流程的局部增强，当前 Agent 在主目录串行实现，未提交 Git、调用真实模型或接入 Codex。

## 实际改动

- `MarkdownContent.vue` 共用于聊天消息、草稿预览、结果摘要和工具记录；正文保留原文，浏览器派生渲染结果，不迁移数据库或改变产品 API。
- 支持 Markdown 标题、粗体／斜体／删除线、行内代码、列表／任务清单、引用、链接、表格与代码块。代码块显示语言、提供复制；常用语言及别名高亮，未知语言保留纯文本。
- 工具记录保留折叠结构，JSON 可读缩进／高亮／复制；不把工具输出解析成任意网页。
- 输入框右上角眼睛图标切换预览，铅笔返回编辑并恢复焦点；切范围回到编辑模式，原有独立草稿保留。发送成功清空原范围草稿并退出预览；预览同样支持 Ctrl／⌘ + Enter，组合输入不误发送。
- 流式内容按原 WebSocket revision 通知读取快照；未闭合代码围栏随文本更新即可显示。未增加第二套事件协议或改变连接与 pi 的生命周期。
- 原始 HTML 转义为文字，生成的 HTML 经 DOMPurify 白名单净化。HTTP(S)／mailto 链接允许点击并隔离新窗口；其他协议／相对文件路径仅显示文本，图片显示为链接，不自动加载。未实现公式、Mermaid、交互终端或其他 CLI 适配。
- 大于 100,000 字符的内容完整回退为纯文本块；单代码块大于 20,000 字符时保留原文并跳过高亮，不猜测语言。代码块／表格局部滚动，不撑宽页面。
- 复制优先 Clipboard API；私网 HTTP 没有该 API 时，由用户点击触发 `execCommand('copy')`，临时元素及时移除并恢复焦点／选择。失败明确提示手动选择；复制反馈定时器在内容变化／卸载时清理。

固定前端依赖：`marked@18.1.0`、`dompurify@3.4.16`、`highlight.js@11.12.0`，依赖锁文件已更新。所有资源同源打包，无 CDN、Node 后端或 pi 扩展。

## 实际验证

| 检查 | 结果 |
| --- | --- |
| `npm --prefix apps/web test` | 5/5 通过 |
| `npm --prefix apps/web run build` | TypeScript／Vite 通过，静态资源复制到 Go embed |
| `npm --prefix apps/web run test:browser` | 最终完整 Chromium 15/15 通过 |
| `GOTOOLCHAIN=local go -C server test ./...` | 全部通过；首次受沙箱禁止绑定套接字影响，允许隔离本机监听后复验通过 |
| Go 生产构建 | `server/bin/kaiwu-server-markdown` 构建通过 |
| 实际局域网 Chromium | 7331 重启前登录保留、预览／高亮／表格、HTTP 复制、原文恢复、WebSocket 通知均通过；项目 revision 不变，未发送消息 |

浏览器用真实 Go 服务、临时 Git 目录和隔离假 pi，不调用模型。新增场景实际发送 Markdown 并通过 WebSocket 更新，检查结构化展示、代码精确内容、未知语言、脚本／危险链接不执行、刷新恢复与窄屏不溢出；关闭 Clipboard API 后复制并读取真实系统剪贴板确认原文。草稿场景检查切范围不串、返回编辑焦点、预览快捷键发送及 SQLite 快照中的原始消息。隔离假 pi 新增三段流式固定样例，验证围栏尚未闭合／轮次尚未结束时已有代码展示。

先运行新增验收，实际在缺少 Markdown 标题渲染处失败。实现后发现严格 URI 正则也过滤了复选框类型和按钮辅助属性；核对 DOMPurify 源码后保留内置 URI 净化及渲染器的协议限制，完整回归通过。测试中修正假 pi 前缀导致标题不再位于行首的错误假设及异步旧回复定位歧义。隔离登录 Cookie 仅存本轮临时服务目录、0600，随测试服务清理，避免 worker 重启后的登录限流掩盖失败。

检查合成内容截图：`/tmp/kaiwu-markdown-desktop.png`、`/tmp/kaiwu-markdown-mobile.png`。本批未验证 Safari／Firefox、真实模型格式兼容或原生客户端，不以 Chromium 结果代替这些验收。

## 运行记录

固定入口仍是 `http://192.168.2.122:7331`。核对 PID／二进制归属后替换为 `server/bin/kaiwu-server-markdown`，保留原数据目录、访问令牌和登录凭证，健康检查返回 200。实际私网 HTTP 下确认 `navigator.clipboard` 不存在，预览中的复制仍成功；只操作临时浏览器草稿，清空后刷新，服务器状态 revision 未改变。临时登录 Cookie 检查文件已清理，未保存真实对话截图或打印密钥。
