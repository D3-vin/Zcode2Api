# ⚡ Zcode2API

OpenAI & Anthropic-compatible proxy for Z.AI (ZCode Start Plan) with JWT account pool, automatic failover, captcha solving, and smart proxy rotation.

[![Version](https://img.shields.io/badge/version-1.0.0-blue)](https://github.com/D3-vin/Zcode2Api/releases)
[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-AS_IS-green)](#license)

[![Telegram](https://img.shields.io/badge/Telegram-D3__vin-blue?logo=telegram)](https://t.me/D3_vin)
[![Author](https://img.shields.io/badge/Author-D3vin__dev-blue?logo=telegram)](https://t.me/D3vin_dev)
[![GitHub](https://img.shields.io/badge/GitHub-Repository-black?logo=github)](https://github.com/D3-vin/Zcode2Api)

[Features](#features) • [Quick Start](#quick-start) • [Usage](#usage) • [Configuration](#configuration) • [API Endpoints](#api-endpoints) • [Contact](#contact)

[English](#) | [Русский](README_RU.md)

---

## Features

- 🚀 **Single Binary** — server, captcha solver, and token pool in one executable
- 🔓 **Dual Protocol** — Anthropic Messages (`/v1/messages`) + OpenAI Chat (`/v1/chat/completions`)
- 🧩 **Account Pool** — JWT rotation with automatic failover on quota exhaustion
- 🛡️ **Smart Failover** — handles 429, load errors (3012/3007/3009/3010/529) with proxy rotation
- 📊 **Live Dashboard** — account quotas, proxy profiles, real-time monitoring
- 🌐 **Proxy Support** — sticky/rotate modes, auto-pause on failures, IP probing
- 🔐 **Captcha Auto-Solve** — embedded Node.js solver for Aliyun captchas
- 🎯 **Dynamic Models** — catalog synced from account billing (currently **GLM-5.3** & **GLM-5.3-Flash**)
- 📦 **Zero Dependencies** — Node.js solver embedded, SQLite database, no external services

> 💡 **Works with Cursor, OpenCode, Claude Code, and any OpenAI/Anthropic-compatible client out of the box!**

---

## Quick Start

### 1. Download

Download the latest release for your platform:
- [Windows (64-bit)](https://github.com/D3-vin/Zcode2Api/releases)
- [Linux (64-bit)](https://github.com/D3-vin/Zcode2Api/releases)
- [macOS Intel](https://github.com/D3-vin/Zcode2Api/releases)
- [macOS Apple Silicon](https://github.com/D3-vin/Zcode2Api/releases)

Or build from source:

```bash
git clone https://github.com/D3-vin/Zcode2Api.git
cd Zcode2Api
go build -trimpath -ldflags="-s -w" -o zcode2api ./cmd/zcode2api
```

**Requirements:**
- **Node.js on PATH** (for captcha solver)
- Go 1.25+ (if building from source)

### 2. Configure

```bash
cp .env.example .env
# Edit .env — change AUTH_TOKEN to your own secret
```

### 3. Run

```bash
./zcode2api
```

Dashboard: http://localhost:3101

### 4. Add Accounts

**Option 1: Dashboard Login (Recommended)**
1. Open http://localhost:3101 → login with your `AUTH_TOKEN`
2. Click **Login Z.AI** → authorize via OAuth
3. JWT auto-added to pool with quota refresh

**Option 2: Bulk Import**
1. Dashboard → **Add (bulk)** → paste JWT tokens (one per line)

**Option 3: File Import**
1. Create `tokens.txt` with JWT tokens (one per line)
2. Set `ZCODE_KEYS=tokens.txt` in `.env`
3. Restart server

### 5. Test

**OpenAI format:**
```bash
curl -N http://localhost:3101/v1/chat/completions \
  -H "Authorization: Bearer d3vin" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "GLM-5.3",
    "messages": [{"role": "user", "content": "Hello!"}],
    "stream": true
  }'
```

**Anthropic format:**
```bash
curl -N http://localhost:3101/v1/messages \
  -H "x-api-key: d3vin" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "GLM-5.3",
    "messages": [{"role": "user", "content": "Hello!"}],
    "max_tokens": 1024,
    "stream": true
  }'
```

---

## Usage

### IDE Configuration

**For OpenAI-compatible clients (Cursor, OpenCode, etc.):**
```
Base URL: http://localhost:3101/v1
API Key: d3vin  # your AUTH_TOKEN
Model: GLM-5.3
```

**For Anthropic-compatible clients (Claude Code):**
```
Base URL: http://localhost:3101
API Key: d3vin  # your AUTH_TOKEN
Model: GLM-5.3
```

### Dashboard Features

**Accounts Tab:**
- Add accounts via OAuth, bulk JWT paste, or file import
- View quotas, status (Active/Standby/Cooling/Exhausted/Revoked/Disabled)
- Manual quota refresh or auto-refresh (configurable interval)
- Claim available plans with preview
- Export/import pool with device identities (JSON)

**Proxies Tab:**
- Add proxy profiles (sticky/rotate mode)
- Assign proxies to specific accounts (sticky egress)
- Auto-pause after 2 probe failures
- IP probing with configurable delay

**Settings:**
- Change `AUTH_TOKEN` and `GATEWAY_KEY` on-the-fly (persists to DB)
- Configure billing workers, failover limits, captcha timeouts

---

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/v1/messages` | Anthropic Messages API (streaming & non-streaming) |
| POST | `/v1/chat/completions` | OpenAI Chat Completions (auto-converted to Anthropic) |
| POST | `/async/v1/messages` | Async ticket-based SSE (ticket → ready → chunks → done) |
| GET | `/v1/models` | Available models (synced from account billing) |
| GET | `/v1/usage` | Usage statistics |
| GET | `/api/health` | Server health check |
| GET | `/` | Dashboard (login-gated at `/login.html`) |

**Authorization:**
- Header: `Authorization: Bearer <token>` or `x-api-key: <token>`
- Token: `AUTH_TOKEN` (dashboard + `/api/*`) or `GATEWAY_KEY` (if set, for `/v1/*` and `/async`)

---

## Configuration

All settings in `.env` (see `.env.example` for full reference):

| Variable | Default | Description |
|----------|---------|-------------|
| `HOST` | `127.0.0.1` | Server bind address |
| `PORT` | `3101` | Server port |
| `AUTH_TOKEN` | `d3vin` | Dashboard & API authentication token |
| `GATEWAY_KEY` | _(empty)_ | Separate key for `/v1/*` endpoints (defaults to `AUTH_TOKEN`) |
| `ZCODE_KEYS` | _(empty)_ | JWT file to seed pool at startup (one per line) |
| `PROXY_FILE` | `proxy.txt` | One-time seed of proxy profiles on empty DB |
| `LOAD_SWAPS` | `4` | Egress rotations on load errors (3012/3007/3009/3010) |
| `ACCOUNT_MAX_TRY` | `8` | Max account attempts per client request (failover) |
| `BILLING_WORKERS` | `25` | Concurrent workers for quota refresh |
| `CAPTCHA_SOLVE_TIMEOUT` | `40s` | Captcha solver timeout |
| `CAPTCHA_RETRIES` | `3` | Captcha retry attempts |
| `CAPTCHA_MODE` | _(empty)_ | Set to `off` to disable background captcha mint |

**Dynamic Settings:**
- `AUTH_TOKEN` and `GATEWAY_KEY` can be changed in Dashboard → Settings (persists to DB, survives restarts)

---

## Error Handling

| Error Type | Behavior |
|------------|----------|
| Model exhaustion (429) | Mark account as dead until quota period resets, try next account |
| Load errors (3012/3007/3009/3010/529) | Pause, rotate egress, mint fresh captcha, retry |
| 429 with `Retry-After` | Wait (capped at 120s) up to 5 retries on same egress |
| 401 Unauthorized | Mark account as Revoked, remove from rotation |
| `max_tokens` > 131072 | Clamp to 131072 (origin returns 400/1210 above this limit) |

**Account Statuses:**
- **Active** — ready for requests
- **Standby** — available but not currently selected
- **Cooling** — 3 strikes → 30-minute cooldown
- **Exhausted** — quota depleted, waiting for reset
- **Revoked** — 401 error, invalid JWT
- **Expired** — JWT expired
- **Disabled** — manually toggled off (survives quota refresh)

---

## Proxies

**Supported Formats:**
```
http://proxy.example.com:8080
http://user:pass@proxy.example.com:8080
https://proxy.example.com:443
socks5://proxy.example.com:1080
socks5://user:pass@proxy.example.com:1080
```

**Behavior:**
- Chat and billing requests use one egress per account (sticky)
- Captcha minting always goes direct from server machine
- Empty proxy pool = direct connection

**Proxy Profiles:**
- **Sticky mode** — assign specific proxy to account (egress locked)
- **Rotate mode** — proxy rotates on each request
- **Auto-pause** — disables proxy after 2 consecutive probe failures
- **IP Probe** — periodic IP check with configurable delay

---

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| `captcha solver failed` | Node.js not on PATH or captcha timeout | Ensure `node` command works, increase `CAPTCHA_SOLVE_TIMEOUT` |
| `all accounts exhausted` | No active accounts with quota | Add more accounts or wait for quota reset |
| `429 rate limit` | Too many requests | Server auto-retries with backoff; client should implement exponential backoff |
| `proxy connection failed` | Bad proxy or network issue | Check proxy credentials, disable failing proxies in dashboard |
| Dashboard login fails | Wrong `AUTH_TOKEN` | Check `.env` or update token in Settings |

---

## Project Structure

```
Zcode2Api/
├── zcode2api                    # Binary (Linux/macOS)
├── zcode2api.exe                # Binary (Windows)
├── .env                         # Configuration (copy from .env.example)
├── .env.example                 # Configuration template
├── proxy.txt                    # Proxy seed file (optional)
├── tokens.txt                   # JWT seed file (optional)
├── data/                        # Runtime data directory
│   ├── zcode2api.db            # SQLite database (accounts, proxies, settings)
│   └── captcha_node/           # Embedded captcha solver (auto-extracted)
├── cmd/zcode2api/               # Main application
├── internal/
│   ├── gateway/                 # Core server logic
│   ├── captcha/                 # Captcha solver integration
│   ├── store/                   # Database layer
│   ├── upstream/                # Z.AI client
│   ├── proxy/                   # Proxy management
│   └── config/                  # Configuration loader
└── tests/                       # Test suite
```

---

## Building

**Cross-platform builds:**

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

**Custom version:**
```bash
go build -ldflags "-X main.AppVersion=1.0.1" -o zcode2api ./cmd/zcode2api
```

---

## Contact

- **GitHub**: https://github.com/D3-vin/Zcode2Api
- **Telegram**: [@D3_vin](https://t.me/D3_vin)
- **Author**: [@D3vin_dev](https://t.me/D3vin_dev)

---

## License

Provided AS-IS without warranty. Use at your own risk.

**⚠️ Disclaimer**: This tool is for educational purposes. Ensure compliance with Z.AI Terms of Service.
