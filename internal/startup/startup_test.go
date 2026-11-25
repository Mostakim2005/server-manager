package startup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mostakim2005/server-manager/internal/config"
	"github.com/Mostakim2005/server-manager/internal/discovery"
)

type fakeBackend struct {
	supported bool
	dir       string
	items     map[string]ShortcutMeta
	approved  map[string]bool
	openedDir string
}

func newFakeBackend(dir string) *fakeBackend {
	return &fakeBackend{
		supported: true,
		dir:       dir,
		items:     map[string]ShortcutMeta{},
		approved:  map[string]bool{},
	}
}

func (f *fakeBackend) Supported() bool { return f.supported }

func (f *fakeBackend) ListShortcuts(string) ([]ShortcutMeta, error) {
	out := make([]ShortcutMeta, 0, len(f.items))
	for _, v := range f.items {
		out = append(out, v)
	}
	return out, nil
}

func (f *fakeBackend) WriteShortcut(spec ShortcutSpec) error {
	name := filepath.Base(spec.Path)
	f.items[strings.ToLower(name)] = ShortcutMeta{
		Name:             name,
		Path:             spec.Path,
		TargetPath:       spec.TargetPath,
		Arguments:        spec.Arguments,
		WorkingDirectory: spec.WorkingDirectory,
		Description:      spec.Description,
	}
	return os.WriteFile(spec.Path, []byte("lnk-stub"), 0o600)
}

func (f *fakeBackend) RemoveShortcut(path string) error {
	delete(f.items, strings.ToLower(filepath.Base(path)))
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (f *fakeBackend) IsStartupApproved(shortcutName string) (bool, error) {
	if v, ok := f.approved[strings.ToLower(shortcutName)]; ok {
		return v, nil
	}
	return true, nil
}

func (f *fakeBackend) SetStartupApproved(shortcutName string, enabled bool) error {
	if enabled {
		delete(f.approved, strings.ToLower(shortcutName))
	} else {
		f.approved[strings.ToLower(shortcutName)] = false
	}
	return nil
}

func (f *fakeBackend) OpenFolder(dir string) error {
	f.openedDir = dir
	return nil
}

func TestSanitizeAndPreferredNames(t *testing.T) {
	clip := discovery.ServiceEntry{ID: "cli-proxy-api-run-cliproxy", Name: ".cli-proxy-api (run-cliproxy.bat)"}
	if got := PreferredShortcutName(clip); got != "CLIProxyAPI.lnk" {
		t.Fatalf("expected CLIProxyAPI.lnk, got %q", got)
	}

	weird := discovery.ServiceEntry{ID: "my-svc", Name: `..Bad:Name/With*Chars?<>|`}
	if got := PreferredShortcutName(weird); got != "Bad-Name-WithChars---.lnk" {
		t.Fatalf("unexpected sanitized shortcut name: %q", got)
	}

	reserved := discovery.ServiceEntry{ID: "custom-sm-2", Name: "Server Manager"}
	if got := PreferredShortcutName(reserved); got == SelfShortcutName {
		t.Fatalf("service shortcut must not collide with %q", SelfShortcutName)
	}
}

func TestParseStartupApprovedBytes(t *testing.T) {
	if !ParseStartupApprovedBytes(nil) {
		t.Fatalf("empty bytes should default to approved")
	}
	if !ParseStartupApprovedBytes([]byte{2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("0x02 should be approved")
	}
	if !ParseStartupApprovedBytes([]byte{6, 0, 0, 0}) {
		t.Fatalf("0x06 should be approved")
	}
	if ParseStartupApprovedBytes([]byte{3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("0x03 should be marked disabled")
	}
	if ParseStartupApprovedBytes([]byte{1, 0, 0, 0}) {
		t.Fatalf("0x01 should be marked disabled")
	}
}

func TestFindHiddenVBSAndStartupLifecycle(t *testing.T) {
	tmp := t.TempDir()
	startupDir := filepath.Join(tmp, "Startup")
	_ = os.MkdirAll(startupDir, 0o755)
	clipDir := filepath.Join(tmp, ".cli-proxy-api")
	_ = os.MkdirAll(clipDir, 0o755)
	vbsPath := filepath.Join(clipDir, "start-cliproxy-hidden.vbs")
	_ = os.WriteFile(vbsPath, []byte("WScript.Sleep 1"), 0o600)

	paths := config.DiscoverPaths(tmp)
	paths.StartupDir = startupDir

	fb := newFakeBackend(startupDir)
	mgr := newManagerWithBackend(paths, fb)

	clipEntry := discovery.ServiceEntry{
		ID:         "cli-proxy-api-run-cliproxy",
		Name:       ".cli-proxy-api (run-cliproxy.bat)",
		ScriptPath: filepath.Join(clipDir, "run-cliproxy.bat"),
		WorkingDir: clipDir,
		StartCmd:   `cmd.exe /c call "run-cliproxy.bat"`,
	}
	appEntry := discovery.ServiceEntry{
		ID:         "deeptutor-start",
		Name:       "DeepTutor (start.bat)",
		ScriptPath: filepath.Join(tmp, "DeepTutor", "start.bat"),
		WorkingDir: filepath.Join(tmp, "DeepTutor"),
		StartCmd:   `cmd.exe /c call "start.bat"`,
	}
	noCmdEntry := discovery.ServiceEntry{
		ID:   "reg-external",
		Name: "External Service",
	}

	// Pre-existing CLIProxyAPI.lnk with Task Manager disabled state should be adopted and flagged.
	existingPath := filepath.Join(startupDir, "CLIProxyAPI.lnk")
	_ = fb.WriteShortcut(ShortcutSpec{
		Path:             existingPath,
		TargetPath:       `C:\Windows\System32\wscript.exe`,
		Arguments:        `"` + vbsPath + `"`,
		WorkingDirectory: clipDir,
	})
	_ = fb.SetStartupApproved("CLIProxyAPI.lnk", false)

	snap, selfSt := mgr.Snapshot([]discovery.ServiceEntry{clipEntry, appEntry, noCmdEntry})
	if selfSt.Enabled {
		t.Fatalf("expected self startup disabled initially")
	}
	if !snap[clipEntry.ID].Enabled || !snap[clipEntry.ID].TaskManagerDisabled || snap[clipEntry.ID].LaunchMode != "vbs-hidden" {
		t.Fatalf("unexpected adopted cliproxy status: %+v", snap[clipEntry.ID])
	}
	if snap[noCmdEntry.ID].CanToggle {
		t.Fatalf("expected entry without command not toggleable")
	}

	// Re-enabling CLIProxyAPI clears Task Manager disabled override and keeps vbs-hidden launch.
	reSt, err := mgr.SetServiceEnabled(clipEntry, true)
	if err != nil {
		t.Fatalf("SetServiceEnabled(clip, true) failed: %v", err)
	}
	if !reSt.Enabled || reSt.TaskManagerDisabled || !reSt.Approved {
		t.Fatalf("expected cleared TaskManagerDisabled after re-enable, got %+v", reSt)
	}

	// Enabling a regular .bat service creates a --launch-service hidden shortcut.
	appSt, err := mgr.SetServiceEnabled(appEntry, true)
	if err != nil {
		t.Fatalf("SetServiceEnabled(app, true) failed: %v", err)
	}
	if !appSt.Enabled || appSt.LaunchMode != "server-manager-hidden" {
		t.Fatalf("unexpected app startup status: %+v", appSt)
	}
	written := fb.items[strings.ToLower(appSt.ShortcutName)]
	if !strings.Contains(written.Arguments, `--launch-service "deeptutor-start"`) {
		t.Fatalf("expected --launch-service arg, got %q", written.Arguments)
	}

	// Self startup toggle creates and removes Server Manager.lnk (--tray).
	selfOn, err := mgr.SetSelfEnabled(true)
	if err != nil || !selfOn.Enabled || selfOn.ShortcutName != SelfShortcutName {
		t.Fatalf("SetSelfEnabled(true) failed: %+v err=%v", selfOn, err)
	}
	selfOff, err := mgr.SetSelfEnabled(false)
	if err != nil || selfOff.Enabled {
		t.Fatalf("SetSelfEnabled(false) failed: %+v err=%v", selfOff, err)
	}

	// Disabling service removes shortcut.
	appOff, err := mgr.SetServiceEnabled(appEntry, false)
	if err != nil || appOff.Enabled {
		t.Fatalf("SetServiceEnabled(app, false) failed: %+v err=%v", appOff, err)
	}
	if _, err := mgr.SetServiceEnabled(noCmdEntry, true); err == nil {
		t.Fatalf("expected error enabling service with empty StartCmd/ScriptPath")
	}
}
