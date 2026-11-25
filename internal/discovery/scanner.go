package discovery

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Mostakim2005/server-manager/internal/config"
)

// ConflictWarning describes a detected port collision or registry mismatch (Charlie 3).
type ConflictWarning struct {
	Severity string `json:"severity"` // "error" | "warning" | "info"
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// ServiceEntry represents a discovered or custom launcher/service card.
type ServiceEntry struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	ProjectName    string            `json:"projectName"`
	Category       string            `json:"category"`   // "server" | "desktop" | "utility" | "registry" | "custom"
	SourceType     string            `json:"sourceType"` // "bat" | "cmd" | "ps1" | "custom" | "registry"
	ScriptPath     string            `json:"scriptPath,omitempty"`
	StopScriptPath string            `json:"stopScriptPath,omitempty"`
	WorkingDir     string            `json:"workingDir"`
	StartCmd       string            `json:"startCmd"`
	StopCmd        string            `json:"stopCmd,omitempty"`
	Ports          []int             `json:"ports"`
	WebURL         string            `json:"webUrl,omitempty"`
	BindHost       string            `json:"bindHost,omitempty"`
	Runtime        string            `json:"runtime"`
	StaticSummary  string            `json:"staticSummary"`
	UserNote       string            `json:"userNote,omitempty"`
	ScriptHash     string            `json:"scriptHash"`
	IsUtility      bool              `json:"isUtility"`
	IsCustom       bool              `json:"isCustom"`
	AddedByAI      bool              `json:"addedByAi"`
	Tags           []string          `json:"tags,omitempty"`
	Conflicts      []ConflictWarning `json:"conflicts,omitempty"`
}

var skipDirNames = map[string]struct{}{
	"node_modules": {}, ".venv": {}, "venv": {}, ".git": {}, "dist": {}, "build": {},
	".next": {}, "target": {}, "__pycache__": {}, ".pytest_cache": {}, ".ruff_cache": {},
	".mypy_cache": {}, ".ci-logs": {}, ".ci-report": {}, "android": {}, "external": {},
	"backups": {}, "images": {}, "comics": {}, "steamlibrary": {}, "calibre": {},
	"books": {}, "nippon": {}, "blobs": {}, "cache": {}, "generated": {},
}

var (
	portFlagRegex   = regexp.MustCompile(`(?i)(?:--port|-p|localport|-port)\s*[=\s]?\s*(\d{2,5})`)
	urlPortRegex    = regexp.MustCompile(`https?://(?:127\.0\.0\.1|localhost|0\.0\.0\.0|%[A-Za-z0-9_]+%):(\d{2,5})`)
	colonPortRegex  = regexp.MustCompile(`(?m)(?:listen|host|port|findstr)\s*[:=]?\s*:(\d{3,5})\b`)
	httpURLRegex    = regexp.MustCompile(`https?://(?:127\.0\.0\.1|localhost):\d{2,5}[^\s"'<>]*`)
	titleEchoRegex  = regexp.MustCompile(`(?im)^\s*(?:title|echo)\s+(.+)$`)
	commandEchoSkip = regexp.MustCompile(`(?i)^(@?off|=+|\s*\.?\s*|starting\b.*|done\.?)$`)
)

// Scan discovers scripts across configured roots, merges registry services and custom entries,
// pairs stop scripts, and flags port/registry conflicts.
func Scan(paths config.Paths, cfg config.AppConfig, reg *config.EcosystemRegistry) []ServiceEntry {
	roots := append([]config.ScanRoot(nil), cfg.ScanRoots...)
	if cfg.IncludeVirtualDriveD {
		if _, err := os.Stat(`D:\`); err == nil {
			roots = append(roots, config.ScanRoot{
				ID:       "virtual-drive-d",
				Label:    "Cryptomator D:",
				Path:     `D:\`,
				Enabled:  true,
				MaxDepth: 2,
			})
		}
	}

	rawScripts := make([]ServiceEntry, 0, 32)
	stopCandidates := make(map[string][]string) // dir -> stop script paths
	seenPaths := make(map[string]struct{})

	for _, root := range roots {
		if !root.Enabled || strings.TrimSpace(root.Path) == "" {
			continue
		}
		absRoot, err := filepath.Abs(root.Path)
		if err != nil {
			continue
		}
		if _, err := os.Stat(absRoot); err != nil {
			continue
		}
		maxDepth := root.MaxDepth
		if maxDepth <= 0 {
			maxDepth = 3
		}
		_ = filepath.WalkDir(absRoot, func(p string, d fs.DirEntry, werr error) error {
			if werr != nil {
				return nil
			}
			rel, rerr := filepath.Rel(absRoot, p)
			depth := 0
			if rerr == nil && rel != "." {
				depth = strings.Count(rel, string(filepath.Separator)) + 1
			}
			if d.IsDir() {
				if p != absRoot && shouldSkipDir(d.Name()) {
					return filepath.SkipDir
				}
				if depth >= maxDepth {
					return filepath.SkipDir
				}
				return nil
			}
			ext := strings.ToLower(filepath.Ext(d.Name()))
			if ext != ".bat" && ext != ".cmd" && ext != ".ps1" {
				return nil
			}
			normPath := filepath.Clean(p)
			lowerPath := strings.ToLower(normPath)
			if _, ok := seenPaths[lowerPath]; ok {
				return nil
			}
			seenPaths[lowerPath] = struct{}{}

			// Never list Server Manager's own helper scripts unless explicitly added.
			if strings.Contains(lowerPath, `\server manager\`) {
				return nil
			}

			baseLower := strings.ToLower(d.Name())
			if strings.HasPrefix(baseLower, "stop") {
				dirKey := strings.ToLower(filepath.Dir(normPath))
				stopCandidates[dirKey] = append(stopCandidates[dirKey], normPath)
				return nil
			}

			entry, ok := parseScriptEntry(absRoot, normPath)
			if ok {
				rawScripts = append(rawScripts, entry)
			}
			return nil
		})
	}

	// Pair stop scripts with start scripts in the same folder (or parent folder for scripts\ subdir).
	for i := range rawScripts {
		dir := filepath.Dir(rawScripts[i].ScriptPath)
		cands := stopCandidates[strings.ToLower(dir)]
		if len(cands) == 0 {
			cands = stopCandidates[strings.ToLower(filepath.Dir(dir))]
		}
		if bestStop := pickMatchingStop(rawScripts[i].ScriptPath, cands); bestStop != "" {
			rawScripts[i].StopScriptPath = bestStop
			rawScripts[i].StopCmd = buildScriptCommand(bestStop)
			rawScripts[i].ScriptHash = hashFiles(rawScripts[i].ScriptPath, bestStop)
		}
	}

	// Merge custom services and notes.
	usedIDs := make(map[string]struct{}, len(rawScripts)+len(cfg.CustomServices)+8)
	out := make([]ServiceEntry, 0, len(rawScripts)+len(cfg.CustomServices)+8)

	regPortMap := extractRegistryPortMap(reg)
	regURLMap := extractRegistryURLMap(reg)

	for _, s := range rawScripts {
		s.ID = ensureUniqueID(s.ID, usedIDs)
		projSlug := slugify(s.ProjectName)
		if len(s.Ports) == 0 {
			if rp, ok := regPortMap[projSlug]; ok && rp > 0 {
				s.Ports = []int{rp}
			}
		}
		if s.WebURL == "" {
			if ru, ok := regURLMap[projSlug]; ok && ru != "" {
				s.WebURL = ru
			} else if len(s.Ports) > 0 && s.Category == "server" {
				s.WebURL = fmt.Sprintf("http://127.0.0.1:%d", s.Ports[0])
			}
		}
		if note := strings.TrimSpace(cfg.Notes[s.ID]); note != "" {
			s.UserNote = note
		}
		out = append(out, s)
	}

	for _, cs := range cfg.CustomServices {
		id := cs.ID
		if id == "" {
			id = slugify("custom-" + cs.Name)
		}
		id = ensureUniqueID(id, usedIDs)
		ports := append([]int(nil), cs.Ports...)
		if len(ports) == 0 && cs.WebURL != "" {
			ports = extractPorts(cs.WebURL)
		}
		note := strings.TrimSpace(cfg.Notes[id])
		if note == "" {
			note = cs.Description
		}
		out = append(out, ServiceEntry{
			ID:            id,
			Name:          cs.Name,
			ProjectName:   cs.Name,
			Category:      firstNonEmpty(cs.Category, "custom"),
			SourceType:    "custom",
			ScriptPath:    cs.ScriptPath,
			WorkingDir:    cs.WorkingDir,
			StartCmd:      cs.StartCmd,
			StopCmd:       cs.StopCmd,
			Ports:         ports,
			WebURL:        cs.WebURL,
			Runtime:       "Custom Launcher",
			StaticSummary: firstNonEmpty(cs.Description, "User-configured launcher command."),
			UserNote:      note,
			ScriptHash:    hashString(cs.StartCmd + "|" + cs.StopCmd + "|" + cs.Description),
			IsCustom:      true,
			AddedByAI:     cs.AddedByAI,
			Tags:          cs.Tags,
		})
	}

	// Add known registry web services that don't already have a discovered script/port entry.
	if reg != nil {
		existingPorts := make(map[int]bool)
		for _, e := range out {
			if e.IsUtility {
				continue
			}
			for _, pt := range e.Ports {
				existingPorts[pt] = true
			}
		}
		out = append(out, buildRegistryFallbacks(reg, existingPorts, usedIDs, cfg.Notes)...)
	}

	DetectConflicts(out, reg)

	sort.Slice(out, func(i, j int) bool {
		if out[i].IsUtility != out[j].IsUtility {
			return !out[i].IsUtility
		}
		if out[i].IsCustom != out[j].IsCustom {
			return out[i].IsCustom
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})

	return out
}

func shouldSkipDir(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" || strings.HasPrefix(lower, ".") && lower != ".cli-proxy-api" {
		return true
	}
	_, skip := skipDirNames[lower]
	return skip
}

func parseScriptEntry(rootDir, scriptPath string) (ServiceEntry, bool) {
	raw, err := os.ReadFile(scriptPath)
	if err != nil {
		return ServiceEntry{}, false
	}
	content := string(raw)
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(scriptPath), "."))
	base := filepath.Base(scriptPath)
	stem := strings.TrimSuffix(base, filepath.Ext(base))

	projectName := inferProjectName(rootDir, scriptPath)
	lowerBase := strings.ToLower(base)
	lowerContent := strings.ToLower(content)

	isUtility := isUtilityScript(lowerBase, lowerContent)
	ports := extractPorts(content)
	webURL := extractWebURL(content, ports)
	bindHost := "127.0.0.1"
	if strings.Contains(lowerContent, "0.0.0.0") {
		bindHost = "0.0.0.0"
	}
	runtimeLabel := detectRuntime(lowerContent, ext)
	category := "server"
	if isUtility {
		category = "utility"
	} else if strings.Contains(lowerContent, "presentationframework") || strings.Contains(lowerContent, "electron") {
		category = "desktop"
	}

	id := slugify(projectName + "-" + stem)
	displayName := fmt.Sprintf("%s (%s)", projectName, base)
	if strings.EqualFold(projectName, stem) || strings.Contains(strings.ToLower(stem), strings.ToLower(strings.ReplaceAll(projectName, " ", "-"))) {
		displayName = projectName
	}

	summary := buildStaticSummary(projectName, base, runtimeLabel, ports, webURL, bindHost, content, isUtility)
	return ServiceEntry{
		ID:            id,
		Name:          displayName,
		ProjectName:   projectName,
		Category:      category,
		SourceType:    ext,
		ScriptPath:    scriptPath,
		WorkingDir:    filepath.Dir(scriptPath),
		StartCmd:      buildScriptCommand(scriptPath),
		Ports:         ports,
		WebURL:        webURL,
		BindHost:      bindHost,
		Runtime:       runtimeLabel,
		StaticSummary: summary,
		ScriptHash:    hashFiles(scriptPath, ""),
		IsUtility:     isUtility,
	}, true
}

func inferProjectName(rootDir, scriptPath string) string {
	rel, err := filepath.Rel(rootDir, scriptPath)
	if err != nil {
		return filepath.Base(filepath.Dir(scriptPath))
	}
	parts := strings.Split(filepath.Clean(rel), string(filepath.Separator))
	if len(parts) <= 1 {
		return filepath.Base(rootDir)
	}
	// If inside Agentic Harness\Obsidian Plugins\PluginName\..., use PluginName.
	if len(parts) >= 3 && strings.EqualFold(parts[0], "Obsidian Plugins") {
		return parts[1]
	}
	return parts[0]
}

func isUtilityScript(lowerBase, lowerContent string) bool {
	for _, exact := range []string{
		"gradlew.bat", "local-ci.ps1", "configure-ai.bat", "configure-ai.ps1",
		"deploy-to-vault.bat", "deploy-to-vault.ps1", "install-autostart.cmd",
		"run-cli.bat", "updateandstart.bat", "updateforkandstart.bat", "remote-link.cmd",
	} {
		if lowerBase == exact {
			return true
		}
	}
	for _, needle := range []string{
		"bump-version", "build_releases", "verify-release", "fix-encoding",
		"make_icon", "pack.ps1", "smoke-installer", "update_models", "setup_", "sync-to-st",
	} {
		if strings.Contains(lowerBase, needle) {
			return true
		}
	}
	// Keep explicit server/gui launchers visible; treat remaining scripts without server clues as utility.
	for _, clue := range []string{"uvicorn", "vite", "electron", "server", "start", "run", "cliproxy", "http://", "--gui", "presentationframework"} {
		if strings.Contains(lowerBase, clue) || strings.Contains(lowerContent, clue) {
			return false
		}
	}
	return true
}

func detectRuntime(lowerContent, ext string) string {
	switch {
	case strings.Contains(lowerContent, "uvicorn"):
		return "Python / Uvicorn"
	case strings.Contains(lowerContent, "vite") || strings.Contains(lowerContent, "npm run dev"):
		return "Node / Vite"
	case strings.Contains(lowerContent, "electron"):
		return "Electron Desktop"
	case strings.Contains(lowerContent, "presentationframework"):
		return "PowerShell WPF GUI"
	case strings.Contains(lowerContent, "python"):
		return "Python"
	case strings.Contains(lowerContent, "node ") || strings.Contains(lowerContent, "node.exe"):
		return "Node.js"
	case strings.Contains(lowerContent, "cliproxyapi"):
		return "Go Binary (CLIProxyAPI)"
	case ext == "ps1":
		return "PowerShell"
	default:
		return "Windows Batch"
	}
}

func extractPorts(text string) []int {
	seen := map[int]bool{}
	var ports []int
	add := func(raw string) {
		p, err := strconv.Atoi(raw)
		if err != nil || p < 80 || p > 65535 {
			return
		}
		if !seen[p] {
			seen[p] = true
			ports = append(ports, p)
		}
	}
	for _, m := range urlPortRegex.FindAllStringSubmatch(text, -1) {
		add(m[1])
	}
	for _, m := range portFlagRegex.FindAllStringSubmatch(text, -1) {
		add(m[1])
	}
	for _, m := range colonPortRegex.FindAllStringSubmatch(text, -1) {
		add(m[1])
	}
	sort.Ints(ports)
	return ports
}

func extractWebURL(text string, ports []int) string {
	if m := httpURLRegex.FindString(text); m != "" {
		return strings.TrimRight(m, `.,;)'"`)
	}
	if len(ports) > 0 {
		return fmt.Sprintf("http://127.0.0.1:%d", ports[0])
	}
	return ""
}

func buildStaticSummary(project, file, runtime string, ports []int, webURL, bindHost, content string, isUtil bool) string {
	if isUtil {
		return fmt.Sprintf("Utility/helper script (%s) inside %s.", file, project)
	}
	var hints []string
	for _, m := range titleEchoRegex.FindAllStringSubmatch(content, 5) {
		line := strings.TrimSpace(m[1])
		line = strings.Trim(line, `"'`)
		if commandEchoSkip.MatchString(line) || len(line) < 4 || len(line) > 90 {
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "note:") || strings.HasPrefix(strings.ToLower(line), "press ") {
			continue
		}
		hints = append(hints, line)
		if len(hints) == 1 {
			break
		}
	}
	portPart := "no HTTP port detected"
	if len(ports) > 0 {
		ps := make([]string, len(ports))
		for i, p := range ports {
			ps[i] = strconv.Itoa(p)
		}
		portPart = "port(s) " + strings.Join(ps, ", ")
		if bindHost == "0.0.0.0" {
			portPart += " (LAN 0.0.0.0)"
		}
	}
	if len(hints) > 0 {
		return fmt.Sprintf("%s — runs via %s on %s.", hints[0], runtime, portPart)
	}
	if webURL != "" {
		return fmt.Sprintf("Launches %s (%s) on %s (%s).", project, runtime, portPart, webURL)
	}
	return fmt.Sprintf("Launches %s via %s.", project, runtime)
}

func pickMatchingStop(startPath string, candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	ext := strings.ToLower(filepath.Ext(startPath))
	for _, c := range candidates {
		if strings.ToLower(filepath.Ext(c)) == ext {
			return c
		}
	}
	return candidates[0]
}

func buildScriptCommand(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".ps1" {
		return fmt.Sprintf(`powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%s"`, path)
	}
	return fmt.Sprintf(`cmd.exe /c call "%s"`, path)
}

func buildRegistryFallbacks(reg *config.EcosystemRegistry, existingPorts map[int]bool, usedIDs map[string]struct{}, notes map[string]string) []ServiceEntry {
	var out []ServiceEntry
	seenRegPorts := make(map[int]bool)
	seenAliases := map[string]bool{}

	// Merge apps & services preferring apps metadata first (contains repo/cli/exe), then unique services.
	keys := make([]string, 0, len(reg.Apps)+len(reg.Services))
	merged := make(map[string]map[string]any)
	for k, m := range reg.Apps {
		merged[k] = m
		keys = append(keys, k)
	}
	for k, m := range reg.Services {
		if _, exists := merged[k]; !exists {
			merged[k] = m
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	for _, key := range keys {
		meta := merged[key]
		urlStr, _ := meta["url"].(string)
		webURL, _ := meta["webUrl"].(string)
		if urlStr == "" {
			urlStr = webURL
		}
		var ports []int
		if pFloat, ok := meta["port"].(float64); ok && pFloat > 0 {
			ports = append(ports, int(pFloat))
		}
		for _, pt := range extractPorts(urlStr + " " + webURL) {
			if len(ports) == 0 || ports[0] != pt {
				ports = append(ports, pt)
			}
		}
		if len(ports) == 0 {
			continue
		}
		// Skip self, already-discovered script ports, and alias duplicates (e.g. anki vs ankiConnect, obsidianRestApi vs obsidianAgentManager).
		if strings.EqualFold(key, "serverManager") || existingPorts[ports[0]] || seenRegPorts[ports[0]] {
			continue
		}
		provider, _ := meta["provider"].(string)
		if provider != "" && seenAliases[provider] {
			continue
		}
		seenRegPorts[ports[0]] = true
		seenAliases[key] = true

		id := ensureUniqueID("reg-"+slugify(key), usedIDs)
		repo, _ := meta["repo"].(string)
		exe, _ := meta["executable"].(string)
		cli, _ := meta["cli"].(string)
		startCmd := exe
		if startCmd == "" && key == "ragManager" && repo != "" {
			startCmd = "uv run rag-manager serve"
		}
		if startCmd == "" && key == "cloudManager" {
			startCmd = "CloudManager.exe"
		}
		if startCmd == "" && key == "ollama" {
			startCmd = "ollama serve"
		}
		if startCmd == "" && cli != "" {
			startCmd = cli
		}
		summary := fmt.Sprintf("Registered ecosystem service (%s) at %s.", key, firstNonEmpty(webURL, urlStr))
		if startCmd == "" {
			summary += " (External/app-managed — monitored by port.)"
		}
		out = append(out, ServiceEntry{
			ID:            id,
			Name:          prettyKey(key),
			ProjectName:   prettyKey(key),
			Category:      "registry",
			SourceType:    "registry",
			WorkingDir:    repo,
			StartCmd:      startCmd,
			Ports:         ports,
			WebURL:        firstNonEmpty(webURL, urlStr),
			BindHost:      "127.0.0.1",
			Runtime:       "Ecosystem Registry",
			StaticSummary: summary,
			UserNote:      strings.TrimSpace(notes[id]),
			ScriptHash:    hashString(key + "|" + urlStr + "|" + startCmd),
		})
	}
	return out
}

func extractRegistryURLMap(reg *config.EcosystemRegistry) map[string]string {
	res := make(map[string]string)
	if reg == nil {
		return res
	}
	readMap := func(m map[string]map[string]any) {
		for k, meta := range m {
			slug := slugify(prettyKey(k))
			u, _ := meta["webUrl"].(string)
			if u == "" {
				u, _ = meta["url"].(string)
			}
			if u != "" {
				res[slug] = u
			}
		}
	}
	readMap(reg.Services)
	readMap(reg.Apps)
	return res
}

func ensureUniqueID(base string, used map[string]struct{}) string {
	if base == "" {
		base = "service"
	}
	cand := base
	n := 2
	for {
		if _, exists := used[cand]; !exists {
			used[cand] = struct{}{}
			return cand
		}
		cand = fmt.Sprintf("%s-%d", base, n)
		n++
	}
}

func slugify(s string) string {
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

func prettyKey(k string) string {
	var b strings.Builder
	for i, r := range k {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
		}
		if i == 0 && r >= 'a' && r <= 'z' {
			b.WriteRune(r - 32)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func hashFiles(p1, p2 string) string {
	h := sha256.New()
	for _, p := range []string{p1, p2} {
		if p == "" {
			continue
		}
		if raw, err := os.ReadFile(p); err == nil {
			h.Write(raw)
		}
		h.Write([]byte{'|'})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:16]
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
