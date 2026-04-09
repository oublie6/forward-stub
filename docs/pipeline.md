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

- 这次改造**不是**为了引入新的协议互转能力。
- 当前架构本身已经支持 `file -> realtime`、`realtime -> file` 等多协议/多语义互转。
- 本次新增/调整的缓冲型 stage，只负责解决不同协议或不同输入来源带来的**数据粒度不一致**问题，不改变主路由模型。

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

### 2.4 `split_file_chunk_to_packets`

- 作用：把一个 `file_chunk` packet 拆成多个实时 packet（`PayloadKindStream`）。
- 常用字段：`packet_size`、`preserve_file_meta`
- 关键语义：
  - 按 `packet_size` 顺序切分 payload；
  - 子包 `offset` 递增；
  - 仅最后一个子包继承 `EOF=true`（前提是输入包本身 EOF）；
  - `preserve_file_meta=false` 时清理 `transfer_id/file_name/file_path/total_size/checksum`。

### 2.5 `stream_packets_to_file_segments`

- 作用：将连续实时 packet 组装成“滚动文件段”的 `file_chunk` packet。
- 常用字段：`segment_size`、`chunk_size`、`path`、`file_prefix`、`time_layout`
- 关键语义：
  - 以 `segment_size` 为文件段滚动阈值；
  - 每个段满后，按 `chunk_size` 产出多个 `file_chunk`；
  - 每段文件名带“首包时间 + 自增序号”；
  - 每段最后一个 chunk 标记 `EOF=true`，用于下游 SFTP sender 提交 rename。
  - 缓冲分桶统一按 `stage instance + receiver_name + match_key` 生效；
  - 其中 `buffer_key = receiver_name + "|" + match_key`；
  - `selector` 不参与缓冲分桶；
  - `task` 不显式进入 buffer key，因为 stage 实例天然是 task 专属的；
  - 当 `receiver_name` 或 `match_key` 为空时，stage 会记录明确错误日志并拒绝进入缓冲，避免歧义 key。

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

### 5.3 file_chunk 拆实时包（file -> realtime）

```json
[
  {"type": "split_file_chunk_to_packets", "packet_size": 1024, "preserve_file_meta": false}
]
```

### 5.4 实时流滚动成文件段（realtime -> file）

```json
[
  {
    "type": "stream_packets_to_file_segments",
    "segment_size": 1048576,
    "chunk_size": 65536,
    "path": "/stream-out",
    "file_prefix": "rt",
    "time_layout": "20060102-150405"
  }
]
```

## 6. 缓冲型 stage 的作用域规则

当前仓库里的缓冲型 stage，统一遵守以下作用域：

```text
stage instance + receiver_name + match_key
```

也就是：

```text
buffer_key = receiver_name + "|" + match_key
```

这样做的目的不是改变 selector/task_set/task 的职责，而是避免：

- 不同 receiver 进入同一个 task 时共用同一缓冲区；
- 同一 receiver 内不同子流（由 match key 区分）共用同一缓冲区。
