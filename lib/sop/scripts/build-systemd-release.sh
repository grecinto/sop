#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${1:-$ROOT_DIR/release/systemd}"
TARGET_OS="${TARGET_OS:-$(go env GOOS)}"
TARGET_ARCH="${TARGET_ARCH:-$(go env GOARCH)}"
ARCHIVE_NAME="sop-systemd-release-${TARGET_OS}-${TARGET_ARCH}.tar.gz"

mkdir -p "$OUT_DIR"
cd "$ROOT_DIR"

echo "Building ${TARGET_OS}/${TARGET_ARCH} binaries for systemd deployment..."
GOOS="$TARGET_OS" GOARCH="$TARGET_ARCH" go build -ldflags='-s -w' -o "$OUT_DIR/sop-httpserver" ./tools/httpserver
GOOS="$TARGET_OS" GOARCH="$TARGET_ARCH" go build -ldflags='-s -w' -o "$OUT_DIR/sop-daemon" ./cmd/sop-daemon

install -m 0755 scripts/install-systemd-service.sh "$OUT_DIR/install-systemd-service.sh"
install -m 0755 scripts/install-from-package.sh "$OUT_DIR/install-from-package.sh"
install -m 0644 scripts/systemd/sop-httpserver.service "$OUT_DIR/sop-httpserver.service"
install -m 0644 scripts/systemd/sop-daemon.service "$OUT_DIR/sop-daemon.service"

tar -czf "$OUT_DIR/$ARCHIVE_NAME" -C "$OUT_DIR" \
  sop-httpserver \
  sop-daemon \
  install-systemd-service.sh \
  install-from-package.sh \
  sop-httpserver.service \
  sop-daemon.service

echo "Created $OUT_DIR/$ARCHIVE_NAME"
