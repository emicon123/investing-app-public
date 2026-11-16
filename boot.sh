#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

COMPOSE="docker compose -f infra/compose.yml --env-file infra/.env"

case "${1:-up}" in
  up)
    echo "Starting investing-app..."
    $COMPOSE up --build -d
    echo ""
    echo "Services starting. Check status with:  ./boot.sh status"
    echo "Logs:                                  ./boot.sh logs"
    echo "App will be available at:              http://your-hostname.tail-xxxxx.ts.net"
    ;;
  down)
    echo "Stopping investing-app..."
    $COMPOSE down
    ;;
  restart)
    $COMPOSE down
    $COMPOSE up --build -d
    ;;
  logs)
    $COMPOSE logs -f "${2:-}"
    ;;
  status)
    $COMPOSE ps
    ;;
  rebuild)
    # Rebuild a single service without touching others: ./boot.sh rebuild backend
    $COMPOSE up --build -d "${2:?Usage: ./boot.sh rebuild <service>}"
    ;;
  *)
    echo "Usage: $0 {up|down|restart|logs [service]|status|rebuild <service>}"
    exit 1
    ;;
esac
