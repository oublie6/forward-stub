# 运维与配置变更建议

## 1. 推荐使用双配置模式

生产环境建议始终使用：

```bash
./bin/forward-stub -system-config ./configs/system.example.json -business-config ./configs/business.example.json
```

原因：

- `system` 配置通常需要重启生效。
- `business` 配置支持热更新。
- 分离后更适合做变更审计和风险隔离。

## 2. 变更前检查清单

### 2.1 JSON 结构检查

```bash
jq . ./configs/system.example.json >/dev/null
jq . ./configs/business.example.json >/dev/null
```

### 2.2 强约束检查

重点确认：

- receiver / selector / task_set / task / sender / pipeline 引用链是否完整。
- Kafka duration 字段是否都是合法正 duration。
- `concurrency` 若显式配置为正数，是否是 2 的幂。
- `control.pprof_port` 是否符合 `-1/0/1~65535` 语义。

## 3. 热更新时哪些配置不能改

当前不建议通过热更新修改 system 配置，包括：

- `control`
- `logging`
- `business_defaults`

这些变更会被当作 system 配置变化，需要重启进程后才能生效。

## 4. 变更后重点观察什么

- 是否出现 `配置校验失败`。
- receiver / sender 是否成功启动。
- 流量统计是否恢复到预期。
- task 是否出现队列满或 channel 入队失败日志。
