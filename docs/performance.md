# Performance

## 1. 代码中可识别的性能设计点

1. **gnet 网络模型**：UDP/TCP receiver/sender 依赖 `github.com/panjf2000/gnet/v2`。
2. **任务并发池**：`github.com/panjf2000/ants/v2` 支持 pool 模型与有界阻塞。
3. **Kafka 高性能客户端**：`github.com/twmb/franz-go`。
4. **payload 复用**：`src/packet/pool.go` 提供 payload 内存池。
5. **dispatch 原子快照**：`runtime.Store.dispatchSubs` 使用 `atomic.Value` 做只读映射。
6. **多订阅 clone 策略**：单订阅复用原包，多订阅 clone 降低不必要复制。
7. **stage 编译缓存**：`stageCache` 按 signature 复用 `StageFunc`。
8. **有界队列回压**：pool/channel 都有边界，防止无限堆积。

## 2. 三种执行模型对性能影响

- `fastpath`：最短路径，适合极低延迟，但对慢下游敏感。
- `pool`：吞吐稳健，适合大多数生产场景。
- `channel`：顺序明确，但吞吐上限受单 worker 约束。

## 3. 关键依赖在性能中的角色

- `gnet`：减少连接级 goroutine 扩张和调度开销。
- `ants`：控制并发上限、减少 goroutine 爆炸。
- `franz-go`：Kafka 场景可通过 batch/compression/acks 调优。

## 4. bench 工具定位（cmd/bench）

- 用于本地 UDP/TCP 转发链路压力测试。
- 支持 sweep 参数对比（PPS、payload-size、workers、execution_model 等）。
- 支持 order 校验开关（`-validate-order`）。

常用入口：

```bash
make perf
# 或
go run ./cmd/bench -config ./configs/bench.example.json
```

## 5. 推荐压测方法

1. 固定场景基线（协议、payload、workers、duration）。
2. 单变量 sweep（一次只调一个参数）。
3. 记录无丢包最大 PPS 与 Mbps。
4. 同步采集 CPU/内存/pprof，避免只看吞吐不看资源成本。

## 6. 推荐观测指标

- 吞吐：recv/send PPS、Mbps、loss rate。
- 延迟：端到端和尾延迟（待确认：仓库暂无统一延迟度量模块）。
- 资源：CPU、RSS、GC pause、goroutine。
- 稳定性：sender error、队列满丢包告警。

## 7. 已知瓶颈与优化方向

- `channel` 模型单协程上限明显。
- 过高日志级别或 payload 打印会显著影响吞吐。
- sender 下游慢会放大回压，需要结合 queue/concurrency 调优。
- 大 payload / 多 fan-out 场景下 clone 成本会增加。

## 8. 历史实验资料

- `docs/perf_extreme_sweep_2026-03-10.md`
- `docs/perf_extreme_sweep_2026-03-10_72.md`
- 对应 raw json 文件可用于二次分析。
