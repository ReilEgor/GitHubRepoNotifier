#!/bin/sh
# Single entry point for every project check.
# The Makefile, the git hooks and CI all call this script, so they run exactly the same commands.
#
# Usage: sh scripts/check.sh <fmt|fmt-check|lint|arch|unit|integration|test|build|generate|generate-check|smoke>
#
#   unit        fast tests, no Docker
#   integration tests on a real PostgreSQL started by Testcontainers (needs Docker)
#   test        unit + integration
#   smoke       Docker Compose: PostgreSQL, Redis, migrations, the services, and the user flow through the API

set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"

MODULES="shared services/subscription services/tracking services/notification"
INTEGRATION_MODULES="shared services/subscription services/tracking"
SERVICES="subscription tracking notification"
BUILDINFO=github.com/ReilEgor/GitHubRepoNotifier/shared/buildinfo
BIN=bin

# Generated code that must match its source; generate-check fails when it does not.
GENERATED="shared/infrastructure/grpc services/subscription/cmd/subscription/wire_gen.go services/tracking/cmd/tracking/wire_gen.go"

# The smoke test runs on its own Compose project and ports, chosen not to clash with a locally running system.
SMOKE_PROJECT=reponotifier-smoke
SMOKE_API_KEY=smoke-key
SMOKE_PORT_subscription=18080
SMOKE_PORT_tracking=18081
SMOKE_PORT_notification=18082
SMOKE_GRPC_PORT=19090
SMOKE_DB_subscription="postgres://subscription:subscription@127.0.0.1:15432/subscription_db?sslmode=disable"
SMOKE_DB_tracking="postgres://tracker:tracker@127.0.0.1:15433/tracker_db?sslmode=disable"

step() { printf '\n==> %s\n' "$*"; }

commit() { git rev-parse --verify -q HEAD 2>/dev/null || echo dev; }

need() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "error: $1 is not installed; run 'make setup'" >&2
		exit 1
	}
}

cmd_fmt() {
	need golangci-lint
	for m in $MODULES; do
		step "format $m"
		(cd "$m" && golangci-lint fmt)
	done
}

cmd_fmt_check() {
	need golangci-lint
	for m in $MODULES; do
		step "format check $m"
		(cd "$m" && golangci-lint fmt --diff) || {
			echo "error: $m is not formatted; run 'make fmt'" >&2
			exit 1
		}
	done
}

# The integration build tag is linted too, so the test harness and the integration tests are checked.
cmd_lint() {
	need golangci-lint
	for m in $MODULES; do
		step "lint $m"
		(cd "$m" && golangci-lint run --timeout=5m --build-tags integration)
	done
}

cmd_arch() {
	need go-arch-lint
	for m in $MODULES; do
		step "layer boundaries $m"
		go-arch-lint check --project-path "./$m"
	done
}

cmd_unit() {
	for m in $MODULES; do
		step "unit tests $m"
		(cd "$m" && go test -race ./...)
	done
}

cmd_integration() {
	need docker
	docker info >/dev/null 2>&1 || {
		echo "error: Docker is not running; start Docker Desktop" >&2
		exit 1
	}
	# The Ryuk cleanup container does not start under Docker Desktop on Windows; the harness stops its own container.
	if [ "$(go env GOOS)" = windows ]; then
		export TESTCONTAINERS_RYUK_DISABLED=true
	fi
	for m in $INTEGRATION_MODULES; do
		step "integration tests $m"
		(cd "$m" && go test -race -count=1 -tags integration ./...)
	done
}

cmd_test() {
	cmd_unit
	cmd_integration
}

cmd_build() {
	version=$(commit)
	ext=$(go env GOEXE)
	mkdir -p "$BIN"
	for s in $SERVICES; do
		step "build $s ($version)"
		go build -ldflags "-X $BUILDINFO.Commit=$version" -o "$BIN/$s$ext" "./services/$s/cmd/$s"
	done
}

cmd_generate() {
	need buf
	need wire
	step "protobuf"
	(cd api/proto && buf generate)
	for s in subscription tracking; do
		step "wire $s"
		(cd "services/$s/cmd/$s" && wire)
	done
}

# Regenerates the code and fails if that changes anything, i.e. if a committed generated file is stale.
cmd_generate_check() {
	cmd_generate
	step "generated code is up to date"
	# shellcheck disable=SC2086
	if [ -n "$(git status --porcelain -- $GENERATED)" ]; then
		echo "error: generated code is out of date; run 'make generate' and commit the result" >&2
		# shellcheck disable=SC2086
		git status --short -- $GENERATED >&2
		exit 1
	fi
}

# fetch URL: prints the response body, fails on a non-2xx status.
fetch() { curl -fsS --max-time 3 "$1"; }

compose() { docker compose -p "$SMOKE_PROJECT" "$@"; }

smoke_cleanup() {
	for p in $smoke_pids; do kill "$p" 2>/dev/null || true; done
	compose down --remove-orphans >/dev/null 2>&1 || true
}

# wait_healthy NAME PORT
wait_healthy() {
	tries=0
	until fetch "http://127.0.0.1:$2/health" >/dev/null 2>&1; do
		tries=$((tries + 1))
		if [ "$tries" -ge 40 ]; then
			echo "error: $1 did not become healthy; log:" >&2
			cat "$BIN/$1.smoke.log" >&2
			exit 1
		fi
		sleep 0.5
	done
}

# expect_status DESCRIPTION WANT URL [curl args...]: fails unless the HTTP status equals WANT.
expect_status() {
	desc=$1 want=$2 url=$3
	shift 3
	got=$(curl -sS --max-time 5 -o "$BIN/smoke.body" -w '%{http_code}' "$@" "$url")
	if [ "$got" != "$want" ]; then
		echo "error: $desc: got $got, want $want; body: $(cat "$BIN/smoke.body")" >&2
		exit 1
	fi
	echo "$desc -> $got"
}

cmd_smoke() {
	need curl
	need docker
	cmd_build
	version=$(commit)
	ext=$(go env GOEXE)
	smoke_pids=""
	trap smoke_cleanup EXIT INT TERM

	step "start PostgreSQL and Redis"
	compose up -d --wait postgres-subscription postgres-tracker redis
	step "apply migrations"
	compose --profile migrate run --rm migrate-subscription
	compose --profile migrate run --rm migrate-tracker
	# tracking knows golang/go from the seed, so the smoke test needs no access to GitHub.
	compose exec -T postgres-tracker psql -v ON_ERROR_STOP=1 -q -U tracker -d tracker_db \
		<services/tracking/migrations/testdata/seed.sql

	step "start the services"
	redis_env="REDIS_HOST=127.0.0.1 REDIS_PORT=16379"
	# shellcheck disable=SC2086
	env $redis_env APP_API_KEY="$SMOKE_API_KEY" DB_URL="$SMOKE_DB_tracking" APP_GRPC_PORT="$SMOKE_GRPC_PORT" \
		WORKER_HEALTH_PORT="$SMOKE_PORT_tracking" "$BIN/tracking$ext" >"$BIN/tracking.smoke.log" 2>&1 &
	smoke_pids="$smoke_pids $!"
	# shellcheck disable=SC2086
	env $redis_env APP_API_KEY="$SMOKE_API_KEY" DB_URL="$SMOKE_DB_subscription" APP_HTTP_PORT="$SMOKE_PORT_subscription" \
		TRACKING_GRPC_ADDR="127.0.0.1:$SMOKE_GRPC_PORT" "$BIN/subscription$ext" >"$BIN/subscription.smoke.log" 2>&1 &
	smoke_pids="$smoke_pids $!"
	SENDER_HEALTH_PORT="$SMOKE_PORT_notification" "$BIN/notification$ext" >"$BIN/notification.smoke.log" 2>&1 &
	smoke_pids="$smoke_pids $!"

	for s in $SERVICES; do
		eval "port=\$SMOKE_PORT_$s"
		step "smoke $s on :$port"
		wait_healthy "$s" "$port"
		echo "/health  -> 200"

		body=$(fetch "http://127.0.0.1:$port/version")
		case "$body" in
		*"\"$version\""*) echo "/version -> $version" ;;
		*)
			echo "error: $s reports $body, expected version $version" >&2
			exit 1
			;;
		esac
	done

	step "user flow: subscribe, confirm, list, unsubscribe"
	api="http://127.0.0.1:$SMOKE_PORT_subscription/api/v1"
	email=smoke@example.com
	expect_status "subscribe" 202 "$api/subscribe" -X POST -H "X-API-Key: $SMOKE_API_KEY" \
		-H 'Content-Type: application/json' -d "{\"email\":\"$email\",\"repository\":\"golang/go\"}"
	# No email is sent yet, so the token is read straight from the database.
	token=$(compose exec -T postgres-subscription psql -U subscription -d subscription_db -tA \
		-c "SELECT s.token FROM subscriptions s JOIN users u ON u.id = s.user_id WHERE u.email = '$email'")
	[ -n "$token" ] || {
		echo "error: no token stored for $email" >&2
		exit 1
	}
	expect_status "confirm" 200 "$api/confirm/$token"
	expect_status "list" 200 "$api/subscriptions?email=$email" -H "X-API-Key: $SMOKE_API_KEY"
	case "$(cat "$BIN/smoke.body")" in
	*'"confirmed":true'*) ;;
	*)
		echo "error: the subscription is not confirmed: $(cat "$BIN/smoke.body")" >&2
		exit 1
		;;
	esac
	expect_status "unsubscribe" 200 "$api/unsubscribe/$token"
	expect_status "unsubscribe again" 404 "$api/unsubscribe/$token"
	expect_status "list after unsubscribe" 200 "$api/subscriptions?email=$email" -H "X-API-Key: $SMOKE_API_KEY"
	case "$(cat "$BIN/smoke.body")" in
	*'"total":0'*) ;;
	*)
		echo "error: the subscription is still listed: $(cat "$BIN/smoke.body")" >&2
		exit 1
		;;
	esac

	step "smoke passed"
}

case "${1:-}" in
fmt) cmd_fmt ;;
fmt-check) cmd_fmt_check ;;
lint) cmd_lint ;;
arch) cmd_arch ;;
unit) cmd_unit ;;
integration) cmd_integration ;;
test) cmd_test ;;
build) cmd_build ;;
generate) cmd_generate ;;
generate-check) cmd_generate_check ;;
smoke) cmd_smoke ;;
*)
	echo "usage: sh scripts/check.sh <fmt|fmt-check|lint|arch|unit|integration|test|build|generate|generate-check|smoke>" >&2
	exit 2
	;;
esac
