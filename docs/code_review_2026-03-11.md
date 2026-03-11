# 代码审查报告（2026-03-11）

## 范围
- 项目整体结构、配置加载、热更新路径、运行时更新关键路径。
- 本次以静态审阅 + 自动化检查为主。

## 自动化检查结果
- `go test ./...`：通过。
- `go vet ./...`：通过。
- `golangci-lint run ./...`：受环境工具链版本限制未完成（当前 golangci-lint 构建于 go1.24，项目目标 Go 版本为 1.25）。

## 主要发现

### 1) 配置解析默认允许未知字段，存在“配置拼写错误静默生效”风险（中）
**现象**
- 本地配置加载与 API 配置加载均使用 `encoding/json` 默认行为反序列化，未启用 `DisallowUnknownFields`。
- 这会导致配置文件里出现拼写错误字段时不会报错，系统可能继续以默认值运行，降低问题可观测性。

**依据**
- `LoadLocal/LoadSystemLocal/LoadBusinessLocal` 使用 `json.Unmarshal`。 
- `FetchConfig` 使用 `json.NewDecoder(resp.Body).Decode(&cfg)`，同样未禁用未知字段。

**建议**
- 引入严格模式（可通过配置开关控制兼容性）：
  - 本地文件：使用 `json.Decoder` 并启用 `DisallowUnknownFields`；
  - API 拉取：同样启用严格解码。
- 若担心存量兼容，可先在日志中告警未知字段并在后续版本切换为强校验。

### 2) API 模式下“仅允许业务热更新”的边界可能被绕过（高）
**现象**
- 热更新前会调用 `CheckSystemConfigStable(systemCfg)` 验证系统配置不变。
- 但当启用 `Control.API` 时，`loadConfigPair` 先读取本地 `systemCfg`，随后用 API 返回的完整 `cfg` 覆盖运行配置。
- 这意味着：系统配置稳定性校验是对“本地 system 配置”做的，而非对“实际将生效的 API 配置里的系统字段”做的。

**依据**
- `reloadAndApplyBusinessConfig` 仅校验 `systemCfg`，然后直接 `rt.UpdateCache(ctx, next)`。
- `loadConfigPair` 在 `cfg.Control.API != ""` 时将 `cfg` 替换为 API 返回值。

**影响**
- 在 API 返回内容包含系统级字段变化时，可能绕过“系统配置变更需重启”的约束，导致运行期行为与预期治理策略不一致。

**建议**
- 在 API 模式下，显式拆分并校验 API 返回的系统配置部分；
- 或将 API 端返回限制为“仅业务配置”，由类型和协议层面杜绝系统字段漂移。

### 3) 运行时更新路径包含固定 10ms sleep，可能带来可预测性与吞吐尾延迟影响（低）
**现象**
- `UpdateCache` 末尾包含 `time.Sleep(10 * time.Millisecond)`。

**影响**
- 在频繁更新（例如压测/自动化回放）场景下，会引入稳定且不可忽略的额外延迟；
- 该方式依赖时间窗口而非确定性状态信号，鲁棒性受机器负载和调度影响。

**建议**
- 考虑用“组件 ready 信号”或“可观测状态轮询 + 超时”替代固定 sleep；
- 若暂不改造，建议将该延迟参数化并记录触发原因，便于后续验证与收敛。

## 总结
- 当前主干质量较好，基础测试与 `go vet` 结果健康。
- 建议优先处理 **发现 2（高风险）**，其余两项可作为稳定性与可维护性改进排期。
