#!/bin/sh
set -eu
NAME="ndweather-smoke-$$"
VOL="ndweather-smoke-data-$$"
TOKEN=smoke-test-token-0123456789abcdef
cleanup(){ docker rm -f "$NAME" >/dev/null 2>&1 || true; docker volume rm "$VOL" >/dev/null 2>&1 || true; }
probe(){ docker exec "$NAME" wget -qO- "http://127.0.0.1:8080$1"; }
trap cleanup EXIT INT TERM
docker volume create "$VOL" >/dev/null
docker run -d --name "$NAME" --network none -e INGEST_TOKEN="$TOKEN" -e DEMO_MODE=true -v "$VOL:/data" internal-weather-system:1.0.0 >/dev/null
for _ in $(seq 1 30);do if probe /health/ready >/dev/null 2>&1;then break;fi;sleep 1;done
probe /health/ready >/dev/null
probe /health/live >/dev/null
probe /api/v1/observations/latest | grep -q HQ
[ "$(docker inspect -f '{{.Config.User}}' "$NAME")" = "10001:10001" ]
docker stop -t 10 "$NAME" >/dev/null
docker rm "$NAME" >/dev/null
docker run -d --name "$NAME" --network none -e INGEST_TOKEN="$TOKEN" -v "$VOL:/data" internal-weather-system:1.0.0 >/dev/null
for _ in $(seq 1 30);do if probe /health/ready >/dev/null 2>&1;then break;fi;sleep 1;done
probe /health/ready >/dev/null
probe /api/v1/observations/latest | grep -q HQ
probe /display | grep -q '내부 기상정보'
echo 'smoke test passed (offline network, persistence, non-root, health, display, SIGTERM)'
