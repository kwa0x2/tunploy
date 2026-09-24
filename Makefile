SHELL := /bin/bash

BINARY     := bin/tunploy
DEV_BINARY := bin/tunploy-dev
VERSION    ?= dev
DATA_DIR   ?= ./data
PORT       ?= 3000

# HTTPS on high ports: no root needed, and nothing else on the laptop is in the way.
GO_ENV := TUNPLOY_DATA_DIR=$(DATA_DIR) TUNPLOY_LISTEN=127.0.0.1:$(PORT) \
	TUNPLOY_HTTPS_LISTEN=127.0.0.1:8443 TUNPLOY_HTTP_LISTEN=127.0.0.1:8080

.PHONY: help all dev api run admin build web docker test lint fmt reset clean

help:
	@echo "Tunploy"
	@echo ""
	@echo "  make dev     API + Vite dev server together, hot reload (http://localhost:5173)"
	@echo "  make api     Go API only, for use with your own frontend server"
	@echo "  make run     Full stack from the embedded UI (http://127.0.0.1:$(PORT))"
	@echo "  make admin   Create the admin account in $(DATA_DIR)"
	@echo "  make build   Compile the frontend into $(BINARY)"
	@echo "  make docker  Build the tunploy:$(VERSION) container image"
	@echo "  make test    Go tests plus a TypeScript type check"
	@echo "  make lint    go vet, gofmt and oxlint"
	@echo "  make reset   Delete $(DATA_DIR), so you start without an account"
	@echo "  make clean   Remove build output"

all: build

# Runs the compiled binary and vite itself rather than `go run` and `npm run`:
# those wrappers die without taking their children, orphaning both ports.
dev: web/node_modules
	@go build -o $(DEV_BINARY) ./cmd/tunploy
	@$(GO_ENV) TUNPLOY_LOG_LEVEL=debug $(DEV_BINARY) & api=$$!; \
	web/node_modules/.bin/vite web & ui=$$!; \
	trap "kill $$api $$ui 2>/dev/null" EXIT INT TERM; \
	wait

api:
	$(GO_ENV) TUNPLOY_LOG_LEVEL=debug go run ./cmd/tunploy

run: web
	$(GO_ENV) go run ./cmd/tunploy

admin:
	$(GO_ENV) go run ./cmd/tunploy admin create

build: web
	go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY) ./cmd/tunploy

docker:
	docker build --build-arg VERSION=$(VERSION) -t tunploy:$(VERSION) .

web: web/node_modules
	cd web && npm run build

web/node_modules: web/package-lock.json
	cd web && npm install
	@touch web/node_modules

test:
	go test -race ./...
	cd web && npx tsc -b

lint:
	go vet ./...
	@test -z "$$(gofmt -l cmd internal)" || { echo "gofmt needed:"; gofmt -l cmd internal; exit 1; }
	cd web && npm run lint

fmt:
	gofmt -w cmd internal

reset:
	rm -rf $(DATA_DIR)

clean:
	rm -rf $(BINARY) $(DEV_BINARY) internal/web/dist
	@mkdir -p internal/web/dist && touch internal/web/dist/.gitkeep
