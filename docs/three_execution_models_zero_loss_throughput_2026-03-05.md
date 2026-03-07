# 三种执行模型 0 丢包吞吐测试说明（更新版）

> 本文保留 2026-03-05 的目标：比较 `fastpath / pool / channel` 在 `loss_rate == 0` 下的吞吐上限；并补充新的复测建议。

## 1. 测试原则

- 统一环境、统一 payload、统一 workers。
- 使用 sweep 逐步加压，记录每档的 `loss_rate` 与 `mbps`。
- 以 `loss_rate == 0` 前提下的最大 `mbps` 作为核心对比指标。

## 2. 推荐命令模板

```bash
# fastpath
go run ./cmd/bench -mode udp -duration 3s -warmup 1s -payload-size 512 \
  -workers 2 -pps-sweep 20000,40000,60000,80000,100000,120000 \
  -task-execution-model fastpath -log-level info -traffic-stats-interval 1h

# pool
go run ./cmd/bench -mode udp -duration 3s -warmup 1s -payload-size 512 \
  -workers 2 -pps-sweep 20000,40000,60000,80000,100000,120000 \
  -task-execution-model pool -task-pool-size 2048 -log-level info -traffic-stats-interval 1h

# channel
go run ./cmd/bench -mode udp -duration 3s -warmup 1s -payload-size 512 \
  -workers 2 -pps-sweep 20000,40000,60000,80000,100000,120000 \
  -task-execution-model channel -log-level info -traffic-stats-interval 1h
```

## 3. 最新复测结果（2026-03-07，duration=12s）

| 模型 | 0 丢包最大吞吐 (Mbps) | 对应 pps |
|---|---:|---:|
| fastpath | 121.37 | 29632.34 |
| pool | 134.67 | 32879.48 |
| channel | 159.64 | 38974.43 |

## 4. pool 模式 0 丢包极限吞吐补测（duration=12~15s）

额外高压 sweep：`82000,86000,90000,94000`

| 档位 | pps(实测) | mbps | loss_rate |
|---:|---:|---:|---:|
| 82000 | 54365.03 | 222.68 | 0 |
| 86000 | 55486.52 | 227.27 | 0 |

**pool 0 丢包极限吞吐（本轮）= 227.27 Mbps**

## 5. 本轮测试关键点

- 参数：`pps-sweep=20000,40000,60000,80000,100000,120000`、`payload=512B`、`workers=2`、`duration=12s`。
- 非 0 丢包区间：
	- fastpath 在 100000/120000 档出现丢包（loss>0）。
	- pool 在 80000/100000/120000 档出现丢包（loss>0）。
	- channel 在 120000 档出现丢包（loss>0）。

本次结果可作为“当前环境”的 0 丢包上限参考，但跨环境不可直接横向对比。

## 6. 严格吞吐评估建议

- 固定 CPU 频率，关闭节能策略。
- 使用 `taskset`/`cset` 做核隔离。
- 至少 5 轮，取中位值。
- 同步记录 p99 延迟与 GC pause，避免只看吞吐。

## 7. 风险提示

- 上游发包工具本身可能成为瓶颈。
- 当 sender 下游不稳定时，执行模型排序可能变化。
- 在 payload 很大时，内存带宽与 cache 命中会主导结果。

---

## 附：按转发协议拆分的 0 丢包极限吞吐（本轮复测）

> 口径说明：
> - UDP/TCP：`cmd/bench` 端到端，按 `loss_rate==0` 选最大吞吐；
> - Kafka/SFTP：`BenchmarkDispatchMatrix` 协议转发基准（同进程模拟），基准模型不统计丢包，视为无丢包处理上限。

| 转发类型 | 关键命令 | 极限吞吐 |
|---|---|---:|
| UDP | `go run ./cmd/bench -mode udp -duration 12s ... -pps-sweep 20000..140000` | **44.30 Mbps** |
| TCP | `go run ./cmd/bench -mode tcp -duration 12s ... -pps-sweep 20000..500000` | **289.59 Mbps** |
| Kafka | `go test ./src/runtime -run '^$' -bench 'BenchmarkDispatchMatrix/...kafka_to_kafka_4096B' -benchmem -benchtime=10s` | **1438.71 MB/s** |
| SFTP | `go test ./src/runtime -run '^$' -bench 'BenchmarkDispatchMatrix/...sftp_to_sftp_4096B' -benchmem -benchtime=10s` | **1528.65 MB/s** |
