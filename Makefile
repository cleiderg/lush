# Root build entry points. Parts of the repo that don't exist yet are skipped.
SHELL := /bin/bash
.PHONY: protocol test lint build smoke loadtest bots deploy-check sentry-sourcemaps

HAS_GO  := $(wildcard server/go.mod)
HAS_NPM := $(wildcard client/package.json)
HAS_BOTS := $(wildcard tools/bots/go.mod)
HAS_GEN := $(wildcard protocol/gen/generate.sh)

# Bots can only import generated protocol code from their own module, so copy
# it in (the copy is gitignored).
bots:
	rm -rf tools/bots/internal/protocol && mkdir -p tools/bots/internal/protocol tools/bots/bin
	cp $(filter-out %_test.go,$(wildcard server/internal/protocol/*.go)) tools/bots/internal/protocol/
	cd tools/bots && go build -o bin/bots ./cmd/bots

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
ifneq ($(HAS_BOTS),)
	$(MAKE) --no-print-directory bots
	cd tools/bots && go test ./...
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
ifneq ($(HAS_BOTS),)
	$(MAKE) --no-print-directory bots
	cd tools/bots && go vet ./...
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
ifneq ($(and $(HAS_GO),$(HAS_BOTS)),)
	scripts/dev/smoke.sh
else
	@echo "smoke: skipped (server or tools/bots not found)"
endif

LOADTEST_OUT ?= loadtest-result.json
loadtest:
ifeq ($(HAS_BOTS),)
	@echo "loadtest: tools/bots not found"
	@exit 1
else ifeq ($(LOADTEST_URL),)
	@echo "loadtest: set LOADTEST_URL=<wss url>"
	@exit 1
else
	$(MAKE) --no-print-directory bots
	tools/bots/bin/bots -url "$(LOADTEST_URL)" -bots 1000 -radius 25 -ramp 30s -duration 60s -out "$(LOADTEST_OUT)"
endif

# Upload the client's source maps to Sentry for a release, then delete them so
# Cloudflare Pages never serves them. Reads SENTRY_AUTH_TOKEN, SENTRY_ORG and
# SENTRY_PROJECT from the environment; SENTRY_CLI overrides the pinned CLI.
SENTRY_DIST ?= client/dist
sentry-sourcemaps:
ifeq ($(SENTRY_RELEASE),)
	@echo "sentry-sourcemaps: set SENTRY_RELEASE=<sha>"
	@exit 1
else
	deploy/scripts/sentry-sourcemaps.sh "$(SENTRY_DIST)" "$(SENTRY_RELEASE)"
endif

# Checks for deploy/: shellcheck, the stubbed-ufw tests, and the systemd units
# and env templates. In CI (CI=true) a missing tool is a failure.
deploy-check:
	@if command -v shellcheck >/dev/null; then \
		shellcheck $$(find deploy -name '*.sh') scripts/dev/deploy-check.sh; \
	elif [ -n "$$CI" ]; then \
		echo "deploy-check: shellcheck not installed"; exit 1; \
	else \
		echo "deploy-check: shellcheck not installed, skipping (required in CI)"; \
	fi
	@for t in deploy/tests/*_test.sh; do echo "== $$t"; bash "$$t" || exit 1; done
	scripts/dev/deploy-check.sh --self-test
	scripts/dev/deploy-check.sh
