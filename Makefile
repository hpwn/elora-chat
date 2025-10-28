SHELL := /bin/bash

ENV_FILE ?= .env
ifneq (,$(wildcard $(ENV_FILE)))
include $(ENV_FILE)
export $(shell sed -n 's/^\([A-Za-z_][A-Za-z0-9_]*\)=.*/\1/p' $(ENV_FILE))
endif

COMPOSE_FILE ?= docker-compose.yml
DOCKER_COMPOSE ?= docker compose
COMPOSE := $(DOCKER_COMPOSE) -f $(COMPOSE_FILE)

DEFAULT_HTTP_PORT := $(if $(ELORA_HTTP_PORT),$(ELORA_HTTP_PORT),8080)
DEFAULT_WS_URL := ws://localhost:$(DEFAULT_HTTP_PORT)/ws/chat
WS_URL ?= $(if $(VITE_PUBLIC_WS_URL),$(VITE_PUBLIC_WS_URL),$(DEFAULT_WS_URL))
API_URL ?= $(if $(VITE_PUBLIC_API_BASE),$(VITE_PUBLIC_API_BASE),http://localhost:$(DEFAULT_HTTP_PORT))

DOCKER_NETWORK ?= $(if $(ELORA_DOCKER_NETWORK),$(ELORA_DOCKER_NETWORK),bridge)
WEBSOCAT_IMAGE ?= ghcr.io/vi/websocat:1.12.0
JQ_IMAGE ?= ghcr.io/jqlang/jq:1.7.1
PYTHON_IMAGE ?= python:3.11-slim
WS_FILTER_SCRIPT ?= $(CURDIR)/scripts/ws_filter.py

.PHONY: bootstrap up down logs ws ws-twitch ws-youtube ws-filter seed:marker seed:burst health healthz readyz configz

bootstrap:
$(COMPOSE) pull --ignore-pull-failures
$(COMPOSE) build

up:
$(COMPOSE) up -d --remove-orphans

down:
$(COMPOSE) down --remove-orphans

logs:
$(COMPOSE) logs -f $(SERVICES)

ws:
	$(MAKE) ws-filter PLATFORM=

ws-twitch:
	$(MAKE) ws-filter PLATFORM=Twitch

ws-youtube:
	$(MAKE) ws-filter PLATFORM=YouTube

ws-filter:
	@if [ ! -f "$(WS_FILTER_SCRIPT)" ]; then \
	echo "Missing ws filter script at $(WS_FILTER_SCRIPT)" >&2; \
	exit 1; \
	fi
	@docker run --rm -i --network $(DOCKER_NETWORK) $(WEBSOCAT_IMAGE) -E --ping-interval=20 "$(WS_URL)" | \
	docker run --rm -i -e PLATFORM="$(PLATFORM)" -v "$(WS_FILTER_SCRIPT)":/ws_filter.py:ro $(PYTHON_IMAGE) python -u /ws_filter.py

seed\:marker:
curl -fsS -X POST "$(API_URL)/api/dev/seed/marker"

seed\:burst:
curl -fsS -X POST "$(API_URL)/api/dev/seed/burst"

healthz:
	@curl -fsS "$(API_URL)/healthz" && echo " (health OK)"

readyz:
	@curl -fsS "$(API_URL)/readyz" && echo " (ready OK)"

health:
	@curl -fsS "$(API_URL)/readyz" && echo " (ready OK)"

configz:
	@curl -fsS "$(API_URL)/configz" | docker run --rm -i $(JQ_IMAGE) .
