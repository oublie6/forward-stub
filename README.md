# forward-stub

`forward-stub` 是一个面向高吞吐、低延迟、支持热更新的 Go 转发引擎。当前主链路为：

```text
receiver -> selector -> task(pipeline + sender)
```

本文只保留**配置入口说明、最小示例、示例索引、文档索引**。字段枚举、默认值、热更新边界、协议细节以 `docs/` 中的权威文档为准。

## 1. 配置入口说明

项目使用双配置启动方式：

- `system config`：只放**系统级配置**，包含 `control`、`logging`、`business_defaults`。
- `business config`：只放**业务拓扑配置**，包含 `version`、`receivers`、`selectors`、`task_sets`、`senders`、`pipelines`、`tasks`。

默认值规则：

- system 配置只有两层来源：system 显式值、代码硬编码默认值。
- business 配置只有三层来源：business 显式值、`system.business_defaults`、代码硬编码默认值。
- `SystemConfig.Merge(BusinessConfig)` 只负责拼装完整配置，不负责默认值回填。
- 最终默认值统一由 `Config.ApplyDefaults(system.business_defaults)` 规范化；运行时构建阶段原则上不再承担通用默认值回填职责。

启动命令：

```bash
./bin/forward-stub \
  -system-config ./configs/system.example.json \
  -business-config ./configs/business.example.json
```

> 说明：当前实现要求 JSON 严格匹配代码结构，未知字段会直接报错；因此示例文件和文档必须与代码保持一致。

## 2. 最小可运行示例

### 2.1 system 配置

```json
{
  "control": {
    "timeout_sec": 5,
    "config_watch_interval": "2s",
    "pprof_port": -1
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
    "gc_stats_log_enabled": false,
    "gc_stats_log_interval": "1m"
  }
}
```

### 2.2 business 配置

```json
{
  "receivers": {
    "rx_udp": {
      "type": "udp_gnet",
      "listen": "0.0.0.0:19000",
      "selector": "sel_default"
    }
  },
  "selectors": {
    "sel_default": {
      "default_task_set": "ts_forward"
    }
  },
  "task_sets": {
    "ts_forward": ["task_forward"]
  },
  "senders": {
    "tx_udp": {
      "type": "udp_unicast",
      "local_ip": "0.0.0.0",
      "local_port": 20000,
      "remote": "127.0.0.1:21000"
    }
  },
  "pipelines": {
    "pipe_passthrough": []
  },
  "tasks": {
    "task_forward": {
      "pipelines": ["pipe_passthrough"],
      "senders": ["tx_udp"],
      "execution_model": "fastpath"
    }
  }
}
```

对应现成文件：

- `configs/minimal.system.example.json`
- `configs/minimal.business.example.json`

## 3. 示例配置索引

### 3.1 全量/主示例

- `configs/system.example.json`：**system 全量示例**。覆盖 `control`、`logging`、`business_defaults` 全字段。
- `configs/business.example.json`：**business 全量示例**。覆盖 receiver / selector / task_set / sender / pipeline / task 全部配置域，并尽量展示不同协议与执行模型。

### 3.2 场景拆分示例

- `configs/minimal.system.example.json` + `configs/minimal.business.example.json`：最小可运行示例。
- `configs/udp-tcp.business.example.json`：UDP/TCP 接入与转发示例。
- `configs/kafka.business.example.json`：Kafka receiver / sender 专项示例。
- `configs/sftp.business.example.json`：SFTP receiver / sender 专项示例。
- `configs/local-timer.business.example.json`：本地定时固定报文造数示例，包含 1 秒 2 帧链监和 1 秒 N 帧压测。
- `configs/task-models.business.example.json`：`fastpath` / `pool` / `channel` 三种 task 执行模型示例。
- `configs/bench.example.json`：benchmark 驱动配置，不属于运行时主配置。
- `deploy/k8s/`：默认使用 `system.json + business.json` 双配置挂载方式，不再示例单文件 `config.json`。

## 4. 文档索引

- `docs/configuration.md`：**配置字段权威入口**，列出字段、类型、默认值、枚举、适用范围、约束与示例一致性要求。
- `docs/architecture.md`：**架构总览入口**，说明系统结构、模块职责、热路径原则和扩展点索引。
- `docs/receivers-and-senders.md`：**协议行为权威入口**，解释各 receiver / sender 的专属配置、match key 行为和协议边界。
- `docs/task-and-dispatch.md`：**路由与任务权威入口**，说明 selector、task_set、task、dispatch、route sender 的关系。
- `docs/pipeline.md`：**pipeline stage 权威入口**，说明 stage 类型、字段约束和丢弃语义。
- `docs/execution-model.md`：`fastpath`、`pool`、`channel` 的配置与选型建议。
- `docs/runtime-and-lifecycle.md`：默认值生效层次、初始化冷路径、热更新边界、停机顺序和关键时序摘要。
- `docs/observability.md`：logging、payload 日志、流量统计、GC 日志、pprof 等配置与排障方式。
- `docs/deployment.md`：本地、Docker、Kubernetes 部署入口。
- `docs/skydds.md`：SkyDDS SDK、cgo 构建和 `dds_skydds` 协议说明。
- `docs/operations-manual.md`：运维、实施、测试和值班人员的标准操作手册。

`docs/internal/` 下只保存 roadmap、代码审查等内部记录，不作为对外主文档入口。

## 5. 配置使用上的关键规则

- `receiver.selector` 必填；receiver 不再由 task 反向绑定。
- `selector.matches` 的 value 必须是 `task_set` 名称，而不是 task 名称。
- `task_sets` 只做复用；运行时会在编译期直接展开为 task 切片。
- Kafka、SFTP、gnet、组播等字段都只在对应 `type` 下生效，不能跨协议混用。
- `receiver.match_key` 为 receiver 局部配置：留空时保持历史兼容 key；显式配置后会在 receiver 初始化/热重载时编译成协议专属 builder，热路径不再走统一公共拼接函数。
- Kafka receiver / sender 的多项字段会直接映射到 franz-go / `kgo` 选项；项目级默认值在统一默认值入口回写，少量协议语义或第三方库默认行为保留在具体组件中解释。
- 字符串 duration 字段必须是合法且大于 0 的 `time.ParseDuration` 文本，例如 `250ms`、`10s`、`5m`。
- `control.pprof_port`：`-1` 表示禁用，`0` 表示回退默认值 `6060`，`1~65535` 表示监听对应端口。

## 5.1 receiver match key 说明

- match key 生成职责已经收敛到各类 receiver 自身；selector 只做完整字符串精确匹配。
- `receiver.match_key.mode` 留空时保持兼容默认行为：UDP/TCP 继续输出历史 `src_addr`，Kafka 继续输出 `topic + partition`，SFTP 继续输出 `remote_dir + file_name`。
- 显式配置模式后，会在 receiver 初始化或热重载重建阶段预编译为协议专属 builder；UDP/TCP 热路径只做一次轻量函数调用，TCP/SFTP 还会尽量按连接或文件复用已生成的 key。
- 详见 `docs/configuration.md`、`docs/receivers-and-senders.md`、`docs/runtime-and-lifecycle.md`。

## 6. 验证命令

```bash
go test ./... -count=1
```

如果只想先验证配置文件能否解析并通过校验，可直接运行仓库内对应测试，或使用：

```bash
go test ./src/config -run 'Example|Test.*Example' -count=1
```

如需运行仓库内现有 benchmark，请使用：

```bash
go test ./src/runtime -bench . -benchmem -run '^$'
```

更多测试、race 和分模块 benchmark 命令见 `docs/benchmark.md`。

## 7. SkyDDS（dds_skydds）轻量字节桥接

`dds_skydds` receiver/sender 支持 SkyDDS `OctetMsg` 与 `BatchOctetMsg` 字节桥接。完整 SDK 目录约定、cgo/C++ 调用链、Batch 语义和测试方法见 `docs/skydds.md`。

```bash
./scripts/skydds/setup_linux.sh
source ./scripts/skydds/env.sh
CGO_ENABLED=1 go build -tags skydds -o bin/forward-stub .
./scripts/skydds/test_sender.sh
./scripts/skydds/test_receiver.sh
./scripts/skydds/test_loop.sh
```

示例配置：
- Octet: `configs/skydds.business.example.json`
- Batch: `configs/skydds-batch.business.example.json`
