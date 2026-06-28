#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR=${INSTALL_DIR:-/usr/local/bin}
SERVICE_DIR=${SERVICE_DIR:-/etc/systemd/system}
CONFIG_DIR=${CONFIG_DIR:-/etc/sop-ha}
SCRIPT_SOURCE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/sop-ha-failover.sh"
SERVICE_SOURCE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/sop-ha-failover.service"
CONFIG_SOURCE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/sop-ha.conf.example"

if [[ $(id -u) -ne 0 ]]; then
  echo "Please run this installer as root." >&2
  exit 1
fi

install -d "$INSTALL_DIR" "$SERVICE_DIR" "$CONFIG_DIR"
install -m 0755 "$SCRIPT_SOURCE" "$INSTALL_DIR/sop-ha"
install -m 0644 "$SERVICE_SOURCE" "$SERVICE_DIR/sop-ha.service"

if [[ ! -f "$CONFIG_DIR/sop-ha.conf" ]]; then
  install -m 0644 "$CONFIG_SOURCE" "$CONFIG_DIR/sop-ha.conf"
fi

if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload >/dev/null 2>&1 || true
  systemctl enable sop-ha.service >/dev/null 2>&1 || true
fi

echo "Installed SOP HA helper to $INSTALL_DIR/sop-ha"
echo "Configuration file: $CONFIG_DIR/sop-ha.conf"
echo "Systemd unit: $SERVICE_DIR/sop-ha.service"
echo "Next steps:"
echo "  1. Edit $CONFIG_DIR/sop-ha.conf and set SERVICE_NAME and PEER_HOST"
echo "  2. Run: systemctl start sop-ha.service"
