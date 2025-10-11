TARGET_HOST := root@192.168.7.2
TARGET_PATH := /root
TARGET_PASS := $(TEST_SSH_PASSWD)
BINARY_NAME := simple_tty

.PHONY: all build deploy clean

all: build

build:
	@echo "Building for linux/arm64..."
	@GOOS=linux GOARCH=arm64 go build -o $(BINARY_NAME) main.go
	@echo "Build complete: $(BINARY_NAME)"

deploy: build
	@echo "Deploying $(BINARY_NAME) to $(TARGET_HOST):$(TARGET_PATH)/"
	@sshpass -p '$(TARGET_PASS)' scp $(BINARY_NAME) $(TARGET_HOST):$(TARGET_PATH)/
	@echo "Deployment complete."

clean:
	@echo "Cleaning up..."
	@rm -f $(BINARY_NAME)
	@echo "Clean complete."
