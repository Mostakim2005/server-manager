@echo off
setlocal
cd /d "%~dp0"
set "SERVER_MANAGER_WEBUI_DIR=%~dp0internal\webui\static"
set "LOG_DIR=%LOCALAPPDATA%\ServerManager"
if not exist "%LOG_DIR%" mkdir "%LOG_DIR%" >nul 2>&1

set "GO_BIN=go"
where go >nul 2>&1
if errorlevel 1 if exist "%LOCALAPPDATA%\Programs\Go\bin\go.exe" set "GO_BIN=%LOCALAPPDATA%\Programs\Go\bin\go.exe"

set "ARGS="
:argloop
if "%~1"=="" goto :run
if /i "%~1"=="--restart" (
  "%GO_BIN%" run ./cmd/server-manager quit >nul 2>&1
  timeout /t 1 /nobreak >nul
  shift
  goto :argloop
)
set "ARGS=%ARGS% "%~1""
shift
goto :argloop

:run
"%GO_BIN%" run -ldflags="-H=windowsgui" ./cmd/ServerManager %ARGS% 1>"%LOG_DIR%\dev-launcher.log" 2>&1
exit /b %ERRORLEVEL%
