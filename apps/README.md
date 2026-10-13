# 客户端模块

- [desktop](desktop/README.md)：Nim + Leaf 电脑端，Windows/macOS/Linux 均为首版目标。
- [android](android/README.md)：Kotlin 原生 + KMP + Compose Multiplatform 安卓端。
- [web](web/README.md)：主要开发入口，已实现看板＋右侧聊天的本地交互原型，尚无生产工程。

当前仅模块说明入口，尚无可构建客户端工程。Web 模拟交互原型放在 `docs/design/`，不代表业务接入。未来其他端使用 `apps/<端名>/`，不提前创建未批准的客户端。

客户端连接 Go 服务，不作为代码执行节点；首版围绕对话、任务进度、人工确认与结果摘要，不做源码/diff、编辑器或 IDE。
