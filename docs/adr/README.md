# Записи архітектурних рішень

Формат описано в [standards/adr-format.md](../../standards/adr-format.md).

| № | Рішення | Статус |
|---|---|---|
| [0001](0001-split-into-three-services.md) | Система складається з трьох окремих сервісів | accepted |
| [0002](0002-publish-tag-updates-as-events.md) | Новий тег передається подією, а не gRPC-викликом | accepted |
| [0003](0003-go-workspace-with-a-module-per-service.md) | Go, один репозиторій, окремий модуль на сервіс | accepted |
| [0004](0004-checks-in-one-script-run-by-hooks-and-ci.md) | Перевірки живуть в одному скрипті, який запускають hooks і CI | accepted |
