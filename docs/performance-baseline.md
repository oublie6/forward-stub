# Performance Baseline (Latest)

## 1. 本轮基线目标

本轮基线用于在**当前仓库最新代码**下建立可复用、可追溯、可横向对比的性能参考，重点覆盖：

1. `cmd/bench` 在 UDP/TCP 模式下的端到端本地闭环表现。
2. 三种执行模型 `fastpath` / `pool` / `channel` 的对比。
3. payload、workers 变化带来的吞吐和丢包趋势。
4. 微基准（runtime/task）用于补充 bench 未覆盖的维度。

## 2. 版本、时间与环境

- 测试时间（UTC）：见 `test-results/performance/2026-03-14-baseline/environment.txt`
- 代码版本：见 `git_commit` 字段
- Go 版本：见 `go_version` 字段
- OS / CPU：见 `os`、`cpu_count` 字段

> 环境信息按文件落盘，避免文档手工抄写出错。

## 3. 测试能力盘点与覆盖边界

### 3.1 当前真实支持并已覆盖

- 执行模型：`fastpath`、`pool`、`channel`
- 协议模式：`udp`、`tcp`
- payload 档位：`256`、`1024`、`4096`
- workers 档位：`2`、`4`
- 统计指标：`sent/recv packets`、`loss_rate`、`pps`、`mbps`、`order_errors`

### 3.2 bench 当前未直接覆盖

- Kafka/SFTP 外部依赖链路性能
- 复杂 pipeline stage 组合对吞吐的影响（bench 默认空 pipeline）
- 跨主机网络、容器网络、磁盘 IO 等环境因素

这些未覆盖项在本轮被明确标注，不混入基线结论。

## 4. 测试矩阵设计

## 4.1 主矩阵（端到端 bench）

- 维度：`proto(2) * execution_model(3) * payload(3) * workers(2)`
- 场景总数：`36`
- 统一参数：`duration=2s`、`warmup=1s`、`pps-per-worker=0`（不限速）

设计理由：

- 模型维度必须完整覆盖。
- payload 采用小/中/大三档观察趋势。
- workers 采用两档观察并发扩展性。
- 不限速场景用于观察峰值与拥塞形态。

## 4.2 补充矩阵（零丢包扫频）

- 协议：UDP 与 TCP
- 模型：三种 execution_model
- 参数：`pps-sweep=2000,4000,8000,12000`、`payload=udp:512 tcp:1024`、`workers=2`

设计理由：

- 主矩阵中 UDP 在不限速下丢包高，需要补一组限速档位确认“可稳定区间”。
- 该矩阵用于给出“可对比的零丢包基线”。

## 4.3 微基准补充

- `go test ./src/runtime -bench BenchmarkDispatchMatrix -benchmem -benchtime=1s`
- `go test ./src/task -bench BenchmarkTaskExecutionModels|BenchmarkTaskRouteSenderLookup -benchmem -benchtime=1s`

设计理由：

- 端到端 bench 负责链路级结果。
- 微基准负责 dispatch/task 局部路径的稳定趋势参考。

## 5. 执行命令与原始结果位置

### 5.1 端到端矩阵

- 命令清单：`test-results/performance/2026-03-14-baseline/commands.json`
- 原始日志：`test-results/performance/2026-03-14-baseline/raw/*.log`
- 结构化结果：
  - `results.json`
  - `results.csv`
  - `matrix-results.md`
  - `model-summary.md`

### 5.2 零丢包扫频

- 结构化结果：
  - `sweep_results.json`
  - `sweep_summary.json`
  - `sweep-results.md`
  - `sweep-summary.md`

### 5.3 微基准

- `raw/runtime_dispatch_benchmark.txt`
- `raw/task_model_route_benchmark.txt`

## 6. 关键结果汇总

### 6.1 主矩阵模型汇总

| proto | model | avg_pps | max_pps | avg_mbps | max_mbps | avg_loss_rate |
|---|---|---:|---:|---:|---:|---:|
| udp | fastpath | 8036.57 | 17279.87 | 57.73 | 109.22 | 0.988471 |
| udp | pool | 7124.12 | 14080.77 | 53.73 | 93.14 | 0.989533 |
| udp | channel | 7066.13 | 14573.10 | 52.98 | 110.88 | 0.989284 |
| tcp | fastpath | 191077.44 | 346404.23 | 1615.79 | 3680.80 | 0.098081 |
| tcp | pool | 167896.76 | 239779.88 | 1716.47 | 3393.13 | 0.000000 |
| tcp | channel | 221066.01 | 327068.21 | 1937.35 | 3767.52 | 0.000000 |

### 6.2 零丢包扫频汇总

| proto | model | max_zero_loss_pps | max_zero_loss_mbps |
|---|---|---:|---:|
| udp | fastpath | 16007.47 | 65.57 |
| udp | pool | 16009.06 | 65.57 |
| udp | channel | 24017.50 | 98.38 |
| tcp | fastpath | 24003.88 | 196.64 |
| tcp | pool | 24033.56 | 196.88 |
| tcp | channel | 24004.61 | 196.65 |

### 6.3 微基准要点

- `BenchmarkDispatchMatrix` 在 4096B 场景普遍达到约 1.2~1.5 GB/s 级别（不同 proto 组合有差异）。
- `BenchmarkTaskExecutionModels` 在该环境下，`fastpath` 与 `single_goroutine_channel` 单测开销接近，`pool_size_1` 更高。

详见原始 benchmark 输出文件。

## 7. 主要发现

1. **UDP 不限速场景丢包显著**：主矩阵中 UDP 丢包率普遍较高，说明该设置更适合“峰值压力探测”，不适合作为容量承诺。
2. **TCP 在本机闭环下稳定性更高**：多数 TCP 场景零丢包，且在大 payload 下 Mbps 更高。
3. **零丢包区间可通过 sweep 获取**：对 UDP/TCP 引入 pps 档位后，可得到可比较的零丢包基线。
4. **模型差异需要分场景解读**：不同协议、payload、workers 下的模型排名并非固定。

## 8. 结果解读边界

可以得出的结论：

- 同一环境、同参数下，不同模型与参数的相对趋势。
- 代码改动前后的相对退化或提升。

不能草率得出的结论：

- 直接把本地 bench 数值当作生产容量。
- 用空 pipeline 结果代表复杂 stage 场景。
- 用 UDP 不限速丢包结果推断系统不可用。

## 9. 本轮未覆盖场景与原因

1. Kafka/SFTP 外部链路：bench 当前不直接提供这些端到端压测路径。
2. pipeline 复杂度分层：bench 当前固定空 pipeline，未提供 stage 组合参数化入口。
3. 跨主机/容器网络影响：本轮为本机基线重测，不包含外部网络变量。

## 10. 后续基线建议

1. 继续保留本轮 `36 + sweep + microbench` 组合作为标准基线模板。
2. 如需覆盖 pipeline 复杂度，建议在 bench 增加可选 stage 注入能力（小改动优先）。
3. 增加固定轮次重复执行并输出均值/中位数，降低偶然抖动影响。
4. 后续版本对比时，优先对比：
   - `model-summary.md`
   - `sweep-summary.md`
   - `runtime_dispatch_benchmark.txt`

## 11. 关联文档

- `docs/bench.md`
- `docs/performance.md`
- `docs/operations.md`
- `docs/observability.md`
