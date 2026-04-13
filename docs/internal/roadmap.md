# 后续演进记录

本文不是当前实现的权威说明，只记录可后续评估的演进方向。当前行为以 README 和各主题文档为准。

## 1. 运行时与可靠性

- 为 route sender miss、sender latency、receiver drop 等路径补充结构化指标。
- 为配置热重载增加变更影响分析输出，提前展示会重建的 receiver/sender/task/pipeline。
- 在更多协议组合下补充 reload 失败回滚测试。

## 2. 配置与工具

- 生成 machine-readable 配置 schema，用于 CI 和配置发布前校验。
- 提供 example config 最小化检查工具，避免示例字段与 `src/config` 漂移。
- 补充控制面 API 契约文档。

## 3. 可观测性

- 评估 OpenTelemetry metrics/trace 导出。
- 统一 traffic stats、runtime stats、协议错误计数的命名规范。
- 为文件链路提交成功但通知失败的场景设计持久化 outbox/补发机制。

## 4. 性能与容量

- 扩展 benchmark 场景，覆盖更多 receiver/sender 组合、执行模型和 reload 规模。
- 增加千级 task/task_set/selector 配置下的增量更新 benchmark。
- 为文件分块链路增加大文件、乱序、重复 chunk 的容量压测基线。
