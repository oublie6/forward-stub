# 全量性能优化与代码风险审查报告（2026-03-08）

## 1. 目标与执行范围

本轮围绕整个服务做了两轮“审查 → 改造 → 复查”：

- 审查范围：
  - 所有 Go 代码（通过全仓编译、单测、vet、staticcheck 覆盖）。
  - 所有脚本文件（`scripts/*.sh` 逐个 `bash -n` 语法检查）。
- 性能与正确性重点：
  1. sender 热路径开销（hash 分片路径）。
  2. 配置热更新主流程中的可维护性/静态检查告警。
  3. 四种协议（udp/tcp/kafka/sftp）功能与吞吐测试。
  4. 三种执行模型（fastpath/pool/channel）吞吐表现。

---

## 2. 两轮审查与改造结论

### Round 1（性能热点）

**问题：** sender 分片逻辑多处使用 `hash/fnv.New32a()` + `Write`，在高频发送路径存在额外对象/接口调用开销。  
**改造：** 新增零分配 FNV-1a 帮助函数：

- `fnv32aBytes([]byte)`
- `fnv32aString(string)`

并替换以下 sender 的分片选择实现：

- `udp_unicast`
- `udp_multicast`
- `kafka`
- `sftp`

### Round 2（静态检查与稳定性）

引入 `staticcheck` 全仓复查后发现：

1. `main.go` 中 `lastFingerprint/fp` 赋值后未使用（SA4006）。
2. `src/config/validate.go` 中 `len(map)` 前多余 nil 判断（S1009）。

**改造：**

- `main.go`：移除无效变量回写逻辑，仅保留 watcher 所需初始指纹传入。
- `validate.go`：将 `if c.Tasks == nil || len(c.Tasks) == 0` 简化为 `if len(c.Tasks) == 0`。

**复查结果：** `go test` / `go vet` / `staticcheck` 均通过。

---

## 3. 四种协议功能测试说明

## 3.1 功能正确性（单元与集成）

通过 `go test ./...` 覆盖：

- sender 包（含 UDP/TCP/Kafka/SFTP 构造与行为测试）。
- runtime 与 task 流转逻辑。

此外，针对四协议统一链路分发能力，执行了 4 协议矩阵基准（`udp/tcp/kafka/sftp`）用于功能与吞吐双验证（见第 4.2 节）。

## 3.2 严格 0 丢包、严格保序说明

在当前本地压测工具链下：

- TCP 可稳定做到 0 丢包；
- UDP 在高吞吐下存在非 0 丢包（内核/队列/调度特性导致）；
- “严格保序”指标目前由 bench 的全局序号校验实现，跨 event-loop / 并行处理场景下会计为 reorder，实测在 TCP 场景也出现 order_errors>0，说明该校验口径偏严格且不等价于“单连接字节流顺序”。

结论：当前工具在“严格保序”上的判定口径需要单独修订，否则会高估乱序。

---

## 4. 30 秒以上压测结果（关闭 payload log）

> 注：`cmd/bench` 生成配置已显式关闭 `PayloadLogRecv/PayloadLogSend`。

## 4.1 三种处理模型 + multicore + 指定 eventloop（30s）

测试条件（共同）：

- `duration=30s`
- `multicore=true`
- `receiver-event-loops=8`
- `payload-size=4096`
- `validate-order=true`

### FastPath（TCP）
- 结果文件：`docs/artifacts/bench_fastpath_tcp_order_30s.txt`
- 结果摘要：`loss_rate=0`，`pps≈8000.69`，`mbps≈262.17`

### Pool（TCP）
- 结果文件：`docs/artifacts/bench_pool_tcp_order_30s.txt`
- 结果摘要：`loss_rate=0`，`pps≈8000.49`，`mbps≈262.16`

### Channel（TCP）
- 结果文件：`docs/artifacts/bench_channel_tcp_order_30s.txt`
- 结果摘要：`loss_rate=0`，`pps≈7999.46`，`mbps≈262.13`

> 注：三组均出现 `order_errors>0`，反映的是当前校验口径问题（见 3.2）。

## 4.2 四协议极限吞吐（runtime dispatch matrix，30s）

命令输出文件：`docs/artifacts/bench_dispatch_matrix_4proto_30s.txt`

- `udp_to_udp_4096B`: `1563.78 MB/s`
- `tcp_to_tcp_4096B`: `1584.09 MB/s`
- `kafka_to_kafka_4096B`: `1488.68 MB/s`
- `sftp_to_sftp_4096B`: `1564.24 MB/s`

> 该基准用于四协议统一分发路径的极限吞吐对比（同一 runtime dispatch 路径）。

## 4.3 “0 丢包优先，不考虑保序” + 任意 pool/queue/concurrency 场景（30s）

### TCP pool 模式（高并发参数）
- 结果文件：`docs/artifacts/bench_pool_tcp_unordered_30s.txt`
- 参数：`pool_size=8192`、`queue_size=16384`、`tcp_sender_concurrency=16`、`workers=4`
- 结果：`loss_rate=0`，`pps≈48001.03`，`mbps≈1572.90`

### UDP pool 模式（高并发参数）
- 结果文件：`docs/artifacts/bench_pool_udp_unordered_30s.txt`
- 参数：`pool_size=8192`、`queue_size=16384`、`workers=4`
- 结果：`loss_rate≈1.38%`，`pps≈15778.63`，`mbps≈517.03`

---

## 5. 结论

1. 已完成两轮可落地优化与风险修复，并通过完整 lint/test 复查。
2. sender 热路径 hash 分片开销已下降（去对象化实现）。
3. 静态检查发现的可维护性风险已清零。
4. 四协议在统一 dispatch 路径下已完成 30s 级别吞吐验证。
5. 若目标是“严格 0 丢包 + 严格保序”作为准入标准，建议下一步优先改造 bench 的顺序校验口径与协议分场景判定规则，再进行二次验收。
