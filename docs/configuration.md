# Configuration

## 1. 配置体系

项目支持两种模式：

1. **推荐：双文件模式**
   - `-system-config`：系统级配置（`control`、`logging`、`business_defaults`）。
   - `-business-config`：业务拓扑（`version`、`receivers`、`senders`、`pipelines`、`tasks`）。
2. **兼容：legacy 单文件模式**
   - `-config`，system/business 读取同一文件。

`config.ResolveConfigPaths` 会校验路径组合合法性。

## 2. system-config 与 business-config 职责划分

### system-config

- `control`：配置中心 API、超时、文件监听间隔、pprof 端口。
- `logging`：日志级别、文件滚动、流量统计、payload 日志默认截断、payload 池缓存上限。
- `business_defaults`：为 business 配置中常省略字段提供系统级默认值。

### business-config

- `version`：业务配置版本号。
- `receivers` / `senders` / `pipelines` / `tasks`：转发拓扑核心定义。

## 3. 加载与生效流程

```mermaid
flowchart LR
  A[LoadLocalPair] --> B[Merge system+business]
  B --> C[ApplyBusinessDefaults]
  C --> D[ApplyDefaults]
  D --> E[Validate]
  E --> F[runtime.UpdateCache]
```

- 当 `control.api` 非空时，启动和 reload 阶段会调用 `control.ConfigAPIClient.FetchBusinessConfig` 覆盖本地 business 文件。
- `Run` 会在启动时记录 system 配置基线，后续 reload 若 system 配置变更会拒绝应用。

## 4. 启动参数与环境

- 二进制参数：
  - `-system-config`
  - `-business-config`
  - `-config`（legacy）
  - `-version`
- 环境变量：当前代码未定义专属配置环境变量入口（待确认是否有外部启动脚本约定）。

## 5. 默认值与校验

### 默认值入口

- `Config.ApplyBusinessDefaults`：应用 system 提供的业务默认值。
- `Config.ApplyDefaults`：应用代码默认值（如 task 队列、socket buffer、pprof port）。

### 关键校验规则（摘要）

- 必须存在 `tasks/receivers/senders/pipelines`。
- `task.execution_model` 仅允许 `fastpath|pool|channel`。
- `sender.concurrency` 若显式设置，必须是 2 的幂。
- SFTP 收发都要求 `host_key_fingerprint` 且格式合法（`SHA256:<base64_raw>`）。
- 路由 stage `route_offset_bytes_sender` 的目标 sender 必须在 task 的 `senders` 列表中。

## 6. 配置示例

- `configs/system.example.json`
- `configs/business.example.json`
- `configs/example.json`（legacy）

建议从示例复制后最小化修改，避免引入未知字段（JSON 反序列化开启 `DisallowUnknownFields`）。

## 7. 常见配置项说明（按入口）

- `control.config_watch_interval`：business 文件轮询周期。
- `control.pprof_port`：`>0` 启用，`-1` 禁用。
- `logging.traffic_stats_interval`：流量聚合日志输出周期。
- `business_defaults.task.execution_model`：任务缺省模型。
- `receivers.<name>.type`：`udp_gnet|tcp_gnet|kafka|sftp`。
- `senders.<name>.type`：`udp_unicast|udp_multicast|tcp_gnet|kafka|sftp`。
- `pipelines.<name>[]`：stage 数组，按顺序执行。

## 8. 热更新影响范围

- **可热更新**：business 维度（receiver/sender/pipeline/task）。
- **不建议热更新**：system 维度（control/logging/business_defaults），当前实现会拒绝 system 漂移。
- **影响策略**：runtime 优先尝试增量更新，无法增量时全量替换。

## 9. 待确认项

- `control.api` 的服务端协议契约（版本兼容、鉴权、失败重试策略）在仓库内未见完整文档，需补充。
