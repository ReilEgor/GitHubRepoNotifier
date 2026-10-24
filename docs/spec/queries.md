# Запити до бази

Частина специфікації; короткий огляд і критерії прийняття — у [spec.md](../../spec.md). Схема — у [data.md](data.md).

Правила для всіх запитів:

- текст запиту — іменована константа поруч із методом репозиторію;
- параметри лише позиційні (`$1`, `$2`); значення в текст не підставляються;
- кожен метод працює і всередині транзакції, і поза нею: виконавець береться з контексту, якщо транзакцію відкрито;
- жоден сценарій не виконує запит у циклі по результатах іншого запиту.

## subscription_db

### Сценарії API

| Метод                                         | SQL                                                                                                                                                                                                                                  | Сценарій         | Транзакція                          |
| --------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------- | ----------------------------------- |
| `RepositoryRepository.GetOrCreate(name, tag)` | `INSERT INTO repositories (full_name, last_seen_tag) VALUES ($1, NULLIF($2, '')) ON CONFLICT (full_name) DO UPDATE SET full_name = EXCLUDED.full_name RETURNING id, full_name`                                                       | SC-1             | окремо, до транзакції «підписка»    |
| `UserRepository.GetOrCreate(email)`           | `INSERT INTO users (email) VALUES ($1) ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email RETURNING id, email, created_at`                                                                                                     | SC-1             | підписка                            |
| `SubscriptionRepository.Upsert(sub)` | `INSERT INTO subscriptions (user_id, repository_id, token, is_confirmed) VALUES ($1, $2, $3, FALSE) ON CONFLICT (user_id, repository_id) DO UPDATE SET token = CASE WHEN subscriptions.is_confirmed THEN subscriptions.token ELSE EXCLUDED.token END RETURNING id, token, is_confirmed, created_at` — для вже підтвердженої пари токен і підтвердження не змінюються; метод повертає `is_confirmed`, за яким сценарій вирішує, чи створювати сагу й команду на лист | SC-1 | підписка |
| `SagaRepository.Create(subscriptionID)`       | `INSERT INTO subscription_sagas (subscription_id, status, current_step) VALUES ($1, 'STARTED', 'SEND_CONFIRMATION') RETURNING id`                                                                                                    | SC-1             | підписка                            |
| `OutboxRepository.Insert(queue, payload)`     | `INSERT INTO outbox_messages (queue, payload) VALUES ($1, $2)`                                                                                                                                                                       | SC-1, SC-3, SC-6 | підписка / підтвердження / відписка |
| `SubscriptionRepository.GetByToken(token)`    | `SELECT s.id, s.user_id, u.email, s.repository_id, r.full_name, s.token, s.is_confirmed, s.created_at FROM subscriptions s JOIN users u ON u.id = s.user_id JOIN repositories r ON r.id = s.repository_id WHERE s.token = $1`        | SC-3, SC-6       | поза транзакцією                    |
| `SubscriptionRepository.Confirm(id)` | `UPDATE subscriptions SET is_confirmed = TRUE WHERE id = $1` — повертає кількість змінених рядків; `0` → `ErrInvalidToken`, транзакція відкочується без запису події | SC-3 | підтвердження |
| `SubscriptionRepository.ListByEmail(email)`   | `SELECT s.id, u.email, r.full_name, s.is_confirmed, r.last_seen_tag, s.created_at FROM subscriptions s JOIN users u ON u.id = s.user_id JOIN repositories r ON r.id = s.repository_id WHERE u.email = $1 ORDER BY s.created_at DESC, s.id DESC` | SC-5             | поза транзакцією                    |
| `SubscriptionRepository.Delete(id)` | `DELETE FROM subscriptions WHERE id = $1` — повертає кількість видалених рядків; `0` → `ErrInvalidToken`, транзакція відкочується без запису події. Той самий метод використовує компенсація саги (SC-2) | SC-6, SC-2 | відписка |

| Транзакція | Що фіксується разом |
|---|---|
| підписка | користувач, підписка, сага, команда на лист підтвердження |
| підтвердження | ознака підтвердження й подія активації |
| відписка | видалення підписки й подія відписки |

`GetOrCreate` для користувача й репозиторію — один запит з `ON CONFLICT`, а не пара «знайти, потім створити». Пара дає гонку: два одночасні запити з новою поштою обидва не знаходять користувача, і другий `INSERT` падає на унікальному обмеженні.

`ListByEmail` — один запит із `JOIN`. Варіант «спершу підписки, потім репозиторій для кожної» дав би N+1 запитів на сторінку.

### Фонові процеси

> **Відкладено до Завдання 3.** Ретранслятор, сага й подія про тег потребують брокера та сервісу `notification`. Записи в `outbox_messages` створюються вже зараз, але ніхто їх не публікує.

| Метод | SQL | Де |
|---|---|---|
| `OutboxRepository.FetchPending(limit)` | `SELECT id, queue, payload FROM outbox_messages WHERE status = 'PENDING' ORDER BY id LIMIT $1 FOR UPDATE SKIP LOCKED` | ретранслятор |
| `OutboxRepository.Delete(id)` | `DELETE FROM outbox_messages WHERE id = $1` | ретранслятор |
| `OutboxRepository.Fail(id, maxAttempts, err)` | `UPDATE outbox_messages SET attempts = attempts + 1, last_error = $3, status = CASE WHEN attempts + 1 >= $2 THEN 'FAILED' ELSE 'PENDING' END WHERE id = $1` | ретранслятор |
| `SagaRepository.GetByID(id)` | `SELECT id, subscription_id, status, current_step, created_at, updated_at FROM subscription_sagas WHERE id = $1` | SC-2 |
| `SagaRepository.UpdateStatusAndStep(id, status, step)` | `UPDATE subscription_sagas SET status = $1, current_step = $2, updated_at = now() WHERE id = $3` | SC-2 |
| `RepositoryRepository.UpdateTag(name, tag)` | `UPDATE repositories SET last_seen_tag = $1, updated_at = now() WHERE full_name = $2` | SC-4, подія `TagUpdatedEvent` |

## tracker_db

> **У Завданні 2** з методів `tracking` входять лише `RepositoryRepository.GetByName` і `Create`: їх потребує `GetOrCreateRepository`. Решта таблиці (сканер, підписники, відписка, outbox) відкладена до Завдання 3 разом із брокером; слідів сканера в коді `tracking` у цьому завданні немає.

| Метод | SQL | Сценарій | Транзакція |
|---|---|---|---|
| `RepositoryRepository.GetByName(name)` | `SELECT id, full_name, last_seen_tag, updated_at FROM repositories WHERE full_name = $1` | `GetOrCreateRepository` | поза транзакцією |
| `RepositoryRepository.Create(name, tag)` | `INSERT INTO repositories (full_name, last_seen_tag) VALUES ($1, $2) ON CONFLICT (full_name) DO UPDATE SET full_name = EXCLUDED.full_name RETURNING id, last_seen_tag, updated_at` | `GetOrCreateRepository` | поза транзакцією |
| `RepositoryRepository.ListTracked()` | `SELECT r.id, r.full_name, r.last_seen_tag, r.updated_at FROM repositories r WHERE EXISTS (SELECT 1 FROM subscriptions s WHERE s.repository_id = r.id)` | SC-4 | поза транзакцією |
| `RepositoryRepository.UpdateTag(id, tag)` | `UPDATE repositories SET last_seen_tag = $1, updated_at = now() WHERE id = $2` | SC-4 | реліз |
| `SubscriberRepository.ListByRepository(id)` | `SELECT email, token FROM subscriptions WHERE repository_id = $1` | SC-4 | поза транзакцією |
| `SubscriberRepository.Upsert(repoID, email, token)` | `INSERT INTO subscriptions (repository_id, email, token) VALUES ($1, $2, $3) ON CONFLICT (repository_id, email) DO UPDATE SET token = EXCLUDED.token` | SC-3 | поза транзакцією |
| `SubscriberRepository.Delete(email, name)` | `DELETE FROM subscriptions WHERE email = $1 AND repository_id = (SELECT id FROM repositories WHERE full_name = $2)` | SC-6 | відписка в `tracking` |
| `RepositoryRepository.DeleteIfUnused(name)` | `DELETE FROM repositories WHERE full_name = $1 AND NOT EXISTS (SELECT 1 FROM subscriptions s WHERE s.repository_id = repositories.id)` — один оператор замість пари «перевірити, потім видалити»: між перевіркою й видаленням нова підписка не встигає потрапити під каскадне видалення | SC-6 | відписка в `tracking` |
| Outbox: `Insert`, `FetchPending`, `Delete`, `Fail` | як у `subscription_db` | SC-4, ретранслятор | «реліз» для `Insert` |

| Транзакція | Що фіксується разом |
|---|---|
| реліз | новий тег, по команді `SendNotificationCommand` на підписника, подія `TagUpdatedEvent` |
| відписка в `tracking` | видалення підписника; видалення репозиторію, якщо підписників не лишилося |

Сканер (SC-4) читає підписників кожного репозиторію окремим запитом лише тоді, коли в репозиторію змінився тег: кількість запитів дорівнює кількості репозиторіїв із новим релізом, а не кількості всіх репозиторіїв.

Примітка до сканера: запис в outbox окремою командою на кожного підписника — це запит у циклі й суперечить правилу вище. Перед реалізацією сканера вставка має стати пакетною (`unnest` чи `pgx.Batch`) і мати тест на кількість запитів.
