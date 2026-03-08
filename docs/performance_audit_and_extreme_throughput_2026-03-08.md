# Forward Stub 全服务多轮性能优化与代码审查报告（2026-03-08）

## 1) 多轮执行概览

本轮继续执行多轮循环：

1. 全量审查（代码 + 脚本）识别性能点与 bug 风险。
2. 落地改造并回归（每次代码变更后执行 `go test` + `go vet`）。
3. 再审查，直到未发现新的低风险高收益改造点。
4. 完成四协议功能测试。
5. 完成 30 秒档性能测试：
   - 严格 0 丢包 + 严格保序极限吞吐（3 种处理模式，开启 receiver multicore，可设任意 eventloop）
   - 严格 0 丢包 + 不考虑保序（任意 pool size + 任意 queue size）

---

## 2) 本轮新增改造

### 2.1 bench 参数与运行模型能力补齐

文件：`cmd/bench/main.go`

- 新增 `--task-queue-size`，并新增 `bench config` 字段 `task_queue_size`，透传到 `TaskConfig.QueueSize`，满足“任意 queue size”压测。
- 保持 bench 生成配置中 `PayloadLogRecv=false`、`PayloadLogSend=false`，确保性能测试期间关闭 payload log。

### 2.2 严格保序统计逻辑修复（bug 风险修复）

文件：`cmd/bench/main.go`

- 修复了“warmup 后直接清零顺序状态”引发的顺序统计失真风险。
- 新实现改为：
  - warmup 结束后记录基线（sent/recv/bytes/order_errors）；
  - 测量结束使用增量（当前总量 - 基线）计算结果；
  - 避免测量窗口切换时与发送并发重置造成的伪乱序。

### 2.3 严格保序模式约束放宽

文件：`cmd/bench/main.go`

- 去掉对 `multicore=false`、`receiver-event-loops=1` 的硬限制，允许在严格保序测试中开启 multicore，并使用任意 eventloop 数进行验证。

---

## 3) 多轮复审结论

- 第 1 轮后已落地上述改造。
- 第 2 轮复审未发现新的低风险高收益改造点或明显 bug 风险。

---

## 4) 四种转发协议功能测试

通过 runtime 矩阵测试覆盖 UDP/TCP/Kafka/SFTP 四协议转发路径（4x4 组合），并完成全量回归：

- `GOFLAGS='-mod=vendor' go test ./...` 通过
- `GOFLAGS='-mod=vendor' go test ./src/runtime -run 'TestForwardMatrix|TestDispatchClonesForEveryTaskAndReleasesOriginal|TestBuildTaskPayloadLogOptions'` 通过
- `GOFLAGS='-mod=vendor' go vet ./...` 通过
- `for f in scripts/*.sh; do bash -n "$f"; done` 通过

---

## 5) 严格 0 丢包 + 严格保序极限吞吐（30s，三种处理模式）

> 协议：TCP
>
> 统一：`duration=30s`, `warmup=5s`, `payload=512`, `validate-order=true`, `workers=1`, `multicore=true`
>
> 且每组使用了不同 eventloop（体现“可任意 eventloop”）：
> - fastpath：`receiver-event-loops=4`
> - channel：`receiver-event-loops=8`
> - pool：`receiver-event-loops=6`

### 5.1 fastpath
- 4k pps：0 丢包 + 严格保序
- 8k pps：0 丢包 + 严格保序
- 10k pps：0 丢包但出现乱序

**极限（strict + zero-loss）**：约 **8,000 pps / 32.77 Mbps**

### 5.2 channel
- 2k pps：0 丢包 + 严格保序
- 4k pps：0 丢包 + 严格保序
- 6k pps：0 丢包 + 严格保序

**本轮扫描上限内极限**：约 **6,000 pps / 24.58 Mbps**

### 5.3 pool（`pool-size=1`, `queue-size=16384`）
- 4k pps：0 丢包 + 严格保序
- 8k pps：0 丢包 + 严格保序
- 10k / 12k pps：0 丢包但出现乱序

**极限（strict + zero-loss）**：约 **8,000 pps / 32.77 Mbps**

---

## 6) 严格 0 丢包 + 不考虑保序（任意 pool/queue，30s）

### 6.1 UDP（pool, workers=4, multicore=true, event-loops=4）
测试组合：
- `(pool,queue)=(1024,4096)`
- `(pool,queue)=(4096,16384)`
- `(pool,queue)=(8192,65536)`

每组扫频：`pps-per-worker=4000,6000`

结果：
- 16k pps 级别可达 0 丢包（最佳约 **16,001 pps / 65.54 Mbps**）
- 24k pps 级别出现小比例丢包（约 0.10% ~ 0.27%）

### 6.2 TCP（pool, workers=4, multicore=true, event-loops=8, sender-concurrency=8）
测试组合：
- `(pool,queue)=(1024,4096)`
- `(pool,queue)=(4096,16384)`
- `(pool,queue)=(8192,65536)`

每组扫频：`pps-per-worker=30000,60000,90000`

结果：
- 所有组合在各档位均为 0 丢包。
- 本轮最高 0 丢包观测吞吐：约 **289,417 pps / 1185.45 Mbps**（`pool=1024,queue=4096`, 90k 档）。

---

## 7) 最终结论

1. 已完成本轮多次审查与改造，且回归通过。
2. 严格保序测试已支持开启 `receiver.multicore` 且使用任意 `eventloop` 数。
3. 在本环境下：
   - 严格保序极限（TCP）约在 6k~8k pps 区间（不同执行模型不同）。
   - 不考虑保序时，TCP 在多 pool/queue 组合下可达到显著更高吞吐且保持 0 丢包。
4. 当前未发现新的低风险高收益优化点；后续建议继续聚焦：
   - 严格保序高 PPS 的乱序根因定位（receiver/task/sender 分段时间线）
   - UDP 高 PPS 场景内核参数与网卡队列调优
