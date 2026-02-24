APP_NAME ?= forword-stub
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test vet package package-all clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/$(APP_NAME) .

test:
	go test ./...

vet:
	go vet ./...

package:
	APP_NAME=$(APP_NAME) VERSION=$(VERSION) TARGETS="linux/amd64" ./scripts/package.sh

package-all:
	APP_NAME=$(APP_NAME) VERSION=$(VERSION) TARGETS="linux/amd64 linux/arm64 windows/amd64" ./scripts/package.sh

clean:
	rm -rf bin dist
