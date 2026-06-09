# ha-energy-schema

Живая однолинейная схема (single-line diagram) энергосистемы **BobRIXOS** для Home Assistant — проект VELMUR.

Сервис на **Go**, упакован как **Home Assistant add-on** (Docker) с **ingress**: запускается на той же машине, что и HA (HAOS VM), читает состояния заданных сущностей через REST API и рендерит живой SVG. HA отдаёт страницу своим origin, поэтому работает по HTTPS и в Android-приложении (без mixed-content). Параллельно add-on пишет `energy_schema.svg`/`.html` в `/config/www` → доступно как `/local/energy_schema.html` (для встраивания iframe-карточкой в дашборд).

## Что отображает
- **Ввод №1 «Рыбхоз»** — 3 однофазных стабилизатора (вход/выход/ступень/ток/нагрузка/U-мин-макс по фазам), цвет линии по состоянию.
- **Ввод №2 «Зелёный»** — напряжение/ток по фазам, направление (потребление/отдача), состояние линии.
- **Контактор** (активный ввод), **АВР** (вход/резерв/выход), **Инвертор** (статус, берёт ли от сети).
- **Дом** — гейдж нагрузки (пороги из опций).
- **Батарея** — гейдж SOC, заряд/разряд, статус/темп/ток/SOH, грубая автономия.
- **Солнышко** — 3 гейджа по MPPT (V·A под каждым) + суммарная полоса.
- **Генератор** — состояние, наработка, до замены масла, подогрев ОЖ, до запуска, V/нагрузка по фазам.
- **Потоки энергии** анимированы кружочками (скорость ∝ мощности); отключённые линии серые пунктиром, проблемные — красные с ✕.

## Архитектура (Go)
Модуль `energy-schema/` (stdlib-only, без внешних зависимостей):

```
energy-schema/
  cmd/energy-schema/      — точка входа (main), тонкая обвязка
  internal/config/        — загрузка опций аддона (/data/options.json) поверх дефолтов
  internal/hass/          — Store (потокобезопасный снимок состояний) + REST-клиент
  internal/scada/         — рендерер SVG: theme/builder/geometry/gauge/icons/flow/render
  internal/web/           — HTTP-сервер (ingress) + цикл опроса + запись в /config/www
  internal/scada/testdata/golden.svg — эталон рендера для regress-теста
  Dockerfile, build.yaml, config.yaml — упаковка аддона
```

Рендер — чистая функция `scada.Render(state, cfg) string`: на одинаковом снимке состояний даёт байт-идентичный SVG, что и проверяет golden-тест.

## Разработка
Запускать из **Git Bash** (нужны `sh`, `go`; для деплоя — `plink`/`pscp` из PuTTY).

```
make check     # gofmt-check + vet + test  (CI-гейт)
make test      # юнит-тесты
make cover     # покрытие
make build     # локальная сборка бинарника (саннити; реально HA собирает в Docker)
make golden    # перегенерировать эталон после намеренного изменения вида
make help      # все цели
```

## Деплой
Add-on установлен из этого GitHub-репозитория, поэтому деплой = `git push` + на HAOS `ha store reload` → `ha addons update` → `restart`. Идёт через два SSH-хопа (dev → FreeBSD-мост → HAOS).

Настройка (один раз): `cp deploy.local.mk.example deploy.local.mk` и заполнить (host/пароль/ключ/слаг). Файл **в .gitignore** — секреты не попадают в публичный репозиторий.

```
make release MSG="0.5.0: ..."   # bump версии + check + commit + push + удалённый update
make deploy  MSG="..."          # check + commit/push + удалённый update (без bump)
make remote-update              # только перезалить/перезапустить аддон на HAOS
make logs                       # хвост логов аддона
```

> ⚠️ `config.yaml` содержит кириллицу в UTF-8 — править только редактором, сохраняющим UTF-8 без BOM (например через `make bump`), **не** через `Set-Content`/`Out-File` PowerShell.

## Конфигурация (опции аддона)
Названия вводов, подписи 4 солнечных полей, ёмкость батареи, пороги гейджей Дома и солнца, интервал, URL/токен HA — задаются в опциях аддона (`config.yaml` → `options`/`schema`).
