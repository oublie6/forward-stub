# Operations

## 1. 服务启动与停止

### 启动

```bash
./bin/forward-stub -system-config ./configs/system.example.json -business-config ./configs/business.example.json
```

### 停止

- 前台：`Ctrl+C`。
- 后台：发送终止信号（如 `kill -TERM <pid>`）。

runtime 在停止阶段会：

1. 停止 receiver。
2. 等待 task in-flight 请求完成。
3. 关闭 sender 连接。

## 2. 日志与流量统计

- 日志由 `src/logx` 统一管理，支持 stdout/stderr 或文件落盘。
- 关键配置：`logging.level`、`logging.file`、`traffic_stats_interval`。
- payload 日志默认关闭，可在 receiver/task 层按需打开。

## 3. 配置更新方式

- 文件监听：按 `control.config_watch_interval` 检测 business 文件变更。
- 信号触发：`HUP/USR1`（平台差异见 `src/bootstrap/signal_*.go`）。
- 约束：若 system 配置变化，reload 会被拒绝并提示需重启。

## 4. 日常巡检建议

1. 检查进程存活与启动参数。
2. 检查 error/warn 日志突增。
3. 检查流量统计是否符合预期（收发速率、异常波动）。
4. 检查下游可达性（尤其 Kafka/SFTP）。
5. 定期抓取 pprof（若开启）。

## 5. bench 使用入口

```bash
go run ./cmd/bench -config ./configs/bench.example.json
```

或手工参数：

```bash
go run ./cmd/bench -mode both -duration 4s -warmup 1s -payload-size 512 -workers 2 -pps-sweep 2000,4000,8000
```

## 6. pprof 使用入口

开启：`control.pprof_port > 0`（默认 6060）。

常用命令：

```bash
go tool pprof http://127.0.0.1:6060/debug/pprof/profile?seconds=30
go tool pprof http://127.0.0.1:6060/debug/pprof/heap
curl -s http://127.0.0.1:6060/debug/pprof/goroutine?debug=1 | head
```

## 7. 运行中建议观测项

- Receiver 入站速率、异常日志。
- Task 丢包告警（队列满、上下文取消）。
- Sender 发送错误率与超时。
- CPU、内存、GC、goroutine 数。

## 8. 常用运维命令

```bash
# 查看版本
./bin/forward-stub -version

# 触发业务配置重载（Unix）
kill -HUP <pid>

# 查看端口监听
ss -lntup | rg forward-stub

# 快速查看最近日志
journalctl -u forward-stub -n 200 --no-pager
```
