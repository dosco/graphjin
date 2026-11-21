#!/bin/bash
# DeepORG Installer
# Usage: curl -fsSL https://deeporg.io/install.sh | bash
#
# Installs the `deeporg` command, and `graphjin` (its old name) as a link to
# the same file, so anything that still calls `graphjin` keeps working.
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
    echo ""
    echo -e "${BLUE}DeepORG${NC}"
    echo "One AI agent for your whole organization"
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
        armv7l|armv6l|i386|i686) error "No 32-bit build is published. Use a 64-bit system or build from source." ;;
        *) error "Unsupported architecture: $arch" ;;
    esac
}

# Get latest version from GitHub
get_latest_version() {
    local latest
    if command -v curl &> /dev/null; then
        latest=$(curl -fsSL "https://api.github.com/repos/dosco/deeporg/releases/latest" | grep '"tag_name"' | sed -E 's/.*"v([^"]+)".*/\1/')
    elif command -v wget &> /dev/null; then
        latest=$(wget -qO- "https://api.github.com/repos/dosco/deeporg/releases/latest" | grep '"tag_name"' | sed -E 's/.*"v([^"]+)".*/\1/')
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

    # Build download URL candidates, newest naming first. Since the command
    # was renamed the archive is deeporg_<version>_<os>_<arch>; releases up to
    # v3.21.21 were graphjin_<version>_<os>_<arch>, and much older ones had no
    # version in the name. Keep every form so any version still installs.
    local release_base="https://github.com/dosco/deeporg/releases/download/v${version}"
    local filenames=()
    if [ "$os" = "windows" ]; then
        filenames=(
            "deeporg_${version}_${os}_${arch}.tar.gz"
            "deeporg_${version}_${os}_${arch}.zip"
            "graphjin_${version}_${os}_${arch}.tar.gz"
            "graphjin_${version}_${os}_${arch}.zip"
            "graphjin_${os}_${arch}.tar.gz"
            "graphjin_${os}_${arch}.zip"
        )
    else
        filenames=(
            "deeporg_${version}_${os}_${arch}.tar.gz"
            "graphjin_${version}_${os}_${arch}.tar.gz"
            "graphjin_${os}_${arch}.tar.gz"
        )
    fi

    # Create temp directory
    local tmp_dir=$(mktemp -d)
    trap "rm -rf $tmp_dir" EXIT

    local archive_path=""
    local selected_filename=""

    # Misses on the newer names are expected for an older version, so the
    # downloader's own errors are hidden below; rule out the one failure that
    # would then be silent.
    if ! command -v curl &> /dev/null && ! command -v wget &> /dev/null; then
        error "Neither curl nor wget found. Please install one of them."
    fi

    info "Downloading DeepORG v${version}..."
    for filename in "${filenames[@]}"; do
        local url="${release_base}/${filename}"
        archive_path="$tmp_dir/$filename"
        if download "$url" "$archive_path" 2>/dev/null; then
            selected_filename="$filename"
            break
        fi
        rm -f "$archive_path"
    done

    if [ -z "$selected_filename" ]; then
        error "Failed to download DeepORG v${version}. Tried: ${filenames[*]}"
    fi

    info "Extracting..."
    if [[ "$selected_filename" == *.zip ]]; then
        unzip -q "$archive_path" -d "$tmp_dir"
    else
        tar -xzf "$archive_path" -C "$tmp_dir"
    fi

    # Find the binary. A release from before the rename holds `graphjin`; it
    # is installed under that name and `deeporg` becomes the link, so both
    # commands exist whichever version was asked for.
    local ext=""
    if [ "$os" = "windows" ]; then
        ext=".exe"
    fi

    local binary_name=""
    local alias_name=""
    if [ -f "$tmp_dir/deeporg$ext" ]; then
        binary_name="deeporg$ext"
        alias_name="graphjin$ext"
    elif [ -f "$tmp_dir/graphjin$ext" ]; then
        binary_name="graphjin$ext"
        alias_name="deeporg$ext"
    else
        error "Binary not found in archive"
    fi

    local binary_path="$tmp_dir/$binary_name"

    # Install
    info "Installing to $install_dir..."

    local dest="$install_dir/$binary_name"
    local alias_dest="$install_dir/$alias_name"
    local as_root=""
    if [ ! -w "$install_dir" ]; then
        as_root="sudo"
    fi

    # mv replaces whatever is at $dest, including a link left by an install
    # of the other naming; it never writes through that link.
    $as_root mv "$binary_path" "$dest"
    $as_root chmod +x "$dest"

    # The second name. A relative link, so the pair can be moved together.
    # Windows shells cannot be relied on for symlinks: use a hard link there,
    # or a copy if the filesystem refuses one.
    if [ "$os" = "windows" ]; then
        $as_root rm -f "$alias_dest"
        $as_root ln "$dest" "$alias_dest" 2>/dev/null || $as_root cp "$dest" "$alias_dest"
    else
        $as_root ln -sfn "$binary_name" "$alias_dest"
    fi

    echo ""
    success "DeepORG v${version} installed successfully!"
    echo "  $dest"
    echo "  $alias_dest -> $binary_name"
    echo ""

    # Verify installation
    if command -v deeporg &> /dev/null; then
        echo "Installed version:"
        deeporg version 2>/dev/null || deeporg --version 2>/dev/null || true
    else
        check_path "$install_dir"
    fi

    echo ""
    echo "Get started:"
    echo "  deeporg serve new myapp    # Create a new project"
    echo "  deeporg serve              # Start the server"
    echo ""
    echo "\`graphjin\` is the old name of the same command and still works."
    echo ""
    echo "Documentation: https://deeporg.io"
    echo ""
}

main "$@"
