# 当前版本吞吐量复测记录（2026-03-09）

## 1. 测试范围

- 删除旧性能说明后，按以下口径复测：
  - 严格 0 丢包 + 严格保序（6 个场景）
  - 严格 0 丢包 + 不保序（UDP/TCP/Kafka 模拟/SFTP 模拟）
- 全过程关闭 payload log。

## 2. 测试环境与统一参数

- 压测工具：`cmd/bench`
- 统一：`--traffic-stats-interval 1h --log-level info`
- 严格保序场景：`--validate-order=true --payload-size 1024 --duration 4s --warmup 1s --workers 1`
- 不保序场景：`--validate-order=false --task-fast-path=false --payload-size 4096 --duration 8s --warmup 1s`

## 3. 测试命令与结果

### 3.1 严格 0 丢包 + 严格保序

命令模板（按 multicore / task model 组合跑）：

```bash
go run ./cmd/bench --mode both --duration 4s --warmup 1s --payload-size 1024 --workers 1 \
  --pps-sweep 500,1000,2000,4000 --multicore=<true|false> \
  --task-execution-model <channel|pool> --task-pool-size 1 \
  --task-fast-path <true|false> --validate-order=true \
  --traffic-stats-interval 1h --log-level info
```

结果（0 丢包且 strict_order_ok=true 口径）：

| 场景 | UDP 最大吞吐 (Mbps) | TCP 结果 |
|---|---:|---|
| 1.1.1 channel + multicore=off | 32.77 | strict_order_ok 持续为 false |
| 1.1.2 pool_size=1 + multicore=off | 32.78 | strict_order_ok 持续为 false |
| 1.1.3 fastpath=true + multicore=off | 32.78 | strict_order_ok 持续为 false |
| 1.2.1 channel + multicore=on | 32.77 | strict_order_ok 持续为 false |
| 1.2.2 pool + multicore=on | 32.79 | strict_order_ok 持续为 false |
| 1.2.3 fastpath + multicore=on | 复测中断 | 复测中断 |

原始输出：
- `docs/new_artifacts/2026-03-09-rerun2/strict_mc_false_channel.txt`
- `docs/new_artifacts/2026-03-09-rerun2/strict_mc_false_pool1.txt`
- `docs/new_artifacts/2026-03-09-rerun2/strict_mc_false_fastpath.txt`
- `docs/new_artifacts/2026-03-09-rerun2/strict_mc_true_channel.txt`
- `docs/new_artifacts/2026-03-09-rerun2/strict_mc_true_pool1.txt`
- `docs/new_artifacts/2026-03-09-rerun2/strict_mc_true_fastpath.txt`

### 3.2 严格 0 丢包 + 不保序

#### 2.1 UDP 收 UDP 发

```bash
go run ./cmd/bench --mode udp --duration 8s --warmup 1s --payload-size 4096 --workers 1 \
  --pps-sweep 500,1000,1500,2000,2500,3000 --multicore=true \
  --task-fast-path=false --task-execution-model pool --task-pool-size 4096 --task-queue-size 16384 \
  --receiver-event-loops 8 --traffic-stats-interval 1h --log-level info
```

- 最大 0 丢包吞吐：**98.31 Mbps**
- 原始输出：`docs/new_artifacts/2026-03-09-rerun2/unordered_udp_zero_loss_search.txt`

#### 2.2 TCP 收 TCP 发

```bash
go run ./cmd/bench --mode tcp --duration 8s --warmup 1s --payload-size 4096 --workers 4 \
  --pps-sweep 8000,16000,32000,48000,64000 --multicore=true \
  --task-fast-path=false --task-execution-model pool --task-pool-size 4096 --task-queue-size 16384 \
  --receiver-event-loops 8 --tcp-sender-concurrency 16 --traffic-stats-interval 1h --log-level info
```

- 最大 0 丢包吞吐：**4212.25 Mbps**
- 原始输出：`docs/new_artifacts/2026-03-09-rerun2/unordered_tcp.txt`

#### 2.3 Kafka 收 Kafka 发（模拟）
#### 2.4 SFTP 收 SFTP 发

```bash
go test ./src/runtime -run '^$' -bench BenchmarkDispatchMatrix -benchmem -benchtime=12s
```

从 `BenchmarkDispatchMatrix` 结果提取：
- Kafka→Kafka（4096B）：**2035.70 MB/s**
- SFTP→SFTP（4096B）：**2037.44 MB/s**

原始输出：`docs/new_artifacts/2026-03-09-rerun2/dispatch_matrix.txt`

## 4. 结论

1. 在当前版本、payload log 关闭条件下，UDP 严格保序 0 丢包极限维持在约 32.77~32.79 Mbps。
2. TCP 在本轮 `cmd/bench + validate-order` 口径下出现 `order_errors>0`，未满足“严格保序”判定。
3. 不保序口径下，UDP 0 丢包极限约 98.31 Mbps；TCP 0 丢包极限约 4212.25 Mbps。
4. 模拟协议路径下，Kafka→Kafka 与 SFTP→SFTP 均在约 2.0 GB/s 量级。
