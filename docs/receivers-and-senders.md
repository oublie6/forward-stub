# Receivers 与 Senders

## 1. receiver 新职责

每类 receiver 都必须做两件事：

1. 生成 `packet.Packet`
2. 生成唯一 `match key`

### UDP / TCP

- 使用源地址生成 key。
- 示例：`udp|src_addr=1.1.1.1:9000`
- 示例：`tcp|src_addr=1.1.1.1:9000`

### Kafka

- receiver 显式使用 `topic + partition` 生成 key。
- 示例：`kafka|topic=order|partition=3`
- 不再依赖 `Remote` 像地址一样被猜测。

### SFTP

- receiver 显式使用 `remote_dir + file_name` 生成 key。
- 示例：`sftp|remote_dir=/input|file_name=a.txt`
- 不再依赖 file path 被误当作地址推断。

## 2. sender 职责

sender 没有架构性变化，仍然只负责最终输出：

- UDP / TCP 网络发送
- Kafka produce
- SFTP 文件输出

## 3. packet.Meta 的变化

`packet.Meta` 新增 `MatchKey` 字段，用于把 receiver 构造出的唯一匹配串带入 runtime / selector。
