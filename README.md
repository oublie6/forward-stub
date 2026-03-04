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

## 7. 吞吐量测试复验（多次实验取平均）

为复验当前仓库中的吞吐相关 benchmark，本次在同一环境下执行了 5 轮重复实验（`-count=5`），并对每个子用例计算平均值。

执行命令：

```bash
go test ./src/runtime -run '^$' \
  -bench 'Benchmark(DispatchMatrix|PayloadLogSwitchThroughput)$' \
  -benchmem -count=5 -benchtime=1s
```

测试环境（来自 `go test` 输出）：

- `goos=linux`
- `goarch=amd64`
- `cpu=Intel(R) Xeon(R) Platinum 8370C CPU @ 2.80GHz`

> 说明：下表均为 5 次实验的算术平均值；单次压测会受调度、CPU 频率波动等因素影响。

### 7.1 Dispatch Matrix（4x4 协议组合）

| 输入->输出 | 包长 | 平均 ns/op | 平均 MB/s | B/op | allocs/op |
|---|---:|---:|---:|---:|---:|
| `kafka_to_kafka` | 256B | 1274.6 | 202.94 | 592 | 6 |
| `kafka_to_kafka` | 4096B | 5711.4 | 718.92 | 4432 | 6 |
| `kafka_to_sftp` | 256B | 1232.2 | 209.10 | 592 | 6 |
| `kafka_to_sftp` | 4096B | 5575.4 | 738.94 | 4432 | 6 |
| `kafka_to_tcp` | 256B | 1297.8 | 197.92 | 592 | 6 |
| `kafka_to_tcp` | 4096B | 5797.8 | 709.79 | 4432 | 6 |
| `kafka_to_udp` | 256B | 1311.0 | 196.34 | 592 | 6 |
| `kafka_to_udp` | 4096B | 5815.0 | 705.83 | 4432 | 6 |
| `sftp_to_kafka` | 256B | 1233.8 | 207.87 | 592 | 6 |
| `sftp_to_kafka` | 4096B | 5486.6 | 749.69 | 4432 | 6 |
| `sftp_to_sftp` | 256B | 1294.2 | 198.99 | 592 | 6 |
| `sftp_to_sftp` | 4096B | 5852.4 | 703.55 | 4432 | 6 |
| `sftp_to_tcp` | 256B | 1225.4 | 209.53 | 592 | 6 |
| `sftp_to_tcp` | 4096B | 5485.8 | 747.48 | 4432 | 6 |
| `sftp_to_udp` | 256B | 1206.6 | 212.89 | 592 | 6 |
| `sftp_to_udp` | 4096B | 6023.2 | 681.09 | 4432 | 6 |
| `tcp_to_kafka` | 256B | 1322.0 | 194.50 | 592 | 6 |
| `tcp_to_kafka` | 4096B | 5796.8 | 708.84 | 4432 | 6 |
| `tcp_to_sftp` | 256B | 1331.0 | 192.67 | 592 | 6 |
| `tcp_to_sftp` | 4096B | 5848.2 | 710.09 | 4432 | 6 |
| `tcp_to_tcp` | 256B | 1302.6 | 196.89 | 592 | 6 |
| `tcp_to_tcp` | 4096B | 5484.2 | 748.54 | 4432 | 6 |
| `tcp_to_udp` | 256B | 1311.8 | 195.48 | 592 | 6 |
| `tcp_to_udp` | 4096B | 5496.8 | 746.83 | 4432 | 6 |
| `udp_to_kafka` | 256B | 1410.2 | 181.63 | 592 | 6 |
| `udp_to_kafka` | 4096B | 5778.4 | 711.12 | 4432 | 6 |
| `udp_to_sftp` | 256B | 1347.8 | 190.38 | 592 | 6 |
| `udp_to_sftp` | 4096B | 5332.2 | 770.15 | 4432 | 6 |
| `udp_to_tcp` | 256B | 1376.4 | 186.35 | 592 | 6 |
| `udp_to_tcp` | 4096B | 5748.8 | 715.44 | 4432 | 6 |
| `udp_to_udp` | 256B | 1371.2 | 187.26 | 589 | 5.8 |
| `udp_to_udp` | 4096B | 5717.2 | 718.17 | 4432 | 6 |

### 7.2 Payload Log 开关吞吐

| 协议 | payloadLog | 包长 | 平均 ns/op | 平均 MB/s | B/op | allocs/op |
|---|---|---:|---:|---:|---:|---:|
| `kafka` | `false` | 256B | 1074.3 | 266.81 | 592 | 6 |
| `kafka` | `false` | 4096B | 3101.6 | 1328.09 | 4432 | 6 |
| `kafka` | `true` | 256B | 674.1 | 380.11 | 592 | 6 |
| `kafka` | `true` | 4096B | 3042.4 | 1351.97 | 4432 | 6 |
| `tcp` | `false` | 256B | 1206.2 | 213.46 | 592 | 6 |
| `tcp` | `false` | 4096B | 5673.0 | 725.06 | 4432 | 6 |
| `tcp` | `true` | 256B | 1155.8 | 221.55 | 592 | 6 |
| `tcp` | `true` | 4096B | 6220.2 | 661.58 | 4432 | 6 |
| `udp` | `false` | 256B | 1236.6 | 208.01 | 592 | 6 |
| `udp` | `false` | 4096B | 6243.0 | 659.24 | 4432 | 6 |
| `udp` | `true` | 256B | 1257.0 | 203.82 | 592 | 6 |
| `udp` | `true` | 4096B | 6326.2 | 647.77 | 4432 | 6 |
