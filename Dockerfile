# syntax=docker/dockerfile:1.7
FROM node:22.18.0-alpine AS web
WORKDIR /src/frontend
COPY frontend/package*.json ./
RUN npm install --ignore-scripts
COPY frontend/ ./
RUN npm run build

FROM golang:1.24.6-alpine3.22 AS backend
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY backend ./backend
COPY config ./config
COPY --from=web /src/frontend/dist ./web
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/weather ./backend/cmd/weather

FROM alpine:3.22.1
RUN addgroup -g 10001 weather && adduser -D -H -u 10001 -G weather weather && mkdir -p /app/config /app/web /data && chown -R weather:weather /data
WORKDIR /app
COPY --from=backend /out/weather /app/weather
COPY --from=backend /src/config /app/config
COPY --from=backend /src/web /app/web
USER 10001:10001
ENV APP_ENV=production BIND_ADDRESS=0.0.0.0 PORT=8080 DATA_DIR=/data SQLITE_PATH=/data/weather.db ASSET_DIR=/data/assets INBOX_DIR=/data/inbox SITES_CONFIG=/app/config/sites.yaml
EXPOSE 8080
ENTRYPOINT ["/app/weather"]
