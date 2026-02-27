.PHONY: build test lint vet fmt clean help

BINARY := control-plane
BUILD_DIR := build

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':' | sed 's/^/  /'

## build: Build the binary
build:
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY) .

## test: Run all tests
test:
	go test ./... -v

## lint: Run golangci-lint
lint:
	golangci-lint run ./...

## vet: Run go vet
vet:
	go vet ./...

## fmt: Format code with goimports and golangci-lint
fmt:
	goimports -w .
	golangci-lint run --fix ./...

## clean: Remove build artifacts
clean:
	rm -rf $(BUILD_DIR)

## run: Run the control plane locally
run: build
	./$(BUILD_DIR)/$(BINARY)
