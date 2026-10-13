# Дані

Частина специфікації; короткий огляд і критерії прийняття — у [spec.md](../../spec.md).

## subscription_db

```mermaid
erDiagram
    users ||--o{ subscriptions : "має"
    repositories ||--o{ subscriptions : "на нього підписані"
    subscriptions ||--o{ subscription_sagas : "супроводжує"

    users {
        bigserial id PK
        varchar email UK
        timestamptz created_at
    }
    repositories {
        bigserial id PK
        varchar full_name UK
        varchar last_seen_tag "може бути NULL"
        timestamptz created_at
        timestamptz updated_at
    }
    subscriptions {
        bigserial id PK
        bigint user_id FK
        bigint repository_id FK
        text token UK
        boolean is_confirmed
        timestamptz created_at
    }
    subscription_sagas {
        bigserial id PK
        bigint subscription_id "без зовнішнього ключа"
        varchar status
        varchar current_step
        timestamptz created_at
        timestamptz updated_at
    }
    outbox_messages {
        bigserial id PK
        varchar queue
        jsonb payload
        varchar status
        int attempts
        text last_error
        timestamptz created_at
    }
```

| Сутність | Призначення | Власник |
|---|---|---|
| `users` | адреси пошти | subscription |
| `repositories` | репозиторії, на які хтось підписувався, з копією останнього тегу для показу в списку | subscription |
| `subscriptions` | підписки з ознакою підтвердження і токеном | subscription |
| `subscription_sagas` | стан надсилання листа підтвердження | subscription |
| `outbox_messages` | повідомлення, що чекають публікації | subscription |

`subscription_sagas` не має зовнішнього ключа на `subscriptions` навмисно: після компенсації підписку видалено, а запис саги лишається як історія.

| Інваріант | Чим забезпечується |
|---|---|
| Одна адреса пошти — один користувач | `UNIQUE (email)` |
| Одна назва — один репозиторій | `UNIQUE (full_name)` |
| Користувач має не більше однієї підписки на репозиторій | `UNIQUE (user_id, repository_id)` |
| Токен унікальний у межах системи | `UNIQUE (token)`; токен — UUID |
| Підписка не існує без користувача та репозиторію | `ON DELETE CASCADE` на обох зовнішніх ключах |
| Нова підписка непідтверджена | сценарій підписки записує `is_confirmed = false` |

Стани саги:

```mermaid
stateDiagram-v2
    [*] --> STARTED: підписку створено, команду покладено в outbox
    STARTED --> COMPLETED: лист надіслано
    STARTED --> COMPENSATED: лист надіслати неможливо, підписку видалено
    COMPLETED --> [*]
    COMPENSATED --> [*]
```

Поле `current_step` має значення `SEND_CONFIRMATION`, доки сага в стані `STARTED`, і `ACTIVATE_SUBSCRIPTION` після переходу в `COMPLETED`.

Стани запису outbox: `PENDING` — чекає публікації; `FAILED` — вичерпано 5 спроб. Успішно опублікований запис видаляється.

## tracker_db

```mermaid
erDiagram
    repositories ||--o{ subscriptions : "має підписників"

    repositories {
        bigserial id PK
        varchar full_name UK
        varchar last_seen_tag "може бути NULL"
        timestamptz updated_at
        timestamptz created_at
    }
    subscriptions {
        bigserial id PK
        bigint repository_id FK
        varchar email
        varchar token
    }
    outbox_messages {
        bigserial id PK
        varchar queue
        jsonb payload
        varchar status
        int attempts
        text last_error
        timestamptz created_at
    }
```

| Сутність | Призначення | Власник |
|---|---|---|
| `repositories` | репозиторії, за якими стежить сканер, і останній побачений тег | tracking |
| `subscriptions` | підписники репозиторію: кому надсилати лист про реліз | tracking |
| `outbox_messages` | повідомлення, що чекають публікації | tracking |

| Інваріант | Чим забезпечується |
|---|---|
| Одна назва — один репозиторій | `UNIQUE (full_name)` |
| Пошта є підписником репозиторію не більше одного разу | `UNIQUE (repository_id, email)` |
| Серед підписників лише підтверджені | запис з'являється тільки як реакція на `SubscriptionActivatedEvent` |
| Сканер опитує лише репозиторії, що мають підписників | запит вибирає репозиторії, для яких існує рядок у `subscriptions`; після відписки останнього підписника репозиторій видаляється |
| Підписник не існує без репозиторію | `ON DELETE CASCADE` |

## Дублювання між сервісами

| Дані | Джерело правди | Копія | Чим синхронізується | Допустиме відставання |
|---|---|---|---|---|
| Останній побачений тег | `tracker_db.repositories` | `subscription_db.repositories` | значення з відповіді `GetOrCreateRepository` під час підписки; подія `TagUpdatedEvent` після кожного релізу | інтервал ретранслятора плюс час у черзі; копія лише показується в списку підписок і на розсилку не впливає |
| Підтверджені підписки | `subscription_db.subscriptions` | `tracker_db.subscriptions` | події `SubscriptionActivatedEvent` і `UnsubscriptionActivatedEvent` | інтервал ретранслятора плюс час у черзі |
| Назва репозиторію | `tracker_db.repositories` | `subscription_db.repositories` | створюється під час підписки | немає: назва не змінюється |

`tracking` тримає власний список підписників, щоб під час релізу не звертатися до `subscription` по кожну адресу. Ціна — узгодженість у кінцевому підсумку: щойно підтверджена підписка може пропустити реліз, що вийшов до обробки події.
