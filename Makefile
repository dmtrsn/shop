GO ?= $(if $(wildcard /usr/local/go/bin/go),/usr/local/go/bin/go,go)
GOFMT ?= $(if $(wildcard /usr/local/go/bin/gofmt),/usr/local/go/bin/gofmt,gofmt)
APP_NAME ?= sweetshop
BIN_DIR ?= bin
WEB_DIR ?= web
MIGRATIONS_DIR ?= migrations
RUN_MIGRATIONS ?= true
DOCKER_COMPOSE ?= docker compose
FIRST_PARTY_GO_PACKAGES := ./cmd/... ./internal/... ./api/... ./tests/...

.DEFAULT_GOAL := help

.PHONY: help run run-memory run-postgres build test test-race vet fmt fmt-check check \
	docker-build compose-config compose-up compose-down compose-logs test-integration check-api check-js

help: ## Показать доступные команды
	@awk 'BEGIN {FS = ":.*## "; printf "Использование: make <цель>\n\n"} /^[a-zA-Z0-9_-]+:.*## / {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

run: run-memory ## Запустить локально с временным in-memory хранилищем

run-memory: ## Запустить без PostgreSQL; данные сбросятся после рестарта
	APP_MODE=demo DATABASE_URL= WEB_DIR="$(WEB_DIR)" MIGRATIONS_DIR="$(MIGRATIONS_DIR)" $(GO) run ./cmd/server

run-postgres: ## Запустить с PostgreSQL (обязательно передать DATABASE_URL)
	@test -n "$(DATABASE_URL)" || (echo "DATABASE_URL не задан" >&2; exit 1)
	@DATABASE_URL="$(DATABASE_URL)" RUN_MIGRATIONS="$(RUN_MIGRATIONS)" WEB_DIR="$(WEB_DIR)" MIGRATIONS_DIR="$(MIGRATIONS_DIR)" $(GO) run ./cmd/server

build: ## Собрать production-бинарник в bin/
	mkdir -p "$(BIN_DIR)"
	CGO_ENABLED=0 $(GO) build -buildvcs=false -trimpath -ldflags="-s -w" -o "$(BIN_DIR)/$(APP_NAME)" ./cmd/server

test: ## Запустить unit- и integration-style тесты без внешней БД
	$(GO) test $(FIRST_PARTY_GO_PACKAGES)

test-race: ## Запустить тесты с детектором гонок
	$(GO) test -race $(FIRST_PARTY_GO_PACKAGES)

vet: ## Выполнить статическую проверку go vet
	$(GO) vet $(FIRST_PARTY_GO_PACKAGES)

fmt: ## Отформатировать Go-код
	$(GOFMT) -w cmd internal api tests

fmt-check: ## Проверить gofmt без изменения файлов
	@files="$$($(GOFMT) -l cmd internal api tests)"; if [ -n "$$files" ]; then echo "Требуется gofmt:"; echo "$$files"; exit 1; fi

check: fmt-check vet test check-js ## Выполнить основные проверки проекта

docker-build: ## Собрать OCI-образ приложения
	docker build -t "$(APP_NAME):local" .

compose-config: ## Проверить итоговую конфигурацию Docker Compose
	$(DOCKER_COMPOSE) config --quiet

compose-up: ## Собрать и запустить приложение с PostgreSQL
	$(DOCKER_COMPOSE) up --build -d

compose-down: ## Остановить контейнеры, сохранив данные PostgreSQL
	$(DOCKER_COMPOSE) down

compose-logs: ## Следить за логами приложения и PostgreSQL
	$(DOCKER_COMPOSE) logs -f app postgres

test-integration: ## Run race-enabled tests against a disposable PostgreSQL cluster
	GO="$(GO)" bash scripts/test-integration.sh

check-api: ## Validate OpenAPI and exercise every current HTTP operation
	$(GO) test -count=1 ./api

check-js: ## Validate legacy JavaScript syntax
	node --check web/app.js
