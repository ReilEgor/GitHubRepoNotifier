# Тестування: що змінено, що виміряно, що лишилось відкритим

Гілка `feat/tests-realization`. Документ для рев'ю й для аудиту: описує зміни, результати вимірювань, відхилення від стандартів і питання без відповіді. Рішення про інструменти — в [ADR-0009](adr/0009-behavior-tests-mutation-and-e2e.md).

## 1. Підсумок

| Рівень | Що додано | Як запустити |
|---|---|---|
| Unit | `ratelimit` доведено до 100% операторів і до 0 вцілілих мутантів; знайдено й виправлено помилку (п. 4) | `make unit` |
| Інтеграційні | сценарії користувача через HTTP з реальними PostgreSQL і Redis (`subscription`); gRPC → use case → PostgreSQL → GitHub-клієнт → Redis-кеш проти фейкового GitHub (`tracking`) | `make test-integration` |
| E2E | 23 тести Playwright (режим API) проти всієї системи в Docker Compose | `make e2e` |
| Покриття | скрипт із таблицею «пакет → % операторів» і порогом | `make cover`, `make cover-unit`, `COVER_MIN=80 make cover` |
| Мутації | `gremlins` на 7 критичних пакетах, JSON-звіти | `make mutation` |

Усі цілі йдуть через `scripts/check.sh`, як вирішено в [ADR-0004](adr/0004-checks-in-one-script-run-by-hooks-and-ci.md). У CI додано job `e2e`, `coverage`, `mutation`; hooks не змінено й лишаються без Docker.

## 2. Зміни по файлах

### Скрипти й інструменти
- `scripts/check.sh`: нові команди `cover`, `cover-unit`, `mutation`, `e2e`. Блок запуску системи зі `smoke` винесено в `stack_up`/`stack_down`; `smoke` і `e2e` користуються ним (поведінка `smoke` та сама, перевірено). Налаштування — змінні `COVER_MODULES`, `COVER_MIN`, `MUTATION_TARGETS`, `MUTATION_FLAGS`, `MUTATION_WORKERS`, `MUTATION_MIN_EFFICACY`.
- `Makefile`: цілі `cover`, `cover-unit`, `mutation`, `e2e`; `gremlins@v0.6.0` у `make setup`.
- `.gitignore`: `reports/`, `e2e/node_modules`, звіти Playwright.
- `.github/workflows/ci.yml`: job `e2e`, `coverage` (таблиця в підсумку job), `mutation` (звіти як артефакт).

### Тести
- `services/subscription/internal/infrastructure/ratelimit/ratelimit_test.go`: одиниця «година», від'ємні й порожні значення, межа ліміту, вікно не подовжується повторним запитом, завислий Redis, відсутність повторів.
- `services/subscription/internal/integration/journey_test.go` (новий): повний шлях підписника; повторна підписка вбиває старе посилання; автентифікація; обмеження частоти на реальному Redis (рахунок на викликача, відновлення після вікна, fail-open).
- `services/subscription/internal/integration/{env,main}_test.go`: запуск Redis, `newEnvLimited`, `request` з адресою викликача.
- `services/tracking/internal/integration/` (новий пакет): реєстрація репозиторію, усі збої GitHub, автентифікація gRPC, паралельні запити, circuit breaker, кеш.
- `shared/infrastructure/storage/redistest/` (новий): контейнер Redis для інтеграційних тестів, за зразком `pgtest`.
- `e2e/` (новий): `fake-github.mjs` (фейковий GitHub, рахує звернення), `tests/{journey,errors,ratelimit}.spec.ts`, `tests/support.ts`.

### Код продакшену (єдина зміна поза тестами)
- `ratelimit.NewClient` + виклик у `cmd/subscription/app.go`; `ContextTimeoutEnabled: true` у `cmd/tracking/app.go`. Причина — п. 4.

## 3. Результати

### 3.1 Покриття операторів (`make cover`, модульні + інтеграційні)

Рахується один профіль на модуль із `-coverpkg=./...`; генерований код, `cmd/` і допоміжні пакети тестів виключено. «До» — `make cover-unit` на початку роботи.

| Модуль / пакет | До | Після |
|---|---|---|
| **subscription, разом** | 81.1% | **93.8%** |
| `infrastructure/ratelimit` | 94.7% (за `go test -cover`) | 100.0% |
| `repository/postgres` | 0.0% | 82.4% |
| `transport/http` | 95.8% | 98.6% |
| `transport/http/middleware` | 95.9% | 95.9% |
| `usecase` | 89.9% | 88.4% |
| **tracking, разом** | 78.0% | **86.6%** |
| `repository/postgres` | 0.0% | 90.0% |
| `transport/grpc` | 67.3% | 69.1% |
| `infrastructure/clients/github` | 91.9% | 92.7% |
| **shared, разом** | 27.1% | **68.2%** |
| `infrastructure/storage/postgres` | 0.0% | 89.7% |

**`usecase` у subscription:** на початку роботи профіль показав 89.9%, зараз на тому самому коді модульні тести дають стабільні 87.0% (три прогони підряд), а разом з інтеграційними 88.4%. Різницю в 2 оператори я не зміг пояснити й не бачу, що змінило покриття: у `usecase` і його модульних тестах я нічого не міняв. Тому в таблиці «до» для цього рядка не відтворюється; його не слід брати як свідчення втрати тестів, але й як чисте порівняння теж.

Покриття **гілок** Go сам не вимірює (лише оператори). Гілки тут перевіряє мутаційне тестування: мутатори `CONDITIONALS_*` і `INVERT_LOGICAL` змінюють умови.

### 3.2 Мутаційне тестування (`gremlins v0.6.0`, `make mutation`)

Мутатори: типові (`CONDITIONALS_BOUNDARY/NEGATION`, `ARITHMETIC_BASE`, `INCREMENT_DECREMENT`, `INVERT_NEGATIVES`) і додані `INVERT_LOGICAL`, `INVERT_LOOPCTRL`, `INVERT_ASSIGNMENTS`. Лише модульні тести пакета.

| Пакет | Killed | Lived | Not covered | Ефективність | Покриття мутаторами |
|---|---|---|---|---|---|
| subscription `infrastructure/ratelimit` | 10 | 0 | 1 | 100% | 90.9% |
| subscription `usecase` | 16 | 0 | 0 | 100% | 100% |
| subscription `transport/http` (без `middleware`) | 15 | 0 | 4 | 100% | 78.9% |
| subscription `transport/http/middleware` | 17 | 0 | 1 | 100% | 94.4% |
| tracking `usecase` | 4 | 0 | 0 | 100% | 100% |
| tracking `transport/grpc` | 7 | 0 | 2 | 100% | 77.8% |
| tracking `infrastructure/clients/github` | 46 | 0 | 8 | 100% | 85.2% |

**Вцілілих мутантів немає, тому класифікувати нічого.** Це чесний результат, а не доказ повної якості тестів:

- Набір мутаторів gremlins вузький: він не видаляє виклики, не підміняє значення, що повертаються, не чіпає рядки й структури. Мутант «прибрати виклик `outbox.Insert`» тут не генерується.
- Мутанти рахуються лише там, де є модульні тести; інтеграційні тести (а в них основна перевірка SQL і транзакцій) у мутаціях не беруть участі.

Тому для аудиту корисніше дивитись на «Not covered» і на список у п. 5, ніж на 100%.

**Not covered (усі):**

| Місце | Мутант | Висновок |
|---|---|---|
| `ratelimit.go` константа `redisTimeout` | `ARITHMETIC_BASE` | Константа без оператора, gremlins не бачить покриття. Поведінку «Redis завис, відповідь за ≤2 с» перевіряє тест, який не залежить від значення з точністю до секунди. Прийнятний. |
| `handler.go:22,23` | `ARITHMETIC_BASE` | Константи таймаутів, той самий випадок. Прийнятний. |
| `handler.go:49,52` (`case email == ""`, `case repo == ""`) | `CONDITIONALS_NEGATION` | **Імовірно помилка інструмента:** тести «email missing» і «repository missing» в `handler_test.go` ці гілки виконують (покриття рядка є). Gremlins не мапить покриття на `case` без тега. Перевірити вручну в аудиті. |
| `middleware/errors.go:44` (`Unwrap`) | профіль покриття | Метод не викликається жодним тестом (0%). Реальна прогалина, але тривіальна. |

### 3.3 E2E (`make e2e`)

23 тести, ~3 с на самі тести; ~40 с із запуском і збіркою. Перевіряють: повний шлях підписки; реєстрацію нового репозиторію в `tracking` і те, що GitHub питають один раз; повторну підписку й токени; паралельні підписки; усі коди помилок з [errors.md](spec/errors.md) (400, 401, 404, 422, 503); обмеження частоти (429), включно з неавтентифікованим потоком; те, що `/health` не обмежується.

## 4. Знайдена помилка

**Тайм-аут Redis у `subscription` не працював.** `ratelimit.Allow` обмежує виклик 200 мс (`redisTimeout`), але клієнт створювався без `ContextTimeoutEnabled: true`, і go-redis ігнорує дедлайн контексту. Тест «Redis приймає з'єднання й мовчить» показав, що `Allow` висить ~3 с (read timeout клієнта), а не 0.2 с. Наслідок: за завислого Redis кожен запит API затримується, хоча стандарт ([code-style.md](../standards/code-style.md) §6) і [errors.md](spec/errors.md) обіцяють fail-open.

Виправлення: `ratelimit.NewClient` (інваріант живе поруч із кодом, що на нього спирається) і `ContextTimeoutEnabled: true` у кеші `tracking`, де та сама `redisTimeout`. У `tracking` окремого тесту на це поки немає (див. п. 5).

## 5. Відомі прогалини (для аудиту)

- `subscription/repository/postgres`: `Confirm` і `Delete` — 71.4%, `outbox.Insert` — 75%. Гілки помилок бази (не «рядок не знайдено») не викликаються.
- `subscription/usecase`: 9 гілок `return err`, що виконуються лише коли репозиторій повертає помилку (рядки 63, 79, 83, 90, 113, 143, 159, 168, 190 файлу `subscription_usecase.go`). Інтеграційні тести покривають збій outbox і бази на кінці транзакції (відкат), але не кожен крок окремо. Мутатор `INVERT_ASSIGNMENTS`/`CONDITIONALS_NEGATION` їх не зачепив, бо вони не є умовами.
- `subscription/transport/http/handler.go`: `List` — гілка збою use case (рядки 102–105); `validEmail` — адреса довша за `maxEmailLen`. E2E перевіряє лише коротку неправильну адресу.
- `tracking/transport/grpc`: 69.1%. Не покрита `Run` (запуск і м'яка зупинка сервера), а також гілки `toStatus` для `DeadlineExceeded`.
- `tracking`: немає тесту, що кеш Redis справді поважає таймаут (аналог тесту `ratelimit`).
- `shared/infrastructure/httpserver`, `shared/config`: 0%.
- Стан гонки між `confirm` і `unsubscribe` з одним токеном описано в [errors.md](spec/errors.md); інтеграційні тести покривають два паралельних `unsubscribe`, але не пару `confirm` + `unsubscribe`.
- Інтеграційні тести не входять у мутації, тому їхня сила не виміряна.
- E2E не перевіряє подій в outbox (брокера ще немає, [scenarios.md](spec/scenarios.md)); це робить інтеграційний рівень.

## 6. Відхилення від стандартів і рішення, які варто перевірити

1. **Модульні тести `ratelimit` слухають loopback-порт** (рішення автора: лишити) (`net.Listen("127.0.0.1:0")`), хоча [code-style.md](../standards/code-style.md) §14 каже, що модульний тест «не ходить у мережу». Інакше завислий і обірваний Redis не відтворити. `miniredis`, який уже був у цих тестах, теж працює на loopback. Якщо правило жорстке, ці два тести треба перенести під тег `integration`.
2. **Мутації запускаються без `-i`** (лише модульні тести пакета). Режим `-i` запускає весь набір на кожен мутант; з Testcontainers це години.
3. **Gremlins на Windows:** один воркер і `--timeout-coefficient 20`, інакше всі мутанти завершуються як «timed out» (побачено при першому запуску). На Linux у CI можна підняти `MUTATION_WORKERS`.
4. **Gremlins `v0.6.0` падає з `nil pointer` на шаблоні `./pkg/...`**, тому цілі в `MUTATION_TARGETS` — окремі пакети, із необов'язковим виключенням файлів (`module:package:regexp`).
5. **Playwright працює без браузера:** у системі немає веб-сторінки, це HTTP-клієнт із фікстурами й HTML-звітом.
6. **Фейковий GitHub** (`e2e/fake-github.mjs`) замість `StaticClient` з коду: так `tracking` ходить справжнім клієнтом з ретраями, breaker-ом і кешем, а не зі спеціальною гілкою коду.
7. **E2E-стек працює з `RATE_LIMIT=5-M` і `TRUSTED_PROXIES=127.0.0.1`**, щоб кожен тест мав власну «адресу» через `X-Forwarded-For`. Це перевіряє й довірчий шлях проксі, але не дефолтну конфігурацію (`10-S`, без довірених проксі). Дефолт перевіряє `make smoke`.
8. **ADR-0009 додано без запиту:** [git-workflow.md](../standards/git-workflow.md) вимагає ADR на нову залежність (Playwright, gremlins, `pg`).

## 7. Питання та рішення

Рішення автора (2026-11-15):

| Питання | Рішення |
|---|---|
| Loopback у модульних тестах `ratelimit` | лишити в unit; це свідоме відхилення від [code-style.md](../standards/code-style.md) §14, оскільки `miniredis` теж працює на loopback |
| Модульні тести `http`/`usecase`, що дублюють інтеграційні | лишити як швидкий шар |
| Пороги в CI | лише `COVER_MIN=80` для `subscription` і `tracking` (`COVER_GATED`); `shared` вимірюється без порога; `MUTATION_MIN_EFFICACY` не вводиться |
| Node.js заради Playwright | лишити Playwright; браузерні тести додати, коли з'явиться сторінка |

Ще відкрито:

1. **Розширити мутатори?** Gremlins їх не має. `go-mutesting` видаляє виклики й підміняє вирази, але гірше працює з workspace. Без цього вцілілих мутантів для класифікації немає.
2. **`ContextTimeoutEnabled` у `tracking`** виправлено без окремого тесту. Додати тест кешу на завислий Redis?
3. **`make setup` у job `mutation`** ставить усі інструменти; можна звузити до `gremlins`.

## 8. Як відтворити

```sh
make setup           # встановлює gremlins та інші інструменти
make unit            # швидкі тести, без Docker
make cover-unit      # покриття без Docker
make cover           # покриття з інтеграційними (Docker)
make test-integration
make e2e             # Docker + Node.js; звіт у e2e/playwright-report
make mutation        # звіти в reports/mutation/*.json
```

На Windows запускати з Git Bash. Для Testcontainers скрипт сам вимикає Ryuk.
