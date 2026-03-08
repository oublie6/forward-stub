# Forward Stub 全服务性能优化与代码审查（迭代更新，2026-03-08）

## 1. 本轮新增优化（针对 UDP 吞吐）

### 1.1 UDP Sender 分片并发（已落地）

文件：`src/sender/udp_unicast_gnet.go`、`src/runtime/update_cache.go`

- `udp_unicast` sender 新增并发分片能力（使用 sender `concurrency` 参数）。
- 初始化阶段按并发度建立多个 UDP socket（同一 sender 内多个 shard）。
- 发送阶段按 payload 哈希选择 shard，分散单 socket 热点，提升并行发送能力。
- runtime `buildSender` 已把 sender `concurrency` 透传给 UDP unicast sender。

### 1.2 Bench 默认使用更合理的 UDP sender 并发（已落地）

文件：`cmd/bench/main.go`

- `benchConfig` 生成 UDP sender 配置时，会在 `workers > 1` 场景下将 `sender.concurrency` 设为 `workers`，使压测模型与并行发包更匹配。

### 1.3 正确性与稳定性修复（延续）

- 保持 warmup 基线差值统计（避免并发清零导致测量失真）。
- payload log 继续强制关闭（`PayloadLogRecv/Send=false`）。
- 严格保序测试保留可配置 multicore / eventloop 能力。

---

## 2. 多轮代码审查结论

本轮继续对 `src/`、`cmd/`、`scripts/` 做静态与运行路径复审，重点检查：

- UDP 热路径锁竞争与原子访问
- sender 建连/关闭并发安全
- task 队列配置与丢包边界
- bench 测量可信度

结论：

1. **“把互斥锁改成读写锁”不是当前 UDP 吞吐主瓶颈**。
   - UDP sender 热路径 `Send()` 使用原子读取连接指针；锁主要在建连/关闭慢路径。
2. UDP 主要瓶颈仍在“每包处理成本 + 队列阈值 + UDP 无重传特性 + sink 能力”。
3. 本轮未发现新的高收益低风险改造点（在当前架构约束内）。

---

## 3. 功能与质量验证

- `GOFLAGS='-mod=vendor' go test ./...`：通过
- `GOFLAGS='-mod=vendor' go vet ./...`：通过
- 四协议矩阵相关用例（runtime forward matrix）已包含在回归中通过。

---

## 4. 严格 0 丢包 + 严格保序极限吞吐（30s，三种处理模式）

> 协议：TCP（严格序号校验）
>
> 统一参数：`duration=30s`、`warmup=5s`、`payload=512B`、`validate-order=true`、`multicore=true`、可配置 eventloop。

### 4.1 fastpath（eventloop=4）
- 4k pps：0 丢包 + 严格保序
- 8k pps：0 丢包 + 严格保序
- 10k pps：0 丢包但乱序

**极限：约 8k pps / 32.77 Mbps**

### 4.2 channel（eventloop=8）
- 3k pps：0 丢包 + 严格保序
- 5k pps：0 丢包 + 严格保序
- 7k pps：0 丢包 + 严格保序

**本轮扫描上限内极限：约 7k pps / 28.69 Mbps**

### 4.3 pool（pool=1, queue=16384, eventloop=6）
- 4k pps：0 丢包 + 严格保序
- 8k pps：0 丢包 + 严格保序
- 10k pps：0 丢包但乱序

**极限：约 8k pps / 32.77 Mbps**

---

## 5. 严格 0 丢包 + 不考虑保序（任意 pool size + 任意 queue size，30s）

### 5.1 UDP（workers=4, multicore=true, eventloop=8）

测试组合：
- `(pool,queue)=(1024,4096)`
- `(pool,queue)=(4096,16384)`
- `(pool,queue)=(8192,65536)`

每组扫频：`pps-per-worker=4000,6000,8000`

结果摘要：
- 16k pps 档稳定 0 丢包。
- 24k/32k pps 档出现小比例丢包（约 0.08%~0.48%）。
- 在本轮参数下，UDP 仍呈现“阈值后快速掉包”特征。

### 5.2 TCP（workers=4, multicore=true, eventloop=8, sender-concurrency=8）

测试组合：
- `(pool,queue)=(1024,4096)`
- `(pool,queue)=(4096,16384)`
- `(pool,queue)=(8192,65536)`

每组扫频：`pps-per-worker=30000,60000`

结果摘要：
- 各组合两档均 0 丢包。
- 最高观测约 **240.7k pps / 986 Mbps**（0 丢包）。

---

## 6. 对“UDP 吞吐低”的最终判断与后续动作

### 6.1 结论
- 不是 RWMutex 改造问题；主瓶颈不在“读锁争用”。
- 更关键的是 UDP 数据面（每包成本、内核队列、sink 能力、无重传）

### 6.2 已完成动作
- UDP sender 分片并发（按 payload hash shard）
- bench UDP sender 并发与 workers 对齐

### 6.3 仍建议的下一阶段（系统/架构层）
1. 外置 sink（独立机器）减少本机回环干扰。
2. Linux 内核网络参数调优：`rmem_max/wmem_max/netdev_max_backlog`、NIC ring。
3. 发送批量化（sendmmsg/WriteBatch）路线评估与落地。
4. 接收链路“每包 copy”进一步优化（零拷贝/批处理策略）。
