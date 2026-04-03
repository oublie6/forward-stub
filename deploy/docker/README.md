# deploy/docker 主线入口说明

当前仓库镜像入口统一在 `deploy/docker/...`，不再使用根目录旧 Dockerfile 路线。

## 1. 目录与约束

- SkyDDS 安装包目录（压缩包）：`third_party/skydds/packages/`
- SkyDDS SDK 解压目录：`third_party/skydds/sdk/`
- 离线基础镜像归档（必须保留）：`deploy/images/forward-stub-base-bookworm.tar.gz`

并且：

- `deploy/images/*.tar.gz` 必须继续由 Git LFS 管理（见 `.gitattributes`）。
- 不要把 `deploy/images/*.tar.gz` 写入 `.gitignore`。

## 2. 镜像入口

### 2.1 Base（Bookworm，基础环境镜像）

- Dockerfile: `deploy/docker/base-bookworm/Dockerfile`
- Build&Save: `deploy/docker/build-and-save-base-bookworm.sh`
- Load: `deploy/docker/load-base-bookworm.sh`
- 默认标签: `forward-stub-base:bookworm`
- 默认归档: `deploy/images/forward-stub-base-bookworm.tar.gz`

### 2.2 SkyDDS Runtime（Bookworm，主服务镜像）

- Dockerfile: `deploy/docker/skydds-runtime-bookworm/Dockerfile`
- Build&Save: `deploy/docker/build-and-save-skydds-runtime-bookworm.sh`
- Load: `deploy/docker/load-skydds-runtime-bookworm.sh`
- 默认标签: `forward-stub:skydds-bookworm-runtime`
- 默认归档: `deploy/images/forward-stub-skydds-bookworm-runtime.tar.gz`

该 Dockerfile 在构建阶段会：

1. `COPY . .`（包含 `third_party/skydds/packages/`）。
2. 在容器内执行 `scripts/skydds/setup_linux.sh`，从 `third_party/skydds/packages/` 解压到 `third_party/skydds/sdk/`。
3. `source scripts/skydds/env.sh` 设置 `SKY_DDS` / `DDS_ROOT` / `ACE_ROOT` / `TAO_ROOT` / `LD_LIBRARY_PATH`。
4. `CGO_ENABLED=1 go build -tags skydds` 编译服务。

## 3. 使用方式（仓库根目录执行）

### 3.1 构建并导出 Base

```bash
./deploy/docker/build-and-save-base-bookworm.sh
```

### 3.2 构建并导出 SkyDDS Runtime（主线）

```bash
./deploy/docker/build-and-save-skydds-runtime-bookworm.sh
```

### 3.3 导入离线归档

```bash
./deploy/docker/load-base-bookworm.sh
./deploy/docker/load-skydds-runtime-bookworm.sh
```

## 4. 前置条件与边界

- 构建 SkyDDS runtime 前，`third_party/skydds/packages/` 中必须放入 SkyDDS 安装包（支持 `*.tar.gz` / `*.tgz` / `*.tar.xz` / `*.zip`）。
- 本仓库脚本只提供构建与归档入口；是否能在当前环境真实 `docker build` 取决于 Docker daemon、网络和安装包可用性。
