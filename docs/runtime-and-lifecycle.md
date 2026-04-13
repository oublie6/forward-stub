# 运行时、默认值与生命周期

> 职责边界：本文负责说明启动流程、默认值生效层次、runtime 对象生命周期、热更新边界、停机顺序和关键时序摘要。不展开配置字段全集、协议专属字段、pipeline stage 或运维操作步骤；这些内容分别见 `docs/configuration.md`、`docs/receivers-and-senders.md`、`docs/pipeline.md` 和 `docs/operations-manual.md`。

## 0. 关键运行时对象

| 对象 | 主要职责 |
| --- | --- |
| `bootstrap.Run` | 启动、重载、停机的总控流程。 |
| `app.Runtime` | 固化 system 配置基线，并把完整配置应用到 runtime store。 |
| `runtime.Store` | 保存 receivers / selectors / taskSets / senders / tasks / pipelines 的运行态快照。 |
| `runtime.UpdateCache` | 冷启动或热更新时构建、替换、复用运行时对象。 |
| `CompiledSelector` | 把 `match key -> []*TaskState` 编译成热路径只读快照。 |
| `logx.TrafficCounter` | 在 receiver / task 热路径做轻量统计，由后台聚合输出。 |

## 1. 启动阶段实际发生了什么

当前启动链路以 `bootstrap.loadConfigPair -> app.Runtime -> runtime.UpdateCache` 为准，顺序如下：

1. 解析 CLI 参数。
2. 解析 `-system-config` / `-business-config`，得到 system + business 路径。
3. 本地严格反序列化 system / business 配置；未知字段直接报错。
4. 先对 system 自身字段做默认值规范化，保证控制面 API、请求超时、文件监听周期、pprof、logging 等 system 参数可用。
5. 若 `control.api` 非空，则拉取远端 business 配置；否则使用本地 business。
6. 用 `SystemConfig.Merge(BusinessConfig)` 合并出完整 `config.Config`。`Merge` 只做结构拼装，不回填默认值。
7. 对最终完整配置调用一次 `ApplyDefaults(system.business_defaults)`，完成最终默认值规范化。
8. 执行 `Validate()`。
9. 初始化 logger、流量统计参数、GC 周期日志、pprof。
10. 调用 `runtime.UpdateCache()`，编译 pipeline / selector，构建 sender / task / receiver，并启动 receiver。
11. 启动业务配置监听和信号处理，进入稳定运行态。

`waitReceiversStartInvoked` 只等待 receiver `Start` goroutine 已被触发，不保证底层 socket、Kafka consumer group 或 SFTP 轮询已经完全 ready。

## 1.1 Runtime 生命周期边界

冷启动和热更新都通过 `runtime.UpdateCache()` 发布运行态快照，但两者边界不同：

- 冷启动走完整构建：编译 pipeline，构建 sender，构建并启动 task，编译 selector，构建并启动 receiver。
- 热更新走 business delta：先构建新对象，再切换快照，最后回收旧对象。
- system 配置只在冷启动时固化到 `app.Runtime`；热更新时必须与基线一致。
- receiver / task 的统计对象在各自 `Start()` 阶段创建，真正收到或发送数据后才开始累计字节。

## 2. 默认值分别在哪一层生效

### 2.1 system 默认值

system 配置只有两层来源：

1. system 配置显式值。
2. 代码硬编码默认值。

它只覆盖 `control`、`logging` 等 system 自身字段，不读取 `business_defaults`。

### 2.2 business 默认值

business 配置只有三层来源：

1. business 配置显式值。
2. system 配置中的 `business_defaults`。
3. 代码硬编码默认值。

`system.business_defaults` 不是完整 business 配置模板，而是少量可继承字段的系统级默认模板。它只给 business 中**未显式填写**的字段提供系统级默认值。

当前只覆盖三类对象：

- `business_defaults.task`
- `business_defaults.receiver`
- `business_defaults.sender`

它不会生成新对象，也不会覆盖 business 已显式填写的值。

`BusinessDefaultsConfig` 使用独立的窄化 schema，只包含 `ApplyDefaults` 当前会读取并下发的字段。这样做是为了让结构定义和真实生效范围一致：身份字段、拓扑字段、认证字段、协议专属主配置字段仍必须写在具体 business 对象中；如果放进 `business_defaults`，严格 JSON 解析会直接报错。

### 2.3 统一默认值入口

`Config.ApplyDefaults(system.business_defaults)` 是最终默认值规范化总入口，主要处理以下内容：

- `control.timeout_sec=5`
- `control.config_watch_interval=2s`
- `control.pprof_port=6060`（仅当原值为 `0`）
- `logging.*` 一整套默认值
- `task.pool_size=4096`
- `task.channel_queue_size=8192`
- `receiver.multicore=true`
- `receiver.num_event_loop=max(8, runtime.NumCPU())`
- `receiver.socket_recv_buffer=1073741824`
- `sender.concurrency=8`
- `sender.socket_send_buffer=1073741824`
- Kafka receiver / sender 的一组配置默认值

### Kafka receiver 在统一默认值入口回写的字段

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

### Kafka sender 在统一默认值入口回写的字段

- `dial_timeout=10s`
- `request_timeout=30s`
- `retry_timeout=1m`
- `retry_backoff=250ms`
- `conn_idle_timeout=30s`
- `metadata_max_age=5m`
- `partitioner=sticky`

### 2.4 组件构建阶段的运行时回退

`runtime/builders.go` 不再为 receiver/sender 的通用配置补默认值，例如 `receiver.multicore`、`receiver.num_event_loop`、`sender.concurrency` 都必须由统一默认值入口提前写入。

仍有一部分行为不是项目级配置默认值，而是在具体协议组件或第三方库适配时生效：

- SFTP receiver：`poll_interval_sec` 默认 `5` 秒。
- SFTP receiver：`chunk_size` 默认 `65536`，最小强制为 `1024`。
- Kafka receiver：`group_id` 未配置时生成 `forward-stub-<receiver_name>`。
- Kafka receiver：`fetch_min_bytes` 未配或 `<=0` 时按 `1`。
- Kafka receiver：`fetch_max_bytes` 未配或 `<=0` 时按 `16777216`。
- Kafka receiver：`fetch_max_wait_ms` 未配或 `<=0` 时按 `100` 毫秒。
- Kafka sender：`acks` 为空时按 `all/-1` 解释，这是 Kafka 语义解析，不在通用 builder 中兜底。
- Kafka sender：`idempotent` 为空时按 `true`，这是 Kafka 语义解析，不在通用 builder 中兜底。
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

- 一部分在统一默认值入口就回写到配置对象里，例如 `dial_timeout`、`metadata_max_age`、`partitioner`、`balancers`。
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

## 5. 热更新边界：到底会更新哪些配置块

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
2. `business_defaults.receiver.payload_log_max_bytes`（`>0`）
3. `logging.payload_log_max_bytes`

### task

`task.payload_log_max_bytes` 的回退顺序：

1. task 局部值（`>0`）
2. `business_defaults.task.payload_log_max_bytes`（`>0`）
3. `logging.payload_log_max_bytes`

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

## 9. 关键时序摘要

### 9.1 启动时序

```text
main
  -> bootstrap.Run
  -> loadConfigPair
  -> logx / GC logger / pprof
  -> app.Runtime.SeedSystemConfig
  -> runtime.UpdateCache
  -> watchConfigFile + signal handler
```

`runtime.UpdateCache` 在冷启动时的内部顺序是：

```text
compile pipelines
  -> build senders
  -> build/start tasks
  -> compile selectors
  -> build receivers
  -> refresh receiver selector snapshots
  -> start receivers
```

### 9.2 收包与转发时序

热路径稳定为：

```text
receiver(构造 Packet 与 MatchKey)
  -> dispatchToSelector(查 CompiledSelector)
  -> task(按 execution_model 执行)
  -> pipeline(顺序执行 stage)
  -> sender(发送或按 RouteSender 选择单个 sender)
```

关键边界：

- receiver 负责生成完整 `packet.Meta.MatchKey`。
- dispatch 只做 payload 摘要日志、selector 精确查表和 fan-out。
- pipeline stage 返回 false 时，当前 task 停止后续处理并丢弃该包。
- task 发送统计只在真正调用 sender 后累计。

### 9.3 热重载时序

```text
file watcher / reload signal
  -> reloadAndApplyBusinessConfig
  -> loadConfigPair + Validate
  -> CheckSystemConfigStable
  -> runtime.UpdateCache(nextCfg)
  -> rebuild/swap/reuse runtime objects
```

对象更新语义：

- receiver 变化：新建 receiver，刷新 selector 快照，启动新实例，再停止旧实例。
- sender 变化：新建 sender，替换引用，再关闭旧 sender。
- task 变化：按 remove + add 处理；同名重建时尽量复用发送统计句柄。
- selector / task_set / pipeline 变化：重新编译相关 selector 快照，并扩散重建受影响 task。

### 9.4 停机时序

```text
stop signal
  -> stop config watcher
  -> cancel run context
  -> stop GC logger
  -> Store.StopAll
  -> stop pprof
```

`Store.StopAll` 的顺序固定为：

```text
stop receivers concurrently
  -> stop tasks gracefully in order
  -> close senders concurrently
```

## 10. 为什么文档里要强调“仅某类型生效”

因为 `ReceiverConfig`、`SenderConfig` 是按协议并列复用的统一结构体，不是“每个协议一个完全独立 schema”。

所以必须同时看两层：

1. 字段是否存在于配置结构体。
2. 当前代码是否真的在某个协议实现里消费它。

本文和其余文档都以第 2 条为准：**只有代码真实消费的字段，才会被描述为“有效”或“有行为”。**
