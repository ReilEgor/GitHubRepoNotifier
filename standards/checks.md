# Перелік перевірок

Що перевіряється автоматично, коли і чим. Тут описано лише те, що реально налаштовано в репозиторії.

Усі перевірки живуть в одному скрипті — [`scripts/check.sh`](../scripts/check.sh). `Makefile`, hooks і CI викликають його, тому локальний результат збігається з результатом CI.

## Перевірки

| № | Перевірка | Чим | Ціль `make` | commit | push | CI |
|---|---|---|---|:-:|:-:|:-:|
| 1 | Код відформатовано | `golangci-lint fmt`: `gofumpt`, `gci`, `golines` | `fmt-check` | ✔ | | ✔ |
| 2 | Лінтер і статичний аналіз (разом із файлами під тегом `integration`) | `golangci-lint run --build-tags integration` | `lint` | ✔ | | ✔ |
| 3 | Межі шарів дотримано | `go-arch-lint check` | `arch` | ✔ | | ✔ |
| 4 | Модульні тести з детектором гонок, без Docker | `go test -race` | `unit` | | ✔ | ✔ |
| 5 | Усі сервіси збираються | `go build` | `build` | | ✔ | ✔ |
| 6 | Інтеграційні тести на справжньому PostgreSQL | `go test -tags integration` + Testcontainers | `test-integration` | | | ✔ |
| 7 | Згенерований код (protobuf, wire) актуальний | `buf generate`, `wire`, `git status` | `generate-check` | | | ✔ |
| 8 | Smoke-тест на Docker Compose | `scripts/check.sh smoke` | `smoke` | | | ✔ |

`make check` запускає перевірки 1–5, тобто все, що не потребує Docker. `make test` запускає `unit` і `test-integration`. Перевірки 6–8 потребують запущеного Docker і виконуються в CI; локально їх запускають вручну тією самою командою. Рішення — [ADR-0008](../docs/adr/0008-environment-and-integration-tests.md).

Перевірки 1–4 виконуються окремо в кожному модулі (`shared`, `services/subscription`, `services/tracking`, `services/notification`): у кожного свій `go.mod` і свій `.go-arch-lint.yml`. Конфігурація лінтера одна — `.golangci.yml` у корені.

Швидкі перевірки стоять на коміті, повільні — на відправленні, а ті, що потребують Docker, — у CI. CI повторює все на чистому середовищі.

## Hooks

| Hook | Файл | Що запускає |
|---|---|---|
| `pre-commit` | [`.githooks/pre-commit`](../.githooks/pre-commit) | перевірки 1, 2, 3 |
| `pre-push` | [`.githooks/pre-push`](../.githooks/pre-push) | перевірки 4, 5 |

Hooks лежать у репозиторії, тож їхні зміни проходять рецензію як код. Вмикаються командою `make setup`, яка виконує `git config core.hooksPath .githooks`.

## Інтеграційні тести

Лежать поруч із кодом за build tag `integration` (`//go:build integration`), тому `go test ./...` без тегу їх не запускає.

1. `TestMain` запускає один контейнер PostgreSQL на пакет тестів (`pgtest.Start`).
2. Кожен тест отримує свою базу з унікальною назвою (`CREATE DATABASE`), на яку `golang-migrate` з коду застосовує файли з `services/<сервіс>/migrations`, ті самі, що й контейнер `migrate/migrate`.
3. Сід з відомими токенами лежить у `services/<сервіс>/migrations/testdata/seed.sql`; його застосовує лише тестовий харнес.
4. Тест `up` → `down` → `up` на порожній базі перевіряє, що кожна міграція зворотна.

На Windows Docker Desktop контейнер-прибиральник Testcontainers (Ryuk) не стартує, тому `scripts/check.sh integration` вимикає його (`TESTCONTAINERS_RYUK_DISABLED=true`); контейнер зупиняє сам `TestMain`. Якщо тести обірвати, контейнер може лишитися: його видаляють командою `docker rm -f`.

## Smoke-тест

Перевіряє, що система запускається й проходить головний шлях користувача. Працює на окремому проєкті Compose (`reponotifier-smoke`) і портах `15432`, `15433`, `16379`, тож не можна запускати його одночасно з `docker compose up`.

1. Зібрати три сервіси, вшивши хеш поточного коміту.
2. Підняти PostgreSQL ×2 і Redis, застосувати міграції контейнерами `migrate/migrate`, додати в `tracker_db` сід із `golang/go` (щоб `tracking` не ходив у GitHub).
3. Запустити сервіси на портах `18080`, `18081`, `18082`; gRPC `tracking` — на `19090`.
4. Для кожного: `GET /health` дає `200`, `GET /version` дорівнює `git rev-parse HEAD`.
5. Шлях користувача: підписка (`202`), токен із бази, підтвердження (`200`), список із `"confirmed":true`, відписка (`200`), повторна відписка (`404`), порожній список.
6. Зупинити сервіси й `docker compose down`.

## Версії інструментів

| Інструмент | Версія | Де зафіксовано |
|---|---|---|
| Go | 1.25 | `go.mod` кожного модуля, `go.work`, CI |
| `golangci-lint` | v2.12.1 | `Makefile`, CI |
| `go-arch-lint` | v1.15.0 | `Makefile`, CI |
| `wire` | v0.7.0 | `Makefile` (`make setup`), CI |
| `buf` | v1.71.0 | `Makefile`, CI |
| `protoc-gen-go` | v1.36.11 | `Makefile`, CI |
| `protoc-gen-go-grpc` | v1.6.1 | `Makefile`, CI |

## Що робити, коли перевірка падає

| Що впало | Що робити |
|---|---|
| Формат | `make fmt`, додати зміни, повторити коміт |
| Лінтер | виправити код; якщо правило не підходить для цього місця — `//nolint:<лінтер> // причина` |
| Межі шарів | перенести код у правильний шар; нова залежність між шарами спершу з'являється в `spec.md` |
| Тест | виправити код або тест; вимикати тест не можна |
| Інтеграційний тест | запустити пакет окремо: `cd services/<сервіс> && go test -tags integration ./internal/integration/` |
| Згенерований код | `make generate` і закомітити результат |
| Smoke | переглянути `bin/<сервіс>.smoke.log` |

## Доказ, що конвеєр блокує

Перевірено на копії репозиторію з увімкненими hooks:

| № | Дія | Результат |
|---|---|---|
| 1 | Коміт чистого коду | прийнято |
| 2 | Коміт файла з `func  Bad( ) string {   return "x" }` | відхилено: `error: shared is not formatted; run 'make fmt'` |
| 3 | Коміт функції з `os.Remove("x")` без перевірки помилки | відхилено: `Error return value of os.Remove is not checked (errcheck)` |
| 4 | Коміт, де `internal/usecase` імпортує `internal/transport/http` | відхилено: `Component usecase-impl shouldn't depend on …/internal/transport/http` |
| 5 | `git push` чистого коду | прийнято; `/version` усіх трьох сервісів дорівнює хешу коміту |
| 6 | `git push` зі зламаним тестом | відхилено: `FAIL: TestNewHandler/success_-_health_reports_ok` |
| 7 | `git push` з кодом, що не компілюється (коміт створено з `--no-verify`) | відхилено: `undefined: undefined … [build failed]` |

Рядок 7 показує, навіщо потрібні два рівні: обхід `pre-commit` не дає відправити зламаний код, бо його зупиняє `pre-push`, а після нього — CI.

## Особливість Windows

З `core.autocrlf=true` Git записує файли в робочу теку з `CRLF`, і перевірка формату падає на незміненому коді. Тому в репозиторії є `.gitattributes` з `* text=auto eol=lf`.

Лінтер `forbidigo` забороняє `fmt.Sprintf` у `internal/repository` ([ADR-0005](../docs/adr/0005-postgresql-with-pgx-and-plain-sql.md)).
