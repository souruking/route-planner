include .env
export


.PHONY: up down restart compose-build logs ps \
        migrate-up migrate-down migrate-status \
        clean \
		run build test clean \
		test-integration \
		fmt check

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

compose-build:
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

# Testing
test-integration:
	TEST_DATABASE_URL="$(TEST_DB)" go test -count=1 ./...

cover:
	TEST_DATABASE_URL="$(TEST_DB)" go test -p 1 -count=1 -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1
	go tool cover -html=coverage.out -o coverage.htm

# Checking
fmt:
	gofmt -w .

check:
	@echo "==> gofmt"
	@test -z "$$(gofmt -l .)" || (echo "Unformatted files (run 'make fmt'):"; gofmt -l .; exit 1)
	@echo "==> go vet"
	@go vet ./...
	@echo "==> go mod tidy"
	@go mod tidy -diff
	@echo "==> staticcheck"
	@staticcheck ./...
	@echo "==> build"
	@go build ./...
	@echo "==> tests (race, with database)"
	@TEST_DATABASE_URL="$(TEST_DB)" go test -p 1 -race -count=1 ./...
	@echo "All checks passed."