# 运行时、默认值与生命周期

## 1. 启动阶段发生了什么

当前启动顺序如下：

1. 解析 CLI 参数。
2. 解析 system / business 配置文件路径。
3. 读取本地配置并严格反序列化（未知字段报错）。
4. 合并 `system + business`。
5. 应用 `business_defaults`。
6. 应用代码默认值。
7. 若 `control.api` 非空，则再从控制面拉取 business 配置并重新合并、重新默认化。
8. 执行配置校验。
9. 初始化 logger、GC 日志、pprof。
10. 调用 runtime `UpdateCache`，编译 pipeline / selector，构建 sender / task / receiver。

## 2. 默认值在哪一层生效

### 2.1 business_defaults

这是 system 配置的一部分，用来为 business 中没有显式配置的字段补系统级默认值。

### 2.2 代码默认值

例如：

- `control.timeout_sec=5`
- `logging.level=info`
- `task.pool_size=4096`
- `task.queue_size=8192`
- Kafka timeout / backoff / partitioner 等默认值

### 2.3 运行时内部回退

有些字段不是在 `ApplyDefaults()` 时统一回写，而是在对应组件创建时回退，例如：

- SFTP receiver：`poll_interval_sec` 默认 `5` 秒。
- SFTP receiver：`chunk_size` 默认 `65536`，最小强制为 `1024`。
- Kafka receiver：`group_id` 未配时按 `forward-stub-<receiver_name>` 生成。
- UDP / 组播 sender：`local_ip` 为空时回退 `0.0.0.0`。
- UDP 组播 sender：`ttl<=0` 时回退 `1`。
- SFTP sender：`temp_suffix` 为空时回退 `.part`。

## 3. 热更新会更新哪些配置块

当前 business 配置支持热更新，system 配置不支持热更新。

### 3.1 business 热更新涉及的对象

- `receivers`
- `selectors`
- `task_sets`
- `senders`
- `pipelines`
- `tasks`

### 3.2 system 配置变更的行为

如果热更新时检测到 system 配置变化，当前实现会拒绝这次重载，需要重启进程后才能生效。

## 4. selector 与 dispatch 在运行时的实际形态

配置层看起来是：

```text
receiver -> selector -> task_set -> tasks
```

运行时热路径上则是：

```text
receiver -> compiled selector -> []*TaskState
```

也就是说：

- selector 会提前编译。
- task_set 会在编译期展开。
- dispatch 只做一次精确查询和 fan-out。

## 5. payload 日志默认值如何回退

### receiver

`receiver.payload_log_max_bytes` 的回退顺序：

1. receiver 局部值（`>0`）
2. `logging.payload_log_max_bytes`

### task

`task.payload_log_max_bytes` 的回退顺序：

1. task 局部值（`>0`）
2. `logging.payload_log_max_bytes`

## 6. 停机阶段发生了什么

当前停机顺序如下：

1. 收到 `TERM/INT` 等停止信号。
2. 停止配置监听。
3. 取消主运行 context。
4. 停止 GC 周期日志任务。
5. 停止 runtime（receiver / task / sender 优雅回收）。
6. 停止 pprof 服务。

## 7. 为什么某些字段文档里要写“仅某类型生效”

因为 `ReceiverConfig`、`SenderConfig` 是“按协议并列复用结构体”，并不是“每个协议一个完全独立的 JSON schema”。

因此必须同时关注两件事：

1. 字段是否在结构体里存在。
2. 当前代码是否真的在某个协议实现里消费它。

本次文档全部按第 2 条为准，即：**只有代码实际消费的字段才会被描述为“有效”或“有行为”**。
