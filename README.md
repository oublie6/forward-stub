# forword-stub

一个面向高吞吐报文转发场景的 Go 服务：支持多种接收端（UDP/TCP gnet、Kafka）、可编排 pipeline 规则处理，以及多种发送端（UDP 单播/组播、TCP、Kafka）。

---

## 1. 项目目标

`forword-stub` 的核心目标：

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
│   ├── receiver/             # UDP/TCP/Kafka 接收端
│   ├── runtime/              # 核心运行时（Store、UpdateCache）
│   ├── sender/               # UDP/TCP/Kafka 发送端
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
go run . -config ./configs/example.json -log-level info
```

### 4.4 从配置 API 启动

```bash
go run . -api http://127.0.0.1:8080/config -timeout 5 -log-level info
```

## 5. 启动参数

| 参数 | 说明 | 默认值 |
|---|---|---|
| `-config` | 本地 JSON 配置文件路径，与 `-api` 二选一 | `""` |
| `-api` | 远端配置服务 URL，与 `-config` 二选一 | `""` |
| `-timeout` | API 拉取配置超时（秒） | `5` |
| `-log-level` | 日志级别：`debug` / `info` / `warn` / `error` | `info` |
| `-log-file` | 日志文件路径（空则 stdout） | `""` |
| `-traffic-stats-interval` | 统一流量统计输出周期（如 `5s` / `10s`） | `1s` |
| `-traffic-stats-sample-every` | 流量统计采样倍率 N（每 N 个包计 1 次并按倍率折算） | `1` |
| `-traffic-stats-enable-sender` | 是否开启 sender 维度统计 | `true` |

## 6. 配置文件说明

配置结构（`src/config/config.go`）：

```json
{
  "version": 5,
  "logging": {
    "level": "info",
    "file": "",
    "traffic_stats_interval": "5s",
    "traffic_stats_sample_every": 1,
    "traffic_stats_enable_sender": true
  },
  "receivers": {},
  "senders": {},
  "pipelines": {},
  "tasks": {}
}
```

### 6.1 顶层字段

- `version`：配置版本号。
- `logging`：日志级别与日志文件。
- `receivers`：输入端定义。
- `senders`：输出端定义。
- `pipelines`：处理规则编排（按顺序执行 stage）。
- `tasks`：把 receiver / pipeline / sender 连接起来的任务定义。

### 6.2 receivers

- `type`：`udp_gnet` / `tcp_gnet` / `kafka`
- `listen`：监听地址（Kafka 场景为 broker 列表）
- `multicore`：是否启用 gnet multicore
- `frame`：TCP 分帧（`""` 或 `"u16be"`）
- `topic` / `group_id`：Kafka receiver 参数

### 6.3 senders

- `type`：`udp_unicast` / `udp_multicast` / `tcp_gnet` / `kafka`
- 常见字段：`remote`、`concurrency`、`frame`、`topic`
- UDP 扩展字段：`local_ip`、`local_port`、`iface`、`ttl`、`loop`

### 6.4 pipelines

pipeline 是 stage 数组，按顺序执行。例如：`match_offset_bytes`。

### 6.5 tasks

一个 task 绑定：`receivers`、`pipelines`、`senders`，并可配置 `pool_size` 与 `fast_path`。

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

## 11. 常见问题（FAQ）

### Q1：启动时报 `must provide -config or -api`
A：必须二选一提供配置来源。

### Q2：`udp_unicast/udp_multicast` 报错 requires local_port
A：当前实现要求显式配置 `local_port`。

### Q3：如何把日志写到文件
A：传入 `-log-file /path/to/app.log`。

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
docker build --build-arg BINARY_PATH=dist/linux/forword-stub -t forword-stub:latest .
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
