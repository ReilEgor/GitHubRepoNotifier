#!/bin/sh
# Single entry point for every project check.
# The Makefile, the git hooks and CI all call this script, so they run exactly the same commands.
#
# Usage: sh scripts/check.sh <fmt|fmt-check|lint|arch|test|build|smoke>

set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"

MODULES="shared services/subscription services/tracking services/notification"
SERVICES="subscription tracking notification"
BUILDINFO=github.com/ReilEgor/GitHubRepoNotifier/shared/buildinfo
BIN=bin

# Ports used only by the smoke test, chosen not to clash with a locally running system.
SMOKE_PORT_subscription=18080
SMOKE_PORT_tracking=18081
SMOKE_PORT_notification=18082
PORT_ENV_subscription=APP_HTTP_PORT
PORT_ENV_tracking=WORKER_HEALTH_PORT
PORT_ENV_notification=SENDER_HEALTH_PORT

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

cmd_lint() {
	need golangci-lint
	for m in $MODULES; do
		step "lint $m"
		(cd "$m" && golangci-lint run --timeout=5m)
	done
}

cmd_arch() {
	need go-arch-lint
	for m in $MODULES; do
		step "layer boundaries $m"
		go-arch-lint check --project-path "./$m"
	done
}

cmd_test() {
	for m in $MODULES; do
		step "test $m"
		(cd "$m" && go test -race ./...)
	done
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

# fetch URL: prints the response body, fails on a non-2xx status.
fetch() { curl -fsS --max-time 2 "$1"; }

cmd_smoke() {
	need curl
	cmd_build
	version=$(commit)
	ext=$(go env GOEXE)
	pids=""
	trap 'for p in $pids; do kill "$p" 2>/dev/null || true; done' EXIT INT TERM

	for s in $SERVICES; do
		eval "port=\$SMOKE_PORT_$s"
		eval "port_env=\$PORT_ENV_$s"
		env "$port_env=$port" "$BIN/$s$ext" >"$BIN/$s.smoke.log" 2>&1 &
		pids="$pids $!"
	done

	for s in $SERVICES; do
		eval "port=\$SMOKE_PORT_$s"
		step "smoke $s on :$port"

		tries=0
		until fetch "http://127.0.0.1:$port/health" >/dev/null 2>&1; do
			tries=$((tries + 1))
			if [ "$tries" -ge 30 ]; then
				echo "error: $s did not become healthy; log:" >&2
				cat "$BIN/$s.smoke.log" >&2
				exit 1
			fi
			sleep 0.5
		done
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

	step "smoke passed"
}

case "${1:-}" in
fmt) cmd_fmt ;;
fmt-check) cmd_fmt_check ;;
lint) cmd_lint ;;
arch) cmd_arch ;;
test) cmd_test ;;
build) cmd_build ;;
smoke) cmd_smoke ;;
*)
	echo "usage: sh scripts/check.sh <fmt|fmt-check|lint|arch|test|build|smoke>" >&2
	exit 2
	;;
esac
