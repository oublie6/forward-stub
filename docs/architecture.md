# 架构说明

## 1. 当前核心拓扑

系统当前采用：

```text
receiver -> selector -> task(pipeline + sender)
```

旧架构里 `task` 直接绑定 `receiver`，接入和路由耦合在一起。重构后，路由职责被收敛到极简 selector 层。

## 2. 四层职责边界

### receiver

负责：

- 接收不同协议的数据。
- 构造 `packet.Packet`。
- 依据本协议固定规则生成唯一 `match key`。

不负责：

- 任务选择。
- 规则遍历。
- 解释其他协议字段。

### selector

负责：

- `match key -> task set` 精确匹配。

不负责：

- 解析协议。
- 猜测字段语义。
- 从 `Meta.Remote` 推断 topic/path/address。

### task set

负责：

- 做配置复用。
- 让多个 match key 共享同一组 task 名称。

不负责：

- 参与热路径额外跳转。

### runtime / dispatch

负责：

- 串联 receiver、selector、task。
- fanout 到最终命中的 task slice。

不负责：

- 协议解析。
- 模板式复杂框架。
- 多级 fallback 规则引擎。

## 3. 热路径设计

热路径只做三件事：

1. receiver 生成唯一 `match key`
2. selector 对 `match key` 做一次 map lookup
3. runtime fanout 到 `[]*TaskState`

复杂性全部前置到：

- 配置校验期
- 编译期 / `UpdateCache`
- receiver 自己的 key 构造逻辑

## 4. match key 示例

- UDP：`udp|src_addr=1.1.1.1:9000`
- TCP：`tcp|src_addr=1.1.1.1:9000`
- Kafka：`kafka|topic=order|partition=3`
- SFTP：`sftp|remote_dir=/input|file_name=a.txt`

所有 key 都遵循同一原则：**协议前缀明确、字段顺序固定、分隔符固定、由 receiver 显式构造**。
