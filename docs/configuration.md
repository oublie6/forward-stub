# Configuration（权威配置文档）

> 本文以当前源码实现为准，覆盖配置定义、默认值、校验规则、继承覆盖关系和运行时行为。README 仅保留摘要与入口。

## 1. 文档概览

## 1.1 配置体系

forward-stub 支持两种加载模式：

- **双文件模式（推荐）**
  - `system-config`：系统级配置（控制面、日志、业务默认值）
  - `business-config`：业务拓扑配置（收发器、selector、pipeline、task）
- **legacy 单文件模式（兼容）**
  - 使用 `-config`，同一文件同时按 `SystemConfig` 和 `BusinessConfig` 读取并合并。

## 1.2 system / business 职责边界

- `SystemConfig`：`control`、`logging`、`business_defaults`。属于进程级配置，重载 business 时必须保持稳定。
- `BusinessConfig`：`version`、`receivers`、`senders`、`pipelines`、`selectors`、`tasks`。支持热更新。

## 1.3 配置加载方式

1. `ResolveConfigPaths` 解析 CLI 参数组合。
2. `LoadSystemLocal` / `LoadBusinessLocal` 使用 **严格 JSON** 解析（`DisallowUnknownFields`，未知字段直接报错）。
3. `SystemConfig.Merge(BusinessConfig)` 得到运行态 `Config`。
4. 若 `control.api` 非空，启动时会远端拉取 business 并覆盖本地 business。
5. `ApplyBusinessDefaults`：仅把 `business_defaults` 应用于 business 的空缺字段。
6. `ApplyDefaults`：填充最终默认值。
7. `Validate`：做结构、引用、类型、取值合法性校验。

## 1.4 默认值、继承、覆盖关系

优先级（高 -> 低）：

1. business 显式值。
2. `system.business_defaults`。
3. 代码默认值（`ApplyDefaults`）。

说明：

- `business_defaults` 仅影响 `task/receiver/sender` 的部分字段（见后文）。
- `ApplyDefaults` 会进一步兜底，并补齐 logging/control 等 system 字段。

## 1.5 business_defaults 如何作用于 task / receiver / sender

- `business_defaults.task`：`pool_size`、`queue_size`、`channel_queue_size`、`execution_model`、`payload_log_max_bytes`
- `business_defaults.receiver`：`multicore`、`num_event_loop`、`payload_log_max_bytes`
- `business_defaults.sender`：`concurrency`

仅在对应 business 字段未设置或 <=0 时生效。

---

## 2. 顶层配置结构

## 2.1 双文件结构

```text
system-config
├─ control
├─ logging
└─ business_defaults
   ├─ task
   ├─ receiver
   └─ sender

business-config
├─ version
├─ receivers
├─ senders
├─ pipelines
├─ selectors
└─ tasks
```

## 2.2 legacy 单文件结构

legacy 文件是运行态 `Config` 结构：

```text
config
├─ version
├─ control
├─ logging
├─ receivers
├─ senders
├─ pipelines
├─ selectors
└─ tasks
```

---

## 3. System 配置详解

## 3.1 control

| 字段 | 类型 | 含义 | 必填 | 默认值 | 生效条件 | 备注/约束 |
|---|---|---|---|---|---|---|
| `api` | string | 远端 business 配置 API 地址 | 否 | `""` | 非空时启用远端拉取 | 返回体必须是 `BusinessConfig` JSON |
| `timeout_sec` | int | 控制面请求超时（秒） | 否 | `5` | `api` 非空时影响 HTTP 超时 | <=0 回退默认 |
| `config_watch_interval` | string(duration) | 本地 business 文件变更检测间隔 | 否 | `"2s"` | 文件监听热更新 | 非法值会 fallback 到 `2s` |
| `pprof_port` | int | pprof HTTP 端口 | 否 | `6060` | >0 时启用 pprof | `-1` 禁用；校验范围 `[-1,65535]` |

## 3.2 logging

| 字段 | 类型 | 含义 | 必填 | 默认值 | 生效条件 | 备注/约束 |
|---|---|---|---|---|---|---|
| `level` | string | 日志级别 | 否 | `"info"` | 启动时 | 支持 `debug/info/warn/error`，非法值启动失败 |
| `file` | string | 日志文件路径 | 否 | `""` | 启动时 | 空表示 stdout |
| `max_size_mb` | int | 单日志文件大小上限(MB) | 否 | `100` | `file` 非空时 | <=0 回退默认 |
| `max_backups` | int | 历史分卷数 | 否 | `5` | `file` 非空时 | <=0 回退默认 |
| `max_age_days` | int | 历史分卷天数 | 否 | `30` | `file` 非空时 | <=0 回退默认 |
| `compress` | bool | 历史分卷压缩 | 否 | `true` | `file` 非空时 | 未设置回退默认 |
| `traffic_stats_interval` | string(duration) | 流量统计输出周期 | 否 | `"1s"` | 日志模块 | 启动时解析，非法值启动失败 |
| `traffic_stats_sample_every` | int | 流量统计采样倍率 | 否 | `1` | 日志模块 | <=0 回退默认 |
| `payload_log_max_bytes` | int | payload 摘要默认截断长度 | 否 | `256` | receiver/task payload 日志 | <=0 回退默认 |
| `payload_pool_max_cached_bytes` | int64 | payload 内存池缓存上限 | 否 | `0` | payload pool | `<0` 校验失败；`0` 表示不限制 |

## 3.3 business_defaults

### 3.3.1 `business_defaults.task`

| 字段 | 类型 | 含义 | 必填 | 默认值 | 生效条件 | 备注 |
|---|---|---|---|---|---|---|
| `pool_size` | int | task worker 池大小默认值 | 否 | 无 | task `pool_size<=0` 时 | 若仍为空，最终回退 4096 |
| `queue_size` | int | task 排队上限默认值 | 否 | 无 | task `queue_size<=0` 时 | 若仍为空，最终回退 8192 |
| `channel_queue_size` | int | channel 模型队列默认值 | 否 | 无 | task `channel_queue_size<=0` 时 | 若仍为空，最终取 queue_size |
| `execution_model` | string | task 执行模型默认值 | 否 | 无 | task 未设置时 | 支持 `fastpath/pool/channel` |
| `payload_log_max_bytes` | int | task payload 摘要默认值 | 否 | 无 | task `payload_log_max_bytes<=0` | 若仍为空，最终取 logging 默认 |

### 3.3.2 `business_defaults.receiver`

| 字段 | 类型 | 含义 | 必填 | 默认值 | 生效条件 | 备注 |
|---|---|---|---|---|---|---|
| `multicore` | bool | gnet receiver multicore 默认值 | 否 | 无 | receiver 未设置 multicore | 若仍为空，最终默认 true |
| `num_event_loop` | int | gnet receiver event loop 默认值 | 否 | 无 | receiver `num_event_loop<=0` | 若仍为空，最终默认 `max(8, NumCPU)` |
| `payload_log_max_bytes` | int | receiver payload 摘要默认值 | 否 | 无 | receiver `payload_log_max_bytes<=0` | 若仍为空，最终取 logging 默认 |

### 3.3.3 `business_defaults.sender`

| 字段 | 类型 | 含义 | 必填 | 默认值 | 生效条件 | 备注 |
|---|---|---|---|---|---|---|
| `concurrency` | int | sender 并发默认值 | 否 | 无 | sender `concurrency<=0` | 若仍为空，最终默认 8 |

---

## 4. Business 配置详解

## 4.1 顶层字段

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `version` | int64 | 否 | 配置版本号，记录在启动日志和热更新日志中 |
| `receivers` | map[string]ReceiverConfig | 是 | receiver 定义表；selector 通过名字引用 |
| `senders` | map[string]SenderConfig | 是 | sender 定义表；task/route stage 通过名字引用 |
| `pipelines` | map[string][]StageConfig | 是 | pipeline 定义表；task 按顺序引用 |
| `selectors` | map[string]SelectorConfig | 是 | selector 定义表；按 receiver + source 特征返回 task 集 |
| `tasks` | map[string]TaskConfig | 是 | 业务任务定义；至少 1 个 |

## 4.2 receivers

- key 为 receiver 名称，供 `selector.receivers[]` 引用。
- `type` 决定其专属字段和校验逻辑。

## 4.3 senders

- key 为 sender 名称，供 `task.senders[]` 和 `route_offset_bytes_sender` 引用。
- `concurrency` 如显式设置且 >0，必须是 2 的幂。

## 4.4 pipelines

- key 为 pipeline 名称。
- value 为 stage 数组，按数组顺序执行。

## 4.5 selectors

- key 为 selector 名称。
- selector 绑定 `receivers + tasks`，可选附加 `source` 条件；命中后返回 task 集，而不是 bool。
- `source` 为空表示对应 receiver 下的 default selector。
- 当前支持 `source.src_cidrs`（单 IP 或 CIDR）与 `source.src_port_ranges`（单端口或范围）。

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `receivers` | []string | 是 | 不可为空，必须引用已定义 receiver |
| `tasks` | []string | 是 | 不可为空，必须引用已定义 task |
| `source.src_cidrs` | []string | 否 | 支持单 IP 或 CIDR；与端口条件组合时为 AND 语义 |
| `source.src_port_ranges` | []string | 否 | 支持 `8080` 或 `8000-8999`；与 IP 条件组合时为 AND 语义 |

## 4.6 tasks

- key 为 task 名称。
- 一个 task 绑定 pipelines -> senders，并选择执行模型。

| 字段 | 类型 | 必填 | 默认值 | 生效条件 | 备注/约束 |
|---|---|---|---|---|---|
| `pool_size` | int | 否 | 4096 | `execution_model=pool` 时用于 worker 数 | <=0 时回退默认 |
| `fast_path` | bool | 否 | false | 仅 `execution_model` 为空时参与兼容推导 | `true` 时推导为 `fastpath` |
| `execution_model` | string | 否 | `pool`（通过兼容规则） | 所有 task | 仅支持 `fastpath/pool/channel` |
| `queue_size` | int | 否 | 8192 | pool 模型 | <=0 回退默认 |
| `channel_queue_size` | int | 否 | 先取 `queue_size` | channel 模型 | `<0` 校验失败；`0` 则回退 |
| `pipelines` | []string | 否 | 空数组 | 所有 task | 可为空（表示直通） |
| `senders` | []string | 是 | - | 所有 task | 不可为空，必须引用已定义 sender |
| `log_payload_send` | bool | 否 | false | task payload 发送日志 | info 级日志下可见 |
| `payload_log_max_bytes` | int | 否 | 继承 logging 默认 256 | task payload 发送日志 | <=0 时回退 logging |

---

## 5. 多态配置分类型详解

## 5.1 Receiver 类型

### 5.1.1 `udp_gnet`

- 作用：UDP 高吞吐接入。
- 必填：`type`、`listen`。
- 生效字段：`multicore`、`num_event_loop`、`read_buffer_cap`、`socket_recv_buffer`、`log_payload_recv`、`payload_log_max_bytes`。
- 注意：`frame` 对 `udp_gnet` 不生效。

最小示例：

```json
{ "type": "udp_gnet", "listen": "0.0.0.0:19000" }
```

### 5.1.2 `tcp_gnet`

- 作用：TCP 接入，支持可选分帧。
- 必填：`type`、`listen`。
- `frame` 支持：`""`（不分帧）或 `"u16be"`。
- 其它 gnet 参数与 `udp_gnet` 相同。

最小示例：

```json
{ "type": "tcp_gnet", "listen": "0.0.0.0:19001", "frame": "u16be" }
```

### 5.1.3 `kafka`

- 作用：消费 Kafka topic。
- 必填：`type`、`listen`（brokers CSV）、`topic`。
- 可选：`group_id`、`client_id`、`start_offset`、`fetch_*`、`tls*`、`sasl*`。
- 默认行为：
  - `group_id` 为空时自动设为 `forward-stub-<receiver_name>`。
  - `fetch_min_bytes` 默认 1、`fetch_max_bytes` 默认 16MiB、`fetch_max_wait_ms` 默认 100。
- 约束：
  - `start_offset` 仅 `earliest/latest`。
  - Kafka 鉴权只支持 `PLAIN`；若提供账号密码但未写 `sasl_mechanism`，内部按 `PLAIN` 处理。

最小示例：

```json
{ "type": "kafka", "listen": "127.0.0.1:9092", "topic": "input-topic" }
```

### 5.1.4 `sftp`

- 作用：轮询远端目录，按 chunk 读文件并转成 `file_chunk` packet。
- 必填：`type`、`listen`、`username`、`password`、`remote_dir`、`host_key_fingerprint`。
- 可选：`poll_interval_sec`（默认 5）、`chunk_size`（默认 65536，最小 1024）。
- 约束：
  - `host_key_fingerprint` 必须是合法 SSH SHA256 指纹。

最小示例：

```json
{
  "type": "sftp",
  "listen": "127.0.0.1:22",
  "username": "demo",
  "password": "demo",
  "remote_dir": "/input",
  "host_key_fingerprint": "SHA256:W5M5Qf3jQ8jD8I2LqzY9zT6QfPj1O9g3k8xw0Jm9r3A"
}
```

## 5.2 Sender 类型

### 5.2.1 `udp_unicast`

- 作用：UDP 单播发送。
- 必填：`type`、`remote`、`local_port`。
- 可选：`local_ip`（空则 `0.0.0.0`）、`socket_send_buffer`、`concurrency`。

最小示例：

```json
{ "type": "udp_unicast", "remote": "127.0.0.1:21000", "local_port": 20000 }
```

### 5.2.2 `udp_multicast`

- 作用：UDP 组播发送。
- 必填：`type`、`remote`、`local_port`。
- 可选：`local_ip`、`iface`、`ttl`、`loop`、`socket_send_buffer`、`concurrency`。
- 默认：`ttl<=0` 时内部回退为 1。

最小示例：

```json
{ "type": "udp_multicast", "remote": "239.0.0.10:21001", "local_port": 20001 }
```

### 5.2.3 `tcp_gnet`

- 作用：TCP 发送。
- 必填：`type`、`remote`。
- `frame` 支持：`""` / `"none"` / `"u16be"`。

最小示例：

```json
{ "type": "tcp_gnet", "remote": "127.0.0.1:21002", "frame": "u16be" }
```

### 5.2.4 `kafka`

- 作用：Kafka 生产发送。
- 必填：`type`、`remote`（brokers CSV）、`topic`。
- 可选：`acks`、`idempotent`、`retries`、`max_in_flight_requests_per_connection`、`linger_ms`、`batch_max_bytes`、`max_buffered_bytes`、`max_buffered_records`、`compression`、`client_id`、`tls*`、`sasl*`、`concurrency`。
- 默认行为：
  - `acks` 默认按 `all`（`-1`）处理。
  - `idempotent` 默认 `true`（即启用 Kafka 幂等写入）。
  - `retries<=0` 使用 franz-go 默认值（近似无限重试）。
  - `max_in_flight_requests_per_connection<=0` 使用 franz-go 默认值；在 `idempotent=true` 下等价为 1。
  - `linger_ms<=0` 回退 1ms。
  - `batch_max_bytes<=0` 回退 1MiB。
  - `max_buffered_bytes<=0` 使用 franz-go 默认值（不限制）。
  - `max_buffered_records<=0` 使用 franz-go 默认值（10000）。
  - `compression` 空或 `none` 表示不压缩。
- `acks` 允许值与语义：
  - `0`：不等待 broker 确认，最低时延、最低可靠性。
  - `1`：仅等待 leader 确认。
  - `all` 或 `-1`：等待 ISR 全部副本确认，可靠性最高。
- 约束（与 Kafka/franz-go 语义对齐）：
  - `idempotent=true` 时，`acks` 必须是 `all`（或 `-1`）。
  - `idempotent=true` 时，不额外强制 `max_in_flight_requests_per_connection` 的具体取值；`<=0` 仍表示使用 franz-go 默认值。
  - `retries`、`max_in_flight_requests_per_connection`、`max_buffered_bytes`、`max_buffered_records` 必须 `>=0`。
- 与 franz-go 的映射：
  - `max_buffered_bytes` -> `kgo.MaxBufferedBytes`（与 Java producer `buffer.memory` 语义近似对应，按字节限制 producer 缓冲）。
  - `max_buffered_records` -> `kgo.MaxBufferedRecords`（按 record 条数限制 producer 缓冲）。
  - 达到上限时，franz-go 会阻塞后续 produce，直到已缓冲记录被发送完成（非手动 flush 模式）。

最小示例：

```json
{ "type": "kafka", "remote": "127.0.0.1:9092", "topic": "output-topic", "acks": "all", "idempotent": true }
```

### 5.2.5 `sftp`

- 作用：把 `file_chunk` packet 组装并写入 SFTP，最终 rename 到正式文件。
- 必填：`type`、`remote`、`username`、`password`、`remote_dir`、`host_key_fingerprint`。
- 可选：`temp_suffix`（默认 `.part`）、`concurrency`。

最小示例：

```json
{
  "type": "sftp",
  "remote": "127.0.0.1:22",
  "username": "demo",
  "password": "demo",
  "remote_dir": "/output",
  "host_key_fingerprint": "SHA256:W5M5Qf3jQ8jD8I2LqzY9zT6QfPj1O9g3k8xw0Jm9r3A"
}
```

## 5.3 Pipeline Stage 类型

### 5.3.1 `match_offset_bytes`

- 作用：匹配 payload 指定偏移处字节，不匹配则丢弃（stage 返回 false）。
- 字段：`offset`、`hex`（hex 字符串）。

### 5.3.2 `replace_offset_bytes`

- 作用：把 payload 指定偏移处字节替换为 `hex`。
- 字段：`offset`、`hex`。

### 5.3.3 `mark_as_file_chunk`

- 作用：把 stream payload 标记为 file chunk 语义。
- 字段：`path`（可选默认路径）、`bool`（EOF 标记，默认 true）。

### 5.3.4 `clear_file_meta`

- 作用：清理文件元信息并转回 stream。
- 字段：无。

### 5.3.5 `route_offset_bytes_sender`

- 作用：按 payload 偏移字节路由到指定 sender。
- 字段：`offset`、`cases`、`default_sender`。
- 约束：
  - `cases` 必须非空；key 必须是合法 hex。
  - 所有 case key 解码后长度必须一致。
  - `cases/default_sender` 中引用的 sender 必须在当前 task 的 `senders` 列表内。

最小示例：

```json
{
  "type": "route_offset_bytes_sender",
  "offset": 0,
  "cases": { "01": "tx_udp" },
  "default_sender": "tx_kafka"
}
```

## 5.4 Task execution_model 类型

### 5.4.1 `fastpath`

- 作用：在调用协程内同步处理，最低延迟。
- 适用：计算轻、链路短、并发可控。

### 5.4.2 `pool`

- 作用：worker pool + 有界排队（`pool_size` + `queue_size`）。
- 适用：通用高吞吐场景。

### 5.4.3 `channel`

- 作用：单 goroutine + 有界 channel（`channel_queue_size`）。
- 适用：需要 task 内顺序处理的场景。

兼容说明：

- `execution_model` 为空时：`fast_path=true` -> `fastpath`，否则 `pool`。

---

## 6. 关系与约束说明

## 6.1 引用关系

- `selector.receivers[]` 必须引用已定义的 `receivers` key。
- `selector.tasks[]` 必须引用已定义的 `tasks` key。
- `task.pipelines[]` 必须引用已定义的 `pipelines` key。
- `task.senders[]` 必须引用已定义的 `senders` key。

## 6.2 type 生效字段

- receiver/sender 的大多数字段只对特定 `type` 生效（见 5.x）。
- 例如 `receiver.frame` 仅对 `tcp_gnet` 生效；`sender.iface/ttl/loop` 仅对 `udp_multicast` 生效。

## 6.3 互斥/条件规则

- CLI 参数：
  - 使用双文件模式时 `-system-config` 与 `-business-config` 必须同时提供。
  - 或者只提供 `-config` 使用 legacy。
- `sender.concurrency`：显式设置时若 >0 必须是 2 的幂。
- `control.pprof_port`：允许 `-1`（禁用）、`0`（默认 6060）或 1~65535。

## 6.4 缺失导致校验失败的常见项

- 无 task。
- task 缺 receiver/sender。
- task 引用了不存在的 receiver/sender/pipeline。
- Kafka receiver 缺 `listen/topic`。
- SFTP receiver 缺 `listen/username/password/remote_dir/host_key_fingerprint`。
- Kafka sender 缺 `remote/topic`。
- SFTP sender 缺 `remote/username/password/remote_dir/host_key_fingerprint`。

---

## 7. 全量示例

- system 全量示例：见 `configs/system.example.json`
- business 全量示例：见 `configs/business.example.json`
- legacy 对照示例：见 `configs/example.json`

---

## 8. 注意事项 / 常见误区

1. **不要把未知字段写进 JSON**：解析器开启了严格模式，未知字段会直接失败。
2. **不要假设 business_defaults 会覆盖显式值**：只补空缺，不会覆盖业务显式配置。
3. **`channel_queue_size` 不是全局队列**：仅 `execution_model=channel` 时使用。
4. **route stage 目标 sender 必须在 task.senders 里**：否则校验失败。
5. **SFTP 指纹必须正确**：格式正确但值不匹配也会在连接时失败。
