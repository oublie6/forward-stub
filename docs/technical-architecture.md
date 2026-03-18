# forward-stub 技术架构（2026 复审版）

本文聚焦：模块边界、数据路径、性能设计、扩展点、安全基线与运维策略。本文按“首次接手项目”的阅读路径组织：先看分层，再看时序，再看性能和扩展。

## 1. 架构分层

```text
入口层: main + bootstrap（参数解析、信号、热重载触发）
控制层: config + validate + reload trigger
运行层: runtime(store/update_cache/compiler)
处理层: task(execution model) + pipeline(stages)
协议层: receiver/* + sender/*
可观测: logx + traffic counter
```

### 1.1 入口层（bootstrap）

- `main.go` 仅保留进程入口，所有启动编排沉淀到 `src/bootstrap`。
- `bootstrap.Run` 负责：
  - 解析 CLI 参数（`-config` / `-system-config` / `-business-config`）。
  - 读取并校验配置。
  - 初始化 logger 和 pprof。
  - 启动 runtime、信号监听、文件变更监听。
  - 响应 `HUP/USR1`（Unix）触发业务配置热更新。
- 这样做的目的：把“启动控制流”从 `main` 拆出，便于独立测试和后续扩展（例如增加启动前健康检查）。

### 1.2 控制层

- 负责配置加载、默认值填充、语义校验。
- 分系统配置与业务配置，业务配置支持热更新。
- `system-config.business_defaults` 给业务配置提供系统级默认值（显式业务配置优先，最后回退代码默认值）。
- 版本号驱动 runtime 更新，避免无意义重建。

### 1.3 运行层

- `UpdateCache` 负责编译 pipeline、构建 sender/task，并把 selector 编译为 receiver 维度的 dispatch 快照后再启动 receiver。
- `Store` 保存活跃实例并提供 dispatch 快照。
- 生命周期遵循“构建成功后切换、失败不污染现网状态”。

### 1.4 数据层

- receiver 收到数据后包装为 `packet.Packet`。
- dispatch 按 receiver 查 selector 快照，解析源 IP / 端口后命中 task 集。
- task 执行 pipeline，再发送到一个或多个 sender。

---

## 2. 关键时序

### 2.1 启动/更新时序

1. bootstrap 解析参数，确定 system/business 配置路径。
2. load+validate 配置（本地文件 / 控制面 API）。
3. runtime 编译 pipeline。
4. runtime 构建 sender。
5. runtime 构建 task（初始化执行模型）。
6. runtime 编译 selector dispatch 快照并原子切换。
7. runtime 构建并启动 receiver。
8. 监听信号/文件变更，触发后续热更新。

### 2.2 单包数据路径

1. receiver 生成 packet。
2. dispatch 查 selector 快照并投递 task。
3. task 按 execution_model 执行。
4. pipeline 阶段处理；任一 stage 返回 false 则丢弃。
5. sender fan-out 发送；单个 sender 失败不阻断其他 sender。

---

## 3. 高吞吐设计点

- gnet 驱动 UDP/TCP IO，降低 goroutine 调度成本。
- ants pool 支撑高并发任务执行。
- payload 内存复用减少 GC 抖动。
- 有界队列 + in-flight 计数实现可控背压。
- dispatch 快照避免业务路径频繁锁竞争。
- 聚合统计常驻，减小业务路径观测开销。

---

## 4. 执行模型选型建议

- `fastpath`：低延迟、低开销；适合轻处理+稳定下游。
- `pool`：吞吐和隔离更强；适合重处理或波峰场景。
- `channel`：顺序语义最佳；适合严格顺序场景。

建议：默认 `pool`，稳定后按链路目标优化到 `fastpath` 或 `channel`。

---

## 5. 安全基线

### 5.1 SFTP 主机身份校验

- receiver/sender 连接 SFTP 均要求 `host_key_fingerprint`。
- 使用 `SHA256` 指纹做校验，拒绝未知主机键。

### 5.2 Kafka TLS

- 支持 TLS 与 `tls_skip_verify`。
- 生产建议 `tls=true` 且 `tls_skip_verify=false`。

### 5.3 凭据治理

- 用户名/密码建议通过 Secret 注入。
- 避免把生产凭据写入仓库与镜像。

---

## 6. 可扩展点

- 新协议：实现 receiver/sender 接口并在 runtime 注册。
- 新 stage：在 compiler 增加编译分支。
- 新执行模型：扩展 task 执行入口。
- 新观测：在 logx 增加聚合器或导出器。

---

## 7. 运维与可观测

- `traffic stats summary` 提供任务维度吞吐统计。
- payload 观测支持按任务开关，避免日志放大。
- runtime 更新日志包含 updating/updated + cost，便于定位配置发布风险。

---

## 8. 性能测试资产

- Benchmark 设计文档：`docs/benchmark.md`
- benchmark 通过 `go test -bench` 直接输出，不再维护本地闭环结果归档。

---

## 9. 演进建议

1. 增加 OpenTelemetry metrics/trace。
2. receiver/sender 引入细粒度重试预算与熔断策略。
3. 完善配置 schema 导出与 CI 校验。
4. 增强千级 task 配置下的更新性能压测。
