# Task 执行模型实验（FastPath / Pool / Channel）

## 1. 实验目的

在同一代码版本下比较 task 三种执行模型的纯调度开销与端到端吞吐差异：

- `fastpath`
- `pool`
- `channel`

## 2. 测试维度

1. 微基准（`src/task`）
   - 聚焦模型本身开销。
2. 端到端压测（`cmd/bench`）
   - 聚焦真实收发链路。

## 3. 延迟微基准命令（本次）

```bash
go test ./src/task -run '^$' -bench BenchmarkTaskExecutionModels -benchmem -benchtime=10s -count=1
```

本次延迟结果：

- `fastpath_sync`: **391.7 ns/op**
- `task_pool_size_1`: **1095 ns/op**
- `single_goroutine_channel`: **546.8 ns/op**

## 4. 端到端复测命令（本次 0 丢包上限复测）

```bash
# fastpath
go run ./cmd/bench -mode udp -duration 12s -warmup 2s -payload-size 512 \
  -workers 2 -pps-sweep 20000,40000,60000,80000,100000,120000 \
  -task-execution-model fastpath -log-level info -traffic-stats-interval 1h

# pool
go run ./cmd/bench -mode udp -duration 12s -warmup 2s -payload-size 512 \
  -workers 2 -pps-sweep 20000,40000,60000,80000,100000,120000 \
  -task-execution-model pool -task-pool-size 2048 -log-level info -traffic-stats-interval 1h

# channel
go run ./cmd/bench -mode udp -duration 12s -warmup 2s -payload-size 512 \
  -workers 2 -pps-sweep 20000,40000,60000,80000,100000,120000 \
  -task-execution-model channel -log-level info -traffic-stats-interval 1h
```

## 5. 本次复测结果（loss_rate=0，duration=12s）

### fastpath

| pps/worker | pps(实测) | mbps | loss_rate |
|---:|---:|---:|---:|
| 20000 | 7571.91 | 31.01 | 0 |
| 40000 | 17608.90 | 72.13 | 0 |
| 60000 | 24608.06 | 100.79 | 0 |
| 80000 | 29632.34 | 121.37 | 0 |

**0 丢包最大吞吐：121.37 Mbps（pps=29632.34）**

### pool

| pps/worker | pps(实测) | mbps | loss_rate |
|---:|---:|---:|---:|
| 20000 | 8711.68 | 35.68 | 0 |
| 40000 | 17112.38 | 70.09 | 0 |
| 60000 | 32879.48 | 134.67 | 0 |

**0 丢包最大吞吐：134.67 Mbps（pps=32879.48）**

### channel

| pps/worker | pps(实测) | mbps | loss_rate |
|---:|---:|---:|---:|
| 20000 | 7886.55 | 32.30 | 0 |
| 40000 | 16942.04 | 69.39 | 0 |
| 60000 | 26246.09 | 107.50 | 0 |
| 80000 | 38080.27 | 155.98 | 0 |
| 100000 | 38974.43 | 159.64 | 0 |

**0 丢包最大吞吐：159.64 Mbps（pps=38974.43）**

## 6. pool 模式 0 丢包极限吞吐补测（duration=12~15s）

额外 sweep：`82000,86000,90000,94000`

| pps/worker | pps(实测) | mbps | loss_rate |
|---:|---:|---:|---:|
| 82000 | 54365.03 | 222.68 | 0 |
| 86000 | 55486.52 | 227.27 | 0 |

**pool 0 丢包极限吞吐（本轮）：227.27 Mbps（pps=55486.52）**

## 7. 解释

- 本次已经使用 2~12 万 pps/worker 做上限复测，但依旧建议多轮取中位值。
- 若用于容量规划，建议固定 CPU 亲和与频率。
- 同一模型不同轮次波动正常，应取中位值并保留原始日志。

## 8. 推荐实践

- 延迟敏感：优先 fastpath。
- 波峰吞吐和隔离：优先 pool。
- 严格顺序：优先 channel。
