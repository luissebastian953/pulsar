COMPOSE ?= docker compose
PROFILE := $(if $(GPU),--profile gpu)

.DEFAULT_GOAL := help
.PHONY: help dev down migrate seed-tags fetch-models eval test run-core run-orbit run-comfy psql hooks

help: ## list targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-12s %s\n", $$1, $$2}'

dev: ## start infra: postgres (GPU=1 adds local ComfyUI)
	$(COMPOSE) $(PROFILE) up -d
	@echo "infra up — next: make migrate, then make run-core / make run-orbit in separate terminals"

down: ## stop everything, all profiles
	$(COMPOSE) --profile gpu --profile full down

migrate: ## apply db/migrations/*.sql in order (forward-only, numbered)
	./db/migrate.sh

seed-tags: ## Danbooru -> tag DB (slow, idempotent; MIN_POST_COUNT=50 to tune floor)
	cd core && go run ./cmd/seed-tags

fetch-models: ## download pinned model weights into comfy/models (~7 GB, resumable, sha256-verified)
	./comfy/fetch-models.sh

eval: ## tuning harness: prompt/seed sweeps -> contact sheets
	@echo "eval: not implemented yet (build before the NoobAI swap — see docs/tuning.md)"; exit 1

test: ## core suite; set PULSAR_TEST_DATABASE_URL to include integration + e2e
	cd core && go vet ./... && go test ./...

run-core: ## run the Go service natively
	cd core && go run ./cmd/server

run-orbit: ## run the Nuxt dev server natively
	cd orbit && pnpm dev

run-comfy: ## run ComfyUI natively (MPS on Apple Silicon; the docker gpu profile is the deploy path)
	./comfy/run-native.sh

hooks: ## enable the repo's git hooks (conventional-commit check; needs commitizen)
	git config core.hooksPath .githooks

psql: ## shell into the dev database
	$(COMPOSE) exec postgres psql -U $${POSTGRES_USER:-pulsar} -d $${POSTGRES_DB:-pulsar}
