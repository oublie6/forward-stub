# UDP 转发能力复盘与优化建议（2026-03-11）

## 1. 先对齐认知：你现在其实已经不差

你给出的结果是 **25000 pps @ 1250B ≈ 200 Mbps（有效载荷）**，这个数值本身是成立的：

- 25000 × 1250 × 8 = 250,000,000 bit/s ≈ 250 Mbps（纯 payload 理论值）
- 若你的统计口径扣除了部分包头/时间窗口抖动，看到 ~200 Mbps 也很常见

> 结论：当前系统并非“只能 150Mbps”，而是已经在 200Mbps 附近。

## 2. 为什么“别人说 Go 可以 5w~20w pps”不一定可直接对比

“5w~20w pps”通常成立于特定前提，与你当前链路未必一致：

1. **包长**不同：64B/128B 与 1250B 不是一个量级。
2. **路径复杂度**不同：仅回环收发 vs `receiver -> task -> pipeline -> sender` 全链路。
3. **统计口径**不同：应用层 payload pps / 网卡 pps / 零丢包 pps。
4. **部署形态**不同：单机 loopback、同 NUMA、无容器网络，往往会更高。

换句话说，别人的“20w pps”不一定比你“25k pps @ 1250B”更强，它可能只是测试条件更“轻”。

## 3. 你提到的关键现象：容器给了 4 核，但进程只用到 25% CPU

这个现象通常意味着：**当前只有 1 个核接近满载，其他核没有被有效利用**。如果按 4 核配额计算，`top` 看到 25% 很常见。

常见原因（按概率）：

1. **单热点线程/单队列瓶颈**：例如某个 event-loop、某个 sender shard、或某个 sink reader 成为瓶颈。
2. **IRQ/RSS 没打散**：网卡中断集中在单核，应用即使有并发也喂不满。
3. **容器 CFS 限流**：`cpu.max` / quota 配置导致周期性 throttle，看起来 CPU 低但吞吐上不去。
4. **task/receiver/sender 并发不匹配**：比如 workers 多，但 receiver 或 sink 侧仍是单点。

### 快速判断（5 分钟）

1. 看容器是否被节流（throttle）：`cpu.stat` 中 `nr_throttled`、`throttled_usec` 是否持续增长。
2. 看是否单核热点：`top -H -p <pid>` 或 `pidstat -t -p <pid> 1`。
3. 看中断分布：`/proc/interrupts` 是否集中在一个 CPU。
4. 看应用队列：task pool waiting、queue available 是否长时间逼近上限。

> 若结论是“单核热点”，优先做并发打散；若是“被 throttle”，优先改 cgroup 配额；若是“IRQ 单核”，优先改 RSS/亲和。

## 4. 从代码看，你当前的主要优化空间在哪

现有 UDP 路径里每包固定成本主要在三块：

- receiver：`gnet` 收包后 `Peek+Discard`，再 `packet.CopyFrom`（每包一次复制）。
- task：默认 `pool` 模型，存在池提交与队列管理成本。
- sender：`UDPConn.Write`，按 shard/socket 并发发送，受内核队列和系统调用频率影响。

因此你的上限一般由“**每包固定开销 × 每秒包数**”决定，而不是单点 bug。

## 5. 你最值得优先做的优化（按收益排序）

### P0：先把测试方法标准化（避免误判）

1. 固定统计口径：统一看 `recv pps / loss / payload Mbps`。
2. 每组至少跑 30~60 秒，记录稳定区间，不看瞬时峰值。
3. 每次只改 1 个变量。

### P1：配置级优化（无需改代码）

1. **receiver.multicore**：UDP 场景优先比较 `true/false`。
2. **receiver.num_event_loop**：从 `2/4/8` 扫参，观察是否打散单核热点。
3. **receiver.read_buffer_cap**：重点试 `262144`。
4. **并发对齐**：`generator workers`、`udp sink readers`、`sender concurrency` 尽量同阶（4~8 起步）。
5. **减少日志干扰**：压测窗口设 `level=warn`，关闭 payload 日志，拉长 traffic stats 间隔。

### P2：系统级优化（常被忽略，但收益大）

1. 网卡队列/RSS 与 CPU 亲和（避免软中断集中到单核）。
2. `rmem_max/wmem_max`、`netdev_max_backlog`、`udp_mem` 等内核参数。
3. 容器环境下检查 CNI 路径、veth 开销、宿主机限额与 cpuset。
4. 检查 cgroup CPU 配额：确认不是 “4 核 request + 1 核 limit” 这种配置偏差。
5. 锁频率固定时，优先扩大包长（如 1250 -> 1400）提高 Mbps。

### P3：代码级优化（中期）

1. sender 增加可配置 `write_buffer_bytes`（当前固定 4MB）。
2. 增加 runtime 暴露：pool waiting / queue available / sender errors。
3. 评估批量发送（如 sendmmsg）以降低 syscall/packet。
4. 评估 receiver 侧进一步降复制策略（在可维护性允许前提下）。

## 6. 建议你下一个“可执行目标”

把目标拆成两条线，不要混在一起：

- **线A（业务真实包长 1250B）**：目标先做到 30k~50k pps 稳定零丢包。
- **线B（极限小包能力）**：独立测 128B/256B，看是否达到 80k+ pps。

这样你能同时回答：

- 业务链路是否达标；
- 引擎极限是否有继续优化价值；
- 4 核是否真的被有效利用（而不是单核跑满 + 显示 25%）。

## 7. 一句话结论

你现在 25k pps @ 1250B（约 200Mbps）并不“差”。当前最关键的不是盲目追别人 pps，而是先确认“4 核只用 25%”到底是**单核热点、IRQ 偏斜还是 cgroup 限流**，再做对应优化。
