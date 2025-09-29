# Micran IO Workflow Makefile
# Supports cross-compilation for ARM64 (real MCU) and native x64 (simulation)

# Directories
BIN_DIR := bin
OUT_DIR := out
CMD_DIR := cmd
SIM_DIR := sim_rtos
LINUX_TTY_DIR := sim_linux_tty

# Binaries
HOST_DRIVER := $(BIN_DIR)/host-driver
SIM_RTOS := $(BIN_DIR)/sim_rtos
SIM_LINUX_TTY := $(BIN_DIR)/sim_linux_tty

# Go build flags
GO_BUILD_FLAGS := 
GO_ARM64_FLAGS := GOOS=linux GOARCH=arm64
GO_X64_FLAGS := GOOS=linux GOARCH=amd64

# Optional verbose flag (set V=1 to enable verbose output)
ifdef V
  GO_BUILD_FLAGS := -v
endif

# Deployment configuration (read from .env file first, then environment variables)
-include .env

TARGET_HOST ?= $(DEPLOY_HOST)
TARGET_PATH ?= $(DEPLOY_PATH)
TARGET_PASS ?= $(DEPLOY_PASS)

# Default target
.PHONY: all
all: native

# Native x64 build for simulation testing
.PHONY: native
native: $(HOST_DRIVER)-native $(SIM_RTOS)-native $(SIM_LINUX_TTY)-native

# ARM64 cross-compilation for real MCU testing
.PHONY: arm64
arm64: $(HOST_DRIVER)-arm64 $(SIM_RTOS)-arm64 $(SIM_LINUX_TTY)-arm64

# Build host-driver for native x64
$(HOST_DRIVER)-native:
	@mkdir -p $(BIN_DIR)
	$(GO_X64_FLAGS) go build $(GO_BUILD_FLAGS) -o $@ ./$(CMD_DIR)/host-driver
	@ln -sf $(notdir $@) $(HOST_DRIVER) 2>/dev/null || true

# Build sim_rtos for native x64
$(SIM_RTOS)-native:
	@mkdir -p $(BIN_DIR)
	$(GO_X64_FLAGS) go build $(GO_BUILD_FLAGS) -o $@ ./$(SIM_DIR)
	@ln -sf $(notdir $@) $(SIM_RTOS) 2>/dev/null || true

# Build sim_linux_tty for native x64
$(SIM_LINUX_TTY)-native:
	@mkdir -p $(BIN_DIR)
	$(GO_X64_FLAGS) go build $(GO_BUILD_FLAGS) -o $@ ./$(LINUX_TTY_DIR)
	@ln -sf $(notdir $@) $(SIM_LINUX_TTY) 2>/dev/null || true

# Build host-driver for ARM64
$(HOST_DRIVER)-arm64:
	@mkdir -p $(BIN_DIR)
	$(GO_ARM64_FLAGS) go build $(GO_BUILD_FLAGS) -o $@ ./$(CMD_DIR)/host-driver

# Build sim_rtos for ARM64
$(SIM_RTOS)-arm64:
	@mkdir -p $(BIN_DIR)
	$(GO_ARM64_FLAGS) go build $(GO_BUILD_FLAGS) -o $@ ./$(SIM_DIR)

# Build sim_linux_tty for ARM64
$(SIM_LINUX_TTY)-arm64:
	@mkdir -p $(BIN_DIR)
	$(GO_ARM64_FLAGS) go build $(GO_BUILD_FLAGS) -o $@ ./$(LINUX_TTY_DIR)

# Build both architectures
.PHONY: both
both: native arm64

# Deploy ARM64 binaries to target (requires environment variables)
.PHONY: deploy
deploy: arm64
	@if [ -z "$(TARGET_HOST)" ] || [ -z "$(TARGET_PATH)" ]; then \
		echo "Error: Deployment requires environment variables:"; \
		echo "  DEPLOY_HOST - Target host (e.g., root@192.168.7.2)"; \
		echo "  DEPLOY_PATH - Target path (e.g., /root)"; \
		echo ""; \
		echo "Optional variable:"; \
		echo "  DEPLOY_PASS - SSH password (if using password authentication)"; \
		echo ""; \
		echo "Usage examples:"; \
		echo "  DEPLOY_HOST=root@192.168.7.2 DEPLOY_PATH=/root make deploy"; \
		echo "  DEPLOY_HOST=root@192.168.7.2 DEPLOY_PATH=/root DEPLOY_PASS=mypassword make deploy"; \
		exit 1; \
	fi
	@echo "Deploying ARM64 binaries to $(TARGET_HOST):$(TARGET_PATH)/"
	@if [ -n "$(TARGET_PASS)" ]; then \
		sshpass -p '$(TARGET_PASS)' scp $(HOST_DRIVER)-arm64 $(TARGET_HOST):$(TARGET_PATH)/host-driver; \
		sshpass -p '$(TARGET_PASS)' scp $(SIM_RTOS)-arm64 $(TARGET_HOST):$(TARGET_PATH)/sim_rtos; \
		sshpass -p '$(TARGET_PASS)' scp $(SIM_LINUX_TTY)-arm64 $(TARGET_HOST):$(TARGET_PATH)/sim_linux_tty; \
	else \
		scp $(HOST_DRIVER)-arm64 $(TARGET_HOST):$(TARGET_PATH)/host-driver; \
		scp $(SIM_RTOS)-arm64 $(TARGET_HOST):$(TARGET_PATH)/sim_rtos; \
		scp $(SIM_LINUX_TTY)-arm64 $(TARGET_HOST):$(TARGET_PATH)/sim_linux_tty; \
	fi
	@echo "Deployment complete."


# Clean build artifacts
.PHONY: clean
clean:
	rm -rf $(BIN_DIR)/*
	rm -rf $(OUT_DIR)/*

# Install dependencies and setup environment
.PHONY: setup
setup:
	@echo "Setting up Go environment for China network..."
	go env -w GOPROXY=https://goproxy.cn,direct
	go env -w GOSUMDB=off
	@echo "Environment setup complete"

# Quick test with simulation (native build)
.PHONY: test-sim
test-sim: native
	@echo "Running simulation test..."
	./run_sim.sh

# Quick test with real RTOS (ARM64 build)
.PHONY: test-real
test-real: arm64
	@echo "Running real RTOS test (requires ARM64 binaries)..."
	./run_real.sh

# Quick test with Linux bash shell (native build)
.PHONY: test-linux
test-linux: native
	@echo "Running Linux bash shell test (requires native build)..."
	./run_linux_tty.sh

# Show help
.PHONY: help
help:
	@echo "Micran IO Workflow Makefile"
	@echo ""
	@echo "Targets:"
	@echo "  all/native    - Build native x64 binaries for simulation testing"
	@echo "  arm64         - Cross-compile ARM64 binaries for real MCU testing"
	@echo "  both          - Build both native and ARM64 binaries"
	@echo "  deploy        - Deploy ARM64 binaries to target (requires env vars)"
	@echo "  clean         - Remove all build artifacts"
	@echo "  setup         - Setup Go environment for China network"
	@echo "  test-sim      - Run simulation test (requires native build)"
	@echo "  test-real     - Run real RTOS test (requires ARM64 build)"
	@echo "  test-linux    - Run Linux bash shell test (requires native build)"
	@echo "  help          - Show this help message"
	@echo ""
	@echo "Usage examples:"
	@echo "  make native           # Build for x64 host testing (silent)"
	@echo "  make native V=1       # Build for x64 host testing (verbose)"
	@echo "  make arm64            # Build for ARM64 MCU deployment (silent)"
	@echo "  make arm64 V=1        # Build for ARM64 MCU deployment (verbose)"
	@echo "  make both             # Build both architectures (silent)"
	@echo "  make test-sim         # Build and run simulation test"
	@echo "  make deploy           # Deploy ARM64 binaries (requires env vars)"
	@echo ""
	@echo "Deployment configuration:"
	@echo "  The Makefile reads from .env file first, then environment variables"
	@echo "  Copy .env.example to .env and update with your values"
	@echo "  Variables:"
	@echo "    DEPLOY_HOST - Target host (e.g., root@192.168.7.2)"
	@echo "    DEPLOY_PATH - Target path (e.g., /root)"
	@echo "    DEPLOY_PASS - SSH password (if using password authentication)"
	@echo ""
	@echo "Output binaries:"
	@echo "  bin/host-driver-native   - x64 host driver"
	@echo "  bin/sim_rtos-native      - x64 simulated RTOS"
	@echo "  bin/sim_linux_tty-native - x64 Linux bash shell PTY"
	@echo "  bin/host-driver-arm64    - ARM64 host driver"
	@echo "  bin/sim_rtos-arm64       - ARM64 simulated RTOS"
	@echo "  bin/sim_linux_tty-arm64  - ARM64 Linux bash shell PTY"
	@echo "  bin/host-driver          - Symlink to native version"
	@echo "  bin/sim_rtos             - Symlink to native version"
	@echo "  bin/sim_linux_tty        - Symlink to native version"
