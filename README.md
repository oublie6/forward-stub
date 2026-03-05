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

补充丢包率验证：

- `BenchmarkPayloadLogSwitchThroughput` 在每个子用例内有 `cap.pkts == b.N` 断言，复验结果均通过，可视为丢包率 `0`。
- `BenchmarkDispatchMatrix` 原始 benchmark 未统计丢包；本次额外运行了临时校验测试（同样的 4x4 协议矩阵、256B/4096B），逐项校验 `recv == sent`，结果均为丢包率 `0`。

> 说明：下表均为 5 次实验的算术平均值；单次压测会受调度、CPU 频率波动等因素影响。

### 7.1 Dispatch Matrix（4x4 协议组合）

| 输入->输出 | 包长 | 平均 ns/op | 平均 MB/s | 平均丢包率 | B/op | allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| `kafka_to_kafka` | 256B | 1274.6 | 202.94 | 0 | 592 | 6 |
| `kafka_to_kafka` | 4096B | 5711.4 | 718.92 | 0 | 4432 | 6 |
| `kafka_to_sftp` | 256B | 1232.2 | 209.10 | 0 | 592 | 6 |
| `kafka_to_sftp` | 4096B | 5575.4 | 738.94 | 0 | 4432 | 6 |
| `kafka_to_tcp` | 256B | 1297.8 | 197.92 | 0 | 592 | 6 |
| `kafka_to_tcp` | 4096B | 5797.8 | 709.79 | 0 | 4432 | 6 |
| `kafka_to_udp` | 256B | 1311.0 | 196.34 | 0 | 592 | 6 |
| `kafka_to_udp` | 4096B | 5815.0 | 705.83 | 0 | 4432 | 6 |
| `sftp_to_kafka` | 256B | 1233.8 | 207.87 | 0 | 592 | 6 |
| `sftp_to_kafka` | 4096B | 5486.6 | 749.69 | 0 | 4432 | 6 |
| `sftp_to_sftp` | 256B | 1294.2 | 198.99 | 0 | 592 | 6 |
| `sftp_to_sftp` | 4096B | 5852.4 | 703.55 | 0 | 4432 | 6 |
| `sftp_to_tcp` | 256B | 1225.4 | 209.53 | 0 | 592 | 6 |
| `sftp_to_tcp` | 4096B | 5485.8 | 747.48 | 0 | 4432 | 6 |
| `sftp_to_udp` | 256B | 1206.6 | 212.89 | 0 | 592 | 6 |
| `sftp_to_udp` | 4096B | 6023.2 | 681.09 | 0 | 4432 | 6 |
| `tcp_to_kafka` | 256B | 1322.0 | 194.50 | 0 | 592 | 6 |
| `tcp_to_kafka` | 4096B | 5796.8 | 708.84 | 0 | 4432 | 6 |
| `tcp_to_sftp` | 256B | 1331.0 | 192.67 | 0 | 592 | 6 |
| `tcp_to_sftp` | 4096B | 5848.2 | 710.09 | 0 | 4432 | 6 |
| `tcp_to_tcp` | 256B | 1302.6 | 196.89 | 0 | 592 | 6 |
| `tcp_to_tcp` | 4096B | 5484.2 | 748.54 | 0 | 4432 | 6 |
| `tcp_to_udp` | 256B | 1311.8 | 195.48 | 0 | 592 | 6 |
| `tcp_to_udp` | 4096B | 5496.8 | 746.83 | 0 | 4432 | 6 |
| `udp_to_kafka` | 256B | 1410.2 | 181.63 | 0 | 592 | 6 |
| `udp_to_kafka` | 4096B | 5778.4 | 711.12 | 0 | 4432 | 6 |
| `udp_to_sftp` | 256B | 1347.8 | 190.38 | 0 | 592 | 6 |
| `udp_to_sftp` | 4096B | 5332.2 | 770.15 | 0 | 4432 | 6 |
| `udp_to_tcp` | 256B | 1376.4 | 186.35 | 0 | 592 | 6 |
| `udp_to_tcp` | 4096B | 5748.8 | 715.44 | 0 | 4432 | 6 |
| `udp_to_udp` | 256B | 1371.2 | 187.26 | 0 | 589 | 5.8 |
| `udp_to_udp` | 4096B | 5717.2 | 718.17 | 0 | 4432 | 6 |

### 7.2 Payload Log 开关吞吐

| 协议 | payloadLog | 包长 | 平均 ns/op | 平均 MB/s | 平均丢包率 | B/op | allocs/op |
|---|---|---:|---:|---:|---:|---:|---:|
| `kafka` | `false` | 256B | 1074.3 | 266.81 | 0 | 592 | 6 |
| `kafka` | `false` | 4096B | 3101.6 | 1328.09 | 0 | 4432 | 6 |
| `kafka` | `true` | 256B | 674.1 | 380.11 | 0 | 592 | 6 |
| `kafka` | `true` | 4096B | 3042.4 | 1351.97 | 0 | 4432 | 6 |
| `tcp` | `false` | 256B | 1206.2 | 213.46 | 0 | 592 | 6 |
| `tcp` | `false` | 4096B | 5673.0 | 725.06 | 0 | 4432 | 6 |
| `tcp` | `true` | 256B | 1155.8 | 221.55 | 0 | 592 | 6 |
| `tcp` | `true` | 4096B | 6220.2 | 661.58 | 0 | 4432 | 6 |
| `udp` | `false` | 256B | 1236.6 | 208.01 | 0 | 592 | 6 |
| `udp` | `false` | 4096B | 6243.0 | 659.24 | 0 | 4432 | 6 |
| `udp` | `true` | 256B | 1257.0 | 203.82 | 0 | 592 | 6 |
| `udp` | `true` | 4096B | 6326.2 | 647.77 | 0 | 4432 | 6 |

### 7.3 丢包率为 0 的最大吞吐（`cmd/bench` 实测）

为补充“工程链路（生成流量 -> runtime -> sink）”口径下的 0 丢包上限，本次使用 `cmd/bench` 对 UDP/TCP 做 pps 扫描，选取 `loss_rate == 0` 的最大吞吐结果。

执行命令：

```bash
# UDP
go run ./cmd/bench -mode udp -duration 3s -warmup 1s -payload-size 512 \
  -workers 2 -pps-sweep 20000,40000,60000,80000,100000 \
  -log-level info -traffic-stats-interval 1h

# TCP
go run ./cmd/bench -mode tcp -duration 3s -warmup 1s -payload-size 512 \
  -workers 2 -pps-sweep 5000,10000,15000,20000,30000 \
  -log-level info -traffic-stats-interval 1h
```

| 协议 | payload | workers | 扫描范围（pps/worker） | 丢包率=0 的最大吞吐 (Mbps) | 对应 pps |
|---|---:|---:|---|---:|---:|
| UDP | 512B | 2 | 20000,40000,60000,80000,100000 | 264.70 | 64625.21 |
| TCP | 512B | 2 | 5000,10000,15000,20000,30000 | 78.38 | 19135.83 |

### 7.4 三种执行模型多轮平均（0 丢包口径）

为减少单次波动影响，本次对 `fastpath` / `pool` / `channel` 三种任务执行模型分别执行 3 轮 UDP 扫描测试（同一参数口径），每轮取 `loss_rate == 0` 的最大吞吐，再对 3 轮结果取平均。

执行命令（每个模型重复 3 轮）：

```bash
# fastpath
go run ./cmd/bench -mode udp -duration 3s -warmup 1s -payload-size 512 \
  -workers 2 -pps-sweep 20000,40000,60000,80000,100000,120000 \
  -task-execution-model fastpath -log-level info -traffic-stats-interval 1h

# pool
go run ./cmd/bench -mode udp -duration 3s -warmup 1s -payload-size 512 \
  -workers 2 -pps-sweep 20000,40000,60000,80000,100000,120000 \
  -task-execution-model pool -task-pool-size 2048 -log-level info -traffic-stats-interval 1h

# channel
go run ./cmd/bench -mode udp -duration 3s -warmup 1s -payload-size 512 \
  -workers 2 -pps-sweep 20000,40000,60000,80000,100000,120000 \
  -task-execution-model channel -task-channel-queue-size 4096 \
  -log-level info -traffic-stats-interval 1h
```

各轮 0 丢包最大吞吐（Mbps）：

| 执行模型 | 第 1 轮 | 第 2 轮 | 第 3 轮 | 3 轮平均 Mbps | 3 轮平均 pps |
|---|---:|---:|---:|---:|---:|
| fastpath | 156.44 | 316.66 | 296.93 | 256.68 | 62665.99 |
| pool | 174.40 | 80.83 | 116.57 | 123.93 | 30257.15 |
| channel | 215.95 | 221.96 | 196.79 | 211.57 | 51651.91 |

结论（本机 3 轮均值）：`fastpath > channel > pool`。
