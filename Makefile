# Root build entry points. Parts of the repo that don't exist yet are skipped.
SHELL := /bin/bash
.PHONY: protocol test lint build smoke loadtest

HAS_GO  := $(wildcard server/go.mod)
HAS_NPM := $(wildcard client/package.json)
HAS_GEN := $(wildcard protocol/gen/generate.sh)

protocol:
ifneq ($(HAS_GEN),)
	protocol/gen/generate.sh
else
	@echo "protocol: skipped (protocol/gen/generate.sh not found)"
endif

test:
ifneq ($(HAS_GO),)
	cd server && go test ./...
else
	@echo "test: server skipped (server/go.mod not found)"
endif
ifneq ($(HAS_NPM),)
	cd client && npm test -- --run
else
	@echo "test: client skipped (client/package.json not found)"
endif

lint:
ifneq ($(HAS_GO),)
	cd server && go vet ./...
else
	@echo "lint: server skipped (server/go.mod not found)"
endif
ifneq ($(HAS_NPM),)
	cd client && npm run lint
else
	@echo "lint: client skipped (client/package.json not found)"
endif

build:
ifneq ($(HAS_GO),)
	cd server && go build ./...
else
	@echo "build: server skipped (server/go.mod not found)"
endif
ifneq ($(HAS_NPM),)
	cd client && npm run build
else
	@echo "build: client skipped (client/package.json not found)"
endif

smoke:
	@echo "smoke: not available yet"

loadtest:
	@echo "loadtest: not available yet"
	@exit 1
