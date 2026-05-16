#!/usr/bin/env bash
set -Eeuo pipefail

APP_DIR=${APP_DIR:-/opt/sub2api}
STATE_DIR=${STATE_DIR:-/etc/sub2api/bluegreen}
SERVICE_TEMPLATE=${SERVICE_TEMPLATE:-sub2api@}
CADDY_ACTIVE=${CADDY_ACTIVE:-$STATE_DIR/active-upstream.caddy}
HEALTH_PATH=${HEALTH_PATH:-/ready}
DRAIN_SECONDS=${DRAIN_SECONDS:-120}
SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)

usage() {
  cat <<'USAGE'
Usage:
  deploy-bluegreen.sh install
  deploy-bluegreen.sh status
  deploy-bluegreen.sh deploy --binary /path/to/sub2api [--version name] [--drain-seconds seconds]
  deploy-bluegreen.sh switch --to blue|green [--drain-seconds seconds]
  deploy-bluegreen.sh rollback [--drain-seconds seconds]

Environment:
  APP_DIR=/opt/sub2api
  STATE_DIR=/etc/sub2api/bluegreen
  DRAIN_SECONDS=120
USAGE
}

die() {
  echo "ERROR: $*" >&2
  exit 1
}

need_root() {
  if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
    die "run as root"
  fi
}

instance_port() {
  case "$1" in
    blue) echo 5091 ;;
    green) echo 5092 ;;
    *) die "invalid instance: $1" ;;
  esac
}

other_instance() {
  case "$1" in
    blue) echo green ;;
    green) echo blue ;;
    *) die "invalid instance: $1" ;;
  esac
}

service_name() {
  echo "${SERVICE_TEMPLATE}$1.service"
}

active_instance() {
  if [[ -f "$STATE_DIR/active" ]]; then
    local active
    active=$(tr -d '[:space:]' < "$STATE_DIR/active")
    if [[ "$active" == "blue" || "$active" == "green" ]]; then
      echo "$active"
      return
    fi
  fi

  if [[ -f "$CADDY_ACTIVE" ]]; then
    if grep -q '127\.0\.0\.1:5092' "$CADDY_ACTIVE"; then
      echo green
      return
    fi
    if grep -q '127\.0\.0\.1:5091' "$CADDY_ACTIVE"; then
      echo blue
      return
    fi
  fi

  echo blue
}

write_active_upstream() {
  local instance=$1
  local port
  port=$(instance_port "$instance")
  install -d -m 0755 "$STATE_DIR"
  cat > "$CADDY_ACTIVE" <<EOF
reverse_proxy 127.0.0.1:${port} {
	health_uri ${HEALTH_PATH}
	health_interval 10s
	health_timeout 3s
	health_status 200
	lb_try_duration 5s
	lb_try_interval 250ms
	transport http {
		keepalive 120s
		keepalive_idle_conns 256
		compression off
	}
}
EOF
  echo "$instance" > "$STATE_DIR/active"
}

reload_caddy() {
  if ! command -v caddy >/dev/null 2>&1; then
    die "caddy is not installed"
  fi
  caddy validate --config /etc/caddy/Caddyfile
  systemctl reload caddy
}

wait_ready() {
  local instance=$1
  local port
  port=$(instance_port "$instance")
  local url="http://127.0.0.1:${port}${HEALTH_PATH}"
  for _ in $(seq 1 60); do
    if curl -fsS --max-time 2 "$url" >/dev/null; then
      return 0
    fi
    sleep 1
  done
  return 1
}

install_files() {
  need_root
  install -d -m 0755 "$APP_DIR" "$APP_DIR/releases" "$STATE_DIR"
  install -m 0644 "$SCRIPT_DIR/sub2api@.service" /etc/systemd/system/sub2api@.service
  install -m 0644 "$SCRIPT_DIR/blue.env" "$STATE_DIR/blue.env"
  install -m 0644 "$SCRIPT_DIR/green.env" "$STATE_DIR/green.env"
  if [[ ! -f "$CADDY_ACTIVE" ]]; then
    install -m 0644 "$SCRIPT_DIR/active-upstream.caddy" "$CADDY_ACTIVE"
    echo blue > "$STATE_DIR/active"
  fi
  install -d -m 0755 /etc/caddy
  if [[ -f /etc/caddy/Caddyfile && ! -f /etc/caddy/Caddyfile.backup ]]; then
    cp /etc/caddy/Caddyfile /etc/caddy/Caddyfile.backup
  fi
  install -m 0644 "$SCRIPT_DIR/Caddyfile" /etc/caddy/Caddyfile
  systemctl daemon-reload
  echo "installed blue-green files"
}

deploy_binary() {
  need_root
  local binary=
  local version=
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --binary) binary=${2:-}; shift 2 ;;
      --version) version=${2:-}; shift 2 ;;
      --drain-seconds) DRAIN_SECONDS=${2:-}; shift 2 ;;
      *) die "unknown argument: $1" ;;
    esac
  done
  [[ -n "$binary" ]] || die "--binary is required"
  [[ -f "$binary" ]] || die "binary not found: $binary"
  [[ -x "$binary" ]] || chmod +x "$binary"

  if [[ -z "$version" ]]; then
    version=$(date +%Y%m%d_%H%M%S)
  fi

  local active target release_dir
  active=$(active_instance)
  target=$(other_instance "$active")
  release_dir="$APP_DIR/releases/$version"

  install -d -m 0755 "$release_dir"
  install -m 0755 "$binary" "$release_dir/sub2api"
  ln -sfn "$release_dir" "$APP_DIR/current-$target"
  chown -h sub2api:sub2api "$APP_DIR/current-$target" || true
  chown -R sub2api:sub2api "$release_dir"

  systemctl restart "$(service_name "$target")"
  if ! wait_ready "$target"; then
    systemctl status "$(service_name "$target")" --no-pager || true
    die "$target did not become ready"
  fi

  switch_to "$target" "$DRAIN_SECONDS"
}

switch_to() {
  need_root
  local target=$1
  local drain=${2:-$DRAIN_SECONDS}
  local previous
  previous=$(active_instance)
  [[ "$target" == "blue" || "$target" == "green" ]] || die "target must be blue or green"
  [[ "$target" != "$previous" ]] || die "$target is already active"

  wait_ready "$target" || die "$target is not ready"
  write_active_upstream "$target"
  reload_caddy

  echo "$previous" > "$STATE_DIR/previous"
  echo "switched traffic: $previous -> $target"
  echo "waiting ${drain}s before stopping $previous"
  sleep "$drain"
  systemctl stop "$(service_name "$previous")" || true
}

rollback() {
  need_root
  [[ -f "$STATE_DIR/previous" ]] || die "no previous instance recorded"
  local previous
  previous=$(tr -d '[:space:]' < "$STATE_DIR/previous")
  systemctl start "$(service_name "$previous")"
  wait_ready "$previous" || die "$previous is not ready"
  switch_to "$previous" "$DRAIN_SECONDS"
}

status() {
  local active
  active=$(active_instance)
  echo "active=$active"
  for instance in blue green; do
    local port
    port=$(instance_port "$instance")
    echo "[$instance] port=$port service=$(systemctl is-active "$(service_name "$instance")" 2>/dev/null || true)"
    curl -fsS --max-time 2 "http://127.0.0.1:${port}${HEALTH_PATH}" 2>/dev/null || true
    echo
  done
}

main() {
  local cmd=${1:-}
  [[ -n "$cmd" ]] || { usage; exit 1; }
  shift || true
  case "$cmd" in
    install) install_files "$@" ;;
    status) status "$@" ;;
    deploy) deploy_binary "$@" ;;
    switch)
      [[ ${1:-} == "--to" ]] || die "switch requires --to blue|green"
      local target=${2:-}
      shift 2
      while [[ $# -gt 0 ]]; do
        case "$1" in
          --drain-seconds) DRAIN_SECONDS=${2:-}; shift 2 ;;
          *) die "unknown argument: $1" ;;
        esac
      done
      switch_to "$target" "$DRAIN_SECONDS"
      ;;
    rollback)
      while [[ $# -gt 0 ]]; do
        case "$1" in
          --drain-seconds) DRAIN_SECONDS=${2:-}; shift 2 ;;
          *) die "unknown argument: $1" ;;
        esac
      done
      rollback
      ;;
    -h|--help|help) usage ;;
    *) die "unknown command: $cmd" ;;
  esac
}

main "$@"
