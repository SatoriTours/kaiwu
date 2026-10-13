# 首批 Go 基础：独立验收请求

## 目标与范围

按已确认规格审查本批次 Go 服务基础及仓库模块划分；不是完整产品或首版验收。仓库没有提交，Base/Head 均不可用，直接核对文件清单，禁止为制造 diff 提交。

- 规格：docs/superpowers/specs/2026-10-11-go-foundation-design.md
- 计划：docs/superpowers/plans/2026-10-11-go-foundation.md
- 实际验证：docs/development/go-foundation-verification.md
- 裁定与执行证据：docs/development/go-foundation-ledger.md

实际文件范围：server/ 全部 Go 源码、测试、go.mod/go.sum、Python 烟测与 README；根 .gitignore/.editorconfig、README、AGENTS；apps/、integrations/pi/、contracts/ 的 README；docs/ 中架构、路线图、初始化范围和本批次规格/计划/记录。

## 只读权限

- 不编辑、格式化、go mod tidy、安装工具或写入项目；测试使用 GOFLAGS=-mod=readonly 和 GOTOOLCHAIN=local。
- 不创建 Agent、workspace/worktree，不动 index/HEAD/分支，不提交/合并/推送，不发布、不调用模型/Agent、不读取个人凭据。
- 允许标准 Go 缓存及本步骤专属 /tmp 构建目录；烟测仅回环地址，结束回收本步骤资源，不清理用户文件。
- 独立执行本次验收，不再次派发审查。

## Review Focus（计划原文）

1. 看似本地的主机名/IPv4 映射 IPv6/zone 地址：不得借名称解析或格式歧义允许非回环绑定；任务 1 固定测试。
2. 已取消 context：不得短暂启动监听器或留下服务；任务 3 固定测试。
3. 处理器写出响应后 panic：不得继续追加伪装成功的 JSON 500；任务 2 固定测试。
4. 请求包含查询参数、请求体、Authorization：日志不得记录这些内容，日志状态必须与客户端一致；任务 2 固定测试。
5. 取消时有在途请求、关闭超时或 Serve 已异常退出：不能报告正常完成或让控制 goroutine 永久阻塞；任务 3 固定测试。

裁定：在 Unwrap 之外支持 FlushError，使 ResponseController flush 后的 panic 不错误追加 500。还应检查日志保密性、强制关闭失败状态、资源与上下文归属。

## 验收方式

先读规格、计划和实际文件，再在 server/ 执行 go test ./...、go test -race ./...、go vet ./...、go mod verify、gofmt -l cmd internal；必要时临时构建并运行 scripts/smoke_test.py。本机为 go1.27.0-X:nodwarf5 Linux amd64，不冒充稳定 Go 或跨平台验证。

规格不是穷举输入清单。对规格未显式写出的行为，按合理用户期待判断，按实际影响分级，不以“未写入规格”回避问题。

## 返回格式

1. Strengths：实际核对的优点。
2. Issues：Critical/Important/Minor，附文件:行号、原因、复现/证据和建议。
3. Declined to judge：考虑但因本批次边界未判断的每项及原因；没有则写无。
4. 实际测试命令、退出结果、未执行的检查。
5. Assessment：本批次是否通过验收；不能把通过等同于合并/推送授权。

仅回复原协调 Agent，不修改项目保存报告；原协调 Agent负责记录和修复。
