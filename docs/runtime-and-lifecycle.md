# 运行时、默认值与生命周期

## 1. 启动阶段实际发生了什么

当前启动链路以 `bootstrap.loadConfigPair -> app.Runtime -> runtime.UpdateCache` 为准，顺序如下：

1. 解析 CLI 参数。
2. 解析 `-config` / `-system-config` / `-business-config`，统一成 system + business 路径。
3. 本地严格反序列化 system / business 配置；未知字段直接报错。
4. 用 `SystemConfig.Merge(BusinessConfig)` 合并出完整 `config.Config`。
5. 先应用 `business_defaults`。
6. 再执行 `ApplyDefaults()`，回写代码层默认值。
7. 若 `control.api` 非空，则拉取远端 business 配置并重新执行“合并 + 默认值回写”。
8. 执行 `Validate()`。
9. 初始化 logger、流量统计参数、GC 周期日志、pprof。
10. 调用 `runtime.UpdateCache()`，编译 pipeline / selector，构建 sender / task / receiver，并启动 receiver。

## 2. 默认值分别在哪一层生效

### 2.1 `business_defaults`

这是 system 配置的一部分，只给 business 中**未显式填写**的字段提供系统级默认值。

当前只覆盖三类对象：

- `business_defaults.task`
- `business_defaults.receiver`
- `business_defaults.sender`

它不会生成新对象，也不会覆盖 business 已显式填写的值。

### 2.2 `ApplyDefaults()` 代码层默认值

这是配置层的第二道默认值回写，主要处理以下内容：

- `control.timeout_sec=5`
- `control.config_watch_interval=2s`
- `control.pprof_port=6060`（仅当原值为 `0`）
- `logging.*` 一整套默认值
- `task.pool_size=4096`
- `task.queue_size=8192`
- `task.channel_queue_size` 默认跟随 `queue_size`
- `receiver.multicore=true`
- `receiver.num_event_loop=max(8, runtime.NumCPU())`
- `receiver.socket_recv_buffer=1073741824`
- `sender.concurrency=8`
- `sender.socket_send_buffer=1073741824`
- Kafka receiver / sender 的一组配置默认值

### Kafka receiver 在 `ApplyDefaults()` 层回写的字段

- `dial_timeout=10s`
- `conn_idle_timeout=30s`
- `metadata_max_age=5m`
- `retry_backoff=250ms`
- `session_timeout=45s`
- `heartbeat_interval=3s`
- `rebalance_timeout=1m`
- `balancers=["cooperative_sticky"]`
- `auto_commit=true`
- `auto_commit_interval=5s`（仅 `auto_commit=true` 时）
- `fetch_max_partition_bytes=1048576`
- `isolation_level=read_uncommitted`

### Kafka sender 在 `ApplyDefaults()` 层回写的字段

- `dial_timeout=10s`
- `request_timeout=30s`
- `retry_timeout=1m`
- `retry_backoff=250ms`
- `conn_idle_timeout=30s`
- `metadata_max_age=5m`
- `partitioner=sticky`

### 2.3 组件构建阶段的运行时回退

仍有一部分默认值不是在 `ApplyDefaults()` 里统一回写，而是在具体组件构建时按协议生效：

- SFTP receiver：`poll_interval_sec` 默认 `5` 秒。
- SFTP receiver：`chunk_size` 默认 `65536`，最小强制为 `1024`。
- Kafka receiver：`group_id` 未配置时生成 `forward-stub-<receiver_name>`。
- Kafka receiver：`fetch_min_bytes` 未配或 `<=0` 时按 `1`。
- Kafka receiver：`fetch_max_bytes` 未配或 `<=0` 时按 `16777216`。
- Kafka receiver：`fetch_max_wait_ms` 未配或 `<=0` 时按 `100` 毫秒。
- Kafka sender：`acks` 为空时按 `all/-1` 解释。
- Kafka sender：`idempotent` 为空时按 `true`。
- Kafka sender：`max_buffered_records<=0` 时沿用 franz-go 默认值（当前为 `10000`）。
- Kafka sender：`max_buffered_bytes<=0` 时不显式设置，沿用 franz-go 默认行为。
- UDP / 组播 sender：`local_ip` 为空时回退 `0.0.0.0`。
- UDP 组播 sender：`ttl<=0` 时回退 `1`。
- SFTP sender：`temp_suffix` 为空时回退 `.part`。

## 3. receiver `match_key` 的生命周期

当前代码中，`match_key` 已经不再是“统一公共函数在热路径里拼字符串”的模型，而是 receiver 自己负责。

#### 3.1 配置层

- `receiver.match_key` 是 `ReceiverConfig` 的局部配置。
- 每种 receiver 支持的 `mode` 不同，校验也按协议分别执行。
- `match_key.fixed_value` 只允许和 `mode=fixed` 一起出现。

#### 3.2 构建层

在 receiver 构造阶段就会编译 match key builder：

- UDP：`compileUDPMatchKeyBuilder()`
- TCP：`compileTCPMatchKeyBuilder()`
- Kafka：`compileKafkaMatchKeyBuilder()`
- SFTP：`compileSFTPMatchKeyBuilder()`

因此当前 receiver 构建阶段不只是“普通 build”，而是已经包含协议专属的冷路径编译逻辑。

#### 3.3 运行层

- UDP：每个报文到达时调用已编译 builder 生成 key。
- TCP：在连接建立时就预生成或预缓存连接级 key，后续每帧复用。
- Kafka：按 record 调用已编译 builder；`topic` / `fixed` 模式会尽量复用预构建前缀或整串结果。
- SFTP：单文件读取前生成一次 key，后续各 chunk 复用。

#### 3.4 热更新层

如果 receiver 配置发生变化（包括 `match_key` 变化）：

1. runtime 先构建新的 receiver 实例；
2. 新实例在构建阶段重新编译自己的 match key builder；
3. 刷新 selector 快照；
4. 启动新的 receiver；
5. 再停止旧的 receiver。

也就是说，`match_key` 的变更是通过 receiver 重建生效，而不是在线替换一个全局拼接函数。

## 4. Kafka sender / receiver 新增配置能力对生命周期的影响

### 4.1 默认值层次

Kafka 新增配置项并不都在同一层生效：

- 一部分在 `ApplyDefaults()` 就回写到配置对象里，例如 `dial_timeout`、`metadata_max_age`、`partitioner`、`balancers`。
- 一部分保留到组件构建阶段回退，例如 receiver 的 `group_id`、`fetch_min_bytes`、`fetch_max_bytes`、`fetch_max_wait_ms`，以及 sender 的部分 franz-go 缓冲默认值。
- 还有一部分没有“项目级默认值”，直接沿用 franz-go / `kgo` 的默认行为，例如 sender 的 `max_buffered_records`、`max_buffered_bytes` 在未显式设置时不会被项目层改写。

### 4.2 生效层次

- 配置校验层：负责检查 duration、枚举值、互斥关系、跨字段约束。
- 组件构建层：负责把配置转成具体 `kgo.Opt`。
- 运行层：真正由 franz-go client 在连接、拉取、提交、发送时执行这些选项。

### 4.3 启动行为

### Kafka receiver

`NewKafkaReceiver()` 会在冷路径完成：

- brokers 解析
- group id 决定
- duration 解析
- balancer 解析
- isolation level 解析
- SASL/TLS 选项拼装
- `kgo.Client` 构造
- match key builder 编译

### Kafka sender

`NewKafkaSender()` 会在冷路径完成：

- brokers 解析
- duration 解析
- acks / partitioner / compression 解析
- SASL/TLS 选项拼装
- 并发 client 切片构造
- record key 策略固化

因此 Kafka sender / receiver 的大量新配置，都是在“启动/热更新构建阶段”一次性编译进 client，而不是运行中零散判断。

### 4.4 热重载行为

当前 business 热更新支持 receiver / sender 整体替换：

- Kafka receiver 任一配置块变化，都会被视为 receiver 配置变化，走“新建 client + 启动新实例 + 停旧实例”。
- Kafka sender 任一配置块变化，都会被视为 sender 配置变化；引用该 sender 的 task 也会被扩散成 remove+add，同步重建。
- 同名 task 重建时会优先复用旧的发送统计句柄，避免 task 发送统计断档。
- system 配置不支持热更新；一旦检测到 system 配置变化，会拒绝本次重载。

## 5. 热更新到底会更新哪些配置块

当前只有 business 配置支持热更新，涉及：

- `receivers`
- `selectors`
- `task_sets`
- `senders`
- `pipelines`
- `tasks`

对应运行时行为如下：

- `receivers`：新增/修改时先建新实例并启动，再停旧实例；删除时异步停止旧实例。
- `selectors` / `task_sets`：重编译每个 receiver 持有的只读 selector 快照。
- `senders`：先建新 sender，再替换 Store 中引用；无引用旧 sender 会被关闭回收。
- `pipelines`：重新编译 pipeline；依赖该 pipeline 的 task 会被扩散为 remove+add。
- `tasks`：走“先删后加”；若是同名重建，会尝试把发送统计句柄移交给新实例。

## 6. selector 与 dispatch 在运行时的真实形态

配置层看起来是：

```text
receiver -> selector -> task_set -> tasks
```

热路径真正执行的是：

```text
receiver -> compiled selector -> []*TaskState
```

也就是说：

- selector 会提前编译成 `CompiledSelector`；
- task_set 会在编译期展开；
- dispatch 只做一次精确查表和 fan-out；
- `packet.Meta.MatchKey` 的生成已经由 receiver 负责，dispatch 不再参与拼接。

## 7. payload 日志默认值如何回退

### receiver

`receiver.payload_log_max_bytes` 的回退顺序：

1. receiver 局部值（`>0`）
2. `logging.payload_log_max_bytes`

### task

`task.payload_log_max_bytes` 的回退顺序：

1. task 局部值（`>0`）
2. `logging.payload_log_max_bytes`

## 8. 停机阶段发生了什么

当前停机顺序如下：

1. 收到 `TERM/INT` 等停止信号。
2. 停止业务配置监听。
3. 取消主运行 context。
4. 停止 GC 周期日志任务。
5. 调用 `runtime.Store.StopAll()`：并发停 receiver、顺序停 task、并发关 sender。
6. 停止 pprof 服务。

### 8.1 统计对象在停机时的行为

- receiver 统计句柄是在各自 `Start()` 阶段创建的，停机时由 receiver 自己关闭。
- task 发送统计句柄是在 `Task.Start()` 阶段创建的，正常停机时由 `Task.StopGraceful()` 关闭。
- 聚合统计后台线程本身由第一次 `AcquireTrafficCounter()` 惰性启动，进程常驻期间持续运行。

## 9. 为什么文档里要强调“仅某类型生效”

因为 `ReceiverConfig`、`SenderConfig` 是按协议并列复用的统一结构体，不是“每个协议一个完全独立 schema”。

所以必须同时看两层：

1. 字段是否存在于配置结构体。
2. 当前代码是否真的在某个协议实现里消费它。

本文和其余文档都以第 2 条为准：**只有代码真实消费的字段，才会被描述为“有效”或“有行为”。**
