package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Mostakim2005/server-manager/internal/aiproxy"
	"github.com/Mostakim2005/server-manager/internal/branding"
	"github.com/Mostakim2005/server-manager/internal/config"
	"github.com/Mostakim2005/server-manager/internal/discovery"
	"github.com/Mostakim2005/server-manager/internal/icons"
	"github.com/Mostakim2005/server-manager/internal/security"
	"github.com/Mostakim2005/server-manager/internal/startup"
	"github.com/Mostakim2005/server-manager/internal/supervisor"
	"github.com/Mostakim2005/server-manager/internal/tray"
	"github.com/Mostakim2005/server-manager/internal/webui"
)

// Daemon coordinates discovery, process supervision, CLIProxyAPI AI calls, and loopback HTTP/SSE.
type Daemon struct {
	mu                sync.RWMutex
	Paths             config.Paths
	Config            config.AppConfig
	Registry          *config.EcosystemRegistry
	Secrets           *config.EcosystemSecrets
	AICache           config.AICache
	Supervisor        *supervisor.Manager
	StartupManager    *startup.Manager
	Icons             *icons.Manager
	AI                *aiproxy.Client
	Services          []discovery.ServiceEntry
	Conflicts         []discovery.ConflictWarning
	UIToken           string
	Port              int
	LiveWebUIDir      string
	OnShutdownRequest func()
	srv               *http.Server
	ln                net.Listener
	watchCancel       context.CancelFunc
}

// NewDaemon initializes Server Manager components safely.
func NewDaemon(paths config.Paths) (*Daemon, error) {
	cfg, err := config.LoadOrInitConfig(paths)
	if err != nil {
		return nil, err
	}
	reg, _ := config.LoadRegistry(paths.RegistryPrimaryPath, paths.RegistryMirrorPath)
	sec, err := config.LoadOrInitSecrets(paths.SecretsPath)
	if err != nil {
		return nil, err
	}
	aiCache, _ := config.LoadOrInitAICache(paths)
	tok, err := security.GenerateToken()
	if err != nil {
		return nil, err
	}
	jobSup, _ := security.NewJobSupervisor()
	sup := supervisor.NewManager(jobSup)
	startupMgr := startup.NewManager(paths)
	iconMgr := icons.NewManager(paths.IconsCacheDir)
	aiClient := aiproxy.NewClient(cfg.CLIProxyURL, paths.CLIProxyDir)

	d := &Daemon{
		Paths:          paths,
		Config:         cfg,
		Registry:       reg,
		Secrets:        sec,
		AICache:        aiCache,
		Supervisor:     sup,
		StartupManager: startupMgr,
		Icons:          iconMgr,
		AI:             aiClient,
		UIToken:        tok,
		Port:           config.DefaultPort,
	}
	d.Rescan()
	return d, nil
}

// Rescan reloads ecosystem-registry.json and rescans configured folders.
func (d *Daemon) Rescan() []discovery.ServiceEntry {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Registry, _ = config.LoadRegistry(d.Paths.RegistryPrimaryPath, d.Paths.RegistryMirrorPath)
	services := discovery.Scan(d.Paths, d.Config, d.Registry)
	if d.Icons != nil {
		resolved := d.Icons.Refresh(services, d.Config, d.Registry)
		for i := range services {
			if ic, ok := resolved[services[i].ID]; ok {
				services[i].IconURL = fmt.Sprintf("/service-icons/%s?v=%s", services[i].ID, ic.Version)
				services[i].IconSource = ic.Source
			}
		}
	}
	d.Services = services
	d.Conflicts = discovery.DetectConflicts(d.Services, d.Registry)
	d.LiveWebUIDir = resolveLiveWebUIDir(d.Paths, d.Registry)
	return append([]discovery.ServiceEntry(nil), d.Services...)
}

func (d *Daemon) findService(id string) (discovery.ServiceEntry, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, s := range d.Services {
		if s.ID == id {
			return s, true
		}
	}
	return discovery.ServiceEntry{}, false
}

// Start binds strictly to 127.0.0.1 and begins serving the HTTP + SSE API and Web UI.
func (d *Daemon) Start(bindAddr string) (string, error) {
	if !security.IsLoopbackBind(bindAddr) {
		return "", fmt.Errorf("refusing non-loopback bind %q", bindAddr)
	}
	ln, err := net.Listen("tcp", bindAddr)
	if err != nil {
		return "", err
	}
	if tcp, ok := ln.Addr().(*net.TCPAddr); ok {
		d.Port = tcp.Port
	}
	d.ln = ln
	d.srv = &http.Server{
		Handler:           d.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	d.mu.RLock()
	liveDir := d.LiveWebUIDir
	d.mu.RUnlock()
	if liveDir != "" {
		wCtx, cancel := context.WithCancel(context.Background())
		d.mu.Lock()
		if d.watchCancel != nil {
			d.watchCancel()
		}
		d.watchCancel = cancel
		d.mu.Unlock()
		go d.watchLiveWebUIDir(wCtx, liveDir)
	}
	go func() {
		_ = d.srv.Serve(ln)
	}()
	return "http://" + ln.Addr().String(), nil
}

// Shutdown gracefully stops HTTP listeners and managed child trees.
func (d *Daemon) Shutdown(ctx context.Context) error {
	d.mu.Lock()
	cancel := d.watchCancel
	d.watchCancel = nil
	d.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if d.srv != nil {
		_ = d.srv.Shutdown(ctx)
	}
	if d.Supervisor != nil {
		d.Supervisor.ShutdownAll()
	}
	return nil
}

// Handler returns the hardened HTTP router.
func (d *Daemon) Handler() http.Handler {
	mux := http.NewServeMux()
	uiFS := webui.FileServer()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !security.ValidateLoopbackHost(r.Host, d.Port) || !security.ValidateOrigin(r.Header.Get("Origin"), d.Port, false) {
			http.Error(w, "Forbidden loopback host/origin", http.StatusForbidden)
			return
		}
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			d.serveIndexWithSession(w)
			return
		}
		if r.URL.Path == "/favicon.svg" || r.URL.Path == "/logo.svg" {
			if liveDir := d.liveWebUIDir(); liveDir != "" {
				logoPath := filepath.Join(liveDir, "logo.svg")
				if raw, err := os.ReadFile(logoPath); err == nil && len(raw) > 0 {
					w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
					w.Header().Set("Cache-Control", "no-store")
					_, _ = w.Write(raw)
					return
				}
			}
			w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
			_, _ = w.Write(branding.SVG())
			return
		}
		if strings.HasPrefix(r.URL.Path, "/shared-libs/") {
			d.serveSharedLib(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/service-icons/") {
			d.serveServiceIcon(w, r)
			return
		}
		if liveDir := d.liveWebUIDir(); liveDir != "" {
			rel := strings.TrimPrefix(r.URL.Path, "/")
			if target, err := security.ResolveConfinedPath(liveDir, rel); err == nil && fileExists(target) {
				w.Header().Set("Cache-Control", "no-store")
				http.ServeFile(w, r, target)
				return
			}
		}
		uiFS.ServeHTTP(w, r)
	})

	mux.HandleFunc("/api/v1/health", d.withGuard(false, false, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "ok",
			"service": "server-manager",
			"port":    d.Port,
		})
	}))

	mux.HandleFunc("/api/v1/shutdown", d.withGuard(true, true, d.handleShutdown))
	mux.HandleFunc("/api/v1/state", d.withGuard(false, true, d.handleState))
	mux.HandleFunc("/api/v1/rescan", d.withGuard(false, true, d.handleRescan))
	mux.HandleFunc("/api/v1/events", d.withGuard(false, true, d.handleSSE))
	mux.HandleFunc("/api/v1/settings", d.withGuard(false, true, d.handleSettings))
	mux.HandleFunc("/api/v1/install-shortcut", d.withGuard(false, true, d.handleInstallShortcut))
	mux.HandleFunc("/api/v1/startup/self", d.withGuard(false, true, d.handleStartupSelf))
	mux.HandleFunc("/api/v1/startup/open-folder", d.withGuard(false, true, d.handleStartupOpenFolder))
	mux.HandleFunc("/api/v1/open-folder", d.withGuard(false, true, d.handleOpenFolder))
	mux.HandleFunc("/api/v1/ai/status", d.withGuard(false, true, d.handleAIStatus))
	mux.HandleFunc("/api/v1/ai/analyze-project", d.withGuard(false, true, d.handleAIAnalyzeProject))
	mux.HandleFunc("/api/v1/catalog/custom", d.withGuard(false, true, d.handleCustomCatalog))
	mux.HandleFunc("/api/v1/catalog/custom/", d.withGuard(false, true, d.handleCustomCatalogItem))
	mux.HandleFunc("/api/v1/stacks", d.withGuard(false, true, d.handleStacks))
	mux.HandleFunc("/api/v1/stacks/", d.withGuard(false, true, d.handleStackItem))
	mux.HandleFunc("/api/v1/services/", d.withGuard(false, true, d.handleServiceSubroute))
	mux.HandleFunc("/api/v1/groups/", d.withGuard(false, true, d.handleGroupSubroute))

	return mux
}

func (d *Daemon) withGuard(strictNoOrigin, requireAuth bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !security.ValidateLoopbackHost(r.Host, d.Port) {
			http.Error(w, "Invalid loopback Host header", http.StatusForbidden)
			return
		}
		if !security.ValidateOrigin(r.Header.Get("Origin"), d.Port, strictNoOrigin) {
			http.Error(w, "Cross-origin or disallowed Origin header blocked", http.StatusForbidden)
			return
		}
		if requireAuth {
			cand := security.ExtractBearerToken(r)
			if cand == "" {
				// Allow EventSource SSE query token scoped to localhost session when headers aren't supported.
				if strings.HasPrefix(r.URL.Path, "/api/v1/events") {
					cand = strings.TrimSpace(r.URL.Query().Get("token"))
				}
			}
			var persistent string
			if d.Secrets != nil {
				persistent = d.Secrets.Tokens["serverManager"]
			}
			if !security.ConstantTimeTokenMatch(cand, persistent, d.UIToken) {
				http.Error(w, "Unauthorized: Bearer token required", http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	}
}

func (d *Daemon) serveIndexWithSession(w http.ResponseWriter) {
	var raw []byte
	var err error
	if liveDir := d.liveWebUIDir(); liveDir != "" {
		raw, err = os.ReadFile(filepath.Join(liveDir, "index.html"))
	}
	if len(raw) == 0 || err != nil {
		sub, serr := webui.Assets()
		if serr != nil {
			http.Error(w, "Missing embedded UI", http.StatusInternalServerError)
			return
		}
		f, oerr := sub.Open("index.html")
		if oerr != nil {
			http.Error(w, "Missing index.html", http.StatusInternalServerError)
			return
		}
		raw, err = io.ReadAll(f)
		_ = f.Close()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	nonce, _ := security.GenerateToken()
	htmlStr := string(raw)
	metaTag := fmt.Sprintf(`<meta name="sm-session" content="%s">`, d.UIToken)
	if strings.Contains(htmlStr, "</head>") {
		htmlStr = strings.Replace(htmlStr, "<script data-sm-fouc>", fmt.Sprintf(`<script nonce="%s" data-sm-fouc>`, nonce), 1)
		htmlStr = strings.Replace(htmlStr, "</head>", "  "+metaTag+"\n</head>", 1)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", fmt.Sprintf("default-src 'self'; script-src 'self' 'nonce-%s'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none';", nonce))
	_, _ = io.WriteString(w, htmlStr)
}

func (d *Daemon) serveSharedLib(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/shared-libs/")
	target, err := security.ResolveConfinedPath(d.Paths.SharedLibrariesDir, rel)
	if err != nil {
		http.Error(w, "Forbidden shared asset path", http.StatusForbidden)
		return
	}
	http.ServeFile(w, r, target)
}

func (d *Daemon) serveServiceIcon(w http.ResponseWriter, r *http.Request) {
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		http.Error(w, "Cross-site icon access blocked", http.StatusForbidden)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/service-icons/")
	if id == "" || strings.Contains(id, "/") || d.Icons == nil {
		http.NotFound(w, r)
		return
	}
	ic, ok := d.Icons.Get(id)
	if !ok || len(ic.Data) == 0 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ic.ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(ic.Data)
}

func (d *Daemon) handleState(w http.ResponseWriter, r *http.Request) {
	d.mu.RLock()
	services := append([]discovery.ServiceEntry(nil), d.Services...)
	conflicts := append([]discovery.ConflictWarning(nil), d.Conflicts...)
	cfg := d.Config
	cache := d.AICache.Reports
	d.mu.RUnlock()

	runtimeMap := d.Supervisor.StatusMap(services)
	startupMap, selfStartup := d.StartupManager.Snapshot(services)
	aiStatus := d.AI.Probe(r.Context())

	if d.Icons != nil {
		for _, s := range services {
			rt := runtimeMap[s.ID]
			if (rt.State == "running" || rt.State == "running_external") && s.IconURL == "" && s.WebURL != "" {
				d.Icons.ProbeWebFaviconAsync(s, func(svcID string, ic icons.ResolvedIcon) {
					d.mu.Lock()
					for i := range d.Services {
						if d.Services[i].ID == svcID && d.Services[i].IconURL == "" {
							d.Services[i].IconURL = fmt.Sprintf("/service-icons/%s?v=%s", svcID, ic.Version)
							d.Services[i].IconSource = ic.Source
						}
					}
					d.mu.Unlock()
					d.Supervisor.EmitStatus(svcID)
				})
			}
		}
	}

	liveDir := d.liveWebUIDir()
	writeJSON(w, http.StatusOK, map[string]any{
		"services":      services,
		"runtime":       runtimeMap,
		"startup":       startupMap,
		"selfStartup":   selfStartup,
		"conflicts":     conflicts,
		"stacks":        cfg.Stacks,
		"config":        cfg,
		"aiReports":     cache,
		"aiProxyStatus": aiStatus,
		"hotReload":     liveDir != "",
		"webUiDir":      liveDir,
		"paths": map[string]string{
			"desktop": d.Paths.DesktopDir,
			"startup": d.Paths.StartupDir,
			"dataDir": d.Paths.DataDir,
		},
	})
}

func (d *Daemon) handleRescan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d.Rescan()
	d.handleState(w, r)
}

func (d *Daemon) handleServiceSubroute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/services/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	id, action := parts[0], parts[1]
	entry, ok := d.findService(id)
	if !ok {
		http.Error(w, "Service not found", http.StatusNotFound)
		return
	}

	switch action {
	case "start":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		st, err := d.Supervisor.Start(entry)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": st})
	case "stop":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		st, err := d.Supervisor.Stop(r.Context(), entry)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": st})
	case "restart":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		_, _ = d.Supervisor.Stop(r.Context(), entry)
		time.Sleep(350 * time.Millisecond)
		st, err := d.Supervisor.Start(entry)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": st})
	case "logs":
		writeJSON(w, http.StatusOK, map[string]any{
			"serviceId": id,
			"lines":     d.Supervisor.Logs(id),
		})
	case "ready":
		st := d.Supervisor.StatusFor(entry, nil)
		ready := len(st.ActivePorts) > 0 || (len(entry.Ports) == 0 && st.State == "running")
		writeJSON(w, http.StatusOK, map[string]any{
			"serviceId": id,
			"ready":     ready,
			"state":     st.State,
			"webUrl":    entry.WebURL,
			"lastError": st.LastError,
		})
	case "source":
		startText := readFileSafe(entry.ScriptPath, 64*1024)
		stopText := readFileSafe(entry.StopScriptPath, 32*1024)
		writeJSON(w, http.StatusOK, map[string]any{
			"serviceId":      id,
			"scriptPath":     entry.ScriptPath,
			"stopScriptPath": entry.StopScriptPath,
			"startCommand":   entry.StartCmd,
			"stopCommand":    entry.StopCmd,
			"startContent":   startText,
			"stopContent":    stopText,
		})
	case "note":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Note string `json:"note"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		d.mu.Lock()
		if d.Config.Notes == nil {
			d.Config.Notes = map[string]string{}
		}
		if strings.TrimSpace(body.Note) == "" {
			delete(d.Config.Notes, id)
		} else {
			d.Config.Notes[id] = strings.TrimSpace(body.Note)
		}
		_ = config.SaveConfig(d.Paths, d.Config)
		d.mu.Unlock()
		d.Rescan()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "ai-report":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Force bool `json:"force"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body)
		d.handleServiceAIReport(w, r, entry, body.Force)
	case "startup":
		if r.Method != http.MethodPost && r.Method != http.MethodPut {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		st, err := d.StartupManager.SetServiceEnabled(entry, body.Enabled)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "status": st})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": st})
	case "icon":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			IconPath string `json:"iconPath"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		trimmed := strings.TrimSpace(body.IconPath)
		if trimmed != "" && d.Icons != nil {
			if _, err := d.Icons.ApplyCustomOverride(r.Context(), entry, trimmed); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
		}
		d.mu.Lock()
		if d.Config.IconOverrides == nil {
			d.Config.IconOverrides = map[string]string{}
		}
		if trimmed == "" {
			delete(d.Config.IconOverrides, id)
			if entry.GroupKey != "" {
				delete(d.Config.IconOverrides, entry.GroupKey)
			}
		} else {
			d.Config.IconOverrides[id] = trimmed
			if entry.GroupKey != "" {
				d.Config.IconOverrides[entry.GroupKey] = trimmed
			}
		}
		for i := range d.Config.CustomServices {
			if d.Config.CustomServices[i].ID == id {
				d.Config.CustomServices[i].IconPath = trimmed
			}
		}
		_ = config.SaveConfig(d.Paths, d.Config)
		d.mu.Unlock()
		d.Rescan()
		updated, _ := d.findService(id)
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":         true,
			"iconPath":   trimmed,
			"iconUrl":    updated.IconURL,
			"iconSource": updated.IconSource,
		})
	case "grouping":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Action          string `json:"action"` // "split" | "rejoin" | "merge"
			TargetGroupKey  string `json:"targetGroupKey"`
			TargetServiceID string `json:"targetServiceId"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		d.mu.Lock()
		if d.Config.ManualGroups == nil {
			d.Config.ManualGroups = map[string]string{}
		}
		switch body.Action {
		case "split":
			d.Config.UngroupedIDs = addUniqueString(d.Config.UngroupedIDs, id)
			delete(d.Config.ManualGroups, id)
		case "rejoin":
			d.Config.UngroupedIDs = removeString(d.Config.UngroupedIDs, id)
			delete(d.Config.ManualGroups, id)
		case "merge":
			targetKey := strings.TrimSpace(body.TargetGroupKey)
			targetSvcID := strings.TrimSpace(body.TargetServiceID)
			if targetKey == "" && targetSvcID != "" {
				for _, svc := range d.Services {
					if svc.ID == targetSvcID {
						targetKey = svc.GroupKey
						if targetKey == "" {
							targetKey = "custom-grp-" + sanitizeGroupSlug(svc.ID)
							d.Config.ManualGroups[svc.ID] = targetKey
						}
						break
					}
				}
			}
			if targetKey == "" {
				d.mu.Unlock()
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing target group or service"})
				return
			}
			d.Config.UngroupedIDs = removeString(d.Config.UngroupedIDs, id)
			if targetSvcID != "" {
				d.Config.UngroupedIDs = removeString(d.Config.UngroupedIDs, targetSvcID)
			}
			d.Config.ManualGroups[id] = targetKey
		default:
			d.mu.Unlock()
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid grouping action"})
			return
		}
		_ = config.SaveConfig(d.Paths, d.Config)
		d.mu.Unlock()
		d.Rescan()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		http.NotFound(w, r)
	}
}

func (d *Daemon) handleGroupSubroute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/groups/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] != "preferred" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	groupKey := parts[0]
	var body struct {
		ServiceID string `json:"serviceId"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	d.mu.Lock()
	if d.Config.PreferredScripts == nil {
		d.Config.PreferredScripts = map[string]string{}
	}
	if strings.TrimSpace(body.ServiceID) == "" {
		delete(d.Config.PreferredScripts, groupKey)
	} else {
		d.Config.PreferredScripts[groupKey] = strings.TrimSpace(body.ServiceID)
	}
	err := config.SaveConfig(d.Paths, d.Config)
	d.mu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "groupKey": groupKey, "serviceId": body.ServiceID})
}

func (d *Daemon) handleServiceAIReport(w http.ResponseWriter, r *http.Request, entry discovery.ServiceEntry, force bool) {
	d.mu.RLock()
	cached, hasCached := d.AICache.Reports[entry.ID]
	cfg := d.Config
	d.mu.RUnlock()

	if !force && hasCached && cached.ScriptHash == entry.ScriptHash && cached.Summary != "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"cached":        true,
			"report":        cached,
			"aiProxyStatus": d.AI.Probe(r.Context()),
		})
		return
	}

	report, proxyStatus, err := d.AI.GenerateReport(r.Context(), entry, cfg.PreferredAIModel, cfg.AutoStartCLIProxy)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error":         err.Error(),
			"aiProxyStatus": proxyStatus,
			"cachedReport":  cached,
		})
		return
	}

	d.mu.Lock()
	if d.AICache.Reports == nil {
		d.AICache.Reports = map[string]config.AIReport{}
	}
	d.AICache.Reports[entry.ID] = report
	_ = config.SaveAICache(d.Paths, d.AICache)
	d.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"cached":        false,
		"report":        report,
		"aiProxyStatus": proxyStatus,
	})
}

func (d *Daemon) handleAIStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		d.mu.RLock()
		autoStart := d.Config.AutoStartCLIProxy
		d.mu.RUnlock()
		st, err := d.AI.EnsureRunning(r.Context(), autoStart)
		code := http.StatusOK
		errMsg := ""
		if err != nil {
			code = http.StatusBadGateway
			errMsg = err.Error()
		}
		writeJSON(w, code, map[string]any{"status": st, "error": errMsg})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": d.AI.Probe(r.Context())})
}

func (d *Daemon) handleAIAnalyzeProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	d.mu.RLock()
	cfg := d.Config
	d.mu.RUnlock()
	prop, st, err := d.AI.AnalyzeProjectFolder(r.Context(), body.Path, cfg.PreferredAIModel, cfg.AutoStartCLIProxy)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error":         err.Error(),
			"aiProxyStatus": st,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"proposal":      prop,
		"aiProxyStatus": st,
	})
}

func (d *Daemon) handleCustomCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var cs config.CustomService
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&cs); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(cs.Name) == "" || (strings.TrimSpace(cs.StartCmd) == "" && strings.TrimSpace(cs.ScriptPath) == "") {
		http.Error(w, "Name and startCmd/scriptPath are required", http.StatusBadRequest)
		return
	}
	cs.UpdatedAt = time.Now().Format(time.RFC3339)
	d.mu.Lock()
	if cs.ID == "" {
		cs.ID = fmt.Sprintf("custom-%d", time.Now().UnixMilli())
	}
	replaced := false
	for i := range d.Config.CustomServices {
		if d.Config.CustomServices[i].ID == cs.ID {
			d.Config.CustomServices[i] = cs
			replaced = true
			break
		}
	}
	if !replaced {
		d.Config.CustomServices = append(d.Config.CustomServices, cs)
	}
	err := config.SaveConfig(d.Paths, d.Config)
	d.mu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.Rescan()
	writeJSON(w, http.StatusOK, map[string]any{"saved": cs})
}

func (d *Daemon) handleCustomCatalogItem(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/catalog/custom/")
	if id == "" || r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d.mu.Lock()
	var removed *config.CustomService
	filtered := d.Config.CustomServices[:0]
	for _, cs := range d.Config.CustomServices {
		if cs.ID == id {
			cp := cs
			removed = &cp
			continue
		}
		filtered = append(filtered, cs)
	}
	d.Config.CustomServices = filtered
	_ = config.SaveConfig(d.Paths, d.Config)
	d.mu.Unlock()
	if removed != nil && d.StartupManager != nil && d.StartupManager.Supported() {
		_, _ = d.StartupManager.SetServiceEnabled(discovery.ServiceEntry{
			ID:         removed.ID,
			Name:       removed.Name,
			ScriptPath: removed.ScriptPath,
			WorkingDir: removed.WorkingDir,
			StartCmd:   removed.StartCmd,
		}, false)
	}
	d.Rescan()
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

func (d *Daemon) handleStacks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var st config.StackPreset
	if err := json.NewDecoder(io.LimitReader(r.Body, 32*1024)).Decode(&st); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(st.Name) == "" {
		http.Error(w, "Stack name required", http.StatusBadRequest)
		return
	}
	cleanIDs := make([]string, 0, len(st.ServiceIDs))
	seenIDs := make(map[string]bool, len(st.ServiceIDs))
	for _, sid := range st.ServiceIDs {
		sid = strings.TrimSpace(sid)
		if sid != "" && !seenIDs[sid] {
			seenIDs[sid] = true
			cleanIDs = append(cleanIDs, sid)
		}
	}
	st.ServiceIDs = cleanIDs
	d.mu.Lock()
	if st.ID == "" {
		st.ID = fmt.Sprintf("stack-%d", time.Now().UnixMilli())
	}
	updated := false
	for i := range d.Config.Stacks {
		if d.Config.Stacks[i].ID == st.ID {
			d.Config.Stacks[i] = st
			updated = true
			break
		}
	}
	if !updated {
		d.Config.Stacks = append(d.Config.Stacks, st)
	}
	_ = config.SaveConfig(d.Paths, d.Config)
	d.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"stack": st})
}

func (d *Daemon) handleStackItem(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/stacks/")
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if len(parts) == 1 && r.Method == http.MethodDelete {
		d.mu.Lock()
		next := d.Config.Stacks[:0]
		for _, s := range d.Config.Stacks {
			if s.ID != id {
				next = append(next, s)
			}
		}
		d.Config.Stacks = next
		_ = config.SaveConfig(d.Paths, d.Config)
		d.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
		return
	}
	if len(parts) == 2 && parts[1] == "action" && r.Method == http.MethodPost {
		var body struct {
			Action string `json:"action"` // "start" | "stop"
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		d.mu.RLock()
		var target *config.StackPreset
		for i := range d.Config.Stacks {
			if d.Config.Stacks[i].ID == id {
				cp := d.Config.Stacks[i]
				target = &cp
				break
			}
		}
		d.mu.RUnlock()
		if target == nil {
			http.Error(w, "Stack not found", http.StatusNotFound)
			return
		}
		results := make([]map[string]any, 0, len(target.ServiceIDs))
		ids := append([]string(nil), target.ServiceIDs...)
		if body.Action == "stop" {
			for i, j := 0, len(ids)-1; i < j; i, j = i+1, j-1 {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
		for _, sid := range ids {
			entry, ok := d.findService(sid)
			if !ok {
				results = append(results, map[string]any{"serviceId": sid, "error": "not found in catalog"})
				continue
			}
			var err error
			if body.Action == "stop" {
				_, err = d.Supervisor.Stop(r.Context(), entry)
			} else {
				_, err = d.Supervisor.Start(entry)
			}
			res := map[string]any{"serviceId": sid, "ok": err == nil}
			if err != nil {
				res["error"] = err.Error()
			}
			results = append(results, res)
		}
		writeJSON(w, http.StatusOK, map[string]any{"results": results})
		return
	}
	http.NotFound(w, r)
}

func (d *Daemon) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var next config.AppConfig
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&next); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	d.mu.Lock()
	// Preserve existing notes/custom services/stacks if omitted in partial settings form.
	if next.CustomServices == nil {
		next.CustomServices = d.Config.CustomServices
	}
	if next.Stacks == nil {
		next.Stacks = d.Config.Stacks
	}
	if next.Notes == nil {
		next.Notes = d.Config.Notes
	}
	if next.IconOverrides == nil {
		next.IconOverrides = d.Config.IconOverrides
	}
	if next.PreferredScripts == nil {
		next.PreferredScripts = d.Config.PreferredScripts
	}
	if next.UngroupedIDs == nil {
		next.UngroupedIDs = d.Config.UngroupedIDs
	}
	if next.ManualGroups == nil {
		next.ManualGroups = d.Config.ManualGroups
	}
	if next.ScanRoots == nil {
		next.ScanRoots = d.Config.ScanRoots
	}
	if next.CLIProxyURL == "" {
		next.CLIProxyURL = config.DefaultCLIProxyURL
	}
	d.Config = next
	d.AI = aiproxy.NewClient(next.CLIProxyURL, d.Paths.CLIProxyDir)
	err := config.SaveConfig(d.Paths, next)
	d.mu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.Rescan()
	writeJSON(w, http.StatusOK, map[string]any{"config": next})
}

func (d *Daemon) handleInstallShortcut(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	exePath, _ := os.Executable()
	if gui := filepath.Join(d.Paths.LocalBinDir, "ServerManager.exe"); fileExists(gui) {
		exePath = gui
	}
	lnk, err := tray.InstallDesktopShortcut(d.Paths.DesktopDir, d.Paths.IconICOPath, exePath, d.Paths.DataDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	var devLnk string
	d.mu.RLock()
	reg := d.Registry
	d.mu.RUnlock()
	if repoDir := config.ResolveRepoDir(d.Paths, reg); repoDir != "" {
		devLnk, _ = tray.InstallDevDesktopShortcut(d.Paths.DesktopDir, d.Paths.IconICOPath, repoDir)
	}
	writeJSON(w, http.StatusOK, map[string]any{"shortcut": lnk, "devShortcut": devLnk})
}

func (d *Daemon) handleStartupSelf(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	st, err := d.StartupManager.SetSelfEnabled(body.Enabled)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "status": st})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": st})
}

func (d *Daemon) handleStartupOpenFolder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := d.StartupManager.OpenFolder(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"opened": d.Paths.StartupDir})
}

func (d *Daemon) handleOpenFolder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ServiceID string `json:"serviceId"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	entry, ok := d.findService(body.ServiceID)
	if !ok || strings.TrimSpace(entry.WorkingDir) == "" {
		http.Error(w, "Folder not found", http.StatusNotFound)
		return
	}
	if runtime.GOOS == "windows" {
		_ = exec.Command("explorer.exe", entry.WorkingDir).Start()
	}
	writeJSON(w, http.StatusOK, map[string]any{"opened": entry.WorkingDir})
}

func (d *Daemon) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, unsub := d.Supervisor.Subscribe()
	defer unsub()

	_, _ = io.WriteString(w, "event: ready\ndata: {\"ok\":true}\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = io.WriteString(w, ": ping\n\n")
			flusher.Flush()
		case ev, open := <-ch:
			if !open {
				return
			}
			b, _ := json.Marshal(ev)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, string(b))
			flusher.Flush()
		}
	}
}

func (d *Daemon) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"shuttingDown": true})
	if d.OnShutdownRequest != nil {
		go func() {
			time.Sleep(150 * time.Millisecond)
			d.OnShutdownRequest()
		}()
	}
}

// RunForegroundDaemon starts Server Manager on addr (or opens existing running instance).
func RunForegroundDaemon(addr string, openUI bool, withTray bool) int {
	// Single-instance handoff: if already listening, open UI cleanly and exit.
	probeClient := &http.Client{Timeout: 600 * time.Millisecond}
	if resp, err := probeClient.Get("http://" + addr + "/api/v1/health"); err == nil {
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			if openUI {
				_ = tray.OpenDashboard("http://" + addr)
			}
			return 0
		}
	}

	paths := config.DiscoverPaths("")
	d, err := NewDaemon(paths)
	if err != nil {
		return 1
	}

	url, err := d.Start(addr)
	if err != nil {
		return 1
	}

	stopCh := make(chan struct{}, 1)
	triggerStop := func() {
		select {
		case stopCh <- struct{}{}:
		default:
		}
	}
	d.OnShutdownRequest = triggerStop

	var trayCtrl *tray.Controller
	if withTray {
		trayCtrl = tray.Start(tray.Callbacks{
			DashboardURL: url,
			OnInstallDesktop: func() {
				exePath, _ := os.Executable()
				_, _ = tray.InstallDesktopShortcut(paths.DesktopDir, paths.IconICOPath, exePath, paths.DataDir)
				d.mu.RLock()
				reg := d.Registry
				d.mu.RUnlock()
				if repoDir := config.ResolveRepoDir(paths, reg); repoDir != "" {
					_, _ = tray.InstallDevDesktopShortcut(paths.DesktopDir, paths.IconICOPath, repoDir)
				}
			},
			IsStartupEnabled: func() bool {
				return d.StartupManager.SelfStatus().Enabled
			},
			OnToggleStartup: func(enable bool) {
				_, _ = d.StartupManager.SetSelfEnabled(enable)
			},
			OnQuit: triggerStop,
		})
	}

	if openUI {
		_ = tray.OpenDashboard(url)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case <-stopCh:
	case <-sigCh:
	}

	if trayCtrl != nil {
		trayCtrl.Stop()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = d.Shutdown(ctx)
	cancel()
	return 0
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func readFileSafe(path string, maxBytes int64) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	b, _ := io.ReadAll(io.LimitReader(f, maxBytes))
	return string(b)
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func addUniqueString(items []string, val string) []string {
	for _, it := range items {
		if it == val {
			return items
		}
	}
	return append(items, val)
}

func removeString(items []string, val string) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		if it != val {
			out = append(out, it)
		}
	}
	return out
}

func sanitizeGroupSlug(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func (d *Daemon) liveWebUIDir() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.LiveWebUIDir
}

func resolveLiveWebUIDir(paths config.Paths, reg *config.EcosystemRegistry) string {
	if env := strings.TrimSpace(os.Getenv("SERVER_MANAGER_WEBUI_DIR")); env != "" {
		if strings.EqualFold(env, "embed") || strings.EqualFold(env, "off") {
			return ""
		}
		if fileExists(filepath.Join(env, "index.html")) {
			return filepath.Clean(env)
		}
		return ""
	}
	if repo := config.ResolveRepoDir(paths, reg); repo != "" {
		cand := filepath.Join(repo, "internal", "webui", "static")
		if fileExists(filepath.Join(cand, "index.html")) {
			return cand
		}
	}
	return ""
}

func (d *Daemon) watchLiveWebUIDir(ctx context.Context, dir string) {
	prev := liveDirSignature(dir)
	ticker := time.NewTicker(450 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cur := liveDirSignature(dir)
			if cur != "" && cur != prev {
				prev = cur
				if d.Supervisor != nil {
					d.Supervisor.EmitUIReload()
				}
			}
		}
	}
}

func liveDirSignature(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, de := range entries {
		if de.IsDir() {
			continue
		}
		info, ierr := de.Info()
		if ierr != nil {
			continue
		}
		fmt.Fprintf(&b, "%s:%d:%d|", de.Name(), info.Size(), info.ModTime().UnixNano())
	}
	return b.String()
}
