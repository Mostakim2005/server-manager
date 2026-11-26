//go:build windows

package discovery

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type cachedLauncher struct {
	scriptPath string
	workingDir string
	startCmd   string
}

var installedLauncherCache sync.Map // slug -> cachedLauncher

func findInstalledLauncher(key, prettyName string) (scriptPath, workingDir, startCmd string) {
	slug := compactSlug(key)
	if slug == "" {
		slug = compactSlug(prettyName)
	}
	if len(slug) < 3 {
		return "", "", ""
	}
	if v, ok := installedLauncherCache.Load(slug); ok {
		cl := v.(cachedLauncher)
		return cl.scriptPath, cl.workingDir, cl.startCmd
	}

	res := scanProgramDirsForLauncher(slug)
	installedLauncherCache.Store(slug, res)
	return res.scriptPath, res.workingDir, res.startCmd
}

func scanProgramDirsForLauncher(slug string) cachedLauncher {
	roots := []string{
		os.Getenv("ProgramFiles"),
		os.Getenv("ProgramFiles(x86)"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs"),
	}
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, de := range entries {
			if !de.IsDir() {
				continue
			}
			dirSlug := compactSlug(de.Name())
			if !strings.Contains(dirSlug, slug) {
				continue
			}
			dirPath := filepath.Join(root, de.Name())
			if cl := pickLauncherInDir(dirPath, slug); cl.startCmd != "" {
				return cl
			}
		}
	}
	return cachedLauncher{}
}

func pickLauncherInDir(dirPath, slug string) cachedLauncher {
	items, err := os.ReadDir(dirPath)
	if err != nil {
		return cachedLauncher{}
	}
	var bestBat, bestExe string
	for _, it := range items {
		if it.IsDir() {
			continue
		}
		name := it.Name()
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "unins") || strings.Contains(lower, "uninstall") || strings.Contains(lower, "update") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		full := filepath.Join(dirPath, name)
		switch ext {
		case ".bat", ".cmd":
			if bestBat == "" || strings.Contains(lower, "launcher") || strings.Contains(lower, "start") {
				bestBat = full
			}
		case ".exe":
			if strings.Contains(compactSlug(name), slug) && bestExe == "" {
				bestExe = full
			}
		}
	}
	if bestBat != "" {
		return cachedLauncher{
			scriptPath: bestBat,
			workingDir: dirPath,
			startCmd:   buildScriptCommand(bestBat),
		}
	}
	if bestExe != "" {
		return cachedLauncher{
			workingDir: dirPath,
			startCmd:   `"` + bestExe + `"`,
		}
	}
	return cachedLauncher{}
}

func compactSlug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
