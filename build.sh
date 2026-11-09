#!/bin/bash
set -e

echo "Building T-REX..."
echo

# 检查 Go 是否安装
if ! command -v go &> /dev/null; then
    echo "Error: Go is not installed"
    exit 1
fi

# 设置版本信息
VERSION="${VERSION:-dev}"
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S')

# 检测操作系统
OS="$(uname | tr '[:upper:]' '[:lower:]')"
case "$OS" in
    darwin) OS="darwin" ;;
    linux)  OS="linux" ;;
    msys*|mingw*) OS="windows" ;;
esac
ARCH="amd64"
if [ "$(uname -m)" = "arm64" ]; then
    ARCH="arm64"
fi

echo "Version: $VERSION"
echo "Target: $OS/$ARCH"
echo

# 创建输出目录
mkdir -p dist

# 设置编译参数
export CGO_ENABLED=0
LDFLAGS="-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME}"

# 编译
OUTPUT_NAME="t-rex"
if [ "$OS" = "windows" ]; then
    OUTPUT_NAME="t-rex.exe"
fi

go build -buildvcs=false -ldflags="$LDFLAGS" -o "dist/${OUTPUT_NAME}" .

if [ $? -eq 0 ]; then
    echo
    echo "Build successful! Output: dist/${OUTPUT_NAME}"
    
    # 显示文件大小
    if command -v stat &> /dev/null; then
        SIZE=$(stat -f%z "dist/${OUTPUT_NAME}" 2>/dev/null || stat -c%s "dist/${OUTPUT_NAME}" 2>/dev/null || echo "unknown")
        echo "File size: $SIZE bytes"
    fi
    
    # 如果是 Linux/macOS，设置可执行权限
    if [ "$OS" != "windows" ]; then
        chmod +x "dist/${OUTPUT_NAME}"
    fi
else
    echo
    echo "Build failed!"
    exit 1
fi
