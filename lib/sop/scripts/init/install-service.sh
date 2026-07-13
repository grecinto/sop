#!/usr/bin/env bash
set -euo pipefail

BIN_NAME="${1:-sop-httpserver}"
PORT="${2:-8080}"
DATA_DIR="${3:-/var/lib/sop}"
CONFIG_FILE="${4:-}"
SERVICE_NAME="${BIN_NAME//\//_}"
BIN_PATH="/usr/local/bin/$BIN_NAME"

if [[ "$EUID" -ne 0 ]]; then
  echo "This script must be run as root" >&2
  exit 1
fi

if ! command -v systemctl >/dev/null 2>&1; then
  echo "systemd not available on this host; skipping service installation" >&2
  exit 0
fi

mkdir -p /etc/sop /opt/sop/bin "$DATA_DIR"

if [[ -f "$BIN_PATH" ]]; then
  echo "Binary already installed at $BIN_PATH"
else
  if [[ -f "$BIN_NAME" ]]; then
    install -m 0755 "$BIN_NAME" "$BIN_PATH"
  elif [[ -f "/opt/sop/bin/$BIN_NAME" ]]; then
    install -m 0755 "/opt/sop/bin/$BIN_NAME" "$BIN_PATH"
  else
    echo "Binary not found: $BIN_NAME" >&2
    exit 1
  fi
fi

mkdir -p "$DATA_DIR"
cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<EOF
[Unit]
Description=SOP service for ${BIN_NAME}
After=network.target

[Service]
Type=simple
WorkingDirectory=$DATA_DIR
Environment=datapath=$DATA_DIR
ExecStart=$BIN_PATH --port $PORT
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
User=root
Group=root

[Install]
WantedBy=multi-user.target
EOF

if [[ "$BIN_NAME" == "sop-httpserver" ]]; then
  sed -i "s#ExecStart=.*#ExecStart=$BIN_PATH --port $PORT --database $DATA_DIR#" "/etc/systemd/system/${SERVICE_NAME}.service"
fi

if [[ -n "$CONFIG_FILE" ]]; then
  sed -i "s#ExecStart=.*#ExecStart=$BIN_PATH --config $CONFIG_FILE --port $PORT#" "/etc/systemd/system/${SERVICE_NAME}.service"
fi

systemctl daemon-reload
systemctl enable "${SERVICE_NAME}.service"
systemctl restart "${SERVICE_NAME}.service"

echo "Installed and started ${SERVICE_NAME}.service"
