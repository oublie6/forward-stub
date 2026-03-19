# forward-stub

`forward-stub` 是一个面向高吞吐、低延迟、支持热更新的 Go 转发引擎。本次架构已经从旧的 `receiver -> task(pipeline + sender)` 重构为新的主链路：

```text
receiver -> selector -> task(pipeline + sender)
```

核心目标只有三个：**职责边界明确、热路径极简、配置可维护**。

## 架构摘要

### 1. 新职责划分

- **receiver**：负责协议接入、构造 `packet.Packet`、显式生成唯一 `match key`。
- **selector**：只负责 `match key -> task set` 的精确匹配；不解析协议，不猜字段语义。
- **task set**：只做配置复用；编译后会直接展开成 `[]*TaskState`，不会在热路径保留额外跳转。
- **task**：负责执行 `pipeline + sender`。
- **runtime / dispatch**：只负责串联 `receiver -> selector -> task`，热路径只做一次 key lookup 和 fanout。

### 2. match key 规则

所有协议都由各自 receiver 显式构造唯一 key：

- UDP：`udp|src_addr=1.1.1.1:9000`
- TCP：`tcp|src_addr=1.1.1.1:9000`
- Kafka：`kafka|topic=order|partition=3`
- SFTP：`sftp|remote_dir=/input|file_name=a.txt`

`selector` 只做完整字符串精确匹配，不再从 `Meta.Remote`、topic/path 的偶然形态里推断语义。

## 快速开始

### 编译

```bash
make build
```

或：

```bash
go build -mod=vendor -o bin/forward-stub .
```

### 启动（推荐双配置模式）

```bash
./bin/forward-stub \
  -system-config ./configs/system.example.json \
  -business-config ./configs/business.example.json
```

### 启动（legacy 单文件示例）

```bash
./bin/forward-stub -config ./configs/example.json
```

## 配置总览

### system config

`configs/system.example.json` 仍然只负责：

- `control`
- `logging`
- `business_defaults`

### business config

新的 business 配置核心字段为：

- `receivers`
- `selectors`
- `task_sets`
- `senders`
- `pipelines`
- `tasks`

### 典型配置关系

```json
{
  "receivers": {
    "rx_kafka_order": {
      "type": "kafka",
      "listen": "127.0.0.1:9092",
      "selector": "sel_kafka_order",
      "topic": "order"
    }
  },
  "selectors": {
    "sel_kafka_order": {
      "matches": {
        "kafka|topic=order|partition=0": "ts_order_shared",
        "kafka|topic=order|partition=1": "ts_order_shared"
      },
      "default_task_set": "ts_order_shared"
    }
  },
  "task_sets": {
    "ts_order_shared": ["task_normalize", "task_forward"]
  },
  "tasks": {
    "task_normalize": {
      "pipelines": ["pipe_normalize"],
      "senders": ["tx_kafka"]
    },
    "task_forward": {
      "pipelines": [],
      "senders": ["tx_udp"]
    }
  }
}
```

这个例子展示了：**多个 match key 可以复用同一个 task set**，但运行时会在编译期直接展开，不会留下 `key -> task_set -> tasks` 的额外热路径跳转。

## 运行时处理流程

```text
1. receiver 收到数据
2. receiver 构造 packet + match key
3. selector 用 match key 做一次 map 查找
4. runtime 取到 task slice 并 fanout
5. task 执行 pipeline
6. sender 输出
```

## 文档索引

- 架构说明：`docs/architecture.md`
- 配置模型：`docs/configuration.md`
- 运行时与热更新：`docs/runtime-and-lifecycle.md`
- receiver / sender：`docs/receivers-and-senders.md`
- selector / task / dispatch：`docs/task-and-dispatch.md`
- pipeline：`docs/pipeline.md`

## 验证命令

```bash
go test ./...
```

如需基准测试：

```bash
go test ./src/runtime -bench BenchmarkScenarioForwarding -benchmem
```
