# Forward Stub 全服务多轮性能优化与代码审查报告（2026-03-08）

## 一、执行目标（本轮）

本轮按要求执行了多轮循环：

1. 全量审查代码与脚本，识别性能优化点与 bug 风险。
2. 落地改造后回归测试（每次代码变更均执行 test + vet）。
3. 重复审查，直到未发现新的低风险高收益改造点。
4. 完成四种协议转发功能测试。
5. 完成两类压测（每个性能测试档位 `duration >= 30s`，并关闭 payload log）：
   - 严格 0 丢包 + 严格保序极限吞吐（三种处理模式）
   - 严格 0 丢包 + 不考虑保序，任意 pool size + 任意 queue size 极限吞吐

---

## 二、多轮审查与改造

### 第 1 轮审查发现

1. `cmd/bench` 缺少 `task_queue_size` 参数透传，无法满足“任意 queue size”压测。
2. 严格保序校验约束不完整：未限制 `receiver-event-loops` 与 `multicore`，可能在 ingress 并行导致重排噪声。
3. sink 顺序校验在非严格模式下仍走校验分支（可进一步收敛开销与语义）。
4. bench 结果需要明确输出顺序误差信息用于判定严格保序。

### 第 1 轮改造

1. `cmd/bench/main.go`
   - 新增 `--task-queue-size` flag，并接入 config file 字段 `task_queue_size`。
   - `benchConfig` 将 `QueueSize` 写入 `TaskConfig`。
   - 严格保序模式补充参数约束：
     - `receiver-event-loops=1`
     - `multicore=false`
     - `task-execution-model=pool` 时要求 `task-pool-size=1`
   - sink 顺序校验仅在 `validate-order` 打开时执行。
2. 保持压测配置中 `PayloadLogRecv=false`、`PayloadLogSend=false`，确保压测期间 payload log 关闭。

### 第 2 轮复审结论

- 复查热路径、生命周期和压测工具行为后，未发现新的同级别（低风险、可直接提交）优化点或明显 bug 风险。

---

## 三、四种协议功能测试

通过 runtime 矩阵测试覆盖 UDP/TCP/Kafka/SFTP 四种协议组合转发路径（包含 4x4 组合矩阵），并执行全量回归。

- `GOFLAGS='-mod=vendor' go test ./...`：通过
- `GOFLAGS='-mod=vendor' go test ./src/runtime -run 'TestForwardMatrix|TestDispatchClonesForEveryTaskAndReleasesOriginal|TestBuildTaskPayloadLogOptions'`：通过
- `GOFLAGS='-mod=vendor' go vet ./...`：通过
- `for f in scripts/*.sh; do bash -n "$f"; done`：通过

---

## 四、严格 0 丢包 + 严格保序极限吞吐（30s 档，三种处理模式）

> 测试协议：TCP（使用 u16 帧，便于严格顺序校验）
>
> 统一参数：`workers=1`, `payload=512B`, `duration=30s`, `warmup=5s`, `validate-order=true`, `receiver-event-loops=1`, `multicore=false`, `tcp-sender-concurrency=1`

### 1) fastpath
- 4k pps：`loss=0`, `order_errors=0`, `strict_order_ok=true`
- 8k pps：`loss=0`, `order_errors=0`, `strict_order_ok=true`
- 10k pps：`loss=0`, 但 `order_errors>0`, `strict_order_ok=false`

**严格保序极限（fastpath）**：约 **8,000 pps / 32.77 Mbps**

### 2) channel
- 4k pps：`loss=0`, `order_errors=0`, `strict_order_ok=true`
- 8k pps：`loss=0`, 但 `order_errors>0`, `strict_order_ok=false`
- 10k pps：`loss=0`, 但 `order_errors>0`, `strict_order_ok=false`

**严格保序极限（channel）**：约 **4,000 pps / 16.38 Mbps**

### 3) pool（`task-pool-size=1`, `task-queue-size=16384`）
- 2k / 4k / 6k / 8k / 10k pps：均 `loss=0`, `order_errors=0`, `strict_order_ok=true`
- 12k pps：`loss=0`, 但 `order_errors>0`, `strict_order_ok=false`

**严格保序极限（pool）**：约 **10,001 pps / 40.96 Mbps**

---

## 五、严格 0 丢包 + 不考虑保序（任意 pool size + 任意 queue size，30s 档）

### A. UDP（pool）
参数：`workers=4`, `duration=30s`, `warmup=5s`, `payload=512B`

测试组：
1. `pool=1024, queue=4096`
2. `pool=4096, queue=16384`
3. `pool=8192, queue=65536`

每组扫频：`pps-per-worker=4000,6000`（总约 16k / 24k）

结论：
- 总约 16k pps 可达到 0 丢包（最佳观测约 **16,004 pps / 65.55 Mbps**）。
- 总约 24k pps 出现丢包（约 0.3%~0.43%）。

### B. TCP（pool）
参数：`workers=4`, `duration=30s`, `warmup=5s`, `payload=512B`, `tcp-sender-concurrency=8`

测试组：
1. `pool=1024, queue=4096`
2. `pool=4096, queue=16384`
3. `pool=8192, queue=65536`

每组扫频：`pps-per-worker=30000,60000`（总约 120k / 240k）

结论：
- 全部组合在两档速率均保持 `loss=0`。
- 本轮最佳 0 丢包吞吐观测值：约 **240,762 pps / 986.16 Mbps**（`pool=1024,queue=4096` 组 60k 档）。

---

## 六、最终结论

1. 已完成多轮“审查→改造→复审”，并补齐了严格保序压测约束与任意 queue size 能力。
2. 四协议功能测试通过（UDP/TCP/Kafka/SFTP 矩阵）。
3. 严格保序场景下三种处理模式极限不同：`pool(size=1) > fastpath > channel`（本机本次测得）。
4. 不考虑保序时，TCP 在多 pool/queue 组合下可显著提升至高吞吐且 0 丢包。
5. 当前未发现新的低风险高收益改造点；后续若继续优化，建议聚焦：
   - 严格保序下高 PPS 重排根因（网络栈/事件循环/任务调度链路）
   - UDP 高 PPS 的内核 socket buffer 与 NIC 队列参数联调
