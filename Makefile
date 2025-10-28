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

.PHONY: bootstrap up down logs ws ws:twitch ws:youtube seed:marker seed:burst healthz readyz

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
cd src/backend && go run ./cmd/wsprobe --ws-url "$(WS_URL)" $(if $(WS_PLATFORM),--platform "$(WS_PLATFORM)")

ws\:twitch:
$(MAKE) ws WS_PLATFORM=Twitch

ws\:youtube:
$(MAKE) ws WS_PLATFORM=YouTube

seed\:marker:
curl -fsS -X POST "$(API_URL)/api/dev/seed/marker"

seed\:burst:
curl -fsS -X POST "$(API_URL)/api/dev/seed/burst"

healthz:
@curl -fsS "$(API_URL)/healthz" && echo " (health OK)"

readyz:
@curl -fsS "$(API_URL)/readyz" && echo " (ready OK)"
