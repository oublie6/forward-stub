# 极限无丢包转发吞吐量扫参复测（2026-03-10）

说明：以 `cmd/bench` 对性能相关可配置项做单变量扫参（其余参数固定），每个场景使用统一 `pps_sweep`，结果取 `loss_rate=0` 下最大 `pps`。

## 基线参数

- `duration` = `1s`
- `log-level` = `info`
- `multicore` = `true`
- `payload-size` = `512`
- `pps-sweep` = `2000,8000,16000,24000`
- `receiver-event-loops` = `8`
- `receiver-read-buffer-cap` = `0`
- `task-channel-queue-size` = `4096`
- `task-execution-model` = `pool`
- `task-fast-path` = `false`
- `task-pool-size` = `2048`
- `task-queue-size` = `4096`
- `tcp-sender-concurrency` = `4`
- `traffic-stats-interval` = `30s`
- `udp-sink-read-buf` = `16777216`
- `udp-sink-readers` = `4`
- `validate-order` = `false`
- `warmup` = `300ms`
- `workers` = `4`

- 场景总数：`73`；成功：`73`；失败：`0`

## UDP 结果

| 场景 | 无丢包最大PPS | 无丢包最大Mbps |
|---|---:|---:|
| execution_model=channel | 31959 | 130.91 |
| execution_model=fastpath | 96013 | 393.27 |
| execution_model=pool | 64221 | 263.05 |
| multicore=false | 64137 | 262.70 |
| multicore=true | 64007 | 262.17 |
| payload_size=1024 | 8002 | 65.55 |
| payload_size=256 | 8010 | 16.41 |
| payload_size=4096 | 0 | 0.00 |
| payload_size=512 | 64303 | 263.39 |
| receiver_event_loops=1 | 32030 | 131.19 |
| receiver_event_loops=2 | 7961 | 32.61 |
| receiver_event_loops=4 | 8008 | 32.80 |
| receiver_event_loops=8 | 8012 | 32.82 |
| receiver_read_buffer_cap=0 | 32052 | 131.28 |
| receiver_read_buffer_cap=1048576 | 64061 | 262.39 |
| receiver_read_buffer_cap=262144 | 31632 | 129.57 |
| receiver_read_buffer_cap=65536 | 32070 | 131.36 |
| task_channel_queue_size=1024 | 8001 | 32.77 |
| task_channel_queue_size=16384 | 31968 | 130.94 |
| task_channel_queue_size=4096 | 63987 | 262.09 |
| task_pool_size=2048 | 32036 | 131.22 |
| task_pool_size=512 | 64057 | 262.38 |
| task_pool_size=8192 | 32038 | 131.23 |
| task_queue_size=1024 | 8009 | 32.81 |
| task_queue_size=16384 | 31997 | 131.06 |
| task_queue_size=4096 | 8007 | 32.80 |
| udp_sink_read_buf=1048576 | 8003 | 32.78 |
| udp_sink_read_buf=16777216 | 64034 | 262.28 |
| udp_sink_read_buf=4194304 | 64045 | 262.33 |
| udp_sink_read_buf=67108864 | 32080 | 131.40 |
| udp_sink_readers=1 | 32006 | 131.09 |
| udp_sink_readers=2 | 8009 | 32.81 |
| udp_sink_readers=4 | 8019 | 32.84 |
| udp_sink_readers=8 | 64080 | 262.47 |
| workers=1 | 24055 | 98.53 |
| workers=2 | 16020 | 65.62 |
| workers=4 | 8001 | 32.77 |
| workers=8 | 64073 | 262.44 |

## TCP 结果

| 场景 | 无丢包最大PPS | 无丢包最大Mbps |
|---|---:|---:|
| execution_model=channel | 96082 | 393.55 |
| execution_model=fastpath | 96144 | 393.80 |
| execution_model=pool | 96142 | 393.80 |
| multicore=false | 96239 | 394.19 |
| multicore=true | 96169 | 393.91 |
| payload_size=1024 | 96127 | 787.47 |
| payload_size=256 | 96282 | 197.19 |
| payload_size=4096 | 96669 | 3167.64 |
| payload_size=512 | 96099 | 393.62 |
| receiver_event_loops=1 | 96307 | 394.47 |
| receiver_event_loops=2 | 96341 | 394.61 |
| receiver_event_loops=4 | 96359 | 394.68 |
| receiver_event_loops=8 | 96081 | 393.55 |
| receiver_read_buffer_cap=0 | 96085 | 393.57 |
| receiver_read_buffer_cap=1048576 | 96370 | 394.73 |
| receiver_read_buffer_cap=262144 | 96091 | 393.59 |
| receiver_read_buffer_cap=65536 | 95460 | 391.00 |
| task_channel_queue_size=1024 | 96086 | 393.57 |
| task_channel_queue_size=16384 | 96213 | 394.09 |
| task_channel_queue_size=4096 | 96125 | 393.73 |
| task_pool_size=2048 | 96152 | 393.84 |
| task_pool_size=512 | 96163 | 393.88 |
| task_pool_size=8192 | 96101 | 393.63 |
| task_queue_size=1024 | 96748 | 396.28 |
| task_queue_size=16384 | 96331 | 394.57 |
| task_queue_size=4096 | 96076 | 393.53 |
| tcp_sender_concurrency=1 | 96222 | 394.13 |
| tcp_sender_concurrency=16 | 95659 | 391.82 |
| tcp_sender_concurrency=2 | 96525 | 395.37 |
| tcp_sender_concurrency=4 | 96125 | 393.73 |
| tcp_sender_concurrency=8 | 96328 | 394.56 |
| workers=1 | 23966 | 98.17 |
| workers=2 | 48079 | 196.93 |
| workers=4 | 96187 | 393.98 |
| workers=8 | 203232 | 832.44 |

## Top 10（无丢包PPS）

| 排名 | 协议 | 场景 | 无丢包最大PPS | Mbps |
|---:|---|---|---:|---:|
| 1 | tcp | workers=8 | 203232 | 832.44 |
| 2 | tcp | task_queue_size=1024 | 96748 | 396.28 |
| 3 | tcp | payload_size=4096 | 96669 | 3167.64 |
| 4 | tcp | tcp_sender_concurrency=2 | 96525 | 395.37 |
| 5 | tcp | receiver_read_buffer_cap=1048576 | 96370 | 394.73 |
| 6 | tcp | receiver_event_loops=4 | 96359 | 394.68 |
| 7 | tcp | receiver_event_loops=2 | 96341 | 394.61 |
| 8 | tcp | task_queue_size=16384 | 96331 | 394.57 |
| 9 | tcp | tcp_sender_concurrency=8 | 96328 | 394.56 |
| 10 | tcp | receiver_event_loops=1 | 96307 | 394.47 |

原始数据：`docs/perf_extreme_sweep_raw_2026-03-10.json`。
