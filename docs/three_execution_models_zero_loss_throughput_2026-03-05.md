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

## 3. 2026-03-05 历史结果（保留）

| 模型 | 0 丢包最大吞吐 (Mbps) | 对应 pps |
|---|---:|---:|
| fastpath | 299.31 | 73074.35 |
| pool | 151.36 | 36951.94 |
| channel | 139.85 | 34142.75 |

## 4. 最新复测（低压 sweep，仅回归）

- 参数：`pps-sweep=2000,4000,8000`、`payload=512B`、`workers=2`。
- 结果：fastpath 15.68 Mbps，pool 7.55 Mbps，channel 8.39 Mbps（均 0 丢包）。

这组数据不用于容量上限判断，只用于回归：

1. 执行模型是否仍可用；
2. 相对趋势是否出现异常变化；
3. 新增改动是否引入明显性能退化。

## 5. 严格吞吐评估建议

- 固定 CPU 频率，关闭节能策略。
- 使用 `taskset`/`cset` 做核隔离。
- 至少 5 轮，取中位值。
- 同步记录 p99 延迟与 GC pause，避免只看吞吐。

## 6. 风险提示

- 上游发包工具本身可能成为瓶颈。
- 当 sender 下游不稳定时，执行模型排序可能变化。
- 在 payload 很大时，内存带宽与 cache 命中会主导结果。

