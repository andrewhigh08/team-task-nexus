.PHONY: build run test test-integration test-coverage lint migrate-up migrate-down docker-up docker-down

APP_NAME=team-task-nexus
BUILD_DIR=bin
MAIN_PATH=./cmd/api

build:
	go build -o $(BUILD_DIR)/api $(MAIN_PATH)

run:
	go run $(MAIN_PATH)

test:
	go test ./internal/... -v -count=1

test-integration:
	go test ./test/integration/... -v -count=1 -tags=integration

test-coverage:
	go test ./internal/... -coverprofile=coverage.out -covermode=atomic
	go tool cover -func=coverage.out
	go tool cover -html=coverage.out -o coverage.html

lint:
	golangci-lint run ./...

migrate-up:
	migrate -path migrations -database "mysql://app:apppassword@tcp(localhost:3306)/team_task_nexus" up

migrate-down:
	migrate -path migrations -database "mysql://app:apppassword@tcp(localhost:3306)/team_task_nexus" down

docker-up:
	docker-compose up --build -d

docker-down:
	docker-compose down

docker-logs:
	docker-compose logs -f app

clean:
	rm -rf $(BUILD_DIR) coverage.out coverage.html
