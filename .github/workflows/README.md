# GitHub Actions Workflows

## Release Workflow

Автоматическая сборка и публикация релизов при создании тега.

### Триггер

Запускается при push тега с префиксом `v*` (например: `v1.1.2`, `v2.0.0`)

### Процесс

1. **Build** — собирает бинарники для 4 платформ параллельно:
   - Windows AMD64 (.exe)
   - Linux AMD64
   - macOS Intel (AMD64)
   - macOS Apple Silicon (ARM64)

2. **Release** — создаёт GitHub Release с:
   - Автогенерированным changelog из коммитов между тегами
   - Всеми бинарниками
   - `env.example` как config template
   - Готовым описанием с таблицей Downloads

### Использование

```bash
# 1. Создать тег
git tag -a v1.1.2 -m "Zcode2API v1.1.2 - Description"

# 2. Запушить тег
git push origin v1.1.2

# 3. Дождаться завершения workflow (3-5 минут)
# Релиз появится автоматически: https://github.com/D3-vin/Zcode2Api/releases
```

### Мониторинг

- **Actions tab**: https://github.com/D3-vin/Zcode2Api/actions
- **Email notifications**: GitHub отправит письмо при фейле

### Troubleshooting

**Build failed:**
- Проверить логи в Actions tab
- Убедиться что код компилируется локально: `go build ./cmd/zcode2api`

**Release not created:**
- Проверить что тег начинается с `v`
- Убедиться что `GITHUB_TOKEN` имеет права `contents: write` (настроено в workflow)

**Missing artifacts:**
- Workflow автоматически фейлится если бинарники не собрались
- Проверить логи Build job

### Настройки

Все параметры в `.github/workflows/release.yml`:
- **Go version**: `1.25` (меняется в `uses: actions/setup-go@v5`)
- **Build flags**: `-trimpath -ldflags="-s -w"` (минимальный размер)
- **CGO**: отключён (`CGO_ENABLED=0`) для статических бинарников

### Ручная сборка

Если нужно собрать вручную:

```bash
# Windows
.\build.cmd

# Linux/macOS
./build.sh
```
