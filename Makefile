include .env
export


.PHONY: up down restart build logs ps \
        migrate-up migrate-down migrate-status \
        clean \
		run build test clean

# Go
run:
	go run ./cmd/api

build:
	go build -o bin/api ./cmd/api

test:
	go test ./...

clean:
	rm -rf bin/

# Docker Compose
up:
	docker compose up -d

down:
	docker compose down

restart:
	docker compose restart

build:
	docker compose build

logs:
	docker compose logs -f

ps:
	docker compose ps

# Postgres migrations (goose)
migrate-up:
	goose -dir ${MIGRATIONS_DIR} postgres ${DB_URL} up

migrate-down: 
	goose -dir ${MIGRATIONS_DIR} postgres ${DB_URL} down

migrate-status: 
	goose -dir ${MIGRATIONS_DIR} postgres ${DB_URL} status

# Remove containers and volumes
clean: 
	docker compose down -v --remove-orphans