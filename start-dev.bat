@echo off
setlocal
cd /d "%~dp0"
set "SERVER_MANAGER_WEBUI_DIR=%~dp0internal\webui\static"
set "LOG_DIR=%LOCALAPPDATA%\ServerManager"
if not exist "%LOG_DIR%" mkdir "%LOG_DIR%" >nul 2>&1
set "DEV_EXE=%LOG_DIR%\ServerManager-dev.exe"
set "CLI_EXE=%USERPROFILE%\.local\bin\server-manager.exe"

set "GO_BIN=go"
where go >nul 2>&1
if errorlevel 1 if exist "%LOCALAPPDATA%\Programs\Go\bin\go.exe" set "GO_BIN=%LOCALAPPDATA%\Programs\Go\bin\go.exe"

set "DO_RESTART=0"
set "ARGS="
:argloop
if "%~1"=="" goto :check
if /i "%~1"=="--restart" (
  set "DO_RESTART=1"
  shift
  goto :argloop
)
set "ARGS=%ARGS% "%~1""
shift
goto :argloop

:check
if "%DO_RESTART%"=="1" (
  if exist "%CLI_EXE%" (
    "%CLI_EXE%" quit >nul 2>&1
  ) else (
    "%GO_BIN%" run ./cmd/server-manager quit >nul 2>&1
  )
  timeout /t 1 /nobreak >nul
)

powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "try { $r = Invoke-WebRequest -UseBasicParsing -TimeoutSec 1 http://127.0.0.1:8790/api/v1/health; if ($r.StatusCode -eq 200) { exit 0 } } catch {}; exit 1" >nul 2>&1
if %ERRORLEVEL%==0 (
  if exist "%DEV_EXE%" (
    start "" "%DEV_EXE%" %ARGS%
    exit /b 0
  )
  if exist "%USERPROFILE%\.local\bin\ServerManager.exe" (
    start "" "%USERPROFILE%\.local\bin\ServerManager.exe" %ARGS%
    exit /b 0
  )
  start "" "http://127.0.0.1:8790"
  exit /b 0
)

"%GO_BIN%" build -trimpath -ldflags="-s -w -H=windowsgui" -o "%DEV_EXE%" ./cmd/ServerManager 1>"%LOG_DIR%\dev-launcher.log" 2>&1
if errorlevel 1 exit /b %ERRORLEVEL%
start "" "%DEV_EXE%" %ARGS%
exit /b 0
