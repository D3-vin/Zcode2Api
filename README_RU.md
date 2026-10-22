# ⚡ Zcode2API

OpenAI и Anthropic-совместимый прокси для Z.AI (ZCode Start Plan) с пулом JWT-аккаунтов, автоматическим фейловером, обработкой капчи и умной ротацией прокси.

[![Version](https://img.shields.io/badge/version-1.0.0-blue)](https://github.com/D3-vin/Zcode2Api/releases)
[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-AS_IS-green)](#лицензия)

[![Telegram](https://img.shields.io/badge/Telegram-D3__vin-blue?logo=telegram)](https://t.me/D3_vin)
[![Author](https://img.shields.io/badge/Author-D3vin__dev-blue?logo=telegram)](https://t.me/D3vin_dev)
[![GitHub](https://img.shields.io/badge/GitHub-Repository-black?logo=github)](https://github.com/D3-vin/Zcode2Api)

[Возможности](#возможности) • [Быстрый старт](#быстрый-старт) • [Использование](#использование) • [Конфигурация](#конфигурация) • [API эндпоинты](#api-эндпоинты) • [Контакты](#контакты)

[English](README.md) | [Русский](#)

---

## Возможности

- 🚀 **Один бинарник** — сервер, обработчик капчи и пул токенов в одном исполняемом файле
- 🔓 **Два протокола** — Anthropic Messages (`/v1/messages`) + OpenAI Chat (`/v1/chat/completions`)
- 🧩 **Пул аккаунтов** — ротация JWT с автоматическим фейловером при исчерпании квот
- 🛡️ **Умный фейловер** — обработка 429, ошибок нагрузки (3012/3007/3009/3010/529) с ротацией прокси
- 📊 **Живой дашборд** — квоты аккаунтов, профили прокси, мониторинг в реальном времени
- 🌐 **Поддержка прокси** — sticky/rotate режимы, авто-пауза при ошибках, проверка IP
- 🔐 **Обработка капчи** — встроенный Node.js модуль для Aliyun капчи
- 🎯 **Динамический каталог моделей** — синхронизация из биллинга аккаунтов (сейчас **GLM-5.3** и **GLM-5.3-Flash**)
- 📦 **Нулевые зависимости** — встроенный модуль Node.js, SQLite база, без внешних сервисов

> 💡 **Работает с Cursor, OpenCode, Claude Code и любым OpenAI/Anthropic-совместимым клиентом из коробки!**

---

## Быстрый старт

### 1. Скачать

Загрузите последний релиз для вашей платформы:
- [Windows (64-bit)](https://github.com/D3-vin/Zcode2Api/releases)
- [Linux (64-bit)](https://github.com/D3-vin/Zcode2Api/releases)
- [macOS Intel](https://github.com/D3-vin/Zcode2Api/releases)
- [macOS Apple Silicon](https://github.com/D3-vin/Zcode2Api/releases)

Или соберите из исходников:

```bash
git clone https://github.com/D3-vin/Zcode2Api.git
cd Zcode2Api
go build -trimpath -ldflags="-s -w" -o zcode2api ./cmd/zcode2api
```

**Требования:**
- **Node.js в PATH** (для обработчика капчи)
- Go 1.25+ (если сборка из исходников)

### 2. Настроить

```bash
cp .env.example .env
# Отредактируйте .env — смените AUTH_TOKEN на свой секретный ключ
```

### 3. Запустить

```bash
./zcode2api
```

Дашборд: http://localhost:3101

### 4. Добавить аккаунты

**Вариант 1: Вход через дашборд (рекомендуется)**
1. Откройте http://localhost:3101 → войдите с вашим `AUTH_TOKEN`
2. Нажмите **Login Z.AI** → авторизуйтесь через OAuth
3. JWT автоматически добавится в пул с обновлением квот

**Вариант 2: Массовый импорт**
1. Дашборд → **Add (bulk)** → вставьте JWT-токены (по одному на строку)

**Вариант 3: Импорт из файла**
1. Создайте `tokens.txt` с JWT-токенами (по одному на строку)
2. Установите `ZCODE_KEYS=tokens.txt` в `.env`
3. Перезапустите сервер

### 5. Тест

**Формат OpenAI:**
```bash
curl -N http://localhost:3101/v1/chat/completions \
  -H "Authorization: Bearer d3vin" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "GLM-5.3",
    "messages": [{"role": "user", "content": "Привет!"}],
    "stream": true
  }'
```

**Формат Anthropic:**
```bash
curl -N http://localhost:3101/v1/messages \
  -H "x-api-key: d3vin" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "GLM-5.3",
    "messages": [{"role": "user", "content": "Привет!"}],
    "max_tokens": 1024,
    "stream": true
  }'
```

---

## Использование

### Настройка IDE

**Для OpenAI-совместимых клиентов (Cursor, OpenCode и др.):**
```
Base URL: http://localhost:3101/v1
API Key: d3vin  # ваш AUTH_TOKEN
Model: GLM-5.3
```

**Для Anthropic-совместимых клиентов (Claude Code):**
```
Base URL: http://localhost:3101
API Key: d3vin  # ваш AUTH_TOKEN
Model: GLM-5.3
```

### Возможности дашборда

**Вкладка Accounts:**
- Добавление аккаунтов через OAuth, массовую вставку JWT или импорт из файла
- Просмотр квот, статусов (Active/Standby/Cooling/Exhausted/Revoked/Disabled)
- Ручное обновление квот или авто-обновление (настраиваемый интервал)
- Клейм доступных планов с предпросмотром
- Экспорт/импорт пула с device-идентичностями (JSON)

**Вкладка Proxies:**
- Добавление профилей прокси (sticky/rotate режим)
- Привязка прокси к конкретным аккаунтам (sticky egress)
- Авто-пауза после 2 неудачных проверок
- Проверка IP с настраиваемой задержкой

**Вкладка Settings:**
- Изменение `AUTH_TOKEN` и `GATEWAY_KEY` на лету (сохраняется в БД)
- Настройка воркеров биллинга, лимитов фейловера, таймаутов капчи

---

## API эндпоинты

| Метод | Путь | Описание |
|-------|------|----------|
| POST | `/v1/messages` | Anthropic Messages API (стриминг и обычный) |
| POST | `/v1/chat/completions` | OpenAI Chat Completions (авто-конверсия в Anthropic) |
| POST | `/async/v1/messages` | Асинхронный тикет-SSE (ticket → ready → chunks → done) |
| GET | `/v1/models` | Доступные модели (синхронизация из биллинга аккаунтов) |
| GET | `/v1/usage` | Статистика использования |
| GET | `/api/health` | Проверка здоровья сервера |
| GET | `/` | Дашборд (login-гейт на `/login.html`) |

**Авторизация:**
- Заголовок: `Authorization: Bearer <token>` или `x-api-key: <token>`
- Токен: `AUTH_TOKEN` (дашборд + `/api/*`) или `GATEWAY_KEY` (если установлен, для `/v1/*` и `/async`)

---

## Конфигурация

Все настройки в `.env` (см. `.env.example` для полного списка):

| Переменная | По умолчанию | Описание |
|------------|--------------|----------|
| `HOST` | `127.0.0.1` | Адрес привязки сервера |
| `PORT` | `3101` | Порт сервера |
| `AUTH_TOKEN` | `d3vin` | Токен авторизации дашборда и API |
| `GATEWAY_KEY` | _(пусто)_ | Отдельный ключ для `/v1/*` эндпоинтов (по умолчанию `AUTH_TOKEN`) |
| `ZCODE_KEYS` | _(пусто)_ | Файл JWT для заполнения пула при старте (по одному на строку) |
| `PROXY_FILE` | `proxy.txt` | Одноразовое заполнение профилей прокси в пустой БД |
| `LOAD_SWAPS` | `4` | Ротации egress при ошибках нагрузки (3012/3007/3009/3010) |
| `ACCOUNT_MAX_TRY` | `8` | Макс. попыток аккаунтов на один запрос клиента (фейловер) |
| `BILLING_WORKERS` | `25` | Параллельные воркеры для обновления квот |
| `CAPTCHA_SOLVE_TIMEOUT` | `40s` | Таймаут обработчика капчи |
| `CAPTCHA_RETRIES` | `3` | Попытки повтора капчи |
| `CAPTCHA_MODE` | _(пусто)_ | Установите `off` для отключения фонового минта капчи |

**Динамические настройки:**
- `AUTH_TOKEN` и `GATEWAY_KEY` можно изменить в Дашборд → Settings (сохраняется в БД, переживает рестарты)

---

## Обработка ошибок

| Тип ошибки | Поведение |
|------------|-----------|
| Исчерпание модели (429) | Пометка аккаунта как мёртвого до сброса квот, переход к следующему |
| Ошибки нагрузки (3012/3007/3009/3010/529) | Пауза, ротация egress, свежая капча, повтор |
| 429 с `Retry-After` | Ожидание (макс. 120с) до 5 повторов на том же egress |
| 401 Unauthorized | Пометка аккаунта как Revoked, удаление из ротации |
| `max_tokens` > 131072 | Обрезка до 131072 (origin возвращает 400/1210 выше этого лимита) |

**Статусы аккаунтов:**
- **Active** — готов к запросам
- **Standby** — доступен, но не выбран
- **Cooling** — 3 страйка → 30-минутный кулдаун
- **Exhausted** — квота исчерпана, ожидание сброса
- **Revoked** — ошибка 401, невалидный JWT
- **Expired** — JWT истёк
- **Disabled** — вручную отключён (переживает обновление квот)

---

## Прокси

**Поддерживаемые форматы:**
```
http://proxy.example.com:8080
http://user:pass@proxy.example.com:8080
https://proxy.example.com:443
socks5://proxy.example.com:1080
socks5://user:pass@proxy.example.com:1080
```

**Поведение:**
- Чат и биллинг используют один egress на аккаунт (sticky)
- Минт капчи всегда идёт напрямую с машины сервера
- Пустой пул прокси = прямое соединение

**Профили прокси:**
- **Sticky режим** — привязка конкретного прокси к аккаунту (egress закреплён)
- **Rotate режим** — прокси ротируется на каждом запросе
- **Авто-пауза** — отключает прокси после 2 последовательных неудачных проверок
- **IP Probe** — периодическая проверка IP с настраиваемой задержкой

---

## Решение проблем

| Симптом | Причина | Решение |
|---------|---------|---------|
| `captcha solver failed` | Node.js не в PATH или таймаут капчи | Убедитесь, что команда `node` работает, увеличьте `CAPTCHA_SOLVE_TIMEOUT` |
| `all accounts exhausted` | Нет активных аккаунтов с квотами | Добавьте больше аккаунтов или дождитесь сброса квот |
| `429 rate limit` | Слишком много запросов | Сервер авто-ретраит с backoff; клиент должен использовать exponential backoff |
| `proxy connection failed` | Плохой прокси или проблема сети | Проверьте credentials прокси, отключите неработающие прокси в дашборде |
| Вход в дашборд не работает | Неверный `AUTH_TOKEN` | Проверьте `.env` или обновите токен в Settings |

---

## Структура проекта

```
Zcode2Api/
├── zcode2api                    # Бинарник (Linux/macOS)
├── zcode2api.exe                # Бинарник (Windows)
├── .env                         # Конфигурация (скопируйте из .env.example)
├── .env.example                 # Шаблон конфигурации
├── proxy.txt                    # Файл заполнения прокси (опционально)
├── tokens.txt                   # Файл заполнения JWT (опционально)
├── data/                        # Директория runtime данных
│   ├── zcode2api.db            # SQLite база (аккаунты, прокси, настройки)
│   └── captcha_node/           # Встроенный модуль капчи (авто-извлечение)
├── cmd/zcode2api/               # Основное приложение
├── internal/
│   ├── gateway/                 # Ядро серверной логики
│   ├── captcha/                 # Интеграция обработчика капчи
│   ├── store/                   # Слой базы данных
│   ├── upstream/                # Z.AI клиент
│   ├── proxy/                   # Управление прокси
│   └── config/                  # Загрузчик конфигурации
└── tests/                       # Набор тестов
```

---

## Сборка

**Кросс-платформенная сборка:**

```bash
# Windows AMD64
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o build/zcode2api-windows-amd64.exe ./cmd/zcode2api

# Linux AMD64
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o build/zcode2api-linux-amd64 ./cmd/zcode2api

# macOS Intel
GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o build/zcode2api-macos-amd64 ./cmd/zcode2api

# macOS Apple Silicon
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o build/zcode2api-macos-arm64 ./cmd/zcode2api
```

**Кастомная версия:**
```bash
go build -ldflags "-X main.AppVersion=1.0.1" -o zcode2api ./cmd/zcode2api
```

---

## Контакты

- **GitHub**: https://github.com/D3-vin/Zcode2Api
- **Telegram**: [@D3_vin](https://t.me/D3_vin)
- **Автор**: [@D3vin_dev](https://t.me/D3vin_dev)

---

## Лицензия

Предоставляется «как есть» в образовательных целях и для целей совместимости — см. файл [LICENSE](LICENSE).

## ⚠️ Отказ от ответственности

ПО предоставляется **исключительно в образовательных и ознакомительных целях и для целей совместимости**, «как есть» (AS IS), без каких-либо гарантий. Автор не несёт ответственности за любое использование и последствия — вся ответственность лежит на конечном пользователе. Используйте в соответствии с условиями сервиса Z.AI. Не аффилировано с Z.AI и Alibaba Cloud.

> This software is provided for educational and interoperability purposes only, "AS IS". The author is not responsible for any misuse.
