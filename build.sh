#!/bin/bash

echo "Building Zcode2API for all platforms..."
echo

# Create build directory
mkdir -p build

# Windows AMD64
echo "[1/4] Building Windows AMD64..."
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o build/zcode2api-windows-amd64.exe ./cmd/zcode2api || exit 1

# Linux AMD64
echo "[2/4] Building Linux AMD64..."
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o build/zcode2api-linux-amd64 ./cmd/zcode2api || exit 1

# macOS AMD64
echo "[3/4] Building macOS AMD64..."
GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o build/zcode2api-macos-amd64 ./cmd/zcode2api || exit 1

# macOS ARM64
echo "[4/4] Building macOS ARM64..."
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o build/zcode2api-macos-arm64 ./cmd/zcode2api || exit 1

echo
echo "✓ All builds completed successfully!"
echo
echo "Final build artifacts:"
ls -lh build/
echo
echo "Ready for GitHub release!"
