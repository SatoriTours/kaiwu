# 开悟 · kaiwu

**从想法到作品，在创造中更新认知。**

开悟是一个本地优先的个人 AI 开发工作台。用专题看板组织任务，在独立对话中与 Agent 协作，跟踪开发进展、查看实际效果，再决定是否确认和合并。

[快速开始](#快速开始) · [使用说明](apps/web/README.md) · [路线图](docs/roadmap.md) · [反馈问题](https://github.com/SatoriTours/kaiwu/issues)

## 界面预览

![开悟桌面工作台：专题看板、独立任务对话与成果确认](docs/images/workbench-desktop.png)

<details>
<summary>查看手机界面</summary>
<br>
<img src="docs/images/workbench-mobile.png" width="320" alt="开悟手机界面：任务进展、成果确认与聊天">
</details>

<sub>当前 Web 界面的演示截图，使用合成项目和对话，不包含真实用户数据。</sub>

## 核心功能

- **按专题组织任务**：功能分组与执行状态独立，进展一目了然。
- **一任务一上下文**：项目与任务分别绑定独立 pi 会话，历史和草稿互不混淆。
- **隔离开发**：任务在独立 Git worktree 中执行，中断后按新的发送意图接回原会话。
- **看结果再确认**：查看进展、工具记录、错误原因及受控网页预览，不把模型回复结束当作验收通过。
- **保留你的控制权**：模型沿用服务器 pi 配置；审核后显式选择本地合并或合并并推送，不自动上传，也不把 Git 成功当作 CI／部署通过。

## 快速开始

需要 **Linux、Go 1.25+、Node.js 22.18+（含 npm）和 Git**。使用 AI 功能前，还需在服务器安装 pi、配置模型登录，并确保服务的 `PATH` 能找到它。任务所需的编译器和工具也应安装在服务器上。

```bash
git clone https://github.com/SatoriTours/kaiwu.git
cd kaiwu

npm --prefix apps/web ci
npm --prefix apps/web run build

export KAIWU_DATA_DIR="$PWD/.kaiwu"
export KAIWU_WEB_TOKEN="$(node -p 'require("node:crypto").randomBytes(32).toString("hex")')"
printf '本机访问令牌：%s\n' "$KAIWU_WEB_TOKEN"
GOTOOLCHAIN=local go -C server run ./cmd/kaiwu-server
```

打开 **http://127.0.0.1:7331**，输入上面的令牌。添加服务器项目目录、选择模型并创建任务，准备好后点击「开始开发」。端口已被占用时，先检查已有服务。

> `.kaiwu/` 保存数据库、会话等私有数据，请勿提交。远程访问请使用受信任的局域网或已有 VPN，并保留认证；Git worktree 不是系统安全沙箱。

## 开发与文档

前端使用 **Vue 3 + TypeScript + Vite**，由 **Go** 同源托管；状态保存在 **SQLite**，Go 通过 RPC 管理第三方 **pi CLI**。

```bash
npm --prefix apps/web test
GOTOOLCHAIN=local go -C server test ./...
```

- [Web 使用与配置](apps/web/README.md) · [服务端开发](server/README.md)
- [产品需求](docs/product-requirements.md) · [架构与边界](docs/architecture.md)
- [本地部署与版本核查](docs/development/local-deployment.md) · [历史开发记录](docs/development/project-history.md)

## 项目状态

聊天已在本地改为右侧独立抽屉：默认收起，看板占满全宽；卡片／项目对话打开720px浮层，手机全屏，关闭不停止任务或丢草稿。57项主配置浏览器、70项单元及22项聊天回归独立通过；独立读图及浅色截图窄复验通过；用户已要求提交代码，主题与抽屉按既有流水线交付；运行版本以实际入口修订号为准。[本批验证](docs/development/web-chat-drawer-verification.md)。

Web 深色模式已在本地实现：默认跟随系统并实时切换，可手选浅色／深色，浏览器记住偏好；登录、看板、聊天、Markdown、菜单及弹窗沿用灰绿风格（其后获批的聊天抽屉布局另见上文）。7项主题浏览器、70项单元与构建自测通过，独立功能／回归／读图验收通过，首轮发现的顶栏高度回归已修复并窄复验关闭；本批已获提交代码授权，与聊天抽屉一并交付，运行版本以实际入口为准。[本批验证](docs/development/web-theme-verification.md)。

任务聊天 V5 已实现并以 `1ebcc7a` 推送 main：保留原 Kanban 与顶栏，支持左右消息、轻量结果操作、折叠历史，普通问答不替换开发结果；独立功能、22 项聊天回归及独立视觉验收通过。用户追加授权修复旧部署门禁：V5 交互断言、测试服务生命周期及真实 Git fixture 追踪已修正，47 项浏览器与150项部署脚本测试及独立复核通过。保留两项非阻塞的回归覆盖缺口，不跳过部署门禁；部署版本以实际入口为准。线上生效以实际入口完整修订号为准。[本批验证](docs/development/web-task-chat-focus-verification.md)。

登录页已按紧凑灰绿设计完成本地改版：支持令牌显示／隐藏，保留 30 天登录；10 项登录浏览器测试及独立复验通过；实际部署以入口显示的修订号为准。[本批记录](TODO.md#当前改版紧凑灰绿登录页)。

2026-10-24 的成果流程增量已在本地实现：静态网页新标签页、显式文件下载、双合并审核与每问题一条可展开回复。下载 Go 后端的 manifest 绑定修复已通过独立复验；前端 68 项单测与构建通过；登录同步及夹具容量问题修正后，正式 `cd apps/web && npm run test:chat-turn` 自动串行分片的全部 19 项 Chromium 自测通过，独立功能／质量窄复验通过，独立视觉验收仍受模型能力限制，待补授权。**用户已要求提交，收尾交付进行中；独立视觉与真实 UI 远端联调尚未完成，不将本地测试称为整体交付通过。线上生效以实际入口完整修订号为准。** [当前台账](TODO.md) · [验证范围](docs/development/web-task-result-preview-verification.md#2026-10-24-产物直达与双合并增量)。

项目仍在开发中，当前优先打磨 **Web + pi** 的任务流程。桌面端（Windows / macOS / Linux，Nim + Leaf）与 Android（Kotlin / KMP / Compose）仍是目标，目前尚未提供原生客户端；Codex、自动多 Agent 编排和真实自动交付尚未开放。

欢迎通过 [Issues](https://github.com/SatoriTours/kaiwu/issues) 交流问题与建议。**许可证尚未确定**，公开仓库不代表已授予使用、修改或分发许可。
