# 转发协议性能测试过程与结论（2026-03-09，当前版本复测）

## 1. 测试目标

按要求复测以下两类场景（统一关闭 payload log）：

1. 严格 `0` 丢包 + 严格保序最大吞吐：
   - 1.1 receiver eventloop `multicore=false`
     - 1.1.1 `channel`
     - 1.1.2 `pool_size=1`
     - 1.1.3 `fastpath=true`
   - 1.2 receiver eventloop `multicore=true`
     - 1.2.1 `channel`
     - 1.2.2 `pool_size=1`
     - 1.2.3 `fastpath=true`
2. 严格 `0` 丢包 + 不保序最大吞吐：
   - 2.1 UDP→UDP
   - 2.2 TCP→TCP
   - 2.3 Kafka→Kafka（模拟）
   - 2.4 SFTP→SFTP（模拟）

## 2. 测试环境与统一约束

- 测试时间：2026-03-09（当前分支最新代码）
- 统一关闭 payload log（`cmd/bench` 生成配置中固定 `PayloadLogRecv=false`, `PayloadLogSend=false`）
- 严格口径判定：`loss_rate == 0 && strict_order_ok == true`
- 不保序口径判定：只看 `loss_rate == 0` 下的吞吐上限
- 原始输出目录：`docs/new_artifacts/2026-03-09-rerun/`

## 3. 执行命令

### 3.1 严格 0 丢包 + 严格保序（1.1 / 1.2）

公共参数：
- `duration=4s`, `warmup=1s`, `payload-size=1024`, `workers=1`
- `pps-sweep=200,500,1000,2000,4000`
- `mode=both`, `receiver-event-loops=1`, `tcp-sender-concurrency=1`
- `validate-order=true`, `log-level=info`

执行：
- `go run ./cmd/bench ... -multicore=false -task-execution-model channel -task-fast-path=false -task-pool-size 1 -task-queue-size 1 > strict_mc_off_channel.txt`
- `go run ./cmd/bench ... -multicore=false -task-execution-model pool -task-fast-path=false -task-pool-size 1 -task-queue-size 1 > strict_mc_off_pool1.txt`
- `go run ./cmd/bench ... -multicore=false -task-execution-model fastpath -task-fast-path=true -task-pool-size 1 -task-queue-size 1 > strict_mc_off_fastpath.txt`
- `go run ./cmd/bench ... -multicore=true  -task-execution-model channel -task-fast-path=false -task-pool-size 1 -task-queue-size 1 > strict_mc_on_channel.txt`
- `go run ./cmd/bench ... -multicore=true  -task-execution-model pool -task-fast-path=false -task-pool-size 1 -task-queue-size 1 > strict_mc_on_pool1.txt`
- `go run ./cmd/bench ... -multicore=true  -task-execution-model fastpath -task-fast-path=true -task-pool-size 1 -task-queue-size 1 > strict_mc_on_fastpath.txt`

### 3.2 严格 0 丢包 + 不保序（2.1~2.4）

- UDP→UDP 初始大范围扫描（观察上限与丢包拐点）：
  - `go run ./cmd/bench -mode udp -duration 8s -warmup 2s -payload-size 4096 -workers 4 -pps-sweep 2000,...,18000 -multicore=true -task-execution-model pool -task-fast-path=false -task-pool-size 8192 -task-queue-size 16384 -receiver-event-loops 4 -validate-order=false -log-level info > unordered_udp_max.txt`
- UDP→UDP 零丢包搜索：
  - `go run ./cmd/bench -mode udp -duration 8s -warmup 2s -payload-size 4096 -workers 2 -pps-sweep 500,1000,1500,2000,2500,3000 -multicore=true -task-execution-model pool -task-fast-path=false -task-pool-size 8192 -task-queue-size 16384 -receiver-event-loops 4 -validate-order=false -log-level info > unordered_udp_zero_loss_search.txt`
- TCP→TCP：
  - `go run ./cmd/bench -mode tcp -duration 8s -warmup 2s -payload-size 4096 -workers 4 -pps-sweep 4000,8000,12000,16000,20000,24000,28000,32000 -multicore=true -task-execution-model pool -task-fast-path=false -task-pool-size 8192 -task-queue-size 16384 -receiver-event-loops 4 -tcp-sender-concurrency 8 -validate-order=false -log-level info > unordered_tcp_max.txt`
- Kafka→Kafka（模拟）：
  - `go test ./src/runtime -run '^$' -bench 'BenchmarkDispatchMatrix/kafka_to_kafka_4096B' -benchmem -benchtime=12s > unordered_kafka_sim.txt`
- SFTP→SFTP（模拟）：
  - `go test ./src/runtime -run '^$' -bench 'BenchmarkDispatchMatrix/sftp_to_sftp_4096B' -benchmem -benchtime=12s > unordered_sftp_sim.txt`

## 4. 结果汇总

### 4.1 严格 0 丢包 + 严格保序最大吞吐（Mbps）

| 场景 | UDP | TCP |
|---|---:|---:|
| 1.1.1 channel + multicore=off | 32.79 | 16.39 |
| 1.1.2 pool_size=1 + multicore=off | 16.39 | 32.78 |
| 1.1.3 fastpath=true + multicore=off | 32.77 | 32.77 |
| 1.2.1 channel + multicore=on | 32.79 | 16.39 |
| 1.2.2 pool_size=1 + multicore=on | 32.77 | 16.46 |
| 1.2.3 fastpath=true + multicore=on | 32.78 | 32.79 |

### 4.2 严格 0 丢包 + 不保序最大吞吐

| 场景 | 最大吞吐 |
|---|---:|
| 2.1 UDP→UDP（端到端） | 98.29 Mbps |
| 2.2 TCP→TCP（端到端） | 2094.91 Mbps |
| 2.3 Kafka→Kafka（模拟） | 990.19 MB/s |
| 2.4 SFTP→SFTP（模拟） | 972.23 MB/s |

> 注：Kafka/SFTP 为同进程模拟基准，反映框架内处理能力，不等价于真实外部系统端到端吞吐。

## 5. 结论

1. 严格保序口径下，`fastpath=true` 组合在 UDP/TCP 上最稳定（两者都保持在 ~32.7Mbps）。
2. `channel` 与 `pool_size=1` 在本轮 TCP 严格保序口径下表现出明显分化（分别在不同 multicore 组合下掉到 ~16Mbps），说明当前顺序校验口径与执行模型/调度耦合仍明显。
3. 不保序 + 0 丢包口径下，TCP 仍显著高于 UDP（约 `2.09Gbps` vs `98Mbps`）。
4. 与上一轮历史数据相比，本轮 UDP 0 丢包上限明显降低；建议下一步优先针对 UDP 路径做专项排查（socket buffer、sender 并发策略、bench 发送节流与 sink 读取配比）。

## 6. 产物清单

- 解析汇总：`docs/new_artifacts/2026-03-09-rerun/parsed_results.json`
- 严格口径原始日志：
  - `strict_mc_off_channel.txt`
  - `strict_mc_off_pool1.txt`
  - `strict_mc_off_fastpath.txt`
  - `strict_mc_on_channel.txt`
  - `strict_mc_on_pool1.txt`
  - `strict_mc_on_fastpath.txt`
- 不保序原始日志：
  - `unordered_udp_max.txt`
  - `unordered_udp_zero_loss_search.txt`
  - `unordered_tcp_max.txt`
  - `unordered_kafka_sim.txt`
  - `unordered_sftp_sim.txt`
