# 极限无丢包转发吞吐量扫参复测（2026-03-10）

> 说明：已清理旧结果，仅保留本轮最新结果。口径为每个场景在 `loss_rate=0` 下的最大吞吐。

## 测试参数

- duration=2s
- warmup=1s
- pps-sweep=4000,8000,16000,32000,64000,96000
- execution_model 场景：payload_size=512, workers=2
- payload 场景：execution_model=pool, workers=8

## UDP - execution_model 场景

| 场景 | 无丢包最大PPS | 无丢包最大Mbps |
| --- | ---: | ---: |
| execution_model=fastpath | 16100 | 65.94 |
| execution_model=pool | 16003 | 65.55 |
| execution_model=channel | 16007 | 65.57 |

## UDP - payload 场景

| 场景 | 无丢包最大PPS | 无丢包最大Mbps |
| --- | ---: | ---: |
| payload_size=256 | 31982 | 65.50 |
| payload_size=512 | 32016 | 131.14 |
| payload_size=1024 | 0 | 0.00 |
| payload_size=4096 | 0 | 0.00 |

## TCP - execution_model 场景

| 场景 | 无丢包最大PPS | 无丢包最大Mbps |
| --- | ---: | ---: |
| execution_model=fastpath | 192214 | 787.31 |
| execution_model=pool | 182973 | 749.46 |
| execution_model=channel | 193967 | 794.49 |

## TCP - payload 场景

| 场景 | 无丢包最大PPS | 无丢包最大Mbps |
| --- | ---: | ---: |
| payload_size=256 | 313994 | 643.06 |
| payload_size=512 | 262001 | 1073.16 |
| payload_size=1024 | 204468 | 1675.00 |
| payload_size=4096 | 98480 | 3227.00 |

## Top 10（无丢包PPS）

| 排名 | 协议 | 场景 | 无丢包最大PPS | Mbps |
| ---: | --- | --- | ---: | ---: |
| 1 | tcp | payload_size=256 | 313994 | 643.06 |
| 2 | tcp | payload_size=512 | 262001 | 1073.16 |
| 3 | tcp | payload_size=1024 | 204468 | 1675.00 |
| 4 | tcp | execution_model=channel | 193967 | 794.49 |
| 5 | tcp | execution_model=fastpath | 192214 | 787.31 |
| 6 | tcp | execution_model=pool | 182973 | 749.46 |
| 7 | tcp | payload_size=4096 | 98480 | 3227.00 |
| 8 | udp | payload_size=512 | 32016 | 131.14 |
| 9 | udp | payload_size=256 | 31982 | 65.50 |
| 10 | udp | execution_model=fastpath | 16100 | 65.94 |

原始数据：`docs/perf_extreme_sweep_raw_2026-03-10.json`。