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

func TestUtilityHelpersAndCamelCaseRegistryMatching(t *testing.T) {
	tmp := t.TempDir()
	avDir := filepath.Join(tmp, "AV Manager")
	stDir := filepath.Join(tmp, "SillyTavern")
	miscDir := filepath.Join(tmp, "Misc App")
	for _, d := range []string{avDir, stDir, miscDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	_ = os.WriteFile(filepath.Join(avDir, "run.bat"), []byte("@echo off\npython -m uvicorn app.main:app --port 8000\n"), 0o600)
	_ = os.WriteFile(filepath.Join(avDir, "allow-lan-firewall.bat"), []byte("@echo off\npowershell -Command \"Start-Process powershell -ArgumentList '-Command New-NetFirewallRule -LocalPort 8000'\"\necho restart AV Manager\n"), 0o600)
	_ = os.WriteFile(filepath.Join(avDir, "enable-usb-sync.bat"), []byte("@echo off\necho Use http://127.0.0.1:8000\nadb reverse tcp:8000 tcp:8000\n"), 0o600)
	_ = os.WriteFile(filepath.Join(stDir, "run-st.bat"), []byte("@echo off\nnode server.js\n"), 0o600)
	_ = os.WriteFile(filepath.Join(stDir, "config.yaml"), []byte("listen: false\nport: 8005\n"), 0o600)
	_ = os.WriteFile(filepath.Join(miscDir, "notes-check.bat"), []byte("@echo off\necho Service not running; please restart manually.\n"), 0o600)

	paths := config.DiscoverPaths(t.TempDir())
	cfg := config.DefaultConfig(paths)
	cfg.ScanRoots = []config.ScanRoot{{ID: "tmp", Label: "Tmp", Path: tmp, Enabled: true, MaxDepth: 3}}
	reg := &config.EcosystemRegistry{
		Apps: map[string]map[string]any{
			"avManager":   {"port": float64(8000)},
			"sillyTavern": {"port": float64(8005)},
		},
	}

	entries := Scan(paths, cfg, reg)
	byID := make(map[string]ServiceEntry, len(entries))
	for _, e := range entries {
		byID[e.ID] = e
	}

	if !byID["av-manager-allow-lan-firewall"].IsUtility || !byID["av-manager-enable-usb-sync"].IsUtility {
		t.Fatalf("expected firewall/usb helper scripts to be classified as utilities: fw=%v usb=%v",
			byID["av-manager-allow-lan-firewall"].IsUtility, byID["av-manager-enable-usb-sync"].IsUtility)
	}
	if !byID["misc-app-notes-check"].IsUtility {
		t.Fatalf("expected english substring 'running'/'restart' not to misclassify helper script as server")
	}
	for _, id := range []string{"av-manager-run", "sillytavern-run-st"} {
		for _, c := range byID[id].Conflicts {
			if c.Code == "DUPLICATE_PORT" || c.Code == "REGISTRY_PORT_MISMATCH" {
				t.Fatalf("unexpected conflict on %s: %+v", id, c)
			}
		}
	}

	// If SillyTavern YAML drifts to 8000 while camelCase sillyTavern expects 8005, both duplicate & mismatch should fire.
	_ = os.WriteFile(filepath.Join(stDir, "config.yaml"), []byte("port: 8000\n"), 0o600)
	entriesDrift := Scan(paths, cfg, reg)
	var sawMismatch bool
	for _, e := range entriesDrift {
		if e.ID == "sillytavern-run-st" {
			for _, c := range e.Conflicts {
				if c.Code == "REGISTRY_PORT_MISMATCH" {
					sawMismatch = true
				}
			}
		}
	}
	if !sawMismatch {
		t.Fatalf("expected camelCase sillyTavern registry key to match SillyTavern project and flag port mismatch")
	}
}

func TestMultiEndpointRegistryPortsAndIdempotentConflicts(t *testing.T) {
	tmp := t.TempDir()
	dtDir := filepath.Join(tmp, "DeepTutor")
	dtScripts := filepath.Join(dtDir, "scripts")
	dmDir := filepath.Join(tmp, "Doujin Manager")
	for _, d := range []string{dtDir, dtScripts, dmDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	_ = os.WriteFile(filepath.Join(dtDir, "start.bat"), []byte("@echo off\npython -m deeptutor_cli.main start\nREM http://127.0.0.1:8001 http://localhost:3782\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dtScripts, "start_backend.bat"), []byte("@echo off\necho http://localhost:8001\npython -m deeptutor.api.run_server\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dtScripts, "start_frontend.bat"), []byte("@echo off\nnpm run dev -- -p 3782\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dtScripts, "start_bad.bat"), []byte("@echo off\npython -m uvicorn api:app --port 9099\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dmDir, "run-bad.bat"), []byte("@echo off\npython -m uvicorn dm:app --port 4567\n"), 0o600)

	paths := config.DiscoverPaths(t.TempDir())
	cfg := config.DefaultConfig(paths)
	cfg.ScanRoots = []config.ScanRoot{{ID: "tmp", Label: "Tmp", Path: tmp, Enabled: true, MaxDepth: 3}}
	reg := &config.EcosystemRegistry{
		Apps: map[string]map[string]any{
			"deepTutor": {
				"url":    "http://127.0.0.1:8001",
				"webUrl": "http://127.0.0.1:3782",
			},
			"doujinManager": {
				"url":         "http://127.0.0.1:8780",
				"suwayomiUrl": "http://127.0.0.1:4567",
			},
		},
		Services: map[string]map[string]any{
			"deepTutor": {
				"url":    "http://127.0.0.1:8001",
				"webUrl": "http://127.0.0.1:3782",
			},
		},
	}

	entries := Scan(paths, cfg, reg)
	// Calling DetectConflicts again (as server.Daemon.Rescan does) must not duplicate warnings.
	DetectConflicts(entries, reg)

	byID := make(map[string]ServiceEntry, len(entries))
	for _, e := range entries {
		byID[e.ID] = e
	}

	for _, okID := range []string{"deeptutor-start", "deeptutor-start-backend", "deeptutor-start-frontend"} {
		for _, c := range byID[okID].Conflicts {
			if c.Code == "REGISTRY_PORT_MISMATCH" {
				t.Fatalf("unexpected registry mismatch on %s: %+v", okID, c)
			}
		}
	}

	checkSingleMismatch := func(id string) {
		t.Helper()
		var count int
		for _, c := range byID[id].Conflicts {
			if c.Code == "REGISTRY_PORT_MISMATCH" {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("expected exactly 1 REGISTRY_PORT_MISMATCH on %s, got %d (%+v)", id, count, byID[id].Conflicts)
		}
	}
	checkSingleMismatch("deeptutor-start-bad")
	checkSingleMismatch("doujin-manager-run-bad")
}
