# Product Intent & Feature Map (`PRODUCT.md`)
> **1-Line Summary:** Permanent source of truth for `Server Manager`, a single Go Web GUI, Windows tray, and CLI hub for discovering, explaining, starting, stopping, and adding local project servers.

## 1. Core Vision & User Experience Intent
* **What This App Does (Plain English):** Automatically scans local project folders and `ecosystem-registry.json` for `.bat`, `.cmd`, and `.ps1` web GUI / service launchers, explains in plain English what each script and port does (both statically and via local `CLIProxyAPI` AI), detects port conflicts, and lets you start, stop, view logs, group stacks, and add future projects from one simple interface and Desktop shortcut.
* **How It Must Feel to Use:** Instant launch from a custom-icon Desktop shortcut (`< 20 MB` Go binary), crisp Light/Dark Material 3 tonal UI with WCAG AA contrast, zero cluttered build-script noise by default, and reliable process-tree Stop controls even for servers started outside the app.

## 2. Active Features (Do Not Break)

### [FEATURE: Multi-Root-Launcher-Discovery] | Aliases: scanner, bat, cmd, ps1, auto-discover, ecosystem registry
> **1-Line Summary:** Scans configured folders (plus `ecosystem-registry.json`) with bounded depth and directory exclusions, pairing `start`/`stop` scripts and hiding internal build/CI scripts unless toggled.
* **User Intention (Non-Negotiable):** Surface real web GUI and local service launchers immediately without drowning the user in `node_modules`, `gradlew.bat`, or `local-ci.ps1` files, and never hang crawling huge media trees.
* **Visual / UI Behavior:** Dashboard grid/list shows clean service cards with category chips, detected ports, live status (`Running`, `Running (External)`, `Stopped`), and a `Show Utility/Build Scripts` filter toggle.
* **Code Anchors (Grep Targets):**
  * `internal/discovery/scanner.go` -> `Scan`, `AnalyzeScript`, `DefaultRoots`

### [FEATURE: Process-Tree-Supervision-And-Live-Logs] | Aliases: start, stop, restart, job object, stdout, stderr, charlie 1
> **1-Line Summary:** Launches scripts cleanly with Job Object / process-tree tracking, pairs live TCP port probing for external servers, and streams live scrolling `stdout`/`stderr` logs via SSE.
* **User Intention (Non-Negotiable):** Stopping a `.bat` server must reliably terminate the entire spawned tree (such as `cmd.exe -> python.exe / uvicorn / node.exe`) without leaving zombie port listeners, and live output must be visible without opening separate terminal windows.
* **Visual / UI Behavior:** Each card has `Start` / `Stop` / `Restart` / `Open UI` / `Logs` / `Options`; the live log drawer tails color-formatted lines in real time with `Copy Command` and `Open Folder` quick actions.
* **Code Anchors (Grep Targets):**
  * `internal/supervisor/manager.go` -> `StartService`, `StopService`, `StreamLogs`
  * `internal/security/job_windows.go` -> `JobSupervisor`

### [FEATURE: Options-Drawer-Script-Viewer-And-AI-Report] | Aliases: options, inspect, script viewer, cliproxyapi, ai report, charlie 2
> **1-Line Summary:** Clicking Options opens the read-only `.bat`/`.ps1` source viewer alongside an automatic (and cached) plain-English AI Report from local `CLIProxyAPI` (`127.0.0.1:8317`).
* **User Intention (Non-Negotiable):** Understand at a glance what any script runs, which ports/URLs it touches, what it depends on, and any risks before clicking Start—auto-starting `CLIProxyAPI` silently if `:8317` is offline.
* **Visual / UI Behavior:** Clicking `Options` opens a structured inspector drawer showing Overview, AI Report (auto-loaded with cached fallback + Refresh button), and line-numbered read-only Script Source tabs (start + paired stop script).
* **Code Anchors (Grep Targets):**
  * `internal/aiproxy/client.go` -> `GenerateReport`, `EnsureRunning`
  * `internal/webui/static/app.js` -> `openOptionsDrawer`

### [FEATURE: AI-Assisted-Project-Addition] | Aliases: add project, ai add, future projects, custom service
> **1-Line Summary:** Lets the user point to any new folder, script, or custom command so `CLIProxyAPI` inspects its metadata/scripts and pre-fills a clean, editable catalog entry.
* **User Intention (Non-Negotiable):** Adding future web GUIs or local scripts must take seconds without hand-writing JSON schemas or hunting down ports manually.
* **Visual / UI Behavior:** `+ Add Project` modal accepts a folder or script path (with optional `Analyze with AI` button), populates name, command, stop command, working directory, port, URL, and summary for one-click saving.
* **Code Anchors (Grep Targets):**
  * `internal/aiproxy/client.go` -> `AnalyzeProjectFolder`
  * `internal/server/server.go` -> `handleAIAnalyzeProject`, `handleCustomCatalog`

### [FEATURE: Port-Conflict-And-Registry-Audit] | Aliases: port conflict, duplicate port, ecosystem-registry, charlie 3
> **1-Line Summary:** Detects duplicate TCP ports across scripts and flags mismatches against `ECOSYSTEM_INTEGRATION_CONTRACT.md` / `ecosystem-registry.json` and `0.0.0.0` LAN exposure.
* **User Intention (Non-Negotiable):** Prevent confusing port collisions (e.g., two services trying to bind `:8000`) and keep ports aligned with the ecosystem registry.
* **Visual / UI Behavior:** Amber/red warning badges on conflicting cards plus a compact `Conflicts & Registry` summary banner/drawer.
* **Code Anchors (Grep Targets):**
  * `internal/discovery/conflicts.go` -> `DetectConflicts`

### [FEATURE: Stack-Presets-Tray-Desktop-Shortcut-And-CLI] | Aliases: stack presets, charlie 4, charlie 5, svg icon, desktop shortcut, tray
> **1-Line Summary:** Bundles one-click Stack Presets, custom SVG/ICO branding, Windows System Tray, Desktop shortcut, and a `--json` CLI registered in `ecosystem-registry.json`.
* **User Intention (Non-Negotiable):** Launch the manager cleanly from the Desktop in one click and start/stop grouped workflows (e.g., *DeepTutor + Paradee TTS*) or drive everything from scripts/agents via CLI.
* **Visual / UI Behavior:** Header bar includes Stack Preset quick pills, Light/Dark/System theme toggle, and tray background option; Desktop shortcut launches/focuses the GUI seamlessly.
* **Code Anchors (Grep Targets):**
  * `internal/branding/logo.go` -> `SVG`, `ICO`, `PNG`
  * `internal/tray/shortcut_windows.go` -> `InstallDesktopShortcut`
  * `internal/cli/cli.go` -> `Run`

## 3. Planned Features & App-Merge Wishlist

### [WISHLIST: Scheduled-Health-Checks-And-Autostart-Profiles] | Aliases: autostart, scheduled startup, wake on boot
> **1-Line Summary:** Optional per-service auto-start-with-Server-Manager toggle and periodic health alerts when a pinned background daemon unexpectedly exits.
* **User Intention:** Keep core background services like `CLIProxyAPI` or `RAG Manager` resilient without adding heavy Windows Task Scheduler clutter until needed.
* **Target Integration Point:** `internal/supervisor/manager.go` -> `Manager`
