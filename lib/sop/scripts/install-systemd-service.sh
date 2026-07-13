#!/usr/bin/env bash
set -euo pipefail

BIN_NAME="${1:-sop-httpserver}"
PORT="${2:-8080}"
DATA_DIR="${3:-/var/lib/sop}"
CONFIG_FILE="${4:-}"
SERVICE_FILE=""

if [[ "$EUID" -ne 0 ]]; then
  echo "This script must be run as root" >&2
  exit 1
fi

if [[ "$BIN_NAME" == "sop-httpserver" ]]; then
  SERVICE_FILE="/etc/systemd/system/sop-httpserver.service"
  DEFAULT_BIN="/usr/local/bin/sop-httpserver"
elif [[ "$BIN_NAME" == "sop-daemon" ]]; then
  SERVICE_FILE="/etc/systemd/system/sop-daemon.service"
  DEFAULT_BIN="/usr/local/bin/sop-daemon"
else
  echo "Unsupported binary: $BIN_NAME" >&2
  exit 1
fi

mkdir -p "$DATA_DIR" /etc/sop /usr/local/bin

if [[ -f "$BIN_NAME" ]]; then
  install -m 0755 "$BIN_NAME" "$DEFAULT_BIN"
elif [[ -f "/opt/sop/bin/$BIN_NAME" ]]; then
  install -m 0755 "/opt/sop/bin/$BIN_NAME" "$DEFAULT_BIN"
else
  echo "Binary not found at $BIN_NAME or /opt/sop/bin/$BIN_NAME" >&2
  exit 1
fi

if [[ "$BIN_NAME" == "sop-httpserver" ]]; then
  cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=SOP HTTP Server
After=network.target

[Service]
Type=simple
WorkingDirectory=$DATA_DIR
Environment=datapath=$DATA_DIR
ExecStart=$DEFAULT_BIN --port $PORT --database $DATA_DIR
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
User=root
Group=root

[Install]
WantedBy=multi-user.target
EOF
else
  cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=SOP Daemon
After=network.target

[Service]
Type=simple
WorkingDirectory=$DATA_DIR
Environment=datapath=$DATA_DIR
ExecStart=$DEFAULT_BIN --port $PORT
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
User=root
Group=root

[Install]
WantedBy=multi-user.target
EOF
fi

if [[ -n "$CONFIG_FILE" ]]; then
  if [[ "$BIN_NAME" == "sop-httpserver" ]]; then
    sed -i "s#ExecStart=.*#ExecStart=$DEFAULT_BIN --config $CONFIG_FILE --port $PORT --database $DATA_DIR#" "$SERVICE_FILE"
  else
    sed -i "s#ExecStart=.*#ExecStart=$DEFAULT_BIN --config $CONFIG_FILE --port $PORT#" "$SERVICE_FILE"
  fi
fi

systemctl daemon-reload
systemctl enable "$(basename "$SERVICE_FILE")"
systemctl restart "$(basename "$SERVICE_FILE")"

echo "Installed and started $(basename "$SERVICE_FILE")"
