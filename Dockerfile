# syntax=docker/dockerfile:1.7

# 使用 distroless nonroot 作为最小运行时镜像：
# - 减少攻击面；
# - 无 shell 与包管理器，适合生产环境运行 Go 静态二进制。
FROM gcr.io/distroless/static-debian12:nonroot

# 应用运行目录。
WORKDIR /app

# BINARY_PATH 允许外部注入预构建二进制路径，默认使用 dist/linux 产物。
ARG BINARY_PATH=dist/linux/forward-stub

# 拷贝服务二进制并设置 nonroot 所有权。
COPY --chown=nonroot:nonroot ${BINARY_PATH} /app/forward-stub
# 拷贝默认示例配置，便于容器开箱即跑（生产可挂载覆盖）。
COPY --chown=nonroot:nonroot configs/example.json /app/configs/example.json

# 以非 root 用户运行，满足容器安全基线。
USER nonroot:nonroot

# 固定入口为服务进程。
ENTRYPOINT ["/app/forward-stub"]
# 默认使用容器内配置文件并启用 info 级日志。
CMD ["-config", "/app/configs/example.json", "-log-level", "info"]
