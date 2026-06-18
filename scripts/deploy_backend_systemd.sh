#!/usr/bin/env bash
set -euo pipefail

DEPLOY_DIR="${DEPLOY_DIR:-/opt/paper}"
SERVICE_NAME="${SERVICE_NAME:-paper.service}"
SERVER_PORT="${SERVER_PORT:-8002}"
HEALTH_URL="${HEALTH_URL:-http://127.0.0.1:8002/health}"
HEALTH_RETRIES="${HEALTH_RETRIES:-60}"
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

diagnose_service() {
  log "systemd status for ${SERVICE_NAME}"
  systemctl status "$SERVICE_NAME" --no-pager || true
  log "recent journal for ${SERVICE_NAME}"
  journalctl -u "$SERVICE_NAME" -n 120 --no-pager || true
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
CREATED_UNIT=0
if ! systemctl cat "$SERVICE_NAME" >/dev/null 2>&1; then
  log "create systemd unit /etc/systemd/system/${SERVICE_NAME}"
  cat >"/etc/systemd/system/${SERVICE_NAME}" <<EOF
[Unit]
Description=Paper backend service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=${DEPLOY_DIR}
EnvironmentFile=-${DEPLOY_DIR}/.env
Environment=SERVER_PORT=${SERVER_PORT}
ExecStart=${APP_PATH}
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
  CREATED_UNIT=1
fi
mkdir -p "/etc/systemd/system/${SERVICE_NAME}.d"
cat >"/etc/systemd/system/${SERVICE_NAME}.d/override.conf" <<EOF
[Service]
WorkingDirectory=${DEPLOY_DIR}
EnvironmentFile=-${DEPLOY_DIR}/.env
Environment=SERVER_PORT=${SERVER_PORT}
EOF
systemctl daemon-reload
if [ "$CREATED_UNIT" -eq 1 ]; then
  systemctl enable "$SERVICE_NAME"
fi

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
systemctl stop "$SERVICE_NAME" || true

log "install new binary -> ${APP_PATH}"
install -m 0755 -o root -g root "$ARTIFACT_PATH" "$APP_PATH"

log "start ${SERVICE_NAME}"
if ! systemctl start "$SERVICE_NAME"; then
  diagnose_service
  fail "failed to start ${SERVICE_NAME}"
fi

log "waiting health check: ${HEALTH_URL}"
for i in $(seq 1 "$HEALTH_RETRIES"); do
  if curl -fsS "$HEALTH_URL" >/dev/null; then
    trap - ERR
    log "deploy success"
    systemctl status "$SERVICE_NAME" --no-pager
    exit 0
  fi
  log "health check attempt ${i}/${HEALTH_RETRIES} failed"
  sleep 2
done

rollback
diagnose_service
fail "service did not become healthy after $((HEALTH_RETRIES * 2)) seconds"
