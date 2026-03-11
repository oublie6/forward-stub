# UDP 150Mbps 瓶颈复盘与优化建议（2026-03-11）

## 1. 现状结论（先给结论）

结合现有代码与历史 72 场景扫参结果，`UDP ~150Mbps` 更像是**参数组合未命中高吞吐区间**，而不是单一代码缺陷。

最关键的三个杠杆：

1. **receiver.multicore**：UDP 场景下，`multicore=true` 在历史扫参中显著低于 `multicore=false`。
2. **receiver.read_buffer_cap**：`262144` 显著优于 `0/65536/1048576`。
3. **端到端并发平衡**（workers / sink readers / sender concurrency）：任一侧并发偏低都会把整链路钳制在 100~160Mbps 量级。

## 2. 代码路径复盘（数据面）

数据路径：`receiver -> dispatch -> task -> sender`。

- UDP 接收使用 `gnet` 的 `OnTraffic`，每包走 `Peek(-1)` + `Discard`，然后 `packet.CopyFrom` 复制到池化 payload，再进入 task。该路径属于“正确但每包固定有一次复制开销”。
- task 默认执行模型是 `pool`，默认 `pool_size=4096`、`queue_size=8192`，走 ants 池提交。
- UDP 发送端是 `net.UDPConn.Write`，并发通过 sender shard（多 socket）实现，单 shard 一把锁。

这说明当前瓶颈更大概率来自：

- IO 并发参数与机器拓扑不匹配；
- sink 侧读能力不足导致“看起来像 forward 慢”；
- 小包高 PPS 场景下每包固定成本（复制/调度/系统调用）放大。

## 3. 用你现有资产可直接验证的“高收益调参序”

建议按下面顺序做最小代价验证（每次只改一个变量，跑 30~60s 取稳态）：

1. **先固定基线**：`execution_model=pool`，`task_pool_size=2048`，`task_queue_size=4096`。
2. **调 receiver.multicore**：优先试 `false`（UDP 常见更稳）。
3. **调 receiver.read_buffer_cap**：按 `65536 -> 262144 -> 1048576` 试，重点关注 `262144`。
4. **对齐并发度**：
   - generator/workers
   - udp sink readers
   - sender.concurrency
   三者尽量同阶（例如都在 4~8），避免单点成为瓶颈。
5. **提高观测间隔避免日志干扰**：`traffic_stats_interval` 拉长（如 10s/30s），压测窗口关闭 payload 日志。

## 4. 为什么你会卡在 150Mbps 左右

从仓库历史结果看，出现“吞吐腰斩”通常来自以下组合之一：

- `multicore=true`（UDP 场景）+ 默认/不合适的 event loop 绑定；
- sink 侧 reader 太少（比如 1~2 个）或 socket read buffer 太小；
- `workers` 偏小，导致 sender 端喂不满；
- payload 较小（如 256~512）时 PPS 上限先到，换算 Mbps 看起来不高。

## 5. 你可以优先改的配置模板（UDP）

```json
{
  "logging": {
    "level": "warn",
    "traffic_stats_interval": "10s",
    "payload_log_max_bytes": 256
  },
  "receivers": {
    "in_udp": {
      "type": "udp_gnet",
      "listen": "udp://0.0.0.0:19100",
      "multicore": false,
      "num_event_loop": 4,
      "read_buffer_cap": 262144,
      "log_payload_recv": false
    }
  },
  "senders": {
    "out_udp": {
      "type": "udp_unicast",
      "remote": "x.x.x.x:19101",
      "local_ip": "0.0.0.0",
      "local_port": 19102,
      "concurrency": 4
    }
  },
  "tasks": {
    "t_udp": {
      "execution_model": "pool",
      "pool_size": 2048,
      "queue_size": 4096,
      "receivers": ["in_udp"],
      "pipelines": ["p"],
      "senders": ["out_udp"]
    }
  }
}
```

> 若目标是“绝对吞吐”，先把 payload 增至 1024/1400 观察 Mbps 上限，再回到真实 payload 做权衡。

## 6. 下一步建议（按优先级）

### P0（本周）

- 用 `cmd/bench` 复现你当前环境并记录完整参数（不是只看 Mbps）。
- 按“高收益调参序”跑 8~12 组就能定位主要瓶颈。

### P1（可选代码优化）

- 为 UDP sender 增加可配置 `write_buffer_bytes`（当前固定 4MB），减少内核发送队列受限风险。
- 增加 runtime 指标导出（pool waiting / queue available / sender error rate），让瓶颈定位从“猜”变成“看图”。

### P2（中期）

- 评估批量发送路径（如 sendmmsg / 批处理）在小包高 PPS 下的收益。
- 评估是否为 UDP 场景增加更激进的 zero-copy/降复制策略。

## 7. 一句话结论

你现在的 150Mbps 更像是“参数和压测拓扑的上限”，不是架构天花板。按 `multicore/read_buffer_cap/并发对齐` 三步走，通常可以直接把 UDP 吞吐拉回到历史 260Mbps 量级附近（同等机器与链路条件下）。
