# forward-stub

面向高吞吐报文转发场景的 Go 服务，支持多种 receiver/sender（UDP、TCP、Kafka、SFTP）和 pipeline 编排。

## 1. 环境准备

- Go：`1.25.x`（与 `go.mod` 一致）
- Docker：`20.10+`
- Kubernetes：`1.24+`（可选）

## 2. 本地运行

```bash
go run . -config ./configs/example.json
```

> 程序启动必须显式传入 `-config` 参数。

## 3. Docker 部署（详细步骤）

### 3.1 构建镜像

```bash
docker build -t forward-stub:dev \
  --build-arg VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo dev) \
  --build-arg TARGETOS=linux \
  --build-arg TARGETARCH=arm64 \
  .
```

如需 amd64：把 `TARGETARCH=arm64` 改为 `TARGETARCH=amd64`。

### 3.2 准备配置文件

建议先复制示例配置：

```bash
cp ./configs/example.json ./configs/config.json
```

按需修改 `./configs/config.json`（例如监听端口、sender 目标地址、日志级别等）。

### 3.3 启动容器（在 `docker run` 指定挂载和参数）

> Dockerfile 中不再声明 `VOLUME` 和 `CMD`，由运行命令显式指定。

```bash
docker run -d --name forward-stub \
  -p 19000:19000/udp \
  -v $(pwd)/configs/config.json:/app/config/config.json:ro \
  -v $(pwd)/logs:/var/log/forward-stub \
  forward-stub:dev \
  -config /app/config/config.json
```

说明：

- `-v ...:/app/config/config.json:ro`：挂载配置文件（只读）
- `-v ...:/var/log/forward-stub`：挂载日志目录
- 最后的 `-config ...`：作为容器启动参数传给 `ENTRYPOINT`

### 3.4 运维命令

```bash
# 查看日志
docker logs -f forward-stub

# 查看容器内进程参数
docker inspect forward-stub --format='{{json .Config.Cmd}}'

# 停止并删除
docker rm -f forward-stub
```

## 4. Kubernetes 部署（详细步骤）

仓库提供了 `deploy/k8s/` 下的基础清单（Namespace、ConfigMap、Deployment、Service、Kustomization）。

### 4.1 方式 A：使用现有 YAML（推荐）

1) 构建并推送镜像

```bash
docker build -t <registry>/forward-stub:<tag> .
docker push <registry>/forward-stub:<tag>
```

2) 替换镜像地址（两种任选其一）

- 方式 1：改 `deploy/k8s/kustomization.yaml` 中 `images`。
- 方式 2：直接命令行覆盖：

```bash
kubectl kustomize deploy/k8s | \
  sed 's#image: forward-stub:latest#image: <registry>/forward-stub:<tag>#g' | \
  kubectl apply -f -
```

3) 部署

```bash
kubectl apply -k deploy/k8s
```

4) 查看状态

```bash
kubectl -n forward-stub get pods
kubectl -n forward-stub logs -f deploy/forward-stub
```

### 4.2 方式 B：使用 `kubectl run`（在命令中指定参数）

如果你希望像 `docker run` 一样一次性启动，可用如下命令（示例将 ConfigMap 挂载到 `/app/config`，并传入 `-config` 参数）：

```bash
kubectl create namespace forward-stub
kubectl -n forward-stub create configmap forward-stub-config \
  --from-file=config.json=./configs/config.json

kubectl -n forward-stub run forward-stub \
  --image=<registry>/forward-stub:<tag> \
  --restart=Never \
  --overrides='{
    "apiVersion": "v1",
    "spec": {
      "containers": [{
        "name": "forward-stub",
        "image": "<registry>/forward-stub:<tag>",
        "args": ["-config", "/app/config/config.json"],
        "volumeMounts": [{
          "name": "config",
          "mountPath": "/app/config",
          "readOnly": true
        }],
        "ports": [{"containerPort": 19000, "protocol": "UDP"}]
      }],
      "volumes": [{
        "name": "config",
        "configMap": {"name": "forward-stub-config"}
      }]
    }
  }'
```

查看日志：

```bash
kubectl -n forward-stub logs -f pod/forward-stub
```

## 5. 目录说明（精简）

```text
.
├── main.go
├── configs/                # 示例配置
├── src/                    # 核心实现（runtime/receiver/sender/pipeline 等）
├── deploy/k8s/             # Kubernetes 清单
├── scripts/                # 构建与部署辅助脚本
└── docs/technical-architecture.md
```

## 6. 参考文档

- 技术架构文档：`docs/technical-architecture.md`
