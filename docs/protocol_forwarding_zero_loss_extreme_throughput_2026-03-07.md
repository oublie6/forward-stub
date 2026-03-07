# 全协议转发性能优化分析与 0 丢包极限吞吐测试（2026-03-07）

## 1. 目标与口径

本轮针对四类转发链路分别做“极限吞吐”分析与优化：

- UDP 转发
- TCP 转发
- Kafka 转发
- SFTP 转发

统一口径：

- UDP/TCP：使用 `cmd/bench` 端到端链路，按 `loss_rate == 0` 选最大 `mbps`。
- Kafka/SFTP：使用 `BenchmarkDispatchMatrix` 的同进程转发基准（不统计丢包，视为无丢包上限）。
- 测试不要求保序，允许任意 event loop / pool size / 并发参数组合。

---

## 2. 代码层性能改造

### 2.1 dispatch 多订阅路径减少一次 Clone

在 `dispatch` 多任务 fan-out 场景下，将“原始包”直接给第一个任务，其余任务使用副本，减少一次不必要的 Clone 与一次额外对象生命周期开销。

预期收益：

- 多订阅场景下降低复制与分配压力。
- 高 PPS 条件下减少内存带宽与 GC 压力。

### 2.2 bench 增强可调参数（用于极限吞吐搜索）

为 `cmd/bench` 新增以下可调能力（CLI + bench-config 文件）：

- `receiver-event-loops`：接收端 gnet event-loop 数
- `receiver-read-buffer-cap`：接收端读缓冲上限
- `tcp-sender-concurrency`：TCP sender 内部连接并发
- `base-port`：基准端口（避免并发测试端口冲突）

通过这些参数，可以在同一环境做更大搜索空间的“吞吐上限扫描”。

---

## 3. 测试命令（本轮关键命令）

### 3.1 UDP 0 丢包扫描（pool）

```bash
go run ./cmd/bench -mode udp -base-port 29200 -duration 6s -warmup 1s -payload-size 512 \
  -workers 4 -pps-sweep 5000,10000,15000,20000,25000,30000 \
  -task-execution-model pool -task-pool-size 8192 \
  -receiver-event-loops 8 -udp-sink-readers 8 -udp-sink-read-buf 33554432 \
  -log-level info -traffic-stats-interval 1h
```

### 3.2 TCP 0 丢包扫描（pool）

```bash
go run ./cmd/bench -mode tcp -base-port 29300 -duration 6s -warmup 1s -payload-size 512 \
  -workers 4 -pps-sweep 5000,10000,20000,40000,60000,80000,100000 \
  -task-execution-model pool -task-pool-size 8192 \
  -receiver-event-loops 8 -tcp-sender-concurrency 16 \
  -log-level info -traffic-stats-interval 1h
```

### 3.3 Kafka / SFTP 同进程转发上限基准

```bash
go test ./src/runtime -run '^$' \
  -bench 'BenchmarkDispatchMatrix/(kafka_to_kafka_4096B|sftp_to_sftp_4096B)' \
  -benchmem -benchtime=10s
```

---

## 4. 结果汇总（0 丢包极限吞吐）

| 转发类型 | 0 丢包极限吞吐 | 对应速率/说明 | 关键配置 |
|---|---:|---:|---|
| UDP | **87.23 Mbps** | 21295.34 pps | pool / pool_size=8192 / event_loops=8 |
| TCP | **274.19 Mbps** | 66941.15 pps | pool / pool_size=8192 / event_loops=8 / tcp_sender_concurrency=16 |
| Kafka | **945.51 MB/s** | `kafka_to_kafka_4096B` | `BenchmarkDispatchMatrix` |
| SFTP | **919.02 MB/s** | `sftp_to_sftp_4096B` | `BenchmarkDispatchMatrix` |

> 说明：UDP 在更高压档位可达到更高 Mbps，但会出现非 0 丢包，因此不纳入“0 丢包极限吞吐”口径。

---

## 5. 分协议分析与优化建议

### 5.1 UDP

现象：

- UDP 在高压下更容易出现 `loss_rate > 0`，受发包端节流精度、接收 socket 缓冲、用户态调度共同影响。

建议：

1. 优先固定 `pool` 模型并增大 `task_pool_size`。
2. 增大 `udp-sink-read-buf` 与 `receiver-read-buffer-cap`。
3. 采用独立核绑核（sender/receiver 分离）继续提升 0 丢包上限。

### 5.2 TCP

现象：

- TCP 在当前环境更容易维持 0 丢包并获取更高稳定吞吐。

建议：

1. 将 `tcp-sender-concurrency` 作为主调参项。
2. 根据 CPU 拓扑调整 `receiver-event-loops`（建议与物理核匹配）。

### 5.3 Kafka

现象：

- 同进程转发基准显示 Kafka 链路在 4KB payload 下可达约 945 MB/s。

建议：

1. 生产环境继续联调 `linger_ms` / `batch_max_bytes` / `acks`。
2. 如果关注稳定 0 丢包，应配合 broker 端 ISR 与磁盘指标做联合压测。

### 5.4 SFTP

现象：

- 同进程基准约 919 MB/s，略低于 Kafka，主要受 chunk 与文件语义处理路径影响。

建议：

1. 优化 chunk size 与并发上传策略。
2. 在真实远端环境联调 SSH 窗口、远端存储带宽与 fsync 策略。

---

## 6. 结论

- 本轮已完成“按 UDP/TCP/Kafka/SFTP 分协议”的优化与极限吞吐复测。
- 代码层优化已落地（dispatch clone 降低 + bench 全面参数化）。
- 现阶段 0 丢包上限：**UDP 87.23 Mbps、TCP 274.19 Mbps、Kafka 945.51 MB/s、SFTP 919.02 MB/s**。
- 若继续追求更高 0 丢包吞吐，建议下一步引入 CPU 绑核、独立发包器、网卡队列亲和与内核网络参数调优。
