# 可观测性与相关配置

## 1. 可观测性入口一览

当前代码内的主要观测手段有：

- 结构化日志（`logging.*`）
- 流量统计日志（`traffic_stats_interval`、`traffic_stats_sample_every`）
- receiver / task 级 payload 摘要日志
- pprof（`control.pprof_port`）
- GC 周期日志（`gc_stats_log_enabled`、`gc_stats_log_interval`）
- benchmark 配置（`configs/bench.example.json`）

## 2. logging 配置如何影响运行时

### 2.1 基础日志输出

- `logging.level`：控制常规日志级别，同时也会被 gnet sender/receiver 用来解析 gnet 自身日志级别。
- `logging.file`：为空输出 stderr；非空则使用滚动日志文件。
- `max_size_mb` / `max_backups` / `max_age_days` / `compress`：控制日志轮转策略。

### 2.2 流量统计日志

- `traffic_stats_interval`：统计输出周期。
- `traffic_stats_sample_every`：采样倍率，`1` 表示每条都统计。

当前代码会在以下位置按 info 级别输出聚合流量统计：

- receiver 侧：UDP、TCP、Kafka、SFTP 接收流量。统计句柄在各自 `Start()` 时创建，真正收到首包、首条 record、首个 chunk 后才开始 `AddBytes()`。
- task 侧：任务发送流量。统计句柄在 `Task.Start()` 时创建。
- `trafficStatsHub` 后台 flush 线程会在第一次 `AcquireTrafficCounter()` 时惰性启动。

当前 task 聚合统计会附带执行模型专属运行时字段：

- 公共字段：`execution_model`、`inflight`
- pool 模式附加：`pool_size`、`worker_pool.running`、`worker_pool.free`、`worker_pool.waiting`
- channel 模式附加：`channel.queue_size`、`channel.queue_used`、`channel.queue_available`
- fastpath 模式：只输出最小必要字段，不再伪造 pool/channel 指标

### 2.3 payload 摘要日志

#### receiver 侧

- 开关：`receiver.log_payload_recv`
- 截断长度：`receiver.payload_log_max_bytes`
- 回退：`logging.payload_log_max_bytes`

#### task 侧

- 开关：`task.log_payload_send`
- 截断长度：`task.payload_log_max_bytes`
- 回退：`logging.payload_log_max_bytes`

### 2.4 payload 池上限

- `0` 表示默认不限制。
- 负数会被默认值逻辑修正为 `0`，同时校验也要求它不能小于 `0`。

## 3. GC 周期日志

### 3.1 配置项

| 字段 | 默认值 | 说明 |
|---|---|---|
| `logging.gc_stats_log_enabled` | `false` | 是否开启 GC 周期日志。 |
| `logging.gc_stats_log_interval` | `1m` | GC 周期日志间隔，必须是合法且 `>0` 的 duration。 |

### 3.2 日志内容

当前固定输出：

- 采样时间
- goroutine 数量
- `heap_alloc`
- `heap_inuse`
- `heap_sys`
- `stack_inuse`
- `next_gc`
- GC 次数
- 最近一次 GC 时间
- 最近一次 GC 暂停纳秒
- `gc_cpu_fraction`

### 3.3 生命周期

- logger 初始化后启动。
- 收到停机信号时随主 context 一起停止。
- 如果配置间隔非法，运行时会回退到默认值并打印告警。

## 4. pprof

### 4.1 配置项

- `control.pprof_port=-1`：禁用。
- `control.pprof_port=0`：会先在默认值阶段回写为 `6060`。
- `control.pprof_port=6060`：监听 `:6060`。

### 4.2 常用接口

- `/debug/pprof/`
- `/debug/pprof/profile`
- `/debug/pprof/heap`
- `/debug/pprof/goroutine`
- `/debug/pprof/trace`

### 4.3 当前实现特点

- 每次 pprof 请求都会打印结构化访问日志。
- 停机时会对 pprof server 做显式 `Shutdown`。

## 5. benchmark 配置（`configs/bench.example.json`）

该文件不属于运行时主配置结构，而是 benchmark 驱动自己的参数集。当前字段如下：

| 字段 | 说明 |
|---|---|
| `mode` | benchmark 运行模式。 |
| `duration` | 正式压测时长。 |
| `warmup` | 预热时长。 |
| `payload_size` | payload 字节数。 |
| `workers` | 并发 worker 数。 |
| `pps_per_worker` | 每个 worker 固定 PPS；`0` 时可配合 sweep。 |
| `pps_sweep` | 多组 PPS 采样点。 |
| `multicore` | 是否启用多核。 |
| `udp_sink_readers` | UDP sink reader 数。 |
| `udp_sink_read_buf` | UDP sink 读缓冲。 |
| `task_fast_path` | benchmark 中是否使用 fast path。 |
| `task_pool_size` | benchmark 中 task pool 大小。 |
| `log_level` | benchmark 日志级别。 |
| `log_file` | benchmark 日志文件。 |
| `traffic_stats_interval` | benchmark 流量统计周期。 |

## 6. 观测与配置联动建议

### 6.1 排障窗口建议

- 开启 `receiver.log_payload_recv` 或 `task.log_payload_send` 时，只建议针对单链路、短时间开启。
- 如需观察内存与 GC 抖动，再开启 `gc_stats_log_enabled`。

### 6.2 生产默认建议

- `logging.level=info` 或 `warn`
- `gc_stats_log_enabled=false`
- `payload` 摘要日志默认关闭
- `traffic_stats_interval` 设为 `1s~10s` 之间的可观测窗口
