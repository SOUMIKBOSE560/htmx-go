.PHONY: build run test vet smoke db-up db-down logs docker-build docker-up docker-down clean

# Local development (expects Postgres on localhost:5432, e.g. via `make db-up`)
build:
	go build -o bin/pageturner ./cmd/server

run:
	go run ./cmd/server

test:
	go test ./...

smoke:
	bash scripts/smoke.sh 8090

vet:
	go vet ./...

# Postgres via Docker
db-up:
	docker compose up -d db

db-down:
	docker compose down

logs:
	docker compose logs -f db

# Production image / stack
docker-build:
	docker compose build app

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

clean:
	rm -rf bin
