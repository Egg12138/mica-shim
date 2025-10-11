TARGET_HOST := root@192.168.7.2
TARGET_PATH := /root
TARGET_PASS := $(TEST_SSH_PASSWD)
BINARY_NAME := xl_vcpulist_parser

.PHONY: all build deploy clean test

all: build


build:
	@echo "Building for host platform..."
	@go build -o $(BINARY_NAME) main.go
	@echo "Build complete: $(BINARY_NAME)"

build-arm64:
	@echo "Building for linux/arm64..."
	@GOOS=linux GOARCH=arm64 go build -o $(BINARY_NAME) main.go
	@echo "Build complete: $(BINARY_NAME)"

deploy: build-arm64
	@echo "Deploying $(BINARY_NAME) and sample data to $(TARGET_HOST):$(TARGET_PATH)/"
	@sshpass -p '$(TARGET_PASS)' scp $(BINARY_NAME) vcpu-list-example.txt $(TARGET_HOST):$(TARGET_PATH)/
	@echo "Deployment complete."
	@echo ""
	@echo "Testing on remote machine..."
	@sshpass -p '$(TARGET_PASS)' ssh $(TARGET_HOST) "cd $(TARGET_PATH) && ./$(BINARY_NAME) --help || true"
	@echo ""

test: build
	@echo "Running test with sample data..."
	@./$(BINARY_NAME) -sample -verbose

test-remote: deploy
	@echo "Running remote test..."
	@sshpass -p '$(TARGET_PASS)' ssh $(TARGET_HOST) "cd $(TARGET_PATH) && ./$(BINARY_NAME) -sample"
	@echo ""
	@echo "Running remote verbose test..."
	@sshpass -p '$(TARGET_PASS)' ssh $(TARGET_HOST) "cd $(TARGET_PATH) && ./$(BINARY_NAME) -sample -verbose"

clean:
	@echo "Cleaning up..."
	@rm -f $(BINARY_NAME)
	@echo "Clean complete."
