# Commands (main matrix)

- udp_fastpath_p256_w2: `./bin/bench -mode udp -duration 2s -warmup 1s -payload-size 256 -workers 2 -task-execution-model fastpath -pps-per-worker 0 -base-port 26010 -log-level info -traffic-stats-interval 30s`
- udp_fastpath_p256_w4: `./bin/bench -mode udp -duration 2s -warmup 1s -payload-size 256 -workers 4 -task-execution-model fastpath -pps-per-worker 0 -base-port 26020 -log-level info -traffic-stats-interval 30s`
- udp_fastpath_p1024_w2: `./bin/bench -mode udp -duration 2s -warmup 1s -payload-size 1024 -workers 2 -task-execution-model fastpath -pps-per-worker 0 -base-port 26030 -log-level info -traffic-stats-interval 30s`
- udp_fastpath_p1024_w4: `./bin/bench -mode udp -duration 2s -warmup 1s -payload-size 1024 -workers 4 -task-execution-model fastpath -pps-per-worker 0 -base-port 26040 -log-level info -traffic-stats-interval 30s`
- udp_fastpath_p4096_w2: `./bin/bench -mode udp -duration 2s -warmup 1s -payload-size 4096 -workers 2 -task-execution-model fastpath -pps-per-worker 0 -base-port 26050 -log-level info -traffic-stats-interval 30s`
- udp_fastpath_p4096_w4: `./bin/bench -mode udp -duration 2s -warmup 1s -payload-size 4096 -workers 4 -task-execution-model fastpath -pps-per-worker 0 -base-port 26060 -log-level info -traffic-stats-interval 30s`
- udp_pool_p256_w2: `./bin/bench -mode udp -duration 2s -warmup 1s -payload-size 256 -workers 2 -task-execution-model pool -pps-per-worker 0 -base-port 26070 -log-level info -traffic-stats-interval 30s`
- udp_pool_p256_w4: `./bin/bench -mode udp -duration 2s -warmup 1s -payload-size 256 -workers 4 -task-execution-model pool -pps-per-worker 0 -base-port 26080 -log-level info -traffic-stats-interval 30s`
- udp_pool_p1024_w2: `./bin/bench -mode udp -duration 2s -warmup 1s -payload-size 1024 -workers 2 -task-execution-model pool -pps-per-worker 0 -base-port 26090 -log-level info -traffic-stats-interval 30s`
- udp_pool_p1024_w4: `./bin/bench -mode udp -duration 2s -warmup 1s -payload-size 1024 -workers 4 -task-execution-model pool -pps-per-worker 0 -base-port 26100 -log-level info -traffic-stats-interval 30s`
- udp_pool_p4096_w2: `./bin/bench -mode udp -duration 2s -warmup 1s -payload-size 4096 -workers 2 -task-execution-model pool -pps-per-worker 0 -base-port 26110 -log-level info -traffic-stats-interval 30s`
- udp_pool_p4096_w4: `./bin/bench -mode udp -duration 2s -warmup 1s -payload-size 4096 -workers 4 -task-execution-model pool -pps-per-worker 0 -base-port 26120 -log-level info -traffic-stats-interval 30s`
- udp_channel_p256_w2: `./bin/bench -mode udp -duration 2s -warmup 1s -payload-size 256 -workers 2 -task-execution-model channel -pps-per-worker 0 -base-port 26130 -log-level info -traffic-stats-interval 30s`
- udp_channel_p256_w4: `./bin/bench -mode udp -duration 2s -warmup 1s -payload-size 256 -workers 4 -task-execution-model channel -pps-per-worker 0 -base-port 26140 -log-level info -traffic-stats-interval 30s`
- udp_channel_p1024_w2: `./bin/bench -mode udp -duration 2s -warmup 1s -payload-size 1024 -workers 2 -task-execution-model channel -pps-per-worker 0 -base-port 26150 -log-level info -traffic-stats-interval 30s`
- udp_channel_p1024_w4: `./bin/bench -mode udp -duration 2s -warmup 1s -payload-size 1024 -workers 4 -task-execution-model channel -pps-per-worker 0 -base-port 26160 -log-level info -traffic-stats-interval 30s`
- udp_channel_p4096_w2: `./bin/bench -mode udp -duration 2s -warmup 1s -payload-size 4096 -workers 2 -task-execution-model channel -pps-per-worker 0 -base-port 26170 -log-level info -traffic-stats-interval 30s`
- udp_channel_p4096_w4: `./bin/bench -mode udp -duration 2s -warmup 1s -payload-size 4096 -workers 4 -task-execution-model channel -pps-per-worker 0 -base-port 26180 -log-level info -traffic-stats-interval 30s`
- tcp_fastpath_p256_w2: `./bin/bench -mode tcp -duration 2s -warmup 1s -payload-size 256 -workers 2 -task-execution-model fastpath -pps-per-worker 0 -base-port 26190 -log-level info -traffic-stats-interval 30s`
- tcp_fastpath_p256_w4: `./bin/bench -mode tcp -duration 2s -warmup 1s -payload-size 256 -workers 4 -task-execution-model fastpath -pps-per-worker 0 -base-port 26200 -log-level info -traffic-stats-interval 30s`
- tcp_fastpath_p1024_w2: `./bin/bench -mode tcp -duration 2s -warmup 1s -payload-size 1024 -workers 2 -task-execution-model fastpath -pps-per-worker 0 -base-port 26210 -log-level info -traffic-stats-interval 30s`
- tcp_fastpath_p1024_w4: `./bin/bench -mode tcp -duration 2s -warmup 1s -payload-size 1024 -workers 4 -task-execution-model fastpath -pps-per-worker 0 -base-port 26220 -log-level info -traffic-stats-interval 30s`
- tcp_fastpath_p4096_w2: `./bin/bench -mode tcp -duration 2s -warmup 1s -payload-size 4096 -workers 2 -task-execution-model fastpath -pps-per-worker 0 -base-port 26230 -log-level info -traffic-stats-interval 30s`
- tcp_fastpath_p4096_w4: `./bin/bench -mode tcp -duration 2s -warmup 1s -payload-size 4096 -workers 4 -task-execution-model fastpath -pps-per-worker 0 -base-port 26240 -log-level info -traffic-stats-interval 30s`
- tcp_pool_p256_w2: `./bin/bench -mode tcp -duration 2s -warmup 1s -payload-size 256 -workers 2 -task-execution-model pool -pps-per-worker 0 -base-port 26250 -log-level info -traffic-stats-interval 30s`
- tcp_pool_p256_w4: `./bin/bench -mode tcp -duration 2s -warmup 1s -payload-size 256 -workers 4 -task-execution-model pool -pps-per-worker 0 -base-port 26260 -log-level info -traffic-stats-interval 30s`
- tcp_pool_p1024_w2: `./bin/bench -mode tcp -duration 2s -warmup 1s -payload-size 1024 -workers 2 -task-execution-model pool -pps-per-worker 0 -base-port 26270 -log-level info -traffic-stats-interval 30s`
- tcp_pool_p1024_w4: `./bin/bench -mode tcp -duration 2s -warmup 1s -payload-size 1024 -workers 4 -task-execution-model pool -pps-per-worker 0 -base-port 26280 -log-level info -traffic-stats-interval 30s`
- tcp_pool_p4096_w2: `./bin/bench -mode tcp -duration 2s -warmup 1s -payload-size 4096 -workers 2 -task-execution-model pool -pps-per-worker 0 -base-port 26290 -log-level info -traffic-stats-interval 30s`
- tcp_pool_p4096_w4: `./bin/bench -mode tcp -duration 2s -warmup 1s -payload-size 4096 -workers 4 -task-execution-model pool -pps-per-worker 0 -base-port 26300 -log-level info -traffic-stats-interval 30s`
- tcp_channel_p256_w2: `./bin/bench -mode tcp -duration 2s -warmup 1s -payload-size 256 -workers 2 -task-execution-model channel -pps-per-worker 0 -base-port 26310 -log-level info -traffic-stats-interval 30s`
- tcp_channel_p256_w4: `./bin/bench -mode tcp -duration 2s -warmup 1s -payload-size 256 -workers 4 -task-execution-model channel -pps-per-worker 0 -base-port 26320 -log-level info -traffic-stats-interval 30s`
- tcp_channel_p1024_w2: `./bin/bench -mode tcp -duration 2s -warmup 1s -payload-size 1024 -workers 2 -task-execution-model channel -pps-per-worker 0 -base-port 26330 -log-level info -traffic-stats-interval 30s`
- tcp_channel_p1024_w4: `./bin/bench -mode tcp -duration 2s -warmup 1s -payload-size 1024 -workers 4 -task-execution-model channel -pps-per-worker 0 -base-port 26340 -log-level info -traffic-stats-interval 30s`
- tcp_channel_p4096_w2: `./bin/bench -mode tcp -duration 2s -warmup 1s -payload-size 4096 -workers 2 -task-execution-model channel -pps-per-worker 0 -base-port 26350 -log-level info -traffic-stats-interval 30s`
- tcp_channel_p4096_w4: `./bin/bench -mode tcp -duration 2s -warmup 1s -payload-size 4096 -workers 4 -task-execution-model channel -pps-per-worker 0 -base-port 26360 -log-level info -traffic-stats-interval 30s`

## Additional commands

- runtime microbench: `go test ./src/runtime -run '^$' -bench BenchmarkDispatchMatrix -benchmem -benchtime=1s`
- task microbench: `go test ./src/task -run '^$' -bench 'BenchmarkTaskExecutionModels|BenchmarkTaskRouteSenderLookup' -benchmem -benchtime=1s`
- sweep commands: see raw logs `*_sweep_*.log`
