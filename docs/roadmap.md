# Roadmap

## 1. 当前文档待补项

- control API 契约文档仍不完整。
- SFTP 消费幂等策略未形成单独设计文档。
- 运行指标统一标准尚未文档化。

## 2. 当前架构局限

- 观测入口以日志为主，自动化指标体系待增强。
- route stage 能力聚焦固定偏移匹配，表达能力有限。
- 部分运行时策略需要更明确 ADR 记录。

## 3. 可演进方向

### 3.1 运行时

- 增加 task 队列深度和丢包的结构化指标。
- 增加 sender 延迟分布观测能力。

### 3.2 配置系统

- 增加配置变更影响分析工具。
- 增加配置模板校验脚本。

### 3.3 文档体系

- 补充 `docs/benchmark.md` 的场景覆盖（如更多协议组合与执行模型维度）。
- 继续用 `docs/architecture.md`、`docs/technical-architecture.md` 补充关键设计决策记录。

## 4. 建议补充的代码注释与设计说明

- `runtime.applyBusinessDelta` 决策分支说明。
- `task.processAndSend` 在 route sender 场景的行为说明。
- sender 并发参数在不同协议实现中的语义差异说明。

## 5. 文档维护建议

1. README 保持主使用手册和配置总览角色。
2. docs 重点讲机制和维护，不重复粘贴大段 JSON。
3. 每次新增协议或 stage 时同步更新 README 和 docs。
4. 每次性能回归输出统一格式报告到 docs。

## 6. 优先级建议

- 高优先：观测与指标标准化。
- 中优先：运行时增量更新策略文档化。
- 中优先：SFTP 与 Kafka 场景运行说明强化。
