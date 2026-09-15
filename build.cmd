@echo off
echo Building Zcode2API for all platforms...
echo.

REM Create build directory
if not exist "build" mkdir build

REM Windows AMD64
echo [1/4] Building Windows AMD64...
set GOOS=windows
set GOARCH=amd64
go build -trimpath -ldflags="-s -w" -o build/zcode2api-windows-amd64.exe ./cmd/zcode2api
if %ERRORLEVEL% NEQ 0 goto error

REM Linux AMD64
echo [2/4] Building Linux AMD64...
set GOOS=linux
set GOARCH=amd64
go build -trimpath -ldflags="-s -w" -o build/zcode2api-linux-amd64 ./cmd/zcode2api
if %ERRORLEVEL% NEQ 0 goto error

REM macOS AMD64 (Intel)
echo [3/4] Building macOS AMD64 (Intel)...
set GOOS=darwin
set GOARCH=amd64
go build -trimpath -ldflags="-s -w" -o build/zcode2api-macos-amd64 ./cmd/zcode2api
if %ERRORLEVEL% NEQ 0 goto error

REM macOS ARM64 (Apple Silicon)
echo [4/4] Building macOS ARM64 (Apple Silicon)...
set GOOS=darwin
set GOARCH=arm64
go build -trimpath -ldflags="-s -w" -o build/zcode2api-macos-arm64 ./cmd/zcode2api
if %ERRORLEVEL% NEQ 0 goto error

echo.
echo [32m✓ All builds completed successfully![0m
echo.
echo Final build artifacts:
dir /B build
echo.
echo [36mReady for GitHub release![0m
goto end

:error
echo.
echo [31m✗ Build failed![0m
exit /b 1

:end
