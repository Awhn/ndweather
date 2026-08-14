.PHONY: test build docker-build smoke-test
test:
	go test ./...
	cd frontend && npm test
build:
	cd frontend && npm run build
	rm -rf web && cp -r frontend/dist web
	CGO_ENABLED=0 go build -o bin/weather ./backend/cmd/weather
docker-build:
	docker build -t internal-weather-system:1.0.0 .
smoke-test: docker-build
	./scripts/smoke-test.sh
