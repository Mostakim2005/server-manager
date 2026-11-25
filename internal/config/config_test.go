package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteJSONAtomicNoBOMAndConfigRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	p := DiscoverPaths(tmp)
	p.RegistryPrimaryPath = filepath.Join(tmp, "ecosystem-registry.json")
	p.RegistryMirrorPath = filepath.Join(tmp, "mirror-registry.json")
	p.SecretsPath = filepath.Join(tmp, "ecosystem-secrets.json")

	cfg, err := LoadOrInitConfig(p)
	if err != nil {
		t.Fatalf("LoadOrInitConfig failed: %v", err)
	}
	if !cfg.AutoStartCLIProxy || cfg.CLIProxyURL != DefaultCLIProxyURL {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}

	raw, err := os.ReadFile(p.ConfigPath)
	if err != nil {
		t.Fatalf("read config failed: %v", err)
	}
	if bytes.HasPrefix(raw, utf8BOM) {
		t.Fatalf("config.json must not start with UTF-8 BOM")
	}

	sec, err := LoadOrInitSecrets(p.SecretsPath)
	if err != nil || sec.Tokens["serverManager"] == "" {
		t.Fatalf("LoadOrInitSecrets failed: %v", err)
	}

	if err := WriteJSONAtomic(p.RegistryPrimaryPath, map[string]any{"schemaVersion": 2}, 0o600); err != nil {
		t.Fatalf("seed registry failed: %v", err)
	}
	if err := SyncEcosystemRegistry(p, 8790, `C:\Repo`, `C:\Bin\ServerManager.exe`, `C:\Bin\server-manager.exe`); err != nil {
		t.Fatalf("SyncEcosystemRegistry failed: %v", err)
	}
	reg, err := LoadRegistry(p.RegistryPrimaryPath)
	if err != nil || reg.Services["serverManager"]["url"] != "http://127.0.0.1:8790" {
		t.Fatalf("registry sync mismatch: %+v, err=%v", reg, err)
	}
}
