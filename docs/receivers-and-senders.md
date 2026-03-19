# Receiver 与 Sender 配置详解

本文从**协议类型**而不是顶层 JSON 结构出发，解释每种 receiver / sender 的有效字段、match key、默认值来源和注意事项。

## 1. Receiver：负责接入并构造 match key

### 1.1 UDP gnet receiver

- `type=udp_gnet`
- 关键字段：`listen`、`selector`
- 可选字段：`multicore`、`num_event_loop`、`read_buffer_cap`、`socket_recv_buffer`、`log_payload_recv`、`payload_log_max_bytes`
- `match key` 生成规则：`udp|src_addr=<源地址>`

适用说明：

- `multicore`、`num_event_loop`、`read_buffer_cap`、`socket_recv_buffer` 都只对 gnet 类型生效。
- `socket_recv_buffer` 默认会被回写成 `1073741824`，但只有 gnet receiver 真正消费它。

### 1.2 TCP gnet receiver

- `type=tcp_gnet`
- 关键字段：`listen`、`selector`
- 额外字段：`frame`
- `match key` 生成规则：`tcp|src_addr=<源地址>`

`frame` 说明：

- 空字符串：不拆帧，收到多少就交给上游多少。
- `u16be`：按 2 字节大端长度前缀拆帧。
- receiver 侧不支持 `none`，`none` 只是 sender 侧为兼容空字符串提供的别名。

### 1.3 Kafka receiver

- `type=kafka`
- 关键字段：`listen`、`selector`、`topic`
- `group_id` 可选，未配置时自动生成为 `forward-stub-<receiver_name>`。
- `match key` 生成规则：`kafka|topic=<topic>|partition=<partition>`

#### 1.3.1 Kafka receiver 常用配置分组

**连接与元数据**

- `dial_timeout`
- `conn_idle_timeout`
- `metadata_max_age`
- `retry_backoff`
- `client_id`
- `tls`
- `tls_skip_verify`
- `username` / `password` / `sasl_mechanism`

**消费组行为**

- `group_id`
- `session_timeout`
- `heartbeat_interval`
- `rebalance_timeout`
- `balancers`
- `auto_commit`
- `auto_commit_interval`

**拉取行为**

- `start_offset`
- `fetch_min_bytes`
- `fetch_max_bytes`
- `fetch_max_partition_bytes`
- `fetch_max_wait_ms`
- `isolation_level`

#### 1.3.2 Kafka receiver 需要特别注意的点

- `start_offset` 仅在没有已提交位点时更有意义；配置值只允许 `earliest` / `latest`。
- `heartbeat_interval` 必须小于 `session_timeout`。
- `auto_commit=false` 时不能再配置 `auto_commit_interval`。
- `fetch_max_partition_bytes` 最好不要大于 `fetch_max_bytes`。
- 当前未提供 `request_timeout`、`commit_timeout`、`fetch_max_records` 等“看起来常见但代码没有一一对应安全映射”的字段。

### 1.4 SFTP receiver

- `type=sftp`
- 关键字段：`listen`、`selector`、`username`、`password`、`remote_dir`、`host_key_fingerprint`
- `match key` 生成规则：`sftp|remote_dir=<remote_dir>|file_name=<文件名>`

#### 1.4.1 运行时行为

- `poll_interval_sec` 未配置时按 5 秒轮询。
- `chunk_size` 未配置时按 64 KiB 读文件；若配置小于 1024，则自动抬升到 1024。
- 读取出的 packet 会携带 `file_name`、`file_path`、`transfer_id`、`offset`、`total_size`、`checksum`、`EOF` 等文件分块元数据。

## 2. Sender：负责最终输出

### 2.1 UDP 单播 sender

- `type=udp_unicast`
- 必填：`remote`、`local_port`
- 常用：`local_ip`、`concurrency`、`socket_send_buffer`

行为说明：

- 每个 shard 维护一个固定 socket；同 shard 内天然保序。
- `local_ip` 为空时运行时回退到 `0.0.0.0`。
- `concurrency` 如果显式配置为正数，必须是 2 的幂。

### 2.2 UDP 组播 sender

- `type=udp_multicast`
- 必填：`remote`、`local_port`
- 常用：`local_ip`、`iface`、`ttl`、`loop`、`concurrency`、`socket_send_buffer`

行为说明：

- `ttl<=0` 时运行时会回退为 `1`。
- `iface` 用于多网卡主机上绑定正确的组播出接口。
- `loop` 打开后，本机也能收到自己发出的组播包。

### 2.3 TCP sender

- `type=tcp_gnet`
- 必填：`remote`
- 常用：`frame`、`concurrency`、`socket_send_buffer`

`frame` 说明：

- `""` 或 `none`：直接发送原始 payload。
- `u16be`：发送前加 2 字节大端长度头。

### 2.4 Kafka sender

- `type=kafka`
- 必填：`remote`、`topic`
- 其余字段基本分四类：连接、可靠性、批处理/缓冲、分区与 key。

#### 2.4.1 连接与鉴权

- `dial_timeout`
- `request_timeout`
- `retry_timeout`
- `retry_backoff`
- `conn_idle_timeout`
- `metadata_max_age`
- `client_id`
- `tls`
- `tls_skip_verify`
- `username` / `password` / `sasl_mechanism`

#### 2.4.2 可靠性与吞吐

- `acks`
- `idempotent`
- `retries`
- `max_in_flight_requests_per_connection`
- `linger_ms`
- `batch_max_bytes`
- `max_buffered_bytes`
- `max_buffered_records`
- `compression`
- `compression_level`

联动规则：

- `idempotent=true` 时，`acks` 必须是 `all/-1`。
- `max_in_flight_requests_per_connection` 在幂等开启时通常应保持 `1`。
- `linger_ms`、`batch_max_bytes`、压缩、缓冲上限一起决定吞吐、CPU 与内存的平衡。

#### 2.4.3 分区与 record key

- `partitioner=sticky`：粘性分区。
- `partitioner=round_robin`：轮询分区。
- `partitioner=hash_key`：基于 key 哈希，因此必须提供 `record_key` 或 `record_key_source`。

`record_key_source` 当前支持：

- `payload`
- `match_key`
- `remote`
- `local`
- `file_name`
- `file_path`
- `transfer_id`
- `route_sender`

当前**不支持**：

- 从 payload 解析 JSONPath。
- 自定义模板字符串。
- 表达式或脚本。

### 2.5 SFTP sender

- `type=sftp`
- 必填：`remote`、`username`、`password`、`remote_dir`、`host_key_fingerprint`
- 可选：`temp_suffix`、`concurrency`

行为说明：

- 未配置 `temp_suffix` 时，运行时默认使用 `.part`。
- sender 会先把 chunk 写入临时文件，直到收到 EOF 且写满总长度后再 rename 为正式文件。
- 如果 packet 带了 `checksum`，sender 会先做校验，不一致会直接报错。

## 3. receiver 与 sender 之间的关系

- receiver 负责**接入 + 产生 match key**。
- selector 负责**match key -> task_set**。
- task 负责**pipeline + sender fan-out**。
- sender 负责**最终输出**。

也就是说：

- receiver **不会**直接绑定 task。
- sender **不会**参与 selector 匹配。
- route stage 只会影响 task 内部的 sender 选择，不会影响 receiver 到 task 的主路由。

## 4. payload 日志配置在哪一层生效

### receiver 侧

- 开关：`receiver.log_payload_recv`
- 截断：`receiver.payload_log_max_bytes`
- 回退：若未配置或 `<=0`，回退到 `logging.payload_log_max_bytes`

### task 侧

- 开关：`task.log_payload_send`
- 截断：`task.payload_log_max_bytes`
- 回退：若未配置或 `<=0`，回退到 `logging.payload_log_max_bytes`

## 5. 协议专属配置不要混用

虽然 `Config` 结构体把多种协议字段放在同一个 `ReceiverConfig` / `SenderConfig` 中，但实际只有对应 `type` 会消费对应字段：

- `frame` 只对 TCP 生效。
- `multicore` / `num_event_loop` / `read_buffer_cap` / `socket_recv_buffer` 只对 gnet receiver 生效。
- `local_ip` / `local_port` / `iface` / `ttl` / `loop` 只对 UDP sender 生效。
- Kafka timeout / acks / balancers / key / compression 只对 Kafka 生效。
- `remote_dir` / `temp_suffix` / `host_key_fingerprint` 只对 SFTP 生效。
