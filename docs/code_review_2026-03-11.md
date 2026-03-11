# 代码审查报告（2026-03-11）

## 范围
- 项目整体（`main`、`cmd/bench`、`src/*`）的静态审阅。
- 自动化检查覆盖：`go test ./...`、`go vet ./...`、`go test -race ./...`。

## 自动化检查结果
- `go test ./...`：通过。
- `go vet ./...`：通过。
- `go test -race ./...`：失败，当前主要集中在 `src/runtime` 相关路径。

## 主要发现

### 1) `Task.StopGraceful` 与统计回调并发访问存在数据竞争（高）
**现象**
- `Task` 在 `StopGraceful()` 中会写 `t.pool/t.ch/t.sendStats`（置空、close）。
- 同时，流量聚合线程通过 `listTaskRuntimeStats -> fn()` 异步调用 `Task.runtimeStats()`，会读取 `t.pool/t.ch`。
- `-race` 已复现该冲突：`task.go:222`（写）与 `task.go:283`（读）。

**影响**
- 关闭/热更新窗口内出现未定义行为风险，可能导致统计不准、测试不稳定，极端情况下触发崩溃。

**建议（低影响优先）**
- 为 `Task` 增加轻量状态锁（仅保护 `pool/ch/sendStats` 的读写），`runtimeStats()` 做只读快照；
- 或将 `pool/ch` 改为可原子读取的包装状态，避免 stop 路径与统计线程直接并发读写裸指针。

### 2) TCP 实际转发测试存在启动时序脆弱性（中）
**现象**
- `TestForwardMatrixTCPToTCP_Actual` 偶发 `connect: connection refused`。
- 该用例在 `UpdateCache` 返回后立即 `Dial` 到 receiver 端口，未显式等待 gnet listener 就绪。

**影响**
- 在 `-race` 或低性能环境下，测试稳定性下降，容易出现假失败，影响并发回归可信度。

**建议**
- 为测试增加就绪探测（重试 `Dial` + 总超时预算），或在 runtime 暴露 receiver ready 信号供测试等待。

### 3) 回调注册表在读锁内执行回调，放大锁持有窗口（低）
**现象**
- `listTaskRuntimeStats()` 持有 `taskRuntimeStatsMu.RLock` 时直接执行 `fn()`。
- 若回调计算变重或发生阻塞，会延长读锁持有时间，影响注册/注销时延。

**影响**
- 当前影响偏可维护性与可扩展性；高频统计周期下可能放大锁竞争。

**建议**
- 先复制 `task->fn` 快照再释放锁，锁外执行回调，避免把回调成本放进临界区。

## 总结
- 常规质量门（`go test`、`go vet`）健康。
- 并发风险仍建议优先处理 **发现 1（高）**，并同步修复 **发现 2** 以提升 `-race` 回归稳定性。
