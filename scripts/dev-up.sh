#!/usr/bin/env bash
# 本地一键启动（Git Bash / WSL / macOS / Linux）
#
# 分层：docker compose 起 PG/Redis → 迁移（可选）→ goreman 起 api+rpc
#
# 用法：
#   ./scripts/dev-up.sh                  # compose + 起服务
#   ./scripts/dev-up.sh --migrate        # 含迁移
#   ./scripts/dev-up.sh --skip-compose   # 已起过 compose，只起进程（连云库时用这个）
#   ./scripts/dev-up.sh --help
#
# 与 `make dev` 的区别：本脚本会**等 PG 真正就绪**（pg_isready 轮询）再往下走；
# `make dev` 直接拉起，库没起来时 migrate 会失败。
# 连云库请配合 scripts/cloud/start-tunnel.ps1（Windows）或自建 ssh -L 隧道。
#
# 停服务：Ctrl+C

set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

MIGRATE=0
SKIP_COMPOSE=0
for arg in "$@"; do
  case "$arg" in
    --migrate) MIGRATE=1 ;;
    --skip-compose) SKIP_COMPOSE=1 ;;
    -h|--help)
      echo "Usage: $0 [--migrate] [--skip-compose]"
      exit 0
      ;;
    *)
      echo "unknown arg: $arg" >&2
      exit 1
      ;;
  esac
done

if [[ ! -f .env ]]; then
  echo "无 .env，从 .env.example 复制"
  cp .env.example .env
fi

set -a
# shellcheck disable=SC1091
source .env
set +a
export APP_ENV="${APP_ENV:-dev}"

if [[ "$SKIP_COMPOSE" -eq 0 ]]; then
  echo "[compose] docker compose up -d ..."
  docker compose up -d
  echo "[compose] waiting for postgres ..."
  for i in $(seq 1 60); do
    if docker compose exec -T postgres pg_isready -U "${DB_USER:-postgres}" -d "${DB_NAME:-postgres}" >/dev/null 2>&1; then
      break
    fi
    sleep 1
  done
fi

if [[ "$MIGRATE" -eq 1 ]]; then
  if [[ -z "${DB_DSN:-}" ]]; then
    echo "ERROR: DB_DSN empty in .env" >&2
    exit 1
  fi
  command -v migrate >/dev/null || go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.17.1
  echo "[migrate] up ..."
  migrate -path ./migrations -database "$DB_DSN" up
fi

command -v goreman >/dev/null || go install github.com/mattn/goreman@v0.3.15
echo "[app] make run-all (goreman) APP_ENV=$APP_ENV"
exec make run-all
