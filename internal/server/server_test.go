package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Mostakim2005/server-manager/internal/config"
)

func TestDaemonSecurityAndStateEndpoints(t *testing.T) {
	tmp := t.TempDir()
	paths := config.DiscoverPaths(tmp)
	paths.RegistryPrimaryPath = filepath.Join(tmp, "ecosystem-registry.json")
	paths.RegistryMirrorPath = filepath.Join(tmp, "mirror.json")
	paths.SecretsPath = filepath.Join(tmp, "ecosystem-secrets.json")
	paths.AgenticHarnessDir = filepath.Join(tmp, "Harness")
	_ = os.MkdirAll(paths.AgenticHarnessDir, 0o755)
	_ = os.WriteFile(filepath.Join(paths.AgenticHarnessDir, "start.bat"), []byte("@echo off\npython -m http.server 8123\n"), 0o600)

	d, err := NewDaemon(paths)
	if err != nil {
		t.Fatalf("NewDaemon failed: %v", err)
	}
	defer d.Supervisor.ShutdownAll()

	h := d.Handler()

	// 1. Unauthenticated /api/v1/state must return 401.
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8790/api/v1/state", nil)
	req.Host = "127.0.0.1:8790"
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rw.Code)
	}

	// 2. Cross-origin Origin header must be blocked with 403.
	reqBadOrigin := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8790/api/v1/state", nil)
	reqBadOrigin.Host = "127.0.0.1:8790"
	reqBadOrigin.Header.Set("Origin", "https://evil.example")
	reqBadOrigin.Header.Set("Authorization", "Bearer "+d.UIToken)
	rw2 := httptest.NewRecorder()
	h.ServeHTTP(rw2, reqBadOrigin)
	if rw2.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on cross-origin, got %d", rw2.Code)
	}

	// 3. Valid bearer token returns discovered state.
	reqOK := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8790/api/v1/state", nil)
	reqOK.Host = "127.0.0.1:8790"
	reqOK.Header.Set("Authorization", "Bearer "+d.UIToken)
	rw3 := httptest.NewRecorder()
	h.ServeHTTP(rw3, reqOK)
	if rw3.Code != http.StatusOK {
		t.Fatalf("expected 200 on valid request, got %d: %s", rw3.Code, rw3.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rw3.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode state JSON failed: %v", err)
	}
	if _, ok := payload["services"]; !ok {
		t.Fatalf("state missing services key")
	}
}
