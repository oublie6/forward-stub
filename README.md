# forward-stub

一个面向高吞吐报文转发场景的 Go 服务：支持多种接收端（UDP/TCP gnet、Kafka、SFTP）、可编排 pipeline 规则处理，以及多种发送端（UDP 单播/组播、TCP、Kafka、SFTP）。

---

## 1. 项目目标

`forward-stub` 的核心目标：

- **可配置**：通过本地 JSON 或远端配置 API 驱动运行时拓扑。
- **高性能**：基于 `gnet` 事件循环与 `ants` 协程池，兼顾吞吐与资源控制。
- **可观测**：使用 `zap` 结构化日志，支持日志切割。
- **可维护**：采用“全量替换 UpdateCache”策略，降低热更新复杂度。

## 2. 主要依赖

- `github.com/panjf2000/gnet/v2`
- `github.com/panjf2000/ants/v2`
- `go.uber.org/zap`
- `gopkg.in/natefinch/lumberjack.v2`
- `github.com/valyala/bytebufferpool`
- `golang.org/x/sync/errgroup`
- `go.uber.org/multierr`

## 3. 目录结构

```text
.
├── main.go                   # 程序入口
├── configs/
│   └── example.json          # 示例配置
├── src/
│   ├── app/                  # Runtime 对外封装
│   ├── config/               # 配置定义、加载、校验
│   ├── control/              # 远端配置 API 客户端
│   ├── logx/                 # 日志初始化与工具
│   ├── packet/               # 报文对象、buffer 池
│   ├── pipeline/             # pipeline 编译与 stage
│   ├── receiver/             # UDP/TCP/Kafka/SFTP 接收端
│   ├── runtime/              # 核心运行时（Store、UpdateCache）
│   ├── sender/               # UDP/TCP/Kafka/SFTP 发送端
│   └── task/                 # 单条处理链路（pipeline + senders）
├── scripts/                  # 构建与打包脚本
└── deploy/k8s/               # Kubernetes 部署清单
```

## 4. 快速开始

### 4.1 环境要求

- Go 版本：`go 1.25`（见 `go.mod`）。
- 网络端口：确保配置中监听和发送端口可用。

### 4.2 安装依赖

```bash
go mod download
```

### 4.3 本地配置启动

```bash
go run . -config ./configs/example.json
```

### 4.4 从配置 API 启动

在 `config` 文件内配置 `control.api` 与 `control.timeout_sec`，程序会先加载本地文件，再按该地址拉取远端配置。

```bash
go run . -config ./configs/example.json
```

## 5. 启动参数

| 参数 | 说明 | 默认值 |
|---|---|---|
| `-config` | 本地 JSON 配置文件路径（必填） | `""` |

> 说明：`control`、`logging` 以及流量统计相关参数统一从配置文件读取（不再提供独立启动参数）。

## 6. 配置文件说明

配置结构（`src/config/config.go`）：

```json
{
  "version": 5,
  "control": {
    "api": "",
    "timeout_sec": 5
  },
  "logging": {
    "level": "info",
    "file": "",
    "traffic_stats_interval": "5s",
    "traffic_stats_sample_every": 1
  },
  "receivers": {},
  "senders": {},
  "pipelines": {},
  "tasks": {}
}
```

### 6.1 顶层字段

- `version`：配置版本号。
- `control`：配置中心拉取配置参数（可选）。
- `logging`：日志级别与日志文件。
- `receivers`：输入端定义。
- `senders`：输出端定义。
- `pipelines`：处理规则编排（按顺序执行 stage）。
- `tasks`：把 receiver / pipeline / sender 连接起来的任务定义。

### 6.2 receivers

- `type`：`udp_gnet` / `tcp_gnet` / `kafka` / `sftp`
- `listen`：监听地址（Kafka 场景为 broker 列表）
- `multicore`：是否启用 gnet multicore
- `frame`：TCP 分帧（`""` 或 `"u16be"`）
- `topic` / `group_id`：Kafka receiver 参数
- Kafka 扩展字段：`username`、`password`、`sasl_mechanism`（当前支持 `PLAIN`）、`tls`、`tls_skip_verify`、`client_id`、`start_offset`（`earliest/latest`）、`fetch_min_bytes`、`fetch_max_bytes`、`fetch_max_wait_ms`
- SFTP 扩展字段：`remote_dir`、`poll_interval_sec`、`chunk_size`，并复用 `username/password` 进行登录认证

### 6.3 senders

- `type`：`udp_unicast` / `udp_multicast` / `tcp_gnet` / `kafka` / `sftp`
- 常见字段：`remote`、`concurrency`、`frame`、`topic`
- Kafka 扩展字段：`username`、`password`、`sasl_mechanism`（当前支持 `PLAIN`）、`tls`、`tls_skip_verify`、`client_id`、`acks`（`-1/1/0`）、`linger_ms`、`batch_max_bytes`、`compression`（`none/gzip/snappy/lz4/zstd`）
- UDP 扩展字段：`local_ip`、`local_port`、`iface`、`ttl`、`loop`

### 6.4 pipelines

pipeline 是 stage 数组，按顺序执行。例如：`match_offset_bytes`。

### 6.5 tasks

一个 task 绑定：`receivers`、`pipelines`、`senders`，并可配置 `pool_size` 与 `fast_path`。

### 6.6 Kafka receiver / sender 配置示例

下面给出一个最小可用示例：从 Kafka topic 消费报文，经过任务处理后再写入另一个 Kafka topic。

```json
{
  "version": 5,
  "logging": {
    "level": "info",
    "traffic_stats_interval": "5s",
    "traffic_stats_sample_every": 1
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
      "compression": "snappy"
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
- `pipelines` 可先配置为空数组（透传），后续再按需追加 stage。


### 6.7 SFTP receiver 配置示例（V1）

V1 版本支持把 SFTP 目录中的文件按 chunk 读取为内部 packet，后续可经 pipeline 处理并转发至现有 sender。

```json
{
  "receivers": {
    "sftp_in": {
      "type": "sftp",
      "listen": "127.0.0.1:22",
      "username": "demo",
      "password": "demo-pass",
      "remote_dir": "/data/in",
      "poll_interval_sec": 5,
      "chunk_size": 65536
    }
  }
}
```

说明：
- 同一文件按顺序分块读取，块大小由 `chunk_size` 控制（默认 64KiB）。
- `poll_interval_sec` 控制扫描周期（默认 5 秒）。
- 当前版本通过内存去重避免重复处理“同路径同 size+mtime”的文件。


### 6.8 SFTP sender 配置示例（V2）

V2 版本支持将带文件元信息（`file_path/offset/total_size/eof`）的 packet 分块写入 SFTP，并在完成后原子 rename 提交。

```json
{
  "senders": {
    "sftp_out": {
      "type": "sftp",
      "remote": "127.0.0.1:22",
      "username": "demo",
      "password": "demo-pass",
      "remote_dir": "/data/out",
      "temp_suffix": ".part"
    }
  }
}
```

### 6.9 V3 互转 stage 示例

- `mark_as_file_chunk`：将实时数据标记为文件分块语义（stream -> file）。
- `clear_file_meta`：清理文件元信息（file -> stream）。

```json
{
  "pipelines": {
    "stream_to_file": [
      {"type": "mark_as_file_chunk", "path": "stream/out.bin", "bool": true}
    ],
    "file_to_stream": [
      {"type": "clear_file_meta"}
    ]
  }
}
```


### 6.10 Envelope 统一数据模型（V3）

V3 在 receiver/pipeline/sender 之间统一使用 Envelope 语义（兼容现有 `packet.Packet` 外壳），支持两类 payload：
- `stream`：实时数据
- `file_chunk`：文件分块

关键元数据：`transfer_id`、`file_name`、`offset`、`total_size`、`checksum`、`eof`。

链路规则：
- task/pipeline 只处理 Envelope 元数据与 payload；
- sender 按 mode 输出（如 kafka/udp/tcp 输出 stream，sftp 输出 file_chunk）；
- 可组合实现 stream->file、file->stream、file->file。


### 6.11 SFTP 转发专题说明（端到端）

SFTP 链路的目标是让系统既能消费远端文件，也能将内部 packet 可靠落盘到 SFTP 目录。当前实现采用“分块 + 元信息 + 临时文件提交”模型。

#### 6.11.1 数据模型

SFTP 转发使用 `Envelope(file_chunk)` 语义，关键字段：

- `transfer_id`：同一文件传输会话 ID，用于 sender 侧分块归并。
- `file_name` / `file_path`：目标文件名与路径。
- `offset`：当前 chunk 的写入偏移。
- `total_size`：文件总大小。
- `checksum`：单 chunk 校验值（SHA-256，十六进制）。
- `eof`：是否最后一个分块。

#### 6.11.2 SFTP Receiver 工作流（详细）

Receiver 的实现可以理解为“**轮询扫描器 + 分块读取器 + 元信息封装器**”，核心流程如下。

**A. 启动与生命周期**

1. `Start` 被 runtime 调用后，会记录 `onPacket` 回调并创建可取消上下文；
2. 启用流量统计（如果日志级别允许），用于输出 receiver 吞吐；
3. 进入循环：`scanOnce -> sleep(poll_interval_sec) -> scanOnce ...`；
4. `Stop` 被调用时触发 cancel，等待 `Start` 退出并回收状态。

**B. 单次扫描（scanOnce）**

1. 建立 SSH + SFTP 连接（本轮短连接，用完关闭）；
2. 读取 `remote_dir` 下目录项，仅处理常规文件；
3. 按文件名排序后顺序处理（确保行为稳定、便于排障复现）；
4. 基于 `size + mtime` 生成文件指纹：
   - 指纹已出现：跳过（避免重复消费未变化文件）；
   - 指纹未出现：进入 `streamFile` 分块读取；
5. 文件成功处理后才写入 `seen`，确保失败文件可在下一轮重试。

**C. 文件分块（streamFile）**

1. 按 `chunk_size` 分配缓冲区（最小兜底 1024 字节）；
2. 循环读取文件，每次读取生成一个 `file_chunk` packet；
3. 为每个 chunk 填充核心元信息：
   - `transfer_id`：默认由 `filePath + totalSize` 组合；
   - `offset`：当前块在原文件中的起始位置；
   - `total_size`：文件总字节数；
   - `checksum`：当前 chunk 的 SHA-256；
   - `eof`：当前 chunk 是否文件末块；
4. 通过 `onPacket` 将 packet 推给 task/pipeline/sender 链路。

**D. Receiver 的几个设计取舍**

- **短连接扫描**：每轮扫描单独建连，降低长期空闲连接失效后的异常处理复杂度；
- **指纹去重而非落库断点**：实现简单、开销低，但进程重启后不会保留历史 `seen`；
- **顺序处理文件**：牺牲部分并行读取能力，换取更稳定的处理顺序。

建议：
- 大文件优先增大 `chunk_size`（如 256KiB~1MiB）降低对象数量与调度开销；
- 小文件高频目录优先缩短 `poll_interval_sec`，但要关注目录扫描与 SSH 建连成本；
- 若目录文件非常多，建议在上游按日期/批次分目录，避免单目录扫描过重。

#### 6.11.3 SFTP Sender 工作流（详细）

Sender 的实现可以理解为“**按 transfer 会话聚合 + 随机写入临时文件 + 原子提交**”。

**A. 输入约束与会话定位**

1. 每次 `Send` 接收一个 packet；
2. 优先使用 `meta.transfer_id` 定位会话状态；若缺失则自动生成临时会话 ID；
3. 文件名优先级：`file_name` > `base(file_path)` > `transfer_id.bin`；
4. 首次出现的会话会创建状态：`tempPath/finalPath/totalSize/eofSeen/writtenMax`。

**B. 校验与分块写入**

1. 若 packet 带 `checksum`，sender 先计算 payload SHA-256 并校验；
2. 校验通过后，以 `WriteAt(offset)` 写入 `remote_dir/<file><temp_suffix>`；
3. 更新会话进度：
   - `writtenMax = max(writtenMax, offset + len(payload))`
   - 若 `eof=true`，则标记 `eofSeen=true`。

这种写法支持“按偏移写入”，对乱序 chunk 或补写场景更友好。

**C. 提交条件与原子可见性**

当且仅当满足以下条件才提交最终文件：

- 已见到 EOF（`eofSeen=true`）；
- 且 `total_size<=0` 或 `writtenMax >= total_size`。

满足条件后执行：

1. 尝试删除同名最终文件（如已存在）；
2. `Rename(tempPath, finalPath)` 原子提交；
3. 删除内存会话状态。

通过“临时文件 + rename”机制，下游通常只能看到完整文件，避免读到半成品。

**D. Sender 的几个设计取舍**

- **串行锁保护**：`Send` 内部串行化，保证连接状态和会话 map 一致性；
- **连接复用 + 按需重连**：正常路径复用 SFTP/SSH 连接，不可用时 `ensureConn` 重建；
- **会话状态在内存中**：轻量高效，但进程重启会丢失未提交会话上下文。

运维建议：
- 将 `temp_suffix` 设为下游不会消费的后缀（如 `.part/.tmp`）；
- 配置定期清理策略，处理异常中断后遗留的临时文件；
- 若业务要求严格“整文件一致性”，务必保证上游正确设置 `total_size + eof`。

#### 6.11.4 典型编排

- **file -> stream**：`receiver(sftp)` + `pipeline(clear_file_meta)` + `sender(udp/tcp/kafka)`
- **stream -> file**：`receiver(udp/tcp/kafka)` + `pipeline(mark_as_file_chunk)` + `sender(sftp)`
- **file -> file**：`receiver(sftp)` + 可选处理 pipeline + `sender(sftp)`

#### 6.11.5 最小可运行示例（SFTP -> Kafka）

```json
{
  "receivers": {
    "sftp_in": {
      "type": "sftp",
      "listen": "127.0.0.1:22",
      "username": "demo",
      "password": "demo-pass",
      "remote_dir": "/data/in",
      "poll_interval_sec": 5,
      "chunk_size": 65536
    }
  },
  "pipelines": {
    "file_to_stream": [
      {"type": "clear_file_meta"}
    ]
  },
  "senders": {
    "kafka_out": {
      "type": "kafka",
      "remote": "127.0.0.1:9092",
      "topic": "demo.file.payload"
    }
  },
  "tasks": {
    "sftp_to_kafka": {
      "receivers": ["sftp_in"],
      "pipelines": ["file_to_stream"],
      "senders": ["kafka_out"],
      "pool_size": 128,
      "fast_path": false
    }
  }
}
```

## 7. 运行时流程

1. 启动后加载配置（本地文件或远端 API）。
2. 校验配置完整性与引用关系。
3. `UpdateCache` 执行全量切换：停止旧组件并构建、启动新组件。
4. receiver 收到报文后触发 `dispatch`，按订阅任务分发报文。
5. task 执行 pipeline，最后写入 sender。

## 8. 日志与可观测性

- 日志级别：`debug` / `info` / `warn` / `error`
- 高频路径先通过 `logx.Enabled(level)` 判断等级，减少无效字段构造。
- 支持 `lumberjack` 滚动文件日志。
- `logging` 关键字段：
  - `file`：非空时写入滚动日志文件
  - `compress`：是否压缩历史分卷（默认 `true`）
  - `max_size_mb` / `max_backups` / `max_age_days`：分卷大小、保留份数、保留天数

### 8.1 Docker/Kubernetes 日志挂载建议

- Docker：挂载日志目录到宿主机（示例）

```bash
docker run --rm \
  -v $(pwd)/configs/example.json:/app/configs/example.json:ro \
  -v $(pwd)/logs:/var/log/forward-stub \
  forward-stub:latest
```

- Kubernetes：`deploy/k8s/deployment.yaml` 已示例 `logs` volume 挂载到 `/var/log/forward-stub`，`deploy/k8s/configmap.yaml` 里将 `logging.file` 指向 `/var/log/forward-stub/app.log` 并开启 `compress=true`。

## 9. 性能与稳定性建议

1. 高吞吐优先启用 `multicore` 并压测调参。
2. 轻处理链路可启用 `fast_path=true` 追求低延迟。
3. 生产默认 `info` 或 `warn`，排障时临时升 `debug`。
4. 多任务分发才触发 clone，减少不必要拷贝。

## 10. 校验与测试

### 10.1 静态检查

```bash
go vet ./...
```

### 10.2 构建与测试

```bash
go test ./...
```

### 10.3 本地压测工具

```bash
go run ./cmd/bench -mode both -duration 8s -warmup 2s -payload-size 512 -workers 4
```

也支持将压测参数收敛到 JSON 配置，避免长命令行：

```bash
go run ./cmd/bench -bench-config ./configs/bench.example.json
```

`configs/bench.example.json` 已覆盖 bench 常用可配置项（如 `workers`、`task_pool_size`、`pps_per_worker`、`pps_sweep`、`traffic_stats_interval` 等），可按机器规模直接调参复用。

### 10.4 每次改动的回归基线（功能 + 性能）

建议每次提交前执行：

```bash
make verify
```

`make verify` 会串行执行：
- `go test ./...`（功能回归）
- `go test ./src/runtime -run '^$' -bench BenchmarkDispatchMatrix -benchmem -benchtime=2s`（4 协议笛卡尔积 dispatch 微基准）
- `go run ./cmd/bench -mode both -duration 4s -warmup 1s -payload-size 512 -workers 2 -pps-sweep 2000,4000,8000 -log-level error`（UDP/TCP 端到端压测）

最近一次高压复测（3 vCPU 容器，本机 loopback）结果如下（仅供趋势比较）：
- dispatch 微基准（`go test ./src/runtime -run '^$' -bench BenchmarkDispatchMatrix -benchmem -benchtime=1s`）：
  - 256B 负载：约 `613~697 ns/op`
  - 4096B 负载：约 `2.5~3.1 us/op`
  - 分配：`6 allocs/op`，约 `592B/op(256B 载荷)` / `4432B/op(4096B 载荷)`
- 端到端压测（高压 + 不丢包最大吞吐）：
  - UDP（1400B，4 workers）：约 `27539 pps`（`308.43 Mbps`，`0.00% loss`）
  - TCP（1400B，4 workers）：约 `48461 pps`（`542.77 Mbps`，`0.00% loss`）

若出现以下情况，建议阻断发布并定位：
- 功能测试失败或转发正确性异常；
- 微基准 `ns/op` 回退超过 20%；
- 在相同压测参数下，端到端“无丢包最大吞吐”回退超过 20%。

## 11. 常见问题（FAQ）

### Q1：启动时报 `must provide -config`
A：必须提供本地配置文件路径。

### Q2：`udp_unicast/udp_multicast` 报错 requires local_port
A：当前实现要求显式配置 `local_port`。

### Q3：如何把日志写到文件
A：在配置文件设置 `logging.file`，例如：`"file":"/path/to/app.log"`。

### Q4：如何观察配置热更新效果
A：关注 `updating runtime cache` 与 `runtime cache updated` 两条日志。

### Q5：为何没有区分 UDP 单播/组播 receiver
A：receiver 侧统一 `udp_gnet`；单播/组播差异在 sender 侧。

## 12. 一键打包与部署

### 12.1 单平台打包（默认 linux/arm64，aarch64）

```bash
make package
```

### 12.2 多平台打包

```bash
make package-all
```

### 12.3 Docker 构建（默认基于 linux/arm64 产物）

```bash
./scripts/build-linux.sh
docker build --build-arg BINARY_PATH=dist/linux/forward-stub -t forward-stub:latest .
```

### 12.4 Kubernetes 部署

`deploy/k8s` 目录提供了 `deployment`、`service`、`configmap` 与 `kustomization` 示例。

## 13. 后续演进方向

- 增量热更新（按组件差量替换）
- 指标体系（QPS、丢包率、发送失败率、处理耗时）
- 更丰富 pipeline stage（解析、改写、路由）
- 补充单元测试与 benchmark

## 14. License

本项目采用 Apache License 2.0，详见 [LICENSE](./LICENSE)。
