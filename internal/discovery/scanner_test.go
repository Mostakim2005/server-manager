package discovery

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Mostakim2005/server-manager/internal/config"
)

func TestScanDiscoveryPairingAndConflicts(t *testing.T) {
	tmp := t.TempDir()
	projA := filepath.Join(tmp, "Alpha App")
	projB := filepath.Join(tmp, "Beta App")
	nm := filepath.Join(projA, "node_modules", "ignored")
	if err := os.MkdirAll(nm, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projB, 0o755); err != nil {
		t.Fatal(err)
	}

	_ = os.WriteFile(filepath.Join(projA, "start-server.bat"), []byte("@echo off\ntitle Alpha Hub\npython -m uvicorn app:app --port 8000\npause\n"), 0o600)
	_ = os.WriteFile(filepath.Join(projA, "stop-server.bat"), []byte("@echo off\ntaskkill /F /IM python.exe\n"), 0o600)
	_ = os.WriteFile(filepath.Join(projA, "gradlew.bat"), []byte("@echo off\necho gradle\n"), 0o600)
	_ = os.WriteFile(filepath.Join(nm, "start-fake.bat"), []byte("python -m http.server 9999"), 0o600)
	_ = os.WriteFile(filepath.Join(projB, "run-lan.bat"), []byte("@echo off\npython -m uvicorn b:app --host 0.0.0.0 --port 8000\n"), 0o600)

	paths := config.DiscoverPaths(t.TempDir())
	cfg := config.DefaultConfig(paths)
	cfg.ScanRoots = []config.ScanRoot{{ID: "tmp", Label: "Tmp", Path: tmp, Enabled: true, MaxDepth: 3}}

	entries := Scan(paths, cfg, nil)
	var foundAlpha, foundBeta, foundGradle, foundIgnored bool
	for _, e := range entries {
		switch e.ProjectName {
		case "Alpha App":
			if e.IsUtility {
				foundGradle = true
				continue
			}
			foundAlpha = true
			if e.StopScriptPath == "" {
				t.Fatalf("expected paired stop script for Alpha App")
			}
			if len(e.Ports) != 1 || e.Ports[0] != 8000 {
				t.Fatalf("expected port 8000 for Alpha App, got %v", e.Ports)
			}
			if len(e.Conflicts) == 0 {
				t.Fatalf("expected duplicate port conflict on Alpha App")
			}
		case "Beta App":
			foundBeta = true
			if e.BindHost != "0.0.0.0" {
				t.Fatalf("expected 0.0.0.0 bind host for Beta App")
			}
		case "ignored":
			foundIgnored = true
		}
	}
	if !foundAlpha || !foundBeta || !foundGradle {
		t.Fatalf("missing expected scripts: alpha=%v beta=%v gradle=%v", foundAlpha, foundBeta, foundGradle)
	}
	if foundIgnored {
		t.Fatalf("node_modules script should have been ignored")
	}
}

func TestSmartProjectGroupingAndWrapperDeduplication(t *testing.T) {
	tmp := t.TempDir()
	avDir := filepath.Join(tmp, "AV Manager")
	avDesktop := filepath.Join(avDir, "desktop")
	dtDir := filepath.Join(tmp, "DeepTutor")
	dtScripts := filepath.Join(dtDir, "scripts")
	rcDir := filepath.Join(tmp, "Retro Console Manager")
	for _, d := range []string{avDir, avDesktop, dtDir, dtScripts, rcDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// AV Manager: run.bat + run-lan.bat share :8000; PhysicsLab.bat is the project's Electron desktop wrapper (missing desktop/node_modules).
	_ = os.WriteFile(filepath.Join(avDir, "run.bat"), []byte("@echo off\npython -m uvicorn app.main:app --port 8000\n"), 0o600)
	_ = os.WriteFile(filepath.Join(avDir, "run-lan.bat"), []byte("@echo off\npython -m uvicorn app.main:app --host 0.0.0.0 --port 8000\n"), 0o600)
	_ = os.WriteFile(filepath.Join(avDesktop, "main.js"), []byte("// electron wrapper\n"), 0o600)
	_ = os.WriteFile(filepath.Join(avDesktop, "..", "PhysicsLab.bat"), []byte("@echo off\ntitle PhysicsLab Launcher\ncd /d \"%~dp0desktop\"\nnpx --no-install electron .\n"), 0o600)

	// SillyTavern-style YAML port config + collision detection.
	stDir := filepath.Join(tmp, "SillyApp")
	_ = os.MkdirAll(stDir, 0o755)
	_ = os.WriteFile(filepath.Join(stDir, "run-st.bat"), []byte("@echo off\nnode server.js\n"), 0o600)
	_ = os.WriteFile(filepath.Join(stDir, "config.yaml"), []byte("listen: false\nport: 8000\n"), 0o600)

	// DeepTutor: start.bat + start.ps1 duplicate -> start.ps1 hidden; start_backend/frontend share ports -> grouped.
	_ = os.WriteFile(filepath.Join(dtDir, "start.bat"), []byte("@echo off\npython -m deeptutor_cli.main start\nREM http://127.0.0.1:8001 http://127.0.0.1:3782\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dtDir, "start.ps1"), []byte("# start.ps1\npython -m deeptutor_cli.main start\n# http://127.0.0.1:8001 http://127.0.0.1:3782\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dtScripts, "start_backend.bat"), []byte("@echo off\nREM http://localhost:8001\npython -m deeptutor.api.run_server\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dtScripts, "start_frontend.bat"), []byte("@echo off\nnpm run dev -- -p 3782\n"), 0o600)

	// Retro Console Manager: .bat wraps .ps1 -> .ps1 marked HiddenDuplicate.
	_ = os.WriteFile(filepath.Join(rcDir, "retro-console-manager.ps1"), []byte("Add-Type -AssemblyName PresentationFramework\n"), 0o600)
	_ = os.WriteFile(filepath.Join(rcDir, "run-retro-console-manager.bat"), []byte("@echo off\npowershell -File \"%~dp0retro-console-manager.ps1\"\n"), 0o600)

	paths := config.DiscoverPaths(t.TempDir())
	cfg := config.DefaultConfig(paths)
	cfg.ScanRoots = []config.ScanRoot{{ID: "tmp", Label: "Tmp", Path: tmp, Enabled: true, MaxDepth: 3}}

	entries := Scan(paths, cfg, nil)
	byID := make(map[string]ServiceEntry, len(entries))
	for _, e := range entries {
		byID[e.ID] = e
	}

	if byID["av-manager-run"].GroupKey == "" || byID["av-manager-run"].GroupKey != byID["av-manager-run-lan"].GroupKey {
		t.Fatalf("expected AV Manager run.bat & run-lan.bat grouped, got %q vs %q", byID["av-manager-run"].GroupKey, byID["av-manager-run-lan"].GroupKey)
	}
	if byID["av-manager-physicslab"].GroupKey != byID["av-manager-run"].GroupKey || !byID["av-manager-physicslab"].MissingDeps {
		t.Fatalf("expected PhysicsLab.bat grouped under AV Manager and flagged MissingDeps, got group=%q missing=%v", byID["av-manager-physicslab"].GroupKey, byID["av-manager-physicslab"].MissingDeps)
	}
	stEntry := byID["sillyapp-run-st"]
	if len(stEntry.Ports) != 1 || stEntry.Ports[0] != 8000 || stEntry.WebURL != "http://127.0.0.1:8000" {
		t.Fatalf("expected YAML port 8000 & WebURL for SillyApp, got ports=%v url=%q", stEntry.Ports, stEntry.WebURL)
	}
	if !byID["deeptutor-start-2"].HiddenDuplicate {
		t.Fatalf("expected DeepTutor start.ps1 to be marked HiddenDuplicate")
	}
	dtGrp := byID["deeptutor-start"].GroupKey
	if dtGrp == "" || byID["deeptutor-start-backend"].GroupKey != dtGrp || byID["deeptutor-start-frontend"].GroupKey != dtGrp {
		t.Fatalf("expected DeepTutor start/backend/frontend grouped, got %q / %q / %q", dtGrp, byID["deeptutor-start-backend"].GroupKey, byID["deeptutor-start-frontend"].GroupKey)
	}
	if !byID["retro-console-manager-retro-console-manager"].HiddenDuplicate {
		t.Fatalf("expected retro-console-manager.ps1 wrapper target to be marked HiddenDuplicate")
	}

	// Ungrouping run-lan.bat leaves run.bat + PhysicsLab.bat grouped; ungrouping both clears singleton GroupKey while preserving AutoGroupKey.
	cfg.UngroupedIDs = []string{"av-manager-run-lan"}
	entries2 := Scan(paths, cfg, nil)
	byID2 := make(map[string]ServiceEntry, len(entries2))
	for _, e := range entries2 {
		byID2[e.ID] = e
	}
	if byID2["av-manager-run-lan"].GroupKey != "" || byID2["av-manager-run"].GroupKey == "" {
		t.Fatalf("expected ungrouped run-lan empty GroupKey and remaining pair still grouped")
	}
	cfg.UngroupedIDs = []string{"av-manager-run-lan", "av-manager-physicslab"}
	entries3 := Scan(paths, cfg, nil)
	byID3 := make(map[string]ServiceEntry, len(entries3))
	for _, e := range entries3 {
		byID3[e.ID] = e
	}
	if byID3["av-manager-run"].GroupKey != "" {
		t.Fatalf("expected singleton AV Manager entry to clear GroupKey, got %q", byID3["av-manager-run"].GroupKey)
	}
	if byID3["av-manager-run-lan"].AutoGroupKey == "" {
		t.Fatalf("expected AutoGroupKey preserved so user can rejoin later")
	}
}
