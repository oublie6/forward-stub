# syntax=docker/dockerfile:1.7

FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/forword-stub .

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /out/forword-stub /app/forword-stub
COPY --chown=nonroot:nonroot configs/example.json /app/configs/example.json
USER nonroot:nonroot
ENTRYPOINT ["/app/forword-stub"]
CMD ["-config", "/app/configs/example.json", "-log-level", "info"]
