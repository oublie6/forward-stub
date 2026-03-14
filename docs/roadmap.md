# Roadmap

## 1. 当前架构局限

1. 观测体系以日志与 pprof 为主，缺少统一指标导出面。
2. 执行模型参数调优仍依赖人工经验与 bench 结果。
3. pipeline 当前偏轻量 stage，复杂场景扩展规范尚可加强。
4. system/business 双配置虽清晰，但 K8s 清单仍默认 legacy 单文件示例。

## 2. 可演进方向

### 2.1 运行时与调度

- 增加 task 级实时队列/丢包指标。
- 增加自适应执行模型建议（基于 runtime 观测数据）。
- 对 route stage 增加命中统计和 fallback 统计。

### 2.2 协议能力

- Kafka/SFTP 增强错误分类与重试策略可视化。
- 补充更多 framing / 编解码 stage（按真实需求演进）。

### 2.3 部署与运维

- 补齐双配置模式的 K8s 官方示例。
- 增加 Helm 或 Kustomize overlay 示例（dev/staging/prod）。

## 3. 文档待补项

1. `control.api` 服务端协议契约与鉴权示例。
2. SFTP receiver 文件消费幂等策略说明。
3. 延迟指标定义与标准压测报告模板。

## 4. 建议补充的代码注释/设计说明

- `runtime.applyBusinessDelta` 的策略矩阵（何时复用、何时重建）。
- 各 sender 的并发模型和连接复用语义说明。
- packet clone/release 生命周期最佳实践（给扩展开发者）。

## 5. 文档维护建议

1. README 保持入口化：只保留概览、Quick Start、导航。
2. 详细设计和操作流程统一下沉 `docs/`。
3. 新增协议或 stage 时，必须同步更新：
   - `docs/receivers-and-senders.md`
   - `docs/pipeline.md`
   - `docs/configuration.md`
4. 每次性能调优后，更新 `docs/performance.md` 和 bench 样例参数。
