.PHONY: help lint lint-fix test build clean install

# default target when just running 'make'
help:
	@echo "Available targets:"
	@echo "  make lint         - run linters on the codebase"
	@echo "  make lint-fix     - run linters and auto-fix issues where possible"
	@echo "  make test         - run all tests"
	@echo "  make build        - build the da-monitor binary"
	@echo "  make install      - install da-monitor to GOPATH/bin"
	@echo "  make clean        - remove build artifacts"

# run golangci-lint on the codebase
lint:
	@echo "Running golangci-lint..."
	golangci-lint run ./...

# run golangci-lint with auto-fix
lint-fix:
	@echo "Running golangci-lint with auto-fix..."
	golangci-lint run --fix ./...

# run all tests
test:
	@echo "Running tests..."
	go test -v ./...

# build the binary
build:
	@echo "Building da-monitor..."
	go build -o da-monitor

# install to GOPATH/bin
install:
	@echo "Installing da-monitor..."
	go install

# clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -f da-monitor
