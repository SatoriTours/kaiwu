# 本地自动部署与运行版本

日期：2026-10-19。范围：本仓库的通用部署脚本（`scripts/`）、运行版本接口
`GET /api/version` 与目标上的用户级 systemd 服务。本文不含任何真实机器名、地址、
凭据或密钥；真实目标配置只存在于被 Git 忽略的 `.kaiwu/local-deployment.conf`。

## 目标与边界

- 只在本地已提交的主分支快照上测试和构建，通过后自动部署到用户配置的本机目标。
- 用户能在实际入口看到运行中的完整修订号、构建时间与最近一次部署结果；失败不冒充
  成功，未部署的开发构建明确显示 `dev`。
- 使用目标上已有的用户级 systemd（`systemctl --user`），不写系统目录、不新增
  轮询或 AI 调度、不安装全局工具、不接管其他任务的进程与端口。
- 脚本只读取显式传入的本地配置，不复制模型或 GitHub 凭据，不提交、合并或推送。

## 运行版本接口

`GET /api/version` 在现有登录 Cookie 与同源检查内返回，响应始终
`Cache-Control: no-store`：

```json
{
  "revision": "8b54309f03dd75759291be48ff3f3b766ab89b5e",
  "shortRevision": "8b54309",
  "summary": "版本可见性与部署状态",
  "builtAt": "2026-10-19T01:02:03Z",
  "deployedAt": "2026-10-19T01:03:00Z",
  "deployment": {
    "revision": "8b54309f03dd75759291be48ff3f3b766ab89b5e",
    "status": "succeeded",
    "at": "2026-10-19T01:03:00Z",
    "message": "部署成功"
  }
}
```

约定：

- `revision` 永远来自运行中的二进制（构建时用 `-ldflags -X` 注入的完整 40 位小写
  Git id）。没有注入值的开发构建为 `dev`，服务不读取 checkout 的 `HEAD` 冒充版本。
- `builtAt` 是构建时间（UTC RFC3339），与部署时间分开；无法解析时为空字符串。
- `deployedAt` 只在部署状态记录的“最近一次成功修订号”等于运行修订号时返回，否则为空。
- `deployment` 描述最近一次尝试（可能失败，修订号可与运行版本不同）。
- `deployment.status` 取值固定为 `testing`、`building`、`deploying`、`succeeded`、
  `failed`、`unknown`；`message` 是服务端根据状态给出的固定中文说明，不回显状态文件
  内容，因此不会泄露路径、命令或凭据。`failed` 的固定说明是“部署未完成，请核对当前
  版本/服务状态”，不再无条件声称“当前版本保持不变”。字段缺失、格式错误或缺少可用
  修订号和时间时一律降级为 `unknown`，不声称成功。

UI 只通过认证 HTTP 读取该接口；加载失败或字段为空时显示“版本未知”，不沿用旧的
成功结果。前端构建必须嵌入 `VITE_BUILD_REVISION`，与 `revision` 不一致时提示刷新页面；部署也强制要求产物内嵌完整候选修订号。

## 部署状态文件（只读）

目标服务数据目录（`KAIWU_DATA_DIR`，绝对路径、私有权限）下的
`deployment-status.json` 由部署脚本原子写入（0600），服务只读、有界（64 KiB）读取，
缺失或损坏按 `unknown` 处理：

```json
{
  "revision": "<最近一次成功激活的完整修订号>",
  "deployedAt": "<该次激活时间，UTC RFC3339>",
  "deployment": {
    "revision": "<本次尝试的完整修订号>",
    "status": "succeeded",
    "at": "<本次尝试时间，UTC RFC3339>"
  }
}
```

## 触发方式

```bash
# 安装/移除本地 reference-transaction 钩子（默认使用 .kaiwu/local-deployment.conf）
scripts/install-hooks.sh
scripts/install-hooks.sh --uninstall

# 手动执行一次完整流水线（不等待提交）
scripts/deploy.sh

# 读取目标上的最近一次部署记录
scripts/deploy.sh --status
```

钩子语义：

- 只在 `committed` 阶段、且 `refs/heads/<DEPLOY_BRANCH>` 有非删除更新时触发；其他分支、
  其他阶段和删除一律跳过。
- 钩子会清理从 Git 继承的 `GIT_DIR`／`GIT_INDEX_FILE` 等变量，并对自己的 Git 调用使用
  `-c core.hooksPath=/dev/null`，避免对错误仓库操作或递归触发。
- 只写入仓库自己的 hooks 目录，或仓库内显式配置的 `core.hooksPath`。指向仓库外的共享
  hooks 目录会被拒绝；`core.hooksPath=/dev/null`（显式停用）会明确报错，不假称安装成功；
  已有其他 `reference-transaction` 钩子不会被覆盖；`--uninstall` 只移除自己创建的钩子。
- `--config` 传入的配置必须未被 Git 跟踪；在仓库内还必须被忽略，否则安装直接失败。
- 不使用轮询或后台调度；为可靠性同步执行测试与部署，合并或提交可能等待。同一目标的
  部署用锁串行，等待超时后明确报错、不并发写入。
- 失败只更新“最近一次尝试”并打印失败，不改写 Git 历史、不回滚 commit。

## 流水线

1. 解析本地配置（严格校验，元字符在接触目标前拒绝），向目标 `prepare`（创建私有目录、
   首次生成访问令牌 0600、写用户服务单元），记录 `testing`。
2. 确认浏览器门禁可执行：默认必需，找不到 Chromium 立即阻塞；只有显式
   `DEPLOY_BROWSER_TESTS=0` 才记录本批未做 UI 验收。
3. 再次核对主分支仍指向候选，`git archive <sha>` 导出到私有临时目录（不打包未提交或
   未跟踪文件）。归档没有 `.git`，所以流水线为整个子进程树导出
   `GOFLAGS=-buildvcs=false`：所有真实 `go build`（含浏览器夹具里的 `go build`）都不做
   VCS 标记，运行修订号只来自 ldflags。在该副本内依次执行 `go vet`、`go test ./...`、
   `npm ci`、前端单元测试。每一步的真实输出写入私有运行日志，失败可具名定位。
4. 记录 `building` 后**先构建前端**（`VITE_BUILD_REVISION=<sha>`）：干净归档没有预构建
   页面，旧顺序会让浏览器夹具得到 503。产物必须嵌入完整候选修订号，且构建后真正扫描
   产物不得含 `__MOCK_` 测试标记（单元测试的 existsSync 守卫不算产物门禁）。之后才跑
   浏览器四套件（交互、主流程分组一、主流程分组二、版本套件）；版本套件通过
   `KAIWU_VERSION_TEST_PORT` 与 `KAIWU_VERSION_TEST_REVISION=<sha>` 接收独立端口和候选
   修订号，夹具的后端与断言随候选变化，不再固定为 `8b54309`。
5. 最后用带完整修订号、构建时间和提交说明的 `-ldflags`（并显式 `-buildvcs=false`）构建
   单个 Go 二进制，打包的就是与测试相同的前端产物与候选 SHA。
6. 切换前再核对主分支未前进；上传候选包到目标 `incoming/`。
7. 目标侧 `apply`：**先停自有服务**。`systemctl stop` 必须成功且有界；随后必须取得确定
   停止证据（`ActiveState` inactive/failed、`SubState` dead/failed、`MainPID=0`、
   `ControlPID=0`，cgroup 可读时必须无进程）。失败、超时或状态未知一律视为未停，不开始
   备份。取得证据后才用用户级 Node 内置 `node:sqlite` 备份 API（或可校验的
   `sqlite3 .backup`）把 SQLite 一致备份到私有备份目录并做完整性校验；两者都不可用时
   才在已停写的前提下整体快照 DB＋WAL＋SHM，并记录所用方法。随后记录 schema 标记
   （`PRAGMA user_version`）与文件状态，用同目录临时符号链接加 `rename` 原子切换
   `current`，再启动服务。
8. 服务就绪后通过**实际访问入口**登录并核对：HTTP 必须真实返回 200；`/api/version` 的
   完整修订号等于候选、声明 `no-store` 且类型为 JSON；页面引用的静态资源按扩展名核对
   `Content-Type`，拒绝被 SPA 回退伪装的 HTML；静态 JS 必须无条件嵌入完整候选修订号，
   与后端一致。只有全部满足才写入 `succeeded`。

失败语义：

- 测试或构建失败：只写入 `failed`，目标服务不停止、当前版本不替换。
- 候选启动前失败（例如备份失败）：重新启动原服务，不切换。
- 候选已启动后失败：先停候选；只有确认数据库 schema（`user_version`）未变化、或数据库
  完全未写入时才切回上一个发布；否则**不自动回退**，把候选保持停止、保留失败发布目录与
  备份，标记 `failed` 并要求人工核对。任何情况下都不覆盖或回滚数据库内容。

## 本地配置

复制 `scripts/deployment.conf.example` 到 `.kaiwu/local-deployment.conf` 并替换占位符。
`.kaiwu/` 已被 Git 忽略，不要把真实地址、机器名或凭据写进仓库。支持的项目：

| 配置项 | 说明 |
| --- | --- |
| `DEPLOY_BRANCH` | 触发部署的主分支名（只响应 `refs/heads/<分支>`） |
| `DEPLOY_SSH_HOST` / `DEPLOY_SSH_PORT` | 目标 SSH 主机与端口 |
| `DEPLOY_REMOTE_BASE` | 目标发布目录（绝对路径，存放 releases、current、备份、日志） |
| `DEPLOY_DATA_DIR` | 目标服务数据目录（绝对路径，存放 SQLite 与部署状态） |
| `DEPLOY_DB_NAME` | 服务 SQLite 文件名（默认 `workbench.db`） |
| `DEPLOY_SERVICE_NAME` | 用户级 systemd 服务名 |
| `DEPLOY_LISTEN_ADDR` | 服务监听地址（数字私网 IP 加固定端口） |
| `DEPLOY_HEALTH_URL` | 交付给用户的实际入口（`http://地址:端口`，无路径参数） |
| `DEPLOY_HEALTH_TIMEOUT` | 就绪等待秒数 |
| `DEPLOY_HTTP_CONNECT_TIMEOUT` / `DEPLOY_HTTP_MAX_TIME` | 每次 HTTP 的连接与总超时秒数 |
| `DEPLOY_LOCK_TIMEOUT` | 等待其他部署结束的秒数 |
| `DEPLOY_BROWSER_TESTS` | `1` 必需（默认）、`0` 显式记录未做 UI 验收 |
| `DEPLOY_CHROMIUM` | 可选 Chromium 绝对路径；留空自动查找 |
| `DEPLOY_BROWSER_PORTS` | 浏览器测试的三个端口（existing/delivery/interaction），逗号分隔且不重复 |
| `DEPLOY_VERSION_BROWSER_PORT` | 版本浏览器套件（`playwright.version.config.ts`）的独立端口，不能与前三个重复 |
| `DEPLOY_STOP_TIMEOUT` | 停止自有服务并取得确定停止证据的秒数（默认 20） |
| `DEPLOY_REMOTE_PATH` | 远端服务 `PATH`，留空使用用户级 pi／node 与系统默认路径 |
| `DEPLOY_REMOTE_NODE` | 远端 Node 绝对路径，留空自动查找 |

配置值不能包含空白或 shell 元字符；端口、地址、URL、路径都会被严格校验，未知配置项会
被拒绝，脚本在接触目标前终止。

## 凭据、权限与数据

- 首次部署时在目标生成随机访问令牌，只写入目标私有文件 `service.env`（0600），不进日志、
  不打包、不回传，也不复制模型或 GitHub 凭据。
- 目标发布目录、数据目录、备份目录、日志目录均为 0700，状态文件、备份与临时文件为
  0600；核对用的私有临时目录由 `mktemp -d` 加 `umask 077` 创建，成功、失败与信号终止
  （EXIT/INT/TERM）都会清理，信号路径同样不留下含 cookie 的目录。
- 用户单元的 `UnsetEnvironment` 显式取消继承的 `KAIWU_GITHUB_*` 自动交付凭据，部署不会
  意外启用未验收的交付 worker。
- 持久数据目录与发布目录分离；失败候选保留现场，数据库由服务自身管理，脚本从不写入或
  回滚数据库内容。
- 目标只使用用户级 systemd 和用户目录，不需要也无权写系统目录；只会 `stop`／`start`
  自己创建并核对归属的用户单元，不会杀进程或接管同名外来服务。

## 浏览器检查与已知基线

浏览器检查默认必需；缺少 Chromium 会阻塞部署（可用 `DEPLOY_BROWSER_TESTS=0` 显式记录
本批未做 UI 验收，但不会静默跳过）。门禁包含四部分：交互套件、主流程分组一（结果历史
单独生命周期）、主流程分组二（其余用例与交付项目），以及版本 UI 套件
（`playwright.version.config.ts`）。主流程受产品“最多 8 个驻留会话、不自动回收”的限制，
必须按既有回归记录分成两个独立服务生命周期，`scripts/deploy.sh` 已内建该分组。运行前会
把临时归档内的主流程测试端口替换为 `DEPLOY_BROWSER_PORTS` 指定的独立端口，版本套件端口
由 `DEPLOY_VERSION_BROWSER_PORT` 通过 `KAIWU_VERSION_TEST_PORT` 传入；任一端口被占用都
具名失败而不接管该进程。任一套件失败都会具名报告并阻止上传与切换。干净归档里没有
预构建页面，因此前端构建严格先于浏览器门禁；版本夹具的后端修订号与断言由
`KAIWU_VERSION_TEST_REVISION` 传入候选值，不用固定 `8b54309`。已知的 Go 崩溃注入
用例位于自动交付路径，若出现偶发失败按实际名称报告，不用跳过或换模型绕过。

## 隔离测试

```bash
sh -n scripts/lib/common.sh scripts/remote-deploy.sh
bash -n scripts/deploy.sh scripts/install-hooks.sh scripts/tests/deploy_test.sh
bash scripts/tests/deploy_test.sh
# 可选：真实用户 systemd 的停止证据复现（只创建自己独特名的瞬态 unit）
bash scripts/tests/stop_service_systemd_test.sh
# 必需：真实干净归档端到端流水线（真实 go/npm/chromium + 假 SSH/目标 systemd）
bash scripts/tests/archive_pipeline_test.sh
```

隔离测试使用真实临时 Git 仓库与真实临时 SQLite 数据库，并以本地假
`ssh`／`scp`／`systemctl`／`curl`／`go`／`npm` 运行，不联网、不接触真实目标、不加载模型；
临时目录全部落在测试自己的 `TMPDIR` 下。覆盖：钩子阶段／分支／删除过滤、继承 Git 环境的
清理、未提交文件不进入归档、全量 Go 失败不替换当前版本、真实测试输出写入私有日志、
真实 200 与资源类型／前端 SHA 核对、SPA 回退伪装被拒绝、前端未嵌入完整 SHA 时构建阶段
失败、远端核对不可用参数绕过、SQLite 一致备份与完整性校验、schema 未变化时才回退、
schema 变化时不自动回退并保留现场、`deactivating` 过程中进程仍存活时不得继续或备份、
`systemctl stop` 非零或状态未知时失败、核对临时目录在成功／失败／信号后均无残留、目标
不可达时保留旧版本、并发部署串行、同名外来用户单元与无归属服务被拒绝、私有权限、原子
切换、浏览器必需与四套件端口可配置且不接管、配置被 Git 忽略、`install-hooks` 拒绝仓库外
共享路径／`/dev/null`／未忽略或已跟踪配置、非法配置值在接触目标前被拒绝、所有 curl 调用
都带连接与总超时。

真实 systemd 复现脚本在无用户 systemd 时报 SKIP；它只创建、停止并清理自己的瞬态 unit，
不碰任何外来服务。`archive_pipeline_test.sh` 把工作区内容复制到一个临时 Git 测试仓库并
提交（不是项目提交），用 `git archive` 生成无 `.git` 的干净归档，再以真实
go/npm/chromium 完整跑一遍流水线；假 SSH 与假目标 systemd 真实启动构建出的二进制。它
断言：归档内所有 Go 构建成功（N1）、前端构建先于浏览器门禁且页面已就绪（N2）、版本
套件收到候选修订号、构建产物无 `__MOCK_` 标记、最终打包二进制与实际入口返回的完整
SHA 等于候选而不等于夹具常量，并打印二进制与静态脚本的 sha256。HOME/XDG/TMPDIR 与
端口隔离，共享缓存只读为主，不写项目主目录、不接触真实目标或凭据。

## 未覆盖

- 本文档不代表任何真实目标已部署或已验收；真实机器信息只在原协调会话的本地说明中，
  实际部署与入口核验由原协调会话按其授权执行。
- 真实模型调用、远程交付、跨平台与新浏览器组合不在本批范围。前端必须嵌入
  `VITE_BUILD_REVISION`；缺失时构建阶段即失败，远端也强制核对，不能用参数关闭。
