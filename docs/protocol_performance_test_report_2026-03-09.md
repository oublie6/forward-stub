# 转发协议性能测试过程与结论（2026-03-09）

## 1. 变更前清理

已删除 README 与 docs 目录中历史性能测试内容：

- 删除旧性能报告：
  - `docs/performance_audit_and_extreme_throughput_2026-03-08.md`
  - `docs/performance_code_review_and_protocol_bench_2026-03-08.md`
  - `docs/protocol_forwarding_zero_loss_extreme_throughput_2026-03-07.md`
  - `docs/task_execution_model_experiment.md`
  - `docs/three_execution_models_zero_loss_throughput_2026-03-05.md`
- 删除旧压测产物目录：`docs/artifacts/`

## 2. 测试约束

- 全部测试关闭 payload log（bench 生成配置中 `PayloadLogRecv=false`, `PayloadLogSend=false`）。
- 严格口径测试：`loss_rate==0` 且 `strict_order_ok==true`。
- “不保序”口径测试：仅比较吞吐上限。

## 3. 测试命令与结果

## 3.1 严格 0 丢包 + 严格保序最大吞吐（1.1 / 1.2）

公共参数：
- `duration=4s`, `warmup=1s`, `payload-size=1024`, `workers=1`
- `pps-sweep=200,500,1000,2000,4000`
- `mode=both(UDP+TCP)`
- `tcp-sender-concurrency=1`
- `validate-order=true`

### 1.1 receiver eventloop 关闭 multicore

1.1.1 channel
- 原始输出：`docs/new_artifacts/strict_mc_off_channel.txt`

1.1.2 pool_size=1
- 原始输出：`docs/new_artifacts/strict_mc_off_pool1.txt`

1.1.3 fastpath=true
- 原始输出：`docs/new_artifacts/strict_mc_off_fastpath.txt`

### 1.2 receiver eventloop 打开 multicore

1.2.1 channel
- 原始输出：`docs/new_artifacts/strict_mc_on_channel.txt`

1.2.2 pool_size=1
- 原始输出：`docs/new_artifacts/strict_mc_on_pool1.txt`

1.2.3 fastpath=true
- 原始输出：`docs/new_artifacts/strict_mc_on_fastpath.txt`

### 严格口径汇总（最大值）

| 场景 | UDP 最大吞吐 (Mbps) | TCP 最大吞吐 (Mbps) |
|---|---:|---:|
| 1.1.1 channel + multicore=off | 32.78 | 16.39 |
| 1.1.2 pool_size=1 + multicore=off | 8.20 | 32.77 |
| 1.1.3 fastpath=true + multicore=off | 32.78 | 16.39 |
| 1.2.1 channel + multicore=on | 4.10 | 32.77 |
| 1.2.2 pool_size=1 + multicore=on | 16.39 | 16.39 |
| 1.2.3 fastpath=true + multicore=on | 16.39 | 16.38 |

---

## 3.2 严格 0 丢包 + 不保序最大吞吐（2.1~2.4）

### 2.1 UDP 收 UDP 发最大吞吐

命令：
- `go run ./cmd/bench -mode udp ... -pps-sweep 2000..18000 -task-execution-model pool -task-fast-path=false`

结果文件：
- `docs/new_artifacts/unordered_udp_max.txt`

最大吞吐：
- **883.79 Mbps**

### 2.2 TCP 收 TCP 发最大吞吐

命令：
- `go run ./cmd/bench -mode tcp ... -pps-sweep 4000..32000 -task-execution-model pool -task-fast-path=false -tcp-sender-concurrency=8`

结果文件：
- `docs/new_artifacts/unordered_tcp_max.txt`

最大吞吐：
- **3196.49 Mbps**

### 2.3 Kafka 收 Kafka 发最大吞吐（模拟）

命令：
- `go test ./src/runtime -run '^$' -bench 'BenchmarkDispatchMatrix/kafka_to_kafka_4096B' -benchmem -benchtime=12s`

结果文件：
- `docs/new_artifacts/unordered_kafka_sftp_sim.txt`

最大吞吐：
- **1471.27 MB/s**

### 2.4 SFTP 收 SFTP 发最大吞吐（模拟）

命令：
- `go test ./src/runtime -run '^$' -bench 'BenchmarkDispatchMatrix/sftp_to_sftp_4096B' -benchmem -benchtime=12s`

结果文件：
- `docs/new_artifacts/unordered_kafka_sftp_sim.txt`

最大吞吐：
- **1511.47 MB/s**

## 4. 结论

1. 严格 0 丢包+严格保序场景下，UDP/TCP 均可达成，但最大吞吐显著低于“不保序”口径。 
2. 不保序吞吐上限下，TCP 端到端能力明显高于 UDP（本机环境约 3.20 Gbps vs 0.88 Gbps）。
3. Kafka/SFTP 当前使用仓库内模拟链路基准，反映框架处理上限，不代表真实外部系统端到端吞吐。
