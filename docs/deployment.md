# Deployment

## 1. 部署方式总览

仓库提供三类部署路径：

- 本地二进制运行。
- Docker 镜像运行。
- Kubernetes 清单部署。

## 2. 本地部署（SkyDDS）

### 步骤

1. 准备 SkyDDS SDK（`third_party/skydds/packages/` -> `third_party/skydds/sdk/`）
2. `make build-skydds`
3. 准备 `system.example.json` 与 `business.example.json`
4. 启动二进制

```bash
./bin/forward-stub -system-config ./configs/system.example.json -business-config ./configs/business.example.json
```

### 适用场景

- 开发调试。
- 单机联调。
- 配置验证。

## 3. Docker 部署（主线入口：`deploy/docker/...`）

### 构建（主服务镜像，Bookworm runtime）

```bash
make docker-build-skydds-runtime
```

### 运行（示例）

```bash
docker run --rm -it forward-stub:skydds-bookworm-runtime
```

### 相关文件

- `deploy/docker/skydds-runtime-bookworm/Dockerfile`：Bookworm/glibc 主服务镜像，构建阶段自动完成 `packages -> sdk` 解压并 `-tags skydds` 编译。
- `scripts/docker-local-test.sh`：受限环境的镜像构建验证脚本。

## 4. Kubernetes 部署

目录：`deploy/k8s/`

- `namespace.yaml`
- `configmap.yaml`
- `deployment.yaml`
- `service.yaml`
- `kustomization.yaml`

脚本入口：

```bash
./scripts/k8s-deploy.sh apply
./scripts/k8s-deploy.sh diff
./scripts/k8s-deploy.sh delete
```

## 5. Makefile 与 scripts 角色（主线）

- Makefile：统一 `build-skydds`、`test`、`vet`、`perf`、`deploy/docker` 镜像入口。
- `scripts/build-linux.sh`：linux 制品构建。
- `scripts/package.sh`：多平台归档。
- `scripts/k8s-deploy.sh`：k8s 生命周期操作。

## 6. 配置挂载与端口

- 本地模式：直接读取主机文件。
- Docker 模式：建议挂载配置目录到容器。
- K8s 示例：ConfigMap 挂载到 `/app/config`。
- 默认示例端口：UDP 19000（按 receiver 配置调整）。

## 7. 资源建议

起步建议：

- CPU：500m 到 2 核。
- Memory：256Mi 到 1Gi。

实际值需根据协议类型、payload 大小、并发与 sender 下游能力压测确定。

## 8. 环境差异建议

- 开发环境：优先本地运行，快速迭代配置。
- 测试环境：Docker 或 K8s，验证部署脚本与资源边界。
- 生产环境：建议双配置模式，分离 system 与 business 变更。

## 9. 常见部署错误

- 参数模式混用（单文件与双文件参数冲突）。
- 配置挂载路径与启动参数不一致。
- 协议端口未开放或 Service 协议类型错误。
- 指纹配置错误导致 SFTP 启动失败。
