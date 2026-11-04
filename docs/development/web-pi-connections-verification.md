# Web pi 连接与账号设置 V2 验证

日期：2026-11-01。用户批准 `docs/design/pi-connection-settings-v2.html` 后实现；这是本地限定范围验收，不是生产部署或真实账号／模型联调结论。

## 已实现

- 全局设置「模型与账号」：默认模型入口、连接列表及编辑／移除、服务器 pi 既有来源只读区；保留原顶栏模型／主题快捷入口，不作为项目设置。
- URL＋API Key：OpenAI 兼容 Chat Completions／Responses、Anthropic Messages、Google Gemini；目录刷新、搜索、多选计数与手动模型 ID。刷新失败或暂时缺失保留已选项，地址／协议／Key变化隔离旧目录及迟到响应。
- OpenAI ChatGPT、Anthropic Claude 账号授权入口：Go PKCE、固定官方端点、手动粘贴回调；绑定原工作台登录，10分钟过期，state核验、取消及单次交换。不是新增 Codex CLI 适配，也不承诺全部 pi 登录方式已支持。
- Go 私有持久化位于工作台数据目录 `pi/pi-connections.json`，目录0700／文件0600；新Key连接使用独立provider ID。Key、access/refresh token不回显、不存浏览器持久存储或业务SQLite／聊天。Key的命令／环境插值按已安装pi 1.4.0转义；上游错误不回传原始响应。
- 编辑 Key 留空保留原值；URL／协议变化要求重填，不能暗中将旧Key发送到新目标。目录GET有总超时／响应上限，禁止代理及重定向，解析DNS后拒绝链路本地／元数据等地址；允许用户明确配置本机／私网接口，HTTP展示明文风险。
- 服务器个人pi源配置只读；新会话使用保存配置，已有会话保留其隔离凭据（含pi后续刷新），显式切换新provider才补充所需项。原生OAuth不继承个人同名provider的地址／header。保存／移除连接不自动切换默认模型，不中断运行任务，也不代撤销提供商凭据。
- Vue灰绿紧凑界面，手机全屏／固定底栏，深浅色；关闭／离开清除表单秘密，未保存修改确认，移除二次确认及键盘焦点约束。

## 自测与独立验收

统一证据目录：`~/.local/state/agent-artifacts/kaiwu/2026-11-01-pi-connection-design/`。`STATUS.md`为唯一当前状态与证据索引，`source-sha256.txt`为本批源码内容清单；工作区HEAD为 `7bd3dafe97c4345b737f2446cc2e7a5e1be9a7e8`，含其他任务的未提交成果，本批未暂存／提交。

| 范围 | 实际命令／结果 | 证据 |
| --- | --- | --- |
| 前端默认单元 | `npm --prefix apps/web test` 退出0，含新增5项连接状态 | `frontend-default.log` |
| 类型／生产构建 | `npm --prefix apps/web run build` 退出0；保留bundle体积警告 | `ui-build.log` |
| Go受影响四包 | `GOTOOLCHAIN=local go -C server test -race -count=1 ./internal/pimodel ./internal/webapi ./internal/agent ./internal/app` 全部ok；同范围vet退出0 | `go-race.log`、`go-vet.log` |
| 连接业务浏览器 | `cd apps/web && npx playwright test -c playwright.connections.config.ts` 5项通过，真实Go＋隔离fake pi＋本地合成模型服务 | `browser-final.log`、`local-ui/` |
| 控件／主题回归 | `cd apps/web && npx playwright test -c playwright.config.ts --project=ui-consistency --project=theme` 11项通过 | `ui-regression-corrected.log` |
| 看板／旧模型回归 | `cd apps/web && npx playwright test -c playwright.config.ts --project=existing -g 'real build: login\|start without a model\|pi models:'` 4项通过，包含必需的首项建项目夹具 | `models-regression-with-fixture.log` |
| 独立验收 | 原生Pi子Agent `cdc1087c-63d1-4f58-8e23-56473281c5ac`，只读源码核查＋Go关键测试12项、前端5项、临时overlay独立边界2项、独立实际浏览器授权表单与state拒绝、语义读图；本地限定范围通过，无已确认阻塞缺陷 | `independent-review.md`及`independent-*.log/png` |

红绿与失败保留：`*-red.log`、`browser-first.log`等记录初始缺实现／测试selector问题；`layout-red.log`证实旧全局dialog样式将780px弹窗压成460px，增强作用域后浏览器通过。扩展回归首轮从仓库根运行，旧测试依赖cwd导致5项夹具路径错误，保留 `ui-regression.log`；改为 `cd apps/web` 后11项通过。旧模型3项单独筛选缺首项建项目／登录夹具而超时，保留 `models-regression.log`；包含原首项后4项通过，未改产品或旧测试来绕过断言。独立字面解析脚本首轮JS替换语法错误及更正日志亦保留，不称产品失败。

实现者与独立验收者均实际读桌面／390px手机、浅色／深色截图。独立验收发现指定的账号实现截图命名不存在，未使用旧设计图顶替，另实际采集并读取 `independent-account-desktop.png`、`independent-account-grant-desktop.png`、`independent-account-mobile.png` 补齐。

## 未验证与收尾

- 未执行真实OpenAI／Anthropic授权交换、OAuth刷新、生成请求／计费测试、真实兼容服务目录矩阵、真实pi新连接端到端模型任务。合成凭据的已安装pi模型注册元数据检查不代表生成成功。
- 未测真手机软键盘／读屏、其他浏览器，不把视口模拟称为真机验收。
- 非提交交付，未执行整个仓库全量门禁或GitHub Actions，未提交／合并／推送／Tag／部署，未更新实际入口 `http://192.168.2.91:7331`。
- 浏览器／隔离服务已结束，17331／17332／17334无监听；仅清理本任务可确认无引用的临时目录，保留其他任务／共享资源与证据。
- 后续首先是经授权的真实pi连接联调和交付。团队协作与通用可配置编排引擎仍是未来方向，优先级需另行确认，本批不扩展。
