# 自动交付设置：简化界面与发布验证

日期：2026-10-18。状态：界面改版及独立复验通过，代码 `ddc811f` 已推送 main 并部署到原 7331；真实自动交付仍禁用。

## 用户确认的范围

用户查看独立设计稿后，要求去掉没有操作能力、容易误认为任务进度的四步流程说明；“项目准备”改为“交付设置”，三个入口改为“目标仓库”“通过标准”“失败处理”。随后明确授权修改、提交及发布。本次将设计落到正式 Vue 页面，不只是更新 HTML 原型。

- 保留简短用途说明、两种合并方式、紧凑摘要、分步普通表单和默认折叠的高级选项。
- 保留项目实际主分支；不要求用户输入 JSON、内部仓库编号或凭据引用名。
- 仓库／workflow 元数据发现接口尚未提供，不虚构识别结果、示例工作流或凭据就绪状态；已有内部字段应无损保留。
- “保存草稿”通过原业务通道持久化到 SQLite；保存成功来自真实回执，失败保留输入。
- 当前版本不开放自动交付，启用按钮禁用；草稿不能生成授权或启用流程。已有启用策略只读保护，不能因保存草稿而静默撤销或修改。
- 服务仍固定 `192.168.2.122:7331`，沿用既有令牌、数据库与会话。不调用真实模型、不试跑真实 GitHub 交付、不新增 Tag／GitHub Release。

## 安全边界

本批是界面清晰度与布局修改，不关闭先前后端安全审查问题。真实自动交付仍不可启用，服务继续移除本进程的 `KAIWU_GITHUB_*`，不装配交付 worker。隔离测试中的临时 bare、假 pi 和假 Actions 不代表真实远程写入已通过。

原有审查与部署记录：[审查续接](web-automated-delivery-review-resolution.md)、[此前部署](web-automated-delivery-deployment.md)。

## 实际验证

- 开发者首轮自测：前端 28/28、构建、交付浏览器 10/10 通过。
- 独立首轮验收：前端 28/28、构建、完整浏览器 42/42 通过，但补充取证发现 argv 静默破坏、仓库清空无效、换仓库沿用旧 ID、重复检查 ID、1440×900 底栏裁剪五项缺陷，结论为**不通过**，已交回原开发者。测试全绿不能代替必要边界验收。原开发者修复后，由原验收角色独立复验通过：11/11 argv 编解码、5 组复杂 argv 经真实 UI 保存后逐字节一致；仓库清空／等价 URL／换仓库身份、检查编号唯一性、桌面底栏及加载失败禁用保存均有实际证据。
- 修后复验实际执行：`npm --prefix apps/web test` 28/28；`npm --prefix apps/web run build` 通过；`cd apps/web && npx playwright test --project=delivery` 10/10（33.8 秒）。首轮完整 existing 32＋delivery 10 共 42/42，修后未机械重跑不受影响的 existing 32 项。
- 主控已检查实际 diff、范围和独立复验证据，并查看修后 1440×900 与 390×844 合成截图。复验另覆盖 1440×1000、1280×720、360×800 和全部向导；固定底栏完整可见、dialog 不滚动、body 单一滚动，无横向溢出。
- 保存失败保留输入、重开回显、已启用策略只读、零时间序列化、workflow/jobs 保留、预检真实回执且隔离 pushes 0→0，均经独立复验。新增 API 时间零值兼容仅为 disabled 草稿能被 Go 正确反序列化，无后端代码或存储 schema 改动。
- 代码提交：`ddc811f04bda2f51da7c0f21949b9dcc54687682`，已正常推送 `origin/main`，`git ls-remote` 核对一致。当前就在实际默认主分支 main，不制造额外合并提交，也不强推。GitHub workflows 数量为 0，该提交 Actions runs 为 `[]`，没有 CI 被触发，不能声称 CI 通过。
- 7331 实际烟测：Chromium 检查新概览、保存草稿可用／启用禁用、1440/390/360px 布局、Cookie 登录重开与刷新、只读预检仍返回 503 `delivery_unavailable`，6 项通过，0 页面异常，0 commands／Git merge／交付验收控制／预览启动等副作用请求。主控已实际查看真实服务桌面与手机截图（仅存私有目录）。
- 数据保留：SQLite 完整性检查 ok，原 1 项目／1 任务／2 会话／19 消息／19 登录记录保留；逐条消息摘要、会话 ID/sessionFile、原登录行、访问令牌摘要均一致，策略仍为0，未为了烟测保存真实项目配置。只创建并退出独立测试登录，不影响原登录。

合成服务截图：[桌面](../design/web-service/delivery-settings-desktop.png)、[手机](../design/web-service/delivery-settings-mobile.png)。真实项目截图、令牌、Cookie、数据库及会话日志保留在私有忽略目录，不提交到 Git。

## 本机部署与运行证据

- 入口：`http://192.168.2.122:7331`；Paseo 托管终端沿用“开悟网页服务7331”。
- 二进制：`server/bin/kaiwu-server-ddc811f`，SHA-256 `c8730a7e11b0e2cc3fbc092fab2e06b620f59daa45daabbce1bfab1c75a17a4c`。
- 部署前实际运行 `npm --prefix apps/web run build` 与 `GOTOOLCHAIN=local go -C server build -o bin/kaiwu-server-ddc811f ./cmd/kaiwu-server`，均通过。真实首页引用 `index-DiJABmwr.js`／`index-DurnKtjX.css`，已与构建输出比对。
- Go 构建元数据 revision 为上述代码提交；`vcs.modified=true` 因两份保留的未跟踪测试残留，构建时 tracked/index 无差异，没有伪造干净标记。本机工具链为 `go1.27.0-X:nodwarf5`，不代表稳定工具链或跨平台验证。
- 部署前通过 SQLite backup API 与私有目录备份保存到 `.kaiwu/backups/before-delivery-settings-20261018T092152Z/`；重启前确认没有 starting/running 会话或活动交付，核对实际进程身份后 SIGTERM 正常停止，未按陈旧 PID 杀进程。本次 schema 保持2，无新迁移。
- 首次启动被私有启动器的端口探测阻止：旧连接处于 TIME_WAIT，但无 LISTEN。核实后仅将探测 socket 的 `SO_REUSEADDR` 对齐 Go 监听行为，原端口重试一次成功；没有换端口、强杀或放宽监听范围。
- 私有启动器 `.kaiwu/start-web-delivery-settings.py` 仍只读取原令牌并 exec Go，剥离本进程 `KAIWU_GITHUB_*`。实际 `/proc` 核验该类变量为空，真实只读预检503确认 worker 未装配；并非以预检绕过门禁。
- 私有运行及保留证据：`.kaiwu/delivery-settings-{release-before,release-preservation,live-verification}.json`；实际项目截图仅 `.kaiwu/delivery-settings-live-{desktop,mobile}.png`。后续补记文档提交不改变已部署程序，运行代码版本以上述 `ddc811f` 为准。

## 保留的低危观察与未验证项

- 特殊换行参数保存无损，但单行输入框不能忠实展示换行；空串参数与新空行视觉相同。
- 兼容 argv 字符串 helper 已通过 roundtrip，但正式 UI 采用逐参数输入，组件保留未使用导入，属于清洁度问题。
- 已启用策略只读保护仅是本轮界面行为，不能声称覆盖直接 API 调用；既有后端问题及真实模型／GitHub／多平台验收均未关闭。
- 原测试生成的 11 张既有截图已按本批开始时的干净基线恢复，不夹带随机任务名与时间的无关更新；两份此前的未跟踪测试残留原样保留，不提交或删除。
