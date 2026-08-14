#!/bin/sh
set -eu
NAME=ndweather-smoke; VOL=ndweather-smoke-data; TOKEN=smoke-test-token-0123456789
cleanup(){ docker rm -f "$NAME" >/dev/null 2>&1 || true; };trap cleanup EXIT INT TERM
cleanup; docker volume create "$VOL" >/dev/null
docker run -d --name "$NAME" --network none -p 18080:8080 -e INGEST_TOKEN="$TOKEN" -e DEMO_MODE=true -v "$VOL:/data" internal-weather-system:1.0.0 >/dev/null
for i in $(seq 1 30);do curl -fsS http://localhost:18080/health/ready >/dev/null&&break;sleep 1;done
curl -fsS http://localhost:18080/health/live >/dev/null
[ "$(docker inspect -f '{{.Config.User}}' "$NAME")" = "10001:10001" ]
docker stop -t 10 "$NAME" >/dev/null; docker rm "$NAME" >/dev/null
docker run -d --name "$NAME" --network none -p 18080:8080 -e INGEST_TOKEN="$TOKEN" -v "$VOL:/data" internal-weather-system:1.0.0 >/dev/null
for i in $(seq 1 30);do curl -fsS http://localhost:18080/api/v1/observations/latest|grep -q SAMPLE&&break;sleep 1;done
curl -fsS http://localhost:18080/display | grep -q '내부 기상정보'
echo 'smoke test passed (offline network, persistence, non-root, health, display, SIGTERM)'
