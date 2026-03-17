# forward-stub

`forward-stub` 是一个面向高吞吐、低延迟、可热更新场景的 Go 转发引擎。系统把协议接入、处理、分发统一抽象为 `receiver -> task(pipeline + sender)`，通过配置把 UDP/TCP/Kafka/SFTP 组合成可运行的数据转发链路。

## 1. 项目简介

本项目解决的核心问题：

- 将多协议输入统一收敛为内部 `packet.Packet` 数据模型。
- 通过可编排 `task` 把处理逻辑和下游分发策略配置化。
- 在不停机前提下更新业务拓扑（receiver/sender/pipeline/task）。
- 提供可观测入口（日志、流量统计、pprof、benchmark）支持运维与性能分析。

## 2. 核心能力

- 支持 `udp_gnet`、`tcp_gnet`、`kafka`、`sftp` 四类 receiver。
- 支持 `udp_unicast`、`udp_multicast`、`tcp_gnet`、`kafka`、`sftp` 五类 sender。
- 内置 `match_offset_bytes`、`replace_offset_bytes`、`mark_as_file_chunk`、`clear_file_meta`、`route_offset_bytes_sender` stage。
- `task` 支持 `fastpath`、`pool`、`channel` 三种执行模型。
- runtime 支持 dispatch 快照分发，降低热路径锁竞争。
- 支持 system/business 双配置模式，兼容 legacy 单文件模式。
- 支持 business 配置热更新（文件监听 + 信号触发）。
- 支持 payload 复用、队列边界和回压控制。

## 3. 系统总体架构图

```mermaid
flowchart LR
  SysCfg[System Config] --> Boot[Bootstrap]
  BizCfg[Business Config] --> Boot
  Boot --> Validate[Load Validate Defaults]
  Validate --> Runtime[Runtime Store]

  Runtime --> Receiver[Receiver Set]
  Receiver --> Dispatch[Dispatch Snapshot]
  Dispatch --> Task[Task Set]
  Task --> Pipeline[Pipeline Set]
  Pipeline --> Sender[Sender Set]

  Api[Control API] --> Boot
  Signal[Signal Reload] --> Boot
  Watch[File Watch Reload] --> Boot
```

## 4. 核心处理流程图

```mermaid
flowchart TD
  In[Receiver Packet] --> Fanout[Dispatch by Receiver]
  Fanout --> TaskA[Task A]
  Fanout --> TaskB[Task B]

  TaskA --> ModelA[Execution Model]
  ModelA --> FastA[FastPath Run]
  ModelA --> PoolA[Pool Run]
  ModelA --> ChanA[Channel Run]

  FastA --> PipeA[Pipeline Process]
  PoolA --> PipeA
  ChanA --> PipeA
  PipeA --> SendA[Sender Fanout]

  TaskB --> PipeB[Pipeline Process]
  PipeB --> SendB[Sender Fanout]
```

## 5. 快速开始

### 5.1 编译

```bash
make build
```

或直接：

```bash
go build -mod=vendor -o bin/forward-stub .
```

### 5.2 运行（双配置模式，推荐）

```bash
./bin/forward-stub \
  -system-config ./configs/system.example.json \
  -business-config ./configs/business.example.json
```

### 5.3 运行（legacy 单文件模式）

```bash
./bin/forward-stub -config ./configs/example.json
```

### 5.4 Benchmark 入口

```bash
go test ./src/runtime -bench BenchmarkScenarioForwarding -benchmem
```

## 6. Benchmark / 性能测试说明

新的 benchmark 体系只用于评估 **服务内部完整转发链路** 的理论处理上限，核心问题是：

> 在不执行真实外部 I/O 的前提下，forward-stub 在具体协议场景下能处理多快。

- 常规运行：`go test ./src/runtime -bench BenchmarkScenarioForwarding -benchmem`
- 带 profile：`go test ./src/runtime -bench BenchmarkScenarioForwarding -benchmem -cpuprofile cpu.out -memprofile mem.out`
- 单场景过滤：`go test ./src/runtime -bench 'BenchmarkScenarioForwarding/UDP_to_UDP' -benchmem`

详细设计见 `docs/benchmark.md`。

## 7. 配置快速上手（README 摘要）

### 7.1 当前支持的配置组织方式

- **双文件模式（推荐）**
  - `system-config`：`control`、`logging`、`business_defaults`
  - `business-config`：`version`、`receivers`、`senders`、`pipelines`、`tasks`
- **legacy 单文件模式（兼容）**
  - 使用 `-config`，同一个 JSON 同时作为 system + business 读取。

加载流程：
1. 通过 `ResolveConfigPaths` 解析 `-system-config/-business-config` 或 `-config`。
2. 使用严格 JSON 解析（禁止未知字段）。
3. `SystemConfig.Merge(BusinessConfig)` 合并。
4. 先应用 `business_defaults`，再应用代码默认值。
5. 执行全量校验后启动运行时。

> 配置字段的完整解释、默认值和约束请看 `docs/configuration.md`（权威文档）。

### 7.2 `configs/system.example.json`（完整示例）

```json
{
  "control": {
    "api": "",
    "timeout_sec": 5,
    "config_watch_interval": "2s",
    "pprof_port": 6060
  },
  "logging": {
    "level": "info",
    "file": "",
    "max_size_mb": 100,
    "max_backups": 5,
    "max_age_days": 30,
    "compress": true,
    "traffic_stats_interval": "1s",
    "traffic_stats_sample_every": 1,
    "payload_log_max_bytes": 256,
    "payload_pool_max_cached_bytes": 0
  },
  "business_defaults": {
    "task": {
      "execution_model": "pool",
      "pool_size": 4096,
      "queue_size": 8192,
      "channel_queue_size": 8192,
      "payload_log_max_bytes": 256
    },
    "receiver": {
      "multicore": true,
      "num_event_loop": 8,
      "payload_log_max_bytes": 256
    },
    "sender": {
      "concurrency": 8
    }
  }
}
```

### 7.3 `configs/business.example.json`（完整示例）

```json
{
  "version": 1001,
  "receivers": {
    "rx_udp": {
      "type": "udp_gnet",
      "listen": "0.0.0.0:19000",
      "multicore": true,
      "num_event_loop": 8,
      "read_buffer_cap": 1048576,
      "socket_recv_buffer": 1073741824,
      "log_payload_recv": false,
      "payload_log_max_bytes": 256
    },
    "rx_tcp": {
      "type": "tcp_gnet",
      "listen": "0.0.0.0:19001",
      "frame": "u16be",
      "multicore": true,
      "num_event_loop": 4,
      "socket_recv_buffer": 1073741824
    },
    "rx_kafka": {
      "type": "kafka",
      "listen": "127.0.0.1:9092",
      "topic": "input-topic",
      "group_id": "forward-stub-group",
      "client_id": "forward-stub-rx",
      "start_offset": "latest",
      "fetch_min_bytes": 1,
      "fetch_max_bytes": 1048576,
      "fetch_max_wait_ms": 100,
      "username": "kafka-user",
      "password": "kafka-pass",
      "sasl_mechanism": "PLAIN",
      "tls": false,
      "tls_skip_verify": false
    },
    "rx_sftp": {
      "type": "sftp",
      "listen": "127.0.0.1:22",
      "username": "demo",
      "password": "demo",
      "remote_dir": "/input",
      "poll_interval_sec": 3,
      "chunk_size": 65536,
      "host_key_fingerprint": "SHA256:W5M5Qf3jQ8jD8I2LqzY9zT6QfPj1O9g3k8xw0Jm9r3A"
    }
  },
  "senders": {
    "tx_udp": {
      "type": "udp_unicast",
      "local_ip": "0.0.0.0",
      "local_port": 20000,
      "remote": "127.0.0.1:21000",
      "concurrency": 8,
      "socket_send_buffer": 1073741824
    },
    "tx_mcast": {
      "type": "udp_multicast",
      "local_ip": "0.0.0.0",
      "local_port": 20001,
      "remote": "239.0.0.10:21001",
      "iface": "eth0",
      "ttl": 16,
      "loop": false,
      "concurrency": 8,
      "socket_send_buffer": 1073741824
    },
    "tx_tcp": {
      "type": "tcp_gnet",
      "remote": "127.0.0.1:21002",
      "frame": "u16be",
      "concurrency": 4,
      "socket_send_buffer": 1073741824
    },
    "tx_kafka": {
      "type": "kafka",
      "remote": "127.0.0.1:9092",
      "topic": "output-topic",
      "client_id": "forward-stub-tx",
      "acks": "all",
      "idempotent": true,
      "retries": 20,
      "max_in_flight_requests_per_connection": 1,
      "linger_ms": 5,
      "batch_max_bytes": 1048576,
      "compression": "lz4",
      "username": "kafka-user",
      "password": "kafka-pass",
      "sasl_mechanism": "PLAIN",
      "tls": false,
      "tls_skip_verify": false
    },
    "tx_sftp": {
      "type": "sftp",
      "remote": "127.0.0.1:22",
      "username": "demo",
      "password": "demo",
      "remote_dir": "/output",
      "temp_suffix": ".tmp",
      "host_key_fingerprint": "SHA256:W5M5Qf3jQ8jD8I2LqzY9zT6QfPj1O9g3k8xw0Jm9r3A"
    }
  },
  "pipelines": {
    "pipe_match_replace": [
      {
        "type": "match_offset_bytes",
        "offset": 0,
        "hex": "aabb"
      },
      {
        "type": "replace_offset_bytes",
        "offset": 2,
        "hex": "ccdd"
      }
    ],
    "pipe_mark_file": [
      {
        "type": "mark_as_file_chunk",
        "path": "/auto/out.bin",
        "bool": true
      }
    ],
    "pipe_clear_file": [
      {
        "type": "clear_file_meta"
      }
    ],
    "pipe_route_sender": [
      {
        "type": "route_offset_bytes_sender",
        "offset": 0,
        "cases": {
          "01": "tx_udp",
          "02": "tx_tcp"
        },
        "default_sender": "tx_kafka"
      }
    ]
  },
  "tasks": {
    "task_fastpath": {
      "receivers": [
        "rx_udp"
      ],
      "pipelines": [
        "pipe_match_replace"
      ],
      "senders": [
        "tx_udp"
      ],
      "execution_model": "fastpath",
      "queue_size": 2048,
      "channel_queue_size": 2048,
      "log_payload_send": false,
      "payload_log_max_bytes": 256
    },
    "task_pool": {
      "receivers": [
        "rx_tcp"
      ],
      "pipelines": [
        "pipe_route_sender"
      ],
      "senders": [
        "tx_udp",
        "tx_tcp",
        "tx_kafka"
      ],
      "execution_model": "pool",
      "pool_size": 2048,
      "queue_size": 4096,
      "channel_queue_size": 4096,
      "log_payload_send": false,
      "payload_log_max_bytes": 256
    },
    "task_channel": {
      "receivers": [
        "rx_kafka"
      ],
      "pipelines": [
        "pipe_mark_file"
      ],
      "senders": [
        "tx_sftp"
      ],
      "execution_model": "channel",
      "channel_queue_size": 1024,
      "log_payload_send": false,
      "payload_log_max_bytes": 256
    }
  }
}
```

### 7.4 legacy `configs/example.json` 对照

legacy 模式仍支持，但推荐仅用于兼容旧部署。它等价于把上面 system/business 内容合并到一个 `Config` JSON。

## 8. 文档索引

- 配置权威文档：`docs/configuration.md`
- 运行时与生命周期：`docs/runtime-and-lifecycle.md`
- 执行模型：`docs/execution-model.md`
- 收发器说明：`docs/receivers-and-senders.md`
- Pipeline 说明：`docs/pipeline.md`
- 运维手册：`docs/operations.md`
- 观测与排障：`docs/observability.md` / `docs/troubleshooting.md`
