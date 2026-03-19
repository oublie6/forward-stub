# Receivers 与 Senders

## 1. receiver 新职责

每类 receiver 都必须做两件事：

1. 生成 `packet.Packet`
2. 生成唯一 `match key`

### UDP / TCP

- 使用源地址生成 key。
- 示例：`udp|src_addr=1.1.1.1:9000`
- 示例：`tcp|src_addr=1.1.1.1:9000`

### Kafka

- receiver 显式使用 `topic + partition` 生成 key。
- 示例：`kafka|topic=order|partition=3`
- 不再依赖 `Remote` 像地址一样被猜测。
- 可直接配置 `dial_timeout`、`conn_idle_timeout`、`metadata_max_age`、`retry_backoff`、`session_timeout`、`heartbeat_interval`、`rebalance_timeout`、`balancers`、`auto_commit`、`auto_commit_interval`、`fetch_max_partition_bytes`、`isolation_level`。
- 其中 `balancers` 当前支持 `range`、`round_robin`、`cooperative_sticky`；`isolation_level` 当前支持 `read_uncommitted`、`read_committed`。

### SFTP

- receiver 显式使用 `remote_dir + file_name` 生成 key。
- 示例：`sftp|remote_dir=/input|file_name=a.txt`
- 不再依赖 file path 被误当作地址推断。

## 2. sender 职责

sender 没有架构性变化，仍然只负责最终输出：

- UDP / TCP 网络发送
- Kafka produce
- SFTP 文件输出

Kafka sender 当前额外支持直接映射到 franz-go / kgo 的以下配置：

- 连接与重试：`dial_timeout`、`request_timeout`、`retry_timeout`、`retry_backoff`、`conn_idle_timeout`、`metadata_max_age`
- 分区与 key：`partitioner`、`record_key`、`record_key_source`
- 压缩：`compression_level`（仅 `gzip` / `lz4` / `zstd`）

说明：

- `partitioner=sticky` 使用粘性分区；
- `partitioner=round_robin` 使用轮询分区；
- `partitioner=hash_key` 使用 key 哈希分区，因此必须配合 `record_key` 或 `record_key_source`；
- `record_key_source` 只做现有字段直取，不做 payload 解析 DSL。

## 3. packet.Meta 的变化

`packet.Meta` 新增 `MatchKey` 字段，用于把 receiver 构造出的唯一匹配串带入 runtime / selector。
