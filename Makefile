# Everything runs in containers: only Docker (with Compose) and make are needed.

COMPOSE_TEST := docker compose -f docker-compose.test.yml

.DEFAULT_GOAL := help
.PHONY: help up down clean logs ps test test-backend test-frontend reseed psql

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-14s %s\n", $$1, $$2}'

up: ## Build and start the stack in the background
	docker compose up -d --build --wait

down: ## Stop the stack (data is kept)
	docker compose down

clean: ## Stop the stack and delete all data
	docker compose down -v --remove-orphans

logs: ## Follow logs of all services
	docker compose logs -f

ps: ## Show service status
	docker compose ps -a

test: test-backend test-frontend ## Run all tests in containers

# Each test target cleans up the throwaway Postgres and still exits with the
# test command's own status, so a failing test fails `make test`.
test-backend: ## Go unit + integration tests (throwaway Postgres, race detector on)
	@$(COMPOSE_TEST) build backend-test
	@$(COMPOSE_TEST) run --rm backend-test; status=$$?; \
		$(COMPOSE_TEST) down -v --remove-orphans >/dev/null 2>&1; \
		exit $$status

test-frontend: ## Vitest component tests
	@$(COMPOSE_TEST) build frontend-test
	@$(COMPOSE_TEST) run --rm frontend-test; status=$$?; \
		$(COMPOSE_TEST) down -v --remove-orphans >/dev/null 2>&1; \
		exit $$status

reseed: ## Delete all users and seed again
	docker compose run --rm seed /app/seed -reset

psql: ## Open a SQL shell in the database
	docker compose exec postgres psql -U scc -d scc
