.PHONY: build clean install

BINARY_NAME=containerd-shim-mica-v1
BUILD_DIR=build
INSTALL_DIR=/usr/local/bin

all: build

build:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 go build -o $(BUILD_DIR)/$(BINARY_NAME) .

clean:
	@rm -rf $(BUILD_DIR)

# install: build
# 	@sudo cp $(BUILD_DIR)/$(BINARY_NAME) $(INSTALL_DIR)/
# 	@sudo chmod +x $(INSTALL_DIR)/$(BINARY_NAME)

test:
	go test -v ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

deps:
	go mod tidy
	go mod download

.DEFAULT_GOAL := build