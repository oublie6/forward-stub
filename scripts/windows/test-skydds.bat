@echo off
setlocal

REM Windows 本地辅助测试脚本（主线仍建议在 WSL/Linux 执行）。
set ROOT_DIR=%~dp0\..\..
pushd "%ROOT_DIR%"

go test ./src/config -run SkyDDS -count=1
if errorlevel 1 (
  echo [ERROR] config SkyDDS tests failed.
  popd
  exit /b 1
)

go test ./src/receiver -run SkyDDS -count=1
if errorlevel 1 (
  echo [ERROR] receiver SkyDDS tests failed.
  popd
  exit /b 1
)

echo [INFO] SkyDDS related tests finished.
popd
exit /b 0
