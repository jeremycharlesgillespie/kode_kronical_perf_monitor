#!/bin/bash
# CPU Monitor Build Script
# Simple build script for the Kode Kronical Perf Monitor

set -e  # Exit on any error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Configuration
BINARY_NAME="cpu_monitor"
SOURCE_FILE="cpu_monitor.go"
BUILD_DIR="."

# Function to print colored output
print_step() {
    echo -e "${CYAN}==>${NC} $1"
}

print_success() {
    echo -e "${GREEN}✓${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

print_error() {
    echo -e "${RED}✗${NC} $1"
}

print_info() {
    echo -e "${BLUE}ℹ${NC} $1"
}

# Function to check if a command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Function to check Go installation
check_go() {
    print_step "Checking Go installation..."
    
    if ! command_exists go; then
        print_error "Go is not installed or not in PATH"
        echo "Please install Go 1.19 or later from https://golang.org/dl/"
        exit 1
    fi
    
    GO_VERSION=$(go version | cut -d' ' -f3 | tr -d 'go')
    print_success "Go $GO_VERSION found"
}

# Function to check dependencies
check_dependencies() {
    print_step "Checking optional dependencies..."
    
    # Check for stress command
    if command_exists stress; then
        print_success "stress command available - stress testing will work"
    else
        print_warning "stress command not found - stress testing will be disabled"
        print_info "Install with: sudo apt install stress (Ubuntu/Debian)"
        print_info "             sudo pacman -S stress (Arch Linux)"
        print_info "             sudo yum install stress (CentOS/RHEL)"
    fi
    
    # Check for sensors command (useful for temperature monitoring)
    if command_exists sensors; then
        print_success "sensors command available - enhanced temperature monitoring"
    else
        print_warning "sensors command not found - will use fallback temperature sources"
        print_info "Install with: sudo apt install lm-sensors (Ubuntu/Debian)"
    fi
    
    echo
}

# Function to setup Go module
setup_module() {
    print_step "Setting up Go module..."
    
    if [ ! -f "go.mod" ]; then
        print_info "Creating go.mod file..."
        go mod init cpu_monitor
    fi
    
    print_info "Installing/updating dependencies..."
    go mod tidy
    print_success "Dependencies ready"
}

# Function to get build information
get_build_info() {
    # Get version from git tags or set default
    if command_exists git && [ -d ".git" ]; then
        VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
        COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
    else
        VERSION="dev"
        COMMIT="unknown"
    fi
    
    # Build date
    BUILD_DATE=$(date -u +"%Y-%m-%d %H:%M:%S UTC")
    
    # Export for use in build function
    export BUILD_VERSION="$VERSION"
    export BUILD_COMMIT="$COMMIT"
    export BUILD_DATE="$BUILD_DATE"
}

# Function to build the application
build_app() {
    local build_type="$1"
    
    # Get build information
    get_build_info
    
    # Create ldflags with version information
    LDFLAGS="-X 'main.version=${BUILD_VERSION}' -X 'main.commit=${BUILD_COMMIT}' -X 'main.date=${BUILD_DATE}'"
    
    case "$build_type" in
        "static")
            print_step "Building static binary..."
            print_info "Version: $BUILD_VERSION, Commit: $BUILD_COMMIT"
            CGO_ENABLED=0 go build -ldflags="-w -s $LDFLAGS" -o "$BINARY_NAME" "$SOURCE_FILE"
            ;;
        "debug")
            print_step "Building debug binary..."
            print_info "Version: $BUILD_VERSION, Commit: $BUILD_COMMIT"
            go build -gcflags="-N -l" -ldflags="$LDFLAGS" -o "$BINARY_NAME" "$SOURCE_FILE"
            ;;
        *)
            print_step "Building standard binary..."
            print_info "Version: $BUILD_VERSION, Commit: $BUILD_COMMIT"
            go build -ldflags="$LDFLAGS" -o "$BINARY_NAME" "$SOURCE_FILE"
            ;;
    esac
    
    if [ -f "$BINARY_NAME" ]; then
        print_success "Build complete: ./$BINARY_NAME"
        
        # Show file size
        if command_exists ls; then
            SIZE=$(ls -lh "$BINARY_NAME" | awk '{print $5}')
            print_info "Binary size: $SIZE"
        fi
        
        # Make executable (just in case)
        chmod +x "$BINARY_NAME"
    else
        print_error "Build failed - binary not created"
        exit 1
    fi
}

# Function to run the application
run_app() {
    if [ -f "$BINARY_NAME" ]; then
        print_step "Starting CPU Monitor..."
        echo
        ./"$BINARY_NAME"
    else
        print_error "Binary not found. Run build first."
        exit 1
    fi
}

# Function to clean build artifacts
clean_build() {
    print_step "Cleaning build artifacts..."
    
    if [ -f "$BINARY_NAME" ]; then
        rm "$BINARY_NAME"
        print_success "Removed $BINARY_NAME"
    else
        print_info "No build artifacts to clean"
    fi
}

# Function to show help
show_help() {
    echo -e "${CYAN}CPU Monitor Build Script${NC}"
    echo
    echo "Usage: $0 [command]"
    echo
    echo "Commands:"
    echo "  build         - Build standard binary (default)"
    echo "  static        - Build optimized static binary"
    echo "  debug         - Build with debug symbols"
    echo "  run           - Build and run the application"
    echo "  clean         - Remove build artifacts"
    echo "  check         - Check system dependencies only"
    echo "  install       - Build and install system-wide (requires sudo)"
    echo "  help          - Show this help message"
    echo
    echo "Examples:"
    echo "  $0              # Build standard binary"
    echo "  $0 run          # Build and run"
    echo "  $0 static       # Build optimized static binary"
    echo "  $0 install      # Install system-wide"
    echo
}

# Function to install system-wide
install_system() {
    build_app "static"
    
    print_step "Installing system-wide..."
    
    if [ "$EUID" -eq 0 ]; then
        # Running as root
        cp "$BINARY_NAME" /usr/local/bin/
        print_success "Installed to /usr/local/bin/$BINARY_NAME"
    else
        # Not root, use sudo
        if command_exists sudo; then
            sudo cp "$BINARY_NAME" /usr/local/bin/
            print_success "Installed to /usr/local/bin/$BINARY_NAME"
        else
            print_error "sudo not available. Run as root or install sudo."
            exit 1
        fi
    fi
    
    print_info "You can now run 'cpu_monitor' from anywhere"
}

# Main script logic
main() {
    echo -e "${GREEN}🖥️  CPU Monitor Build Script${NC}"
    echo -e "${CYAN}===============================${NC}"
    echo
    
    # Parse command line argument
    COMMAND="${1:-build}"
    
    case "$COMMAND" in
        "help"|"-h"|"--help")
            show_help
            exit 0
            ;;
        "check")
            check_go
            check_dependencies
            exit 0
            ;;
        "clean")
            clean_build
            exit 0
            ;;
        "build")
            check_go
            check_dependencies
            setup_module
            build_app "standard"
            ;;
        "static")
            check_go
            check_dependencies
            setup_module
            build_app "static"
            ;;
        "debug")
            check_go
            check_dependencies
            setup_module
            build_app "debug"
            ;;
        "run")
            check_go
            check_dependencies
            setup_module
            build_app "standard"
            echo
            run_app
            ;;
        "install")
            check_go
            check_dependencies
            setup_module
            install_system
            ;;
        *)
            print_error "Unknown command: $COMMAND"
            echo
            show_help
            exit 1
            ;;
    esac
}

# Run main function with all arguments
main "$@"