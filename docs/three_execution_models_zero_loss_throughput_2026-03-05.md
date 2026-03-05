# 三种任务执行模型的 0 丢包吞吐测试（2026-03-05）

## 测试目标

在同一环境下比较三种任务执行模型：`fastpath`、`pool`、`channel` 的吞吐表现，并给出 `loss_rate == 0` 条件下的最大吞吐。

## 测试命令

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

## 详细结果

### fastpath

| pps/worker | loss_rate | pps | mbps |
|---:|---:|---:|---:|
| 20000 | 0 | 8223.76 | 33.68 |
| 40000 | 0 | 19058.25 | 78.06 |
| 60000 | 0 | 31520.24 | 129.11 |
| 80000 | 0 | 30781.85 | 126.08 |
| 100000 | 0.0006476 | 37547.56 | 153.79 |
| 120000 | 0 | 73074.35 | 299.31 |

**0 丢包最大吞吐：299.31 Mbps（pps=73074.35）**

### pool

| pps/worker | loss_rate | pps | mbps |
|---:|---:|---:|---:|
| 20000 | 0 | 7049.85 | 28.88 |
| 40000 | 0 | 19436.11 | 79.61 |
| 60000 | 0 | 36951.94 | 151.36 |
| 80000 | 0.0010116 | 52338.52 | 214.38 |
| 100000 | 0.0002993 | 53447.21 | 218.92 |
| 120000 | 0.0003502 | 62800.68 | 257.23 |

**0 丢包最大吞吐：151.36 Mbps（pps=36951.94）**

### channel

| pps/worker | loss_rate | pps | mbps |
|---:|---:|---:|---:|
| 20000 | 0 | 9906.39 | 40.58 |
| 40000 | 0 | 18201.15 | 74.55 |
| 60000 | 0 | 30268.90 | 123.98 |
| 80000 | 0 | 34142.75 | 139.85 |
| 100000 | 0.0001618 | 43255.79 | 177.18 |
| 120000 | 0.0041499 | 53592.93 | 219.52 |

**0 丢包最大吞吐：139.85 Mbps（pps=34142.75）**

## 汇总（按 0 丢包最大吞吐）

| 执行模型 | 0 丢包最大吞吐 (Mbps) | 对应 pps |
|---|---:|---:|
| fastpath | 299.31 | 73074.35 |
| pool | 151.36 | 36951.94 |
| channel | 139.85 | 34142.75 |

## 说明

- 本次仅对 UDP 链路进行了对比，参数固定为 payload=512B、workers=2、duration=3s、warmup=1s。
- 结果存在一定抖动，建议在固定 CPU 频率/隔离核的条件下增加轮次取中位数。
- 从本次单次扫描结果看：`fastpath` 的 0 丢包上限最高，其次 `pool`，再到 `channel`。
