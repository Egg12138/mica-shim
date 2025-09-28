# Micran IO Workflow Makefile
# Supports cross-compilation for ARM64 (real MCU) and native x64 (simulation)

# Directories
BIN_DIR := bin
OUT_DIR := out
CMD_DIR := cmd
SIM_DIR := sim_rtos

# Binaries
HOST_DRIVER := $(BIN_DIR)/host-driver
SIM_RTOS := $(BIN_DIR)/sim_rtos

# Go build flags
GO_BUILD_FLAGS := 
GO_ARM64_FLAGS := GOOS=linux GOARCH=arm64
GO_X64_FLAGS := GOOS=linux GOARCH=amd64

# Optional verbose flag (set V=1 to enable verbose output)
ifdef V
  GO_BUILD_FLAGS := -v
endif

# Default target
.PHONY: all
all: native

# Native x64 build for simulation testing
.PHONY: native
native: $(HOST_DRIVER)-native $(SIM_RTOS)-native

# ARM64 cross-compilation for real MCU testing
.PHONY: arm64
arm64: $(HOST_DRIVER)-arm64 $(SIM_RTOS)-arm64

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

# Build host-driver for ARM64
$(HOST_DRIVER)-arm64:
	@mkdir -p $(BIN_DIR)
	$(GO_ARM64_FLAGS) go build $(GO_BUILD_FLAGS) -o $@ ./$(CMD_DIR)/host-driver

# Build sim_rtos for ARM64
$(SIM_RTOS)-arm64:
	@mkdir -p $(BIN_DIR)
	$(GO_ARM64_FLAGS) go build $(GO_BUILD_FLAGS) -o $@ ./$(SIM_DIR)

# Build both architectures
.PHONY: both
both: native arm64

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

# Show help
.PHONY: help
help:
	@echo "Micran IO Workflow Makefile"
	@echo ""
	@echo "Targets:"
	@echo "  all/native    - Build native x64 binaries for simulation testing"
	@echo "  arm64         - Cross-compile ARM64 binaries for real MCU testing"
	@echo "  both          - Build both native and ARM64 binaries"
	@echo "  clean         - Remove all build artifacts"
	@echo "  setup         - Setup Go environment for China network"
	@echo "  test-sim      - Run simulation test (requires native build)"
	@echo "  test-real     - Run real RTOS test (requires ARM64 build)"
	@echo "  help          - Show this help message"
	@echo ""
	@echo "Usage examples:"
	@echo "  make native           # Build for x64 host testing (silent)"
	@echo "  make native V=1       # Build for x64 host testing (verbose)"
	@echo "  make arm64            # Build for ARM64 MCU deployment (silent)"
	@echo "  make arm64 V=1        # Build for ARM64 MCU deployment (verbose)"
	@echo "  make both             # Build both architectures (silent)"
	@echo "  make test-sim         # Build and run simulation test"
	@echo ""
	@echo "Output binaries:"
	@echo "  bin/host-driver-native   - x64 host driver"
	@echo "  bin/sim_rtos-native      - x64 simulated RTOS"
	@echo "  bin/host-driver-arm64    - ARM64 host driver"
	@echo "  bin/sim_rtos-arm64       - ARM64 simulated RTOS"
	@echo "  bin/host-driver          - Symlink to native version"
	@echo "  bin/sim_rtos             - Symlink to native version"
