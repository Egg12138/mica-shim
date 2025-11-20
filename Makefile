.PHONY: all build-prod build build-arm64 build-prod-arm64 test-prod test clean clean-all help run-debug run-prod containerd-client build-containerd-client install install-nonroot install-arm64 install-prod-arm64 mock-micad mock-micad-py mock-micad-py-quiet test-sched bench-sched

SHIM_NAME := io.containerd.mica.v2
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
SHIM_DIR_NONROOT ?= $(HOME)/.local/bin
BINNAME := containerd-shim-$(RUNTIME_NAME)-$(RUNTIME_VERSION)
BIN := $(BUILD_DIRS)$(BINNAME)
BIN_PROD := $(BIN)
BIN_ARM64 := $(BUILD_DIRS)$(BIN)-arm64
BIN_PROD_ARM64 := $(BUILD_DIRS)$(BIN_PROD)-arm64

# Build mode configuration
# Use vendor mode by default, can be overridden with BUILD_MODE=module
BUILD_MODE ?= vendor

# Base build flags
DEV_BUILD_FLAGS := -ldflags "-X 'main.ShimName=${SHIM_NAME}'"
CROSS_DEV_BUILD_FLAGS := $(DEV_BUILD_FLAGS) -a -installsuffix cgo
RELEASE_BUILD_FLAGS := -ldflags "-s -w -X 'main.ShimName=${SHIM_NAME}'"
CROSS_RELEASE_BUILD_FLAGS := $(RELEASE_BUILD_FLAGS) -a -installsuffix cgo

# Vendor-specific flags
VENDOR_FLAGS := -mod=vendor
MODULE_FLAGS := -mod=mod

# Determine build flags based on mode
ifeq ($(BUILD_MODE),vendor)
	GO_BUILD_FLAGS := $(VENDOR_FLAGS)
	GO_TEST_FLAGS := $(VENDOR_FLAGS)
else ifeq ($(BUILD_MODE),module)
	GO_BUILD_FLAGS := $(MODULE_FLAGS)
	GO_TEST_FLAGS := $(MODULE_FLAGS)
else
	GO_BUILD_FLAGS := $(VENDOR_FLAGS)
	GO_TEST_FLAGS := $(VENDOR_FLAGS)
endif

-include .env
TARGET_HOST ?= $(DEPLOY_HOST)
TARGET_PATH ?= $(DEPLOY_PATH)
TARGET_PASS ?= $(DEPLOY_PASS)


all: build

build-prod:
	@echo "🏭 Building production binary (BUILD_MODE=${BUILD_MODE})..."
	go build ${GO_BUILD_FLAGS} ${RELEASE_BUILD_FLAGS} -o ${BIN_PROD} .

run-prod: build-prod
	@echo "🏭 Running in production mode..."
	./${BIN_PROD}

test-prod:
	@echo "🏭 Testing in production mode (BUILD_MODE=${BUILD_MODE})..."
	go test ${GO_TEST_FLAGS} -v ./...

build:
	@echo "🐛 Building debug binary (BUILD_MODE=${BUILD_MODE})..."
	go build ${GO_BUILD_FLAGS} -tags debug ${DEV_BUILD_FLAGS} -o ${BIN} .

# Temp target for amd64 to build arm64 binary
build-arm64:
	@echo "🔄 Cross-compiling debug binary for ARM64 (BUILD_MODE=${BUILD_MODE})..."
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ${GO_BUILD_FLAGS} -tags debug ${CROSS_DEV_BUILD_FLAGS} -o ${BIN_ARM64} .

build-prod-arm64:
	@echo "🔄 Cross-compiling production binary for ARM64 (BUILD_MODE=${BUILD_MODE})..."
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ${GO_BUILD_FLAGS} ${CROSS_RELEASE_BUILD_FLAGS} -o ${BIN_PROD_ARM64} .

run: build
	@echo "🐛 Running in debug mode..."
	./${BIN}


test-debug:
	@echo "🐛 Testing in debug mode (BUILD_MODE=${BUILD_MODE})..."
	go test ${GO_TEST_FLAGS} -tags debug -v ./...

test-socket:
	@echo "🧪 Testing socket communication in debug mode (BUILD_MODE=${BUILD_MODE})..."
	cd tests && go run ${GO_BUILD_FLAGS} -tags debug test_socket_communication.go

test-socket-prod:
	@echo "🧪 Testing socket communication in production mode (BUILD_MODE=${BUILD_MODE})..."
	cd tests && go run ${GO_BUILD_FLAGS} test_socket_communication.go

test-sched:
	@echo "🧪 Testing CPU scheduler in debug mode (BUILD_MODE=${BUILD_MODE})..."
	cd libmica && go test ${GO_TEST_FLAGS} -tags debug -v -run "Test.*Sched|Test.*Queue|Test.*CPU|Test.*Concurrent|Test.*Priority|Test.*Comprehensive"

bench-sched:
	@echo "📊 Benchmarking CPU scheduler performance (BUILD_MODE=${BUILD_MODE})..."
	cd libmica && go test ${GO_TEST_FLAGS} -bench=BenchmarkSched -benchmem -v

containerd-client: build-containerd-client
	@echo "🐳 Testing containerd client integration..."
	cd tests/containerd_client && sudo ./containerd_client

build-containerd-client:
	@echo "🐳 Building containerd client binary (BUILD_MODE=${BUILD_MODE})..."
	cd tests/containerd_client && go build ${GO_BUILD_FLAGS} -ldflags "-X 'main.customRuntimeName=${SHIM_NAME}'" -o containerd_client containerd_client.go

mock-micad:
	@echo "🎭 Building and running mock_micad... at ${BUILD_DIRS}mock_micad"
	cd tests/mock_micad && make run

fmt:
	go fmt ./...

clean-all: clean
	@echo "🧹 Cleaning up all components including tests and simulations..."
	cd tests/mock_micad && make clean
	cd tests/containerd_client && rm -f containerd_client
	rm -f ${BIN} ${BIN_PROD} ${BIN_ARM64} ${BIN_PROD_ARM64}

# Vendor-specific build targets
build-vendor:
	@echo "📦 Building with vendor dependencies..."
	@$(MAKE) build BUILD_MODE=vendor

build-prod-vendor:
	@echo "📦 Building production with vendor dependencies..."
	@$(MAKE) build-prod BUILD_MODE=vendor

build-arm64-vendor:
	@echo "📦 Cross-compiling ARM64 with vendor dependencies..."
	@$(MAKE) build-arm64 BUILD_MODE=vendor

build-prod-arm64-vendor:
	@echo "📦 Cross-compiling ARM64 production with vendor dependencies..."
	@$(MAKE) build-prod-arm64 BUILD_MODE=vendor

# Module-specific build targets
build-module:
	@echo "📚 Building with Go modules..."
	@$(MAKE) build BUILD_MODE=module

build-prod-module:
	@echo "📚 Building production with Go modules..."
	@$(MAKE) build-prod BUILD_MODE=module

build-arm64-module:
	@echo "📚 Cross-compiling ARM64 with Go modules..."
	@$(MAKE) build-arm64 BUILD_MODE=module

build-prod-arm64-module:
	@echo "📚 Cross-compiling ARM64 production with Go modules..."
	@$(MAKE) build-prod-arm64 BUILD_MODE=module

# Vendor management targets
vendor-update:
	@echo "📦 Updating vendor directory..."
	go mod vendor

vendor-verify:
	@echo "🔍 Verifying vendor directory..."
	go mod verify

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

install-nonroot: build
	@echo "🏠 Installing ${BIN} to ${SHIM_DIR_NONROOT} for non-root user"
	@mkdir -p ${SHIM_DIR_NONROOT}
	install -m 755 ${BIN} ${SHIM_DIR_NONROOT}/$(BINNAME)
	@echo "md5sums:"
	@echo "Source:      $$(md5sum ${BIN})"
	@echo "Installed:   $$(md5sum ${SHIM_DIR_NONROOT}/$(BINNAME))"
	@echo "pass --runtime ${SHIM_NAME} to use it"
	@echo "Make sure ${SHIM_DIR_NONROOT} is in your PATH"

.PHONY: remote
remote: build-arm64
	@if [ -z "${TARGET_HOST}" ] || [ -z "${TARGET_PATH}" ]; then \
		echo "Error: Deployment requires environment variables:"; \
		echo "  DEPLOY_HOST - Target host (e.g., root@192.168.7.2)"; \
		echo "  DEPLOY_PATH - Target path (e.g., /root)"; \
		echo ""; \
		echo "Optional variable:"; \
		echo "  DEPLOY_PASS - SSH password (if using password authentication)"; \
		echo ""; \
		echo "  DEPLOY_HOST=root@192.168.7.2 DEPLOY_PATH=/root make deploy"; \
		echo "Usage examples:"; \
		echo "  DEPLOY_HOST=root@192.168.7.2 DEPLOY_PATH=/root DEPLOY_PASS=mypassword make deploy"; \
		exit 1; \
	fi
	@echo "Deploying to ${TARGET_HOST}:${TARGET_PATH}/"
	@if [ -n "${TARGET_PASS}" ]; then \
		sshpass -p '${TARGET_PASS}' scp ${BIN_ARM64} ${TARGET_HOST}:${TARGET_PATH}/${BINNAME}; \
	else \
		scp ${BIN_ARM64} ${TARGET_HOST}:${TARGET_PATH}/${BINNAME}; \
	fi
	@echo "Deployment complete."



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

test-pty:
	@cd tests/pty
	@make all



# Help
help:
	@echo "🚀 Mica Shim Build System"
	@echo ""
	@echo "Build Mode Configuration:"
	@echo "  BUILD_MODE=vendor  - Use vendor directory (default)"
	@echo "  BUILD_MODE=module  - Use Go modules"
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
	@echo "Vendor-Specific Commands:"
	@echo "  make build-vendor          - Build debug with vendor deps"
	@echo "  make build-prod-vendor     - Build production with vendor deps"
	@echo "  make build-arm64-vendor    - Cross-compile ARM64 with vendor deps"
	@echo "  make build-prod-arm64-vendor - Cross-compile ARM64 production with vendor deps"
	@echo "  make vendor-update         - Update vendor directory"
	@echo "  make vendor-verify         - Verify vendor directory"
	@echo ""
	@echo "Module-Specific Commands:"
	@echo "  make build-module          - Build debug with Go modules"
	@echo "  make build-prod-module     - Build production with Go modules"
	@echo "  make build-arm64-module    - Cross-compile ARM64 with Go modules"
	@echo "  make build-prod-arm64-module - Cross-compile ARM64 production with Go modules"
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
	@echo "Installation:"
	@echo "  make install          - Install debug binary (requires sudo)"
	@echo "  make install-prod     - Install production binary (requires sudo)"
	@echo "  make install-nonroot  - Install debug binary to ~/.local/bin"
	@echo ""
	@echo "Utility Commands:"
	@echo "  make dev-setup     - Complete development setup"
	@echo "  make clean         - Clean build artifacts"
	@echo "  make help          - Show this help"
	@echo ""
	@echo "Containerd Shimv2 Tests:"
	@echo "	 In progress"
