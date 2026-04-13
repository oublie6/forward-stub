# 配置参考手册

> 本文以当前代码实现为唯一准绳，覆盖 `src/config`、默认值逻辑、校验逻辑、runtime/build 阶段实际消费逻辑，以及仓库内全部配置示例文件。

## 1. 配置文件组织方式

### 1.1 双配置模式

#### system config

只包含以下字段：

- `control`
- `logging`
- `business_defaults`

#### business config

只包含以下字段：

- `version`
- `receivers`
- `selectors`
- `task_sets`
- `senders`
- `pipelines`
- `tasks`

### 1.2 加载与校验顺序

当前实现的顺序为：

1. 本地 JSON 严格反序列化，**禁止未知字段**。
2. 先对 system 自身字段做默认值规范化，用于确定 `control.api`、请求超时、监听周期等 system 参数。
3. 若 `control.api` 非空，则通过控制面拉取最终 business 配置；否则使用本地 business。
4. `SystemConfig.Merge(BusinessConfig)` 只做结构拼装，不应用任何默认值。
5. 对最终完整配置调用一次 `ApplyDefaults(system.business_defaults)`，这是最终默认值规范化总入口。
6. 对完整配置执行 `Validate()`。

默认值来源边界：

- system 配置只有两层来源：system 显式值、代码硬编码默认值。
- business 配置只有三层来源：business 显式值、`system.business_defaults`、代码硬编码默认值。
- runtime 构建阶段原则上不再承担配置默认值回填；构建器假定传入配置已经完成规范化。

## 2. 顶层配置域总览

| 配置块 | 所在文件 | 是否必填 | 说明 |
|---|---|---:|---|
| `version` | business | 否 | 配置版本号，通常由控制面递增；代码未强制要求大于 0，但建议显式设置。 |
| `control` | system | 否 | 控制面接口、配置监听周期、pprof 端口。 |
| `logging` | system | 是 | 日志、流量统计、payload 默认截断、GC 日志。 |
| `business_defaults` | 仅 system | 否 | 只给 business 中的 task / receiver / sender 提供系统级默认值。 |
| `receivers` | business | 是 | 输入端实例表。 |
| `selectors` | business | 是 | `match key -> task_set` 精确匹配规则。 |
| `task_sets` | business | 是 | task 名称数组，用于复用。 |
| `senders` | business | 是 | 输出端实例表。 |
| `pipelines` | business | 是 | stage 数组。 |
| `tasks` | business | 是 | pipeline + sender + execution model 组合。 |

## 3. control 配置

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---:|---|---|
| `control.api` | string | 否 | 空字符串 | 控制面接口地址。非空时，启动和重载阶段会通过控制面重新拉取 business 配置。 |
| `control.timeout_sec` | int | 否 | `5` | 控制面请求超时秒数。`<=0` 时回退默认值。 |
| `control.config_watch_interval` | string(duration) | 否 | `2s` | 本地业务配置文件的轮询检查周期。必须是合法 duration；若运行时解析失败，会回退默认值并打印告警。 |
| `control.pprof_port` | int | 否 | `6060` | `-1` 禁用 pprof，`0` 回退默认值，`1~65535` 监听指定端口。校验范围为 `[-1,65535]`。 |

### 3.1 control 注意事项

- `control.api` 只影响 business 配置来源，不改变 system 配置仍然从本地读取的事实。
- `control.config_watch_interval` 是文件监听轮询周期，不是控制面拉取周期。
- `pprof_port=-1` 时禁用；`pprof_port=0` 会先在默认值阶段回写为 `6060`，因此最终仍会启动 pprof。

## 4. logging 配置

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---:|---|---|
| `logging.level` | string | 否 | `info` | 日志级别，代码注释推荐 `debug/info/warn/error`。同时也会被 gnet sender/receiver 解析为 gnet 日志级别。 |
| `logging.file` | string | 否 | 空字符串 | 日志文件路径；为空时输出到 stderr。 |
| `logging.max_size_mb` | int | 否 | `100` | 日志单卷最大大小。 |
| `logging.max_backups` | int | 否 | `5` | 日志保留卷数。 |
| `logging.max_age_days` | int | 否 | `30` | 日志保留天数。 |
| `logging.compress` | bool | 否 | `true` | 是否压缩滚动日志。 |
| `logging.traffic_stats_interval` | string(duration) | 否 | `1s` | 流量统计日志输出周期。启动阶段会直接解析，非法时启动失败。 |
| `logging.traffic_stats_sample_every` | int | 否 | `1` | 流量统计采样倍率；`<=0` 回退为 `1`。 |
| `logging.payload_log_max_bytes` | int | 否 | `256` | receiver/task 局部未配置时的默认 payload 摘要截断长度。 |
| `logging.gc_stats_log_enabled` | bool | 否 | `false` | 是否开启周期性 GC / 内存 / goroutine 日志。 |
| `logging.gc_stats_log_interval` | string(duration) | 否 | `1m` | GC 周期日志间隔。校验时要求合法且 `>0`。 |

### 4.1 logging 联动关系

- `receiver.payload_log_max_bytes <= 0` 时回退到 `logging.payload_log_max_bytes`。
- `task.payload_log_max_bytes <= 0` 时回退到 `logging.payload_log_max_bytes`。
- `logging.gc_stats_log_enabled=false` 时，即使配置了 `gc_stats_log_interval` 也不会启动 GC 统计任务。

## 5. business_defaults（system 专属）

`business_defaults` 只存在于 system 配置中，用于给 business 配置里**未显式设置**的字段补默认值；如果 business 已写明值，则以 business 为准。

实现上，`BusinessDefaultsConfig` 直接复用正式 `TaskConfig` / `ReceiverConfig` / `SenderConfig`，不再维护单独的影子 schema。因此 JSON 字段名、严格反序列化规则、类型定义都与正式 business 配置保持一致。它只作为默认模板覆盖到已有 business 对象上，不会生成新的 task / receiver / sender，也不会替代协议必填字段校验。

### 5.1 `business_defaults.task`

| 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `pool_size` | int | 无 | 当 `task.pool_size <= 0` 时作为系统级默认值。 |
| `channel_queue_size` | int | 无 | 当 `task.channel_queue_size <= 0` 时作为系统级默认值。 |
| `execution_model` | string | 无 | 当 `task.execution_model` 为空时作为系统级默认值。 |
| `payload_log_max_bytes` | int | 无 | 当 `task.payload_log_max_bytes <= 0` 时作为系统级默认值。 |

### 5.2 `business_defaults.receiver`

| 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `multicore` | bool | 无 | 当 `receiver.multicore` 未配置时作为系统级默认值。 |
| `num_event_loop` | int | 无 | 当 `receiver.num_event_loop <= 0` 时作为系统级默认值。 |
| `payload_log_max_bytes` | int | 无 | 当 `receiver.payload_log_max_bytes <= 0` 时作为系统级默认值。 |
| `socket_recv_buffer` | int | 无 | 当 `receiver.socket_recv_buffer <= 0` 时作为系统级默认值。 |
| Kafka receiver 选项 | 对应字段类型 | 无 | 例如 `dial_timeout`、`metadata_max_age`、`balancers`、`auto_commit`、`fetch_max_partition_bytes`、`isolation_level`，仅对 `type=kafka` 的 receiver 生效。 |
| dds_skydds receiver 选项 | 对应字段类型 | 无 | 例如 `wait_timeout`、`drain_max_items`、`drain_buffer_bytes`，仅对 `type=dds_skydds` 的 receiver 生效。 |

### 5.3 `business_defaults.sender`

| 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `concurrency` | int | 无 | 当 `sender.concurrency <= 0` 时作为系统级默认值。 |
| `socket_send_buffer` | int | 无 | 当 `sender.socket_send_buffer <= 0` 时作为系统级默认值。 |
| Kafka sender 选项 | 对应字段类型 | 无 | 例如 `dial_timeout`、`request_timeout`、`retry_timeout`、`metadata_max_age`、`partitioner`，仅对 `type=kafka` 的 sender 生效。 |

### 5.4 business_defaults 与代码默认值的优先级

优先级顺序如下：

1. business 中显式配置的值。
2. system 中的 `business_defaults`。
3. 代码内置默认值（例如 task queue 默认 `8192`、Kafka timeout 默认 `10s/30s` 等）。

## 6. receivers 配置

### 6.1 通用字段

| 字段 | 类型 | 必填 | 默认值 | 适用范围 | 说明 |
|---|---|---:|---|---|---|
| `type` | string | 是 | 无 | 全部 receiver | 当前支持 `udp_gnet`、`tcp_gnet`、`kafka`、`sftp`、`oss`、`dds_skydds`、`local_timer`。 |
| `listen` | string | 大多数场景是 | 无 | 全部 receiver | UDP/TCP 填监听地址；Kafka 填 broker CSV；SFTP 填 `host:port`；OSS 使用 `endpoint`。 |
| `selector` | string | 是 | 无 | 全部 receiver | 必须引用已存在 selector。 |
| `match_key.mode` | string | 否 | 留空表示兼容默认行为 | 全部 receiver | receiver 自身的 match key 生成模式；初始化/热重载时会预编译成专用 builder。 |
| `match_key.fixed_value` | string | 否 | 空 | `match_key.mode=fixed` | fixed 模式下写入的固定值；仅在 `mode=fixed` 时允许配置。 |
| `multicore` | bool | 否 | `true` | 仅 `udp_gnet` / `tcp_gnet` | gnet 多核事件循环开关。 |
| `num_event_loop` | int | 否 | `max(8, runtime.NumCPU())` | 仅 `udp_gnet` / `tcp_gnet` | gnet event loop 数量。 |
| `read_buffer_cap` | int | 否 | gnet 默认值 | 仅 `udp_gnet` / `tcp_gnet` | gnet 每连接/会话的读缓冲上限。 |
| `socket_recv_buffer` | int | 否 | `1073741824` | 仅 `udp_gnet` / `tcp_gnet` | socket 内核接收缓冲，可由 `business_defaults.receiver.socket_recv_buffer` 提供系统级默认。 |
| `frame` | string | 否 | 空字符串 | 仅 `tcp_gnet` | 当前 receiver 仅支持空字符串或 `u16be`。 |
| `log_payload_recv` | bool | 否 | `false` | 全部 receiver | 是否打印接收 payload 摘要。 |
| `payload_log_max_bytes` | int | 否 | 回退到 `logging.payload_log_max_bytes` | 全部 receiver | receiver 局部 payload 摘要截断长度。 |

### 6.2 UDP/TCP gnet receiver

#### `type=udp_gnet`

- 必填：`listen`、`selector`。
- 可选：`match_key`、`multicore`、`num_event_loop`、`read_buffer_cap`、`socket_recv_buffer`、`log_payload_recv`、`payload_log_max_bytes`。
- `match_key.mode` 支持：
  - 留空：兼容默认模式，输出 `udp|src_addr=<remote_addr>`。
  - `remote_addr`：输出 `udp|remote_addr=<remote_addr>`。
  - `remote_ip`：输出 `udp|remote_ip=<remote_ip>`。
  - `local_addr`：输出 `udp|local_addr=<local_addr>`。
  - `local_ip`：输出 `udp|local_ip=<local_ip>`。
  - `fixed`：输出 `udp|fixed=<fixed_value>`。
- 性能注意：`remote_addr` / 兼容默认模式会直接复用已有 `RemoteAddr().String()`；只有 `remote_ip` / `local_ip` 才做最小必要的地址解析。

#### `type=tcp_gnet`

- 必填：`listen`、`selector`。
- 可选：同上，另外支持 `frame`。
- `frame="u16be"` 表示输入流按 2 字节大端长度前缀拆帧。
- `match_key.mode` 支持：
  - 留空：兼容默认模式，输出 `tcp|src_addr=<remote_addr>`。
  - `remote_addr`：输出 `tcp|remote_addr=<remote_addr>`。
  - `remote_ip`：输出 `tcp|remote_ip=<remote_ip>`。
  - `local_addr`：输出 `tcp|local_addr=<local_addr>`。
  - `local_port`：输出 `tcp|local_port=<local_port>`。
  - `fixed`：输出 `tcp|fixed=<fixed_value>`。
- 性能注意：TCP 会在连接建立时一次性编译并缓存本连接的 match key，后续每帧直接复用。

### 6.3 Kafka receiver

#### 6.3.1 Kafka 基础字段

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---:|---|---|
| `topic` | string | 是 | 无 | 消费主题。 |
| `group_id` | string | 否 | `forward-stub-<receiver_name>` | consumer group；未配置时按 receiver 名自动生成。 |
| `username` | string | 否 | 空 | 与 `password` / `sasl_mechanism` 联动。 |
| `password` | string | 否 | 空 | 与 `username` 联动。 |
| `sasl_mechanism` | string | 否 | 空 | 当前仅支持 `PLAIN`；如果用户名/密码已配置但 mechanism 为空，会按 `PLAIN` 处理。 |
| `tls` | bool | 否 | `false` | 是否启用 TLS。 |
| `tls_skip_verify` | bool | 否 | `false` | 是否跳过 TLS 证书校验。 |
| `client_id` | string | 否 | 空 | Kafka client.id。 |
| `start_offset` | string | 否 | 空 | 支持 `earliest` / `latest`；空表示沿用库默认行为。 |

#### 6.3.2 Kafka 直接映射到 kgo 的字段

| 字段 | 类型 | 默认值 | 映射 | 说明 |
|---|---|---|---|---|
| `dial_timeout` | string(duration) | `10s` | `kgo.DialTimeout` | 建连超时。 |
| `conn_idle_timeout` | string(duration) | `30s` | `kgo.ConnIdleTimeout` | 空闲连接回收超时。 |
| `metadata_max_age` | string(duration) | `5m` | `kgo.MetadataMaxAge` | 元数据缓存有效期。 |
| `retry_backoff` | string(duration) | `250ms` | `kgo.RetryBackoffFn` | 可重试请求退避。 |
| `session_timeout` | string(duration) | `45s` | `kgo.SessionTimeout` | consumer group 会话超时。 |
| `heartbeat_interval` | string(duration) | `3s` | `kgo.HeartbeatInterval` | 心跳间隔，必须小于 `session_timeout`。 |
| `rebalance_timeout` | string(duration) | `1m` | `kgo.RebalanceTimeout` | 重平衡超时。 |
| `balancers` | []string | `['cooperative_sticky']` | `kgo.Balancers` | 当前支持 `range`、`round_robin`、`cooperative_sticky`。 |
| `auto_commit` | bool | `true` | `kgo.DisableAutoCommit` / `kgo.AutoCommitInterval` | 是否自动提交位点。 |
| `auto_commit_interval` | string(duration) | `5s` | `kgo.AutoCommitInterval` | 仅 `auto_commit=true` 时可配置。 |
| `fetch_min_bytes` | int | 运行时回退 `1` | `kgo.FetchMinBytes` | 该值不在统一默认值入口中回写；构建 Kafka receiver 时若未配置或 `<=0`，按 `1` 回退。 |
| `fetch_max_bytes` | int | 运行时回退 `16777216` | `kgo.FetchMaxBytes` | 该值不在统一默认值入口中回写；构建 Kafka receiver 时若未配置或 `<=0`，按 `16 MiB` 回退。 |
| `fetch_max_partition_bytes` | int | `1048576` | `kgo.FetchMaxPartitionBytes` | 该值会在统一默认值入口中回写。 |
| `isolation_level` | string | `read_uncommitted` | `kgo.FetchIsolationLevel` | 支持 `read_uncommitted`、`read_committed`。 |
| `fetch_max_wait_ms` | int | 运行时回退 `100` | `kgo.FetchMaxWait` | 该值不在统一默认值入口中回写；构建 Kafka receiver 时若未配置或 `<=0`，按 `100ms` 回退。 |

#### 6.3.3 Kafka receiver match key

- `match_key.mode` 支持：
  - 留空：兼容默认模式，输出 `kafka|topic=<topic>|partition=<partition>`。
  - `topic`：输出 `kafka|topic=<topic>`。
  - `topic_partition`：输出 `kafka|topic_partition=<topic>|<partition>`。
  - `fixed`：输出 `kafka|fixed=<fixed_value>`。
- 性能注意：`topic` / `fixed` 会在初始化时预生成整串 key；`topic_partition` 只在热路径追加分区号。

#### 6.3.4 Kafka receiver 默认值与生效层次

- `dial_timeout`、`conn_idle_timeout`、`metadata_max_age`、`retry_backoff`、`session_timeout`、`heartbeat_interval`、`rebalance_timeout`、`balancers`、`auto_commit`、`auto_commit_interval`、`fetch_max_partition_bytes`、`isolation_level` 会在统一默认值入口回写。
- `group_id`、`fetch_min_bytes`、`fetch_max_bytes`、`fetch_max_wait_ms` 则保留到 `NewKafkaReceiver()` 构建阶段按实现回退。
- 热重载时，只要 Kafka receiver 配置有变化，就会重建 receiver、新建 `kgo.Client`、重新编译 match key builder，再切换到新实例。

#### 6.3.5 Kafka receiver 校验规则

- `listen` 必须非空。
- `topic` 必须非空。
- `start_offset` 仅允许 `earliest` / `latest`。
- 所有 duration 字段必须是合法且 `>0` 的 duration。
- `balancers` 不允许为空。
- `heartbeat_interval < session_timeout`。
- `auto_commit=false` 时不能再配置 `auto_commit_interval`。
- `fetch_max_partition_bytes >= 0`。
- 当 `fetch_max_bytes > 0` 且 `fetch_max_partition_bytes > 0` 时，必须满足 `fetch_max_partition_bytes <= fetch_max_bytes`。

### 6.4 SFTP receiver

| 字段 | 类型 | 必填 | 默认值 / 运行时回退 | 说明 |
|---|---|---:|---|---|
| `username` | string | 是 | 无 | 登录用户名。 |
| `password` | string | 是 | 无 | 登录密码。 |
| `remote_dir` | string | 是 | 无 | 轮询读取目录。 |
| `poll_interval_sec` | int | 否 | 运行时默认 `5` 秒 | 目录轮询周期。 |
| `chunk_size` | int | 否 | 运行时默认 `65536` | 单次读取分块大小；若配置小于 `1024`，运行时会强制提升到 `1024`。 |
| `host_key_fingerprint` | string | 是 | 无 | 服务端 SSH 公钥指纹，必须是 `SHA256:<base64raw>` 格式且摘要长度为 32 字节。 |

#### 6.4.1 SFTP receiver 行为说明

- `match_key.mode` 支持：
  - 留空：兼容默认模式，输出 `sftp|remote_dir=<remote_dir>|file_name=<base(file)>`。
  - `remote_path`：输出 `sftp|remote_path=<remote_path>`。
  - `filename`：输出 `sftp|filename=<base(file)>`。
  - `fixed`：输出 `sftp|fixed=<fixed_value>`。
- 每次扫描目录时会按文件名排序，保证处理顺序稳定。
- receiver 通过 `seen` 指纹避免重复消费未变化文件；该行为是运行时逻辑，不需要额外配置。
- 性能注意：SFTP 会在单文件开始流式读取前先生成一次 match key，后续所有 chunk 直接复用。

### 6.5 OSS receiver

`type=oss` 用于轮询 S3-compatible OSS bucket 中的对象，并按 chunk 输出 `PayloadKindFileChunk`。

| 字段 | 类型 | 必填 | 默认值 / 运行时回退 | 说明 |
|---|---|---:|---|---|
| `endpoint` | string | 是 | 无 | S3-compatible endpoint，不带 scheme 时按 minio-go 语义处理。 |
| `bucket` | string | 是 | 无 | bucket 名。 |
| `region` | string | 否 | 空 | 按服务商要求填写。 |
| `access_key` | string | 是 | 无 | 访问密钥。 |
| `secret_key` | string | 是 | 无 | 访问密钥。 |
| `use_ssl` | bool | 否 | `false` | 是否使用 HTTPS。 |
| `force_path_style` | bool | 否 | `false` | 强制使用 path-style bucket 地址。 |
| `prefix` | string | 否 | 空 | 轮询对象前缀。 |
| `poll_interval_sec` | int | 否 | 运行时默认 `5` 秒 | 轮询周期。 |
| `chunk_size` | int | 否 | 运行时默认 `65536` | 单 chunk 字节数；省略或配置为 `0` 时使用 64 KiB，若显式配置小于 `1024` 会自动抬升到 `1024`。 |

行为说明：

- 对象按 key 排序后处理，跳过目录占位对象。
- 每个对象通过 `GetObject` 流式读取，chunk payload 使用 `packet.CopyFrom` 并设置 `ReleaseFn`。
- 输出 meta 中 `Proto=ProtoOSS`，`FileName=path.Base(key)`，`FilePath=key`，`Remote=key`，`Local=bucket@endpoint`。
- 去重指纹至少包含 `key + size + last_modified + etag`，不是只按对象名去重。
- `match_key.mode` 支持留空、`remote_path`、`filename`、`fixed`；留空时输出 `oss|bucket=<bucket>|key=<key>`。

### 6.6 local_timer receiver

`type=local_timer` 用于本地定时生成固定 payload，并通过现有 receiver 回调进入 `selector -> task -> pipeline -> sender` 主链路。它不监听网络端口，也不新增 task type。

#### 6.6.1 generator 字段

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---:|---|---|
| `generator.interval` | string(duration) | 与 `rate_per_sec` 二选一 | 无 | 固定间隔模式，例如 `500ms` 表示 1 秒 2 帧。 |
| `generator.rate_per_sec` | int | 与 `interval` 二选一 | 无 | 目标每秒帧数，例如 `1000`。 |
| `generator.tick_interval` | string(duration) | 否 | `100ms` | 速率模式内部调度粒度；运行时用 credit/accumulator 平滑摊分，不按秒突发。 |
| `generator.payload_format` | string | 是 | 无 | 固定 payload 编码：`hex`、`text`、`base64`。 |
| `generator.payload_data` | string | 是 | 无 | 固定 payload 内容；启动时解析一次，之后每帧复制。 |
| `generator.start_delay` | string(duration) | 否 | 无 | 首帧前等待时间，必须合法且 `>0`。 |
| `generator.total_packets` | int64 | 否 | `0` | 最多生成包数；`0` 表示无限发送。 |
| `generator.remote` | string | 否 | 空字符串 | 写入 `packet.Meta.Remote`。 |
| `generator.local` | string | 否 | 空字符串 | 写入 `packet.Meta.Local`。 |

#### 6.6.2 校验规则

- `receiver.selector` 仍然必填，并引用已有 selector。
- `payload_data` 必填。
- `payload_format` 仅允许 `hex`、`text`、`base64`。
- `interval` 与 `rate_per_sec` 必须二选一，不能同时为空，也不能同时配置。
- duration 字段必须是合法且大于 0 的 `time.ParseDuration` 文本。
- `rate_per_sec` 必须大于 0；`total_packets` 必须大于等于 0。

#### 6.6.3 match key

- `match_key.mode` 支持：
  - 留空：兼容默认模式，输出 `local|receiver=local_timer`。
  - `fixed`：输出 `local|fixed=<fixed_value>`。
- 建议链监、压测等业务显式配置 `match_key.mode=fixed`，再在 selector 中用完整 key 精确匹配。

## 7. selectors 配置

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---:|---|---|
| `matches` | map[string]string | 否 | 空 | key 是完整 `match key`，value 是 `task_set` 名称。 |
| `default_task_set` | string | 否 | 空 | 未命中任何 `matches` 时的回退 task_set；为空表示直接丢弃。 |

### 7.1 selector 规则说明

- 当前只支持**完整字符串精确匹配**。
- 不支持通配符、正则、优先级、表达式、字段提取 DSL。
- `matches` 中引用的 task_set 必须存在。
- `default_task_set` 若非空，也必须存在。

## 8. task_sets 配置

`task_sets` 是 `map[string][]string`，value 是 task 名称数组。

### 8.1 规则说明

- task_set 名称不能为空。
- task_set 的 task 列表不能为空。
- 每个 task 名称都必须存在于 `tasks` 中。
- 运行时会在 selector 编译阶段直接把 `task_set` 展开成 task 状态切片，因此它是**配置复用概念**，不是热路径中间层。

## 9. senders 配置

### 9.1 通用字段

| 字段 | 类型 | 必填 | 默认值 | 适用范围 | 说明 |
|---|---|---:|---|---|---|
| `type` | string | 是 | 无 | 全部 sender | 当前支持 `udp_unicast`、`udp_multicast`、`tcp_gnet`、`kafka`、`sftp`、`oss`、`dds_skydds`。 |
| `remote` | string | 多数场景是 | 无 | 全部 sender | UDP/TCP/SFTP 为目标地址；Kafka 为 broker CSV；OSS 使用 `endpoint`。 |
| `frame` | string | 否 | 空字符串 | 仅 `tcp_gnet` | sender 侧支持空字符串或 `u16be`。 |
| `concurrency` | int | 否 | `8` | 全部 sender | 对不同 sender 含义略有不同，但统一要求：若显式配置为正数，则必须是 **2 的幂**。 |
| `socket_send_buffer` | int | 否 | `1073741824` | 仅 `udp_unicast` / `udp_multicast` / `tcp_gnet` | socket 内核发送缓冲。 |
| `topic` | string | Kafka 必填 | 无 | 仅 `kafka` | 目标主题。 |

### 9.2 UDP 单播 sender（`type=udp_unicast`）

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---:|---|---|
| `local_ip` | string | 否 | 运行时回退 `0.0.0.0` | 本地绑定 IP。 |
| `local_port` | int | 是 | 无 | 本地源端口；代码会强制要求大于 0。 |
| `remote` | string | 是 | 无 | 目标 `host:port`。 |
| `socket_send_buffer` | int | 否 | `1073741824` | 写缓冲。 |
| `concurrency` | int | 否 | `8` | shard 数 / socket 数。 |

### 9.3 UDP 组播 sender（`type=udp_multicast`）

| 字段 | 类型 | 必填 | 默认值 / 运行时回退 | 说明 |
|---|---|---:|---|---|
| `local_ip` | string | 否 | 运行时回退 `0.0.0.0` | 本地绑定 IP。 |
| `local_port` | int | 是 | 无 | 本地源端口。 |
| `remote` | string | 是 | 无 | 组播地址。 |
| `iface` | string | 否 | 空字符串 | 多网卡主机建议显式指定网卡。 |
| `ttl` | int | 否 | 运行时回退 `1` | 组播 TTL / hop limit。 |
| `loop` | bool | 否 | `false` | 是否开启组播回环。 |
| `socket_send_buffer` | int | 否 | `1073741824` | 写缓冲。 |
| `concurrency` | int | 否 | `8` | shard 数 / socket 数。 |

### 9.4 TCP sender（`type=tcp_gnet`）

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---:|---|---|
| `remote` | string | 是 | 无 | 目标 TCP 地址。 |
| `frame` | string | 否 | 空字符串 | 支持 `""`、`u16be`。`u16be` 会在发送前添加 2 字节大端长度头。 |
| `concurrency` | int | 否 | `8` | 建立多条连接做轮询发送。 |
| `socket_send_buffer` | int | 否 | `1073741824` | 写缓冲。 |

### 9.5 Kafka sender

#### 9.5.1 Kafka 基础字段

| 字段 | 类型 | 必填 | 默认值 / 语义 | 说明 |
|---|---|---:|---|---|
| `username` | string | 否 | 空 | 与 `password` / `sasl_mechanism` 联动。 |
| `password` | string | 否 | 空 | 与 `username` 联动。 |
| `sasl_mechanism` | string | 否 | 空 | 当前仅支持 `PLAIN`。 |
| `tls` | bool | 否 | `false` | 是否启用 TLS。 |
| `tls_skip_verify` | bool | 否 | `false` | 是否跳过证书校验。 |
| `client_id` | string | 否 | 空 | Kafka client.id。 |
| `acks` | string / int | 否 | 空字符串，等价 `all/-1` | 支持 `0`、`1`、`-1`、`all`。JSON 中既可以写字符串也可以写数字。 |
| `idempotent` | bool | 否 | `true` | 幂等写入开关。 |
| `retries` | int | 否 | `0` 表示沿用 franz-go 默认 | record 级重试次数。 |
| `max_in_flight_requests_per_connection` | int | 否 | `0` 表示沿用库默认 | 单 broker 并发 in-flight produce 请求数。 |
| `linger_ms` | int | 否 | 运行时回退 `1` | 批发送聚合等待毫秒数。 |
| `batch_max_bytes` | int | 否 | 运行时回退 `1048576` | 单批最大字节数。 |
| `max_buffered_bytes` | int | 否 | `0` 表示沿用库默认 | producer 侧最大缓冲字节数。 |
| `max_buffered_records` | int | 否 | `0` 表示沿用库默认 | producer 侧最大缓冲 record 数。 |
| `compression` | string | 否 | 空 / `none` | 支持 `none`、`gzip`、`snappy`、`lz4`、`zstd`。 |
| `compression_level` | int | 否 | `0` | 仅 `gzip` / `lz4` / `zstd` 可配；`0` 表示使用库默认级别。 |
| `partitioner` | string | 否 | `sticky` | 支持 `sticky`、`round_robin`、`hash_key`。 |
| `record_key` | string | 否 | 空 | 固定 record key。 |
| `record_key_source` | string | 否 | 空 | 从 packet 中提取 key。 |

#### 9.5.2 Kafka 直接映射到 kgo 的字段

| 字段 | 类型 | 默认值 | 映射 | 说明 |
|---|---|---|---|---|
| `dial_timeout` | string(duration) | `10s` | `kgo.DialTimeout` | 建连超时。 |
| `request_timeout` | string(duration) | `30s` | `kgo.ProduceRequestTimeout` | Produce 请求超时。 |
| `retry_timeout` | string(duration) | `1m` | `kgo.RetryTimeout` | 可重试请求的总超时。 |
| `retry_backoff` | string(duration) | `250ms` | `kgo.RetryBackoffFn` | 退避间隔。 |
| `conn_idle_timeout` | string(duration) | `30s` | `kgo.ConnIdleTimeout` | 空闲连接回收超时。 |
| `metadata_max_age` | string(duration) | `5m` | `kgo.MetadataMaxAge` | 元数据缓存有效期。 |

#### 9.5.3 Kafka sender 校验规则与联动

- `remote` 必填。
- `topic` 必填。
- `acks` 只支持 `0`、`1`、`-1`、`all`。
- `compression` 只支持 `none`、`gzip`、`snappy`、`lz4`、`zstd`。
- `idempotent=true` 时，`acks` 必须是 `all/-1`。
- `retries`、`max_in_flight_requests_per_connection`、`max_buffered_bytes`、`max_buffered_records` 不能为负数。
- 所有 duration 字段必须是合法且 `>0` 的 duration。
- `partitioner` 只支持 `sticky`、`round_robin`、`hash_key`。
- `record_key` 与 `record_key_source` 互斥。
- `record_key_source` 当前只支持：`payload`、`match_key`、`remote`、`local`、`file_name`、`file_path`、`transfer_id`、`route_sender`。热重载时若这些字段变化，会重建 sender，并联动重建引用它的 task。
- `partitioner=hash_key` 时必须提供 `record_key` 或 `record_key_source`。
- `compression_level != 0` 时，`compression` 必须是 `gzip`、`lz4`、`zstd` 之一。

#### 9.5.4 关于 record key 的实际来源

`record_key_source` 不解析 JSONPath、正则或 DSL，只会直接从已有 packet 字段取值：

- `payload`：整个 payload 字节切片。
- `match_key`：receiver 已构造好的 match key。
- `remote` / `local`：packet 元数据中的远端 / 本地地址或逻辑标识。
- `file_name` / `file_path` / `transfer_id`：SFTP / file_chunk 元数据。
- `route_sender`：route stage 决定的 sender 名。

### 9.6 SFTP sender

| 字段 | 类型 | 必填 | 默认值 / 运行时回退 | 说明 |
|---|---|---:|---|---|
| `remote` | string | 是 | 无 | 远端 `host:port`。 |
| `username` | string | 是 | 无 | 登录用户名。 |
| `password` | string | 是 | 无 | 登录密码。 |
| `remote_dir` | string | 是 | 无 | 最终落盘目录。 |
| `temp_suffix` | string | 否 | 运行时回退 `.part` | 临时文件后缀；全部 chunk 写完后再 rename 为正式文件。 |
| `host_key_fingerprint` | string | 是 | 无 | SSH 主机指纹，格式与 SFTP receiver 相同。 |
| `concurrency` | int | 否 | `8` | 分片并发度。 |

行为补充：

- sender 优先使用 `packet.Meta.TargetFilePath` 作为最终相对路径；为空时回退 `FilePath`，再回退 `FileName`。
- `TargetFileName` 可在未显式设置 `TargetFilePath` 时覆盖 basename。
- 最终路径会清洗为安全相对路径后拼到 `remote_dir` 下，并自动创建父目录。
- rename 成功后才触发 `notify_on_success`。

### 9.7 OSS sender

`type=oss` 用 multipart upload 直接写入 OSS，不会先把完整文件组装到本地。

| 字段 | 类型 | 必填 | 默认值 / 运行时回退 | 说明 |
|---|---|---:|---|---|
| `endpoint` | string | 是 | 无 | S3-compatible endpoint。 |
| `bucket` | string | 是 | 无 | 目标 bucket。 |
| `region` | string | 否 | 空 | 按服务商要求填写。 |
| `access_key` | string | 是 | 无 | 访问密钥。 |
| `secret_key` | string | 是 | 无 | 访问密钥。 |
| `use_ssl` | bool | 否 | `false` | 是否使用 HTTPS。 |
| `force_path_style` | bool | 否 | `false` | 强制使用 path-style bucket 地址。 |
| `key_prefix` | string | 否 | 空 | 当 packet 未携带 `TargetFilePath` 时，拼到 `FilePath` / `FileName` 前。 |
| `part_size` | int | 否 | `5242880` | multipart part 聚合阈值；省略或 `<=0` 时默认 5 MiB，最终校验要求默认化后的值 `>0`。 |
| `storage_class` | string | 否 | 空 | 透传到 `PutObjectOptions.StorageClass`。 |
| `content_type` | string | 否 | 空 | 透传到 `PutObjectOptions.ContentType`。 |

object key 生成优先级：

1. `TargetFilePath`
2. `key_prefix + FilePath`
3. `key_prefix + FileName`

所有 key 都会做路径清洗，拒绝空 key 与 `..` 路径。sender 只接受 `PayloadKindFileChunk`，会校验 chunk `checksum`；重复 chunk 会被幂等忽略，乱序 chunk 只在有界缓存内等待缺口补齐；`CompleteMultipartUpload` 成功后才触发 `notify_on_success`。

### 9.8 notify_on_success

`notify_on_success` 是文件型 sender 的 commit success 后置动作，不是 task 普通 sender fan-out。当前适用于 `sftp` 与 `oss`：

- SFTP：远端 `Rename(temp, final)` 成功后触发。
- OSS：`CompleteMultipartUpload` 成功后触发。

配置可写单对象或数组。Kafka 示例：

```json
"notify_on_success": {
  "type": "kafka",
  "remote": "127.0.0.1:9092",
  "topic": "file-ready",
  "record_key_source": "transfer_id",
  "client_id": "forward-stub-ready"
}
```

数组写法可同时配置多种通知；任一通知失败时 sender 返回错误，但已提交文件不会回滚：

```json
"notify_on_success": [
  {
    "type": "kafka",
    "remote": "127.0.0.1:9092",
    "topic": "file-ready",
    "record_key_source": "transfer_id"
  },
  {
    "type": "dds_skydds",
    "dcps_config_file": "dds_tcp_conf.ini",
    "domain_id": 0,
    "topic_name": "FileReady",
    "message_model": "octet"
  }
]
```

SkyDDS 单对象示例：

```json
"notify_on_success": {
  "type": "dds_skydds",
  "dcps_config_file": "dds_tcp_conf.ini",
  "domain_id": 0,
  "topic_name": "FileReady",
  "message_model": "octet"
}
```

Kafka 通知支持 `remote`、`topic`、`record_key_source`、`client_id`、`username`、`password`、`sasl_mechanism`、`tls`、`tls_skip_verify`、`dial_timeout`、`request_timeout`、`retry_timeout`、`retry_backoff`、`metadata_max_age`、`conn_idle_timeout`。SkyDDS 通知第一版只支持 `message_model=octet`，一次文件成功发送一条 JSON 通知。

## 10. pipelines 配置

`pipelines` 的结构是 `map[string][]StageConfig`，每个 stage 的字段如下。

### 10.1 通用 stage 字段

| 字段 | 类型 | 适用范围 | 说明 |
|---|---|---|---|
| `type` | string | 全部 stage | stage 类型名。 |
| `offset` | int | 偏移类 stage | 读取或写入的起始字节偏移。 |
| `hex` | string | `match_offset_bytes` / `replace_offset_bytes` / `route_offset_bytes_sender.cases` 的 key | 无空格十六进制字符串。 |
| `cases` | map[string]string | `route_offset_bytes_sender` | 十六进制字节序列到 sender 名称的映射。 |
| `default_sender` | string | `route_offset_bytes_sender` | 未命中 `cases` 时使用的默认 sender。 |
| `value` | string | `set_target_file_path` | 直接写入 `TargetFilePath`。 |
| `prefix` | string | `rewrite_target_path_strip_prefix` / `rewrite_target_path_add_prefix` | 路径前缀。 |
| `old` / `new` | string | `rewrite_target_filename_replace` | 文件名简单替换参数。 |
| `pattern` / `replacement` | string | regex stage | 正则与替换文本。 |

### 10.2 当前支持的 stage 类型

| `type` | 作用 | 关键约束 |
|---|---|---|
| `match_offset_bytes` | 匹配指定偏移处的字节串；不匹配则终止该 pipeline。 | `hex` 必须是合法十六进制。 |
| `replace_offset_bytes` | 把指定偏移处的内容替换为给定字节串。 | `hex` 必须是合法十六进制。 |
| `route_offset_bytes_sender` | 从 payload 指定偏移读取固定长度字节，映射到 sender 名称，并写入 `packet.Meta.RouteSender`。 | `cases` 不能为空；每个 case key 的字节长度必须一致；case value 不能为空。 |
| `set_target_file_path` | 直接设置 `packet.Meta.TargetFilePath`。 | `value` 必填。 |
| `rewrite_target_path_strip_prefix` | 从 `TargetFilePath` 或 `FilePath` 去掉给定前缀。 | `prefix` 必填。 |
| `rewrite_target_path_add_prefix` | 给目标路径增加前缀。 | `prefix` 必填。 |
| `rewrite_target_filename_replace` | 文件名字符串替换，例如 `.csv -> _done.csv`。 | `old` 必填。 |
| `rewrite_target_path_regex` | 按正则改写目标路径。 | `pattern` 必须是合法正则。 |
| `rewrite_target_filename_regex` | 按正则改写目标文件名，并同步更新目标路径 basename。 | `pattern` 必须是合法正则。 |

文件路径改写约定：

- `FileName` / `FilePath` 保持来源语义。
- stage 只改 `TargetFileName` / `TargetFilePath`。
- 文件型 sender 优先使用 `Target*`，为空时回退 `File*`。
- `rewrite_target_path_strip_prefix` 在前缀不匹配时保持原路径内容，但会去掉开头的 `/`，保证下游看到相对路径。
- `rewrite_target_path_regex` / `rewrite_target_filename_regex` 在正则不匹配时保持当前路径/文件名。
- 文件名改写 stage 会同步更新 `TargetFilePath` 的 basename；若未配置 `FileName`，会从当前路径 basename 推导。

### 10.3 route stage 与 task.senders 的关系

- route stage 只是**在 task 内部选择 sender**，不参与 receiver -> task 的路由。
- route stage 指向的 sender，必须已经出现在当前 `task.senders` 里，否则配置校验会失败。

## 11. tasks 配置

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---:|---|---|
| `pool_size` | int | 否 | `4096` | 仅 `execution_model=pool` 真正影响 worker 池大小。 |
| `execution_model` | string | 否 | 空；最终回退为 `pool` | 支持 `fastpath`、`pool`、`channel`。 |
| `channel_queue_size` | int | 否 | `8192` | 仅 `channel` 模式生效；未配置或 `<=0` 时回退默认值。 |
| `pipelines` | []string | 否 | 空数组 | 按顺序执行的 pipeline 名称列表。 |
| `senders` | []string | 是 | 无 | 输出 sender 列表；不能为空。 |
| `log_payload_send` | bool | 否 | `false` | 是否在发送前打印 payload 摘要。 |
| `payload_log_max_bytes` | int | 否 | 回退到 `logging.payload_log_max_bytes` | task 局部 payload 摘要截断长度。 |

### 11.1 三种执行模型的差异

| 模型 | 配置值 | 行为 | 适用场景 |
|---|---|---|---|
| `fastpath` | `execution_model=fastpath` | 当前 goroutine 内同步执行 | 极低延迟、轻处理链路 |
| `pool` | `execution_model=pool` | 提交到 ants worker pool | 通用生产场景 |
| `channel` | `execution_model=channel` | 入有界 channel，由单 worker 顺序处理 | 顺序敏感链路 |

### 11.2 task 校验规则

- `senders` 不能为空。
- `execution_model` 只允许 `fastpath`、`pool`、`channel`。
- `channel_queue_size` 不能为负数。
- `pipelines` 中引用的 pipeline 必须存在。
- `senders` 中引用的 sender 必须存在。
- route stage 指向的 sender 必须已经出现在当前 task 的 `senders` 列表中；运行态仍出现 miss 时会 warn 并丢弃该 packet。

## 12. 默认值总表（代码级）

### 12.1 control / logging

| 字段 | 默认值 |
|---|---|
| `control.timeout_sec` | `5` |
| `control.config_watch_interval` | `2s` |
| `control.pprof_port` | `6060` |
| `logging.level` | `info` |
| `logging.max_size_mb` | `100` |
| `logging.max_backups` | `5` |
| `logging.max_age_days` | `30` |
| `logging.compress` | `true` |
| `logging.traffic_stats_interval` | `1s` |
| `logging.traffic_stats_sample_every` | `1` |
| `logging.payload_log_max_bytes` | `256` |
| `logging.gc_stats_log_enabled` | `false` |
| `logging.gc_stats_log_interval` | `1m` |

### 12.2 receiver

| 字段 | 默认值 | 说明 |
|---|---|---|
| `multicore` | `true` | 仅 gnet receiver 生效。 |
| `num_event_loop` | `max(8, runtime.NumCPU())` | 仅 gnet receiver 生效。 |
| `socket_recv_buffer` | `1073741824` | 仅 gnet receiver 生效。 |
| `payload_log_max_bytes` | 回退到 logging | 全部 receiver。 |
| Kafka `dial_timeout` | `10s` | 仅 Kafka receiver。 |
| Kafka `conn_idle_timeout` | `30s` | 仅 Kafka receiver。 |
| Kafka `metadata_max_age` | `5m` | 仅 Kafka receiver。 |
| Kafka `retry_backoff` | `250ms` | 仅 Kafka receiver。 |
| Kafka `session_timeout` | `45s` | 仅 Kafka receiver。 |
| Kafka `heartbeat_interval` | `3s` | 仅 Kafka receiver。 |
| Kafka `rebalance_timeout` | `1m` | 仅 Kafka receiver。 |
| Kafka `balancers` | `['cooperative_sticky']` | 仅 Kafka receiver。 |
| Kafka `auto_commit` | `true` | 仅 Kafka receiver。 |
| Kafka `auto_commit_interval` | `5s` | 仅 Kafka receiver 且 auto commit 开启时。 |
| Kafka `fetch_max_partition_bytes` | `1048576` | 仅 Kafka receiver。 |
| Kafka `isolation_level` | `read_uncommitted` | 仅 Kafka receiver。 |

### 12.3 sender

| 字段 | 默认值 | 说明 |
|---|---|---|
| `concurrency` | `8` | 所有 sender 通用默认值。 |
| `socket_send_buffer` | `1073741824` | 仅 udp/tcp sender 生效。 |
| Kafka `dial_timeout` | `10s` | 仅 Kafka sender。 |
| Kafka `request_timeout` | `30s` | 仅 Kafka sender。 |
| Kafka `retry_timeout` | `1m` | 仅 Kafka sender。 |
| Kafka `retry_backoff` | `250ms` | 仅 Kafka sender。 |
| Kafka `conn_idle_timeout` | `30s` | 仅 Kafka sender。 |
| Kafka `metadata_max_age` | `5m` | 仅 Kafka sender。 |
| Kafka `partitioner` | `sticky` | 仅 Kafka sender。 |

### 12.4 task

| 字段 | 默认值 |
|---|---|
| `pool_size` | `4096` |
| `channel_queue_size` | `8192` |
| `payload_log_max_bytes` | 回退到 `logging.payload_log_max_bytes` |
| 最终执行模型 | `pool`（当 `execution_model` 为空） |

## 13. 常见互斥、依赖与回退关系

- `receiver.selector` -> 必须引用已有 `selectors.<name>`。
- `selector.matches/default_task_set` -> 必须引用已有 `task_sets.<name>`。
- `task_sets.<name>[]` -> 必须引用已有 `tasks.<name>`。
- `tasks.<name>.pipelines[]` -> 必须引用已有 `pipelines.<name>`。
- `tasks.<name>.senders[]` -> 必须引用已有 `senders.<name>`。
- `route_offset_bytes_sender.cases/default_sender` -> 目标 sender 必须同时出现在该 task 的 `senders` 列表中。
- Kafka sender：`record_key` 与 `record_key_source` 互斥。
- Kafka sender：`partitioner=hash_key` 依赖 `record_key` 或 `record_key_source`。
- Kafka sender：`idempotent=true` 依赖 `acks=all/-1`。
- Kafka receiver：`auto_commit=false` 时不能设置 `auto_commit_interval`。
- Kafka receiver：`heartbeat_interval < session_timeout`。
- `task.payload_log_max_bytes` / `receiver.payload_log_max_bytes` -> 未设置时回退到 `logging.payload_log_max_bytes`。

## 14. 示例文件如何选择

| 需求 | 推荐文件 |
|---|---|
| 想看所有字段 | `configs/system.example.json`、`configs/business.example.json` |
| 想先跑通最小链路 | `configs/minimal.system.example.json` + `configs/minimal.business.example.json` |
| 只看 UDP/TCP | `configs/udp-tcp.business.example.json` |
| 只看 Kafka | `configs/kafka.business.example.json` |
| 只看 SFTP | `configs/sftp.business.example.json` |
| 只看本地定时造数 | `configs/local-timer.business.example.json` |
| 只看 task 执行模型 | `configs/task-models.business.example.json` |
| 只看 benchmark 配置 | `configs/bench.example.json` |
