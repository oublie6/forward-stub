# Observability

## 1. 可观测性入口总览

当前仓库中可用的观测入口：

- `src/logx` 结构化日志。
- 流量聚合统计日志。
- payload 摘要日志。
- pprof HTTP 端点。
- `cmd/bench` 压测输出。

## 2. 日志

### 作用

- 记录启动、配置加载、更新、错误、停止事件。
- 记录 sender/receiver 异常。
- 记录 task 队列满导致的丢包。

### 配置点

- `logging.level`
- `logging.file`
- `max_size_mb/max_backups/max_age_days/compress`

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

## 6. bench

bench 提供可重复性能观测：

- 无丢包吞吐区间。
- 执行模型对比。
- payload 大小、workers、队列参数对比。

建议把 bench 结果与运行日志一起归档，便于回归对比。参数规范与结果解读详见 `docs/bench.md`。

## 7. 关键观测指标建议

- 入站速率与出站速率差值。
- task 丢包告警频次。
- sender 错误率。
- CPU 利用率、内存占用、GC 抖动。

## 8. 如何判断拥塞和回压

常见信号：

- `pool queue full` 或 channel 入队失败日志。
- 入站持续高但出站下降。
- CPU 明显偏高且 sender 错误增加。

## 9. 日志指标联动定位建议

1. 先看流量统计判断是全局下降还是局部链路下降。
2. 再看 error 日志定位 receiver 或 sender。
3. 若资源异常，抓取 pprof 进一步分析热点和内存分布。

## 10. 待补充项

- 待确认：项目是否规划统一指标导出协议与标准仪表盘模板。
