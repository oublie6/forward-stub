APP_NAME ?= forword-stub
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build build-linux test vet package package-all docker-build clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/$(APP_NAME) .

build-linux:
	APP_NAME=$(APP_NAME) VERSION=$(VERSION) GOOS=linux GOARCH=amd64 OUT_DIR=dist/linux ./scripts/build-linux.sh

test:
	go test ./...

vet:
	go vet ./...

package:
	APP_NAME=$(APP_NAME) VERSION=$(VERSION) TARGETS="linux/amd64" ./scripts/package.sh

package-all:
	APP_NAME=$(APP_NAME) VERSION=$(VERSION) TARGETS="linux/amd64 linux/arm64 windows/amd64" ./scripts/package.sh

docker-build: build-linux
	docker build --build-arg BINARY_PATH=dist/linux/$(APP_NAME) -t $(APP_NAME):$(VERSION) .

clean:
	rm -rf bin dist
