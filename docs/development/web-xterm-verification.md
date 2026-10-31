# 网页终端直接交互

用户已批准使用 xterm.js＋WebGL，替换输出文本及底部命令框。在当前共享 main 目录实现，保留其他任务未提交成果。用户随后明确要求“提交代码”，进入本批提交、推送、既有全量门禁与实际入口部署核验。

实施顺序：
1. 真实 PTY 回归先覆盖终端类型、尺寸及非法尺寸；补 xterm 浏览器交互回归。
2. 新建独立终端组件，逐块处理原始输出、直接键盘输入及尺寸同步；WebGL 不可用时使用 xterm 默认渲染器。
3. Go 保留现有 Shell 生命周期及租约，新增有界尺寸更新、字节无损输出和客户端消费确认，保持旧连接兼容。
4. 自测受影响 Go 包、前端单元／构建、真实 Go＋Shell 浏览器交互和桌面／手机截图，再独立验收。

验收重点：中文、ANSI 清屏与颜色、历史与补全、Ctrl+C、大量输出、切页接回、窗口尺寸、工作区隔离、断线不重放输入。

状态：本地实现、自测、独立功能及桌面／手机视口视觉验收通过；项目全量测试尚未执行，7331 未更新。未提交、推送、调用真实模型。证据目录：`~/.local/state/agent-artifacts/kaiwu/2026-10-31-xterm/`，源码哈希见 `source-sha256.txt`，独立结论见 `review-final.md`。基线 HEAD 为 `9b799356890eb62088b00d23909d5c844d44073e`，本批交付仅纳入终端相关增量。部署脚本已将新增 terminal 浏览器项目接入项目工具门禁；最终 SHA、全量结果及实际入口证据保存于同一证据目录的 delivery-status.md，后续状态以该记录及运行入口修订号为准。

实现：锁定 `@xterm/xterm 6.0.0`、`@xterm/addon-fit 0.11.0`、`@xterm/addon-webgl 0.19.0`；新增 `TerminalSurface.vue`，移除底部命令表单。使用原 PTY 与 WebSocket，`TERM=xterm-256color`，尺寸限制 2–1000 列、1–500 行。新 `v=2` 连接使用 Base64 保留跨读取的 UTF-8／控制字节，渲染消费确认限制最多一个待消费输出帧；15 秒未确认仅关闭连接，不结束 Shell。旧字符串输出协议保留兼容。

自测证据：
- 新 `TestTerminalXtermGeometryAndControl` 先失败于“terminal cannot receive browser geometry”，补真实尺寸同步后通过。
- `npm run build`、`npm test` 通过；构建仍提示单 JS 包超过 500 kB，不声称已有同机性能对照。
- `go test ./internal/projecttools ./internal/webapi` 及两包 `-race` 通过。新增真实 socket 覆盖无 ACK 不继续输出、输入仍可用、字节无损、PTY 尺寸、非法尺寸断开、Shell 保留、跨 scope 拒绝及旧协议。
- 正式 Playwright `project-tools` 与 `terminal` 最终 3 项一起通过，包含文件／附件回归、默认 input-only 中文／emoji 输入及完整终端交互。初轮失败记录保留：`keyboard.insertText` 在读屏模式不发正文、尺寸测试早于 ResizeObserver。后者等待画布适配后查询 PTY；前者由独立验收确认是真实输入兼容问题，已按下述修复，不以换测试输入方式代替产品修复。
- 主控及独立验收均实际读桌面 1440×900 与手机 390×844 截图，最终布局与中文／ANSI 显示通过。手机为 Chromium 视口模拟，未验证真机软键盘。

独立首轮发现与修复：
1. 默认读屏模式排除 input-only／emoji 输入事件：新增真实浏览器回归先红后绿，默认关闭读屏，工具栏提供可选“读屏”。普通中文 composition 经独立 CDP 验证可用，不能把 input-only 问题描述为所有中文输入失败。
2. WebSocket open 早于首帧消费而启用输入：现首次画面消费完成后才显示连接就绪并启用输入，避免恢复期丢字。
3. 整行画布下方露出 xterm 默认黑底：viewport 设置同色，已重拍桌面／手机并由主控读图确认。
独立还验证了 WebGL2 不可用时回到默认渲染，接回原终端后真实执行成功。最终独立窄复验通过：默认 input-only 中文／emoji 完整发送并执行；代理扣住首帧时不宣称连接就绪、不发送输入，释放后正常且无旧输入重放；读屏开关有效，桌面／手机原图黑条消失。首轮三项均关闭，无残留 Critical／Important。

最终证据：`kaiwu-xterm-{build-final,unit-final,race-final,browser-final}.log`；独立 `independent/final.log`、`independent/final.json` 与 `review-final.md`；`final-ui/terminal-{desktop,mobile}.png`。首轮失败日志、`review-initial.md` 与原图单独保留，未覆盖为成功记录。测试服务已退出；未停止共享服务。模型 usage 未提供，记未知。

环境限制：本机没有覆盖独立探针 emoji 的字体，截图显示缺字方框；只确认其输入和传输，未确认全部 emoji 字形。未安装系统字体，也未做硬件 GPU／跨浏览器或真实手机矩阵、GitHub Actions、7331 实际入口验收。

保留边界：进程及历史不跨服务重启；只保留最近 256 KiB 原始输出，超限重连明确提示截断，不保证任意全屏应用画面恢复。每次粘贴／输入最多 4096 字节，超长整次拒绝，不发送截断命令。仍为服务器账号权限，不是系统沙箱；租约与人工终端／AI 互斥沿用原实现。
