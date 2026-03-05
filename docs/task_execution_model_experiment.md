# Task 执行模型小实验（FastPath / PoolSize=1 / 单协程+Channel）

## 实验目的
验证当 `dispatch` 同步调用 task `Handle` 时，不同 task 内执行模型的开销差异，重点对比：

1. `FastPath=true`（同步执行）。
2. `FastPath=false, PoolSize=1`（ants 单 worker）。
3. 单协程 + 有界 channel（实验模型，顺序消费）。

## 实验实现
基于 `src/task/execution_model_benchmark_test.go` 新增基准测试：

- `fastpath_sync`
- `task_pool_size_1`
- `single_goroutine_channel`

测试 payload 为 256B，sender 为 no-op，侧重比较调度/排队模型本身开销。

## 实验命令

```bash
go test ./src/task -run '^$' -bench BenchmarkTaskExecutionModels -benchmem -count=3
```

## 实验结果（本地容器）

- CPU: Intel Xeon Platinum 8370C
- Go: 仓库当前工具链

| 模型 | ns/op（3轮） | 平均 ns/op | B/op | allocs/op |
|---|---:|---:|---:|---:|
| fastpath_sync | 594.3 / 526.0 / 555.2 | 558.5 | 592 | 6 |
| task_pool_size_1 | 1544 / 1520 / 1676 | 1580.0 | 592 | 6 |
| single_goroutine_channel | 891.7 / 862.1 / 790.3 | 848.0 | 544 | 5 |

## 结论

- 在这个“轻处理 + no-op sender”的场景里，性能排序为：
  `FastPath 同步` > `单协程+channel` > `PoolSize=1`。
- 你提出的“单协程+channel”在纯调度开销上，确实优于 `PoolSize=1`（约 46% 更快），
  但仍慢于 `FastPath`（约 52%）。
- 因为该实验故意弱化了 pipeline 与网络发送成本，真实转发链路里差距可能被 IO/业务处理掩盖，建议再用 `cmd/bench` 做端到端复验。
