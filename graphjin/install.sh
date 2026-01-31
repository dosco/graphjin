#!/bin/bash
# GraphJin Installer
# Usage: curl -fsSL https://graphjin.com/install.sh | bash
#
# Environment variables:
#   GRAPHJIN_VERSION  - Specific version to install (default: latest)
#   GRAPHJIN_INSTALL_DIR - Installation directory (default: /usr/local/bin or ~/.local/bin)

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_banner() {
    echo -e "${BLUE}"
    echo '   ____                 _       _ _       '
    echo '  / ___|_ __ __ _ _ __ | |__   | (_)_ __  '
    echo ' | |  _| '\''__/ _` | '\''_ \| '\''_ \  | | | '\''_ \ '
    echo ' | |_| | | | (_| | |_) | | | |_| | | | | |'
    echo '  \____|_|  \__,_| .__/|_| |_(_)_|_|_| |_|'
    echo '                 |_|                      '
    echo -e "${NC}"
    echo "Build APIs in 5 minutes with GraphQL"
    echo ""
}

info() {
    echo -e "${BLUE}==>${NC} $1"
}

success() {
    echo -e "${GREEN}==>${NC} $1"
}

warn() {
    echo -e "${YELLOW}Warning:${NC} $1"
}

error() {
    echo -e "${RED}Error:${NC} $1" >&2
    exit 1
}

# Detect OS
detect_os() {
    local os
    os=$(uname -s | tr '[:upper:]' '[:lower:]')
    case "$os" in
        linux*)  echo "linux" ;;
        darwin*) echo "darwin" ;;
        mingw*|msys*|cygwin*) echo "windows" ;;
        *) error "Unsupported operating system: $os" ;;
    esac
}

# Detect architecture
detect_arch() {
    local arch
    arch=$(uname -m)
    case "$arch" in
        x86_64|amd64) echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        armv7l|armv6l) echo "arm" ;;
        i386|i686) echo "386" ;;
        *) error "Unsupported architecture: $arch" ;;
    esac
}

# Get latest version from GitHub
get_latest_version() {
    local latest
    if command -v curl &> /dev/null; then
        latest=$(curl -fsSL "https://api.github.com/repos/dosco/graphjin/releases/latest" | grep '"tag_name"' | sed -E 's/.*"v([^"]+)".*/\1/')
    elif command -v wget &> /dev/null; then
        latest=$(wget -qO- "https://api.github.com/repos/dosco/graphjin/releases/latest" | grep '"tag_name"' | sed -E 's/.*"v([^"]+)".*/\1/')
    else
        error "Neither curl nor wget found. Please install one of them."
    fi

    if [ -z "$latest" ]; then
        error "Failed to determine latest version"
    fi
    echo "$latest"
}

# Download file
download() {
    local url=$1
    local dest=$2

    if command -v curl &> /dev/null; then
        curl -fsSL "$url" -o "$dest"
    elif command -v wget &> /dev/null; then
        wget -q "$url" -O "$dest"
    else
        error "Neither curl nor wget found"
    fi
}

# Determine install directory
get_install_dir() {
    if [ -n "$GRAPHJIN_INSTALL_DIR" ]; then
        echo "$GRAPHJIN_INSTALL_DIR"
        return
    fi

    # Try /usr/local/bin first (requires sudo)
    if [ -w "/usr/local/bin" ]; then
        echo "/usr/local/bin"
        return
    fi

    # Fall back to ~/.local/bin
    local local_bin="$HOME/.local/bin"
    mkdir -p "$local_bin"
    echo "$local_bin"
}

# Check if directory is in PATH
check_path() {
    local dir=$1
    if [[ ":$PATH:" != *":$dir:"* ]]; then
        warn "$dir is not in your PATH"
        echo ""
        echo "Add it to your PATH by adding this to your shell profile:"
        echo ""
        if [[ "$SHELL" == *"zsh"* ]]; then
            echo "  echo 'export PATH=\"$dir:\$PATH\"' >> ~/.zshrc"
            echo "  source ~/.zshrc"
        elif [[ "$SHELL" == *"fish"* ]]; then
            echo "  fish_add_path $dir"
        else
            echo "  echo 'export PATH=\"$dir:\$PATH\"' >> ~/.bashrc"
            echo "  source ~/.bashrc"
        fi
        echo ""
    fi
}

main() {
    print_banner

    local os=$(detect_os)
    local arch=$(detect_arch)
    local version=${GRAPHJIN_VERSION:-$(get_latest_version)}
    local install_dir=$(get_install_dir)

    info "Detected: $os/$arch"
    info "Version: $version"
    info "Install directory: $install_dir"
    echo ""

    # Build download URL
    local ext="tar.gz"
    if [ "$os" = "windows" ]; then
        ext="zip"
    fi
    local filename="graphjin_${os}_${arch}.${ext}"
    local url="https://github.com/dosco/graphjin/releases/download/v${version}/${filename}"

    # Create temp directory
    local tmp_dir=$(mktemp -d)
    trap "rm -rf $tmp_dir" EXIT

    local archive_path="$tmp_dir/$filename"

    info "Downloading GraphJin v${version}..."
    download "$url" "$archive_path" || error "Failed to download from $url"

    info "Extracting..."
    if [ "$os" = "windows" ]; then
        unzip -q "$archive_path" -d "$tmp_dir"
    else
        tar -xzf "$archive_path" -C "$tmp_dir"
    fi

    # Find the binary
    local binary_name="graphjin"
    if [ "$os" = "windows" ]; then
        binary_name="graphjin.exe"
    fi

    local binary_path="$tmp_dir/$binary_name"
    if [ ! -f "$binary_path" ]; then
        error "Binary not found in archive"
    fi

    # Install
    info "Installing to $install_dir..."

    local dest="$install_dir/$binary_name"
    if [ -w "$install_dir" ]; then
        mv "$binary_path" "$dest"
        chmod +x "$dest"
    else
        sudo mv "$binary_path" "$dest"
        sudo chmod +x "$dest"
    fi

    echo ""
    success "GraphJin v${version} installed successfully!"
    echo ""

    # Verify installation
    if command -v graphjin &> /dev/null; then
        echo "Installed version:"
        graphjin version 2>/dev/null || graphjin --version 2>/dev/null || true
    else
        check_path "$install_dir"
    fi

    echo ""
    echo "Get started:"
    echo "  graphjin new myapp    # Create a new project"
    echo "  graphjin serv         # Start the server"
    echo ""
    echo "Documentation: https://graphjin.com"
    echo ""
}

main "$@"
