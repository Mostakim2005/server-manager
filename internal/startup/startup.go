package startup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Mostakim2005/server-manager/internal/config"
	"github.com/Mostakim2005/server-manager/internal/discovery"
)

const (
	// SelfServiceID identifies Server Manager's own Windows Startup entry.
	SelfServiceID = "server-manager"
	// SelfShortcutName is the Startup folder shortcut filename for Server Manager itself.
	SelfShortcutName = "Server Manager.lnk"
)

// Status describes the Windows Startup configuration and Task Manager approval state for a service or Server Manager itself.
type Status struct {
	ServiceID           string `json:"serviceId"`
	Supported           bool   `json:"supported"`
	CanToggle           bool   `json:"canToggle"`
	Enabled             bool   `json:"enabled"`
	Approved            bool   `json:"approved"`
	TaskManagerDisabled bool   `json:"taskManagerDisabled"`
	ShortcutName        string `json:"shortcutName,omitempty"`
	ShortcutPath        string `json:"shortcutPath,omitempty"`
	LaunchMode          string `json:"launchMode,omitempty"`
	Reason              string `json:"reason,omitempty"`
}

// ShortcutMeta captures metadata parsed from an existing .lnk shortcut in the Startup folder.
type ShortcutMeta struct {
	Name             string `json:"name"`
	Path             string `json:"path"`
	TargetPath       string `json:"target"`
	Arguments        string `json:"args"`
	WorkingDirectory string `json:"workDir"`
	Description      string `json:"description"`
}

// ShortcutSpec defines properties used when writing a Windows .lnk shortcut.
type ShortcutSpec struct {
	Path             string
	TargetPath       string
	Arguments        string
	WorkingDirectory string
	IconLocation     string
	Description      string
}

type backend interface {
	Supported() bool
	ListShortcuts(dir string) ([]ShortcutMeta, error)
	WriteShortcut(spec ShortcutSpec) error
	RemoveShortcut(path string) error
	IsStartupApproved(shortcutName string) (bool, error)
	SetStartupApproved(shortcutName string, enabled bool) error
	OpenFolder(dir string) error
}

// Manager inspects and toggles Windows Startup folder shortcuts and Task Manager StartupApproved state.
type Manager struct {
	mu         sync.Mutex
	paths      config.Paths
	backend    backend
	lastSig    string
	cachedList []ShortcutMeta
}

// NewManager creates a Windows Startup manager bound to the configured Paths.
func NewManager(paths config.Paths) *Manager {
	return newManagerWithBackend(paths, osBackend{})
}

func newManagerWithBackend(paths config.Paths, b backend) *Manager {
	return &Manager{
		paths:   paths,
		backend: b,
	}
}

// Supported reports whether OS-level Windows Startup management is supported on the current platform.
func (m *Manager) Supported() bool {
	return m.backend != nil && m.backend.Supported()
}

// Snapshot returns per-service startup statuses alongside Server Manager's own startup status.
func (m *Manager) Snapshot(entries []discovery.ServiceEntry) (map[string]Status, Status) {
	shortcuts := m.snapshotShortcuts()
	selfStatus := m.computeSelfStatus(shortcuts)
	out := make(map[string]Status, len(entries))
	for _, entry := range entries {
		out[entry.ID] = m.computeServiceStatus(entry, shortcuts)
	}
	return out, selfStatus
}

// SelfStatus returns Server Manager's own Windows Startup status.
func (m *Manager) SelfStatus() Status {
	return m.computeSelfStatus(m.snapshotShortcuts())
}

// ServiceStatus returns the Windows Startup status for a single service entry.
func (m *Manager) ServiceStatus(entry discovery.ServiceEntry) Status {
	return m.computeServiceStatus(entry, m.snapshotShortcuts())
}

// SetSelfEnabled enables or disables Server Manager's own silent tray startup shortcut.
func (m *Manager) SetSelfEnabled(enabled bool) (Status, error) {
	if !m.Supported() {
		return m.SelfStatus(), fmt.Errorf("Windows Startup is only supported on Windows")
	}
	shortcuts := m.snapshotShortcuts()
	existing := matchSelfShortcut(shortcuts)
	if enabled {
		targetPath := existingPath(existing, filepath.Join(m.paths.StartupDir, SelfShortcutName))
		guiExe := ResolveGUIExe(m.paths)
		iconLoc := ""
		if fileExists(m.paths.IconICOPath) {
			iconLoc = m.paths.IconICOPath + ",0"
		}
		spec := ShortcutSpec{
			Path:             targetPath,
			TargetPath:       guiExe,
			Arguments:        "--tray",
			WorkingDirectory: m.paths.DataDir,
			IconLocation:     iconLoc,
			Description:      "Server Manager — Start silently in Windows System Tray",
		}
		if err := m.backend.WriteShortcut(spec); err != nil {
			return m.SelfStatus(), err
		}
		_ = m.backend.SetStartupApproved(filepath.Base(targetPath), true)
	} else {
		var existingList []ShortcutMeta
		if existing != nil {
			existingList = []ShortcutMeta{*existing}
		}
		m.removeMatchingShortcuts(filepath.Join(m.paths.StartupDir, SelfShortcutName), existingList)
	}
	m.invalidateCache()
	return m.SelfStatus(), nil
}

// SetServiceEnabled enables or disables Windows Startup for a discovered, custom, or registry service.
func (m *Manager) SetServiceEnabled(entry discovery.ServiceEntry, enabled bool) (Status, error) {
	if !m.Supported() {
		return m.ServiceStatus(entry), fmt.Errorf("Windows Startup is only supported on Windows")
	}
	if enabled && !CanToggleService(entry) {
		return m.ServiceStatus(entry), fmt.Errorf("service %q has no local start command or script path", entry.Name)
	}

	shortcuts := m.snapshotShortcuts()
	existing := matchShortcutsForService(entry, shortcuts)
	if enabled {
		var firstExisting *ShortcutMeta
		if len(existing) > 0 {
			firstExisting = &existing[0]
		}
		spec := m.buildServiceSpec(entry, firstExisting)
		if err := m.backend.WriteShortcut(spec); err != nil {
			return m.ServiceStatus(entry), err
		}
		_ = m.backend.SetStartupApproved(filepath.Base(spec.Path), true)
	} else {
		canonical := filepath.Join(m.paths.StartupDir, PreferredShortcutName(entry))
		m.removeMatchingShortcuts(canonical, existing)
	}
	m.invalidateCache()
	return m.ServiceStatus(entry), nil
}

// OpenFolder opens the Windows Startup folder in File Explorer.
func (m *Manager) OpenFolder() error {
	if !m.Supported() {
		return fmt.Errorf("Windows Startup folder is only available on Windows")
	}
	return m.backend.OpenFolder(m.paths.StartupDir)
}

// CanToggleService reports whether a service has a runnable local script or start command.
func CanToggleService(entry discovery.ServiceEntry) bool {
	return strings.TrimSpace(entry.StartCmd) != "" || strings.TrimSpace(entry.ScriptPath) != ""
}

// PreferredShortcutName returns a clean, collision-free .lnk filename for a service.
func PreferredShortcutName(entry discovery.ServiceEntry) string {
	if entry.ID == "cli-proxy-api-run-cliproxy" {
		return "CLIProxyAPI.lnk"
	}
	base := SanitizeShortcutBase(entry.Name)
	if base == "" {
		base = SanitizeShortcutBase(entry.ID)
	}
	if base == "" {
		base = "Server"
	}
	if strings.EqualFold(base, "Server Manager") {
		base = base + " - " + SanitizeShortcutBase(entry.ID)
	}
	return base + ".lnk"
}

// SanitizeShortcutBase removes Windows-disallowed filename characters while keeping titles readable.
func SanitizeShortcutBase(name string) string {
	replacer := strings.NewReplacer(
		"<", "-", ">", "-", ":", "-", `"`, "",
		"/", "-", `\`, "-", "|", "-", "?", "", "*", "",
	)
	clean := replacer.Replace(strings.TrimSpace(name))
	clean = strings.TrimLeft(clean, ". ")
	clean = strings.TrimRight(clean, ". ")
	fields := strings.Fields(clean)
	return strings.Join(fields, " ")
}

// FindHiddenVBS looks for an accompanying silent VBScript launcher for a service.
func FindHiddenVBS(entry discovery.ServiceEntry) string {
	dir := strings.TrimSpace(entry.WorkingDir)
	if dir == "" && strings.TrimSpace(entry.ScriptPath) != "" {
		dir = filepath.Dir(entry.ScriptPath)
	}
	if dir == "" {
		return ""
	}
	var candidates []string
	lowerID := strings.ToLower(entry.ID)
	lowerScript := strings.ToLower(entry.ScriptPath)
	if strings.Contains(lowerID, "cliproxy") || strings.Contains(lowerScript, "cliproxy") {
		candidates = append(candidates, filepath.Join(dir, "start-cliproxy-hidden.vbs"))
	}
	if strings.TrimSpace(entry.ScriptPath) != "" {
		base := filepath.Base(entry.ScriptPath)
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		candidates = append(candidates,
			filepath.Join(dir, stem+"-hidden.vbs"),
			filepath.Join(dir, "start-"+stem+"-hidden.vbs"),
		)
	}
	for _, cand := range candidates {
		if fileExists(cand) {
			return cand
		}
	}
	return ""
}

// ResolveGUIExe locates the windowless ServerManager.exe binary for silent Windows Startup launches.
func ResolveGUIExe(paths config.Paths) string {
	if paths.LocalBinDir != "" {
		cand := filepath.Join(paths.LocalBinDir, "ServerManager.exe")
		if fileExists(cand) {
			return cand
		}
	}
	if exe, err := os.Executable(); err == nil && exe != "" {
		sib := filepath.Join(filepath.Dir(exe), "ServerManager.exe")
		if fileExists(sib) {
			return sib
		}
		if strings.EqualFold(filepath.Base(exe), "ServerManager.exe") {
			return exe
		}
	}
	if paths.LocalBinDir != "" {
		return filepath.Join(paths.LocalBinDir, "ServerManager.exe")
	}
	return "ServerManager.exe"
}

// ParseStartupApprovedBytes interprets Windows Explorer StartupApproved binary values.
// Windows uses an even first byte (e.g. 0x02, 0x06) when enabled and an odd first byte (e.g. 0x03, 0x01) when disabled.
func ParseStartupApprovedBytes(raw []byte) bool {
	if len(raw) == 0 {
		return true
	}
	return raw[0]%2 == 0
}

func (m *Manager) buildServiceSpec(entry discovery.ServiceEntry, existing *ShortcutMeta) ShortcutSpec {
	path := existingPath(existing, filepath.Join(m.paths.StartupDir, PreferredShortcutName(entry)))
	iconLoc := ""
	if fileExists(m.paths.IconICOPath) {
		iconLoc = m.paths.IconICOPath + ",0"
	}
	if vbs := FindHiddenVBS(entry); vbs != "" {
		return ShortcutSpec{
			Path:             path,
			TargetPath:       wscriptExePath(),
			Arguments:        fmt.Sprintf(`"%s"`, vbs),
			WorkingDirectory: firstNonEmpty(entry.WorkingDir, filepath.Dir(vbs)),
			IconLocation:     iconLoc,
			Description:      fmt.Sprintf("Start %s silently on Windows startup", entry.Name),
		}
	}
	return ShortcutSpec{
		Path:             path,
		TargetPath:       ResolveGUIExe(m.paths),
		Arguments:        fmt.Sprintf(`--launch-service "%s"`, entry.ID),
		WorkingDirectory: firstNonEmpty(entry.WorkingDir, m.paths.DataDir),
		IconLocation:     iconLoc,
		Description:      fmt.Sprintf("Server Manager Autostart — %s", entry.Name),
	}
}

func (m *Manager) computeSelfStatus(shortcuts []ShortcutMeta) Status {
	st := Status{
		ServiceID:    SelfServiceID,
		Supported:    m.Supported(),
		CanToggle:    m.Supported(),
		Approved:     true,
		ShortcutName: SelfShortcutName,
		ShortcutPath: filepath.Join(m.paths.StartupDir, SelfShortcutName),
		LaunchMode:   "tray",
	}
	if !st.Supported {
		st.Reason = "Windows Startup is only available on Windows"
		return st
	}
	if match := matchSelfShortcut(shortcuts); match != nil {
		st.Enabled = true
		st.ShortcutName = match.Name
		st.ShortcutPath = match.Path
		approved, err := m.backend.IsStartupApproved(match.Name)
		if err == nil {
			st.Approved = approved
			st.TaskManagerDisabled = !approved
		}
	}
	return st
}

func (m *Manager) computeServiceStatus(entry discovery.ServiceEntry, shortcuts []ShortcutMeta) Status {
	canToggle := m.Supported() && CanToggleService(entry)
	defaultMode := "server-manager-hidden"
	if FindHiddenVBS(entry) != "" {
		defaultMode = "vbs-hidden"
	}
	prefName := PreferredShortcutName(entry)
	st := Status{
		ServiceID:    entry.ID,
		Supported:    m.Supported(),
		CanToggle:    canToggle,
		Approved:     true,
		ShortcutName: prefName,
		ShortcutPath: filepath.Join(m.paths.StartupDir, prefName),
		LaunchMode:   defaultMode,
	}
	if !m.Supported() {
		st.Reason = "Windows Startup is only available on Windows"
		return st
	}
	if !CanToggleService(entry) {
		st.Reason = "No local start command configured"
	}
	matches := matchShortcutsForService(entry, shortcuts)
	if len(matches) > 0 {
		match := matches[0]
		st.Enabled = true
		st.ShortcutName = match.Name
		st.ShortcutPath = match.Path
		st.LaunchMode = detectLaunchMode(match)
		approved, err := m.backend.IsStartupApproved(match.Name)
		if err == nil {
			st.Approved = approved
			st.TaskManagerDisabled = !approved
		}
	}
	return st
}

func detectLaunchMode(meta ShortcutMeta) string {
	lowerTarget := strings.ToLower(meta.TargetPath)
	lowerArgs := strings.ToLower(meta.Arguments)
	if strings.Contains(lowerArgs, "--launch-service") {
		return "server-manager-hidden"
	}
	if strings.HasSuffix(lowerTarget, "wscript.exe") || strings.Contains(lowerArgs, ".vbs") {
		return "vbs-hidden"
	}
	if strings.EqualFold(meta.Name, SelfShortcutName) {
		return "tray"
	}
	return "external"
}

func matchSelfShortcut(shortcuts []ShortcutMeta) *ShortcutMeta {
	for i := range shortcuts {
		s := shortcuts[i]
		if strings.EqualFold(s.Name, SelfShortcutName) {
			return &s
		}
		lowerTarget := strings.ToLower(s.TargetPath)
		lowerArgs := strings.ToLower(s.Arguments)
		if strings.HasSuffix(lowerTarget, `\servermanager.exe`) && !strings.Contains(lowerArgs, "--launch-service") {
			return &s
		}
	}
	return nil
}

func matchShortcutsForService(entry discovery.ServiceEntry, shortcuts []ShortcutMeta) []ShortcutMeta {
	prefName := PreferredShortcutName(entry)
	vbs := strings.ToLower(FindHiddenVBS(entry))
	scriptLower := strings.ToLower(strings.TrimSpace(entry.ScriptPath))
	var out []ShortcutMeta
	seen := map[string]bool{}
	add := func(s ShortcutMeta) {
		k := strings.ToLower(s.Path)
		if !seen[k] {
			seen[k] = true
			out = append(out, s)
		}
	}

	for _, s := range shortcuts {
		if strings.EqualFold(s.Name, SelfShortcutName) {
			continue
		}
		if hasLaunchServiceID(s.Arguments, entry.ID) {
			add(s)
			continue
		}
		if strings.EqualFold(s.Name, prefName) {
			add(s)
			continue
		}
		combined := strings.ToLower(s.TargetPath + " " + s.Arguments)
		if vbs != "" && strings.Contains(combined, vbs) {
			add(s)
			continue
		}
		if scriptLower != "" && strings.Contains(combined, scriptLower) {
			add(s)
		}
	}
	return out
}

func hasLaunchServiceID(args, id string) bool {
	if args == "" || id == "" {
		return false
	}
	idx := strings.Index(strings.ToLower(args), "--launch-service")
	if idx < 0 {
		return false
	}
	rest := strings.TrimSpace(args[idx+len("--launch-service"):])
	rest = strings.TrimPrefix(rest, "=")
	rest = strings.TrimSpace(rest)
	if strings.HasPrefix(rest, `"`) {
		rest = strings.TrimPrefix(rest, `"`)
		if end := strings.Index(rest, `"`); end >= 0 {
			rest = rest[:end]
		}
	} else if sp := strings.IndexAny(rest, " \t"); sp >= 0 {
		rest = rest[:sp]
	}
	return rest == id
}

func (m *Manager) removeMatchingShortcuts(canonicalPath string, existing []ShortcutMeta) {
	seen := map[string]bool{}
	remove := func(p string) {
		if p == "" {
			return
		}
		k := strings.ToLower(p)
		if seen[k] {
			return
		}
		seen[k] = true
		_ = m.backend.RemoveShortcut(p)
		_ = m.backend.SetStartupApproved(filepath.Base(p), true)
	}
	for _, s := range existing {
		remove(s.Path)
	}
	remove(canonicalPath)
}

func (m *Manager) snapshotShortcuts() []ShortcutMeta {
	if !m.Supported() {
		return nil
	}
	sig := computeDirSignature(m.paths.StartupDir)
	m.mu.Lock()
	if sig != "" && sig == m.lastSig {
		cp := append([]ShortcutMeta(nil), m.cachedList...)
		m.mu.Unlock()
		return cp
	}
	m.mu.Unlock()

	if sig == "EMPTY" {
		m.mu.Lock()
		m.lastSig = sig
		m.cachedList = nil
		m.mu.Unlock()
		return nil
	}

	items, err := m.backend.ListShortcuts(m.paths.StartupDir)
	if err != nil {
		return nil
	}
	m.mu.Lock()
	m.lastSig = sig
	m.cachedList = append([]ShortcutMeta(nil), items...)
	m.mu.Unlock()
	return items
}

func (m *Manager) invalidateCache() {
	m.mu.Lock()
	m.lastSig = ""
	m.cachedList = nil
	m.mu.Unlock()
}

func computeDirSignature(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "EMPTY"
	}
	var parts []string
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".lnk") {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			parts = append(parts, e.Name())
			continue
		}
		parts = append(parts, fmt.Sprintf("%s|%d|%d", strings.ToLower(e.Name()), info.Size(), info.ModTime().UnixNano()))
	}
	if len(parts) == 0 {
		return "EMPTY"
	}
	sort.Strings(parts)
	return strings.Join(parts, ";")
}

func existingPath(meta *ShortcutMeta, fallback string) string {
	if meta != nil && strings.TrimSpace(meta.Path) != "" {
		return meta.Path
	}
	return fallback
}

func wscriptExePath() string {
	sysRoot := os.Getenv("SystemRoot")
	if sysRoot == "" {
		sysRoot = `C:\Windows`
	}
	cand := filepath.Join(sysRoot, "System32", "wscript.exe")
	if fileExists(cand) {
		return cand
	}
	return "wscript.exe"
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
