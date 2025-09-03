# CPU Monitor Makefile

BINARY_NAME=cpu_monitor
SOURCE_FILE=cpu_monitor.go

.PHONY: all build clean install deps check help

# Default target
all: build

# Build the application
build: deps
	@echo "Building $(BINARY_NAME)..."
	go build -o $(BINARY_NAME) $(SOURCE_FILE)
	@echo "Build complete: ./$(BINARY_NAME)"

# Build optimized static binary
build-static: deps
	@echo "Building static $(BINARY_NAME)..."
	CGO_ENABLED=0 go build -ldflags="-w -s" -o $(BINARY_NAME) $(SOURCE_FILE)
	@echo "Static build complete: ./$(BINARY_NAME)"

# Install dependencies
deps:
	@echo "Installing dependencies..."
	@if [ ! -f "go.mod" ]; then \
		echo "Initializing Go module..."; \
		go mod init cpu_monitor; \
	fi
	go mod tidy
	@echo "Dependencies installed"

# Check if stress command is available
check-stress:
	@if command -v stress >/dev/null 2>&1; then \
		echo "✓ stress command is available"; \
	else \
		echo "⚠ stress command not found - stress testing will not work"; \
		echo "  Install with: sudo apt install stress (Ubuntu/Debian)"; \
		echo "              : sudo pacman -S stress (Arch Linux)"; \
		echo "              : sudo yum install stress (CentOS/RHEL)"; \
	fi

# Run the application
run: build
	./$(BINARY_NAME)

# Run with stress check
run-check: check-stress run

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -f $(BINARY_NAME)
	@echo "Clean complete"

# Install system-wide (requires sudo)
install: build-static
	@echo "Installing $(BINARY_NAME) to /usr/local/bin..."
	sudo cp $(BINARY_NAME) /usr/local/bin/
	@echo "Installation complete"

# Uninstall from system
uninstall:
	@echo "Removing $(BINARY_NAME) from /usr/local/bin..."
	sudo rm -f /usr/local/bin/$(BINARY_NAME)
	@echo "Uninstall complete"

# Development - build and run with checks
dev: check-stress build run

# Show help
help:
	@echo "CPU Monitor Build System"
	@echo ""
	@echo "Targets:"
	@echo "  build        - Build the application"
	@echo "  build-static - Build optimized static binary"
	@echo "  deps         - Install/update dependencies"
	@echo "  check-stress - Check if stress command is available"
	@echo "  run          - Build and run the application"
	@echo "  run-check    - Check dependencies and run"
	@echo "  dev          - Development build with checks"
	@echo "  clean        - Remove build artifacts"
	@echo "  install      - Install system-wide (requires sudo)"
	@echo "  uninstall    - Remove from system"
	@echo "  help         - Show this help"
	@echo ""
	@echo "Quick start:"
	@echo "  make         - Build the application"
	@echo "  make dev     - Check dependencies and run"