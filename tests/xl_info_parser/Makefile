TARGET_HOST := root@192.168.7.2
TARGET_PATH := /root
TARGET_PASS := $(TEST_SSH_PASSWD)
BINARY_NAME := xl_info_parser

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
	@echo "Deploying $(BINARY_NAME) to $(TARGET_HOST):$(TARGET_PATH)/"
	@sshpass -p '$(TARGET_PASS)' scp $(BINARY_NAME) $(TARGET_HOST):$(TARGET_PATH)/
	@echo "Deployment complete."

test: build
	@echo "Running xl info parser test with sample data..."
	@./$(BINARY_NAME) -sample -verbose

test-real: build
	@echo "Running xl info parser test with real xl info..."
	@./$(BINARY_NAME) -verbose

clean:
	@echo "Cleaning up..."
	@rm -f $(BINARY_NAME)
	@echo "Clean complete."
