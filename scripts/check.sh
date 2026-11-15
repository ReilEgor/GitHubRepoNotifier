#!/bin/sh
# Single entry point for every project check.
# The Makefile, the git hooks and CI all call this script, so they run exactly the same commands.
#
# Usage: sh scripts/check.sh <fmt|fmt-check|lint|arch|unit|integration|test|cover|cover-unit|mutation|build|generate|generate-check|smoke|e2e>
#
#   unit        fast tests, no Docker
#   integration tests on a real PostgreSQL started by Testcontainers (needs Docker)
#   test        unit + integration
#   smoke       Docker Compose: PostgreSQL, Redis, migrations, the services, and the user flow through the API
#   cover       statement coverage per package from unit + integration tests (needs Docker)
#   cover-unit  the same from unit tests only
#   mutation    mutation testing with gremlins on the critical packages; reports go to reports/mutation
#   e2e         Playwright tests against the whole system started like the smoke test (needs Docker, Node.js)
#
# Variables: COVER_MODULES (modules to measure), COVER_MIN (fail below this percent of a module),
#            MUTATION_TARGETS ("module:package ..."), MUTATION_WORKERS, MUTATION_MIN_EFFICACY, MUTATION_FLAGS.

set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"

MODULES="shared services/subscription services/tracking services/notification"
INTEGRATION_MODULES="shared services/subscription services/tracking"
SERVICES="subscription tracking notification"
BUILDINFO=github.com/ReilEgor/GitHubRepoNotifier/shared/buildinfo
BIN=bin

COVER_MODULES=${COVER_MODULES:-$INTEGRATION_MODULES}
COVER_MIN=${COVER_MIN:-0}
COVER_DIR=reports/coverage

# Packages whose logic matters most; each entry is module:package[:excluded-files-regexp]. gremlins walks into
# subdirectories, so a package whose subpackage has its own entry excludes it.
MUTATION_TARGETS=${MUTATION_TARGETS:-"services/subscription:./internal/infrastructure/ratelimit services/subscription:./internal/usecase services/subscription:./internal/transport/http:middleware services/subscription:./internal/transport/http/middleware services/tracking:./internal/usecase services/tracking:./internal/transport/grpc services/tracking:./internal/infrastructure/clients/github"}
MUTATION_DIR=reports/mutation
# One worker and a long timeout: on a loaded machine (and always on Windows) parallel mutants time out, which gremlins reports as a lost mutant.
MUTATION_WORKERS=${MUTATION_WORKERS:-1}
MUTATION_TIMEOUT_COEFFICIENT=${MUTATION_TIMEOUT_COEFFICIENT:-20}
MUTATION_MIN_EFFICACY=${MUTATION_MIN_EFFICACY:-0}
# The default mutators of gremlins (comparisons, arithmetic, ++/--) miss most of the logic of a Go service; these add
# && / || swaps, break/continue swaps and inverted assignments.
MUTATION_FLAGS=${MUTATION_FLAGS:---invert-logical --invert-loopctrl --invert-assignments}

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

stack_pids=""

stack_down() {
	for p in $stack_pids; do kill "$p" 2>/dev/null || true; done
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

# stack_up builds and starts the whole system on the smoke ports and waits until every service is healthy.
# The caller sets a trap on stack_down. Optional settings, passed to the services when set:
#   STACK_GITHUB_API_URL  where tracking asks for repositories (default: GitHub; the seed makes golang/go known without it)
#   STACK_RATE_LIMIT, STACK_TRUSTED_PROXIES  the rate limit settings of subscription
stack_up() {
	need curl
	need docker
	cmd_build
	version=$(commit)
	ext=$(go env GOEXE)

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
		WORKER_HEALTH_PORT="$SMOKE_PORT_tracking" ${STACK_GITHUB_API_URL:+GITHUB_API_URL="$STACK_GITHUB_API_URL"} \
		"$BIN/tracking$ext" >"$BIN/tracking.smoke.log" 2>&1 &
	stack_pids="$stack_pids $!"
	# shellcheck disable=SC2086
	env $redis_env APP_API_KEY="$SMOKE_API_KEY" DB_URL="$SMOKE_DB_subscription" APP_HTTP_PORT="$SMOKE_PORT_subscription" \
		TRACKING_GRPC_ADDR="127.0.0.1:$SMOKE_GRPC_PORT" ${STACK_RATE_LIMIT:+RATE_LIMIT="$STACK_RATE_LIMIT"} \
		${STACK_TRUSTED_PROXIES:+TRUSTED_PROXIES="$STACK_TRUSTED_PROXIES"} \
		"$BIN/subscription$ext" >"$BIN/subscription.smoke.log" 2>&1 &
	stack_pids="$stack_pids $!"
	SENDER_HEALTH_PORT="$SMOKE_PORT_notification" "$BIN/notification$ext" >"$BIN/notification.smoke.log" 2>&1 &
	stack_pids="$stack_pids $!"

	for s in $SERVICES; do
		eval "port=\$SMOKE_PORT_$s"
		step "wait for $s on :$port"
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
}

cmd_smoke() {
	trap stack_down EXIT INT TERM
	stack_up

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

# cover_run TAGS: measures every module in COVER_MODULES. -coverpkg=./... counts a package as covered by the tests
# of any other package, so the integration tests and the handler tests count towards the code they exercise.
# Generated code, wiring (cmd/), test helpers and mocks are left out of the numbers.
cover_run() {
	tags=$1
	mkdir -p "$COVER_DIR"
	failed=""
	for m in $COVER_MODULES; do
		step "coverage $m${tags:+ (tags: $tags)}"
		name=$(echo "$m" | tr / -)
		profile="$ROOT/$COVER_DIR/$name.out"
		# shellcheck disable=SC2086
		(cd "$m" && go test -count=1 ${tags:+-tags $tags} -coverpkg=./... -coverprofile="$profile" ./... >/dev/null) || {
			echo "error: tests of $m failed; run 'make unit' to see why" >&2
			exit 1
		}
		modpath=$(awk '$1 == "module" { print $2 }' "$m/go.mod")
		cover_report "$profile" "$modpath" "$COVER_DIR/$name.total"
		if awk -v p="$(cat "$COVER_DIR/$name.total")" -v min="$COVER_MIN" 'BEGIN { exit !(p + 0 < min + 0) }'; then
			failed="$failed $m"
		fi
	done
	if [ -n "$failed" ]; then
		echo "error: coverage is below ${COVER_MIN}% in:$failed" >&2
		exit 1
	fi
	echo
	echo "profiles: $COVER_DIR/*.out; per function: go tool cover -func=<profile>; in a browser: go tool cover -html=<profile>"
}

# cover_report PROFILE MODULE_PATH TOTAL_FILE: prints statement coverage per package and writes the module total
# (a bare percent) to TOTAL_FILE.
cover_report() {
	awk -v mod="$2/" -v totalfile="$3" '
		NR == 1 { next }
		# Every test binary reports every block of -coverpkg: a block counts once, covered if any binary ran it.
		{
			block = $1
			stmtsOf[block] = $2
			if ($3 > 0) hitBlock[block] = 1
		}
		END {
			for (block in stmtsOf) {
				split(block, a, ":")
				file = a[1]
				if (file ~ /\.pb\.go$/ || file ~ /_gen\.go$/ || file ~ /\/cmd\// || file ~ /\/mocks\// || file ~ /\/pgtest\// || file ~ /\/redistest\//) continue
				sub(mod, "", file)
				n = split(file, parts, "/")
				pkg = ""
				for (i = 1; i < n; i++) pkg = pkg (i > 1 ? "/" : "") parts[i]
				if (pkg == "") pkg = "."
				stmts[pkg] += stmtsOf[block]
				all += stmtsOf[block]
				if (block in hitBlock) {
					cov[pkg] += stmtsOf[block]
					hit += stmtsOf[block]
				}
			}
			# Insertion sort by package name: no dependence on sort(1), whose Windows namesake ignores the options.
			count = 0
			for (p in stmts) {
				i = ++count
				while (i > 1 && names[i - 1] > p) { names[i] = names[i - 1]; i-- }
				names[i] = p
			}
			for (i = 1; i <= count; i++) {
				p = names[i]
				printf "  %6.1f%%  %5d/%-5d  %s\n", 100 * cov[p] / stmts[p], cov[p], stmts[p], p
			}
			total = (all ? 100 * hit / all : 0)
			printf "  %6.1f%%  %5d/%-5d  TOTAL\n", total, hit, all
			printf "%.1f\n", total > totalfile
		}
	' "$1"
}

cmd_cover() {
	need docker
	docker info >/dev/null 2>&1 || {
		echo "error: Docker is not running; use 'cover-unit' for unit tests only" >&2
		exit 1
	}
	if [ "$(go env GOOS)" = windows ]; then
		export TESTCONTAINERS_RYUK_DISABLED=true
	fi
	cover_run integration
}

cmd_cover_unit() {
	cover_run ""
}

cmd_mutation() {
	need gremlins
	mkdir -p "$ROOT/$MUTATION_DIR"
	failed=""
	for target in $MUTATION_TARGETS; do
		m=${target%%:*}
		rest=${target#*:}
		pkg=${rest%%:*}
		exclude=""
		case "$rest" in *:*) exclude=${rest#*:} ;; esac
		name=$(echo "$m-$pkg" | tr -c 'A-Za-z0-9\n' - | sed 's/--*/-/g; s/-$//')
		step "mutation $m $pkg"
		# shellcheck disable=SC2086
		(cd "$m" && gremlins unleash "$pkg" ${exclude:+-E "$exclude"} $MUTATION_FLAGS --workers "$MUTATION_WORKERS" \
			--timeout-coefficient "$MUTATION_TIMEOUT_COEFFICIENT" --output "$ROOT/$MUTATION_DIR/$name.json" \
			--threshold-efficacy "$MUTATION_MIN_EFFICACY") || failed="$failed $m:$pkg"
	done
	if [ -n "$failed" ]; then
		echo "error: test efficacy is below ${MUTATION_MIN_EFFICACY}% in:$failed" >&2
		exit 1
	fi
}

E2E_GITHUB_PORT=18099

cmd_e2e() {
	need node
	need npm
	step "install Playwright"
	(cd e2e && npm ci --no-audit --no-fund)

	fake_github_pid=""
	trap 'kill "$fake_github_pid" 2>/dev/null || true; stack_down' EXIT INT TERM
	step "start the fake GitHub on :$E2E_GITHUB_PORT"
	# bin/ does not exist on a clean checkout until the services are built; the log would fail to open and node would not start.
	mkdir -p "$BIN"
	E2E_GITHUB_PORT=$E2E_GITHUB_PORT node e2e/fake-github.mjs >"$BIN/fake-github.log" 2>&1 &
	fake_github_pid=$!
	tries=0
	until fetch "http://127.0.0.1:$E2E_GITHUB_PORT/__calls" >/dev/null 2>&1; do
		tries=$((tries + 1))
		if [ "$tries" -ge 20 ]; then
			echo "error: the fake GitHub did not start; log:" >&2
			cat "$BIN/fake-github.log" >&2
			exit 1
		fi
		sleep 0.5
	done

	# Every test sends its own X-Forwarded-For, so the tests do not share a rate limit; 127.0.0.1 is the trusted proxy.
	STACK_GITHUB_API_URL="http://127.0.0.1:$E2E_GITHUB_PORT" STACK_RATE_LIMIT=5-M STACK_TRUSTED_PROXIES=127.0.0.1 stack_up

	step "run the Playwright tests"
	(cd e2e && \
		E2E_BASE_URL="http://127.0.0.1:$SMOKE_PORT_subscription" E2E_API_KEY="$SMOKE_API_KEY" \
		E2E_GITHUB_URL="http://127.0.0.1:$E2E_GITHUB_PORT" E2E_RATE_LIMIT=5 \
		E2E_SUBSCRIPTION_DB="$SMOKE_DB_subscription" E2E_TRACKING_DB="$SMOKE_DB_tracking" \
		npx playwright test)
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
cover) cmd_cover ;;
cover-unit) cmd_cover_unit ;;
mutation) cmd_mutation ;;
e2e) cmd_e2e ;;
*)
	echo "usage: sh scripts/check.sh <fmt|fmt-check|lint|arch|unit|integration|test|cover|cover-unit|mutation|build|generate|generate-check|smoke|e2e>" >&2
	exit 2
	;;
esac
