.PHONY: build run test vet smoke logs docker-build docker-run docker-stop clean

IMAGE := markitdown
DB_VOLUME := markitdown-data
# SQLite lives at /data inside the container; override for local runs if needed.
DB_URL := file:/data/markitdown.db?_busy_timeout=5000&_foreign_keys=on

# Local development uses the SQLite database at data/pageturner.db.
build:
	go build -o bin/markitdown ./cmd/server

run:
	go run ./cmd/server

test:
	go test ./...

smoke:
	bash scripts/smoke.sh 8909

vet:
	go vet ./...

logs:
	docker logs -f markitdown

# Single-image production flow (no compose): build once, run anywhere.
docker-build:
	docker build -t $(IMAGE) .

docker-run:
	docker run -d --name markitdown -p 8909:8909 --env-file .env \
		-e DATABASE_URL="$(DB_URL)" \
		-v $(DB_VOLUME):/data --restart unless-stopped $(IMAGE)

docker-stop:
	docker rm -f markitdown

clean:
	rm -rf bin
