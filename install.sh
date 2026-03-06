#!/bin/sh
set -e

REPO="inferalabs/mantismo"
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

LATEST=$(curl -sSL "https://api.github.com/repos/$REPO/releases/latest" | grep tag_name | cut -d'"' -f4)
URL="https://github.com/$REPO/releases/download/$LATEST/mantismo_${LATEST#v}_${OS}_${ARCH}.tar.gz"

echo "Installing Mantismo $LATEST for $OS/$ARCH..."
curl -sSL "$URL" | tar xz -C /tmp
sudo mv /tmp/mantismo /usr/local/bin/
echo "Mantismo installed successfully. Run 'mantismo --version' to verify."
