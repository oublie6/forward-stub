# Observability

> 架构基线：`receiver -> selector -> task -> pipelines -> senders`。receiver 只负责收包，selector 返回 task 集，task 负责串行执行 pipelines 并在末端 fan-out 到 senders。

## 1. 可观测性入口总览

当前仓库中可用的观测入口：

- `src/logx` 结构化日志。
- 启动阶段分步日志。
- 流量聚合统计日志。
- GC 周期统计日志。
- payload 摘要日志。
- pprof HTTP 端点。
- `go test -bench` benchmark 输出。

## 2. 日志

### 作用

- 记录启动、配置加载、更新、错误、停止事件。
- 记录 sender/receiver 异常。
- 记录 task 队列满导致的丢包。
- 记录启动阶段已经完成的关键步骤，便于定位卡点。

### 配置点

- `logging.level`
- `logging.file`
- `max_size_mb/max_backups/max_age_days/compress`
- `logging.gc_stats_enabled`
- `logging.gc_stats_interval`

## 3. 吞吐统计

流量统计由 logx 聚合并按 `traffic_stats_interval` 输出。

可用于快速判断：

- 入站是否持续增长。
- 出站是否低于入站。
- 采样周期内是否有异常突降。

## 4. payload 观测

receiver/task 支持 payload 摘要输出：

- `log_payload_recv`
- `log_payload_send`
- `payload_log_max_bytes`

建议仅在短时排障窗口开启。

## 5. pprof

开启方式：`control.pprof_port > 0`。

常用接口：

- `/debug/pprof/profile`
- `/debug/pprof/heap`
- `/debug/pprof/goroutine`

## 6. GC 周期统计

开启方式：`logging.gc_stats_enabled=true`。

打印周期：`logging.gc_stats_interval`，建议保持分钟级。

当前日志重点字段：

- `num_gc`
- `gc_count_delta`
- `pause_total_ns`
- `pause_total_delta_ns`
- `heap_alloc`
- `heap_inuse`
- `heap_sys`
- `heap_objects`
- `next_gc`
- `gc_cpu_fraction`

该能力默认关闭，关闭时会明确打印 disabled 日志；启用后在独立 goroutine 中按 ticker 读取标准库 `runtime.MemStats`，不会进入收包/转发热路径。

## 7. benchmark

benchmark 提供可重复内部链路性能观测：

- 无丢包吞吐区间。
- 执行模型对比。
- payload 大小、workers、队列参数对比。

建议将 benchmark 命令、commit 与 profile 文件一起归档。参数规范与边界解读详见 `docs/benchmark.md`。

## 8. 关键观测指标建议

- 入站速率与出站速率差值。
- task 丢包告警频次。
- sender 错误率。
- CPU 利用率、内存占用、GC 抖动。

## 9. 如何判断拥塞和回压

常见信号：

- `pool queue full` 或 channel 入队失败日志。
- 入站持续高但出站下降。
- CPU 明显偏高且 sender 错误增加。

## 10. 日志指标联动定位建议

1. 先看流量统计判断是全局下降还是局部链路下降。
2. 再看 error 日志定位 receiver 或 sender。
3. 若资源异常，抓取 pprof 进一步分析热点和内存分布。

## 11. 待补充项

- 待确认：项目是否规划统一指标导出协议与标准仪表盘模板。
