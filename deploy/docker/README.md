# Docker / Offline Images

当前 `deploy/docker/` 提供两个 Bookworm 入口：

1. **离线基础镜像（不含业务程序）**
2. **SkyDDS 服务镜像（构建阶段自动解压 `third_party/skydds/packages/` 并编译 `-tags skydds`）**

不再维护 Kali 镜像、debug 镜像、runtime 镜像或其他历史业务镜像入口。

---

## 1) 离线基础镜像（Base）

- Dockerfile: `deploy/docker/base-bookworm/Dockerfile`
- Build&Save: `deploy/docker/build-and-save-base-bookworm.sh`
- Load: `deploy/docker/load-base-bookworm.sh`
- 默认镜像标签: `forward-stub-base:bookworm`
- 默认归档: `deploy/images/forward-stub-base-bookworm.tar.gz`

构建：

```bash
./deploy/docker/build-and-save-base-bookworm.sh
```

导入：

```bash
./deploy/docker/load-base-bookworm.sh
```

---

## 2) SkyDDS 服务镜像（Bookworm）

### 2.1 目录约束（按仓库真实结构）

- 安装包目录（压缩包）：`third_party/skydds/packages/`
- SDK 解压目录：`third_party/skydds/sdk/`

SkyDDS 服务镜像的 Dockerfile 会先 `COPY . .`，再在容器内执行：

1. 从 `third_party/skydds/packages/` 读取压缩包；
2. 解压到 `third_party/skydds/sdk/`；
3. 设置 `SKY_DDS` / `DDS_ROOT` / `ACE_ROOT` / `TAO_ROOT` / `LD_LIBRARY_PATH`；
4. `CGO_ENABLED=1 go build -tags skydds` 生成服务二进制。

### 2.2 入口文件

- Dockerfile: `deploy/docker/skydds-bookworm/Dockerfile`
- Build&Save: `deploy/docker/build-and-save-skydds-bookworm.sh`
- Load: `deploy/docker/load-skydds-bookworm.sh`
- 默认镜像标签: `forward-stub:skydds-bookworm`
- 默认归档: `deploy/images/forward-stub-skydds-bookworm.tar.gz`

### 2.3 构建前准备

把 SkyDDS Linux 安装包放入：

```text
third_party/skydds/packages/
```

支持后缀：`*.tar.gz` / `*.tgz` / `*.tar.xz` / `*.zip`。

### 2.4 Build And Save

```bash
./deploy/docker/build-and-save-skydds-bookworm.sh
```

脚本会执行：

1. 检查 `third_party/skydds/packages/` 里至少有一个安装包；
2. `docker build`（context 为仓库根目录）；
3. `docker save`；
4. `gzip` 输出 `deploy/images/forward-stub-skydds-bookworm.tar.gz`。

### 2.5 Load

```bash
./deploy/docker/load-skydds-bookworm.sh
```

等价逻辑：

```bash
gunzip -c deploy/images/forward-stub-skydds-bookworm.tar.gz | docker load
```

---

## 3) 关于 `deploy/images/forward-stub-base-bookworm.tar.gz`

- 该文件是离线部署必需文件，必须保留。
- 该文件继续通过 Git LFS 管理（见仓库 `.gitattributes`）。
- **不要**把 `deploy/images/*.tar.gz` 加入 `.gitignore`。
