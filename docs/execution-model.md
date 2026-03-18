# Execution Model

## 1. 设计目的

`Task` 负责把 packet 变成下游发送动作。为兼顾不同链路目标，系统提供三种执行模型：

- `fastpath`
- `pool`
- `channel`

模型由 `task.execution_model` 决定，默认值可来自 `business_defaults.task.execution_model`。

## 2. 三种模型执行路径

```mermaid
flowchart TD
  Sel[Selector Match] --> Task[Task Handle]
  Task --> Mode{Model}

  Mode --> Fast[FastPath]
  Mode --> Pool[Pool]
  Mode --> Chan[Channel]

  Fast --> Proc[Process Pipeline]
  Pool --> Proc
  Chan --> Proc
  Proc --> Send[Send Fanout]
```

以上总览图对应 `runtime.dispatch(match selector) -> task.Handle -> task.processAndSend` 的统一主链路；差异点只在 `Handle` 内的执行方式。

## 3. fastpath

```mermaid
flowchart LR
  R[Receiver] --> D[Dispatcher]
  D --> T[Task.Handle]
  T --> DP[Direct Path]
  DP --> P[Pipeline]
  P --> S[Sender Fanout]
```

- 执行路径：`dispatch` 把 packet 交给 `Task.Handle` 后，`fastpath` 直接在当前 goroutine 执行 `processAndSend`。
- 适用场景：链路处理轻、对端稳定、需要尽量压低端到端延迟。
- 突出特点：路径最短、无额外排队；但下游阻塞会直接传导回 Receiver。

### 机制

- 当前 goroutine 直接执行 `processAndSend`。
- 不经过额外队列和 worker。

### 影响

- 固定开销低，延迟路径最短。
- 下游慢会直接反压上游 receiver。

### 场景

- 处理逻辑很轻。
- 对延迟敏感，且下游稳定。

## 4. pool

```mermaid
flowchart LR
  R[Receiver] --> D[Dispatcher]
  D --> T[Task.Handle]
  T --> Q[Pool Submit Queue]
  Q --> WP[Worker Pool]
  WP --> P[Pipeline]
  P --> S[Sender Fanout]
```

- 执行路径：`Task.Handle` 将任务提交给 ants worker pool，再由 worker 并行执行 `processAndSend`。
- 适用场景：通用生产流量，需要吞吐、并发与隔离的平衡。
- 突出特点：可通过 `pool_size` 和 `queue_size` 扩展并发；队列满时会丢弃并记录错误日志。

### 机制

- 使用 ants 池提交任务。
- `pool_size` 控制 worker，`queue_size` 控制最大阻塞任务。

### 影响

- 吞吐更稳健，易横向放大并发。
- 队列积压时尾延迟可能上升。
- 队列满会丢包并记录告警日志。

### 场景

- 通用生产场景。
- 需要平衡吞吐与隔离。

## 5. channel

```mermaid
flowchart LR
  R[Receiver] --> D[Dispatcher]
  D --> T[Task.Handle]
  T --> CQ[Channel Queue]
  CQ --> C[Single Consumer]
  C --> P[Pipeline]
  P --> S[Sender Fanout]
```

- 执行路径：`Task.Handle` 先入有界 channel，再由单消费者 goroutine 顺序执行 `processAndSend`。
- 适用场景：同一 task 内需要更清晰顺序语义，且峰值流量可控。
- 突出特点：队列缓冲实现接入与处理解耦；但消费端为单 worker，吞吐上限受限。

### 机制

- 单 worker goroutine 读取有界 channel。
- `channel_queue_size` 控制排队深度。

### 影响

- 单 task 内顺序语义清晰。
- 峰值吞吐受单 worker 限制。

### 场景

- 顺序敏感链路。
- 处理逻辑中等，流量可控。

## 6. 模型对比

| 维度 | fastpath | pool | channel |
|---|---|---|---|
| 吞吐上限 | 中到高 | 高 | 中 |
| 延迟 | 最低 | 中 | 中到高 |
| 顺序性 | 取决于并发 | 弱 | 强 |
| 隔离性 | 低 | 高 | 中 |
| 回压位置 | 直接到上游 | 队列边界 | channel 边界 |

## 7. 与 task/pipeline/sender 的关系

- 模型只改变“如何执行”，不改变 pipeline/sender 业务语义。
- 三种模型最终都走同一条 `pipeline -> sender` 逻辑。
- route stage、生效 sender 列表、日志策略在三种模型下保持一致。

## 8. 选型建议

1. 首选 `pool` 作为默认模型。
2. 极低延迟链路评估 `fastpath`。
3. 顺序敏感链路选 `channel`。
4. 所有模型都应配合 `go test -bench` 的场景化 benchmark 做验证。
