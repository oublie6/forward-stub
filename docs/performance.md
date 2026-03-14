# Performance

## 1. 性能目标与约束

项目定位高吞吐低延迟转发，但能力上限由协议类型、执行模型、下游承载能力和部署环境共同决定。

## 2. 代码中的性能设计点

1. gnet 驱动 UDP/TCP 收发，降低 goroutine 切换开销。
2. ants 池提供高并发任务执行与有界队列。
3. dispatch 快照使用原子读，减少锁竞争。
4. payload 复用降低短生命周期对象分配。
5. stage cache 减少热更新时重复编译开销。
6. sender 支持并发参数调优。

对应模块：

- 网络收发：`src/receiver/gnet_udp.go`、`src/receiver/gnet_tcp.go`、`src/sender/gnet_tcp.go`、`src/sender/udp_unicast_gnet.go`
- 调度执行：`src/task/task.go`
- 运行时分发：`src/runtime/update_cache.go`
- payload 复用：`src/packet/pool.go`

## 3. 执行模型对性能影响

- `fastpath`：更低延迟，受下游抖动影响更直接。
- `pool`：吞吐弹性好，是默认推荐模型。
- `channel`：顺序性好，但峰值吞吐有限。

## 4. 队列回压机制

- `pool`：`queue_size` 限制排队，满队列会丢包。
- `channel`：`channel_queue_size` 限制排队，取消上下文会丢包。
- `fastpath`：无独立队列，回压直接传递。

## 5. bench 的定位

`cmd/bench` 用于：

- 生成稳定基线。
- 对比不同执行模型。
- 评估参数变化收益。

常用入口：

```bash
go run ./cmd/bench -config ./configs/bench.example.json
```

详细参数、执行流程和结果解释请见 `docs/bench.md`。

最新可复用基线结果与原始数据见 `docs/performance-baseline.md`。

## 6. 推荐压测方法

1. 固定基线参数。
2. 每次只改一个变量。
3. 记录无丢包最大 PPS 与 Mbps。
4. 同步采集 CPU、内存、pprof。

## 7. 推荐观测指标

- 吞吐：recv/send pps 和 mbps。
- 稳定性：loss rate、sender error。
- 资源：CPU、RSS、GC、goroutine。
- 调度：任务队列拥塞日志。

## 8. 典型瓶颈来源

- 下游 sender 慢导致队列积压。
- payload 日志开启造成额外开销。
- 不合理的并发和队列参数。
- 多订阅场景 clone 成本上升。

排查建议：

1. 先判断是入站瓶颈还是出站瓶颈。
2. 再判断是模型参数问题还是协议下游问题。
3. 最后用 pprof 定位 CPU 或内存热点。

## 9. 优化路径

1. 优先确认执行模型与链路目标匹配。
2. 再调 sender 并发和队列边界。
3. 调整 receiver event loop 和缓冲参数。
4. 最后定位协议下游瓶颈与系统资源限制。

## 10. 后续优化建议

- 增加更细粒度 task 运行时指标。
- 建立基于 bench 的自动回归阈值。
- 补充 route 命中率和 sender 延迟观测能力。

待确认：

- 是否规划统一指标导出接口以支撑自动化性能门禁。
