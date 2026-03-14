# Operations

## 1. 运维目标

本文面向值班和维护人员，提供日常操作、巡检、变更和风险控制建议。

## 2. 启动与停止

### 启动

```bash
./bin/forward-stub -system-config ./configs/system.example.json -business-config ./configs/business.example.json
```

### 停止

- 前台运行直接 `Ctrl+C`。
- 后台运行发送停止信号（TERM/INT）。

停止时 runtime 会执行优雅下线，等待 task in-flight 完成。

## 3. 配置变更操作

- 修改 business 配置后，等待文件监听触发或发送重载信号。
- 若误改 system 配置，reload 会被拒绝，需重启生效。

建议流程：

1. 备份当前 business 文件。
2. 预检查 JSON 格式。
3. 应用并观察日志。
4. 验证吞吐与错误率。

## 4. 日志使用方式

关键日志类别：

- 启动与配置加载日志。
- task 队列满丢包日志。
- sender 发送异常日志。
- 流量聚合统计日志。

建议：

- 生产默认 `info` 或 `warn`。
- 排障窗口短时启用更高日志级别。

## 5. 常用运维命令

```bash
# 版本
./bin/forward-stub -version

# 监听端口
ss -lntup | rg forward-stub

# 触发重载
kill -HUP <pid>

# 查看最近日志
journalctl -u forward-stub -n 200 --no-pager
```

## 6. 巡检建议

每个巡检周期建议确认：

- 进程存活和端口状态。
- 错误日志是否突增。
- 流量统计是否偏离基线。
- 下游链路可达性是否稳定。

## 7. bench 入口与运维意义

`cmd/bench` 可用于：

- 上线前容量评估。
- 版本升级回归。
- 参数调优对比。

示例：

```bash
go run ./cmd/bench -config ./configs/bench.example.json
```

## 8. pprof 与运行诊断

若 `control.pprof_port` 开启，可执行：

```bash
go tool pprof http://127.0.0.1:6060/debug/pprof/profile?seconds=30
go tool pprof http://127.0.0.1:6060/debug/pprof/heap
```

## 9. 常见操作风险

- 长期开启 payload 日志会影响吞吐并放大日志体积。
- 队列参数盲目调大可能造成内存压力。
- 未验证的 route sender 配置会导致转发丢失。
