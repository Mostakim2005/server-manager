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
	"github.com/Mostakim2005/server-manager/internal/supervisor"
	"github.com/Mostakim2005/server-manager/internal/tray"
)

// Run executes the Server Manager headless CLI (supporting --json per ecosystem contract).
func Run(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		printHelp(stdout)
		return 0
	}

	verb := args[0]
	rest := args[1:]

	fs := flag.NewFlagSet(verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "Emit machine-readable JSON output")
	includeAll := fs.Bool("all", false, "Include utility/build scripts in list/status")
	refreshAI := fs.Bool("refresh", false, "Bypass cache when calling ai-report")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
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
		return emit(stdout, *asJSON, map[string]any{"shortcut": lnk, "icon": paths.IconICOPath}, func() string {
			return "Created shortcut: " + lnk
		})

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
	fmt.Fprintln(w, "  sync-registry [--json]          Register Server Manager (:8790) in ecosystem-registry.json")
	fmt.Fprintln(w, "  quit [--json]                   Shut down running Server Manager tray daemon")
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
