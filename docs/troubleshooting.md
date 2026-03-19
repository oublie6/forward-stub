# 配置与运行故障排查

## 1. 启动即失败

优先检查：

1. 参数模式是否正确。
2. JSON 是否有未知字段。
3. system / business 结构是否混写。

### 1.1 双配置模式命令

```bash
./bin/forward-stub -system-config ./configs/system.example.json -business-config ./configs/business.example.json
```

### 1.2 单文件模式命令

```bash
./bin/forward-stub -config ./configs/example.json
```

## 2. 最常见的配置错误

### 2.1 selector 相关

- receiver 写了不存在的 `selector`。
- selector 引用了不存在的 `task_set`。
- task_set 引用了不存在的 task。

### 2.2 task 相关

- `execution_model` 不是 `fastpath` / `pool` / `channel`。
- route stage 指向了不在当前 `task.senders` 列表里的 sender。

### 2.3 sender 相关

- `concurrency` 显式配置成了非 2 的幂。
- Kafka `idempotent=true` 但 `acks` 不是 `all/-1`。
- Kafka `partitioner=hash_key` 却没配置 `record_key` 或 `record_key_source`。

### 2.4 receiver 相关

- Kafka `heartbeat_interval >= session_timeout`。
- Kafka `auto_commit=false` 却仍然配置了 `auto_commit_interval`。
- SFTP `host_key_fingerprint` 格式不正确。

## 3. 协议专项排查

### 3.1 UDP / TCP

```bash
ss -lntup | rg ':19000|:19001'
```

重点看：

- 端口是否已监听。
- `frame` 是否和对端协议一致。
- socket buffer 是否过小导致拥塞。

### 3.2 Kafka

```bash
nc -vz 127.0.0.1 9092
```

重点看：

- broker 可达性。
- topic 是否存在。
- TLS / SASL 是否匹配。
- `group_id`、`start_offset`、`balancers` 是否符合预期。

### 3.3 SFTP

重点看：

- `host_key_fingerprint` 是否与服务端一致。
- 账号是否有 `remote_dir` 的读/写/rename 权限。
- `temp_suffix` 是否导致下游误读临时文件。

## 4. 吞吐下降或回压

优先检查：

1. `execution_model` 是否合适。
2. `pool_size` / `queue_size` / `channel_queue_size` 是否过小。
3. sender 下游是否变慢。
4. 是否误开 payload 摘要日志导致日志放大。

## 5. 想核对完整字段时看哪里

- 顶层和所有字段：`docs/configuration.md`
- 协议专属字段：`docs/receivers-and-senders.md`
- 执行模型：`docs/execution-model.md`
- 示例文件：`configs/*.example.json`
