# SkyDDS 接入说明（dds_skydds）

## 1. 设计边界

- 本仓库把 SkyDDS 作为**字节消息桥接协议**接入，保持主链路不变：`receiver -> selector -> task -> pipeline -> sender`。
- 第一版仅支持 `message_model=octet`（对应 PDF 中 `Satellite::OctetMsg`）。
- `BatchOctetMsg` 先不实现；`batch_num/batch_size/batch_delay` 字段保留但校验为不支持。
- DDS 发现、QoS、传输协议、日志由 `dcps_config_file` 所指定的 SkyDDS 配置文件控制。

## 2. 目录布局约定（固定）

- 安装包放置目录：`third_party/skydds/packages/`
- 解压后 SDK 目录：`third_party/skydds/sdk/`

> 不要把大安装包提交到 Git。建议仅保留目录占位。

## 3. 与 PDF 的对应关系

读取文档：`docs/SkyDDS应用开发BatchOctetMsg快速指南（C++）.pdf`。

实现依据：

- 使用 `DomainParticipant / Topic / Publisher / DataWriter` 发送 OctetMsg。
- 使用 `DomainParticipant / Topic / Subscriber / DataReader` 接收 OctetMsg。
- 使用 `-DCPSConfigFile` 传入 SkyDDS 配置文件。
- 保持 C++ SDK 调用在 C ABI 后面，Go 只通过 cgo 调 C ABI。
- receiver 采用 C++ listener 入队 + Go 轮询拉取模型，避免直接从回调线程调用 Go。

## 4. 构建与运行环境变量

先执行：

```bash
source scripts/skydds/env.sh
```

`env.sh` 会导出：

- `SKY_DDS`
- `DDS_ROOT`
- `ACE_ROOT`
- `TAO_ROOT`
- `LD_LIBRARY_PATH`

## 5. 构建方式

SkyDDS 版本需要开启 cgo + build tag：

```bash
CGO_ENABLED=1 go build -tags skydds -o bin/forward-stub .
```

无 SkyDDS SDK 时，默认构建（不加 tag）仍可使用 UDP/TCP/Kafka/SFTP。

## 6. 运行方式

```bash
./bin/forward-stub \
  -system-config ./configs/minimal.system.example.json \
  -business-config ./configs/skydds.business.example.json
```

## 7. 测试脚本

- `scripts/skydds/setup_linux.sh`：检查并解压安装包到 `third_party/skydds/sdk/`
- `scripts/skydds/env.sh`：导出编译/运行环境变量
- `scripts/skydds/test_sender.sh`：验证 sender 初始化和发送链路（依赖本地 SDK）
- `scripts/skydds/test_receiver.sh`：验证 receiver 初始化和接收链路（依赖本地 SDK）
- `scripts/skydds/test_loop.sh`：最小闭环（receiver -> task -> sender）

## 8. 已知限制

- 当前只支持 `OctetMsg`，不支持 `BatchOctetMsg`。
- C++ wrapper 依赖 SDK 中 `SatelliteTypeSupportImpl.h / libSatelliteCommon`。
- 需要用户本地将 SkyDDS Linux 安装包放到 `third_party/skydds/packages/` 并解压后执行。
