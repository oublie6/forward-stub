# syntax=docker/dockerfile:1.7

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app

# 在容器外先完成编译，再将二进制打进镜像。
# 示例：docker build --build-arg BIN_PATH=bin/forword-stub -t forword-stub:latest .
ARG BIN_PATH=bin/forword-stub
COPY --chown=nonroot:nonroot ${BIN_PATH} /app/forword-stub
COPY --chown=nonroot:nonroot configs/example.json /app/configs/example.json

USER nonroot:nonroot
ENTRYPOINT ["/app/forword-stub"]
CMD ["-config", "/app/configs/example.json", "-log-level", "info"]
