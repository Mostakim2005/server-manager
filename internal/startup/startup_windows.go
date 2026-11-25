//go:build windows

package startup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const startupApprovedSubkey = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\StartupFolder`

type osBackend struct{}

func (osBackend) Supported() bool { return true }

func (osBackend) ListShortcuts(dir string) ([]ShortcutMeta, error) {
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	psScript := fmt.Sprintf(
		`$sh=New-Object -ComObject WScript.Shell;$items=Get-ChildItem -LiteralPath '%s' -Filter '*.lnk' -File -ErrorAction SilentlyContinue | ForEach-Object { $s=$sh.CreateShortcut($_.FullName); [PSCustomObject]@{name=$_.Name;path=$_.FullName;target=$s.TargetPath;args=$s.Arguments;workDir=$s.WorkingDirectory;description=$s.Description} }; if ($null -eq $items) { '[]' } else { $items | ConvertTo-Json -Compress }`,
		escapePS(dir),
	)
	out, err := runHiddenPowerShell(psScript)
	if err != nil {
		return nil, fmt.Errorf("list startup shortcuts: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return parseShortcutListJSON(out)
}

func (osBackend) WriteShortcut(spec ShortcutSpec) error {
	if err := os.MkdirAll(filepath.Dir(spec.Path), 0o755); err != nil {
		return err
	}
	var iconStmt string
	if strings.TrimSpace(spec.IconLocation) != "" {
		iconStmt = fmt.Sprintf(`$s.IconLocation='%s';`, escapePS(spec.IconLocation))
	}
	psScript := fmt.Sprintf(
		`$s=(New-Object -ComObject WScript.Shell).CreateShortcut('%s');$s.TargetPath='%s';$s.Arguments='%s';$s.WorkingDirectory='%s';$s.Description='%s';%s$s.Save()`,
		escapePS(spec.Path),
		escapePS(spec.TargetPath),
		escapePS(spec.Arguments),
		escapePS(spec.WorkingDirectory),
		escapePS(spec.Description),
		iconStmt,
	)
	if out, err := runHiddenPowerShell(psScript); err != nil {
		return fmt.Errorf("write shortcut %s: %w (%s)", spec.Path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (osBackend) RemoveShortcut(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (osBackend) IsStartupApproved(shortcutName string) (bool, error) {
	if strings.TrimSpace(shortcutName) == "" {
		return true, nil
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, startupApprovedSubkey, registry.QUERY_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return true, nil
		}
		return true, err
	}
	defer key.Close()

	raw, _, err := key.GetBinaryValue(shortcutName)
	if err != nil {
		if err == registry.ErrNotExist {
			return true, nil
		}
		return true, err
	}
	return ParseStartupApprovedBytes(raw), nil
}

func (osBackend) SetStartupApproved(shortcutName string, enabled bool) error {
	if strings.TrimSpace(shortcutName) == "" {
		return nil
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, startupApprovedSubkey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}
	defer key.Close()

	if enabled {
		err = key.DeleteValue(shortcutName)
		if err == nil || err == registry.ErrNotExist {
			return nil
		}
		// Fallback to explicitly writing Windows' standard enabled marker (0x02 followed by zeros).
		return key.SetBinaryValue(shortcutName, []byte{2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	}
	return key.SetBinaryValue(shortcutName, []byte{3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
}

func (osBackend) OpenFolder(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return exec.Command("explorer.exe", dir).Start()
}

func parseShortcutListJSON(raw []byte) ([]ShortcutMeta, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var list []ShortcutMeta
		if err := json.Unmarshal(trimmed, &list); err != nil {
			return nil, err
		}
		return list, nil
	}
	var single ShortcutMeta
	if err := json.Unmarshal(trimmed, &single); err != nil {
		return nil, err
	}
	if single.Path == "" && single.Name == "" {
		return nil, nil
	}
	return []ShortcutMeta{single}, nil
}

func runHiddenPowerShell(script string) ([]byte, error) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	return cmd.CombinedOutput()
}

func escapePS(s string) string {
	return strings.ReplaceAll(s, `'`, `''`)
}
