# Project Operating Instructions (`AGENTS.md`)
> **Notice:** This project inherits user profile, confirmation gates, OS-default vs. Shared Libraries/Ecosystem Registry rules, GitHub-first CI/CD defaults, on-demand `AUDITS.md`/`UIUX.md` and Downloads-folder disposable report routing, pre-commit doc sync, visual standards, and documentation blueprints from the global master `AGENTS.md`. Do not duplicate those rules here.

## 1. Repository Classification & Tech Stack
* **Project Name:** Server Manager (`server-manager`)
* **Verification Mode:** `[GitHub-First (Remote Configured: https://github.com/Mostakim2005/server-manager)]`
* **Runtime / Language:** Go 1.24+ (zero CGO, pure-Go Windows Win32 tray + Job Objects via `golang.org/x/sys/windows`)
* **Framework / Host:** Embedded HTML5 + Material 3 CSS + ES Module Web UI (`//go:embed`) served on loopback `http://127.0.0.1:8790`
* **Target OS / Build Output:** Windows 10/11 x64 portable binaries (`ServerManager.exe` `-H=windowsgui` tray/app launcher + `server-manager.exe` CLI) and Desktop `.lnk` shortcut
* **Shared Libraries & Local Ports:**
  * Shared M3 tokens / icons resolved from `%LOCALAPPDATA%\SharedLibraries` (`LOCAL_COMMON_LIBRARIES`) with embedded offline fallbacks
  * Registry synced with `%USERPROFILE%\.local\share\ecosystem-registry.json` (`LOCAL_ECOSYSTEM_REGISTRY`) and `%LOCALAPPDATA%\SharedLibraries\ecosystem-registry.json`
  * Secrets read/registered in `%USERPROFILE%\.local\share\ecosystem-secrets.json` (`LOCAL_ECOSYSTEM_SECRETS`, key `tokens.serverManager`)
  * Loopback Service Port: `127.0.0.1:8790`
  * Optional AI Proxy Dependency: `CLIProxyAPI` at `127.0.0.1:8317` (auto-started via `~/.cli-proxy-api/start-cliproxy-hidden.vbs` when offline and enabled)

## 2. Verification & Build Commands (Mirrored in CI)
* **Format & Lint:** `gofmt -l cmd internal` and `go vet ./...`
* **Unit & Integration Tests:** `go test -race ./...` (or `go test ./...` on Windows without `-race` if CGO is disabled)
* **Windows Portable Build:** `CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w -H=windowsgui" -o build/ServerManager.exe ./cmd/ServerManager` and `go build -trimpath -ldflags="-s -w" -o build/server-manager.exe ./cmd/server-manager`
* **Smoke Test:** `server-manager health --json` and `server-manager list --json`
* **Structure & Doc Checks:** Validate `docs/*.md` `### [TYPE: Slug]` + `> **1-Line Summary:**` pairs and UTF-8 without BOM JSON configs

## 3. Repository Directory Structure
* `cmd/ServerManager/` — Windowless (`-H=windowsgui`) Windows GUI tray, `--launch-service` hidden boot runner, and `--app=` launcher entrypoint
* `cmd/server-manager/` — Headless `--json` CLI entrypoint (`list`, `status`, `start`, `stop`, `startup`, `ai-report`, `conflicts`, `install-shortcut`, `quit`)
* `internal/branding/` — Embedded SVG app icon + programmatic PNG & multi-size Windows `.ico` renderer
* `internal/config/` — Path discovery, UTF-8 no-BOM atomic writers, settings, custom entries, stack presets, AI report cache, and ecosystem registry/secrets helpers
* `internal/security/` — Loopback bind check, Host/Origin CSRF checks, constant-time bearer auth, confined `/shared-libs/` path check, secret redaction, and Windows Job Object supervision
* `internal/discovery/` — Multi-root `.bat`/`.cmd`/`.ps1` scanner, noise filter, paired stop-script matcher, smart project grouping & wrapper deduplication (`grouping.go`), static command/port parser, and port/registry conflict detector
* `internal/icons/` — Multi-source project icon lookup (`favicon.*`/`logo.*`/`icon.*`), Win32 `.exe` icon extraction (`extract_windows.go`), loopback Web UI favicon probing/cache, and custom icon overrides
* `internal/startup/` — Windows Startup folder `.lnk` manager, pre-existing shortcut adoption, silent launcher routing, and `StartupApproved` Task Manager disabled-state sync
* `internal/supervisor/` — Process tree launcher, detached hidden boot launcher, graceful/force stop, ring-buffered stdout/stderr log capture, live TCP port probe, stack orchestrator, and SSE event hub
* `internal/aiproxy/` — `CLIProxyAPI` (`127.0.0.1:8317`) client, model discovery, hidden auto-start helper, script report prompt builder, and folder-based AI Add Project analyzer
* `internal/tray/` — Pure-Go Win32 System Tray + Desktop shortcut `.lnk` generator
* `internal/server/` — Loopback HTTP REST + SSE server + CLIProxy/discovery/supervisor orchestration
* `internal/webui/` — Embedded static Web UI (`static/index.html`, `app.css`, `app.js`, `logo.svg`), optionally live-served from disk with SSE hot-reload via `ResolveRepoDir` / `SERVER_MANAGER_WEBUI_DIR`
* `start-dev.bat` / `start-dev-hidden.vbs` — Silent `go run ./cmd/ServerManager` dev launcher for `Desktop\Server Manager (Hot-Reload).lnk`
* `docs/` — Living Markdown docs (`PRODUCT.md`, `ARCHITECTURE.md`, `DECISIONS_AND_TRAPS.md`). Scan with `grep -A 1 "^### \[" docs/`.

## 4. Critical Trap Pointers (1-Line Warnings Only)
* **TRAP 1:** Never bind the HTTP server to `0.0.0.0` or skip `Host`/`Origin`/Bearer guards — see `docs/DECISIONS_AND_TRAPS.md` -> `[TRAP: Loopback-Binding-And-CSRF-Guard]`.
* **TRAP 2:** Never kill only the outer `cmd.exe` PID when stopping `.bat` servers; always use process-tree / Job Object termination and paired stop scripts — see `docs/DECISIONS_AND_TRAPS.md` -> `[TRAP: Bat-Child-Orphan-Process-Tree]`.
* **TRAP 3:** Never hardcode a single Antigravity model ID or leak tokens into `CLIProxyAPI` prompts — see `docs/DECISIONS_AND_TRAPS.md` -> `[TRAP: CLIProxyAPI-Dynamic-Model-And-Redaction]`.
* **TRAP 4:** Never crawl unbounded media folders (`D:\Images`, `D:\Comics`, `node_modules`, `.venv`) during script discovery — see `docs/DECISIONS_AND_TRAPS.md` -> `[TRAP: Unbounded-Media-And-VirtualDrive-Crawl]`.
* **TRAP 5:** Never write JSON/config files with a UTF-8 BOM — see `docs/DECISIONS_AND_TRAPS.md` -> `[TRAP: UTF8-No-BOM-Atomic-Writes]`.
* **TRAP 6:** Never assume PowerShell `ConvertTo-Json` emits an array for 1 `.lnk` or omit `computeDirSignature` caching — see `docs/DECISIONS_AND_TRAPS.md` -> `[TRAP: Startup-Lnk-PowerShell-Single-Array-And-Console-Flash]`.
