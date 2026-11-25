package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Mostakim2005/server-manager/internal/security"
)

const (
	DefaultBindHost    = "127.0.0.1"
	DefaultPort        = 8790
	DefaultCLIProxyURL = "http://127.0.0.1:8317"
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Paths holds resolved runtime, registry, and configuration locations without hardcoded user names.
type Paths struct {
	HomeDir             string `json:"homeDir"`
	DataDir             string `json:"dataDir"`
	LogsDir             string `json:"logsDir"`
	SharedLibrariesDir  string `json:"sharedLibrariesDir"`
	LocalBinDir         string `json:"localBinDir"`
	DesktopDir          string `json:"desktopDir"`
	StartupDir          string `json:"startupDir"`
	RegistryPrimaryPath string `json:"registryPrimaryPath"`
	RegistryMirrorPath  string `json:"registryMirrorPath"`
	SecretsPath         string `json:"secretsPath"`
	CLIProxyDir         string `json:"cliProxyDir"`
	AgenticHarnessDir   string `json:"agenticHarnessDir"`
	ConfigPath          string `json:"configPath"`
	AICachePath         string `json:"aiCachePath"`
	IconsCacheDir       string `json:"iconsCacheDir"`
	IconICOPath         string `json:"iconIcoPath"`
}

// ScanRoot describes a filesystem root scanned for server/launcher scripts.
type ScanRoot struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Path     string `json:"path"`
	Enabled  bool   `json:"enabled"`
	MaxDepth int    `json:"maxDepth"`
}

// CustomService holds a user-defined or AI-assisted project/script entry.
type CustomService struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Category    string   `json:"category"`
	Description string   `json:"description"`
	WorkingDir  string   `json:"workingDir"`
	StartCmd    string   `json:"startCmd"`
	StopCmd     string   `json:"stopCmd,omitempty"`
	ScriptPath  string   `json:"scriptPath,omitempty"`
	Ports       []int    `json:"ports,omitempty"`
	WebURL      string   `json:"webUrl,omitempty"`
	IconPath    string   `json:"iconPath,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	AddedByAI   bool     `json:"addedByAi,omitempty"`
	UpdatedAt   string   `json:"updatedAt,omitempty"`
}

// StackPreset groups multiple service IDs for one-click stack start/stop (Charlie 4).
type StackPreset struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	ServiceIDs  []string `json:"serviceIds"`
}

// AppConfig stores persisted Server Manager settings, custom entries, and stack presets.
type AppConfig struct {
	SchemaVersion        int               `json:"schemaVersion"`
	Theme                string            `json:"theme"`
	AutoStartCLIProxy    bool              `json:"autoStartCliProxy"`
	ShowUtilityScripts   bool              `json:"showUtilityScripts"`
	IncludeVirtualDriveD bool              `json:"includeVirtualDriveD"`
	CLIProxyURL          string            `json:"cliProxyUrl"`
	PreferredAIModel     string            `json:"preferredAiModel,omitempty"`
	ScanRoots            []ScanRoot        `json:"scanRoots"`
	CustomServices       []CustomService   `json:"customServices"`
	Stacks               []StackPreset     `json:"stacks"`
	Notes                map[string]string `json:"notes"`
	IconOverrides        map[string]string `json:"iconOverrides,omitempty"`
	PreferredScripts     map[string]string `json:"preferredScripts,omitempty"`
	UngroupedIDs         []string          `json:"ungroupedIds,omitempty"`
	ManualGroups         map[string]string `json:"manualGroups,omitempty"`
}

// AIReport holds a structured plain-English AI explanation for a script/service.
type AIReport struct {
	ServiceID        string   `json:"serviceId"`
	ScriptHash       string   `json:"scriptHash"`
	Model            string   `json:"model"`
	GeneratedAt      string   `json:"generatedAt"`
	Summary          string   `json:"summary"`
	CommandBreakdown []string `json:"commandBreakdown"`
	Ports            []string `json:"ports"`
	Dependencies     []string `json:"dependencies"`
	RisksOrNotes     []string `json:"risksOrNotes"`
}

// AICache persists generated AI reports keyed by service ID.
type AICache struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Reports       map[string]AIReport `json:"reports"`
}

// EcosystemSecrets matches %USERPROFILE%\.local\share\ecosystem-secrets.json.
type EcosystemSecrets struct {
	SchemaVersion int               `json:"schemaVersion"`
	Tokens        map[string]string `json:"tokens"`
}

// EcosystemRegistry models %USERPROFILE%\.local\share\ecosystem-registry.json dynamically.
type EcosystemRegistry struct {
	SchemaVersion int                       `json:"schemaVersion"`
	UpdatedAt     string                    `json:"updatedAt"`
	Contract      string                    `json:"contract,omitempty"`
	Paths         map[string]string         `json:"paths,omitempty"`
	Services      map[string]map[string]any `json:"services,omitempty"`
	Apps          map[string]map[string]any `json:"apps,omitempty"`
	Toolchains    map[string]string         `json:"toolchains,omitempty"`
}

// DiscoverPaths locates local config, registry, secrets, SharedLibraries, and CLIProxyAPI paths.
func DiscoverPaths(overrideDataDir string) Paths {
	home, _ := os.UserHomeDir()
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		if runtime.GOOS == "windows" {
			localAppData = filepath.Join(home, "AppData", "Local")
		} else {
			localAppData = filepath.Join(home, ".local", "share")
		}
	}
	dataDir := overrideDataDir
	if dataDir == "" {
		if env := os.Getenv("SERVER_MANAGER_DATA_DIR"); env != "" {
			dataDir = env
		} else {
			dataDir = filepath.Join(localAppData, "ServerManager")
		}
	}
	sharedDir := firstNonEmpty(
		os.Getenv("LOCAL_COMMON_LIBRARIES"),
		os.Getenv("SHARED_LIBRARIES_DIR"),
		filepath.Join(localAppData, "SharedLibraries"),
	)
	regPrimary := firstNonEmpty(
		os.Getenv("LOCAL_ECOSYSTEM_REGISTRY"),
		filepath.Join(home, ".local", "share", "ecosystem-registry.json"),
	)
	regMirror := filepath.Join(sharedDir, "ecosystem-registry.json")
	secretsPath := firstNonEmpty(
		os.Getenv("LOCAL_ECOSYSTEM_SECRETS"),
		filepath.Join(home, ".local", "share", "ecosystem-secrets.json"),
	)
	harnessDir := filepath.Join(home, "Documents", "Agentic Harness")
	if reg, err := LoadRegistry(regPrimary, regMirror); err == nil {
		if s := reg.Paths["secretsFile"]; s != "" && os.Getenv("LOCAL_ECOSYSTEM_SECRETS") == "" {
			secretsPath = s
		}
		if ah := reg.Paths["agenticHarness"]; ah != "" {
			harnessDir = ah
		}
	}

	startupDir := os.Getenv("SERVER_MANAGER_STARTUP_DIR")
	if startupDir == "" {
		if runtime.GOOS == "windows" {
			appData := os.Getenv("APPDATA")
			if appData == "" {
				appData = filepath.Join(home, "AppData", "Roaming")
			}
			startupDir = filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "Startup")
		} else {
			startupDir = filepath.Join(dataDir, "startup")
		}
	}

	return Paths{
		HomeDir:             home,
		DataDir:             dataDir,
		LogsDir:             filepath.Join(dataDir, "logs"),
		SharedLibrariesDir:  sharedDir,
		LocalBinDir:         filepath.Join(home, ".local", "bin"),
		DesktopDir:          filepath.Join(home, "Desktop"),
		StartupDir:          startupDir,
		RegistryPrimaryPath: regPrimary,
		RegistryMirrorPath:  regMirror,
		SecretsPath:         secretsPath,
		CLIProxyDir:         filepath.Join(home, ".cli-proxy-api"),
		AgenticHarnessDir:   harnessDir,
		ConfigPath:          filepath.Join(dataDir, "config.json"),
		AICachePath:         filepath.Join(dataDir, "ai-cache.json"),
		IconsCacheDir:       filepath.Join(dataDir, "icons"),
		IconICOPath:         filepath.Join(dataDir, "ServerManager.ico"),
	}
}

// DefaultConfig returns sensible first-run scan roots, defaults, and starter stacks.
func DefaultConfig(p Paths) AppConfig {
	roots := []ScanRoot{
		{ID: "agentic-harness", Label: "Agentic Harness", Path: p.AgenticHarnessDir, Enabled: true, MaxDepth: 3},
		{ID: "user-services", Label: "User Services", Path: filepath.Join(p.HomeDir, "Services"), Enabled: true, MaxDepth: 2},
		{ID: "cli-proxy", Label: "CLIProxyAPI", Path: p.CLIProxyDir, Enabled: true, MaxDepth: 1},
	}
	if _, err := os.Stat(`E:\SillyTavern`); err == nil {
		roots = append(roots, ScanRoot{ID: "sillytavern", Label: "SillyTavern (E:)", Path: `E:\SillyTavern`, Enabled: true, MaxDepth: 2})
	}
	return AppConfig{
		SchemaVersion:        1,
		Theme:                "system",
		AutoStartCLIProxy:    true,
		ShowUtilityScripts:   false,
		IncludeVirtualDriveD: false,
		CLIProxyURL:          DefaultCLIProxyURL,
		ScanRoots:            roots,
		CustomServices:       []CustomService{},
		Stacks: []StackPreset{
			{
				ID:          "study-ai-stack",
				Name:        "Study + AI Stack",
				Description: "Launch CLIProxyAPI + Paradee TTS + DeepTutor together.",
				ServiceIDs:  []string{"cli-proxy-api-run-cliproxy", "paradee-tts-start", "deeptutor-start"},
			},
			{
				ID:          "media-hub-stack",
				Name:        "Data & Media Web Hubs",
				Description: "Start Database Manager (:8770) and Doujin Manager (:8780).",
				ServiceIDs:  []string{"database-manager-start-server", "doujin-manager-run-lan"},
			},
		},
		Notes:            map[string]string{},
		IconOverrides:    map[string]string{},
		PreferredScripts: map[string]string{},
		UngroupedIDs:     []string{},
		ManualGroups:     map[string]string{},
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// StripUTF8BOM removes an accidental UTF-8 BOM prefix.
func StripUTF8BOM(b []byte) []byte {
	return bytes.TrimPrefix(b, utf8BOM)
}

// WriteAtomicNoBOM writes bytes atomically via temp file + sync + rename without UTF-8 BOM.
func WriteAtomicNoBOM(path string, data []byte, perm os.FileMode) error {
	clean := StripUTF8BOM(data)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(clean); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil && runtime.GOOS != "windows" {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// WriteJSONAtomic encodes pretty UTF-8 JSON without BOM atomically.
func WriteJSONAtomic(path string, v any, perm os.FileMode) error {
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	return WriteAtomicNoBOM(path, buf.Bytes(), perm)
}

// LoadOrInitConfig loads config.json or initializes defaults safely.
func LoadOrInitConfig(p Paths) (AppConfig, error) {
	def := DefaultConfig(p)
	raw, err := os.ReadFile(p.ConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			if werr := WriteJSONAtomic(p.ConfigPath, def, 0o600); werr != nil {
				return def, werr
			}
			return def, nil
		}
		return def, err
	}
	var cfg AppConfig
	if err := json.Unmarshal(StripUTF8BOM(raw), &cfg); err != nil {
		return def, err
	}
	if cfg.SchemaVersion == 0 {
		cfg.SchemaVersion = 1
	}
	if cfg.Theme == "" {
		cfg.Theme = "system"
	}
	if cfg.CLIProxyURL == "" {
		cfg.CLIProxyURL = DefaultCLIProxyURL
	}
	if cfg.ScanRoots == nil {
		cfg.ScanRoots = def.ScanRoots
	}
	if cfg.CustomServices == nil {
		cfg.CustomServices = []CustomService{}
	}
	if cfg.Stacks == nil {
		cfg.Stacks = def.Stacks
	}
	if cfg.Notes == nil {
		cfg.Notes = map[string]string{}
	}
	if cfg.IconOverrides == nil {
		cfg.IconOverrides = map[string]string{}
	}
	if cfg.PreferredScripts == nil {
		cfg.PreferredScripts = map[string]string{}
	}
	if cfg.UngroupedIDs == nil {
		cfg.UngroupedIDs = []string{}
	}
	if cfg.ManualGroups == nil {
		cfg.ManualGroups = map[string]string{}
	}
	return cfg, nil
}

// SaveConfig persists AppConfig atomically without UTF-8 BOM.
func SaveConfig(p Paths, cfg AppConfig) error {
	cfg.SchemaVersion = 1
	return WriteJSONAtomic(p.ConfigPath, cfg, 0o600)
}

// LoadOrInitAICache loads cached AI script reports.
func LoadOrInitAICache(p Paths) (AICache, error) {
	empty := AICache{SchemaVersion: 1, Reports: map[string]AIReport{}}
	raw, err := os.ReadFile(p.AICachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return empty, nil
		}
		return empty, err
	}
	var c AICache
	if err := json.Unmarshal(StripUTF8BOM(raw), &c); err != nil {
		return empty, err
	}
	if c.Reports == nil {
		c.Reports = map[string]AIReport{}
	}
	if c.SchemaVersion == 0 {
		c.SchemaVersion = 1
	}
	return c, nil
}

// SaveAICache persists cached AI reports atomically without UTF-8 BOM.
func SaveAICache(p Paths, cache AICache) error {
	cache.SchemaVersion = 1
	return WriteJSONAtomic(p.AICachePath, cache, 0o600)
}

// LoadRegistry loads the first readable ecosystem-registry.json file.
func LoadRegistry(candidates ...string) (*EcosystemRegistry, error) {
	for _, p := range candidates {
		if p == "" {
			continue
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var reg EcosystemRegistry
		if err := json.Unmarshal(StripUTF8BOM(raw), &reg); err != nil {
			return nil, fmt.Errorf("parse registry %s: %w", p, err)
		}
		return &reg, nil
	}
	return nil, os.ErrNotExist
}

// LoadOrInitSecrets loads ecosystem-secrets.json and ensures tokens.serverManager exists.
func LoadOrInitSecrets(path string) (*EcosystemSecrets, error) {
	var sec EcosystemSecrets
	if raw, err := os.ReadFile(path); err == nil {
		if uerr := json.Unmarshal(StripUTF8BOM(raw), &sec); uerr != nil {
			return nil, fmt.Errorf("parse secrets %s: %w", path, uerr)
		}
	}
	if sec.SchemaVersion == 0 {
		sec.SchemaVersion = 1
	}
	if sec.Tokens == nil {
		sec.Tokens = make(map[string]string)
	}
	if sec.Tokens["serverManager"] == "" {
		tok, err := security.GenerateToken()
		if err != nil {
			return nil, err
		}
		sec.Tokens["serverManager"] = tok
		if err := WriteJSONAtomic(path, &sec, 0o600); err != nil {
			return nil, err
		}
	}
	return &sec, nil
}

// SyncEcosystemRegistry registers Server Manager in ecosystem-registry.json (and SharedLibraries mirror if present).
func SyncEcosystemRegistry(p Paths, port int, repoDir, exePath, cliPath string) error {
	raw, err := os.ReadFile(p.RegistryPrimaryPath)
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := json.Unmarshal(StripUTF8BOM(raw), &doc); err != nil {
		return err
	}
	doc["updatedAt"] = time.Now().Format("2006-01-02")

	services, _ := doc["services"].(map[string]any)
	if services == nil {
		services = map[string]any{}
	}
	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	services["serverManager"] = map[string]any{
		"url": url,
	}
	doc["services"] = services

	apps, _ := doc["apps"].(map[string]any)
	if apps == nil {
		apps = map[string]any{}
	}
	appEntry := map[string]any{
		"repo":   repoDir,
		"github": "https://github.com/Mostakim2005/server-manager",
		"url":    url,
		"port":   port,
	}
	if exePath != "" {
		appEntry["executable"] = exePath
	}
	if cliPath != "" {
		appEntry["cli"] = cliPath
	}
	apps["serverManager"] = appEntry
	doc["apps"] = apps

	if err := WriteJSONAtomic(p.RegistryPrimaryPath, doc, 0o600); err != nil {
		return err
	}
	if _, serr := os.Stat(p.RegistryMirrorPath); serr == nil {
		_ = WriteJSONAtomic(p.RegistryMirrorPath, doc, 0o600)
	}
	return nil
}

// ResolveRepoDir returns the local Server Manager source repository directory when present.
func ResolveRepoDir(p Paths, reg *EcosystemRegistry) string {
	hasMarker := func(dir string) bool {
		if strings.TrimSpace(dir) == "" {
			return false
		}
		for _, rel := range []string{
			filepath.Join("internal", "webui", "static", "index.html"),
			"start-dev-hidden.vbs",
		} {
			if st, err := os.Stat(filepath.Join(dir, rel)); err == nil && !st.IsDir() {
				return true
			}
		}
		return false
	}

	if env := strings.TrimSpace(os.Getenv("SERVER_MANAGER_REPO_DIR")); env != "" && hasMarker(env) {
		return filepath.Clean(env)
	}
	if reg != nil {
		if sm, ok := reg.Apps["serverManager"]; ok {
			if repo, _ := sm["repo"].(string); hasMarker(repo) {
				return filepath.Clean(repo)
			}
		}
	}
	if p.AgenticHarnessDir != "" {
		cand := filepath.Join(p.AgenticHarnessDir, "Server Manager")
		if hasMarker(cand) {
			return filepath.Clean(cand)
		}
	}
	if cwd, err := os.Getwd(); err == nil && hasMarker(cwd) {
		return filepath.Clean(cwd)
	}
	return ""
}
