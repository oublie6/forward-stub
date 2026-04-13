# 架构入口

> 职责边界：本文只负责系统结构总览、模块职责边界、热路径原则和扩展点索引。不展开配置字段、启动/热更新细节、协议行为、pipeline stage 或运维步骤；这些内容分别见 `docs/configuration.md`、`docs/runtime-and-lifecycle.md`、`docs/receivers-and-senders.md`、`docs/pipeline.md` 和 `docs/operations-manual.md`。

本文保留架构导航，避免与更细的运行时、配置、协议文档重复维护同一套实现说明。

## 架构分层

```text
入口层: main + bootstrap（参数解析、信号、热重载触发）
控制层: config + validate + reload trigger
运行层: runtime(store/update_cache/compiler)
处理层: task(execution model) + pipeline(stages)
协议层: receiver/* + sender/*
可观测: logx + traffic counter
```

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

- 启动、热重载、停机和默认值生命周期：`docs/runtime-and-lifecycle.md`
- selector、task_set、task、dispatch 和 route sender：`docs/task-and-dispatch.md`
- pipeline stage：`docs/pipeline.md`
- receiver/sender 协议行为：`docs/receivers-and-senders.md`
- task 执行模型：`docs/execution-model.md`
- 日志、统计、pprof 与排障观测：`docs/observability.md`

## 热路径原则

热路径只保留必要动作：

1. receiver 生成或复用 match key。
2. selector 做一次 map lookup。
3. runtime fan-out 到命中的 task slice。
4. task 执行 pipeline 并发送。

配置解析、默认值、校验、stage 编译、selector 展开、receiver/sender 构建都应停留在冷路径。

## 扩展点

- 新协议：实现 receiver/sender 接口并在 runtime 构建入口注册。
- 新 stage：在 pipeline compiler 增加编译分支。
- 新执行模型：扩展 task 执行入口，并同步更新 `docs/execution-model.md`。
- 新观测能力：在 `logx` 增加聚合器或导出器，并同步更新 `docs/observability.md`。
