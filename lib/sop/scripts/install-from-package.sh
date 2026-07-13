#!/usr/bin/env bash
set -euo pipefail

BIN_NAME="${1:-sop-httpserver}"
PORT="${2:-8080}"
DATA_DIR="${3:-/var/lib/sop}"
CONFIG_FILE="${4:-}"

if [[ "$EUID" -ne 0 ]]; then
  echo "This script must be run as root" >&2
  exit 1
fi

if [[ ! -f "$BIN_NAME" ]]; then
  echo "Binary not found in current directory: $BIN_NAME" >&2
  exit 1
fi

install -m 0755 "$BIN_NAME" "/usr/local/bin/$BIN_NAME"
mkdir -p "$DATA_DIR"

if [[ "$BIN_NAME" == "sop-httpserver" ]]; then
  /bin/bash /usr/local/bin/install-systemd-service.sh sop-httpserver "$PORT" "$DATA_DIR" "$CONFIG_FILE"
else
  /bin/bash /usr/local/bin/install-systemd-service.sh sop-daemon "$PORT" "$DATA_DIR" "$CONFIG_FILE"
fi
