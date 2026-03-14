# Configuration

## 1. 配置体系设计

系统采用“控制面与数据面配置分离”的思路：

- system 配置定义进程级行为。
- business 配置定义转发拓扑。

这种拆分的原因是：system 变化往往影响全局行为，business 变化更频繁且可在线切换。

## 2. 三种配置文件关系

- `configs/system.example.json`：双文件模式 system 示例。
- `configs/business.example.json`：双文件模式 business 示例。
- `configs/example.json`：legacy 单文件模式示例。

`SystemConfig.Merge(BusinessConfig)` 会把双文件拼装为运行态 `Config`。

## 3. 加载与校验路径

```mermaid
flowchart LR
  Arg[CLI Args] --> Path[Resolve Paths]
  Path --> Load[Load JSON]
  Load --> Merge[Merge Config]
  Merge --> Def[Apply Defaults]
  Def --> Val[Validate]
  Val --> Run[Update Runtime]
```

## 4. system 与 business 边界

### system 段

- `control`：配置来源、超时、监听周期、pprof 端口。
- `logging`：日志级别、文件轮转、流量统计、payload 日志上限。
- `business_defaults`：task/receiver/sender 的默认值模板。

### business 段

- `version`：版本标识。
- `receivers`：输入协议实例。
- `senders`：输出协议实例。
- `pipelines`：stage 组合。
- `tasks`：编排关系。

## 5. 默认值与缺省策略

默认值主要来源于 `src/config/config.go` 和 `ApplyDefaults`：

- `task.pool_size` 默认 4096。
- `task.queue_size` 默认 8192。
- `receiver.multicore` 默认 true。
- `sender.concurrency` 默认 8。
- `control.pprof_port` 默认 6060。

缺省优先级：

1. business 显式值。
2. system `business_defaults`。
3. 代码默认值。

## 6. 关键校验规则

- task 必须引用已存在 receiver/sender/pipeline。
- `execution_model` 仅允许 fastpath/pool/channel。
- `sender.concurrency` 如显式设置需为 2 的幂。
- sftp 必须提供有效 `host_key_fingerprint`。
- route stage 目标 sender 必须在 task sender 列表内。

## 7. 从最小配置到复杂配置

### 最小可运行

- 1 receiver
- 1 sender
- 1 pipeline（可空）
- 1 task

### 复杂编排

- 多 receiver 订阅到多个 task。
- task 共享 sender。
- pipeline 组合 match/replace/route/file 语义。

## 8. 热更新影响范围

- 可热更新：business 配置。
- 不可在线漂移：system 配置。
- reload 前会对比 system 基线，不一致直接拒绝。

## 9. 与 README 示例关系

README 提供完整可复制 JSON 示例；本文重点解释机制、默认值、边界与影响。

## 10. 待确认

- control API 服务端契约细节（版本兼容、错误码、鉴权）需补设计文档。
