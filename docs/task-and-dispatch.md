# Selector、Task Set、Task 与 Dispatch

> 职责边界：本文负责 selector、task_set、task、dispatch、fan-out 和 route sender 的路由关系。不展开协议字段、pipeline stage 字段、task 执行模型容量细节或 runtime 热更新顺序；分别见 `docs/receivers-and-senders.md`、`docs/pipeline.md`、`docs/execution-model.md` 和 `docs/runtime-and-lifecycle.md`。

## 1. 配置引用链路

当前配置引用关系固定为：

```text
receiver.selector -> selectors.<name>
selector.matches/default_task_set -> task_sets.<name>
task_sets.<name>[] -> tasks.<name>
tasks.<name>.pipelines[] -> pipelines.<name>
tasks.<name>.senders[] -> senders.<name>
```

这条链路里没有“task 直接绑定 receiver”的旧模型了。

## 2. selector 的职责

selector 只做：

```text
match key -> task_set
```

字段说明：

- `matches`：显式命中表。
- `default_task_set`：未命中时的默认 task_set。

关键规则：

- key 必须由 receiver 生成的**完整 match key**；selector 本身不再参与 key 拼接。
- value 必须是 task_set 名称。
- 不支持通配符、正则、优先级、表达式。

## 3. task_set 的职责

task_set 只是 task 名称数组，例如：

```json
"task_sets": {
  "ts_realtime": ["task_parse", "task_forward"]
}
```

它的用途只有两个：

1. 让多个 match key 复用同一组 task。
2. 让配置结构比直接在 selector 中写 task 数组更清晰。

### 3.1 为什么 task_set 不在热路径里保留

运行时会先把：

```text
match key -> task_set_name
```

编译成：

```text
match key -> []*TaskState
```

这样 dispatch 热路径就只需要一次 map lookup，不需要再多跳一层 task_set。

## 4. task 的职责

task 决定的是“命中之后怎么处理”：

- 执行哪些 pipeline。
- 把结果送到哪些 sender。
- 采用哪种 execution model。
- 是否打印发送前 payload 摘要。

### 4.1 `execution_model`

- task 执行模型只通过 `execution_model` 指定。
- 未显式填写时，默认执行模型为 `pool`。

## 5. dispatch 的真实热路径

当前 runtime 热路径如下：

```text
1. receiver 收到数据
2. receiver 用各自已编译的 builder 构造 packet + match key
3. dispatch 读取 receiver 当前 selector 快照
4. 用 match key 做一次 map 精确查找
5. 命中 task 切片后 fan-out
```

### 5.1 未命中的行为

- 命中 `matches`：执行命中的 task 列表。
- 未命中但配置了 `default_task_set`：执行默认 task 列表。
- 未命中且没有 `default_task_set`：直接丢弃。

### 5.2 多 task fan-out 的复制语义

- 如果只命中 1 个 task，原始 packet 会直接复用。
- 如果命中多个 task，runtime 会为第 2 个及之后的 task 逐个 `Clone()`，避免共享 packet 带来释放时序冲突。

## 6. route sender 与 selector 的边界

两者经常被混淆，但职责完全不同：

### selector

- 决定：**这条数据应该进入哪些 task**。
- 输入：`packet.Meta.MatchKey`。
- 输出：`[]*TaskState`。

### route stage（`route_offset_bytes_sender`）

- 决定：**当前 task 内最终选哪个 sender**。
- 输入：payload 中某个偏移的字节内容。
- 输出：`packet.Meta.RouteSender`。

### 必须记住的约束

- route stage 命中的 sender 必须已经在当前 `task.senders` 中列出。
- route stage 不会改变 selector 的匹配结果。

## 7. selector 与 pipeline stage 的边界

这次改造里需要特别强调：

- selector 仍然只负责 `match key -> task_set -> tasks` 的主路由。
- pipeline stage 只在 task 内部按顺序处理 packet。
- selector 不参与 pipeline 内部处理细节。

## 8. match key 示例

| 协议 | match key 示例 |
|---|---|
| UDP | `udp|src_addr=10.0.0.1:9000`（兼容默认） / `udp|remote_ip=10.0.0.1` |
| TCP | `tcp|src_addr=10.0.0.1:9000`（兼容默认） / `tcp|local_port=19001` |
| Kafka | `kafka|topic=orders|partition=0`（兼容默认） / `kafka|topic=orders` |
| SFTP | `sftp|remote_dir=/input|file_name=orders.csv`（兼容默认） / `sftp|filename=orders.csv` |
| local_timer | `local|receiver=local_timer`（兼容默认） / `local|fixed=chain-monitor` |

## 9. 配置设计建议

- 用 receiver 表达**接入协议**。
- 用 selector 表达**主路由**。
- 用 task_set 表达**复用**。
- 用 task 表达**处理与发送**。
- 用 pipeline 表达**数据变换或 task 内 sender 选择**。
