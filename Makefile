.PHONY: dev web api realtime provider-gateway test go-test web-test compose-infra compose-full compose-down

dev:
	pnpm dev

web:
	pnpm --filter @cineweave/web dev

api:
	go run ./apps/api

realtime:
	go run ./apps/realtime

provider-gateway:
	go run ./services/provider-gateway

test: go-test web-test

go-test:
	go test ./...

web-test:
	pnpm --filter @cineweave/web typecheck
	pnpm --filter @cineweave/web lint

compose-infra:
	docker compose --profile infra up -d

compose-full:
	docker compose --profile full up -d

compose-down:
	docker compose down --remove-orphans

