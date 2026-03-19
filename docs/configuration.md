# 配置模型

## 1. 总体结构

### system config

系统配置只包含：

- `control`
- `logging`
- `business_defaults`

### business config

业务配置包含：

- `version`
- `receivers`
- `selectors`
- `task_sets`
- `senders`
- `pipelines`
- `tasks`

## 2. logging

与本次可观测性增强直接相关的配置项：

- `traffic_stats_interval`：流量统计日志周期
- `traffic_stats_sample_every`：流量统计采样倍率
- `payload_log_max_bytes`：payload 摘要默认截断长度
- `payload_pool_max_cached_bytes`：payload 池最大缓存字节数
- `gc_stats_log_enabled`：是否开启 GC 周期日志
- `gc_stats_log_interval`：GC 周期日志输出间隔，必须是大于 0 的合法 `time.ParseDuration` 字符串

推荐：生产默认关闭 GC 周期日志，排障或容量评估窗口再开启。

## 3. receivers

每个 receiver 都必须显式绑定一个 selector：

```json
"receivers": {
  "rx_udp": {
    "type": "udp_gnet",
    "listen": "0.0.0.0:19000",
    "selector": "sel_ingress"
  }
}
```

关键点：

- `selector` 是 receiver 到 selector 的唯一绑定。
- receiver 不再由 task 反向引用。

### Kafka receiver 直接映射配置

以下字段仅对 `type=kafka` 生效，且都直接映射到 franz-go / kgo：

- `dial_timeout` -> `kgo.DialTimeout`
- `conn_idle_timeout` -> `kgo.ConnIdleTimeout`
- `metadata_max_age` -> `kgo.MetadataMaxAge`
- `retry_backoff` -> `kgo.RetryBackoffFn`
- `session_timeout` -> `kgo.SessionTimeout`
- `heartbeat_interval` -> `kgo.HeartbeatInterval`
- `rebalance_timeout` -> `kgo.RebalanceTimeout`
- `balancers` -> `kgo.Balancers`
- `auto_commit` -> `kgo.DisableAutoCommit` / 自动提交开关
- `auto_commit_interval` -> `kgo.AutoCommitInterval`
- `fetch_max_partition_bytes` -> `kgo.FetchMaxPartitionBytes`
- `isolation_level` -> `kgo.FetchIsolationLevel`

默认值：

- `dial_timeout`: `10s`
- `conn_idle_timeout`: `30s`
- `metadata_max_age`: `5m`
- `retry_backoff`: `250ms`
- `session_timeout`: `45s`
- `heartbeat_interval`: `3s`
- `rebalance_timeout`: `1m`
- `balancers`: `["cooperative_sticky"]`
- `auto_commit`: `true`
- `auto_commit_interval`: `5s`
- `fetch_max_partition_bytes`: `1048576`
- `isolation_level`: `read_uncommitted`

注意事项：

- `heartbeat_interval` 必须小于 `session_timeout`。
- `auto_commit=false` 时不能再设置 `auto_commit_interval`。
- `balancers` 当前支持 `range`、`round_robin`、`cooperative_sticky`。
- `fetch_max_partition_bytes` 不应大于 `fetch_max_bytes`。

## 4. selectors

selector 只支持完整字符串精确匹配：

```json
"selectors": {
  "sel_ingress": {
    "matches": {
      "udp|src_addr=10.0.0.10:9000": "ts_realtime",
      "tcp|src_addr=10.0.0.10:9000": "ts_realtime"
    },
    "default_task_set": "ts_realtime"
  }
}
```

规则说明：

- `matches`：`match key -> task_set_name`
- `default_task_set`：未命中时回退
- 不支持通配符、表达式、字段推断、规则优先级系统

## 5. task_sets

```json
"task_sets": {
  "ts_realtime": ["task_parse", "task_forward"],
  "ts_archive": ["task_archive"]
}
```

约束：

- task set 必须引用已存在的 task。
- 空 task set 不允许。
- 运行时会直接展开成 `match key -> []*TaskState`。

## 6. tasks

`task` 只定义执行逻辑：

```json
"tasks": {
  "task_forward": {
    "pipelines": ["pipe_route_sender"],
    "senders": ["tx_udp", "tx_kafka"],
    "execution_model": "pool"
  }
}
```

现在 task 不再承担 receiver 绑定职责。

## 6.1 Kafka sender 直接映射配置

以下字段仅对 `type=kafka` 生效，且都直接映射到 franz-go / kgo：

- `dial_timeout` -> `kgo.DialTimeout`
- `request_timeout` -> `kgo.ProduceRequestTimeout`
- `retry_timeout` -> `kgo.RetryTimeout`
- `retry_backoff` -> `kgo.RetryBackoffFn`
- `conn_idle_timeout` -> `kgo.ConnIdleTimeout`
- `metadata_max_age` -> `kgo.MetadataMaxAge`
- `partitioner` -> `kgo.RecordPartitioner`
- `record_key` -> `kgo.Record.Key`
- `record_key_source` -> `kgo.Record.Key`
- `compression_level` -> `kgo.CompressionCodec.WithLevel`

默认值：

- `dial_timeout`: `10s`
- `request_timeout`: `30s`
- `retry_timeout`: `1m`
- `retry_backoff`: `250ms`
- `conn_idle_timeout`: `30s`
- `metadata_max_age`: `5m`
- `partitioner`: `sticky`

注意事项：

- `partitioner` 当前支持 `sticky`、`round_robin`、`hash_key`。
- `hash_key` 依赖 `record_key` 或 `record_key_source` 提供 key，否则配置会被拒绝。
- `record_key` 与 `record_key_source` 互斥。
- `record_key_source` 当前只支持直接读取已有字段：`payload`、`match_key`、`remote`、`local`、`file_name`、`file_path`、`transfer_id`、`route_sender`。
- `compression_level` 只对 `gzip`、`lz4`、`zstd` 生效；`snappy` / `none` 没有可直接设置的压缩级别。

## 7. 配置校验规则

系统会在加载后校验：

- receiver 绑定的 selector 是否存在
- selector 引用的 task set 是否存在
- task set 引用的 task 是否存在
- task 的 pipeline / sender 是否存在
- execution model 是否合法
- sender / receiver 协议字段是否完整
- `gc_stats_log_interval` 是否为合法正数时长

## 8. 推荐实践

- 用 receiver 表达协议接入。
- 用 selector 表达固定 key 路由。
- 用 task set 表达复用。
- 用 task 表达执行。
- 不要再把 `task.receivers` 当成主配置模型。
