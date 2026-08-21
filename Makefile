.PHONY: build run test vet smoke logs docker-build docker-up docker-down clean

# Local development uses the SQLite database at data/pageturner.db.
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

logs:
	docker compose logs -f app

# Production image / stack
docker-build:
	docker compose build app

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

clean:
	rm -rf bin
