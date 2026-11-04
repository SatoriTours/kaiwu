# pi 连接设置实施计划

目标：按用户批准的 V2 设计，将 pi 连接／账号管理加入全局设置；模型支持刷新、搜索、多选与手动添加。
规格：docs/design/pi-connection-settings-v2.html 与本会话已确认需求。原会话连续实现、自测，再独立只读验收；不新建工作区、不提交／推送／部署。

## 实现边界
- Go 后端、Vue 3 前端，无新依赖或 pi 扩展。凭据存放服务数据目录的 0600 私有文件，不进入 SQLite 业务快照、聊天、日志或浏览器持久存储。
- 服务器个人 pi 配置只读。新连接生成独立 provider ID；OAuth 使用 pi 原生 provider 并明确标注优先于同名服务器配置。已有运行进程／会话的凭据快照不追溯。
- 模型目录按所选协议发送有界 GET，不发送模型生成请求；禁止重定向、凭据命令／环境插值及错误回显。自定义地址不带用户名／密码／查询／fragment，HTTP 地址显示明文传输提示。
- OAuth 根据已安装 pi 1.4.0 的公开客户端实现，用 Go PKCE＋手动回调，不新增监听端口；首批提供 OpenAI ChatGPT 与 Anthropic Claude 支持的授权方式。固定官方授权／令牌端点，绑定原工作台登录、state、10分钟有效期；无真实授权／模型测试。

## 步骤
1. [x] 写并运行失败的 Go 测试：私有持久化、脱敏列表、并发修订、空 Key 保留、移除、模型目录边界、只读原配置与会话快照。
2. [x] 实现 server/internal/pimodel/connections*.go：持久化、模型获取、隔离配置叠加；接 Catalog 和 agent.Config，不改变原 Prepare 调用者语义。
3. [x] 写并运行失败的 OAuth／Web API 测试；实现 PKCE／固定端点／有界交换／失效、认证／CSRF 和双向业务通道。
4. [x] 写并运行失败的前端状态测试及真实 Go＋隔离 fake pi 浏览器用例；实现 PiSettings.vue／CSS 和 connections.ts，接 App 全局设置与已有模型选择器。
5. [x] 前端默认测试／构建，受影响 Go 包 race／vet，真实 Go 浏览器关键流程和桌面／手机深浅色截图；独立验收功能、安全边界及视觉，主控核 diff。
6. [x] 更新 README／roadmap 与同任务 STATUS，保留失败证据、关闭本任务浏览器／测试服务；报告本地实现与未验证／未部署边界。

## 重点回归
Key 不回显且不能转为命令；HTTP失败不泄露响应或秘密；URL／Key变化使旧目录失效但不丢手动选择；并发／迟到响应不串编辑对象；刷新保留手动模型；OAuth state／登录隔离／过期／单次交换；关闭弹窗不停止任务；运行中会话保持旧凭据。

完成：本地限定范围独立验收通过；实际命令、失败边界、截图及未联调／未部署限制见 `docs/development/web-pi-connections-verification.md` 和同任务证据目录 `STATUS.md`。未完成真实账号／模型联调或提交交付。
