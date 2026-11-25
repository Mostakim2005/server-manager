package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Mostakim2005/server-manager/internal/aiproxy"
	"github.com/Mostakim2005/server-manager/internal/config"
	"github.com/Mostakim2005/server-manager/internal/discovery"
	"github.com/Mostakim2005/server-manager/internal/startup"
	"github.com/Mostakim2005/server-manager/internal/supervisor"
	"github.com/Mostakim2005/server-manager/internal/tray"
)

// LaunchService starts a discovered or custom service silently and returns immediately (used by Windows Startup shortcuts).
func LaunchService(id string, stderr io.Writer) int {
	id = strings.TrimSpace(id)
	if id == "" {
		return 2
	}
	paths := config.DiscoverPaths("")
	cfg, _ := config.LoadOrInitConfig(paths)
	reg, _ := config.LoadRegistry(paths.RegistryPrimaryPath, paths.RegistryMirrorPath)
	entries := discovery.Scan(paths, cfg, reg)
	var match *discovery.ServiceEntry
	for i := range entries {
		if entries[i].ID == id {
			match = &entries[i]
			break
		}
	}
	if match == nil {
		if stderr != nil {
			fmt.Fprintf(stderr, "service %q not found\n", id)
		}
		return 1
	}
	safeID := startup.SanitizeShortcutBase(id)
	if safeID == "" {
		safeID = "service"
	}
	logPath := filepath.Join(paths.LogsDir, "startup-"+safeID+".log")
	if _, err := supervisor.LaunchDetached(*match, logPath); err != nil {
		if stderr != nil {
			fmt.Fprintf(stderr, "launch-service %s failed: %v\n", id, err)
		}
		return 1
	}
	return 0
}

// Run executes the Server Manager headless CLI (supporting --json per ecosystem contract).
func Run(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		printHelp(stdout)
		return 0
	}

	verb := args[0]
	rest, trailingJSON, trailingAll := splitKnownTrailingFlags(args[1:])

	fs := flag.NewFlagSet(verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "Emit machine-readable JSON output")
	includeAll := fs.Bool("all", false, "Include utility/build scripts in list/status")
	refreshAI := fs.Bool("refresh", false, "Bypass cache when calling ai-report")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	*asJSON = *asJSON || trailingJSON
	*includeAll = *includeAll || trailingAll
	pos := fs.Args()

	paths := config.DiscoverPaths("")
	cfg, _ := config.LoadOrInitConfig(paths)
	reg, _ := config.LoadRegistry(paths.RegistryPrimaryPath, paths.RegistryMirrorPath)
	sec, _ := config.LoadOrInitSecrets(paths.SecretsPath)
	baseURL := fmt.Sprintf("http://%s:%d", config.DefaultBindHost, config.DefaultPort)

	switch verb {
	case "health":
		online := daemonOnline(baseURL)
		out := map[string]any{"service": "server-manager", "url": baseURL, "daemonOnline": online}
		return emit(stdout, *asJSON, out, func() string {
			return fmt.Sprintf("Server Manager (%s) daemonOnline=%v", baseURL, online)
		})

	case "list", "status":
		entries := discovery.Scan(paths, cfg, reg)
		if !*includeAll {
			filtered := entries[:0]
			for _, e := range entries {
				if !e.IsUtility {
					filtered = append(filtered, e)
				}
			}
			entries = filtered
		}
		sup := supervisor.NewManager(nil)
		rt := sup.StatusMap(entries)
		if *asJSON {
			return emitJSON(stdout, map[string]any{"services": entries, "runtime": rt})
		}
		for _, e := range entries {
			st := rt[e.ID]
			fmt.Fprintf(stdout, "%-36s %-16s ports=%v\n", e.ID, st.State, e.Ports)
		}
		return 0

	case "conflicts":
		entries := discovery.Scan(paths, cfg, reg)
		conflicts := discovery.DetectConflicts(entries, reg)
		if conflicts == nil {
			conflicts = []discovery.ConflictWarning{}
		}
		if *asJSON {
			return emitJSON(stdout, map[string]any{"conflicts": conflicts})
		}
		if len(conflicts) == 0 {
			fmt.Fprintln(stdout, "No port or registry conflicts detected.")
			return 0
		}
		for _, c := range conflicts {
			fmt.Fprintf(stdout, "[%s] %s\n", c.Severity, c.Message)
		}
		return 0

	case "sync-registry":
		repoDir, _ := os.Getwd()
		if _, err := os.Stat(filepath.Join(paths.AgenticHarnessDir, "Server Manager")); err == nil {
			repoDir = filepath.Join(paths.AgenticHarnessDir, "Server Manager")
		}
		guiExe := filepath.Join(paths.LocalBinDir, "ServerManager.exe")
		cliExe := filepath.Join(paths.LocalBinDir, "server-manager.exe")
		if err := config.SyncEcosystemRegistry(paths, config.DefaultPort, repoDir, guiExe, cliExe); err != nil {
			fmt.Fprintf(stderr, "sync-registry failed: %v\n", err)
			return 1
		}
		return emit(stdout, *asJSON, map[string]any{
			"synced":   true,
			"registry": paths.RegistryPrimaryPath,
			"port":     config.DefaultPort,
		}, func() string {
			return "Synced ecosystem-registry.json"
		})

	case "start", "stop":
		if len(pos) == 0 {
			fmt.Fprintf(stderr, "missing service id\n")
			return 2
		}
		id := pos[0]
		if daemonOnline(baseURL) && sec != nil && sec.Tokens["serverManager"] != "" {
			respBytes, code, err := callDaemonAPI(http.MethodPost, fmt.Sprintf("%s/api/v1/services/%s/%s", baseURL, id, verb), sec.Tokens["serverManager"], nil)
			if err == nil && code >= 200 && code < 300 {
				if *asJSON {
					_, _ = stdout.Write(respBytes)
					return 0
				}
				fmt.Fprintf(stdout, "%s %s succeeded via running daemon.\n", verb, id)
				return 0
			}
		}
		entries := discovery.Scan(paths, cfg, reg)
		var match *discovery.ServiceEntry
		for i := range entries {
			if entries[i].ID == id {
				match = &entries[i]
				break
			}
		}
		if match == nil {
			fmt.Fprintf(stderr, "service %q not found\n", id)
			return 1
		}
		sup := supervisor.NewManager(nil)
		var st supervisor.RuntimeStatus
		var err error
		if verb == "start" {
			st, err = sup.Start(*match)
		} else {
			st, err = sup.Stop(context.Background(), *match)
		}
		if err != nil {
			fmt.Fprintf(stderr, "%v\n", err)
			return 1
		}
		return emit(stdout, *asJSON, map[string]any{"status": st}, func() string {
			return fmt.Sprintf("%s: %s (state=%s)", verb, id, st.State)
		})

	case "ai-report":
		if len(pos) == 0 {
			fmt.Fprintf(stderr, "missing service id\n")
			return 2
		}
		id := pos[0]
		entries := discovery.Scan(paths, cfg, reg)
		var match *discovery.ServiceEntry
		for i := range entries {
			if entries[i].ID == id {
				match = &entries[i]
				break
			}
		}
		if match == nil {
			fmt.Fprintf(stderr, "service %q not found\n", id)
			return 1
		}
		cache, _ := config.LoadOrInitAICache(paths)
		if !*refreshAI {
			if rep, ok := cache.Reports[id]; ok && rep.ScriptHash == match.ScriptHash {
				return emit(stdout, *asJSON, map[string]any{"cached": true, "report": rep}, func() string {
					return rep.Summary
				})
			}
		}
		client := aiproxy.NewClient(cfg.CLIProxyURL, paths.CLIProxyDir)
		rep, _, err := client.GenerateReport(context.Background(), *match, cfg.PreferredAIModel, cfg.AutoStartCLIProxy)
		if err != nil {
			fmt.Fprintf(stderr, "ai-report failed: %v\n", err)
			return 3
		}
		cache.Reports[id] = rep
		_ = config.SaveAICache(paths, cache)
		return emit(stdout, *asJSON, map[string]any{"cached": false, "report": rep}, func() string {
			return rep.Summary
		})

	case "install-shortcut":
		exePath, _ := os.Executable()
		if gui := filepath.Join(paths.LocalBinDir, "ServerManager.exe"); fileExists(gui) {
			exePath = gui
		}
		lnk, err := tray.InstallDesktopShortcut(paths.DesktopDir, paths.IconICOPath, exePath, paths.DataDir)
		if err != nil {
			fmt.Fprintf(stderr, "install-shortcut failed: %v\n", err)
			return 1
		}
		var devLnk string
		if repoDir := config.ResolveRepoDir(paths, reg); repoDir != "" {
			devLnk, _ = tray.InstallDevDesktopShortcut(paths.DesktopDir, paths.IconICOPath, repoDir)
		}
		return emit(stdout, *asJSON, map[string]any{"shortcut": lnk, "devShortcut": devLnk, "icon": paths.IconICOPath}, func() string {
			if devLnk != "" {
				return "Created shortcuts: " + lnk + " & " + devLnk
			}
			return "Created shortcut: " + lnk
		})

	case "startup":
		return runStartupCLI(pos, *includeAll, *asJSON, paths, cfg, reg, stdout, stderr)

	case "quit":
		if !daemonOnline(baseURL) || sec == nil {
			return emit(stdout, *asJSON, map[string]any{"quit": false, "reason": "daemon offline"}, func() string {
				return "Server Manager daemon is not running."
			})
		}
		_, code, err := callDaemonAPI(http.MethodPost, baseURL+"/api/v1/shutdown", sec.Tokens["serverManager"], nil)
		if err != nil || code >= 300 {
			fmt.Fprintf(stderr, "quit failed: %v (HTTP %d)\n", err, code)
			return 1
		}
		return emit(stdout, *asJSON, map[string]any{"quit": true}, func() string {
			return "Server Manager daemon shutting down."
		})

	default:
		fmt.Fprintf(stderr, "unknown command %q\n", verb)
		printHelp(stderr)
		return 2
	}
}

func printHelp(w io.Writer) {
	fmt.Fprintln(w, "Server Manager CLI (server-manager)")
	fmt.Fprintln(w, "Usage: server-manager <command> [flags]")
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  health [--json]                 Check daemon reachability on 127.0.0.1:8790")
	fmt.Fprintln(w, "  list [--all] [--json]           List discovered & custom services with live port state")
	fmt.Fprintln(w, "  status [--all] [--json]         Alias for list")
	fmt.Fprintln(w, "  conflicts [--json]              Report port collisions and ecosystem-registry mismatches")
	fmt.Fprintln(w, "  start <id> [--json]             Start a service")
	fmt.Fprintln(w, "  stop <id> [--json]              Stop a running service / paired stop script")
	fmt.Fprintln(w, "  ai-report <id> [--refresh]      Generate or read cached plain-English AI report")
	fmt.Fprintln(w, "  install-shortcut [--json]       Generate ServerManager.ico and Desktop shortcut")
	fmt.Fprintln(w, "  startup list [--all] [--json]   Inspect Windows Startup status and Task Manager locks")
	fmt.Fprintln(w, "  startup <id|self> <on|off>      Enable or disable silent Windows Startup for a service or self")
	fmt.Fprintln(w, "  startup open-folder [--json]    Open the Windows Startup folder")
	fmt.Fprintln(w, "  sync-registry [--json]          Register Server Manager (:8790) in ecosystem-registry.json")
	fmt.Fprintln(w, "  quit [--json]                   Shut down running Server Manager tray daemon")
}

func runStartupCLI(pos []string, includeAll, asJSON bool, paths config.Paths, cfg config.AppConfig, reg *config.EcosystemRegistry, stdout, stderr io.Writer) int {
	mgr := startup.NewManager(paths)
	if len(pos) == 0 || pos[0] == "list" || pos[0] == "status" {
		entries := discovery.Scan(paths, cfg, reg)
		if !includeAll {
			filtered := entries[:0]
			for _, e := range entries {
				if !e.IsUtility {
					filtered = append(filtered, e)
				}
			}
			entries = filtered
		}
		svcMap, selfSt := mgr.Snapshot(entries)
		if asJSON {
			return emitJSON(stdout, map[string]any{
				"supported":  mgr.Supported(),
				"startupDir": paths.StartupDir,
				"self":       selfSt,
				"services":   svcMap,
			})
		}
		printStartupLine(stdout, startup.SelfServiceID, selfSt)
		for _, e := range entries {
			st := svcMap[e.ID]
			if includeAll || st.Enabled || st.TaskManagerDisabled {
				printStartupLine(stdout, e.ID, st)
			}
		}
		return 0
	}

	if pos[0] == "open-folder" {
		if err := mgr.OpenFolder(); err != nil {
			fmt.Fprintf(stderr, "startup open-folder failed: %v\n", err)
			return 1
		}
		return emit(stdout, asJSON, map[string]any{"opened": paths.StartupDir}, func() string {
			return "Opened Startup folder: " + paths.StartupDir
		})
	}

	if len(pos) < 2 {
		fmt.Fprintln(stderr, "usage: server-manager startup <id|self> <on|off> [--json]")
		return 2
	}
	targetID := pos[0]
	enable, ok := parseOnOff(pos[1])
	if !ok {
		fmt.Fprintf(stderr, "invalid startup action %q (expected on or off)\n", pos[1])
		return 2
	}

	var st startup.Status
	var err error
	if targetID == "self" || targetID == startup.SelfServiceID {
		st, err = mgr.SetSelfEnabled(enable)
	} else {
		entries := discovery.Scan(paths, cfg, reg)
		var match *discovery.ServiceEntry
		for i := range entries {
			if entries[i].ID == targetID {
				match = &entries[i]
				break
			}
		}
		if match == nil {
			fmt.Fprintf(stderr, "service %q not found\n", targetID)
			return 1
		}
		st, err = mgr.SetServiceEnabled(*match, enable)
	}
	if err != nil {
		fmt.Fprintf(stderr, "startup failed: %v\n", err)
		return 1
	}
	return emit(stdout, asJSON, map[string]any{"status": st}, func() string {
		stateWord := "disabled"
		if st.Enabled {
			stateWord = "enabled"
		}
		return fmt.Sprintf("Windows Startup %s for %s (%s)", stateWord, st.ServiceID, st.ShortcutName)
	})
}

func printStartupLine(w io.Writer, id string, st startup.Status) {
	state := "off"
	if st.TaskManagerDisabled {
		state = "disabled-in-task-mgr"
	} else if st.Enabled {
		state = "on"
	}
	fmt.Fprintf(w, "%-36s %-22s mode=%-22s shortcut=%s\n", id, state, st.LaunchMode, st.ShortcutName)
}

func parseOnOff(raw string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "on", "enable", "enabled", "true", "1":
		return true, true
	case "off", "disable", "disabled", "false", "0":
		return false, true
	default:
		return false, false
	}
}

func splitKnownTrailingFlags(args []string) ([]string, bool, bool) {
	kept := make([]string, 0, len(args))
	var hasJSON, hasAll bool
	for _, a := range args {
		switch a {
		case "--json", "-json":
			hasJSON = true
		case "--all", "-all":
			hasAll = true
		default:
			kept = append(kept, a)
		}
	}
	return kept, hasJSON, hasAll
}

func emit(w io.Writer, asJSON bool, payload any, textFn func() string) int {
	if asJSON {
		return emitJSON(w, payload)
	}
	fmt.Fprintln(w, textFn())
	return 0
}

func emitJSON(w io.Writer, v any) int {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return 1
	}
	return 0
}

func daemonOnline(baseURL string) bool {
	c := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := c.Get(baseURL + "/api/v1/health")
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func callDaemonAPI(method, url, token string, body io.Reader) ([]byte, int, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	c := &http.Client{Timeout: 15 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return b, resp.StatusCode, err
}

func fileExists(p string) bool {
	if strings.TrimSpace(p) == "" {
		return false
	}
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
