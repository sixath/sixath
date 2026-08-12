@echo off
REM Double-click one-click deploy via WSL (build + up + smoke).
cd /d "%~dp0.."
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0deploy-wsl.ps1" -Build %*
if errorlevel 1 (
  echo.
  echo Deploy failed. See messages above.
  pause
  exit /b 1
)
echo.
pause
