# forward-stub

`forward-stub` 是一个面向高吞吐、低延迟、支持热更新的 Go 转发引擎。当前主链路为：

```text
receiver -> selector -> task(pipeline + sender)
```

本文只保留**配置入口说明、最小示例、示例索引、文档索引**；完整字段手册请直接查看 `docs/configuration.md`。

## 1. 配置入口说明

项目支持两种启动方式：

### 1.1 推荐：双配置模式

- `system config`：只放**系统级配置**，包含 `control`、`logging`、`business_defaults`。
- `business config`：只放**业务拓扑配置**，包含 `version`、`receivers`、`selectors`、`task_sets`、`senders`、`pipelines`、`tasks`。

启动命令：

```bash
./bin/forward-stub \
  -system-config ./configs/system.example.json \
  -business-config ./configs/business.example.json
```

### 1.2 兼容：单文件模式

单文件模式只支持 `config.Config` 中的字段，也就是：`version`、`control`、`logging`、`receivers`、`selectors`、`task_sets`、`senders`、`pipelines`、`tasks`。

**不支持** `business_defaults`；如需系统级业务默认值，请使用双配置模式。

单文件示例：

```bash
./bin/forward-stub -config ./configs/example.json
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
    "payload_pool_max_cached_bytes": 0,
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

- `configs/example.json`：**单文件全量示例**。覆盖单文件模式当前真实支持的全部字段；注意它**不包含** `business_defaults`。
- `configs/system.example.json`：**system 全量示例**。覆盖 `control`、`logging`、`business_defaults` 全字段。
- `configs/business.example.json`：**business 全量示例**。覆盖 receiver / selector / task_set / sender / pipeline / task 全部配置域，并尽量展示不同协议与执行模型。

### 3.2 场景拆分示例

- `configs/minimal.system.example.json` + `configs/minimal.business.example.json`：最小可运行示例。
- `configs/udp-tcp.business.example.json`：UDP/TCP 接入与转发示例。
- `configs/kafka.business.example.json`：Kafka receiver / sender 专项示例。
- `configs/sftp.business.example.json`：SFTP receiver / sender 专项示例。
- `configs/task-models.business.example.json`：`fastpath` / `pool` / `channel` 三种 task 执行模型示例。
- `configs/bench.example.json`：benchmark 驱动配置，不属于运行时主配置。

## 4. 文档索引

- `docs/configuration.md`：**完整配置参考手册**，逐项列出字段、类型、默认值、枚举、适用范围、约束与注意事项。
- `docs/receivers-and-senders.md`：按协议类型解释 receiver / sender 的专属配置和行为差异。
- `docs/task-and-dispatch.md`：说明 selector、task_set、task、dispatch、route sender 的关系。
- `docs/execution-model.md`：说明 `fastpath`、`pool`、`channel` 的配置与选型建议。
- `docs/observability.md`：说明 logging、payload 日志、流量统计、GC 日志、pprof 等配置与排障方式。
- `docs/runtime-and-lifecycle.md`：说明配置加载、默认值合并、校验、热更新和启动/停机顺序。
- `docs/pipeline.md`：说明 pipeline stage 类型与字段约束。

## 5. 配置使用上的关键规则

- `receiver.selector` 必填；receiver 不再由 task 反向绑定。
- `selector.matches` 的 value 必须是 `task_set` 名称，而不是 task 名称。
- `task_sets` 只做复用；运行时会在编译期直接展开为 task 切片。
- `task.fast_path` 是兼容字段：仅当 `execution_model` 为空时才会把 `fast_path=true` 解释为 `fastpath`。
- Kafka、SFTP、gnet、组播等字段都只在对应 `type` 下生效，不能跨协议混用。
- 字符串 duration 字段必须是合法且大于 0 的 `time.ParseDuration` 文本，例如 `250ms`、`10s`、`5m`。
- `control.pprof_port`：`-1` 表示禁用，`0` 表示回退默认值 `6060`，`1~65535` 表示监听对应端口。

## 6. 本次文档治理覆盖范围

本次已按“**代码实际支持的配置**”对齐以下内容：

- 顶层配置：`version`、`control`、`logging`、`receivers`、`selectors`、`task_sets`、`senders`、`pipelines`、`tasks`、`business_defaults`。
- logging：日志级别、文件滚动、流量统计、payload 日志、payload 池、GC 周期日志。
- receivers：UDP/TCP gnet、Kafka、SFTP 及其类型专属字段。
- senders：UDP 单播、UDP 组播、TCP、Kafka、SFTP 及其类型专属字段。
- selector / task_set / task / pipeline：引用关系、默认值、执行模型、stage 参数、route sender 行为。
- 示例文件：单文件全量、system/business 全量、最小示例、按协议/执行模型拆分示例。

## 7. 验证命令

```bash
go test ./src/config ./src/bootstrap ./...
```

如果只想先验证配置文件能否解析并通过校验，可直接运行仓库内对应测试，或使用：

```bash
go test ./src/config -run Example
```
