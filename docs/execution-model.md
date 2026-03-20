# Task 执行模型配置说明

## 1. 可用配置字段

与 task 执行模型直接相关的字段有：

- `execution_model`
- `fast_path`
- `pool_size`
- `channel_queue_size`

## 2. 字段解释

| 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `execution_model` | string | 空，最终回退 `pool` | 支持 `fastpath`、`pool`、`channel`。 |
| `fast_path` | bool | `false` | 兼容旧配置；仅在 `execution_model` 为空时生效。 |
| `pool_size` | int | `4096` | pool 模式 worker 池大小。 |
| `channel_queue_size` | int | `8192` | channel 模式队列长度；仅 `execution_model=channel` 生效。 |

## 3. 生效规则

### 3.1 首选 `execution_model`

如果 `execution_model` 已经明确配置，则直接采用：

- `fastpath`
- `pool`
- `channel`

### 3.2 `fast_path` 只是兼容旧配置

仅当 `execution_model` 为空时：

- `fast_path=true` -> 实际执行模型为 `fastpath`
- `fast_path=false` -> 实际执行模型为 `pool`

## 4. 三种执行模型对比

| 模型 | 配置方式 | 执行方式 | 适用场景 |
|---|---|---|---|
| `fastpath` | `execution_model=fastpath` | 当前 goroutine 直接处理 | 极低延迟、轻量处理 |
| `pool` | `execution_model=pool` | ants worker pool 异步处理 | 通用生产场景 |
| `channel` | `execution_model=channel` | 单 worker + 有界 channel 顺序处理 | 顺序敏感链路 |

## 5. 运行时容量语义

### 5.1 `pool_size`

- 只对 `pool` 模式真正生效。
- 控制 ants worker 并发上限。
- pool 模式不再暴露 `queue_size` 配置，也不再把配置解释为“队列长度”。

### 5.2 `channel_queue_size`

- 只对 `channel` 模式生效。
- 未配置或 `<=0` 时自动回退到默认值 `8192`。
- 聚合统计会输出 `channel.queue_size`、`channel.queue_used`、`channel.queue_available`。

## 6. 推荐写法

### 6.1 pool

```json
{
  "execution_model": "pool",
  "pool_size": 4096
}
```

### 6.2 channel

```json
{
  "execution_model": "channel",
  "channel_queue_size": 1024
}
```

### 6.3 兼容旧写法（仍支持，但不推荐继续新增）

```json
{
  "fast_path": true
}
```

## 7. 什么时候选哪种模型

- 默认优先选 `pool`。
- 需要极低延迟且处理极轻时选 `fastpath`。
- 需要在单个 task 内保持更强顺序语义时选 `channel`。
