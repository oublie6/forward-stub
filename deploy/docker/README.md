# deploy/docker 主线入口说明

当前仓库镜像入口统一在 `deploy/docker/...`，不再使用根目录旧 Dockerfile 路线。
当前默认目标架构统一为 `aarch64`，在 Docker / Buildx 平台语义中对应 `linux/arm64`，不再默认构建 `amd64` 镜像。

## 1. 目录与约束

说明：`deploy/docker/base-bookworm`、`skydds-runtime-bookworm` 以及对应镜像 tag / 归档文件名中的 `bookworm` 仅为历史兼容保留；当前 base userspace 已调整为更接近供应商要求的 legacy arm64 基线。

- SkyDDS 安装包目录（压缩包）：`third_party/skydds/packages/`
- SkyDDS SDK 解压目录：`third_party/skydds/sdk/`
- 离线基础镜像归档（必须保留）：`deploy/images/forward-stub-base-bookworm-arm64.tar.gz`

并且：

- `deploy/images/*.tar.gz` 必须继续由 Git LFS 管理（见 `.gitattributes`）。
- 不要把 `deploy/images/*.tar.gz` 写入 `.gitignore`。

## 2. 镜像入口

### 2.1 Base（Bookworm，基础环境镜像）

- Dockerfile: `deploy/docker/base-bookworm/Dockerfile`
- Build&Save: `deploy/docker/build-and-save-base-bookworm.sh`
- Load: `deploy/docker/load-base-bookworm.sh`
- 默认标签: `forward-stub-base:bookworm-arm64`
- 默认归档: `deploy/images/forward-stub-base-bookworm-arm64.tar.gz`
- 默认目标平台: `linux/arm64`（即 `aarch64`）

### 2.2 SkyDDS Runtime（Bookworm，主服务镜像）

- Dockerfile: `deploy/docker/skydds-runtime-bookworm/Dockerfile`
- Build&Save: `deploy/docker/build-and-save-skydds-runtime-bookworm.sh`
- Load: `deploy/docker/load-skydds-runtime-bookworm.sh`
- 默认标签: `forward-stub:skydds-bookworm-runtime-arm64`
- 默认归档: `deploy/images/forward-stub-skydds-bookworm-runtime-arm64.tar.gz`
- 默认目标平台: `linux/arm64`（即 `aarch64`）

该 Dockerfile 在构建阶段会：

0. 基于 `forward-stub-base:bookworm-arm64`（继承基础构建环境与 tcpdump）。
1. `COPY . .`（包含 `third_party/skydds/packages/`）。
2. 在容器内执行 `scripts/skydds/setup_linux.sh`，从 `third_party/skydds/packages/` 解压到 `third_party/skydds/sdk/`。
3. `source scripts/skydds/env.sh` 设置 `SKY_DDS` / `DDS_ROOT` / `ACE_ROOT` / `TAO_ROOT` / `LD_LIBRARY_PATH`。
4. `CGO_ENABLED=1 go build -tags skydds` 编译服务。

## 3. 使用方式（仓库根目录执行）

### 3.1 构建并导出 Base

```bash
./deploy/docker/build-and-save-base-bookworm.sh
```

如需显式覆盖平台，可传入：

```bash
PLATFORM=linux/arm64 ./deploy/docker/build-and-save-base-bookworm.sh
```

### 3.2 构建并导出 SkyDDS Runtime（主线）

```bash
./deploy/docker/build-and-save-skydds-runtime-bookworm.sh
```

运行时镜像默认同样按 `linux/arm64` 构建，并要求本地已先存在同平台的 `forward-stub-base:bookworm-arm64`。

### 3.3 导入离线归档

```bash
./deploy/docker/load-base-bookworm.sh
./deploy/docker/load-skydds-runtime-bookworm.sh
```

## 4. 前置条件与边界

- 构建 SkyDDS runtime 前，`third_party/skydds/packages/` 中必须放入 SkyDDS 安装包（仅支持 `*.tar.gz`）。
- 当前 build 脚本默认通过 `docker buildx build --platform linux/arm64 --load` 构建本地镜像。
- 若在 x86_64 / amd64 主机上构建 `aarch64` 镜像，通常需要 Docker Buildx，以及可用的 QEMU / `binfmt_misc` 跨架构模拟支持。
- load 脚本只负责导入归档，不会改写归档内镜像的目标架构；默认归档应视为 `linux/arm64`（`aarch64`）镜像。
- 当前镜像只能尽量把 userspace 基线对齐到供应商更老的 glibc / gcc 环境；`Linux 4.4.194` 这类最终内核版本仍取决于宿主机或部署节点，不是 Dockerfile 可在镜像内固定的内容。
- 本仓库脚本只提供构建与归档入口；是否能在当前环境真实完成 `linux/arm64` 构建，取决于 Docker daemon、Buildx、QEMU / binfmt 和 SkyDDS 安装包可用性。
