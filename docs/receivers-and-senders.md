# Receiver 与 Sender 配置详解

本文从**协议类型**而不是顶层 JSON 结构出发，解释每种 receiver / sender 的有效字段、match key、默认值来源和注意事项。

补充说明：`match_key` 已下沉到 receiver 自身实现。各协议会在初始化阶段把 `match_key.mode` 编译成专用 builder；UDP/TCP/Kafka/SFTP/OSS 都是在各自 receiver 构建阶段完成这件事，避免热路径继续经过统一公共拼接函数。

## 1. Receiver：负责接入并构造 packet + match key

补充说明：

- receiver 在构造 packet metadata 时，会同时写入 `receiver_name` 与 `match_key`。
- `receiver_name` 不参与 selector 主路由。

### 1.1 UDP gnet receiver

- `type=udp_gnet`
- 关键字段：`listen`、`selector`
- 可选字段：`multicore`、`num_event_loop`、`read_buffer_cap`、`socket_recv_buffer`、`log_payload_recv`、`payload_log_max_bytes`
- `match key` 默认保持兼容输出：`udp|src_addr=<源地址>`。
- `match_key.mode` 支持：`remote_addr`、`remote_ip`、`local_addr`、`local_ip`、`fixed`。
- receiver 统计对象在 `Start()` 时创建，真正收到报文后才开始 `AddBytes()`。

适用说明：

- `multicore`、`num_event_loop`、`read_buffer_cap`、`socket_recv_buffer` 都只对 gnet 类型生效。
- `socket_recv_buffer` 默认会被回写成 `1073741824`，但只有 gnet receiver 真正消费它。

### 1.2 TCP gnet receiver

- `type=tcp_gnet`
- 关键字段：`listen`、`selector`
- 额外字段：`frame`
- `match key` 默认保持兼容输出：`tcp|src_addr=<源地址>`。
- `match_key.mode` 支持：`remote_addr`、`remote_ip`、`local_addr`、`local_port`、`fixed`。
- receiver 统计对象在 `Start()` 时创建；每帧切出后再按帧累计字节。

`frame` 说明：

- 空字符串：不拆帧，收到多少就交给上游多少。
- `u16be`：按 2 字节大端长度前缀拆帧。
- receiver 侧不支持 `none`，`none` 只是 sender 侧为兼容空字符串提供的别名。

### 1.3 Kafka receiver

- `type=kafka`
- 关键字段：`listen`、`selector`、`topic`
- `group_id` 可选，未配置时自动生成为 `forward-stub-<receiver_name>`。
- `match key` 默认保持兼容输出：`kafka|topic=<topic>|partition=<partition>`。
- `match_key.mode` 支持：`topic`、`topic_partition`、`fixed`。

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

#### 1.3.2 Kafka receiver 默认值、启动与热重载

- `dial_timeout`、`conn_idle_timeout`、`metadata_max_age`、`retry_backoff`、`session_timeout`、`heartbeat_interval`、`rebalance_timeout`、`balancers`、`auto_commit`、`auto_commit_interval`、`fetch_max_partition_bytes`、`isolation_level` 会先在统一默认值入口回写。
- `group_id`、`fetch_min_bytes`、`fetch_max_bytes`、`fetch_max_wait_ms` 则在 `NewKafkaReceiver()` 中回退并映射到 `kgo`。
- `Start()` 时会创建 receiver 统计对象；真正处理到 record 后，才按 `record.Value` 长度累计字节。
- 热重载时，只要 Kafka receiver 配置发生变化，就会重建 `kgo.Client`、重新编译 match key builder，并按“新实例先启动、旧实例后停止”的顺序切换。

#### 1.3.3 Kafka receiver 需要特别注意的点

- `start_offset` 仅在没有已提交位点时更有意义；配置值只允许 `earliest` / `latest`。
- `heartbeat_interval` 必须小于 `session_timeout`。
- `auto_commit=false` 时不能再配置 `auto_commit_interval`。
- `fetch_max_partition_bytes` 最好不要大于 `fetch_max_bytes`。
- 当前未提供 `request_timeout`、`commit_timeout`、`fetch_max_records` 等“看起来常见但代码没有一一对应安全映射”的字段。

### 1.4 SFTP receiver

- `type=sftp`
- 关键字段：`listen`、`selector`、`username`、`password`、`remote_dir`、`host_key_fingerprint`
- `match key` 默认保持兼容输出：`sftp|remote_dir=<remote_dir>|file_name=<文件名>`。
- `match_key.mode` 支持：`remote_path`、`filename`、`fixed`。

#### 1.4.1 运行时行为

- `poll_interval_sec` 未配置时按 5 秒轮询。
- `chunk_size` 未配置时按 64 KiB 读文件；若配置小于 1024，则自动抬升到 1024。
- 读取出的 packet 会携带 `file_name`、`file_path`、`transfer_id`、`offset`、`total_size`、`checksum`、`EOF` 等文件分块元数据。


### 1.5 SkyDDS receiver

### 1.5 OSS receiver

- `type=oss`
- 关键字段：`endpoint`、`bucket`、`access_key`、`secret_key`、`selector`
- 可选字段：`region`、`use_ssl`、`force_path_style`、`prefix`、`poll_interval_sec`
- `match key` 默认输出：`oss|bucket=<bucket>|key=<object key>`。
- `match_key.mode` 支持：`remote_path`、`filename`、`fixed`。

运行时行为：

- 按 `bucket + prefix` 轮询对象，跳过目录占位对象。
- 对象列表按 key 排序，保证处理顺序稳定。
- 对每个对象调用 `GetObject` 流式读取，并按 `chunk_size` 输出 `file_chunk`；`chunk_size` 可省略或配置为 `0`，运行时默认 64 KiB，若显式配置小于 1024 会自动抬升到 1024。
- packet meta 会写入 `ProtoOSS`、`Remote=object key`、`Local=bucket@endpoint`、`FileName=path.Base(key)`、`FilePath=key`、`TransferID=bucket|key|size|etag`。
- 去重指纹包含 `key + size + last_modified + etag`，避免只按对象名导致覆盖对象漏处理。

### 1.6 SkyDDS receiver

- `type=dds_skydds`
- 必填：`selector`、`dcps_config_file`、`domain_id`、`topic_name`、`message_model`
- `message_model` 支持 `octet` 与 `batch_octet`
- 可选：`wait_timeout`、`drain_max_items`
  - `wait_timeout`：通知等待超时（duration，默认 `500ms`）
  - `drain_max_items`：每次 drain 拉取上限（正整数，默认 `2048`）
- `match_key.mode` 仅支持留空（默认 `skydds|topic_name=<topic>`）或 `fixed`
- 接收模型：C++ DataReader listener 入队并通知，Go 侧 `Wait(wait_timeout)` 被唤醒后执行 `Drain(drain_max_items)`；无论 `octet` 还是 `batch_octet`，进入 runtime 前都逐条 packet 下发。
- 说明：当前主数据面不使用“Go 侧纯轮询 Poll/PollBatch”，也不使用“每条消息 direct callback Go”。

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
- 热重载时，只要 Kafka sender 配置变化，就会新建 sender，并联动重建引用该 sender 的 task。

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
- sender 优先使用 `TargetFilePath`，为空时回退 `FilePath`，再回退 `FileName`；最终路径会清洗为安全相对路径并拼到 `remote_dir` 下。
- 远端 rename 成功后，才会执行 `notify_on_success`。

### 2.6 OSS sender

- `type=oss`
- 必填：`endpoint`、`bucket`、`access_key`、`secret_key`
- 可选：`region`、`use_ssl`、`force_path_style`、`key_prefix`、`storage_class`、`content_type`、`notify_on_success`

行为说明：

- 只接受 `PayloadKindFileChunk`。
- 同一个 `transfer_id` 对应一个 multipart upload session。
- 不把完整文件先落到本地；上游小 chunk 会在内存中有界聚合为 multipart part。
- `part_size` 未配置或配置为 `<=0` 时，在统一默认值阶段回写为 `5242880`（5 MiB）。
- chunk 进入时校验 `checksum`，不一致立即报错。
- 重复 offset 区间不会破坏最终对象；乱序 chunk 会在有界缓存内等待连续区间。
- EOF 到达、覆盖范围完整、待上传缓冲 flush 完成、所有 part 成功上传后，才调用 `CompleteMultipartUpload`。
- `CompleteMultipartUpload` 成功后，才会执行 `notify_on_success`。

object key 生成规则：

1. packet 中已有 `TargetFilePath` 时优先使用。
2. 否则使用 `key_prefix + FilePath`。
3. `FilePath` 为空时回退 `key_prefix + FileName`。
4. 当 `TargetFilePath` 为空但 `TargetFileName` 非空时，会用 `TargetFileName` 覆盖来源路径的 basename。

路径会先清洗为安全相对路径，拒绝空 key 与 `..`。

### 2.7 SkyDDS sender

- `type=dds_skydds`
- 必填：`dcps_config_file`、`domain_id`、`topic_name`、`message_model`
- `message_model` 支持 `octet` 与 `batch_octet`
- 通过 C ABI + C++ wrapper 调用 SkyDDS `DataWriter` 写 `OctetMsg` / `BatchOctetMsg`
- 当 `message_model=batch_octet` 时必须配置 `batch_num`/`batch_size`/`batch_delay`，并按三阈值（条数/字节/等待时长）触发 flush

### 2.8 文件提交成功通知

`notify_on_success` 是文件型 sender 的 commit success 后置动作，不是 task 普通 sender fan-out。

触发点：

- SFTP：远端临时文件 rename 为最终文件成功后。
- OSS：multipart upload complete 成功后。

支持通知类型：

- `type=kafka`：发送一条 JSON `file_ready` 事件到 Kafka，支持 `remote`、`topic`、`record_key_source`、Kafka TLS/SASL 与 timeout 字段。
- `type=dds_skydds`：发送一条 JSON `file_ready` 事件到 SkyDDS，第一版只支持 `message_model=octet`。

配置兼容两种写法：

- 单对象：`"notify_on_success": {"type":"kafka", ...}`
- 数组：`"notify_on_success": [{"type":"kafka", ...}, {"type":"dds_skydds", ...}]`

通知事件包含来源协议/路径、目标协议/最终路径、`transfer_id`、文件大小、sender/receiver 名称、ready 时间，以及接收方取文件所需定位信息：

- SFTP：`fetch_protocol=sftp`、`fetch_host`、`fetch_port`、`fetch_path`
- OSS：`fetch_protocol=oss`、`fetch_endpoint`、`fetch_bucket`、`fetch_key`

如果文件已经 commit 成功但通知失败，sender 会返回错误并打印清晰日志；代码中预留了后续持久化补发扩展点。

### 2.9 文件名/路径改写 stage

文件型 pipeline 可改写 `TargetFileName` / `TargetFilePath`，保留来源字段 `FileName` / `FilePath` 不变。sender 优先使用 `Target*`，为空时回退 `File*`。

支持 stage：

- `set_target_file_path`：用 `value` 直接设置目标路径。
- `rewrite_target_path_strip_prefix`：用 `prefix` 从当前目标路径或来源路径去掉前缀。
- `rewrite_target_path_add_prefix`：用 `prefix` 给目标路径加前缀。
- `rewrite_target_filename_replace`：用 `old` / `new` 做文件名简单替换。
- `rewrite_target_path_regex`：用 `pattern` / `replacement` 正则改写路径。
- `rewrite_target_filename_regex`：用 `pattern` / `replacement` 正则改写文件名，并同步更新目标路径 basename。

边界语义：

- 路径 stage 优先读取当前 `TargetFilePath`，为空时读取来源 `FilePath`。
- 文件名 stage 优先读取当前 `TargetFileName`，为空时读取来源 `FileName`，再为空则取当前路径 basename。
- strip prefix 不匹配时保留原路径内容，但会去掉开头 `/`。
- regex 不匹配时保留当前值。
- `rewrite_target_filename_*` 会同步替换 `TargetFilePath` 的 basename，避免文件名与路径末段不一致。

## 3. receiver 与 sender 之间的关系

- receiver 负责**接入 + 产生 match key**。
- selector 负责**match key -> task_set**。
- task 负责**pipeline + sender fan-out**。
- sender 负责**最终输出**。

也就是说：

- receiver **不会**直接绑定 task。
- sender **不会**参与 selector 匹配。
- route stage 只会影响 task 内部的 sender 选择，不会影响 receiver 到 task 的主路由。

补充（职责边界）：

- SFTP receiver 负责读取远端文件并产出 `file_chunk`。
- SFTP sender 负责接收 `file_chunk` 并落盘。
- OSS receiver/sender 与 SFTP 一样走文件分块语义，但 OSS sender 使用 multipart upload 提交。
- pipeline 只负责 task 内部按当前支持的 stage 处理 packet。

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
- `endpoint` / `bucket` / `key_prefix` / `part_size` / `storage_class` / `content_type` 只对 OSS 生效。
- `notify_on_success` 只对文件型 sender 的 commit success 后置通知生效。
