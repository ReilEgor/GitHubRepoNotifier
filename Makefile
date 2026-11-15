# Every target delegates to scripts/check.sh, the same script the git hooks and CI call.
# On Windows run make from Git Bash, so that `sh` is available.

GOLANGCI_LINT_VERSION := v2.12.1
GO_ARCH_LINT_VERSION  := v1.15.0
GREMLINS_VERSION      := v0.6.0
WIRE_VERSION          := v0.7.0
PROTOC_GEN_GO_VERSION := v1.36.11
PROTOC_GEN_GO_GRPC_VERSION := v1.6.1
BUF_VERSION           := v1.71.0

CHECK := sh scripts/check.sh

.PHONY: help setup fmt fmt-check lint arch unit test-integration test build generate generate-check smoke e2e cover cover-unit mutation check run-subscription run-tracking run-notification clean

help: ## Show available targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | sed -E 's/:.*## /\t/' | awk -F '\t' '{ printf "  %-18s %s\n", $$1, $$2 }'

setup: ## Install pinned tools and enable git hooks
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	go install github.com/fe3dback/go-arch-lint@$(GO_ARCH_LINT_VERSION)
	go install github.com/google/wire/cmd/wire@$(WIRE_VERSION)
	go install github.com/go-gremlins/gremlins/cmd/gremlins@$(GREMLINS_VERSION)
	go install github.com/bufbuild/buf/cmd/buf@$(BUF_VERSION)
	go install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@$(PROTOC_GEN_GO_GRPC_VERSION)
	git config core.hooksPath .githooks
	chmod +x .githooks/* scripts/*.sh

fmt: ## Format the code
	@$(CHECK) fmt

fmt-check: ## Fail if the code is not formatted
	@$(CHECK) fmt-check

lint: ## Run the linter and static analysis
	@$(CHECK) lint

arch: ## Check layer boundaries
	@$(CHECK) arch

unit: ## Run unit tests with the race detector (no Docker)
	@$(CHECK) unit

test-integration: ## Run integration tests on a real PostgreSQL (needs Docker)
	@$(CHECK) integration

test: ## Run unit and integration tests (needs Docker)
	@$(CHECK) test

build: ## Build all services into bin/
	@$(CHECK) build

generate: ## Regenerate protobuf and wire code
	@$(CHECK) generate

generate-check: ## Fail if generated code is out of date
	@$(CHECK) generate-check

smoke: ## Docker Compose smoke test: migrations, services, subscribe-confirm-list-unsubscribe (needs Docker, runs in CI)
	@$(CHECK) smoke

e2e: ## Playwright tests against the whole system on Docker Compose (needs Docker and Node.js)
	@$(CHECK) e2e

cover: ## Statement coverage per package from unit + integration tests (needs Docker); COVER_MIN=80 fails below 80%
	@$(CHECK) cover

cover-unit: ## Statement coverage per package from unit tests only (no Docker)
	@$(CHECK) cover-unit

mutation: ## Mutation testing with gremlins on the critical packages; reports in reports/mutation
	@$(CHECK) mutation

check: fmt-check lint arch unit build ## Run everything the hooks run (no Docker)

run-subscription: build ## Run the subscription service
	@bin/subscription

run-tracking: build ## Run the tracking service
	@bin/tracking

run-notification: build ## Run the notification service
	@bin/notification

clean: ## Remove build output
	rm -rf bin
