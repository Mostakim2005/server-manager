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
