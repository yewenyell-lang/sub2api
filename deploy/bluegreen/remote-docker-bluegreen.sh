#!/bin/sh
set -eu

APP_DIR=${APP_DIR:-/opt/sub2api}
ENV_FILE=${ENV_FILE:-/tmp/sub2api-bluegreen-deploy.env}
if [ -f "$ENV_FILE" ]; then
  if command -v sed >/dev/null 2>&1; then
    sed -i 's/\r$//' "$ENV_FILE"
  fi
  . "$ENV_FILE"
fi
ARTIFACT=${ARTIFACT:-/tmp/sub2api-local-build.tar.gz}
VERSION=${VERSION:-local-build-$(date -u +%Y%m%d-%H%M%S)}
DRAIN_SECONDS=${DRAIN_SECONDS:-120}
STATE_FILE=${STATE_FILE:-$APP_DIR/bluegreen/state}
NGINX_UPSTREAM=${NGINX_UPSTREAM:-/etc/nginx/snippets/sub2api-upstream.conf}
IMAGE=${IMAGE:-sub2api:$VERSION}

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

active_instance() {
  if [ -f "$STATE_FILE" ]; then
    awk '{print $1}' "$STATE_FILE"
    return
  fi
  if grep -q '127\.0\.0\.1:8081' "$NGINX_UPSTREAM" 2>/dev/null; then
    echo green
    return
  fi
  echo blue
}

instance_port() {
  case "$1" in
    blue) echo 8080 ;;
    green) echo 8081 ;;
    *) fail "invalid instance: $1" ;;
  esac
}

other_instance() {
  case "$1" in
    blue) echo green ;;
    green) echo blue ;;
    *) fail "invalid instance: $1" ;;
  esac
}

service_name() {
  echo "sub2api-$1"
}

compose_file() {
  echo "$APP_DIR/bluegreen/$1/docker-compose.yml"
}

base_image() {
  local active=$1
  local active_container
  active_container=$(service_name "$active")
  if docker image inspect "${BASE_IMAGE:-}" >/dev/null 2>&1; then
    echo "$BASE_IMAGE"
    return
  fi
  if docker inspect "$active_container" >/dev/null 2>&1; then
    docker inspect --format '{{.Config.Image}}' "$active_container"
    return
  fi
  if docker inspect sub2api >/dev/null 2>&1; then
    docker inspect --format '{{.Config.Image}}' sub2api
    return
  fi
  docker images --format '{{.Repository}}:{{.Tag}}' | awk '/^sub2api:/ {print; exit}'
}

update_compose_image() {
  local file=$1
  local image=$2
  python3 - "$file" "$image" <<'PY'
from pathlib import Path
import re
import sys

path = Path(sys.argv[1])
image = sys.argv[2]
text = path.read_text(encoding="utf-8")
text, count = re.subn(r"image:\s*[^\n]+", f"image: {image}", text, count=1)
if count != 1:
    raise SystemExit(f"failed to update image in {path}")
path.write_text(text, encoding="utf-8")
PY
}

wait_ready() {
  local port=$1
  for _ in $(seq 1 60); do
    if curl -fsS --max-time 3 "http://127.0.0.1:$port/ready" >/dev/null; then
      return 0
    fi
    sleep 2
  done
  return 1
}

need docker
need python3
need curl
test -f "$ARTIFACT" || fail "artifact not found: $ARTIFACT"
test -f "$APP_DIR/.env" || fail "missing $APP_DIR/.env"
test -d "$APP_DIR/bluegreen/blue" || fail "missing blue compose directory"
test -d "$APP_DIR/bluegreen/green" || fail "missing green compose directory"

active=$(active_instance)
target=$(other_instance "$active")
active_port=$(instance_port "$active")
target_port=$(instance_port "$target")
target_service=$(service_name "$target")
target_compose=$(compose_file "$target")
base=$(base_image "$active")
test -n "$base" || fail "no base image found"
test -f "$target_compose" || fail "missing target compose: $target_compose"

build_root="/tmp/sub2api-local-artifact-$VERSION"
rm -rf "$build_root"
mkdir -p "$build_root"
tar -xzf "$ARTIFACT" -C "$build_root"
test -f "$build_root/sub2api-linux-amd64" || fail "artifact missing sub2api-linux-amd64"
test -d "$build_root/resources" || fail "artifact missing resources"
chmod +x "$build_root/sub2api-linux-amd64"

cat > "$build_root/Dockerfile" <<EOF
FROM $base
USER root
COPY sub2api-linux-amd64 /app/sub2api
COPY resources /app/resources
RUN chmod +x /app/sub2api && chown -R sub2api:sub2api /app/sub2api /app/resources
USER sub2api
EOF
docker build -t "$IMAGE" "$build_root"

backup_dir="$APP_DIR/backups/postgres"
mkdir -p "$backup_dir"
if docker ps --format '{{.Names}}' | grep -qx sub2api-postgres; then
  docker exec sub2api-postgres pg_dump -U sub2api -d sub2api | gzip > "$backup_dir/pre-deploy-$VERSION.sql.gz"
fi

cp "$target_compose" "$target_compose.bak-$VERSION"
update_compose_image "$target_compose" "$IMAGE"
docker compose -f "$target_compose" up -d "$target_service"

if ! wait_ready "$target_port"; then
  docker logs --tail 200 "$target_service" || true
  fail "$target_service did not become ready on port $target_port"
fi

printf 'set $sub2api_upstream http://127.0.0.1:%s;\n' "$target_port" > "$NGINX_UPSTREAM"
nginx -t
systemctl reload nginx
mkdir -p "$(dirname "$STATE_FILE")"
printf '%s %s\n' "$target" "$target_port" > "$STATE_FILE"

echo "switched traffic: $active:$active_port -> $target:$target_port"
echo "draining old instance for ${DRAIN_SECONDS}s"
sleep "$DRAIN_SECONDS"

old_service=$(service_name "$active")
docker stop "$old_service" >/dev/null 2>&1 || true
if [ "$active_port" = "8080" ]; then
  docker stop sub2api >/dev/null 2>&1 || true
fi

curl -fsS --max-time 5 "http://127.0.0.1:$target_port/health" >/dev/null
echo "IMAGE=$IMAGE"
echo "ACTIVE=$target $target_port"
echo "BACKUP=$backup_dir/pre-deploy-$VERSION.sql.gz"
