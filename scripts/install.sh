#!/usr/bin/env sh
# install.sh — Forge installer
# Usage: curl -fsSL https://raw.githubusercontent.com/arturklasa/forge/main/scripts/install.sh | sh

set -e

REPO="arturklasa/forge"
BINARY="forge"

detect_os() {
  case "$(uname -s)" in
    Linux*)  echo "linux" ;;
    Darwin*) echo "darwin" ;;
    MINGW*|MSYS*|CYGWIN*) echo "windows" ;;
    *) echo "unsupported" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    *) echo "unsupported" ;;
  esac
}

OS=$(detect_os)
ARCH=$(detect_arch)

if [ "$OS" = "unsupported" ] || [ "$ARCH" = "unsupported" ]; then
  echo "Error: unsupported platform (OS=$OS ARCH=$ARCH)." >&2
  echo "Install from source: go install github.com/arturklasa/forge/cmd/forge@latest" >&2
  exit 1
fi

ASSET_NAME="forge_${OS}_${ARCH}"
[ "$OS" = "windows" ] && ASSET_NAME="${ASSET_NAME}.exe"

# Resolve latest release tag from GitHub API
if command -v curl >/dev/null 2>&1; then
  FETCH="curl -fsSL"
elif command -v wget >/dev/null 2>&1; then
  FETCH="wget -qO-"
else
  echo "Error: curl or wget is required." >&2
  exit 1
fi

LATEST=$($FETCH "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed 's/.*"tag_name": *"\(.*\)".*/\1/')
if [ -z "$LATEST" ]; then
  echo "Error: could not determine latest release version." >&2
  exit 1
fi

DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${LATEST}/${ASSET_NAME}"

# Choose install directory
if [ -w /usr/local/bin ]; then
  INSTALL_DIR="/usr/local/bin"
elif [ -d "$HOME/.local/bin" ]; then
  INSTALL_DIR="$HOME/.local/bin"
else
  INSTALL_DIR="$HOME/.local/bin"
  mkdir -p "$INSTALL_DIR"
fi

INSTALL_PATH="${INSTALL_DIR}/${BINARY}"

echo "Downloading forge ${LATEST} for ${OS}/${ARCH}..."
$FETCH "$DOWNLOAD_URL" -o "$INSTALL_PATH"
chmod +x "$INSTALL_PATH"

echo "forge ${LATEST} installed to ${INSTALL_PATH}"
echo ""

# Warn if install dir not on PATH
case ":$PATH:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    echo "Note: ${INSTALL_DIR} is not in your PATH."
    echo "Add the following to your shell profile:"
    echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
    ;;
esac

# Verify
"$INSTALL_PATH" --version
