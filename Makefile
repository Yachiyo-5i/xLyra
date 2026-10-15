SHELL := /bin/zsh

.PHONY: web-install web-dev web-build server-tidy server-run server-build docker-build docker-up docker-down docker-logs

web-install:
	cd web && pnpm install

web-dev:
	cd web && pnpm dev

web-build:
	cd web && pnpm build

server-tidy:
	cd server && go mod tidy

server-run:
	cd server && go run ./cmd/server

server-build:
	cd server && go build ./cmd/server

docker-build:
	docker compose build

docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f
