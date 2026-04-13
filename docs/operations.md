# 运维入口

本文不再重复维护操作手册正文，避免启动、重载、停机、巡检和排障步骤在多份文档之间漂移。

权威入口：

- 标准操作手册：`docs/operations-manual.md`
- 配置字段：`docs/configuration.md`
- 热更新与生命周期：`docs/runtime-and-lifecycle.md`
- 可观测与排障：`docs/observability.md`
- 常见问题：`docs/troubleshooting.md`

常用启动命令：

```bash
./bin/forward-stub -system-config ./configs/system.example.json -business-config ./configs/business.example.json
```

Unix 业务配置重载：

```bash
kill -HUP <pid>
kill -USR1 <pid>
```
