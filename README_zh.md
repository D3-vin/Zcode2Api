# ⚡ Zcode2API

面向 Z.AI(ZCode Start Plan)的 OpenAI 与 Anthropic 兼容代理,内置 JWT 账号池、自动故障转移、验证码处理与智能代理轮换。

[![Version](https://img.shields.io/badge/version-1.0.0-blue)](https://github.com/D3-vin/Zcode2Api/releases)
[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-AS_IS-green)](#许可证)

[![Telegram](https://img.shields.io/badge/Telegram-D3__vin-blue?logo=telegram)](https://t.me/D3_vin)
[![Author](https://img.shields.io/badge/Author-D3vin__dev-blue?logo=telegram)](https://t.me/D3vin_dev)
[![GitHub](https://img.shields.io/badge/GitHub-Repository-black?logo=github)](https://github.com/D3-vin/Zcode2Api)

[功能特性](#功能特性) • [快速开始](#快速开始) • [使用](#使用) • [配置](#配置) • [API 端点](#api-端点) • [联系我们](#联系我们)

[English](README.md) | [Русский](README_RU.md) | [简体中文](#)

---

## 功能特性

- 🚀 **单文件可执行** —— 服务器、验证码处理、令牌池集于一个可执行文件
- 🔓 **双协议** —— Anthropic Messages(`/v1/messages`)+ OpenAI Chat(`/v1/chat/completions`)
- 🧩 **账号池** —— JWT 轮换,额度耗尽时自动切换账号
- 🛡️ **智能故障转移** —— 处理 429 与负载错误(3012/3007/3009/3010/529),配合代理轮换
- 📊 **实时面板** —— 账号额度、代理配置、实时监控
- 🌐 **代理支持** —— 粘性/轮换模式,失败自动暂停,IP 探测
- 🔐 **验证码处理** —— 内嵌 Node.js 模块,应对阿里云验证码
- 🎯 **动态模型** —— 模型目录从账号账单同步(当前为 **GLM-5.3** 与 **GLM-5.3-Flash**)
- 📦 **零外部依赖** —— Node.js 模块内嵌,SQLite 数据库,无需外部服务

> 💡 **开箱即用:Cursor、OpenCode、Claude Code 以及任何 OpenAI/Anthropic 兼容客户端都能直接接入!**

---

## 快速开始

### 1. 下载

下载适合你平台的最新 Release:
- [Windows (64 位)](https://github.com/D3-vin/Zcode2Api/releases)
- [Linux (64 位)](https://github.com/D3-vin/Zcode2Api/releases)
- [macOS Intel](https://github.com/D3-vin/Zcode2Api/releases)
- [macOS Apple Silicon](https://github.com/D3-vin/Zcode2Api/releases)

或从源码构建:

```bash
git clone https://github.com/D3-vin/Zcode2Api.git
cd Zcode2Api
go build -trimpath -ldflags="-s -w" -o zcode2api ./cmd/zcode2api
```

**环境要求:**
- **PATH 中有 Node.js**(用于验证码处理)
- Go 1.25+(仅从源码构建时需要)

### 2. 配置

```bash
cp .env.example .env
# 编辑 .env —— 把 AUTH_TOKEN 改成你自己的密钥
```

### 3. 运行

```bash
./zcode2api
```

面板地址:http://localhost:3101

### 4. 添加账号

**方式一:面板 OAuth 登录(推荐)**
1. 打开 http://localhost:3101 → 用你的 `AUTH_TOKEN` 登录
2. 点击 **Login Z.AI** → 通过 OAuth 授权
3. JWT 自动加入账号池并刷新额度

**方式二:批量粘贴**
1. 面板 → **添加(批量)** → 粘贴 JWT token(每行一条)

**方式三:文件导入**
1. 创建 `tokens.txt`,每行一条 JWT token
2. 在 `.env` 中设置 `ZCODE_KEYS=tokens.txt`
3. 重启服务

### 5. 测试

**OpenAI 格式:**
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

**Anthropic 格式:**
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

## 使用

### IDE 配置

**OpenAI 兼容客户端(Cursor、OpenCode 等):**
```
Base URL: http://localhost:3101/v1
API Key: d3vin  # 你的 AUTH_TOKEN
Model: GLM-5.3
```

**Anthropic 兼容客户端(Claude Code):**
```
Base URL: http://localhost:3101
API Key: d3vin  # 你的 AUTH_TOKEN
Model: GLM-5.3
```

### 面板功能

**账号页:**
- 通过 OAuth、批量粘贴 JWT 或文件导入添加账号
- 查看额度与状态(活跃/待命/冷却中/额度耗尽/已吊销/已停用)
- 手动刷新额度或自动刷新(间隔可配置)
- 预览并领取可用套餐
- 连同设备身份导出/导入账号池(JSON)

**代理页:**
- 添加代理配置(粘性/轮换模式)
- 将代理绑定到指定账号(粘性出口)
- 连续 2 次探测失败自动暂停
- IP 探测,延迟可配置

**设置页:**
- 在线修改 `AUTH_TOKEN` 与 `GATEWAY_KEY`(持久化到数据库)
- 配置账单并发数、故障转移上限、验证码超时

---

## API 端点

| 方法 | 路径 | 说明 |
|--------|------|-------------|
| POST | `/v1/messages` | Anthropic Messages API(支持流式与非流式) |
| POST | `/v1/chat/completions` | OpenAI Chat Completions(自动转换为 Anthropic 格式) |
| POST | `/async/v1/messages` | 异步工单式 SSE(ticket → ready → chunks → done) |
| GET | `/v1/models` | 可用模型(从账号账单同步) |
| GET | `/v1/usage` | 用量统计 |
| GET | `/api/health` | 服务健康检查 |
| GET | `/` | 管理面板(登录页在 `/login.html`) |

**鉴权:**
- 请求头:`Authorization: Bearer <token>` 或 `x-api-key: <token>`
- 令牌:`AUTH_TOKEN`(面板 + `/api/*`)或 `GATEWAY_KEY`(若设置,用于 `/v1/*` 与 `/async`)

---

## 配置

所有配置均在 `.env` 中(完整参考见 `.env.example`):

| 变量 | 默认值 | 说明 |
|----------|---------|-------------|
| `HOST` | `127.0.0.1` | 服务器绑定地址 |
| `PORT` | `3101` | 服务器端口 |
| `AUTH_TOKEN` | `d3vin` | 面板与 API 的认证令牌 |
| `GATEWAY_KEY` | _(空)_ | `/v1/*` 端点的独立密钥(缺省用 `AUTH_TOKEN`) |
| `ZCODE_KEYS` | _(空)_ | 启动时导入账号池的 JWT 文件(每行一条) |
| `PROXY_FILE` | `proxy.txt` | 空数据库时一次性导入的代理配置文件 |
| `LOAD_SWAPS` | `4` | 负载错误(3012/3007/3009/3010)时的出口轮换次数 |
| `ACCOUNT_MAX_TRY` | `8` | 每个客户端请求的最大账号尝试次数(故障转移) |
| `BILLING_WORKERS` | `25` | 额度刷新的并发 worker 数 |
| `CAPTCHA_SOLVE_TIMEOUT` | `40s` | 验证码处理超时 |
| `CAPTCHA_RETRIES` | `3` | 验证码重试次数 |
| `CAPTCHA_MODE` | _(空)_ | 设为 `off` 可关闭后台验证码预生成 |

**动态设置:**
- `AUTH_TOKEN` 与 `GATEWAY_KEY` 可在面板 → 设置中修改(持久化到数据库,重启后仍生效)

---

## 错误处理

| 错误类型 | 行为 |
|------------|----------|
| 模型额度耗尽(429) | 将账号标记为失效,待额度周期重置,尝试下一个账号 |
| 负载错误(3012/3007/3009/3010/529) | 暂停,轮换出口,重新生成验证码,重试 |
| 429 且带 `Retry-After` | 等待(上限 120 秒),同一条出口最多重试 5 次 |
| 401 未授权 | 将账号标记为已吊销,移出轮换 |
| `max_tokens` > 131072 | 钳制到 131072(上游超过此限制会返回 400/1210) |

**账号状态:**
- **活跃** —— 可接收请求
- **待命** —— 可用但当前未被选中
- **冷却中** —— 3 次违规 → 冷却 30 分钟
- **额度耗尽** —— 额度用完,等待重置
- **已吊销** —— 401 错误,JWT 无效
- **已过期** —— JWT 过期
- **已停用** —— 手动关闭(额度刷新不会改变该状态)

---

## 代理

**支持的格式:**
```
http://proxy.example.com:8080
http://user:pass@proxy.example.com:8080
https://proxy.example.com:443
socks5://proxy.example.com:1080
socks5://user:pass@proxy.example.com:1080
```

**行为:**
- 对话与账单请求按账号固定一条出口(粘性)
- 验证码生成始终从服务器本机直连
- 代理池为空 = 直连

**代理配置:**
- **粘性模式** —— 将特定代理绑定到账号(出口锁定)
- **轮换模式** —— 每次请求轮换代理
- **自动暂停** —— 连续 2 次探测失败后停用该代理
- **IP 探测** —— 周期性 IP 检查,延迟可配置

---

## 故障排查

| 现象 | 原因 | 解决 |
|---------|-------|-----|
| `captcha solver failed` | Node.js 不在 PATH 或验证码超时 | 确认 `node` 命令可用,增大 `CAPTCHA_SOLVE_TIMEOUT` |
| `all accounts exhausted` | 没有额度可用的活跃账号 | 添加更多账号或等待额度重置 |
| `429 rate limit` | 请求过多 | 服务端自动退避重试;客户端也应实现指数退避 |
| `proxy connection failed` | 代理失效或网络问题 | 检查代理凭据,在面板中停用故障代理 |
| 面板登录失败 | `AUTH_TOKEN` 不对 | 检查 `.env`,或在设置页更新令牌 |

---

## 项目结构

```
Zcode2Api/
├── zcode2api                    # 二进制(Linux/macOS)
├── zcode2api.exe                # 二进制(Windows)
├── .env                         # 配置(从 .env.example 复制)
├── .env.example                 # 配置模板
├── proxy.txt                    # 代理种子文件(可选)
├── tokens.txt                   # JWT 种子文件(可选)
├── data/                        # 运行时数据目录
│   ├── zcode2api.db            # SQLite 数据库(账号、代理、设置)
│   └── captcha_node/           # 内嵌验证码模块(自动释放)
├── cmd/zcode2api/               # 主程序入口
├── internal/
│   ├── gateway/                 # 核心服务逻辑
│   ├── captcha/                 # 验证码处理集成
│   ├── store/                   # 数据库层
│   ├── upstream/                # Z.AI 客户端
│   ├── proxy/                   # 代理管理
│   └── config/                  # 配置加载
└── tests/                       # 测试套件
```

---

## 构建

**跨平台构建:**

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

**自定义版本号:**
```bash
go build -ldflags "-X main.AppVersion=1.0.1" -o zcode2api ./cmd/zcode2api
```

---

## 联系我们

- **GitHub**: https://github.com/D3-vin/Zcode2Api
- **Telegram**: [@D3_vin](https://t.me/D3_vin)
- **作者**: [@D3vin_dev](https://t.me/D3vin_dev)

---

## 许可证

按“现状”提供,仅用于教育目的与互操作研究 —— 见 [LICENSE](LICENSE)。

## ⚠️ 免责声明

本软件**仅用于教育与互操作目的**,按“现状”提供,不附带任何形式的保证。作者不对任何滥用行为或后果负责 —— 全部责任由最终使用者承担。请遵守 Z.AI 的服务条款。本项目与 Z.AI 或阿里云无任何关联。
