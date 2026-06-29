#!/usr/bin/env bash
set -euo pipefail

SCRIPT_NAME=$(basename "$0")
CONFIG_FILE=${CONFIG_FILE:-/etc/sop-ha/sop-ha.conf}
STATE_DIR=${STATE_DIR:-/var/lib/sop-ha}
STATE_FILE="$STATE_DIR/state"
HEARTBEAT_FILE="$STATE_DIR/peer-heartbeat"
LOG_FILE=${LOG_FILE:-/var/log/sop-ha.log}

mkdir -p "$STATE_DIR"
touch "$LOG_FILE"

load_config() {
  if [[ -f "$CONFIG_FILE" ]]; then
    # shellcheck disable=SC1090
    source "$CONFIG_FILE"
  fi

  SERVICE_NAME=${SERVICE_NAME:-sop}
  PEER_HOST=${PEER_HOST:-}
  PEER_USER=${PEER_USER:-root}
  POLL_INTERVAL=${POLL_INTERVAL:-10}
  FAIL_TIMEOUT=${FAIL_TIMEOUT:-30}
  SSH_ARGS=( -o BatchMode=yes -o ConnectTimeout=2 -o StrictHostKeyChecking=accept-new )
}

log() {
  echo "[$(date -u +"%Y-%m-%dT%H:%M:%SZ")] $*" | tee -a "$LOG_FILE" >&2
}

service_is_active() {
  if command -v systemctl >/dev/null 2>&1; then
    systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null
  else
    service "$SERVICE_NAME" status >/dev/null 2>&1
  fi
}

service_start() {
  if command -v systemctl >/dev/null 2>&1; then
    systemctl start "$SERVICE_NAME" >/dev/null 2>&1 || true
  else
    service "$SERVICE_NAME" start >/dev/null 2>&1 || true
  fi
}

service_stop() {
  if command -v systemctl >/dev/null 2>&1; then
    systemctl stop "$SERVICE_NAME" >/dev/null 2>&1 || true
  else
    service "$SERVICE_NAME" stop >/dev/null 2>&1 || true
  fi
}

peer_is_healthy() {
  if [[ -z "$PEER_HOST" ]]; then
    return 1
  fi

  local remote_cmd="systemctl is-active --quiet '$SERVICE_NAME'"
  timeout 5 ssh "${SSH_ARGS[@]}" "${PEER_USER}@${PEER_HOST}" "$remote_cmd" >/dev/null 2>&1
}

read_state() {
  if [[ -f "$STATE_FILE" ]]; then
    cat "$STATE_FILE"
  else
    echo "passive"
  fi
}

write_state() {
  local new_state="$1"
  printf '%s\n' "$new_state" > "$STATE_FILE"
}

promote_self() {
  if ! service_is_active; then
    log "Promoting local node to active for service $SERVICE_NAME"
    service_start
  fi
  write_state "active"
}

demote_self() {
  log "Demoting local node to passive for service $SERVICE_NAME"
  service_stop || true
  write_state "passive"
}

status_report() {
  local state
  local heartbeat_age
  state=$(read_state)
  printf 'mode=active-passive\n'
  printf 'storage_layout=active_writes_primary_passive_stands_by_on_replica\n'
  printf 'peer_host=%s\n' "${PEER_HOST:-unset}"
  printf 'state=%s\n' "$state"
  if service_is_active; then
    printf 'service=active\n'
  else
    printf 'service=inactive\n'
  fi
  if [[ -f "$HEARTBEAT_FILE" ]]; then
    heartbeat_age=$(( $(date +%s) - $(stat -f '%m' "$HEARTBEAT_FILE" 2>/dev/null || echo 0) ))
    printf 'peer_last_seen=%s\n' "$(date -r "$HEARTBEAT_FILE" '+%Y-%m-%d %H:%M:%S')"
    printf 'heartbeat_age_seconds=%s\n' "$heartbeat_age"
  else
    printf 'peer_last_seen=never\n'
    printf 'heartbeat_age_seconds=never\n'
  fi
}

watch_loop() {
  while true; do
    if peer_is_healthy; then
      touch "$HEARTBEAT_FILE"
      if [[ $(read_state) == "active" ]] && ! service_is_active; then
        log "Peer is healthy, but the local service is not active. Starting it."
        service_start
      fi
    else
      if [[ ! -f "$HEARTBEAT_FILE" ]]; then
        promote_self
      else
        local age
        age=$(( $(date +%s) - $(stat -f '%m' "$HEARTBEAT_FILE" 2>/dev/null || echo 0) ))
        if (( age >= FAIL_TIMEOUT )); then
          promote_self
        fi
      fi
    fi

    sleep "$POLL_INTERVAL"
  done
}

init_config() {
  mkdir -p "$(dirname "$CONFIG_FILE")"
  if [[ ! -f "$CONFIG_FILE" ]]; then
    cat > "$CONFIG_FILE" <<'EOF'
# Customize this file for each host.
SERVICE_NAME=sop
PEER_HOST=backup-host
PEER_USER=root
POLL_INTERVAL=10
FAIL_TIMEOUT=30
EOF
  fi
}

load_config

case "${1:-watch}" in
  watch)
    watch_loop
    ;;
  promote)
    promote_self
    ;;
  demote)
    demote_self
    ;;
  status)
    status_report
    ;;
  init)
    init_config
    ;;
  *)
    cat <<EOF
Usage: $SCRIPT_NAME {watch|promote|demote|status|init}

Examples:
  $SCRIPT_NAME init
  $SCRIPT_NAME status
  $SCRIPT_NAME watch
EOF
    exit 1
    ;;
esac
