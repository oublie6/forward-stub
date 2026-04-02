# Offline Base Image

本次 `deploy/docker/` 只保留一个用于离线部署的 Bookworm 主线基础镜像：

- Bookworm 主线基础镜像

不再维护 Kali 镜像、debug 镜像、runtime 镜像或任何业务镜像入口。

## 为什么主线不再使用 Alpine

- 主线基础环境优先考虑 glibc 兼容性，而不是极限瘦身。
- Alpine 默认使用 musl，很多构建链、诊断工具和第三方二进制在离线环境里更容易遇到兼容性差异。
- 本次目标是一个可复用的离线基础 Linux 镜像，因此不再继续 Alpine 路线。

## 为什么选择 Bookworm / glibc

- `golang:1.25-bookworm` 直接提供 Debian Bookworm 和 Go 工具链，适合作为后续派生构建基础。
- Bookworm 走 glibc 路线，兼容性和排障体验都比 Alpine 更稳妥。
- 本次镜像定位是基础 Linux 镜像，不要求打入 SkyDDS SDK 或 `forward-stub` 应用。

## 镜像说明

- Dockerfile: `deploy/docker/base-bookworm/Dockerfile`
- 默认标签: `forward-stub-base:bookworm`
- 默认归档: `deploy/images/forward-stub-base-bookworm.tar.gz`
- 基础镜像: `golang:1.25-bookworm`
- 关键工具:
  - `bash`
  - `ca-certificates`
  - `file`
  - `gcc`
  - `g++`
  - `make`
  - `perl`
  - `tar`
  - `xz-utils`
  - `gzip`
  - `libstdc++6`
  - `tcpdump`
  - `iproute2`
  - `net-tools`
  - `procps`

## Build And Save

在仓库根目录执行：

```bash
./deploy/docker/build-and-save-base-bookworm.sh
```

脚本会真实执行：

1. `docker build`
2. `docker save`
3. `gzip`
4. 输出归档到 `deploy/images/forward-stub-base-bookworm.tar.gz`

支持的环境变量：

- `DOCKER_BIN`
- `IMAGE_TAG`
- `OUTPUT_FILE`

示例：

```bash
IMAGE_TAG=forward-stub-base:bookworm \
OUTPUT_FILE=deploy/images/forward-stub-base-bookworm.tar.gz \
./deploy/docker/build-and-save-base-bookworm.sh
```

如果 Docker daemon 不可用，脚本会直接报错并退出。

## Load

在仓库根目录执行：

```bash
./deploy/docker/load-base-bookworm.sh
```

对应逻辑等价于：

```bash
gunzip -c deploy/images/forward-stub-base-bookworm.tar.gz | docker load
```

`load` 脚本支持：

- `DOCKER_BIN`
- `OUTPUT_FILE`

## 离线归档用途

- `deploy/images/forward-stub-base-bookworm.tar.gz` 用于离线部署导入
- 可用于内网环境 `docker load`
- 可用于仓库存档

## tcpdump 权限注意事项

- 容器内执行 `tcpdump` 往往需要 `--cap-add NET_ADMIN --cap-add NET_RAW`
- 某些环境下还可能需要 `--network host`
- 即使镜像里已安装 `tcpdump`，平台策略也可能阻止抓包

示例：

```bash
docker run --rm -it \
  --cap-add NET_ADMIN \
  --cap-add NET_RAW \
  forward-stub-base:bookworm
```

## 实际验证边界

已验证：

- `deploy/docker/base-bookworm/Dockerfile` 可用于真实构建
- `./deploy/docker/build-and-save-base-bookworm.sh` 已真实执行 `docker build`、`docker save`、`gzip`
- `deploy/images/forward-stub-base-bookworm.tar.gz` 已真实生成

未验证：

- `./deploy/docker/load-base-bookworm.sh` 在新的离线目标环境中的实际导入结果
- 基于该基础镜像派生出的后续镜像构建链
