#!/usr/bin/env bash
set -euo pipefail

DEPLOY_DIR="${DEPLOY_DIR:-/opt/paper}"
SERVICE_NAME="${SERVICE_NAME:-paper.service}"
HEALTH_URL="${HEALTH_URL:-http://127.0.0.1:8002/health}"
ARTIFACT_PATH="${1:-/tmp/paper-server.new}"
APP_PATH="${DEPLOY_DIR}/paper-server"
BACKUP_PATH="${APP_PATH}.backup-$(date +%Y%m%d-%H%M%S)"

log() {
  printf '[deploy] %s\n' "$*"
}

fail() {
  printf '[deploy][ERROR] %s\n' "$*" >&2
  exit 1
}

if [ "$(id -u)" -ne 0 ]; then
  fail "must run as root, because systemctl and /opt/paper need root permission"
fi

if [ ! -f "$ARTIFACT_PATH" ]; then
  fail "artifact not found: ${ARTIFACT_PATH}"
fi

command -v systemctl >/dev/null 2>&1 || fail "systemctl not found"
command -v curl >/dev/null 2>&1 || fail "curl not found"

mkdir -p "$DEPLOY_DIR"

if [ -f "$APP_PATH" ]; then
  log "backup ${APP_PATH} -> ${BACKUP_PATH}"
  cp "$APP_PATH" "$BACKUP_PATH"
fi

rollback() {
  if [ -f "$BACKUP_PATH" ]; then
    log "health check failed, rolling back to ${BACKUP_PATH}"
    systemctl stop "$SERVICE_NAME" || true
    cp "$BACKUP_PATH" "$APP_PATH"
    chmod +x "$APP_PATH"
    chown root:root "$APP_PATH"
    systemctl start "$SERVICE_NAME" || true
  fi
}

trap rollback ERR

log "stop ${SERVICE_NAME}"
systemctl stop "$SERVICE_NAME"

log "install new binary -> ${APP_PATH}"
install -m 0755 -o root -g root "$ARTIFACT_PATH" "$APP_PATH"

log "start ${SERVICE_NAME}"
systemctl start "$SERVICE_NAME"

log "waiting health check: ${HEALTH_URL}"
for i in $(seq 1 30); do
  if curl -fsS "$HEALTH_URL" >/dev/null; then
    trap - ERR
    log "deploy success"
    systemctl status "$SERVICE_NAME" --no-pager
    exit 0
  fi
  sleep 2
done

rollback
fail "service did not become healthy after 60 seconds"
