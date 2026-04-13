# forward-stub 标准操作手册

> 职责边界：本文负责启动、重载、停机、巡检、排障和值班检查清单等可执行操作。不维护配置字段全集、协议专属行为、runtime 内部生命周期或部署清单细节；这些内容分别见 `docs/configuration.md`、`docs/receivers-and-senders.md`、`docs/runtime-and-lifecycle.md` 和 `docs/deployment.md`。

> 本手册面向运维、实施、测试、值班与接手维护人员，强调“可直接执行”的操作步骤。内容以当前仓库代码、README、`docs/` 与 `configs/*.json` 为准；若与历史口头经验不一致，应以当前实现和当前文档为准。

## 1. 文档定位与适用范围

### 1.1 适用场景

本手册适用于以下场景：

- 新环境部署 `forward-stub`
- 日常启动、重载、停机
- 业务链路配置变更
- 运行中巡检、观测、排障
- 值班人员快速定位“启动失败、收不到流量、路由未命中、发送失败、优雅停机卡住”等问题

### 1.2 适用运行方式

当前项目使用双配置运行方式：

- **双配置模式**：`system` + `business` 分离，生产环境推荐

### 1.3 适用协议类型

当前实现覆盖以下协议类型：

- receiver：`udp_gnet`、`tcp_gnet`、`kafka`、`sftp`
- sender：`udp_unicast`、`udp_multicast`、`tcp_gnet`、`kafka`、`sftp`

### 1.4 与其他文档的关系

- `README.md`：入口说明、最小示例、文档索引
- `docs/configuration.md`：字段字典、默认值、校验规则
- `docs/receivers-and-senders.md`：协议专属字段与行为
- `docs/runtime-and-lifecycle.md`：默认值生效层次、热重载边界、停机顺序
- 本手册：**操作步骤、判断标准、巡检方法、排障流程**

## 2. 系统概述

### 2.1 forward-stub 是什么

`forward-stub` 是一个 Go 编写的高吞吐、低延迟、支持热更新的转发引擎。主链路固定为：

```text
receiver -> selector -> task(pipeline + sender)
```

### 2.2 从操作人员视角理解主链路

| 组件 | 操作视角职责 |
| --- | --- |
| receiver | 从 UDP/TCP/Kafka/SFTP 接收数据，生成 `packet` 与 `match_key` |
| selector | 用完整 `match_key` 精确匹配到 `task_set` |
| task_set | 仅做 task 名称复用 |
| task | 执行 pipeline、选择 sender、决定并发模型 |
| pipeline | 对 payload / 元数据做处理或改写 `route sender` |
| sender | 把最终结果发往下游 |

### 2.3 system / business 配置分别是什么

| 配置类型 | 作用 | 典型内容 |
| --- | --- | --- |
| system | 系统级、运行基线 | `control`、`logging`、`business_defaults` |
| business | 业务拓扑与协议链路 | `receivers`、`selectors`、`task_sets`、`senders`、`pipelines`、`tasks` |

### 2.4 哪些配置支持热重载，哪些不支持

#### 支持热重载

- `receivers`
- `selectors`
- `task_sets`
- `senders`
- `pipelines`
- `tasks`
- `version`

#### 不支持热重载

- `control`
- `logging`
- `business_defaults`

说明：system 配置一旦与冷启动基线不一致，当前实现会拒绝本次重载，需要重启进程后生效。

## 3. 目录与关键文件说明

### 3.1 程序入口与主要代码路径

| 路径 | 用途 |
| --- | --- |
| `main.go` | 进程入口 |
| `src/bootstrap/run.go` | 启动、重载、停机总控 |
| `src/app/runtime.go` | system 基线校验、转调 runtime |
| `src/runtime/` | 运行时快照、热更新、分发 |
| `src/receiver/` | 接入协议实现 |
| `src/task/` | task 执行模型与优雅停机 |
| `src/sender/` | 发送协议实现 |
| `src/config/` | 配置结构、默认值、校验 |

### 3.2 配置示例文件

| 文件 | 用途 |
| --- | --- |
| `configs/system.example.json` | system 全量示例 |
| `configs/business.example.json` | business 全量示例 |
| `configs/bench.example.json` | benchmark / 压测专用示例，不属于运行时主配置 |

### 3.3 docs 目录关键文档

| 文件 | 重点用途 |
| --- | --- |
| `docs/architecture.md` | 系统结构与模块职责边界 |
| `docs/configuration.md` | 配置字段与默认值核对 |
| `docs/receivers-and-senders.md` | 协议字段与行为核对 |
| `docs/task-and-dispatch.md` | 路由链路与职责边界核对 |
| `docs/pipeline.md` | pipeline stage 字段与丢弃语义 |
| `docs/execution-model.md` | task 执行模型与容量语义 |
| `docs/observability.md` | 日志、流量统计、GC、pprof 观测 |
| `docs/runtime-and-lifecycle.md` | 热重载边界、默认值层次 |
| `docs/deployment.md` | 本地、Docker、Kubernetes 部署入口 |
| `docs/skydds.md` | SkyDDS SDK 与 `dds_skydds` 协议接入 |
| `docs/operations-manual.md` | 标准操作步骤与值班检查清单 |

### 3.4 日志、pprof、GC、traffic stats 入口说明

- 业务与阶段日志：由 `logging.*` 控制
- traffic stats：由 `logging.traffic_stats_interval`、`logging.traffic_stats_sample_every` 控制
- payload 摘要日志：由 receiver / task 局部开关控制，且要求日志级别至少允许 info 输出
- GC 周期日志：由 `logging.gc_stats_log_enabled`、`logging.gc_stats_log_interval` 控制
- pprof：由 `control.pprof_port` 控制；`-1` 禁用，`0` 回退到默认 `6060`

## 4. 部署前准备

### 4.1 环境要求

建议至少准备以下条件：

- 可运行当前 Go 编译产物的 Linux 或 Windows 环境
- 对应协议所需的网络可达性
- 配置文件读权限
- 日志文件目录写权限（若 `logging.file` 非空）
- 若启用 pprof，对应端口可监听且符合安全策略

### 4.2 配置文件准备

推荐生产环境使用双配置模式：

- `system.json`：只放 `control`、`logging`、`business_defaults`
- `business.json`：只放业务拓扑对象

上线前必须确认：

1. JSON 无语法错误
2. 未使用未知字段
3. 引用链完整：receiver -> selector -> task_set -> task -> pipeline/sender
4. 协议专属字段只在对应 `type` 下使用

### 4.3 目录、权限、端口要求

重点确认：

- 日志目录存在并可写
- pprof 端口未被占用
- UDP/TCP receiver 的监听端口可绑定
- SFTP receiver / sender 账号具备对应远端目录权限
- Kafka broker、topic、认证参数已准备好

### 4.4 上线前连通性检查建议

#### UDP / TCP

```bash
ss -lntup
nc -vz 127.0.0.1 19001
```

#### Kafka

```bash
nc -vz 127.0.0.1 9092
```

#### SFTP

```bash
nc -vz 127.0.0.1 22
```

### 4.5 上线前检查项

- 启动命令已确认
- system / business 文件路径已确认
- receiver 与 sender 远端地址已确认
- `match_key` 模式与 selector 中写法一致
- `execution_model` 与吞吐/顺序需求一致
- 是否需要临时开启 payload / GC / pprof 已确认

## 5. 启动手册

### 5.1 双配置模式启动

标准命令：

```bash
./bin/forward-stub \
  -system-config ./configs/system.example.json \
  -business-config ./configs/business.example.json
```

适用场景：生产、预发、长期运行环境。

### 5.2 参数说明

| 参数 | 作用 |
| --- | --- |
| `-system-config` | system 配置路径 |
| `-business-config` | business 配置路径 |

### 5.3 主示例启动命令

```bash
./bin/forward-stub \
  -system-config ./configs/system.example.json \
  -business-config ./configs/business.example.json
```

### 5.5 如何判断启动成功

优先观察日志中是否出现：

- `config_load` 完成
- `config_validate` 完成
- `logger_init` 完成
- `runtime_init` 完成
- `service_start`：**这是最直接的启动成功标志**

### 5.6 启动失败时优先检查什么

按以下顺序排查：

1. CLI 参数和配置路径是否正确
2. JSON 是否混写 system / business 字段
3. 是否存在未知字段
4. receiver / sender / selector / task_set / task / pipeline 引用链是否完整
5. Kafka duration、SFTP 主机指纹、sender concurrency 等校验项是否违规
6. 端口、broker、SFTP 地址是否可达

## 6. 配置使用手册（操作视角）

### 6.1 system 配置控制什么

system 配置主要控制：

- 配置加载与监听：`control.*`
- 日志输出：`logging.*`
- 系统级业务默认值：`business_defaults.*`

### 6.2 business 配置控制什么

business 配置主要控制：

- 接入点：`receivers`
- 主路由：`selectors`
- task 复用：`task_sets`
- 输出端：`senders`
- 数据处理：`pipelines`
- 执行与发送：`tasks`

### 6.3 典型配置思路

1. 先确定接入协议，定义 receiver
2. 根据 receiver 生成的 `match_key` 设计 selector
3. 用 `task_set` 把一组 task 复用出来
4. 在 task 中绑定 pipeline 与 sender
5. 若 task 内还要精确选择 sender，再使用 route stage

### 6.4 Kafka receiver / sender 常改字段

#### Kafka receiver 常改字段

- `topic`
- `group_id`
- `start_offset`
- `balancers`
- `auto_commit` / `auto_commit_interval`
- `fetch_min_bytes` / `fetch_max_bytes` / `fetch_max_wait_ms`
- `dial_timeout`、`session_timeout`、`heartbeat_interval`
- `match_key.mode`

#### Kafka sender 常改字段

- `topic`
- `acks`
- `idempotent`
- `retries`
- `max_in_flight_requests_per_connection`
- `linger_ms`
- `batch_max_bytes`
- `compression` / `compression_level`
- `partitioner`
- `record_key` / `record_key_source`

### 6.5 SFTP receiver / sender 常改字段

#### SFTP receiver

- `remote_dir`
- `poll_interval_sec`
- `chunk_size`
- `host_key_fingerprint`
- `match_key.mode`

#### SFTP sender

- `remote_dir`
- `temp_suffix`
- `host_key_fingerprint`
- `concurrency`

### 6.6 `match_key` 如何理解、如何选模式

`match_key` 是 receiver 侧生成的完整路由键，selector 只做精确匹配。

#### 选型建议

- 需要兼容历史配置：留空，保持兼容默认模式
- 想按来源 IP 聚合：UDP/TCP 选 `remote_ip`
- 想按本地监听区分：UDP/TCP 选 `local_addr` / `local_port`
- 想按 Kafka topic 或 topic+partition 区分：选 `topic` 或 `topic_partition`
- 想把某个 receiver 全量打到固定 task_set：选 `fixed`

### 6.7 `execution_model` 如何选

| 模型 | 适用场景 | 风险与注意点 |
| --- | --- | --- |
| `fastpath` | 最低延迟、链路简单 | sender 慢会直接反压到热路径 |
| `pool` | 常规生产默认选择 | 要关注 `pool_size` 与下游处理/发送耗时 |
| `channel` | 需要单 task 内顺序处理 | 单 worker 容易成为瓶颈 |

### 6.8 何时建议开启 payload / traffic stats / GC / pprof

| 能力 | 建议开启场景 | 不建议长期全开场景 |
| --- | --- | --- |
| payload 摘要日志 | 单链路排障、格式核对 | 高流量生产全量开启 |
| traffic stats | 日常巡检、吞吐核对 | 日志级别高于 info 时本来就不会输出 |
| GC 周期日志 | 容量分析、内存抖动排查 | 常态稳定运行但不需要 JVM 式观察时 |
| pprof | 性能分析、泄漏分析 | 未做访问控制的公网环境 |

## 7. 热重载操作手册

### 7.1 哪些配置支持热重载

支持：`receivers`、`selectors`、`task_sets`、`senders`、`pipelines`、`tasks`、`version`。

### 7.2 哪些配置不支持热重载

不支持：`control`、`logging`、`business_defaults`。

### 7.3 文件监听方式

- `watchConfigFile` 会按 `control.config_watch_interval` 轮询 business 文件指纹
- 检测到变化后，会调用统一重载链路
- 只监听 business 文件指纹，不直接监听 system 文件

### 7.4 信号触发方式

#### Unix

```bash
kill -HUP <pid>
# 或
kill -USR1 <pid>
```

#### Windows

- 当前 Windows 实现没有 reload signal；建议使用文件监听方式。

### 7.5 重载成功 / 失败日志特征

#### 成功时常见日志

- `reload_begin`
- `reload_config`：业务配置重载已生效
- `reload_done`

#### 失败时常见日志

- `reload_config`：重新加载配置失败
- `reload_config`：系统配置发生变化，重载被拒绝
- `reload_config`：运行时应用新配置失败

### 7.6 失败后旧配置是否继续生效

是。当前实现中，重载失败不会把运行态切到半成品；旧配置继续保持生效。

### 7.7 热重载注意事项

- 改动 `match_key` 时，selector 里的 key 也要同步调整
- 改动 sender 配置时，引用它的 task 会联动重建
- 改动 pipeline 时，引用它的 task 也会联动重建
- 改动 system 配置前，应计划重启而不是热重载
- 高流量时尽量避免频繁连续重载

### 7.8 何时建议重载，何时建议重启

#### 建议热重载

- 新增 / 删除 receiver
- 修改 selector / task_set / task 路由
- 调整 pipeline
- 调整 Kafka sender / receiver 业务参数

#### 建议重启

- 修改 `control`
- 修改 `logging`
- 修改 `business_defaults`
- 大规模协议切换、核心资源模型切换、链路级变更较大时

## 8. 停机操作手册

### 8.1 标准优雅停机方式

#### Unix / Linux

```bash
kill -TERM <pid>
# 或
kill -INT <pid>
```

#### Windows

- 发送 Ctrl+C 或等效中断信号

### 8.2 停机时系统内部会做什么

按当前实现，顺序如下：

1. 记录 `shutdown_begin`
2. 停止 config watcher
3. `cancelRun()` 取消主运行 context
4. 停止 GC 周期日志任务
5. `app.Runtime.Stop()` -> `runtime.Store.StopAll()`
6. 并发停止 receiver
7. 顺序执行每个 task 的 `StopGraceful()`
8. 并发关闭 sender
9. 记录 `shutdown_runtime`
10. `pprof.Shutdown()`
11. 记录 `shutdown_done`

### 8.3 如何判断停机完成

以日志为准：

- `shutdown_runtime`：运行时已停止
- `pprof服务已停止`（如果启用过 pprof）
- `shutdown_done`：**这是优雅停机完成标志**

### 8.4 停机卡住时怎么排查

优先看：

1. receiver 是否未正常退出（网络循环、poll、SFTP 扫描）
2. task 是否有长时间 inflight 未完成
3. sender 是否关闭缓慢
4. 是否在高流量下仍有大量 in-flight 数据等待 drain

### 8.5 强制停止的风险

如果直接 `kill -9` 或等效强制终止：

- task 来不及 `StopGraceful()`
- sender 连接与缓冲可能来不及释放
- SFTP sender 可能留下临时文件
- 最后一批 inflight 数据可能丢失

### 8.6 高流量场景停机注意事项

- 先观察 traffic stats 是否处于高峰
- 如条件允许，先让上游降流再停机
- 停机前确认下游 sender 无明显积压或失败放大
- 若使用 `channel` 模型，需考虑队列 drain 时间

## 9. 运行中观测与巡检

### 9.1 常规巡检建议

- 检查进程是否存在
- 检查启动成功日志是否出现
- 检查 receiver 是否持续有接入统计
- 检查 sender 是否有发送失败日志
- 检查 task 是否有队列满、入队失败、路由 sender 未命中日志

### 9.2 启动后应检查什么

- `service_start` 是否出现
- 对应端口是否已监听
- Kafka / SFTP 远端是否可达
- 是否有持续的流量统计输出

### 9.3 运行中应关注哪些日志

- `receiver traffic stats`
- `task send traffic stats`
- `接收端异常退出`
- `发送端发送失败`
- `任务丢弃数据包`
- `路由发送端未命中任务内发送端`
- `gc_stats`
- `pprof请求处理完成`

### 9.4 traffic stats 怎么看

建议从两个方向看：

- **receiver 侧统计**：看接入量是否正常
- **task send 侧统计**：看有多少流量真正走到发送阶段

若 receiver 有量而 task send 明显偏低，优先怀疑：

- selector 未命中
- pipeline 中途丢弃
- task 停止中不再接受新包
- 路由 sender 未命中

### 9.5 payload 日志什么时候开

建议只在以下场景短时开启：

- 对比上下游 payload 是否被改写
- 验证 framing / chunk / route sender 行为
- 定位 selector 未命中时的 key 与数据内容

### 9.6 GC 周期日志怎么看

重点关注：

- `goroutine数量`
- `heap_alloc`
- `heap_inuse`
- `gc次数`
- `最近一次GC暂停纳秒`
- `gc_cpu_fraction`

### 9.7 pprof 如何开启、访问、关闭

#### 开启

- 配置 `control.pprof_port` 为 `0` 或明确端口
- 注意：`0` 会回写到默认 `6060`

#### 访问

```bash
curl http://127.0.0.1:6060/debug/pprof/
```

#### 关闭

- 把 `pprof_port` 设为 `-1` 并重启进程
- 或直接优雅停机，停机流程会执行 `pprof.Shutdown()`

### 9.8 吞吐下降、积压、发送失败时先看什么

按顺序查看：

1. `task send traffic stats` 与 `receiver traffic stats` 的差距
2. 是否有 `发送端发送失败`
3. 是否有 `任务丢弃数据包：协程池提交失败` 或 `任务丢弃数据包：channel入队时上下文已取消`
4. 是否启用了大量 payload 摘要日志
5. 是否需要开启 pprof 分析 CPU / heap / goroutine

## 10. 常见操作场景

### 10.1 新增 receiver

步骤：

1. 在 business 配置中新增 receiver
2. 绑定已有或新增 selector
3. 确认 `match_key` 模式与 selector 中 key 一致
4. 热重载或重启生效
5. 观察 receiver traffic stats 和 `service_start` 后续行为

### 10.2 新增 sender

步骤：

1. 在 business 配置中新增 sender
2. 在 task 中引用该 sender
3. 如使用 route stage，确认 target sender 名称与 task.senders 一致
4. 热重载后观察 sender 发送结果和失败日志

### 10.3 修改 selector / task_set / task 路由

步骤：

1. 修改 selector 的 `matches` / `default_task_set`
2. 必要时同步修改 `task_sets`
3. 检查 task 是否存在、sender 与 pipeline 是否存在
4. 热重载后观察是否出现 `reload_done`

### 10.4 修改 Kafka 参数

建议流程：

1. 明确修改 receiver 还是 sender
2. 检查 duration、acks、partitioner、balancers 等约束
3. 热重载后观察 Kafka 连接、消费、发送日志
4. 若出现持续异常，回退到上一个稳定配置

### 10.5 修改 `match_key` 模式

步骤：

1. 先确定 receiver 新模式
2. 同步更新 selector 里的完整 key
3. 在测试环境或灰度环境先验证
4. 热重载后重点观察 selector 是否命中、task send 是否恢复

### 10.6 修改 task 执行模型

- 从 `pool` 改到 `fastpath`：关注下游 sender 是否会把延迟反压到热路径
- 从 `pool` 改到 `channel`：关注是否出现串行瓶颈
- 调整后观察 traffic stats、发送失败、队列满日志

### 10.7 开关 payload 日志

- receiver 侧：`log_payload_recv`
- task 侧：`log_payload_send`
- 同时根据需要调整 `payload_log_max_bytes`
- 排障结束后应及时关闭

### 10.8 开关 GC 日志

- 开：`logging.gc_stats_log_enabled=true`
- 关：`logging.gc_stats_log_enabled=false`
- 注意：属于 system 配置，修改后需要重启

### 10.9 开关 pprof

- 开：`control.pprof_port=0` 或指定端口
- 关：`control.pprof_port=-1`
- 注意：属于 system 配置，修改后需要重启

## 11. 常见故障与排查指南

### 11.1 启动失败

**现象**：进程启动后直接退出。  
**可能原因**：参数错误、配置路径错误、JSON 非法、配置校验失败。  
**排查步骤**：
1. 核对启动命令
2. 核对配置文件路径
3. 用 `jq .` 校验 JSON 语法
4. 查看启动日志中的 `config_load` / `config_validate` 错误
**建议处理**：修正命令或配置后重启。

### 11.2 配置校验失败

**现象**：日志提示 `配置校验失败`。  
**可能原因**：引用链断裂、Kafka 参数非法、sender 并发非法、SFTP 指纹错误。  
**排查步骤**：
1. 先看报错指向的具体字段
2. 对照 `docs/configuration.md` 核对约束
3. 核对 selector / task_set / task / pipeline / sender 引用链
**建议处理**：只改报错字段，不要一次引入多处无关变更。

### 11.3 重载失败

**现象**：出现 `reload_begin` 但没有 `reload_done`。  
**可能原因**：配置重新加载失败、system 配置变化、运行时应用失败。  
**排查步骤**：
1. 查看 `reload_config` 日志
2. 判断是“读取失败”“system 变化”“runtime 应用失败”哪一类
3. 对照上一次稳定配置做差异检查
**建议处理**：回退到旧 business 配置；若变更的是 system 配置，改为重启发布。

### 11.4 receiver 收不到数据

**现象**：无 receiver traffic stats 增长。  
**可能原因**：端口未监听、broker / SFTP 不可达、上游没发数据、协议参数不匹配。  
**排查步骤**：
1. 用 `ss -lntup` 确认端口监听
2. 用 `nc -vz` 确认 Kafka / SFTP 地址可达
3. 检查 TCP `frame` 是否与上游一致
4. 检查 Kafka `topic`、认证是否正确
**建议处理**：先恢复连通性和协议参数，再看业务数据。

### 11.5 selector 未命中导致丢弃

**现象**：receiver 有量，task send 没量。  
**可能原因**：`match_key` 模式变了，但 selector key 没同步；默认 task_set 未配置。  
**排查步骤**：
1. 检查 receiver 的 `match_key.mode`
2. 检查 selector `matches` 中写的是不是完整 key
3. 必要时短时开启 payload / debug 观察
**建议处理**：统一 receiver 生成规则和 selector 完整 key。

### 11.6 task 不执行

**现象**：selector 命中后仍无发送。  
**可能原因**：task 配置错误、pipeline 丢弃、task 正在停止、queue 满。  
**排查步骤**：
1. 检查 task 是否在 `task_sets` 中被引用
2. 检查 pipeline 是否可能返回 false
3. 查看是否有 `任务丢弃数据包` 日志
**建议处理**：优先恢复 task 可执行性，再优化参数。

### 11.7 sender 发送失败

**现象**：出现 `发送端发送失败` 或 Kafka 发送失败日志。  
**可能原因**：下游不可达、认证失败、topic/目录不存在、网络抖动。  
**排查步骤**：
1. 检查远端连通性
2. 检查 sender 目标参数
3. 检查 Kafka / SFTP 认证与权限
**建议处理**：先恢复下游可达性与权限，再观察是否恢复。

### 11.8 Kafka 连接 / 消费 / 发送异常

**现象**：Kafka receiver 拉取失败或 Kafka sender 发送失败。  
**可能原因**：broker 不可达、TLS/SASL 不匹配、topic 不存在、group 配置不当。  
**排查步骤**：
1. `nc -vz <broker> <port>`
2. 检查 `client_id`、`tls`、`tls_skip_verify`、`username`、`password`、`sasl_mechanism`
3. 检查 `topic`、`group_id`、`balancers`
4. 检查 `heartbeat_interval < session_timeout`
**建议处理**：认证和网络先恢复，参数再逐项回退到已知稳定值。

### 11.9 SFTP 连接 / 读写异常

**现象**：SFTP receiver scan failed 或 sender 写入失败。  
**可能原因**：地址不可达、账号密码错误、主机指纹错误、目录权限不足。  
**排查步骤**：
1. `nc -vz <host> 22`
2. 检查 `host_key_fingerprint`
3. 检查 `remote_dir` 是否存在且有权限
4. 检查 sender 是否留下 `.part` / 临时后缀文件
**建议处理**：优先修复认证与权限，再观察轮询或写入恢复。

### 11.10 流量统计日志不输出

**现象**：长时间没有 traffic stats。  
**可能原因**：日志级别高于 info、没有创建 counter、链路本身无流量。  
**排查步骤**：
1. 检查 `logging.level`
2. 检查是否真的有 receiver / task 流量
3. 检查 `traffic_stats_interval` 是否过长
**建议处理**：在排障窗口把日志级别调整到 info，并确认链路有真实流量。

### 11.11 payload 日志不输出

**现象**：开启开关后仍看不到 payload 摘要。  
**可能原因**：日志级别不允许 info、未开启对应 receiver/task 开关、链路没有实际经过该点。  
**排查步骤**：
1. 检查 `logging.level`
2. 检查 `log_payload_recv` / `log_payload_send`
3. 检查对应链路是否真的命中 task
**建议处理**：短时调高日志可见级别并仅在目标链路开启。

### 11.12 pprof 访问失败

**现象**：访问 `/debug/pprof/` 失败。  
**可能原因**：`pprof_port=-1`、端口未监听、防火墙拦截。  
**排查步骤**：
1. 检查 `control.pprof_port`
2. 检查端口监听状态
3. 检查本机或网络 ACL
**建议处理**：修正 system 配置并重启，或放通访问控制。

### 11.13 优雅停机卡住

**现象**：出现 `shutdown_begin`，长时间没有 `shutdown_done`。  
**可能原因**：receiver.Stop 未返回、task inflight 很多、sender.Close 阻塞。  
**排查步骤**：
1. 检查最后停留在哪类日志
2. 结合 traffic stats 判断是否仍在高流量 drain
3. 必要时开启 pprof 看 goroutine 阻塞
**建议处理**：先降流或等待 drain；只有确认无法恢复时再考虑强制停止。

### 11.14 性能下降或吞吐不足

**现象**：接入量或发送量下降，延迟变高。  
**可能原因**：执行模型不合适、下游变慢、日志开销过大、Kafka/SFTP 参数不当。  
**排查步骤**：
1. 看 receiver / task send traffic stats 差距
2. 看是否有 queue 满、发送失败、receiver 异常退出日志
3. 评估 `execution_model`、`pool_size`、`channel_queue_size`
4. 必要时启用 pprof
**建议处理**：先恢复最关键瓶颈，再做参数优化。

## 12. 值班与维护建议

### 12.1 日常变更建议

- 每次只改一类配置，避免混合改动难以回溯
- 先在测试环境验证 `match_key`、route sender、Kafka/SFTP 参数
- 保留上一个稳定版本的 business 配置副本

### 12.2 发布前检查

- 配置文件已校验
- 连通性已校验
- 示例与实际参数差异已记录
- 回退方案已准备

### 12.3 重载前检查

- 确认本次改动只涉及 business 配置
- 确认 selector 与 `match_key` 已同步
- 确认 sender / pipeline / task 引用链完整

### 12.4 停机前检查

- 是否还在高峰流量
- 是否需要先通知上游降流
- 是否允许等待 in-flight drain
- 是否有未完成的 SFTP 文件写入

### 12.5 高风险配置项提醒

- `match_key.mode`
- `selector.matches`
- `default_task_set`
- `execution_model`
- `partitioner` / `record_key_source`
- `auto_commit` / `group_id`
- `host_key_fingerprint`
- `control.pprof_port`
- `logging.level`

### 12.6 哪些改动建议先在测试环境验证

- 新增协议类型 receiver / sender
- 修改 `match_key` 模式
- 修改 selector 路由
- 调整 task 执行模型
- 调整 Kafka 关键参数
- 调整 SFTP 目录、权限与文件写入策略

## 13. 附录

### 13.1 常用启动命令

```bash
./bin/forward-stub -system-config ./configs/system.example.json -business-config ./configs/business.example.json
```

### 13.2 常用重载命令

```bash
kill -HUP <pid>
kill -USR1 <pid>
```

### 13.3 常用停机命令

```bash
kill -TERM <pid>
kill -INT <pid>
```

### 13.4 常用排查命令

```bash
ss -lntup
nc -vz 127.0.0.1 9092
nc -vz 127.0.0.1 22
curl http://127.0.0.1:6060/debug/pprof/
```

### 13.5 文档索引

- `README.md`
- `docs/architecture.md`
- `docs/configuration.md`
- `docs/receivers-and-senders.md`
- `docs/task-and-dispatch.md`
- `docs/pipeline.md`
- `docs/execution-model.md`
- `docs/observability.md`
- `docs/runtime-and-lifecycle.md`
- `docs/deployment.md`
- `docs/skydds.md`
- `docs/operations-manual.md`
