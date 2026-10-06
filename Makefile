# Every target delegates to scripts/check.sh, the same script the git hooks and CI call.
# On Windows run make from Git Bash, so that `sh` is available.

GOLANGCI_LINT_VERSION := v2.12.1
GO_ARCH_LINT_VERSION  := v1.15.0

CHECK := sh scripts/check.sh

.PHONY: help setup fmt fmt-check lint arch test build smoke check run-subscription run-tracking run-notification clean

help: ## Show available targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | sed -E 's/:.*## /\t/' | awk -F '\t' '{ printf "  %-18s %s\n", $$1, $$2 }'

setup: ## Install pinned tools and enable git hooks
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	go install github.com/fe3dback/go-arch-lint@$(GO_ARCH_LINT_VERSION)
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

test: ## Run unit tests with the race detector
	@$(CHECK) test

build: ## Build all services into bin/
	@$(CHECK) build

smoke: ## Build, start every service and check /health and /version
	@$(CHECK) smoke

check: fmt-check lint arch test smoke ## Run everything the hooks run

run-subscription: build ## Run the subscription service
	@bin/subscription

run-tracking: build ## Run the tracking service
	@bin/tracking

run-notification: build ## Run the notification service
	@bin/notification

clean: ## Remove build output
	rm -rf bin
