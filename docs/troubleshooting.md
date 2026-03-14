# Troubleshooting

## 1. 启动失败

### 现象

进程启动即退出，stderr 提示参数或配置错误。

### 排查

```bash
./bin/forward-stub -version
./bin/forward-stub -system-config ./configs/system.example.json -business-config ./configs/business.example.json
```

- 检查配置文件路径是否存在。
- 检查是否误用单文件/双文件参数组合。

## 2. 配置错误

### 现象

日志出现 `config validate error`。

### 排查

- 检查 task 引用的 receiver/pipeline/sender 是否存在。
- 检查 `execution_model` 是否为 `fastpath|pool|channel`。
- 检查 SFTP `host_key_fingerprint` 格式。

```bash
jq . configs/business.example.json >/dev/null
```

## 3. receiver 启动失败

### UDP/TCP

- 端口被占用：

```bash
ss -lntup | rg ':9000|:19000|:19001'
```

- 地址格式不合法或权限不足。

### Kafka

- broker 不可达、topic/group 配置错误、鉴权失败。

```bash
nc -vz <broker-host> 9092
```

### SFTP

- 用户名密码错误、目录不存在、主机指纹不匹配。

## 4. sender 发送异常

### 现象

日志中出现 sender send failed / close error。

### 排查

- UDP/TCP：目标地址可达性、下游监听状态。
- Kafka：acks/compression/auth 与 broker 配置匹配。
- SFTP：写目录权限、临时后缀重命名权限。

## 5. 队列堆积或丢包

### 现象

日志出现 pool queue full / channel enqueue canceled。

### 排查思路

1. 查看下游是否变慢。
2. 查看 task 模型是否匹配场景。
3. 调整 `pool_size/queue_size/channel_queue_size`。
4. 必要时拆分 task 降低单任务压力。

## 6. 下游慢导致回压

- fastpath：会直接拖慢 receiver 回调。
- pool/channel：表现为队列积压直至告警或丢包。

建议：

- 增加 sender 并发（满足 2 的幂约束）。
- 对慢下游单独 task 隔离。
- 考虑增加 Kafka 等缓冲层。

## 7. 协议专项问题

### UDP

- 内核 buffer 太小导致丢包，检查 `socket_recv_buffer/socket_send_buffer`。

### TCP

- `frame` 配置不一致导致拆包错误。

### Kafka

- SASL/TLS 参数不匹配，注意 `sasl_mechanism` 支持范围。

### SFTP

- `host_key_fingerprint` 校验失败会拒绝连接。

## 8. 吞吐下降

排查顺序建议：

1. 日志级别是否变为 debug 或开启 payload 日志。
2. 配置是否切到 `channel` 模型或过小队列。
3. 下游是否出现抖动。
4. CPU 是否被 cgroup throttle（容器场景）。
5. 使用 bench 复现并对比基线。

## 9. CPU 异常

```bash
top -H -p <pid>
pidstat -t -p <pid> 1
```

- 若单线程热点：检查模型与 sender 热点。
- 若整体高 CPU：检查日志、stage 复杂度、高 fan-out clone。

## 10. 内存异常

```bash
go tool pprof http://127.0.0.1:6060/debug/pprof/heap
```

- 检查队列参数是否过大。
- 检查是否持续开启 payload 明文日志。
- 检查是否存在异常大 payload。

## 11. 待确认项

- 仓库当前未提供统一 Prometheus 指标导出端点（待确认是否由外部 sidecar 采集日志实现）。
