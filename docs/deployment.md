# Deployment

## 1. 本地部署

### 1.1 构建

```bash
make build
```

输出：`bin/forward-stub`。

### 1.2 启动

推荐双配置：

```bash
./bin/forward-stub \
  -system-config ./configs/system.example.json \
  -business-config ./configs/business.example.json
```

legacy：

```bash
./bin/forward-stub -config ./configs/example.json
```

## 2. Docker 部署

### 2.1 构建镜像

```bash
make docker-build VERSION=$(git rev-parse --short HEAD)
```

### 2.2 本地运行容器

```bash
make docker-run SERVICE_ARGS='-system-config /app/config/system.json -business-config /app/config/business.json'
```

> 说明：`docker-run` 默认 host 网络模式，适合本地转发测试。

### 2.3 相关脚本

- `scripts/docker-local-test.sh`：受限环境下的本地镜像构建验证辅助脚本。

## 3. Kubernetes 部署

`deploy/k8s/` 目录包含：

- `namespace.yaml`
- `configmap.yaml`
- `deployment.yaml`
- `service.yaml`
- `kustomization.yaml`

快速操作：

```bash
./scripts/k8s-deploy.sh apply
./scripts/k8s-deploy.sh diff
./scripts/k8s-deploy.sh delete
```

## 4. deploy/k8s 说明与注意事项

- 当前清单示例使用 `-config /app/config/config.json`（legacy 单文件模式）。
- 若切换双文件模式，需要同步修改 `args` 与 ConfigMap 内容。
- 默认暴露 UDP `19000` 端口（Service `ClusterIP`）。

## 5. 配置挂载方式

- K8s 示例通过 ConfigMap 挂载到 `/app/config`。
- 日志目录挂载到 `/var/log/forward-stub`（`emptyDir`）。
- 容器采用 `readOnlyRootFilesystem: true`，临时目录用 `/tmp` 卷。

## 6. 资源建议（起步）

- CPU：`500m~2`（按协议和包长压测调整）。
- 内存：`256Mi~1Gi`（与队列深度、并发、日志策略相关）。
- 若启用 Kafka/SFTP 或大批量文件场景，建议上调内存与网络 buffer。

## 7. 常见部署问题

1. **端口无流量**：检查 Service 协议是否为 UDP/TCP 且与 receiver 类型一致。
2. **配置未生效**：确认挂载路径和启动参数一致。
3. **容器无法写日志**：检查日志路径权限和只读根文件系统策略。
4. **热更新不生效**：检查 `control.config_watch_interval` 与文件更新时间。
