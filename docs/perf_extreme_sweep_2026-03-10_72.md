# 极限无丢包转发吞吐量扫参复测（72场景）

说明：基于 `cmd/bench` 最新方案执行 72 个单变量场景（UDP 36 + TCP 36），每个场景统一 `pps_sweep`，结果取 `loss_rate=0` 下最大 `pps`。

## 基线参数

- `duration` = `300ms`
- `warmup` = `100ms`
- `payload_size` = `512`
- `workers` = `4`
- `pps_sweep` = `2000,8000,16000`
- `multicore` = `true`
- `udp_sink_readers` = `4`
- `udp_sink_read_buf` = `16777216`
- `task_fast_path` = `false`
- `task_pool_size` = `2048`
- `task_queue_size` = `4096`
- `task_channel_queue_size` = `4096`
- `task_execution_model` = `pool`
- `receiver_event_loops` = `8`
- `receiver_read_buffer_cap` = `0`
- `tcp_sender_concurrency` = `4`
- `log_level` = `info`
- `traffic_stats_interval` = `30s`
- `validate_order` = `false`

- 场景总数：`72`；成功：`72`；失败：`0`

## UDP 结果

| 场景 | 无丢包最大PPS | 无丢包最大Mbps |
|---|---:|---:|
| execution_model=pool | 64108 | 262.59 |
| execution_model=channel | 64364 | 263.64 |
| execution_model=fastpath | 63932 | 261.87 |
| multicore=false | 64547 | 264.39 |
| multicore=true | 32251 | 132.10 |
| payload_size=256 | 64388 | 131.87 |
| payload_size=512 | 64782 | 265.35 |
| payload_size=1024 | 32412 | 265.52 |
| payload_size=4096 | 8014 | 262.60 |
| receiver_event_loops=1 | 64257 | 263.20 |
| receiver_event_loops=2 | 64272 | 263.26 |
| receiver_event_loops=4 | 63943 | 261.91 |
| receiver_event_loops=8 | 64338 | 263.53 |
| receiver_read_buffer_cap=0 | 32013 | 131.13 |
| receiver_read_buffer_cap=65536 | 32289 | 132.26 |
| receiver_read_buffer_cap=262144 | 64451 | 263.99 |
| receiver_read_buffer_cap=1048576 | 32244 | 132.07 |
| task_channel_queue_size=1024 | 64292 | 263.34 |
| task_channel_queue_size=4096 | 64138 | 262.71 |
| task_channel_queue_size=16384 | 64401 | 263.79 |
| task_pool_size=512 | 8031 | 32.89 |
| task_pool_size=2048 | 64200 | 262.96 |
| task_pool_size=8192 | 33493 | 137.19 |
| task_queue_size=1024 | 64550 | 264.40 |
| task_queue_size=4096 | 64032 | 262.28 |
| task_queue_size=16384 | 63909 | 261.77 |
| udp_sink_read_buf=1048576 | 31856 | 130.48 |
| udp_sink_read_buf=16777216 | 32031 | 131.20 |
| udp_sink_read_buf=67108864 | 64278 | 263.28 |
| udp_sink_readers=1 | 8049 | 32.97 |
| udp_sink_readers=2 | 8034 | 32.91 |
| udp_sink_readers=8 | 64306 | 263.40 |
| workers=1 | 15971 | 65.42 |
| workers=2 | 16048 | 65.73 |
| workers=8 | 64128 | 262.67 |
| task_fast_path=true | 64489 | 264.15 |

## TCP 结果

| 场景 | 无丢包最大PPS | 无丢包最大Mbps |
|---|---:|---:|
| execution_model=pool | 64287 | 263.32 |
| execution_model=channel | 64668 | 264.88 |
| execution_model=fastpath | 64414 | 263.84 |
| multicore=false | 64526 | 264.30 |
| multicore=true | 64345 | 263.56 |
| payload_size=256 | 64584 | 132.27 |
| payload_size=512 | 70123 | 287.22 |
| payload_size=1024 | 64638 | 529.51 |
| payload_size=4096 | 66515 | 2179.57 |
| receiver_event_loops=1 | 64159 | 262.80 |
| receiver_event_loops=2 | 64522 | 264.28 |
| receiver_event_loops=4 | 64120 | 262.63 |
| receiver_event_loops=8 | 64642 | 264.77 |
| receiver_read_buffer_cap=0 | 64083 | 262.48 |
| receiver_read_buffer_cap=65536 | 64329 | 263.49 |
| receiver_read_buffer_cap=262144 | 64594 | 264.58 |
| receiver_read_buffer_cap=1048576 | 63945 | 261.92 |
| task_channel_queue_size=1024 | 64262 | 263.22 |
| task_channel_queue_size=4096 | 63949 | 261.94 |
| task_channel_queue_size=16384 | 64327 | 263.48 |
| task_pool_size=512 | 64266 | 263.23 |
| task_pool_size=2048 | 63971 | 262.02 |
| task_pool_size=8192 | 64415 | 263.84 |
| task_queue_size=1024 | 64305 | 263.39 |
| task_queue_size=4096 | 61919 | 253.62 |
| task_queue_size=16384 | 64297 | 263.36 |
| tcp_sender_concurrency=1 | 64090 | 262.51 |
| tcp_sender_concurrency=2 | 64471 | 264.07 |
| tcp_sender_concurrency=4 | 64417 | 263.85 |
| tcp_sender_concurrency=8 | 62067 | 254.23 |
| tcp_sender_concurrency=16 | 64678 | 264.92 |
| workers=1 | 16031 | 65.66 |
| workers=2 | 32005 | 131.09 |
| workers=4 | 68919 | 282.29 |
| workers=8 | 135168 | 553.65 |
| validate_order=true | 64300 | 263.37 |

## Top 10（无丢包PPS）

| 排名 | 协议 | 场景 | 无丢包最大PPS | Mbps |
|---:|---|---|---:|---:|
| 1 | tcp | workers=8 | 135168 | 553.65 |
| 2 | tcp | payload_size=512 | 70123 | 287.22 |
| 3 | tcp | workers=4 | 68919 | 282.29 |
| 4 | tcp | payload_size=4096 | 66515 | 2179.57 |
| 5 | udp | payload_size=512 | 64782 | 265.35 |
| 6 | tcp | tcp_sender_concurrency=16 | 64678 | 264.92 |
| 7 | tcp | execution_model=channel | 64668 | 264.88 |
| 8 | tcp | receiver_event_loops=8 | 64642 | 264.77 |
| 9 | tcp | payload_size=1024 | 64638 | 529.51 |
| 10 | tcp | receiver_read_buffer_cap=262144 | 64594 | 264.58 |

原始数据：`docs/perf_extreme_sweep_raw_2026-03-10_72.json`。