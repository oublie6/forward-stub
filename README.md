# forword-stub

一个面向高吞吐报文转发场景的 Go 服务：支持多种接收端（UDP/TCP gnet）、可编排 pipeline 规则处理，以及多种发送端（UDP 单播/组播、TCP、Kafka）。

---

## 1. 项目目标

`forword-stub` 的核心目标是：

- **可配置**：通过本地 JSON 或远端配置 API 驱动运行时拓扑。
- **高性能**：基于 `gnet` 事件循环与 `ants` 协程池，兼顾吞吐与资源控制。
- **可观测**：使用 `zap` 分级结构化日志，支持日志切割。
- **可维护**：采用 “全量替换 UpdateCache” 的稳定策略，降低热更新复杂度。

---

## 2. 主要依赖（稳定第三方库）

- [`github.com/panjf2000/gnet/v2`](https://github.com/panjf2000/gnet): 高性能事件驱动网络框架。
- [`github.com/panjf2000/ants/v2`](https://github.com/panjf2000/ants): 协程池，控制并发与资源上限。
- [`go.uber.org/zap`](https://github.com/uber-go/zap): 结构化高性能日志。
- [`gopkg.in/natefinch/lumberjack.v2`](https://github.com/natefinch/lumberjack): 滚动日志文件。
- [`github.com/valyala/bytebufferpool`](https://github.com/valyala/bytebufferpool): 字节缓冲池，减少内存分配。
- [`golang.org/x/sync/errgroup`](https://pkg.go.dev/golang.org/x/sync/errgroup): 并发任务收敛。
- [`go.uber.org/multierr`](https://pkg.go.dev/go.uber.org/multierr): 多错误聚合。

---

## 3. 目录结构

```text
.
├── main.go                   # 程序入口
├── configs/
│   └── example.json          # 示例配置
└── src/
    ├── app/                  # Runtime 对外封装
    ├── config/               # 配置定义、加载、校验
    ├── control/              # 远端配置 API 客户端
    ├── logx/                 # 日志初始化与工具
    ├── packet/               # 报文对象、buffer 池
    ├── pipeline/             # pipeline 编译与 stage
    ├── receiver/             # UDP/TCP 接收端
    ├── runtime/              # 核心运行时（Store、UpdateCache）
    ├── sender/               # UDP/TCP/Kafka 发送端
    └── task/                 # 单条处理链路（pipeline + senders）
```

---

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

---

## 5. 启动参数说明

| 参数 | 说明 | 默认值 |
|---|---|---|
| `-config` | 本地 JSON 配置文件路径，与 `-api` 二选一 | `""` |
| `-api` | 远端配置服务 URL，与 `-config` 二选一 | `""` |
| `-timeout` | API 拉取配置超时（秒） | `5` |
| `-log-level` | 日志级别：`debug` / `info` / `warn` / `error` | `info` |
| `-log-file` | 日志文件路径（空则 stdout） | `""` |

---

## 6. 配置文件详解

配置结构（`src/config/config.go`）：

```json
{
  "version": 5,
  "logging": {
    "level": "info",
    "file": ""
  },
  "receivers": {},
  "senders": {},
  "pipelines": {},
  "tasks": {}
}
```

### 6.1 顶层字段

- `version`：配置版本号，用于运行时日志与版本跟踪。
- `logging`：日志级别与日志文件。
- `receivers`：输入端定义。
- `senders`：输出端定义。
- `pipelines`：处理规则编排（按顺序执行 stage）。
- `tasks`：把 receiver / pipeline / sender 连接起来的任务定义。

### 6.2 receivers

每个 receiver 包含：

- `type`：
  - `udp_gnet`
  - `tcp_gnet`
  - `kafka`
- `listen`：监听地址（如 `udp://0.0.0.0:9000`）
- `multicore`：是否启用 gnet multicore。
- `frame`：仅 TCP 生效，目前支持：
  - `""`：原始流
  - `"u16be"`：2 字节大端长度帧
- `topic`：`kafka` receiver 消费 topic
- `group_id`：`kafka` receiver 消费组（可选，不填将自动生成）
- `listen`：`kafka` receiver 的 broker 列表（逗号分隔，如 `127.0.0.1:9092,127.0.0.1:9093`）

### 6.3 senders

`type` 支持：

- `udp_unicast`
- `udp_multicast`
- `tcp_gnet`
- `kafka`

常见字段：

- `remote`：目标地址（或 broker 地址，按 sender 实现解释）
- `concurrency`：并发度（对某些 sender 生效）
- `frame`：`tcp_gnet` 时可设置 `u16be`
- `topic`：Kafka topic
- `local_ip` / `local_port`：UDP 绑定地址
- `iface` / `ttl` / `loop`：组播参数

### 6.4 pipelines

pipeline 是 stage 数组，按顺序执行。
示例 stage：

```json
{
  "type": "match_offset_bytes",
  "offset": 0,
  "hex": "AABB",
  "flag": "matched"
}
```

### 6.5 tasks

一个 task 绑定：

- `receivers`: 输入来源（可多个）
- `pipelines`: 处理链（可多个）
- `senders`: 输出目标（可多个）
- `pool_size`: 协程池大小（`fast_path=false` 时生效）
- `fast_path`: 是否同步直通

---

## 7. 运行时流程

1. 启动后加载配置（本地文件或远端 API）。
2. 校验配置完整性与引用关系。
3. `UpdateCache` 执行全量切换：
   - 停止旧 receiver/task/sender
   - 构建新 sender
   - 构建新 task 并建立订阅关系
   - 启动新 receiver
4. receiver 收到报文后触发 `dispatch`：
   - 快照订阅 task 列表
   - 第一个 task 复用原包，后续 task clone
5. task 执行 pipeline，最后写入 sender。

---

## 8. 日志与可观测性

### 8.1 日志级别

- `debug`：调试细节（例如池满丢包）
- `info`：关键生命周期（启动、更新耗时）
- `warn`：可恢复异常（停止旧组件部分失败、发送失败）
- `error`：严重错误（receiver 运行异常退出）

### 8.2 日志性能策略

- 高频路径先通过 `logx.Enabled(level)` 判断级别，避免无效字段构造。
- 使用 zap 结构化日志，减少字符串拼接开销。
- 可选 lumberjack 滚动文件，避免 I/O 压力集中在超大单文件。

---

## 9. 性能与稳定性建议

1. **高吞吐场景**：
   - 优先启用 `multicore`（结合实际 CPU 核数压测）。
   - 调整 task 的 `pool_size`，避免过小导致大量丢包。
2. **低延迟场景**：
   - 对轻处理链路可启用 `fast_path=true`。
3. **日志策略**：
   - 生产默认 `info` 或 `warn`。
   - 排障短期开启 `debug`，问题定位后回退。
4. **内存优化**：
   - clone 仅在多任务分发时触发，减少不必要拷贝。

---

## 10. 校验与测试

### 10.1 静态检查

```bash
go vet ./...
```

### 10.2 构建与测试

```bash
go test ./...
```

> 当前仓库暂无 `_test.go` 文件，`go test` 主要用于验证可编译与包初始化正确性。


### 10.3 本地压测工具（UDP/TCP 转发）

仓库提供了一个可直接运行的经验压测工具：`cmd/bench/main.go`。

```bash
go run ./cmd/bench -mode both -duration 8s -warmup 2s -payload-size 512 -workers 4
```

参数说明：

- `-mode`: `udp` / `tcp` / `both`
- `-duration`: 统计窗口时长
- `-warmup`: 预热时长（预热后会清零计数）
- `-payload-size`: 业务负载字节数（TCP 模式会自动附加 `u16be` 长度头）
- `-workers`: 发送协程数
- `-pps-per-worker`: 每个发送协程的限速（pps，`0` 表示不限速）
- `-multicore`: 是否启用 receiver 的 gnet multicore

输出指标：

- `throughput`: 收到端口统计得到的 `pps` 与 `Mbps`
- `loss`: 以生成端发送包数与接收端收到包数估算的丢包率

> 说明：该工具采用本机 loopback 链路，结果主要用于版本对比和参数调优，不等同于真实网卡/跨机房生产带宽上限。

---

## 11. 常见问题（FAQ）

### Q1：启动时报 `must provide -config or -api`
A：必须二选一提供配置来源，示例：`-config ./configs/example.json`。

### Q2：为何有些 sender 报错“requires local_port”
A：`udp_unicast/udp_multicast` 目前要求显式配置 `local_port`。

### Q3：如何把日志写到文件
A：传入 `-log-file /path/to/app.log`，系统会自动按大小滚动。

### Q4：如何观察配置热更新效果
A：关注 `updating runtime cache` 与 `runtime cache updated` 两条 info 日志，查看版本与耗时。

---

## 12. 后续可演进方向

- 增量热更新（按 receiver/task/sender 差量替换）。
- 指标体系（QPS、丢包率、发送失败率、处理耗时分位数）。
- 更丰富 pipeline stage（协议解析、字段改写、路由表达式）。
- 为关键模块补充单元测试与 benchmark。

---

## 13. License

如无额外声明，遵循仓库默认许可证策略。

---

## 11. 一键打包（可用于交叉编译发布）

你的服务不依赖 go-zero 框架本体，但可以达到和 go-zero `goctl` 类似的“一键打包体验”。
本仓库已提供：

- `Makefile`
- `scripts/package.sh`

### 11.1 单平台打包（默认 linux/amd64）

```bash
make package
```

### 11.2 多平台一键打包

```bash
make package-all
```

默认产物：

- `dist/forword-stub_<version>_linux_amd64.tar.gz`
- `dist/forword-stub_<version>_linux_arm64.tar.gz`
- `dist/forword-stub_<version>_windows_amd64.zip`

打包内容包含：

- 可执行文件
- `configs/example.json`
- `README.md`

### 11.3 自定义目标平台

```bash
APP_NAME=forword-stub VERSION=v1.0.0 TARGETS="linux/amd64 linux/arm64" ./scripts/package.sh
```

### 11.4 查看版本

```bash
./bin/forword-stub -version
```

可通过 `-ldflags "-X main.version=..."` 注入版本号（`make package` 已内置）。

---

## 12. Docker / Kubernetes 部署建议（二进制外部构建）

为避免在 Docker 构建阶段重新拉依赖与编译，推荐先在宿主机产出 Linux 二进制，再在 Dockerfile 中仅拷贝二进制到最小运行时镜像。

### 12.1 先在宿主机编译 Linux 二进制

```bash
./scripts/build-linux.sh
```

默认输出文件：`dist/linux/forword-stub`。

### 12.2 构建运行时镜像（仅拷贝二进制）

```bash
docker build --build-arg BINARY_PATH=dist/linux/forword-stub -t forword-stub:latest .
```

也可直接使用：

```bash
make docker-build
```

该流程与 `deploy/k8s/deployment.yaml` 中的启动参数兼容（`-config /app/config/config.json` 由 ConfigMap 挂载提供）。
