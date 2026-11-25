//go:build windows

package startup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mostakim2005/server-manager/internal/config"
	"github.com/Mostakim2005/server-manager/internal/discovery"
)

func TestWindowsRealShortcutRoundTripInTempDir(t *testing.T) {
	tmp := t.TempDir()
	startupDir := filepath.Join(tmp, "Startup")
	_ = os.MkdirAll(startupDir, 0o755)

	paths := config.DiscoverPaths(tmp)
	paths.StartupDir = startupDir

	mgr := NewManager(paths)
	entry := discovery.ServiceEntry{
		ID:         "sample-server-run",
		Name:       "Sample Server (run.bat)",
		ScriptPath: filepath.Join(tmp, "Sample Server", "run.bat"),
		WorkingDir: filepath.Join(tmp, "Sample Server"),
		StartCmd:   `cmd.exe /c call "run.bat"`,
	}

	stOn, err := mgr.SetServiceEnabled(entry, true)
	if err != nil {
		t.Fatalf("SetServiceEnabled(true) failed: %v", err)
	}
	if !stOn.Enabled || stOn.LaunchMode != "server-manager-hidden" {
		t.Fatalf("unexpected Windows startup status: %+v", stOn)
	}
	if _, err := os.Stat(stOn.ShortcutPath); err != nil {
		t.Fatalf("expected .lnk file on disk at %s: %v", stOn.ShortcutPath, err)
	}

	// Force fresh PowerShell single-item JSON scan to confirm single-.lnk unmarshaling works on Windows.
	mgr.invalidateCache()
	stFresh := mgr.ServiceStatus(entry)
	if !stFresh.Enabled || !strings.EqualFold(stFresh.ShortcutName, stOn.ShortcutName) {
		t.Fatalf("fresh scan did not match shortcut: %+v", stFresh)
	}

	stOff, err := mgr.SetServiceEnabled(entry, false)
	if err != nil {
		t.Fatalf("SetServiceEnabled(false) failed: %v", err)
	}
	if stOff.Enabled {
		t.Fatalf("expected shortcut disabled after removal: %+v", stOff)
	}
	if _, err := os.Stat(stOn.ShortcutPath); !os.IsNotExist(err) {
		t.Fatalf("expected .lnk removed from disk, stat err=%v", err)
	}
}
