BINARY=gofly
BUILD_DIR=build
GOFLAGS=-ldflags="-s -w" -trimpath
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.1.0")

.PHONY: all build test test-race clean docker docker-slim run lint

all: test build

build:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 go build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY) ./cmd/gofly/
	@echo "Built $(BUILD_DIR)/$(BINARY)"

test:
	go test -count=1 ./...

test-race:
	go test -race -count=1 ./...

test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

clean:
	rm -rf $(BUILD_DIR) coverage.out coverage.html

docker:
	docker build -t gofly:$(VERSION) -t gofly:latest .

docker-slim:
	docker build --build-arg=UPX=true -t gofly:$(VERSION)-slim -t gofly:latest .

run: build
	$(BUILD_DIR)/$(BINARY) -config config.json -debug

lint:
	go vet ./...

cross:
	GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY)-linux-amd64 ./cmd/gofly/
	GOOS=linux GOARCH=arm64 go build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY)-linux-arm64 ./cmd/gofly/
	GOOS=darwin GOARCH=amd64 go build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY)-darwin-amd64 ./cmd/gofly/
