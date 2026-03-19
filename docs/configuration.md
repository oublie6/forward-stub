# 配置模型

## 1. 总体结构

### system config

系统配置只包含：

- `control`
- `logging`
- `business_defaults`

### business config

业务配置包含：

- `version`
- `receivers`
- `selectors`
- `task_sets`
- `senders`
- `pipelines`
- `tasks`

## 2. receivers

每个 receiver 都必须显式绑定一个 selector：

```json
"receivers": {
  "rx_udp": {
    "type": "udp_gnet",
    "listen": "0.0.0.0:19000",
    "selector": "sel_ingress"
  }
}
```

关键点：

- `selector` 是 receiver 到 selector 的唯一绑定。
- receiver 不再由 task 反向引用。

## 3. selectors

selector 只支持完整字符串精确匹配：

```json
"selectors": {
  "sel_ingress": {
    "matches": {
      "udp|src_addr=10.0.0.10:9000": "ts_realtime",
      "tcp|src_addr=10.0.0.10:9000": "ts_realtime"
    },
    "default_task_set": "ts_realtime"
  }
}
```

规则说明：

- `matches`：`match key -> task_set_name`
- `default_task_set`：未命中时回退
- 不支持通配符、表达式、字段推断、规则优先级系统

## 4. task_sets

```json
"task_sets": {
  "ts_realtime": ["task_parse", "task_forward"],
  "ts_archive": ["task_archive"]
}
```

约束：

- task set 必须引用已存在的 task。
- 空 task set 不允许。
- 运行时会直接展开成 `match key -> []*TaskState`。

## 5. tasks

`task` 只定义执行逻辑：

```json
"tasks": {
  "task_forward": {
    "pipelines": ["pipe_route_sender"],
    "senders": ["tx_udp", "tx_kafka"],
    "execution_model": "pool"
  }
}
```

现在 task 不再承担 receiver 绑定职责。

## 6. 配置校验规则

系统会在加载后校验：

- receiver 绑定的 selector 是否存在
- selector 引用的 task set 是否存在
- task set 引用的 task 是否存在
- task 的 pipeline / sender 是否存在
- execution model 是否合法
- sender / receiver 协议字段是否完整

## 7. 推荐实践

- 用 receiver 表达协议接入。
- 用 selector 表达固定 key 路由。
- 用 task set 表达复用。
- 用 task 表达执行。
- 不要再把 `task.receivers` 当成主配置模型。
