#!/bin/sh
set -eu
BASE_URL=${BASE_URL:-http://localhost:8080}; INGEST_TOKEN=${INGEST_TOKEN:?set INGEST_TOKEN}; SITE_CODE=${SITE_CODE:-HQ}
NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ); ID="WEATHER-$(date -u +%Y%m%d-%H%M%S)-001"
cat <<JSON | curl --fail-with-body -sS -H "Authorization: Bearer $INGEST_TOKEN" -H 'Content-Type: application/json' --data-binary @- "$BASE_URL/api/v1/ingest/batches"
{"schemaVersion":"1.0","batchId":"$ID","source":"DEMO-SCRIPT","createdAt":"$NOW","records":{"observations":[{"siteCode":"$SITE_CODE","observedAt":"$NOW","temperature":24.2,"humidity":61,"windDirection":"남서","windSpeed":2.8,"gustSpeed":5.2,"precipitation":0,"precipitationState":"없음","sky":"맑음"}],"forecasts":[],"warnings":[],"typhoons":[]},"assets":[]}
JSON
