# Benchmark 设计说明（场景化、内部链路极限）

> 架构基线：`receiver -> selector -> task -> pipelines -> senders`。receiver 只负责收包，selector 返回 task 集，task 负责串行执行 pipelines 并在末端 fan-out 到 senders。

## 1. 定位

本项目 benchmark 仅回答：

> 在不执行真实外部 I/O 的前提下，服务内部完整转发链路的理论处理极限是多少。

这不是端到端压测，不提供真实网卡吞吐、真实 0 丢包能力或外部系统容量结论。

## 2. 设计原则

- 按具体协议场景组织 benchmark，而非协议无关统一入口。
- 场景起点尽量贴近真实 receiver 收到输入后的内部入口。
- 保留 `packet.CopyFrom` 等必要数据装配成本。
- 覆盖完整链路：`dispatch -> task.Handle -> processAndSend -> sender.Send`。
- 在 sender 内部保留分片、连接/目标选择、消息封装等发送前成本。
- 仅 mock 最终外部提交动作，不执行真实 socket/Kafka/SFTP 外部写入。

## 3. 场景边界

### 3.1 UDP/TCP 接收场景

- 起点：字节切片进入 receiver 风格装配入口（包含 `packet.CopyFrom`）。
- 终点：sender 的最终网络写动作之前。

### 3.2 Kafka 接收场景

- 起点：模拟“已消费一条 record value”，再执行 `packet.CopyFrom(value)` 与 packet 构造。
- 终点：Kafka producer 真正提交动作之前。

### 3.3 SFTP 接收场景

- 起点：模拟“已读取一个 chunk”，再执行 `packet.CopyFrom(chunk)` 与 file_chunk 元数据装配。
- 终点：SFTP 远端文件写入动作之前。

## 4. 当前 benchmark 场景

- `UDP -> UDP`
- `UDP -> TCP`
- `TCP -> UDP`
- `Kafka -> UDP`
- `UDP -> Kafka`
- `SFTP -> TCP`
- `TCP -> SFTP`

并按以下维度展开子 benchmark：

- payload size：`64B/128B/512B/1200B/4096B`
- sender concurrency：`1/2/4/8`
- 目标规模：`单目标/多目标`
- execution model：当前默认 `fastpath`

## 5. 结果解释

重点观察标准 Go benchmark 指标：

- `ns/op`
- `B/op`
- `allocs/op`

这些结果代表内部处理极限，不可直接解释为真实端到端吞吐或业务 SLA。

## 6. 运行方式

```bash
go test ./src/runtime -bench BenchmarkScenarioForwarding -benchmem
```

可结合 profile：

```bash
go test ./src/runtime -bench BenchmarkScenarioForwarding -benchmem -cpuprofile cpu.out -memprofile mem.out
```

## 7. 与真实压测区别

- benchmark：定位内部热点函数与理论上限（适合 `pprof`、火焰图、alloc 分析）。
- 端到端压测：评估真实网络、内核、外部系统、部署架构下的实际容量。

两者互补，不能互相替代。
