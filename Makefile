APP_NAME := mailmate
BUILD_DIR := bin
GO := go
GOFLAGS := -trimpath
LDFLAGS := -s -w -X main.version=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

.PHONY: all build run test lint clean migrate docker-build docker-up docker-down help

all: lint test build

build:
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME) ./cmd/server

run: build
	./$(BUILD_DIR)/$(APP_NAME)

dev:
	$(GO) run ./cmd/server

test:
	$(GO) test -race -cover -coverprofile=coverage.out ./...

test-verbose:
	$(GO) test -race -v -cover ./...

coverage: test
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

lint:
	golangci-lint run ./...

clean:
	rm -rf $(BUILD_DIR) coverage.out coverage.html

migrate-up:
	@echo "Running migrations..."
	psql "$$DATABASE_URL" -f migrations/001_initial.sql

docker-build:
	docker build -t $(APP_NAME):latest .

docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f

helm-install:
	helm install $(APP_NAME) deploy/helm/$(APP_NAME) -f deploy/helm/$(APP_NAME)/values.yaml

helm-upgrade:
	helm upgrade $(APP_NAME) deploy/helm/$(APP_NAME) -f deploy/helm/$(APP_NAME)/values.yaml

helm-uninstall:
	helm uninstall $(APP_NAME)

help:
	@echo "Available targets:"
	@echo "  build         - Build the binary"
	@echo "  run           - Build and run"
	@echo "  dev           - Run with go run (development)"
	@echo "  test          - Run tests with coverage"
	@echo "  lint          - Run linter"
	@echo "  clean         - Remove build artifacts"
	@echo "  migrate-up    - Run database migrations"
	@echo "  docker-build  - Build Docker image"
	@echo "  docker-up     - Start with Docker Compose"
	@echo "  docker-down   - Stop Docker Compose"
	@echo "  helm-install  - Install Helm chart"
	@echo "  helm-upgrade  - Upgrade Helm release"
