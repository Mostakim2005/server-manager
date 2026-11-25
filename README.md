# Server Manager (`server-manager`)

A single lightweight Go dashboard, system tray app, and `--json` CLI for discovering, explaining, starting, stopping, and monitoring local Web GUI servers and `.bat` / `.cmd` / `.ps1` launcher scripts across your machine.

## Key Features

* **Auto-Discovery + Smart Filtering:** Scans configured roots (`Agentic Harness`, `Services`, `CLIProxyAPI`, `E:\SillyTavern`, optional `D:\`) plus `ecosystem-registry.json`, pairing `start` and `stop` scripts together and hiding noisy build/CI scripts unless toggled.
* **Options / Inspect with Script Viewer & AI Report:** Click **Options** on any service to read its `.bat`/`.cmd`/`.ps1` source (`Charlie 2`), view live port & `ecosystem-registry.json` conflict warnings (`Charlie 3`), and fetch a cached plain-English **AI Report** via local `CLIProxyAPI` (`127.0.0.1:8317`, auto-started if offline).
* **AI-Assisted Add Project:** Point **Add with AI** at any new project folder or script to let `CLIProxyAPI` detect the start command, stop command, port, URL, and plain-English explanation before saving to your catalog.
* **Live Process Supervision & Logs:** Start, Stop, Restart, Open Web UI, Copy Command, Open Folder, and watch live scrolling `stdout`/`stderr` console logs (`Charlie 1`) plus one-click **Stack Presets** (`Charlie 4`).
* **Windows Startup & Task Manager Sync:** Toggle silent Windows Startup on or off for any service, custom project, or Server Manager itself (`--tray` / `--launch-service` / `*-hidden.vbs`), filter by `Startup`, and detect shortcuts disabled in Windows Task Manager (`StartupApproved`).
* **Desktop Shortcut, Custom SVG/ICO & `--json` CLI:** Launches silently via `ServerManager.exe` (`-H=windowsgui`) with a custom geometric SVG/ICO icon, Windows System Tray, Desktop shortcut, and headless `server-manager --json` CLI (`Charlie 5`).

## Quick Start

```powershell
# Start or focus the Web GUI (also bound to Desktop shortcut "Server Manager")
ServerManager.exe

# Or drive headlessly via CLI
server-manager list --json
server-manager startup list --json
server-manager startup cli-proxy-api-run-cliproxy on --json
server-manager start database-manager-start-server --json
server-manager ai-report av-manager-run --json
```

## Documentation

* [`docs/PRODUCT.md`](docs/PRODUCT.md) — User vision, active feature checklist, and wishlist
* [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — System architecture, visual Mermaid flowcharts, and extension recipes
* [`docs/DECISIONS_AND_TRAPS.md`](docs/DECISIONS_AND_TRAPS.md) — Locked design decisions and engineering traps
