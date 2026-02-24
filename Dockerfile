# syntax=docker/dockerfile:1.7

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app

ARG BINARY_PATH=dist/linux/forword-stub
COPY --chown=nonroot:nonroot ${BINARY_PATH} /app/forword-stub
COPY --chown=nonroot:nonroot configs/example.json /app/configs/example.json

USER nonroot:nonroot
ENTRYPOINT ["/app/forword-stub"]
CMD ["-config", "/app/configs/example.json", "-log-level", "info"]
