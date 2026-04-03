@echo off
setlocal

REM Windows 本地辅助脚本（建议在 WSL/Linux/Bookworm 环境执行主线构建）。
REM 本脚本仅尝试调用仓库现有 SkyDDS 构建路径，不保证原生 Windows SDK 兼容性。

set ROOT_DIR=%~dp0\..\..
pushd "%ROOT_DIR%"

if not exist "third_party\skydds\sdk" (
  echo [ERROR] SDK directory not found: third_party\skydds\sdk
  echo [INFO] Please prepare SDK in WSL/Linux first, then rerun.
  popd
  exit /b 1
)

set CGO_ENABLED=1
go build -trimpath -tags skydds -o bin\forward-stub.exe .
if errorlevel 1 (
  echo [ERROR] build failed.
  popd
  exit /b 1
)

echo [INFO] build success: bin\forward-stub.exe
popd
exit /b 0
