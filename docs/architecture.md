# 架构入口

本文只保留架构导航，避免与更细的运行时、配置、协议文档重复维护同一套实现说明。

## 当前主链路

```text
receiver -> selector -> task_set -> task -> pipeline -> sender
```

职责边界：

- receiver：协议接入，构造 `packet.Packet`，生成完整 `match_key`。
- selector：对完整 `match_key` 做精确匹配，命中 `task_set`。
- task_set：配置复用，在运行时编译为 task slice。
- task：按 `execution_model` 执行 pipeline，并投递到 sender。
- pipeline：task 内部的 packet 匹配、改写、路由 stage。
- sender：最终输出到 UDP/TCP/Kafka/SFTP/OSS/SkyDDS。

## 权威文档

- 架构分层、模块边界和扩展点：`docs/technical-architecture.md`
- 启动、热重载、停机和默认值生命周期：`docs/runtime-and-lifecycle.md`
- 启动/收包/dispatch/reload/shutdown 时序：`docs/runtime-sequence-and-flow.md`
- selector、task_set、task、dispatch 和 route sender：`docs/task-and-dispatch.md`
- pipeline stage：`docs/pipeline.md`
- receiver/sender 协议行为：`docs/receivers-and-senders.md`

## 热路径原则

热路径只保留必要动作：

1. receiver 生成或复用 match key。
2. selector 做一次 map lookup。
3. runtime fan-out 到命中的 task slice。
4. task 执行 pipeline 并发送。

配置解析、默认值、校验、stage 编译、selector 展开、receiver/sender 构建都应停留在冷路径。
