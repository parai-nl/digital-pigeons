.PHONY: help build build-pin fmt-check test verify cover e2e dev release check bundle shell-static import-kb kb-eval visual-check

# Shipping and its evidence must use the exact go.mod dependency. An ignored
# go.work may point at a different local auth/audio API; `make build` remains
# available for an explicit local-core compile check.
test cover e2e dev release: export GOWORK=off

help: ## Toon deze hulp
	@grep -E '^[a-z0-9-]+:.*##' $(MAKEFILE_LIST) | awk -F ':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'

build: ## Compile-check zonder gebouwde frontend (dev-tag)
	go build -tags dev ./...

build-pin: ## Compile-check tegen de gepinde parai-core uit de module-cache (negeert go.work)
	@# De lokale core-checkout kan ouder zijn dan de pin in go.mod; deze stap
	@# bewijst dat de module ook tegen de pin compileert (CI heeft geen go.work).
	GOWORK=off go build -tags dev ./...

fmt-check: ## Faalt zodra er ongeformatteerde Go-code in de module staat
	@# Deze poort hangt onder `test` en dus onder `make verify`, zodat een
	@# gofmt-afwijking al bij het testen opvalt. gofmt loopt de
	@# map zelf af, dus vendor/ en bin/ gaan er expliciet uit — dat is
	@# buildmateriaal en geen broncode van deze module.
	@uit=$$(gofmt -l . 2>/dev/null | grep -v -e '^vendor/' -e '^bin/' || true); \
	if [ -n "$$uit" ]; then \
		echo "gofmt: niet geformatteerd (draai 'gofmt -w' op deze bestanden):"; \
		echo "$$uit"; \
		exit 1; \
	fi

test: build-pin fmt-check ## Go-tests tegen de gepinde core (GOWORK=off; dev-tag swapt go:embed voor os.DirFS)
	@# GOWORK=off staat bovenaan dit bestand en is hier geen detail: een kaal
	@# `go test ./duif/` pakt de core-checkout uit go.work, en loopt die niet
	@# gelijk met de pin in go.mod, dan compileren tests die nieuwere core-API
	@# gebruiken niet.
	@# Zie README.md, sectie "Bouwen en controleren".
	go test -tags dev ./...

verify: test check ## Alles: Go-tests, pin-check, vitest en svelte-check

cover: ## Coverage van de docker-vrije tests (Postgres-repo + boot komen uit `make e2e`)
	go test -tags dev -count=1 -covermode=set -coverprofile=coverage-unit.out ./...
	@go tool cover -func=coverage-unit.out | tail -n1

e2e: ## Boot-e2e: wegwerp-Postgres via docker + stub-LLM + echte serverboot (levert het volledige coveragecijfer)
	@bash scripts/e2e.sh

dev: bundle ## Run met -tags skipauth (zero-friction lokaal; vereist PARAI_DB_URL); bouwt eerst de frontend
	@# Bewust zonder de dev-tag: de dev-variant van parai-core verwacht de
	@# core-checkout als werkmap en boot daarbuiten niet. `go run` hercompileert
	@# toch bij elke start, dus go:embed pikt frontendwijzigingen vanzelf op.
	@if [ -f .env ]; then set -o allexport; . ./.env; set +o allexport; fi; \
	go run -tags skipauth .

release: bundle ## Shipping-binary: bouwt eerst de frontend, dan zonder tags → bin/duif
	@# De volgorde is de hele truc. Zonder de dev-tag embedt embed.go
	@# duif/frontend/dist: is die map leeg op het moment van compileren, dan
	@# levert de binary een lege Assets() en blijft de module onzichtbaar in de
	@# shell (Init logt daar een waarschuwing over, maar dan is het te laat).
	@# Ook geen skipauth: dit is de echte auth-keten van parai-core.
	go build -trimpath -o bin/duif .
	@echo "bin/duif gebouwd (met embedded frontend, personaconfig en migraties)"

import-kb: ## Boot met een eenmalige kennisbank-import: make import-kb KB=/pad/naar/bundel.json
	@# De bundel staat buiten de repo; daarom een variabele en geen default. Vervangen in plaats van bijwerken:
	@#   DUIF_IMPORT_KB_FORCE=1 make import-kb KB=…
	@test -n "$(KB)" || { echo "geef het bundelpad mee: make import-kb KB=/pad/naar/bundel.json"; exit 1; }
	@test -f "$(KB)" || { echo "bundel niet gevonden: $(KB)"; exit 1; }
	DUIF_IMPORT_KB="$(KB)" $(MAKE) dev

kb-eval: ## Kalibratie van DUIF_MIN_SIMILARITY tegen de geïmporteerde KB (live; laadt .env)
	@# De relevantiedrempel. De set staat in duif/eval/kb-eval.yaml (buiten de embedded
	@# config, dus niet in de binary); de test schrijft JSON-regels naar
	@# DUIF_EVAL_OUT (default stdout) en print de histogrammen plus de tabel per
	@# drempel. Zonder database of proxy in de omgeving slaat hij zichzelf over.
	@#   DUIF_EVAL_OUT=/pad/eval.jsonl make kb-eval   # regels naar een bestand
	@#   DUIF_EVAL_ANSWERS=1 make kb-eval             # ook de dure antwoordtoets
	@if [ -f .env ]; then set -o allexport; . ./.env; set +o allexport; fi; \
	go test -tags "dev live" -run TestLive_KBEval -v ./duif/

bundle: ## Frontend-build (bun + Vite lib-build) → duif/frontend/dist/bundle.js
	@# @parai/ui komt via een vite-alias uit de sibling-checkout
	@# ../../../../parai-ui, relatief aan duif/frontend (override: PARAI_UI_DIR).
	@# @parai/graph komt op dezelfde manier uit ../../../../parai-graph
	@# (override: PARAI_GRAPH_DIR) — importeren, niet kopiëren.
	@# dist/ is gitignored en wordt bij dev en release gebouwd.
	@# --frozen-lockfile: een build mag bun.lock nooit stilletjes herschrijven —
	@# dan bouwt CI andere afhankelijkheden dan de repo vastlegt.
	cd duif/frontend && bun install --silent --frozen-lockfile && bun run build

check: ## Frontend: vitest (jsdom + @testing-library/svelte) en svelte-check
	cd duif/frontend && bun install --silent --frozen-lockfile && bun run test && bun run check

visual-check: ## Headless visuele controle van het vergaderscherm (aparte devserver nodig; niet in verify)
	@# Bewust buiten `verify`: dit vraagt een draaiende Vite-devserver met een
	@# eigen Go-API erachter. Zeg dus waar hij staat; zonder variabele probeert
	@# het script de standaardpoort van Vite.
	@#   cd duif/frontend && DUIF_API_PROXY=http://127.0.0.1:8080 bun run dev --port 5173
	@#   DUIF_VISUEEL_BASE=http://127.0.0.1:5173 make visual-check
	node ../tools/meeting-visueel-check.mjs

PARAI_CORE_DIR ?= ../../parai-core
shell-static: ## Kopieer de parai-core-shell-build (frontend/build) naar static/ voor de volledige shell lokaal
	@test -f "$(PARAI_CORE_DIR)/frontend/build/index.html" || { echo "geen shell-build gevonden in $(PARAI_CORE_DIR)/frontend/build (zet PARAI_CORE_DIR of bouw core's frontend)"; exit 1; }
	@find static -mindepth 1 -not -name .gitkeep -delete
	@cp -R "$(PARAI_CORE_DIR)/frontend/build/." static/
	@echo "shell gekopieerd naar static/ (gitignored); make dev serveert nu de volledige shell"
