# forward-stub 技术架构（2026 复审版）

本文聚焦：模块边界、数据路径、性能设计、扩展点、安全基线与演进建议。

## 1. 架构分层

```text
控制层: config + validate + reload trigger
运行层: runtime(store/update_cache/compiler)
处理层: task(execution model) + pipeline(stages)
协议层: receiver/* + sender/*
可观测: logx + traffic counter
```

### 1.1 控制层

- 负责配置加载、默认值填充、语义校验。
- 分系统配置与业务配置，业务配置支持热更新。
- 通过版本号驱动 runtime 更新。

### 1.2 运行层

- `UpdateCache` 负责编译 pipeline、构建 sender/task/receiver。
- `Store` 保存活跃实例并提供 dispatch 快照。
- 生命周期遵循“构建成功后切换、失败不污染现网状态”的思路。

### 1.3 数据层

- receiver 收到数据后包装为 `packet.Packet`。
- dispatch 按 receiver 订阅映射 fan-out 到 task。
- task 执行 pipeline，再发送到一个或多个 sender。

---

## 2. 核心时序

## 2.1 启动/更新

1. 读取并校验配置。
2. 编译 pipeline。
3. 构建 sender。
4. 构建 task（初始化执行模型）。
5. 构建 receiver 并启动。
6. 原子切换 dispatch 快照。

### 2.2 单包路径

1. receiver 生成 packet。
2. dispatch 查快照并投递 task。
3. task 依据 execution_model 执行。
4. pipeline 阶段处理；任一 stage 返回 false 则丢弃。
5. sender 发送；失败记录告警但不阻断其他 sender。

---

## 3. 高吞吐设计点

- gnet 驱动 UDP/TCP IO，降低调度开销。
- ants pool 支撑高并发任务执行。
- packet payload 复用减少 GC。
- 有界队列与 in-flight 计数实现可控背压。
- 运行时统计聚合常驻，降低业务路径额外成本。

---

## 4. 执行模型的工程选择

- `fastpath`：低延迟、低开销，适合轻处理+稳定下游。
- `pool`：吞吐和隔离更强，适合重处理或波峰场景。
- `channel`：顺序语义最佳，单协程处理。

建议：

- 默认选择 `pool`，上线初期更稳。
- 极低延迟链路再评估 `fastpath`。
- 强顺序链路用 `channel`。

---

## 5. 安全基线（复审后）

### 5.1 SFTP 主机身份校验

- receiver/sender 连接 SFTP 均要求 `host_key_fingerprint`。
- 使用 `SHA256` 指纹做校验，拒绝未知主机键。
- 校验流程覆盖配置校验与构造阶段，避免绕过配置校验时出现“静默不安全”。

### 5.2 Kafka TLS

- 支持 TLS 与 skip verify 开关。
- 生产建议 `tls=true` 且 `tls_skip_verify=false`。

### 5.3 凭据治理建议

- 用户名/密码建议由 Secret 注入。
- 避免将生产密钥放入配置仓库。

---

## 6. 可扩展点

- 新协议接入：实现 receiver/sender 接口并在 runtime build 注册。
- 新 stage：在 compiler 增加 stage 编译分支。
- 新执行模型：扩展 task.Start/Handle 即可。
- 新可观测：在 logx 增加聚合器或导出器。

---

## 7. 可运维性与可观测

- `traffic stats summary` 输出任务维度统计。
- payload 观察可按任务开关，避免全局日志放大。
- runtime 更新有清晰日志事件：updating/updated + cost。

---

## 8. 演进建议

1. 增加 OpenTelemetry 指标与 trace。
2. receiver/sender 增加细粒度重试预算与熔断策略。
3. 完善配置 schema 导出与 CI 校验。
4. 增强大规模配置（上千 task）下的更新性能压测。

