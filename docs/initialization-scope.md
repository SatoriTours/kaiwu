# 可继续开发的项目初始化范围

状态：**首批 Go 规格与实施计划已确认并执行，通过本机验证与初次独立验收；3 项边界已获授权修正，通过本机复测及限定独立核对，结果见 [修正记录](development/go-foundation-polish.md)。完整多端初始化未完成。**

后续范围补充（2026-10-11）：用户已确认 Web 为主要开发入口，PC/Android 首版目标保留；`apps/web/` 已在后续获批批次实现 Vue 正式工程与 Go 业务服务，模拟原型仍保留在 `docs/design/`；实际边界见 [服务验证](development/web-service-verification.md)。[Web 设计](superpowers/specs/2026-10-11-web-workbench-design.md) 不属于已完成的生产客户端初始化；以下首批记录保留其原批次范围。

## 目的与已确认范围

用户要求在开悟本目录继续开发、先做好架构设计、先把 Go 搞定，并在仓库内划分服务端、电脑端及未来安卓等客户端。

已采用单仓库、独立 Go 服务模块；电脑端、安卓端、pi 接入和协议模块仅建立说明入口。用户随后明确自有后端全部 Go，原 TypeScript 扩展建议已撤销。不以服务起步代替完整个人 AI 工作台。

- [已确认书面规格](superpowers/specs/2026-10-11-go-foundation-design.md)
- [已确认实施计划](superpowers/plans/2026-10-11-go-foundation.md)
- [本机验证记录](development/go-foundation-verification.md)

## 实际布局

```text
kaiwu/
  server/                 独立 Go module，已有代码与测试
    cmd/kaiwu-server/     环境、信号和退出码
    internal/config/     回环配置校验
    internal/httpapi/    健康接口、错误和日志
    internal/app/        监听与启停生命周期
    scripts/             实际二进制烟测
  apps/
    desktop/             Nim + Leaf，当前仅说明
    android/             Kotlin/KMP/Compose，当前仅说明
  integrations/pi/        pi 外部接入说明，不新增 TypeScript 扩展
  contracts/              跨端产品协议归属，当前仅说明
  docs/                   背景、架构、规格、计划和验证记录
```

根目录没有 Go module 或 go.work；未来其他端放在 apps/<端名>/，不提前建未批准工程。

## 首批已实现

- module `github.com/SatoriTours/kaiwu/server`，语言基线 Go 1.25.0，固定 chi v5.2.3。
- GET/HEAD /healthz、JSON 404/405、安全错误响应、JSON stderr 日志。
- KAIWU_LISTEN_ADDR 默认 127.0.0.1:7331，仅数值回环；不开放远程服务。
- 显式 HTTP 超时、SIGINT/SIGTERM 优雅停止和 5s 超时失败回收。
- 配置、HTTP、生命周期、命令退出码测试；race/vet/构建及实际二进制烟测。
- 基础忽略/编辑器配置和各模块导航。

本机非稳定 Go 工具链上的结果不替代稳定工具链、正式部署平台、三平台 PC 或 Android 验收。测试服务和临时构建产物已回收，不保留常驻服务。

## Go 服务基础首批未做（随后 RPC 批次另记）

- pi RPC、真实模型请求、工具桥、任务编排、配置快照和模型变更。
- 数据库、WebSocket 事件、持久化、重连/恢复、认证/配对/TLS。
- Nim/Leaf 与 Kotlin/Gradle 可构建客户端工程及 UI；没有安装全局工具或修改 Leaf。
- Android SQLite/自动更新、签名，PC 三平台打包。
- GitHub Actions、Tag/Release、许可证、Git 提交/推送。

两类 Actions、Android SQLite/更新和三平台 PC 仍是后续多端要求。Go 骨架完成不代表完整初始化流程或首版通过验收。

## 执行与后续授权

本批次按用户确认方式由当前 Agent 单写入执行；空仓库没有基线提交，不为建立 worktree 擅自提交。独立验收使用相同目录只读检查，不能并发编码。

随后纯 Go pi RPC 批次的 [规格](superpowers/specs/2026-10-11-go-pi-rpc-design.md)、[计划](superpowers/plans/2026-10-11-go-pi-rpc.md) 与串行编码＋独立只读验收方式已获批；基础包已实现、Go 假进程自测通过，独立验收已通过（首轮问题修正后复验）。见 [RPC 验证记录](development/go-pi-rpc-verification.md)。这不是 HTTP 任务入口或真实 pi 联调。需要提交基线、真实 pi/模型调用、远程监听、客户端工具链安装、Leaf 修改或发布时，分别取得授权。
