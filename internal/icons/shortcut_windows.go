//go:build windows

package icons

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

func findWindowsShortcutIcon(names []string) string {
	var searchDirs []string
	if appData := os.Getenv("APPDATA"); appData != "" {
		searchDirs = append(searchDirs, filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs"))
	}
	if progData := os.Getenv("ProgramData"); progData != "" {
		searchDirs = append(searchDirs, filepath.Join(progData, "Microsoft", "Windows", "Start Menu", "Programs"))
	}

	var normNames []string
	for _, n := range names {
		if s := sanitizeSlug(n); s != "" {
			normNames = append(normNames, s)
		}
	}
	if len(normNames) == 0 {
		return ""
	}

	var matchedLnk string
	for _, root := range searchDirs {
		if matchedLnk != "" {
			break
		}
		_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(d.Name()), ".lnk") {
				return nil
			}
			baseSlug := sanitizeSlug(strings.TrimSuffix(d.Name(), filepath.Ext(d.Name())))
			dirSlug := sanitizeSlug(filepath.Base(filepath.Dir(p)))
			for _, want := range normNames {
				if strings.Contains(baseSlug, want) || strings.Contains(dirSlug, want) {
					matchedLnk = p
					return filepath.SkipAll
				}
			}
			return nil
		})
	}
	if matchedLnk == "" {
		return ""
	}

	ps := "$s=(New-Object -ComObject WScript.Shell).CreateShortcut('" + strings.ReplaceAll(matchedLnk, "'", "''") + "'); $s.IconLocation; $s.TargetPath"
	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", ps)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n") {
		cand := strings.TrimSpace(line)
		if idx := strings.LastIndex(cand, ","); idx > 0 {
			cand = strings.TrimSpace(cand[:idx])
		}
		cand = os.ExpandEnv(strings.Trim(cand, `"'`))
		if cand == "" {
			continue
		}
		if info, serr := os.Stat(cand); serr == nil && !info.IsDir() {
			ext := strings.ToLower(filepath.Ext(cand))
			if ext == ".ico" || ext == ".png" || ext == ".svg" || ext == ".exe" {
				return cand
			}
		}
	}
	return ""
}
