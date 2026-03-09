# forward-stub

`forward-stub` 是一个面向**高吞吐、低延迟、可热更新**场景的 Go 转发引擎。它将 `receiver -> pipeline -> sender` 抽象为可编排任务（task），支持 UDP/TCP/Kafka/SFTP 多协议收发与协议转换。

---

## 1. 项目定位

适用场景：

- 多协议接入与统一转发（例如 UDP 入、Kafka 出）。
- 数据治理前置层（match/replace/drop、文件语义转换）。
- 业务配置频繁变更且要求不停机更新。
- 高并发报文链路（网络 IO + 轻量处理 + 多下游 fan-out）。

核心目标：

1. **吞吐优先**：数据面尽量减少锁竞争与对象分配。
2. **可靠运行**：支持优雅停止、配置校验、运行时统计。
3. **可扩展**：通过 receiver/sender/stage/task 组合扩展能力。

---

## 2. 为什么吞吐高

高吞吐并非来自单点“魔法”，而是多个工程策略叠加：

- **gnet 事件驱动网络模型**：UDP/TCP 场景减少 goroutine 切换与系统调用压力。
- **payload 内存复用**：降低 GC 压力，避免高 PPS 下大量短生命周期对象。
- **任务执行模型可选**：`fastpath / pool / channel` 可按场景权衡延迟、吞吐与隔离。
- **dispatch 快照路由**：按 receiver 到 task 的订阅映射做快速分发。
- **可控队列**：有界队列与回压机制，避免无限堆积导致雪崩。

> 实测（见本文性能章节）中，`BenchmarkDispatchMatrix` 在 4096B payload 的多协议组合下可达到约 1.7~2.1 GB/s 级别的基准转发吞吐（同进程、基准条件下）。

---

## 3. 优秀第三方库与特性

项目依赖的关键库（见 `go.mod`）：

- **github.com/panjf2000/gnet/v2**
  - 事件驱动网络框架，适合高并发 UDP/TCP IO。
  - 减少“一连接一 goroutine”模型下的调度成本。
- **github.com/panjf2000/ants/v2**
  - 高性能 goroutine 池，实现 task 的 `pool` 执行模型。
  - 支持有界等待任务，避免无界增长。
- **github.com/twmb/franz-go**
  - Kafka 高性能客户端，支持 producer/consumer 高级参数调优。
- **github.com/pkg/sftp + golang.org/x/crypto/ssh**
  - SFTP 收发实现，支持基于 SSH 的文件分块收发。
  - 当前已支持 `host_key_fingerprint` 主机指纹校验（防 MITM）。
- **go.uber.org/zap + lumberjack**
  - 高性能结构化日志 + 文件滚动。
- **go.uber.org/multierr**
  - 资源关闭等多错误聚合。

---

## 4. 架构概览

```text
Config(system/business)
   -> Runtime.UpdateCache
      -> Compile Pipelines
      -> Build Senders
      -> Build Tasks
      -> Build Receivers

Receiver(onPacket)
   -> dispatch(receiver->tasks snapshot)
      -> Task execution model
         -> Pipeline stages
            -> Sender fan-out
```

### 核心模块

- `src/config`: 配置结构、默认值、校验、拆分加载。
- `src/runtime`: 运行时编译、热更新、实例生命周期管理。
- `src/task`: 执行模型与 pipeline + sender 串联。
- `src/receiver`: UDP/TCP/Kafka/SFTP 接收端。
- `src/sender`: UDP/TCP/Kafka/SFTP 发送端。
- `src/pipeline`: stage 实现（匹配/替换/丢弃/文件语义）。
- `src/logx`: 结构化日志与吞吐聚合统计。
- `cmd/bench`: 功能/吞吐压测工具。

---

## 5. 三种执行模型与 FastPath 详解

### 5.1 fastpath

- 同步执行：`dispatch` 所在线程直接跑 `pipeline + sender`。
- 优势：路径最短、调度开销最低、延迟可控。
- 风险：下游慢时容易把压力直接传回上游。
- 适用：处理逻辑轻、下游稳定、极致低延迟链路。

### 5.2 pool

- 通过 ants worker pool 异步执行。
- 优势：并发能力强，对突发流量缓冲更好。
- 风险：队列过大时可能增加尾延迟。
- 适用：吞吐优先且单包处理有一定 CPU 成本。

### 5.3 channel

- 单 goroutine + 有界 channel，顺序处理。
- 优势：顺序语义好、模型简单、可控。
- 风险：单消费协程上限明显。
- 适用：必须严格顺序或处理逻辑轻中等。

---

## 6. 配置说明（完整）

### 6.1 配置文件模式

推荐双文件：

- `system-config`：控制面、日志等系统级（通常需重启）。
- `business-config`：receiver/sender/pipeline/task（支持热重载）。

兼容单文件 `-config`，但建议新部署使用双文件。

### 6.2 顶层结构

```json
{
  "version": 1001,
  "control": {...},
  "logging": {...},
  "receivers": {...},
  "senders": {...},
  "pipelines": {...},
  "tasks": {...}
}
```

### 6.3 logging 关键项

- `level`: debug/info/warn/error
- `file`: 空为 stderr
- `max_size_mb/max_backups/max_age_days/compress`: 滚动日志策略
- `traffic_stats_interval`: 吞吐聚合输出周期
- `traffic_stats_sample_every`: 采样倍率
- `payload_log_tasks/payload_log_recv/payload_log_send/payload_log_max_bytes`: payload 观测开关

### 6.4 receiver 类型与字段

#### udp_gnet / tcp_gnet

- `listen` 必填；tcp 支持 `frame`（none/u16be/u32be 等）。

#### kafka

- `listen`: brokers CSV
- `topic`: 主题
- `group_id`
- `start_offset`: earliest/latest
- `fetch_min_bytes/fetch_max_bytes/fetch_max_wait_ms`
- 鉴权：`username/password/sasl_mechanism(PLAIN)`
- TLS：`tls/tls_skip_verify`（生产不建议 skip verify）

#### sftp

- `listen`、`username`、`password`、`remote_dir`
- `poll_interval_sec`、`chunk_size`
- **`host_key_fingerprint`（必填）**：`SHA256:<base64_raw>`

### 6.5 sender 类型与字段

#### udp_unicast / udp_multicast

- `remote` 必填
- `local_ip/local_port` 可选（ACL、出口控制）
- multicast: `iface/ttl/loop`

#### tcp_gnet

- `remote`
- `frame`
- `concurrency`

#### kafka

- `remote`、`topic`
- `acks`、`linger_ms`、`batch_max_bytes`、`compression`
- 鉴权 + TLS 参数与 receiver 类似

#### sftp

- `remote`、`username`、`password`、`remote_dir`
- `temp_suffix`
- **`host_key_fingerprint`（必填）**

### 6.6 pipeline stage

支持的 stage：

- `match_offset_bytes`
- `replace_offset_bytes`
- `drop_if_flag`
- `mark_as_file_chunk`
- `clear_file_meta`

flag 名称映射：`matched/rewritten/drop/none`。

### 6.7 task 字段

- `receivers`: 订阅哪些 receiver
- `pipelines`: 按顺序执行
- `senders`: fan-out 下游
- `execution_model`: `fastpath | pool | channel`
- `pool_size`、`queue_size`、`channel_queue_size`
- `log_payload_recv`、`log_payload_send`

---

## 7. 详细配置示例

```json
{
  "version": 1001,
  "control": {"api": "", "timeout_sec": 5},
  "logging": {
    "level": "info",
    "file": "",
    "max_size_mb": 100,
    "max_backups": 5,
    "max_age_days": 30,
    "compress": true,
    "traffic_stats_interval": "1s",
    "traffic_stats_sample_every": 1,
    "payload_log_tasks": ["task_udp_to_tcp"],
    "payload_log_recv": false,
    "payload_log_send": false,
    "payload_log_max_bytes": 256
  },
  "receivers": {
    "rx_udp": {"type": "udp_gnet", "listen": "0.0.0.0:19000", "multicore": true},
    "rx_tcp": {"type": "tcp_gnet", "listen": "0.0.0.0:19001", "frame": "u16be", "multicore": true},
    "rx_kafka": {
      "type": "kafka",
      "listen": "127.0.0.1:9092",
      "topic": "in-topic",
      "group_id": "forward-stub-group",
      "start_offset": "latest",
      "tls": false
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
    "tx_udp": {"type": "udp_unicast", "local_ip": "0.0.0.0", "local_port": 20000, "remote": "127.0.0.1:21000"},
    "tx_mcast": {"type": "udp_multicast", "local_ip": "0.0.0.0", "local_port": 20001, "remote": "239.0.0.10:21001", "iface": "eth0", "ttl": 16, "loop": false},
    "tx_tcp": {"type": "tcp_gnet", "remote": "127.0.0.1:21002", "frame": "u16be", "concurrency": 4},
    "tx_kafka": {"type": "kafka", "remote": "127.0.0.1:9092", "topic": "out-topic", "acks": -1, "linger_ms": 5, "batch_max_bytes": 1048576, "compression": "lz4"},
    "tx_sftp": {"type": "sftp", "remote": "127.0.0.1:22", "username": "demo", "password": "demo", "remote_dir": "/output", "temp_suffix": ".tmp", "host_key_fingerprint": "SHA256:W5M5Qf3jQ8jD8I2LqzY9zT6QfPj1O9g3k8xw0Jm9r3A"}
  },
  "pipelines": {
    "pipe_bytes": [
      {"type": "match_offset_bytes", "offset": 0, "hex": "aabb", "flag": "matched"},
      {"type": "replace_offset_bytes", "offset": 2, "hex": "ccdd", "flag": "rewritten"},
      {"type": "drop_if_flag", "flag": "drop"}
    ],
    "pipe_stream_to_file": [
      {"type": "mark_as_file_chunk", "path": "/auto/out.bin", "bool": true}
    ]
  },
  "tasks": {
    "task_udp_to_tcp": {
      "receivers": ["rx_udp"],
      "pipelines": ["pipe_bytes"],
      "senders": ["tx_tcp"],
      "execution_model": "pool",
      "pool_size": 2048,
      "queue_size": 4096,
      "channel_queue_size": 0,
      "log_payload_recv": false,
      "log_payload_send": false
    }
  }
}
```

---

## 8. 启动与运维

### 本地启动

```bash
go run . -system-config ./configs/system.example.json -business-config ./configs/business.example.json
```

### 常用 make 命令

```bash
make test
make vet
make perf
make verify
```

### 热更新

- 文件监听自动重载业务配置。
- 支持 `HUP/USR1` 信号触发重载。
- 系统配置（control/logging）变更通常需重启。

---

## 9. 最新性能测试结果（2026-03-09）

> 测试统一关闭 payload log（`PayloadLogRecv=false`, `PayloadLogSend=false`）。

### 9.1 严格 0 丢包 + 严格保序最大吞吐（UDP/TCP）

测试维度：
- receiver：`multicore=off/on`
- task：`channel` / `pool_size=1` / `fastpath=true`
- 口径：`loss_rate==0 && strict_order_ok==true`

| 场景 | UDP 最大吞吐 (Mbps) | TCP 最大吞吐 (Mbps) |
|---|---:|---:|
| 1.1.1 channel + multicore=off | 32.78 | 16.39 |
| 1.1.2 pool_size=1 + multicore=off | 8.20 | 32.77 |
| 1.1.3 fastpath=true + multicore=off | 32.78 | 16.39 |
| 1.2.1 channel + multicore=on | 4.10 | 32.77 |
| 1.2.2 pool_size=1 + multicore=on | 16.39 | 16.39 |
| 1.2.3 fastpath=true + multicore=on | 16.39 | 16.38 |

### 9.2 严格 0 丢包 + 不保序最大吞吐

约束：`fastpath=false`，receiver eventloop / task pool_size / queue_size / sender concurrency 可调。

| 转发类型 | 测试口径 | 最大吞吐 |
|---|---|---:|
| UDP→UDP | `cmd/bench` 端到端（duration=8s） | **883.79 Mbps** |
| TCP→TCP | `cmd/bench` 端到端（duration=8s） | **3196.49 Mbps** |
| Kafka→Kafka（模拟） | `BenchmarkDispatchMatrix`（benchtime=12s） | **1471.27 MB/s** |
| SFTP→SFTP（模拟） | `BenchmarkDispatchMatrix`（benchtime=12s） | **1511.47 MB/s** |

> Kafka/SFTP 为同进程模拟转发基准，不包含真实外部 broker / SFTP 服务网络与磁盘抖动影响。

---

## 10. 可扩展性为什么强

- **协议扩展**：新增 receiver/sender 仅需实现接口并接入 build 逻辑。
- **处理扩展**：新增 stage 通过 compiler 注册即可加入 pipeline。
- **拓扑扩展**：task 映射可 1:N/N:M，适合复杂转发编排。
- **运行时扩展**：业务配置支持热重载与增量切换。

---

## 11. 安全建议

- Kafka/SFTP 生产环境建议开启 TLS，不要使用 `tls_skip_verify`。
- SFTP 必须配置真实 `host_key_fingerprint`（建议通过运维流程自动注入）。
- 凭据不要硬编码在仓库，建议接入密钥管理系统。

---

## 12. 目录结构

```text
cmd/bench/            # 压测工具
configs/              # 示例配置
docs/                 # 架构与实验文档
src/app/              # 应用启动与运行时组装
src/config/           # 配置模型、默认值、校验
src/runtime/          # 运行时编译、更新、分发
src/task/             # 执行模型与任务生命周期
src/receiver/         # 协议接收端
src/sender/           # 协议发送端
src/pipeline/         # stage 管道处理
src/logx/             # 日志与流量统计
```
