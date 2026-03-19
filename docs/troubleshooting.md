# Troubleshooting

> 架构基线：`receiver -> selector -> task -> pipelines -> senders`。receiver 只负责收包，selector 返回 task 集，task 负责串行执行 pipelines 并在末端 fan-out 到 senders。

## 1. 启动失败

### 现象

- 进程启动后立即退出。
- stderr 提示参数或配置错误。

### 排查路径

1. 检查参数模式。
2. 检查配置文件可读。
3. 检查 JSON 格式。

```bash
./bin/forward-stub -h
jq . ./configs/system.example.json >/dev/null
jq . ./configs/business.example.json >/dev/null
```

## 2. 配置错误

### 常见表现

- `config validate error`
- task 引用不存在对象
- sftp 指纹格式错误

### 排查

- 核对 selector 的 receiver/task 引用，以及 task 的 sender/pipeline 名称。
- 核对 `execution_model` 是否有效。
- 核对 `host_key_fingerprint` 格式。

## 3. receiver 启动异常

### UDP/TCP

```bash
ss -lntup | rg ':19000|:19001'
```

关注：端口冲突、地址格式、权限。

### Kafka

```bash
nc -vz 127.0.0.1 9092
```

关注：broker 可达、topic 存在、鉴权参数。

### SFTP

关注：账号权限、目录存在、主机指纹一致。

## 4. sender 发送异常

### 排查顺序

1. 目标端可达性。
2. 目标端处理能力。
3. sender 协议参数。
4. task 是否 route 到错误 sender。

建议同时检查：

- sender `concurrency` 是否误配（非 2 的幂会被校验拒绝）。
- Kafka 的 `topic` 与 broker ACL 是否匹配。

## 5. 队列堆积与回压

### 现象

- 队列满日志。
- 入站高出站低。

### 排查

1. 确认下游是否变慢。
2. 检查 task 执行模型。
3. 调整 `pool_size`、`queue_size`、`channel_queue_size`。
4. 拆分热点 task。

## 6. 吞吐下降

### 判断依据

- benchmark 结果出现显著回退。
- 流量统计持续下降。

### 排查命令

```bash
go test ./src/runtime -bench BenchmarkScenarioForwarding -benchmem
```

结合日志判断是 receiver、task 还是 sender 瓶颈。

判断依据：

- 入站稳定但出站下降，多数为 sender 或下游问题。
- 入站下降，多数为 receiver 或上游问题。
- 入站和出站都下降，优先检查资源限制与配置变更。

## 7. CPU 异常

```bash
top -H -p <pid>
pidstat -t -p <pid> 1
```

- 单线程热点：优先检查执行模型和热点 sender。
- 全局高 CPU：检查日志级别和 payload 打印开关。

## 8. 内存异常

```bash
go tool pprof http://127.0.0.1:6060/debug/pprof/heap
```

重点看：

- 队列是否过深。
- 大 payload 是否异常增多。
- 日志是否放大对象生命周期。

如为持续增长，补充检查：

- 是否反复触发大规模配置替换。
- 是否存在异常大包持续输入。

## 9. 协议专项问题

- UDP：内核缓冲不足导致丢包。
- TCP：frame 不匹配导致边界错误。
- Kafka：SASL 或 TLS 参数不匹配。
- SFTP：host key 指纹错误或目录权限不足。

## 10. 观测联动排查模板

1. 看流量统计确认问题范围。
2. 看 error 日志定位组件。
3. 用场景化 benchmark 复现并量化。
4. 必要时抓 pprof 定位热点。

## 11. 待确认

- 待确认：是否会补充统一指标系统以减少纯日志排障成本。
