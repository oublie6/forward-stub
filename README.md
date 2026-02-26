# forward-stub

高吞吐报文转发服务，支持 **UDP/TCP/Kafka 接收**、可编排 `pipeline` 处理、以及 **UDP/TCP/Kafka 发送**。

## 1. 功能概览

- 多协议接收：`udp_gnet`、`tcp_gnet`、`kafka`
- 多协议发送：`udp_unicast`、`udp_multicast`、`tcp_gnet`、`kafka`
- 任务编排：`task = receivers + pipelines + senders`
- 热更新模型：`UpdateCache` 全量替换运行时对象
- 可观测性：结构化日志、流量统计、pprof

## 2. 目录结构

```text
.
├── main.go
├── configs/
│   ├── example.json
│   └── bench.example.json
├── src/
│   ├── app/
│   ├── config/
│   ├── control/
│   ├── logx/
│   ├── packet/
│   ├── pipeline/
│   ├── receiver/
│   ├── runtime/
│   ├── sender/
│   └── task/
└── deploy/k8s/
```

## 3. 快速开始

### 3.1 环境

- Go：`1.25`（见 `go.mod`）
- 需要可访问配置中的网络端口（监听与下游地址）

### 3.2 启动

```bash
go run . -config ./configs/example.json
```

### 3.3 常用检查

```bash
go test ./...
go vet ./...
```

## 4. 配置文件详解

配置主结构：

```json
{
  "version": 6,
  "control": {},
  "runtime": {
    "default_task_pool_size": 64,
    "payload_pool_size": 1024,
    "payload_max_reuse_bytes": 0
  },
  "pprof": {
    "enabled": false,
    "listen": "127.0.0.1:6060"
  },
  "receivers": {},
  "senders": {},
  "pipelines": {},
  "tasks": {}
}
```

### 4.1 顶层字段

- `version`：配置版本
- `control`：远端配置拉取地址与超时
- `runtime.default_task_pool_size`：任务默认协程池大小（`task.pool_size<=0` 时使用）
- `runtime.payload_pool_size`：payload 内存池对象上限（用于 `src/packet/pool.go`）
- `runtime.payload_max_reuse_bytes`：单个 payload 缓冲复用上限，超过则不回池；`0` 表示不设上限（兼容旧版本）
- `logging`：日志与流量统计
- `pprof`：性能分析服务
- `receivers` / `senders` / `pipelines` / `tasks`：运行拓扑

### 4.2 默认值（代码内）

- `control.timeout_sec`: `5`
- `logging.level`: `info`
- `logging.max_size_mb`: `100`
- `logging.max_backups`: `5`
- `logging.max_age_days`: `30`
- `logging.compress`: `true`
- `logging.traffic_stats_interval`: `1s`
- `logging.traffic_stats_sample_every`: `1`
- `logging.traffic_stats_enable_sender`: `true`
- `pprof.listen`: `127.0.0.1:6060`
- `runtime.default_task_pool_size`: `64`
- `runtime.payload_pool_size`: `1024`
- `runtime.payload_max_reuse_bytes`: `0`（不限制）
- Kafka sender `send_timeout_ms`: `5000`
- Kafka receiver/sender `tls`：默认 `false`（不配置即 false）

### 4.3 receivers

#### UDP receiver

- `version`：配置版本号。
- `control`：配置中心拉取配置参数（可选）。
- `logging`：日志级别与日志文件。
- `receivers`：输入端定义。
- `pprof`：pprof 性能分析 HTTP 服务开关与监听地址。
- `senders`：输出端定义。
- `pipelines`：处理规则编排（按顺序执行 stage）。
- `tasks`：把 receiver / pipeline / sender 连接起来的任务定义。

### 6.1.1 pprof

- `enabled`：是否开启 pprof。
- `listen`：pprof HTTP 监听地址，默认 `127.0.0.1:6060`。
- 访问路径示例：`/debug/pprof/`、`/debug/pprof/profile`、`/debug/pprof/heap`。

### 6.2 receivers

- `type`：`udp_gnet` / `tcp_gnet` / `kafka`
- `listen`：监听地址（Kafka 场景为 broker 列表）
- `multicore`：是否启用 gnet multicore
- `frame`：TCP 分帧（`""` 或 `"u16be"`）
- `topic` / `group_id`：Kafka receiver 参数
- Kafka 扩展字段：`username`、`password`、`sasl_mechanism`（当前支持 `PLAIN`）、`tls`、`tls_skip_verify`、`client_id`、`start_offset`（`earliest/latest`）、`fetch_min_bytes`、`fetch_max_bytes`、`fetch_max_wait_ms`

### 6.3 senders

- `type`：`udp_unicast` / `udp_multicast` / `tcp_gnet` / `kafka`
- 常见字段：`remote`、`concurrency`、`frame`、`topic`
- Kafka 扩展字段：`username`、`password`、`sasl_mechanism`（当前支持 `PLAIN`）、`tls`、`tls_skip_verify`、`client_id`、`acks`（`-1/1/0`）、`linger_ms`、`batch_max_bytes`、`compression`（`none/gzip/snappy/lz4/zstd`）、`send_timeout_ms`（默认 5000）
- UDP 扩展字段：`local_ip`、`local_port`、`iface`、`ttl`、`loop`

#### Kafka receiver

```json
{
  "type": "kafka",
  "listen": "127.0.0.1:9092,127.0.0.1:9093",
  "topic": "demo.in",
  "group_id": "forward-stub-group",
  "client_id": "forward-stub-recv",
  "tls": false,
  "tls_skip_verify": false,
  "sasl_mechanism": "PLAIN",
  "username": "user",
  "password": "pass",
  "start_offset": "latest",
  "fetch_min_bytes": 1,
  "fetch_max_bytes": 16777216,
  "fetch_max_wait_ms": 100
}
```

### 4.4 senders

#### Kafka sender

### 6.6 Kafka receiver / sender 配置示例

下面给出一个最小可用示例：从 Kafka topic 消费报文，经过任务处理后再写入另一个 Kafka topic。

```json
{
  "version": 5,
  "logging": {
    "level": "info",
    "traffic_stats_interval": "5s",
    "traffic_stats_sample_every": 1,
    "traffic_stats_enable_sender": true
  },
  "receivers": {
    "kafka_in": {
      "type": "kafka",
      "listen": "127.0.0.1:9092,127.0.0.1:9093",
      "topic": "demo.input",
      "group_id": "forward-stub-group",
      "username": "kafka_user",
      "password": "kafka_pass",
      "sasl_mechanism": "PLAIN",
      "tls": true,
      "tls_skip_verify": false,
      "client_id": "forward-stub-recv",
      "start_offset": "latest",
      "fetch_min_bytes": 1,
      "fetch_max_bytes": 16777216,
      "fetch_max_wait_ms": 100
    }
  },
  "senders": {
    "kafka_out": {
      "type": "kafka",
      "remote": "127.0.0.1:9092,127.0.0.1:9093",
      "topic": "demo.output",
      "concurrency": 2,
      "username": "kafka_user",
      "password": "kafka_pass",
      "sasl_mechanism": "PLAIN",
      "tls": true,
      "tls_skip_verify": false,
      "client_id": "forward-stub-send",
      "acks": -1,
      "linger_ms": 1,
      "batch_max_bytes": 1048576,
      "compression": "snappy",
      "send_timeout_ms": 5000
    }
  },
  "pipelines": {
    "pass": []
  },
  "tasks": {
    "kafka_forward": {
      "receivers": ["kafka_in"],
      "pipelines": ["pass"],
      "senders": ["kafka_out"],
      "pool_size": 128,
      "fast_path": false
    }
  }
}
```

说明：

- Kafka receiver 使用 `listen` 作为 broker 列表（逗号分隔），并配合 `topic`、`group_id` 消费。
- Kafka sender 使用 `remote` 作为 broker 列表（逗号分隔），并通过 `topic` 指定目标主题。
- 配置了 `username/password` 时，默认按 `PLAIN` SASL 鉴权；也可显式设置 `sasl_mechanism=PLAIN`。
- `tls=true` 可启用 TLS，测试环境可配 `tls_skip_verify=true`（生产不建议）。
- `send_timeout_ms` 用于限制单次 `ProduceSync` 最长等待时间，避免异常网络下调用长期阻塞（配置 `0` 表示仅使用上游 context 的超时控制）。
- `pipelines` 可先配置为空数组（透传），后续再按需追加 stage。

## 7. 运行时流程

### 4.5 pipelines

支持 stage：

- `match_offset_bytes`
- `replace_offset_bytes`
- `drop_if_flag`

示例：

```json
{
  "p_match": [
    {"type": "match_offset_bytes", "offset": 0, "hex": "AABB", "flag": "matched"}
  ]
}
```

### 4.6 tasks

- `pool_size<=0`：使用 `runtime.default_task_pool_size`
- `fast_path=true`：不走协程池，当前协程同步执行

```json
{
  "udp_to_kafka": {
    "pool_size": 0,
    "fast_path": false,
    "receivers": ["udp_unicast_in"],
    "pipelines": [],
    "senders": ["kafka_out"]
  }
}
```

## 5. 示例：UDP 单播接收 -> Kafka 发送（无 pipeline）

`configs/example.json` 已提供完整示例，特点：

- receiver：`udp_gnet`/`kafka` 示例均包含完整 Kafka 配置（`tls`、`tls_skip_verify`、`sasl_mechanism`、`username`、`password`、消费参数等）
- sender：`kafka` 示例包含完整认证与生产参数（含 `username`/`password`）
- task：三个示例任务（`udp_to_udp`、`udp_to_kafka`、`kafka_to_udp`）均使用各自独立 receiver/sender

启动：

```bash
go run . -config ./configs/example.json
```

## 6. 运行流程

1. 读取本地配置并应用默认值
2. 若配置了 `control.api`，则拉取远端配置并再次应用默认值
3. 校验配置合法性
4. 初始化 runtime 并 `UpdateCache`
5. receiver 收包 -> dispatch -> task -> pipeline -> sender

## 7. FAQ

### Q1：task 没配置 `pool_size` 会怎样？
会自动使用 `runtime.default_task_pool_size`，默认 `64`。

### Q2：Kafka TLS 默认开吗？
默认关闭（`false`），需要时显式配置 `"tls": true`。

### Q3：某些包为何没有被转发？
常见原因：
- receiver 没有被任何 task 订阅
- task 已停止接收或协程池满
- sender 写出失败（会记录 warn 日志）

### Q4：如何打开 pprof？
设置：

```json
"pprof": {"enabled": true, "listen": "127.0.0.1:6060"}
```

然后访问：`/debug/pprof/`。


### Q5：payload 内存池上限默认是多少？
默认是 `0`，表示不限制单个 buffer 回池大小，这和旧版本（未显式限制上限）行为一致。
