COMPOSE := docker compose
DEPLOY_COMPOSE := $(COMPOSE) -f deploy/compose.yaml
LOCAL_PORT ?= 8080
PRODUCTION_VOLUME := go-website_game-data
BETA_VOLUME := go-website_beta-game-data
LOCAL_VOLUME := go-website_local-game-data

.DEFAULT_GOAL := help

.PHONY: help status restart-local rebuild-local restart-production reset-local reset-production reset-beta

help:
	@echo "Available commands:"
	@echo "  make status              Show local and deployed containers"
	@echo "  make restart-local       Restart the existing port-8080 local app"
	@echo "  make rebuild-local       Rebuild and recreate the port-8080 local app"
	@echo "  make restart-production  Restart the production service"
	@echo "  make reset-local         Erase local data and restart local"
	@echo "  make reset-production    Erase production data and restart production"
	@echo "  make reset-beta          Erase beta data and restart beta"

status:
	@echo "Local stack:"
	@$(COMPOSE) ps -a
	@echo
	@echo "Deployment stack:"
	@$(DEPLOY_COMPOSE) ps -a

restart-local:
	@$(COMPOSE) restart app

rebuild-local:
	@$(COMPOSE) up --build --detach --no-deps app

restart-production:
	@$(DEPLOY_COMPOSE) restart production

reset-local:
	@printf "This permanently erases LOCAL game data. Type 'reset-local' to continue: "; \
	read answer; \
	[ "$$answer" = "reset-local" ]
	@$(COMPOSE) stop app
	@conflicts="$$(docker ps --filter publish=$(LOCAL_PORT) --format '{{.Names}}')"; \
	if [ -n "$$conflicts" ]; then \
		echo "Cannot reset local: port $(LOCAL_PORT) is still used by: $$conflicts" >&2; \
		echo "Stop that container, then run make reset-local again." >&2; \
		exit 1; \
	fi
	@docker run --rm -v $(LOCAL_VOLUME):/data alpine rm -f /data/game.db
	@$(COMPOSE) up --build --detach --no-deps app
	@echo "Local database cleared and local restarted."

reset-production:
	@printf "This permanently erases PRODUCTION game data. Type 'reset-production' to continue: "; \
	read answer; \
	[ "$$answer" = "reset-production" ]
	@$(DEPLOY_COMPOSE) stop production
	@docker run --rm -v $(PRODUCTION_VOLUME):/data alpine rm -f /data/game.db
	@$(DEPLOY_COMPOSE) start production
	@echo "Production database cleared and production restarted."

reset-beta:
	@printf "This permanently erases BETA game data. Type 'reset-beta' to continue: "; \
	read answer; \
	[ "$$answer" = "reset-beta" ]
	@$(DEPLOY_COMPOSE) stop beta
	@docker run --rm -v $(BETA_VOLUME):/data alpine rm -f /data/game.db
	@$(DEPLOY_COMPOSE) start beta
	@echo "Beta database cleared and beta restarted."
