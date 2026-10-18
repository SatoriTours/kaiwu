# 完成区与成果历史：生命周期隔离复验及视觉补验

日期：2026-10-18。执行者：原会话主控，独立于实现写端。授权区间21:17:15–21:32:15+08；21:30:28已结束全部测试、截图与进程核查。未改源码、原测试断言、dist或原截图，未提交/推送/更新现用服务。

## 结论

本批完成区紧凑化、按时间/真实消息查看历史的受影响回归及实际视觉检查通过。**43项是隔离分组1+42通过，不是同一Go服务连续43项通过。** 产品8个驻留会话、不自动空闲回收的既定行为未改变。实时进展流不在本次实现内，尚未接入；真实模型、现用网页服务及远程交付没有借此声称验收通过。

## 原因与分组

原作者 `/tmp/kaiwu-start-failure-diagnosis.md` 的一次有效只观测overlay实证 `session capacity reached`、实际instances=8，纠正先前静态猜测。按批准，不改产品上限/回收/模型，也不在测试里回收业务会话或重置状态；原spec不动，用两个全新测试服务生命周期运行：

| 批次 | 实际范围 | 结果/退出码 | 服务证据 |
|---|---|---|---|
| 1 | existing精确history用例，原全部断言 | 1/1通过，exit0，2.9秒 | 21:17:54.994服务启动；fixture root `/tmp/kaiwu-web-service-qkQfFt`，data为其/data |
| 2 | grep-invert只排除批次1的精确用例，保留existing32+delivery10相对顺序 | 42/42通过，exit0，76.6秒 | 21:18:15.222重新启动；fixture root `/tmp/kaiwu-web-service-fLr3kI`，data为其/data，和第一批不同 |

两批之间实际核对17331/17332均无监听，再启动第二批；不是仅新建browser context。原server.mjs每次mkdtemp并以该目录/data启动Go，两个指针和启动日志保留。没有重复npm test/vue-tsc/build。

命令：在apps/web，均以 `NODE_OPTIONS=--import=/tmp/kaiwu-readonly-test-output.mjs` 及各自 `KAIWU_READONLY_OUTPUT` 执行原Playwright测试；第一批用 `/tmp/kaiwu-result-history-isolated-one.config.mjs --project=existing --grep 'result history UI: compact completed bar, history toggle, request summary, heading without 第N轮, and mobile layout$'`；第二批用 `/tmp/kaiwu-result-history-isolated-rest.config.mjs --grep-invert` 同一精确标题；均 `--trace=on --max-failures=1`、结果只到各自/tmp目录，timeout150秒有界。临时配置只分组、输出和原webServer的cwd，不改spec/断言/业务语义。

完整证据：`/tmp/kaiwu-result-history-isolated-{one,rest}.{log,json}`、`/tmp/kaiwu-result-history-lifecycle-{one,rest}.json`；原生trace在对应输出目录。只有1+42都通过才计43通过，零跳过、零flaky。

## 实际截图与视觉

额外视觉使用原Go测试启动器及隔离fakepi、标准真实API/UI创建合成项目，不mock业务响应。截图不是现用网页服务：

- `/tmp/kaiwu-result-history-visual/1440-completed-unmerged.png`：未合并时保留显式审核入口。
- 同目录 `1440-completed-merged.png`、`390-completed-merged.png`、`360-completed-merged.png`：在临时仓库通过实际UI审核提交并合并后，展示真实master分支，无重复合并按钮；这不是为修空seed测试而偷偷补初始提交。三个尺寸按钮边界完整、无横向溢出；主控实际查看1440及360截图。
- 第一份额外视觉脚本在历史文字检查上exit1：误把task.request当作Round对应用户消息；实际该条是合法原文“开始开发”。原失败脚本和四张图保持，未把错误藏掉或当产品失败修源码。
- 经协调者单独允许，在剩余窗口内新建 `/tmp/kaiwu-result-history-history-visual.mjs`，直接从真实snapshot按firstRoundId定位用户Message.displayText/text作为独立预期，逐项对照时间、两条摘要、历史选择和标题；不调用被测helper自证，不硬改文本匹配，不重跑1+42。21:27:18执行，exit0，5项检查、5张图、0页面异常。
- 新截图 `/tmp/kaiwu-result-history-history-visual/{1440-history-expanded,1440-history-selected,1440-history-result,390-history-selected,360-history-selected}.png`。主控实际查看1440展开/历史结果及390/360历史选择；已确认旧记录无当前确认按钮，返回最新可用，历史预览明确说明不能复现历史工作区，标题含对应时间和真实消息，无第N轮编号。
- 新脚本原始核对结果 `/tmp/kaiwu-result-history-history-visual/verification.json`：firstRoundId=ed08ccb58c3f70167f5f39994bb8ee07，firstMessageText=开始开发，time=10/18 21:27。

低危视觉观察：历史选择框展开时手机上会遮挡部分历史提示文字；历史状态角标和“返回最新”仍可见可操作。部分辅助文案仍有“历史轮次/本轮结果”，但主要历史选项已为时间+原消息。未在只读窗口中顺手修样式或扩展范围。

## 只读、停止及未验项

- `/tmp/kaiwu-result-history-before-isolated.json`的473文件逐条重算0差异；原源码、测试、原图、dist、原夹具指针未变。55源码清单hash仍f852deedcf8126b01cb8e8bed9c8625356fe48d0067d7f6a8256103d6e157a50，两处dist/index均3ec564874eb567aaeb83f36881f98c8cbd2c63dd52ce99f3c5538081556e8559。
- 输出重定向只涉及批准的PNG写入、夹具指针写入/对应读取和Playwright输出，全在/tmp。映射日志位于readonly-single/readonly-rest/readonly-visual/readonly-history-visual各目录。
- 21:30:28核验17331/17332/17441/17442/18999均无监听，按本次四个专属fixture根扫描/proc未发现残留自有进程；两个额外browser均在finally按对象句柄关闭，原服务按自有child句柄SIGTERM并等待退出，没有任何名称级pkill或清理他方进程。
- `/tmp/kaiwu-result-history-isolated-preservation.json`保存473文件比对、fixture根、无残留进程和实际视觉结果。证据保留，不覆盖旧失败日志/trace。
- 手机真机输入法/屏幕阅读器专项、真实模型/现用服务部署未测；切任务历史选择没有另开新视觉场景，已有原始会话/卡片隔离回归及纯函数检查通过。不能用本结论替代交互任务的独立验收或整体checkout交接。
- 当前无运行中/待执行测试、服务器、截图或源码写工具；整体写权由协调者核对，未向a827自行交权。主控保持只读，后续源码更改/部署另需相应授权。
