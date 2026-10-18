# 自动交付界面版本：本机部署记录

日期：2026-10-18。用户明确要求“部署”。本次仅在原指定私网地址与 7331 端口运行最新已推送版本，不发布其他平台、不执行真实模型或 GitHub 联调。

## 实际版本与入口

- 地址：`http://192.168.2.122:7331`。
- 代码：`f7c02326adf9167e0290da01e08d61eca9d81ac5`。
- 二进制：`server/bin/kaiwu-server-f7c0232`。
- SHA-256：`95734b942db8c173d43011b749fad16a0629f00dab1fe7dfc0bd54c4ca8d9a30`。
- 前端资源：`index-fQFLzhKX.js`／`index-CPHDKdwk.css`，由本次 Vite 构建后嵌入 Go。
- Paseo 当前 Workspace 的托管终端：`开悟网页服务7331`；私有启动器为 `.kaiwu/start-web-delivery-ui.py`，读取既有令牌后 exec Go，不将令牌写入命令行。
- `go version -m` 的 vcs.revision 为上述 SHA，vcs.modified=true：工作树只有两项先前保留的未跟踪测试残留，已核对 tracked/index 无差异；没有隐藏该标记或伪造干净构建。

部署前端口未监听、没有既有服务终端，也没有持久化的 running/starting 会话或活动交付周期；未按旧 PID 文件杀进程。

## 安全边界

**自动交付安全验收仍未通过，本次部署不启用真实自动上传。** 启动器只对本次服务进程移除全部 `KAIWU_GITHUB_*` 环境项，未修改全局认证或配置。实际核对 `/proc` 中该类变量均不存在；`/api/delivery/preflight` 返回 503／`delivery_unavailable`，当前数据库无交付策略及周期。界面增加入口不代表真实自动交付已放行。

真实模型兼容、远程凭据／workflow 定义保护、恢复及主线隔离的未关闭问题见[审查裁决](web-automated-delivery-review-resolution.md)。页面及本机部署通过不能替代它们。不得通过重新注入 GitHub 凭据绕过当前部署边界。

## 备份与数据保留

私有备份目录：`.kaiwu/backups/before-delivery-ui-20261018T063655Z/`（不纳入 Git）：

- `workbench.db`：通过 SQLite backup API 保存部署前数据库。
- `private-data.tar.gz`：保存原数据目录，排除嵌套 backups，保留会话、工作区及原令牌；目录 0700、文件 0600。

实际数据库 schema 1 → 2。部署前后核对：1 个项目、1 张任务、2 个会话、1 条执行记录、19 条消息、19 条原登录记录。逐条消息内容与会话 ID／sessionFile 的摘要一致；原登录表全部行摘要一致，原访问令牌与备份字节一致。`PRAGMA integrity_check` 为 ok。

浏览器验证使用独立测试登录，在内存中保存 Cookie 并重开上下文、刷新检查持久登录；随后只退出该测试登录，原19条记录保持不变。没有把原令牌、登录 Cookie 或真实消息／截图提交到 Git。

**回滚注意**：旧服务不支持 schema 2，不能仅切换旧二进制。需先停止并备份当前现场、核对部署后是否新增数据，再制定数据库回滚；不要自动覆盖新消息或恢复工作区。

## 实际执行与结果

- `npm --prefix apps/web run build`：类型检查、Vite 与 Go 静态资源复制通过。
- `GOTOOLCHAIN=local go -C server build -o bin/kaiwu-server-f7c0232 ./cmd/kaiwu-server`：通过。
- 真实 `/healthz`：200，服务标识正确；真实首页引用本次资源指纹。
- Chromium 使用真实 Go 服务检查登录、重开／刷新登录、交付设置默认关闭、桌面1440px与手机390px页面；无页面异常、无水平溢出。
- 浏览器业务操作未调用 commands、验收／控制、Git merge、预览启动／停止；未发送模型 prompt。服务从 PATH 找到 pi 仅表示程序可用，不声称模型调用通过。
- 真实预检确认 worker 未装配，不联网探测 GitHub。
- 测试后服务继续由 Paseo 终端持有，监听仅 `192.168.2.122:7331`，未新增公网／通配监听。

私有运行摘要、截图、进程与终端身份在 `.kaiwu/delivery-ui-{before-service,deployment,live-verification}.json` 和 `.kaiwu/delivery-ui-live-{desktop,mobile}.png`。本部署记录不包含真实会话日志或凭据。

本次仅部署并更新文档，未自动追加 Git 提交或推送；代码远端仍是 f7c0232。
