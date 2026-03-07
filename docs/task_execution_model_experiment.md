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

## 3. 微基准命令

```bash
go test ./src/task -run '^$' -bench BenchmarkTaskExecutionModels -benchmem -count=3
```

结论（历史结论仍成立）：

- 通常 `fastpath` 最低开销。
- `channel` 在纯模型开销上常优于 `pool_size=1`。
- 端到端结果会受到 IO 与 sender 行为影响，不可只看微基准。

## 4. 端到端复测命令（本次）

```bash
# fastpath
go run ./cmd/bench -mode udp -duration 3s -warmup 1s -payload-size 512 \
  -workers 2 -pps-sweep 2000,4000,8000 \
  -task-execution-model fastpath -log-level info -traffic-stats-interval 1h

# pool
go run ./cmd/bench -mode udp -duration 3s -warmup 1s -payload-size 512 \
  -workers 2 -pps-sweep 2000,4000,8000 \
  -task-execution-model pool -task-pool-size 2048 -log-level info -traffic-stats-interval 1h

# channel
go run ./cmd/bench -mode udp -duration 3s -warmup 1s -payload-size 512 \
  -workers 2 -pps-sweep 2000,4000,8000 \
  -task-execution-model channel -log-level info -traffic-stats-interval 1h
```

## 5. 本次复测结果（loss_rate=0）

### fastpath

| pps | pps(实测) | mbps |
|---:|---:|---:|
| 2000 | 1675.89 | 6.86 |
| 4000 | 1495.80 | 6.13 |
| 8000 | 3828.12 | 15.68 |

**最高：15.68 Mbps**

### pool

| pps | pps(实测) | mbps |
|---:|---:|---:|
| 2000 | 1500.49 | 6.15 |
| 4000 | 1518.29 | 6.22 |
| 8000 | 1844.10 | 7.55 |

**最高：7.55 Mbps**

### channel

| pps | pps(实测) | mbps |
|---:|---:|---:|
| 2000 | 1459.94 | 5.98 |
| 4000 | 1552.42 | 6.36 |
| 8000 | 2048.93 | 8.39 |

**最高：8.39 Mbps**

## 6. 解释

- 本次 sweep 范围偏低，更像“回归型测试”，用于检测模型行为变化与功能稳定性。
- 若用于容量规划，建议将 sweep 提升到 2~10 万 pps/worker，并固定 CPU 亲和与频率。
- 同一模型不同轮次波动正常，应取中位值并保留原始日志。

## 7. 推荐实践

- 延迟敏感：优先 fastpath。
- 波峰吞吐和隔离：优先 pool。
- 严格顺序：优先 channel。

