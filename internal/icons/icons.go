package icons

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Mostakim2005/server-manager/internal/config"
	"github.com/Mostakim2005/server-manager/internal/discovery"
)

const (
	maxLocalIconBytes  = 2 * 1024 * 1024
	maxRemoteIconBytes = 1024 * 1024
)

// ResolvedIcon holds served icon image bytes and metadata for one service.
type ResolvedIcon struct {
	Data        []byte
	ContentType string
	Source      string // "custom" | "local" | "exe" | "favicon"
	Version     string
}

var (
	strictIconFileRe = regexp.MustCompile(`(?i)^(?:favicon|logo|drawlogo-color|app[-_]?icon|icon(?:[-_][a-z0-9]+)?|mark)\.(svg|png|ico)$`)
	linkTagRe        = regexp.MustCompile(`(?is)<link\b[^>]*>`)
	relAttrRe        = regexp.MustCompile(`(?is)\brel\s*=\s*["']([^"']*)["']`)
	hrefAttrRe       = regexp.MustCompile(`(?is)\bhref\s*=\s*["']([^"']*)["']`)
	exePathQuotedRe  = regexp.MustCompile(`"([^"]+\.exe)"`)
	exePathBareRe    = regexp.MustCompile(`(?i)([A-Za-z]:\\[^\s"']+\.exe)`)
)

var genericHelperExes = map[string]struct{}{
	"cmd.exe": {}, "powershell.exe": {}, "pwsh.exe": {}, "python.exe": {},
	"pythonw.exe": {}, "node.exe": {}, "npm.exe": {}, "npx.exe": {},
	"uv.exe": {}, "wscript.exe": {}, "cscript.exe": {}, "rundll32.exe": {},
	"explorer.exe": {}, "taskkill.exe": {}, "conhost.exe": {},
}

var candidateAssetDirs = []string{
	"",
	"public",
	filepath.Join("public", "img"),
	"static",
	"assets",
	filepath.Join("src", "assets"),
	"resources",
	filepath.Join("web", "public"),
	filepath.Join("app", "static"),
	filepath.Join("apps", "desktop", "assets"),
	filepath.Join("desktop", "assets"),
	filepath.Join("extension", "assets"),
	filepath.Join("out", "renderer"),
	filepath.Join("out", "renderer", "assets"),
	filepath.Join("drawio", "src", "main", "webapp", "images"),
	filepath.Join("internal", "webui", "static"),
}

var exactIconBasenames = []string{
	"favicon.svg", "logo.svg", "drawlogo-color.svg", "icon.svg", "mark.svg",
	"favicon.png", "logo.png", "mark.png", "icon-192.png", "icon-128.png",
	"icon128.png", "apple-touch-icon.png", "icon.png", "app-icon.png",
	"favicon.ico", "icon.ico", "app.ico", "app_icon.ico",
}

// Manager resolves, caches, and serves service icons safely from loopback.
type Manager struct {
	cacheDir   string
	httpClient *http.Client
	mu         sync.RWMutex
	resolved   map[string]ResolvedIcon
	inFlight   map[string]bool
}

// NewManager constructs an icon manager using cacheDir for extracted/fetched icons.
func NewManager(cacheDir string) *Manager {
	return &Manager{
		cacheDir: cacheDir,
		httpClient: &http.Client{
			Timeout: 1200 * time.Millisecond,
		},
		resolved: make(map[string]ResolvedIcon),
		inFlight: make(map[string]bool),
	}
}

// Refresh resolves local/cached icons for all discovered services and returns lookup metadata.
func (m *Manager) Refresh(services []discovery.ServiceEntry, cfg config.AppConfig, reg *config.EcosystemRegistry) map[string]ResolvedIcon {
	customByID := make(map[string]string, len(cfg.CustomServices))
	for _, cs := range cfg.CustomServices {
		if strings.TrimSpace(cs.IconPath) != "" {
			customByID[cs.ID] = strings.TrimSpace(cs.IconPath)
		}
	}

	next := make(map[string]ResolvedIcon, len(services))
	for _, s := range services {
		override := strings.TrimSpace(cfg.IconOverrides[s.ID])
		if override == "" && s.GroupKey != "" {
			override = strings.TrimSpace(cfg.IconOverrides[s.GroupKey])
		}
		if override == "" {
			override = customByID[s.ID]
		}
		if icon, ok := m.resolveServiceSync(s, override, reg); ok {
			next[s.ID] = icon
		}
	}

	m.mu.Lock()
	m.resolved = next
	m.mu.Unlock()
	return next
}

// Get returns the currently resolved icon for serviceID, if any.
func (m *Manager) Get(serviceID string) (ResolvedIcon, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ic, ok := m.resolved[serviceID]
	return ic, ok
}

// ApplyCustomOverride synchronously resolves and validates a user-provided local path, .exe, or URL override.
func (m *Manager) ApplyCustomOverride(ctx context.Context, entry discovery.ServiceEntry, rawTarget string) (ResolvedIcon, error) {
	target := strings.TrimSpace(rawTarget)
	if target == "" {
		return ResolvedIcon{}, fmt.Errorf("empty icon target")
	}
	if isHTTPURL(target) {
		ic, err := m.fetchAndCacheURL(ctx, "override-"+sanitizeCacheKey(entry.ID), target, "custom")
		if err != nil {
			return ResolvedIcon{}, err
		}
		m.mu.Lock()
		m.resolved[entry.ID] = ic
		m.mu.Unlock()
		return ic, nil
	}
	ic, ok := m.resolveLocalOrExeTarget(entry.WorkingDir, target, "custom")
	if !ok {
		return ResolvedIcon{}, fmt.Errorf("could not read icon from %q (supported: .svg, .png, .ico, .webp, .jpg, .exe)", target)
	}
	m.mu.Lock()
	m.resolved[entry.ID] = ic
	m.mu.Unlock()
	return ic, nil
}

// ProbeWebFaviconAsync fetches and caches a running loopback Web UI's favicon without blocking API callers.
func (m *Manager) ProbeWebFaviconAsync(entry discovery.ServiceEntry, onCached func(serviceID string, ic ResolvedIcon)) {
	if strings.TrimSpace(entry.WebURL) == "" || !isLoopbackURL(entry.WebURL) {
		return
	}
	m.mu.Lock()
	if existing, ok := m.resolved[entry.ID]; ok && existing.Source != "" {
		m.mu.Unlock()
		return
	}
	if m.inFlight[entry.ID] {
		m.mu.Unlock()
		return
	}
	m.inFlight[entry.ID] = true
	m.mu.Unlock()

	go func() {
		defer func() {
			m.mu.Lock()
			delete(m.inFlight, entry.ID)
			m.mu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 2200*time.Millisecond)
		defer cancel()
		ic, ok := m.probeLoopbackFavicon(ctx, entry)
		if !ok {
			return
		}
		m.mu.Lock()
		if _, already := m.resolved[entry.ID]; !already {
			m.resolved[entry.ID] = ic
		} else {
			ic = m.resolved[entry.ID]
		}
		m.mu.Unlock()
		if onCached != nil {
			onCached(entry.ID, ic)
		}
	}()
}

func (m *Manager) resolveServiceSync(s discovery.ServiceEntry, override string, reg *config.EcosystemRegistry) (ResolvedIcon, bool) {
	if override != "" {
		if isHTTPURL(override) {
			if ic, ok := m.loadCachedByPrefix("override-" + sanitizeCacheKey(s.ID)); ok {
				return ic, true
			}
			// Best-effort quick sync fetch only for loopback URLs; remote URLs should be fetched when applied.
			if isLoopbackURL(override) {
				ctx, cancel := context.WithTimeout(context.Background(), 900*time.Millisecond)
				defer cancel()
				if ic, err := m.fetchAndCacheURL(ctx, "override-"+sanitizeCacheKey(s.ID), override, "custom"); err == nil {
					return ic, true
				}
			}
		} else if ic, ok := m.resolveLocalOrExeTarget(s.WorkingDir, override, "custom"); ok {
			return ic, true
		}
	}

	for _, root := range candidateProjectRoots(s, reg) {
		if path := findProjectIconFile(root); path != "" {
			if ic, ok := readIconFile(path, "local"); ok {
				return ic, true
			}
		}
	}

	if exePath := findExecutableTarget(s, reg); exePath != "" {
		if ic, ok := m.resolveExeCached(exePath, "exe"); ok {
			return ic, true
		}
	}

	if ic, ok := m.loadCachedByPrefix("svc-" + sanitizeCacheKey(s.ID)); ok {
		return ic, true
	}
	return ResolvedIcon{}, false
}

func (m *Manager) resolveLocalOrExeTarget(workingDir, target, source string) (ResolvedIcon, bool) {
	resolved := strings.Trim(strings.TrimSpace(target), `"'`)
	if resolved == "" {
		return ResolvedIcon{}, false
	}
	if !filepath.IsAbs(resolved) && strings.TrimSpace(workingDir) != "" {
		resolved = filepath.Join(workingDir, resolved)
	}
	resolved = filepath.Clean(resolved)
	ext := strings.ToLower(filepath.Ext(resolved))
	if ext == ".exe" || ext == ".dll" {
		return m.resolveExeCached(resolved, source)
	}
	return readIconFile(resolved, source)
}

func (m *Manager) resolveExeCached(exePath, source string) (ResolvedIcon, bool) {
	st, err := os.Stat(exePath)
	if err != nil || st.IsDir() {
		return ResolvedIcon{}, false
	}
	key := hashHex(fmt.Sprintf("%s|%d|%d", strings.ToLower(exePath), st.Size(), st.ModTime().Unix()))
	cacheName := "exe-" + key + ".png"
	if m.cacheDir != "" {
		cachePath := filepath.Join(m.cacheDir, cacheName)
		if ic, ok := readIconFile(cachePath, source); ok {
			return ic, true
		}
	}
	pngBytes, err := extractExecutableIconPNG(exePath)
	if err != nil || len(pngBytes) == 0 {
		return ResolvedIcon{}, false
	}
	if m.cacheDir != "" {
		_ = config.WriteAtomicNoBOM(filepath.Join(m.cacheDir, cacheName), pngBytes, 0o600)
	}
	return newResolvedIcon(pngBytes, "image/png", source), true
}

func (m *Manager) probeLoopbackFavicon(ctx context.Context, entry discovery.ServiceEntry) (ResolvedIcon, bool) {
	base, err := url.Parse(entry.WebURL)
	if err != nil || base.Host == "" {
		return ResolvedIcon{}, false
	}
	candidates := make([]string, 0, 6)
	if htmlLinks := m.discoverFaviconLinksFromHTML(ctx, entry.WebURL); len(htmlLinks) > 0 {
		candidates = append(candidates, htmlLinks...)
	}
	rootBase := fmt.Sprintf("%s://%s", base.Scheme, base.Host)
	candidates = append(candidates,
		rootBase+"/favicon.svg",
		rootBase+"/favicon.png",
		rootBase+"/favicon.ico",
		rootBase+"/apple-touch-icon.png",
	)

	seen := map[string]bool{}
	prefix := "svc-" + sanitizeCacheKey(entry.ID)
	for _, cand := range candidates {
		if cand == "" || seen[cand] || !isLoopbackURL(cand) {
			continue
		}
		seen[cand] = true
		if ic, err := m.fetchAndCacheURL(ctx, prefix, cand, "favicon"); err == nil {
			return ic, true
		}
	}
	return ResolvedIcon{}, false
}

func (m *Manager) discoverFaviconLinksFromHTML(ctx context.Context, pageURL string) []string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil
	}
	var out []string
	for _, tag := range linkTagRe.FindAllString(string(raw), 24) {
		relM := relAttrRe.FindStringSubmatch(tag)
		if len(relM) < 2 || !strings.Contains(strings.ToLower(relM[1]), "icon") {
			continue
		}
		hrefM := hrefAttrRe.FindStringSubmatch(tag)
		if len(hrefM) < 2 {
			continue
		}
		href := strings.TrimSpace(hrefM[1])
		if href == "" || strings.HasPrefix(strings.ToLower(href), "data:") {
			continue
		}
		ref, uerr := url.Parse(href)
		if uerr != nil {
			continue
		}
		resolved := base.ResolveReference(ref).String()
		if isLoopbackURL(resolved) {
			out = append(out, resolved)
		}
	}
	return out
}

func (m *Manager) fetchAndCacheURL(ctx context.Context, cachePrefix, targetURL, source string) (ResolvedIcon, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return ResolvedIcon{}, err
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return ResolvedIcon{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ResolvedIcon{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRemoteIconBytes+1))
	if err != nil {
		return ResolvedIcon{}, err
	}
	if len(raw) == 0 || len(raw) > maxRemoteIconBytes {
		return ResolvedIcon{}, fmt.Errorf("icon empty or too large")
	}
	ct, ext, ok := detectImageType(raw, resp.Header.Get("Content-Type"), targetURL)
	if !ok {
		return ResolvedIcon{}, fmt.Errorf("response is not a valid image")
	}
	if m.cacheDir != "" {
		_ = m.writeCachedFile(cachePrefix, ext, raw)
	}
	return newResolvedIcon(raw, ct, source), nil
}

func (m *Manager) loadCachedByPrefix(prefix string) (ResolvedIcon, bool) {
	if m.cacheDir == "" {
		return ResolvedIcon{}, false
	}
	for _, ext := range []string{".svg", ".png", ".ico", ".webp", ".jpg"} {
		p := filepath.Join(m.cacheDir, prefix+ext)
		if ic, ok := readIconFile(p, cachedSourceFromPrefix(prefix)); ok {
			return ic, true
		}
	}
	return ResolvedIcon{}, false
}

func (m *Manager) writeCachedFile(prefix, ext string, data []byte) error {
	for _, oldExt := range []string{".svg", ".png", ".ico", ".webp", ".jpg"} {
		if oldExt != ext {
			_ = os.Remove(filepath.Join(m.cacheDir, prefix+oldExt))
		}
	}
	return config.WriteAtomicNoBOM(filepath.Join(m.cacheDir, prefix+ext), data, 0o600)
}

func cachedSourceFromPrefix(prefix string) string {
	if strings.HasPrefix(prefix, "override-") {
		return "custom"
	}
	return "favicon"
}

func candidateProjectRoots(s discovery.ServiceEntry, reg *config.EcosystemRegistry) []string {
	var roots []string
	seen := map[string]bool{}
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		p = filepath.Clean(p)
		k := strings.ToLower(p)
		if !seen[k] {
			seen[k] = true
			roots = append(roots, p)
		}
	}
	add(s.WorkingDir)
	if s.ScriptPath != "" {
		dir := filepath.Dir(s.ScriptPath)
		add(dir)
		base := strings.ToLower(filepath.Base(dir))
		parent := filepath.Dir(dir)
		// Climb one level when launcher scripts sit in a nested subfolder (scripts/, tools/, local-service/, desktop/).
		if base == "scripts" || base == "tools" || base == "local-service" || base == "desktop" || base == "electron" || strings.EqualFold(filepath.Base(parent), s.ProjectName) {
			add(parent)
			if strings.EqualFold(filepath.Base(filepath.Dir(parent)), s.ProjectName) {
				add(filepath.Dir(parent))
			}
		}
	}
	if reg != nil {
		slug := sanitizeSlug(s.ProjectName)
		for k, meta := range reg.Apps {
			if sanitizeSlug(k) == slug {
				if repo, _ := meta["repo"].(string); repo != "" {
					add(repo)
				}
			}
		}
	}
	return roots
}

func findProjectIconFile(root string) string {
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		return ""
	}
	for _, relDir := range candidateAssetDirs {
		dir := root
		if relDir != "" {
			dir = filepath.Join(root, relDir)
		}
		for _, base := range exactIconBasenames {
			cand := filepath.Join(dir, base)
			if info, serr := os.Stat(cand); serr == nil && !info.IsDir() && info.Size() > 0 && info.Size() <= maxLocalIconBytes {
				return cand
			}
		}
	}
	// Shallow fallback only inside known asset directories, strictly matched to icon filenames.
	for _, relDir := range []string{"assets", "public", "static", "resources", filepath.Join("src", "assets"), filepath.Join("out", "renderer", "assets")} {
		dir := filepath.Join(root, relDir)
		entries, derr := os.ReadDir(dir)
		if derr != nil {
			continue
		}
		for _, de := range entries {
			if de.IsDir() {
				continue
			}
			if strictIconFileRe.MatchString(de.Name()) {
				return filepath.Join(dir, de.Name())
			}
		}
	}
	return ""
}

func findExecutableTarget(s discovery.ServiceEntry, reg *config.EcosystemRegistry) string {
	checkCandidate := func(p string) string {
		p = strings.Trim(strings.TrimSpace(p), `"'`)
		if p == "" || !strings.EqualFold(filepath.Ext(p), ".exe") {
			return ""
		}
		base := strings.ToLower(filepath.Base(p))
		if _, skip := genericHelperExes[base]; skip {
			return ""
		}
		if !filepath.IsAbs(p) && s.WorkingDir != "" {
			p = filepath.Join(s.WorkingDir, p)
		}
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
		return ""
	}
	if reg != nil {
		slug := sanitizeSlug(s.ProjectName)
		for k, meta := range reg.Apps {
			if sanitizeSlug(k) == slug {
				if exe, _ := meta["executable"].(string); exe != "" {
					if hit := checkCandidate(exe); hit != "" {
						return hit
					}
				}
			}
		}
	}
	for _, m := range exePathQuotedRe.FindAllStringSubmatch(s.StartCmd, -1) {
		if hit := checkCandidate(m[1]); hit != "" {
			return hit
		}
	}
	for _, m := range exePathBareRe.FindAllStringSubmatch(s.StartCmd, -1) {
		if hit := checkCandidate(m[1]); hit != "" {
			return hit
		}
	}
	if fields := strings.Fields(s.StartCmd); len(fields) > 0 {
		if hit := checkCandidate(fields[0]); hit != "" {
			return hit
		}
	}
	return ""
}

func readIconFile(path, source string) (ResolvedIcon, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() <= 0 || info.Size() > maxLocalIconBytes {
		return ResolvedIcon{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ResolvedIcon{}, false
	}
	ct, _, ok := detectImageType(raw, "", path)
	if !ok {
		return ResolvedIcon{}, false
	}
	return newResolvedIcon(raw, ct, source), true
}

func detectImageType(data []byte, contentType, nameOrURL string) (ct, ext string, ok bool) {
	if len(data) == 0 {
		return "", "", false
	}
	if len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}) {
		return "image/png", ".png", true
	}
	if len(data) >= 4 && data[0] == 0x00 && data[1] == 0x00 && data[2] == 0x01 && data[3] == 0x00 {
		return "image/x-icon", ".ico", true
	}
	if len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")) {
		return "image/webp", ".webp", true
	}
	if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg", ".jpg", true
	}
	if len(data) >= 6 && (bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a"))) {
		return "image/gif", ".gif", true
	}
	headLen := len(data)
	if headLen > 512 {
		headLen = 512
	}
	headLower := strings.ToLower(strings.TrimSpace(string(data[:headLen])))
	if strings.Contains(headLower, "<svg") && !strings.Contains(headLower, "<html") {
		return "image/svg+xml; charset=utf-8", ".svg", true
	}
	if strings.Contains(strings.ToLower(contentType), "image/svg") && !strings.Contains(headLower, "<html") {
		return "image/svg+xml; charset=utf-8", ".svg", true
	}
	_ = nameOrURL
	return "", "", false
}

func newResolvedIcon(data []byte, contentType, source string) ResolvedIcon {
	return ResolvedIcon{
		Data:        data,
		ContentType: contentType,
		Source:      source,
		Version:     hashHex(string(data))[:10],
	}
}

func isHTTPURL(s string) bool {
	l := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://")
}

func isLoopbackURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func sanitizeCacheKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "svc"
	}
	return out
}

func sanitizeSlug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func hashHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
