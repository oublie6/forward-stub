# Forward Stub 全量性能优化与代码审查报告（多轮迭代，2026-03-08）

## 1. 目标与范围

本次对当前服务进行多轮“审查→改造→复审→测试→压测”，覆盖：

- Go 代码文件（`cmd/`、`src/`、根目录 Go 文件）
- 脚本文件（`scripts/*.sh`）
- 不包含第三方 `vendor/` 代码

并完成以下硬性目标：

1. 找出性能优化点与 bug 风险并改造。
2. 多轮执行直到未发现新的低风险高收益改造点。
3. 完成功能测试。
4. 完成两类极限吞吐测试：
   - 严格 0 丢包 + 严格保序
   - 严格 0 丢包 + 不考虑保序（可用任意 pool size）
5. 压测期间关闭 payload 日志。

---

## 2. 多轮执行过程

### 第 1 轮：全量审查（静态+运行路径）

审查关注点：

- 热路径额外分配
- 生命周期/并发安全
- 配置校验边界
- 压测工具可信度（是否能验证严格保序）
- 脚本语法可执行性

发现问题：

1. `cmd/bench` 不具备严格保序验证能力（只统计收发计数）。
2. `cmd/bench` TCP sink 每包都新建 buffer，影响压测开销与稳定性。
3. `cmd/bench` 启动阶段重复 `logx.Init`，存在冗余初始化。
4. `src/sender/udp_unicast_gnet.go` 对 `local_ip` 缺少严格校验，非法 IP 可能造成误绑定行为。

### 第 1 轮改造

- 新增 bench 严格保序验证能力（基于 payload 前 8 字节序列号）：
  - 新增 `--validate-order` 开关。
  - 严格校验模式下强制参数约束（如 `workers=1`、UDP 单 reader、TCP sender 并发=1）。
  - 输出新增 `order_errors` 与 `strict_order_ok` 指标。
- 优化 TCP sink：复用 `readU16Framed` 缓冲区，避免每包分配。
- 移除 bench 冗余 `logx.Init` 初始化。
- bench 生成配置显式关闭 payload 日志（`PayloadLogRecv/Send=false`）。
- 修复 UDP 单播 sender 本地 IP 校验：非法 `local_ip` 直接报错。

### 第 2 轮：回归审查

- 复查热路径与并发边界。
- 增补单元测试：
  - 序列校验逻辑测试。
  - 非法 local IP 校验测试。
- 复跑全量测试、vet、性能基准与压测。

结论：未发现新的“可直接落地且收益明显”的低风险改造点。

---

## 3. 代码改造清单

1. `cmd/bench/main.go`
   - 新增严格保序验证参数与校验。
   - 新增顺序错误统计字段并写入结果。
   - 生成端注入 sequence；sink 端校验 sequence。
   - TCP sink 读缓冲复用。
   - bench runtime 配置显式关闭 payload log。
   - 清理冗余日志初始化。

2. `src/sender/udp_unicast_gnet.go`
   - `local_ip` 校验：`net.ParseIP == nil` 时返回错误，避免隐式退化。

3. 新增测试
   - `cmd/bench/sequence_test.go`
   - `src/sender/udp_unicast_gnet_ip_test.go`

---

## 4. 功能与质量验证结果

### 4.1 单元/集成测试
- `GOFLAGS='-mod=vendor' go test ./...`：通过

### 4.2 静态检查
- `GOFLAGS='-mod=vendor' go vet ./...`：通过

### 4.3 脚本语法检查
- `for f in scripts/*.sh; do bash -n "$f"; done`：通过

### 4.4 基准测试
- `GOFLAGS='-mod=vendor' make perf`：通过（含 runtime benchmark 与 bench 回归）

---

## 5. 极限吞吐测试结果

> 说明：所有压测在 bench 生成的 runtime 配置中均显式关闭 payload log。

### A. 严格 0 丢包 + 严格保序极限吞吐

#### A1) UDP（channel 模型，严格保序）
参数：
- `workers=1`
- `task-execution-model=channel`
- `udp-sink-readers=1`
- `validate-order=true`
- `payload=512B`

关键结果：
- 20k pps 仍满足：`loss_rate=0`、`order_errors=0`、`strict_order_ok=true`
- 当前扫描上限内严格极限：约 **19,998 pps / 81.91 Mbps**

#### A2) TCP（channel 模型，严格保序）
参数：
- `workers=1`
- `task-execution-model=channel`
- `tcp-sender-concurrency=1`
- `validate-order=true`
- `payload=512B`

关键结果：
- 10k pps：`loss_rate=0`、`order_errors=0`
- 11k pps 出现大量 `order_errors`
- 当前严格极限：约 **9,999 pps / 40.96 Mbps**（10k 档）

### B. 严格 0 丢包 + 不考虑保序（任意 pool size）极限吞吐

#### B1) UDP（pool，扫描 pool_size=1024/4096/8192）
参数：
- `workers=2`
- `task-execution-model=pool`
- `task-pool-size ∈ {1024,4096,8192}`
- `payload=512B`

结果摘要：
- 约 16k pps（总）可稳定 0 丢包。
- 更高档位（24k+）出现不同程度丢包。
- 本轮 0 丢包极限约 **16k pps / 65.5 Mbps**。

#### B2) TCP（pool，扫描 pool_size=1024/4096/8192）
参数：
- `workers=2`
- `task-execution-model=pool`
- `task-pool-size ∈ {1024,4096,8192}`
- `payload=512B`

结果摘要：
- 在更高扫频中持续 0 丢包（含 50k pps/worker、70k pps/worker、100k pps/worker 档）。
- 本轮扫描上限内达到：约 **200,832 pps / 822.61 Mbps** 且 0 丢包。

---

## 6. 最终结论

1. 已完成多轮审查与改造，当前未发现新的低风险高收益优化点与明显 bug 风险。
2. bench 现在支持“严格保序”验证，不再只看收发计数。
3. 在本机环境中：
   - 严格保序下：UDP 严格极限显著高于 TCP（本次扫描结果）。
   - 不考虑保序且允许任意 pool size：TCP 吞吐可大幅提升且保持 0 丢包。
4. 若继续优化，建议下一阶段聚焦：
   - TCP 严格保序高负载下的乱序根因定位（receiver/task/sender 各段事件时序）
   - UDP 高 PPS 丢包点的 socket / 内核缓冲 / event-loop 参数联调
