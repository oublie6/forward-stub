# SkyDDS Docker Images

本目录只补充 SkyDDS 相关镜像、`build + save/load` 脚本与镜像归档目录，不替换根目录 `Dockerfile` 的通用非 SkyDDS 构建路径，也不修改 SkyDDS sender/receiver、`BatchOctetMsg`、SFTP/file-stream stage 或 mock 测试主逻辑。

## 1. 为什么 SkyDDS 主线不再使用 Alpine

- SkyDDS 接入依赖 Go + cgo + C++ + 动态库，主线运行时需要沿用 glibc 兼容链路。
- Alpine 默认使用 musl；SkyDDS 及其依赖库在 glibc 环境下更稳妥，联调与排障成本更低。
- 根目录 `Dockerfile` 仍可服务现有非 SkyDDS 场景；SkyDDS 主线镜像单独维护在 `deploy/docker/`。

## 2. 为什么主线选择 Bookworm / glibc

- `golang:1.25-bookworm` 适合作为 SkyDDS base/builder，便于同时提供 Go、cgo、gcc/g++、make 和常见诊断工具。
- `debian:bookworm-slim` 适合作为运行镜像，保留 glibc 运行时与 SkyDDS 所需动态库环境，同时避免塞入一整套编译工具。
- 这条链路与 `scripts/skydds/env.sh`、`scripts/skydds/setup_linux.sh` 的目录约定一致，便于本地与容器内对齐。

## 3. 为什么 Kali 只作为 debug 镜像

- Kali 面向临时排障、安全工具和网络诊断，不适合作为常驻主服务基础镜像。
- 主服务镜像强调可控、稳定、最小化，因此主线采用 Debian Bookworm。
- `deploy/docker/skydds-debug-kali/Dockerfile` 仅提供临时诊断入口，不应替代 runtime 镜像。

## 4. SkyDDS 目录约定

- 安装包放置目录：`third_party/skydds/packages/`
- SDK 解压目录：`third_party/skydds/sdk/`

所有 `build-and-save` 脚本都会同时检查这两个目录。若目录不存在，或只有 `.gitkeep` 没有真实内容，脚本会直接失败，不会假装构建成功。

## 5. 镜像体系

### `skydds-base`

- Dockerfile：`deploy/docker/skydds-base/Dockerfile`
- 基础镜像：`golang:1.25-bookworm`
- 用途：Go + cgo + C++ + SkyDDS SDK 的 builder/base 镜像
- 至少安装：`gcc g++ make perl bash tcpdump ca-certificates file tar xz-utils gzip libstdc++6`
- 保留目录：
  - `/workspace/third_party/skydds/packages`
  - `/workspace/third_party/skydds/sdk`

### `skydds-runtime`

- Dockerfile：`deploy/docker/skydds-runtime/Dockerfile`
- 运行镜像：`debian:bookworm-slim`
- 用途：运行带 `-tags skydds` 构建出的 `forward-stub`
- 特点：仅保留应用、SkyDDS 运行所需动态库环境与 `tcpdump`，不附带整套编译工具

### `skydds-debug-kali`

- Dockerfile：`deploy/docker/skydds-debug-kali/Dockerfile`
- 基础镜像：`kalilinux/kali-rolling`
- 用途：临时排障、安全工具、网络抓包与问题定位
- 限制：不是主服务镜像

## 6. 镜像产物目录

- 镜像归档目录：`deploy/images/`
- 约定产物名：
  - `deploy/images/forward-stub-skydds-base-bookworm.tar.gz`
  - `deploy/images/forward-stub-skydds-runtime-bookworm.tar.gz`
  - `deploy/images/forward-stub-skydds-debug-kali.tar.gz`

这些 `tar.gz` 产物用于：

- 内网分发
- `docker load`
- Git 仓库存档

如果后续确认镜像产物过大，可再评估是否切换为 Git LFS；本次仍按普通仓库文件方式组织，不新增忽略 `deploy/images/*.tar.gz` 的规则。

## 7. Build And Save

在仓库根目录执行：

```bash
./deploy/docker/build-and-save-skydds-base.sh
./deploy/docker/build-and-save-skydds-runtime.sh
./deploy/docker/build-and-save-skydds-debug.sh
```

脚本会执行：

1. `docker build`
2. `docker save`
3. `gzip` 压缩
4. 将归档输出到 `deploy/images/`

脚本支持的环境变量：

- `DOCKER_BIN`
- `IMAGE_TAG`
- `OUTPUT_FILE`
- `BASE_IMAGE_TAG`（仅 `build-and-save-skydds-runtime.sh` 使用）

示例：

```bash
IMAGE_TAG=forward-stub-skydds-base:bookworm \
OUTPUT_FILE=deploy/images/forward-stub-skydds-base-bookworm.tar.gz \
./deploy/docker/build-and-save-skydds-base.sh
```

```bash
BASE_IMAGE_TAG=forward-stub-skydds-base:bookworm \
IMAGE_TAG=forward-stub-skydds-runtime:bookworm \
OUTPUT_FILE=deploy/images/forward-stub-skydds-runtime-bookworm.tar.gz \
./deploy/docker/build-and-save-skydds-runtime.sh
```

```bash
IMAGE_TAG=forward-stub-skydds-debug:kali \
OUTPUT_FILE=deploy/images/forward-stub-skydds-debug-kali.tar.gz \
./deploy/docker/build-and-save-skydds-debug.sh
```

说明：

- `build-and-save-skydds-runtime.sh` 默认依赖本地已存在的 `forward-stub-skydds-base:bookworm`。
- 若 base 镜像不存在，runtime 脚本会明确报错，要求先构建并加载 base。
- 旧入口 `build-skydds-base.sh`、`build-skydds-runtime.sh`、`build-skydds-debug.sh` 仍保留，但已改为兼容包装，实际会转发到新的 `build-and-save-*` 脚本。

## 8. Load

在仓库根目录执行：

```bash
./deploy/docker/load-skydds-base.sh
./deploy/docker/load-skydds-runtime.sh
./deploy/docker/load-skydds-debug.sh
```

这些脚本会使用等价于以下命令的方式将归档重新导入本地 Docker daemon：

```bash
gunzip -c deploy/images/forward-stub-skydds-base-bookworm.tar.gz | docker load
gunzip -c deploy/images/forward-stub-skydds-runtime-bookworm.tar.gz | docker load
gunzip -c deploy/images/forward-stub-skydds-debug-kali.tar.gz | docker load
```

`load` 脚本支持：

- `DOCKER_BIN`
- `OUTPUT_FILE`

## 9. tcpdump 权限注意事项

- 容器内使用 `tcpdump` 通常需要额外能力，例如 `--cap-add NET_ADMIN --cap-add NET_RAW`。
- 某些环境还需要 `--network host` 或显式挂载目标网络命名空间。
- 若平台策略禁止提升能力，容器内即使安装了 `tcpdump` 也可能无法抓包。

示例：

```bash
docker run --rm -it \
  --cap-add NET_ADMIN \
  --cap-add NET_RAW \
  forward-stub-skydds-debug:kali
```

## 10. 实际验证与未验证边界

已实际验证：

- 仓库目录约定为 `third_party/skydds/packages/` 与 `third_party/skydds/sdk/`
- 当前仓库中这两个目录都只有 `.gitkeep`，因此真实镜像构建当前必然失败
- 新脚本的入口路径、输出路径、前置检查逻辑与 shell 语法

尚未实际验证：

- 真实 `docker build`
- 真实 `docker save` 后生成的 `deploy/images/*.tar.gz`
- 真实 `docker load`
- SkyDDS SDK 动态库联调
- 容器内 `tcpdump` 抓包权限

未验证项不是伪造成果；这里只提供真实入口，待你补入 SkyDDS 安装包与 SDK 后再执行。
