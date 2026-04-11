# Pipeline 与 Stage 配置说明

## 1. pipeline 的角色

pipeline 是 task 内部的数据处理链，位于：

```text
receiver -> selector -> task -> pipeline -> sender
```

它不负责：

- 选择 task
- 选择 selector
- 解析 receiver 主路由

补充说明：

- pipeline 只负责 task 内部按当前支持的 stage 处理 packet。

## 2. 当前支持的 stage 类型

### 2.1 `match_offset_bytes`

- 作用：校验 payload 指定偏移处的字节序列是否匹配。
- 常用字段：`offset`、`hex`
- 结果：不匹配则当前 pipeline 返回 false，task 直接停止后续处理。

### 2.2 `replace_offset_bytes`

- 作用：把 payload 指定偏移处替换成新字节串。
- 常用字段：`offset`、`hex`

### 2.3 `route_offset_bytes_sender`

- 作用：从 payload 某个偏移读固定长度字节，映射到 sender 名称，并写入 `packet.Meta.RouteSender`。
- 常用字段：`offset`、`cases`、`default_sender`

## 3. `route_offset_bytes_sender` 的约束

- `cases` 不能为空。
- `cases` 的 key 必须是合法十六进制字符串。
- 所有 case key 的字节长度必须一致。
- `cases` 的 value 不能为空。
- `default_sender` 可选；为空表示未命中时不路由到任何 sender。

## 4. route sender 与 task.senders 的关系

这是当前配置里最容易配错的地方：

- route stage 选中的 sender 名称，**必须已经出现在当前 task 的 `senders` 列表中**。
- 否则配置校验会报错。

## 5. 常见示例

### 5.1 按固定头匹配并替换

```json
[
  {"type": "match_offset_bytes", "offset": 0, "hex": "aabb"},
  {"type": "replace_offset_bytes", "offset": 2, "hex": "ccdd"}
]
```

### 5.2 按首字节路由到不同 sender

```json
[
  {
    "type": "route_offset_bytes_sender",
    "offset": 0,
    "cases": {
      "01": "tx_udp",
      "02": "tx_tcp"
    },
    "default_sender": "tx_kafka"
  }
]
```
