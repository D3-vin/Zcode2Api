# GitHub Actions Workflows

## Release Workflow

Automated build and release publishing when creating a tag.

### Trigger

Runs on push of tags with `v*` prefix (e.g., `v1.1.2`, `v2.0.0`)

### Process

1. **Build** — compiles binaries for 4 platforms in parallel:
   - Windows AMD64 (.exe)
   - Linux AMD64
   - macOS Intel (AMD64)
   - macOS Apple Silicon (ARM64)

2. **Release** — creates GitHub Release with:
   - Auto-generated changelog from commits between tags
   - All binaries
   - `env.example` as config template
   - Ready-to-use description with Downloads table

### Usage

```bash
# 1. Create tag
git tag -a v1.1.2 -m "Zcode2API v1.1.2 - Description"

# 2. Push tag
git push origin v1.1.2

# 3. Wait for workflow completion (3-5 minutes)
# Release will appear automatically: https://github.com/D3-vin/Zcode2Api/releases
```

### Monitoring

- **Actions tab**: https://github.com/D3-vin/Zcode2Api/actions
- **Email notifications**: GitHub will send email on failure

### Troubleshooting

**Build failed:**
- Check logs in Actions tab
- Ensure code compiles locally: `go build ./cmd/zcode2api`

**Release not created:**
- Verify tag starts with `v`
- Ensure `GITHUB_TOKEN` has `contents: write` permissions (configured in workflow)

**Missing artifacts:**
- Workflow automatically fails if binaries don't build
- Check Build job logs

### Configuration

All parameters in `.github/workflows/release.yml`:
- **Go version**: `1.25` (change in `uses: actions/setup-go@v5`)
- **Build flags**: `-trimpath -ldflags="-s -w"` (minimal size)
- **CGO**: disabled (`CGO_ENABLED=0`) for static binaries

### Manual Build

If you need to build manually:

```bash
# Windows
.\build.cmd

# Linux/macOS
./build.sh
```
