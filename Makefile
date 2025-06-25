.PHONY: all build-prod build build-arm64 build-prod-arm64 test-prod test clean clean-all help run-debug run-prod containerd-client build-containerd-client install install-arm64 install-prod-arm64 mock-micad mock-micad-py mock-micad-py-quiet test-sched bench-sched

SHIM_NAME := org.openeuler.mica.v1
# containerd shim v2 命名规约转换
# Runtime name: io.containerd.runc.v2 → Binary: containerd-shim-runc-v2
# Runtime name: org.openeuler.micashim.v2 → Binary: containerd-shim-micashim-v2
# isulad shimv2 Runtime name: io.containerd.{runtime}.{version} -> Binary: containerd-shim-{runtime}-{version}
# 规则: 
# 1. 移除域名前缀部分 (io.containerd. 或 org.openeuler. 等)
# 2. 取最后两个部分作为 {runtime}.{version}
# 3. 转换为 containerd-shim-{runtime}-{version}
SHIM_PARTS := $(subst ., ,$(SHIM_NAME))
SHIM_PARTS_COUNT := $(words $(SHIM_PARTS))
RUNTIME_NAME := $(word $(shell echo $(SHIM_PARTS_COUNT) - 1 | bc),$(SHIM_PARTS))
RUNTIME_VERSION := $(lastword $(SHIM_PARTS))

BUILD_DIRS := builds/
SHIM_DIR := /usr/local/bin/
BINNAME := containerd-shim-$(RUNTIME_NAME)-$(RUNTIME_VERSION)
BIN := $(BUILD_DIRS)$(BINNAME)
BIN_PROD := $(BIN)
BIN_ARM64 := $(BUILD_DIRS)$(BIN)-arm64
BIN_PROD_ARM64 := $(BUILD_DIRS)$(BIN_PROD)-arm64

DEV_BUILD_FLAGS := -ldflags "-X 'main.ShimName=${SHIM_NAME}'"
CROSS_DEV_BUILD_FLAGS := $(DEV_BUILD_FLAGS) -a -installsuffix cgo
RELEASE_BUILD_FLAGS := -ldflags "-s -w -X 'main.ShimName=${SHIM_NAME}'"
CROSS_RELEASE_BUILD_FLAGS := $(RELEASE_BUILD_FLAGS) -a -installsuffix cgo

all: build

build-prod:
	@echo "🏭 Building production binary..."
	go build ${RELEASE_BUILD_FLAGS} -o ${BIN_PROD} ./cmd

run-prod: build-prod
	@echo "🏭 Running in production mode..."
	./${BIN_PROD}

test-prod:
	@echo "🏭 Testing in production mode..."
	go test -v ./...

build:
	@echo "🐛 Building debug binary..."
	go build -tags debug ${DEV_BUILD_FLAGS} -o ${BIN} ./cmd

# Temp target for amd64 to build arm64 binary
build-arm64:
	@echo "🔄 Cross-compiling debug binary for ARM64..."
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags debug ${CROSS_DEV_BUILD_FLAGS} -o ${BIN_ARM64} ./cmd

build-prod-arm64:
	@echo "🔄 Cross-compiling production binary for ARM64..."
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ${CROSS_DEV_BUILD_FLAGS} -o ${BIN_PROD_ARM64} ./cmd

run: build
	@echo "🐛 Running in debug mode..."
	./${BIN}

test-debug:
	@echo "🐛 Testing in debug mode..."
	go test -tags debug -v ./...

test-socket:
	@echo "🧪 Testing socket communication in debug mode..."
	cd tests && go run -tags debug test_socket_communication.go

test-socket-prod:
	@echo "🧪 Testing socket communication in production mode..."
	cd tests && go run test_socket_communication.go

test-sched:
	@echo "🧪 Testing CPU scheduler in debug mode..."
	cd libmica && go test -tags debug -v -run "Test.*Sched|Test.*Queue|Test.*CPU|Test.*Concurrent|Test.*Priority|Test.*Comprehensive"

bench-sched:
	@echo "📊 Benchmarking CPU scheduler performance..."
	cd libmica && go test -bench=BenchmarkSched -benchmem -v

containerd-client: build-containerd-client
	@echo "🐳 Testing containerd client integration..."
	cd tests/containerd_client && sudo ./containerd_client

build-containerd-client:
	@echo "🐳 Building containerd client binary..."
	cd tests/containerd_client && go build -ldflags "-X 'main.customRuntimeName=${SHIM_NAME}'" -o containerd_client containerd_client.go

mock-micad:
	@echo "🎭 Building and running mock_micad..."
	cd tests/mock_micad && make && ./mock_micad

fmt:
	go fmt ./...

clean-all: clean
	@echo "🧹 Cleaning up all components including tests and simulations..."
	cd tests/mock_micad && make clean
	cd tests/containerd_client && rm -f containerd_client

clean:
	@echo "🧹 Cleaning up build artifacts..."
	rm -f ${BIN} ${BIN_PROD} ${BIN_ARM64} ${BIN_PROD_ARM64}

install-prod: build-prod
	@echo "🏭 Installing ${BIN_PROD} to ${SHIM_DIR}"
	sudo install -m 755 ${BIN_PROD} ${SHIM_DIR}${BINNAME}
	@echo "pass --runtime ${SHIM_NAME} to use it"

install: build
	@echo "🏭 Installing ${BIN} to ${SHIM_DIR} for debug"
	sudo install -m 755 ${BIN} ${SHIM_DIR}${BINNAME}
	@echo "md5sums:"
	@echo "Source:      $$(md5sum ${BIN})"
	@echo "Installed:   $$(md5sum ${SHIM_DIR}${BINNAME})"
	@echo "pass --runtime ${SHIM_NAME} to use it"

dev-setup:
	@echo "🔧 Setting up development environment..."
	@echo "1. Building debug binary..."
	@make build
	@echo "2. Starting mock_micad in background..."
	@cd tests/mock_micad && make && ./mock_micad &
	@echo "3. Waiting for mock_micad to start..."
	@sleep 1
	@echo "4. Running socket tests..."
	@make test-socket
	@make build-containerd-client
	@make install
	@echo "✅ Development setup complete!"


# Help
help:
	@echo "🚀 Mica Shim Build System"
	@echo ""
	@echo "Production Commands:"
	@echo "  make build-prod    - Build production binary"
	@echo "  make run-prod      - Run in production mode"
	@echo "  make test-prod     - Test in production mode"
	@echo ""
	@echo "Debug Commands:"
	@echo "  make build   - Build debug binary"
	@echo "  make run     - Run in debug mode"
	@echo "  make test    - Test in debug mode"
	@echo ""
	@echo "Cross-Compilation Commands:"
	@echo "  make build-arm64      - Cross-compile debug binary for ARM64"
	@echo "  make build-prod-arm64 - Cross-compile production binary for ARM64"
	@echo "  make install-arm64    - Install ARM64 debug binary"
	@echo "  make install-prod-arm64 - Install ARM64 production binary"
	@echo ""
	@echo "Testing & Simulations:"
	@echo "  make test-socket            - Test socket communication (debug)"
	@echo "  make test-socket-prod       - Test socket communication (prod)"
	@echo "  make test-sched             - Test CPU scheduler (debug)"
	@echo "  make bench-sched            - Benchmark CPU scheduler performance"
	@echo "  make containerd-client 		 - Test containerd client integration"
	@echo "  make build-containerd-client - Build containerd client binary"
	@echo "  make mock-micad             - Run mock micad server"
	@echo ""
	@echo "Utility Commands:"
	@echo "  make dev-setup     - Complete development setup"
	@echo "  make clean         - Clean build artifacts"
	@echo "  make help          - Show this help"
	@echo ""
	@echo "Containerd Shimv2 Tests:"
	@echo "	 In progress"