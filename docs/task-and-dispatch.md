# Selector、Task Set 与 Dispatch

## 1. selector 的职责

selector 只做一件事：

```text
match key -> task set
```

并且是：

- 完整字符串精确匹配
- 可选 default task set
- 无规则遍历
- 无 DSL
- 无表达式引擎

## 2. task set 的职责

task set 只负责配置复用：

```json
"task_sets": {
  "ts_order_shared": ["task_normalize", "task_forward"]
}
```

多个 key 可以复用同一个 task set：

```json
"matches": {
  "kafka|topic=order|partition=0": "ts_order_shared",
  "kafka|topic=order|partition=1": "ts_order_shared"
}
```

## 3. dispatch 的职责

dispatch 现在不再按 receiver 名字查“订阅 task”。它只做：

1. 读取 receiver 当前绑定的 selector 快照
2. 用 `packet.Meta.MatchKey` 做一次 map lookup
3. 将结果 fanout 到 task

## 4. 默认行为

- 命中：执行命中的 task slice
- 未命中且有 `default_task_set`：执行默认 task slice
- 未命中且无 default：直接丢弃
