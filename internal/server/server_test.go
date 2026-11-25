package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
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
	if _, ok := payload["startup"]; !ok {
		t.Fatalf("state missing startup key")
	}
	if _, ok := payload["selfStartup"]; !ok {
		t.Fatalf("state missing selfStartup key")
	}

	// 4. Startup toggle endpoint requires auth and handles temp startup folder cleanly.
	paths.StartupDir = filepath.Join(tmp, "Startup")
	d.Paths.StartupDir = paths.StartupDir
	reqToggle := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8790/api/v1/startup/self", bytes.NewBufferString(`{"enabled":true}`))
	reqToggle.Host = "127.0.0.1:8790"
	reqToggle.Header.Set("Authorization", "Bearer "+d.UIToken)
	rw4 := httptest.NewRecorder()
	h.ServeHTTP(rw4, reqToggle)
	if runtime.GOOS == "windows" {
		if rw4.Code != http.StatusOK {
			t.Fatalf("expected 200 toggling self startup on Windows, got %d: %s", rw4.Code, rw4.Body.String())
		}
		reqOff := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8790/api/v1/startup/self", bytes.NewBufferString(`{"enabled":false}`))
		reqOff.Host = "127.0.0.1:8790"
		reqOff.Header.Set("Authorization", "Bearer "+d.UIToken)
		rw5 := httptest.NewRecorder()
		h.ServeHTTP(rw5, reqOff)
		if rw5.Code != http.StatusOK {
			t.Fatalf("expected 200 disabling self startup on Windows, got %d: %s", rw5.Code, rw5.Body.String())
		}
	} else if rw4.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on non-Windows startup toggle, got %d", rw4.Code)
	}

	// 5. Custom service icon override, /service-icons/ serving, preferred script, and grouping split/rejoin.
	svcDir := filepath.Join(paths.AgenticHarnessDir, "DemoHub")
	_ = os.MkdirAll(svcDir, 0o755)
	_ = os.WriteFile(filepath.Join(svcDir, "run.bat"), []byte("@echo off\npython -m uvicorn app:app --port 8991\n"), 0o600)
	_ = os.WriteFile(filepath.Join(svcDir, "run-lan.bat"), []byte("@echo off\npython -m uvicorn app:app --host 0.0.0.0 --port 8991\n"), 0o600)
	iconSVG := filepath.Join(tmp, "demo.svg")
	_ = os.WriteFile(iconSVG, []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><circle cx="8" cy="8" r="6"/></svg>`), 0o600)
	d.Rescan()

	iconBody, _ := json.Marshal(map[string]string{"iconPath": iconSVG})
	reqIcon := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8790/api/v1/services/demohub-run/icon", bytes.NewReader(iconBody))
	reqIcon.Host = "127.0.0.1:8790"
	reqIcon.Header.Set("Authorization", "Bearer "+d.UIToken)
	rwIcon := httptest.NewRecorder()
	h.ServeHTTP(rwIcon, reqIcon)
	if rwIcon.Code != http.StatusOK {
		t.Fatalf("expected 200 saving icon override, got %d: %s", rwIcon.Code, rwIcon.Body.String())
	}

	reqServeIcon := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8790/service-icons/demohub-run", nil)
	reqServeIcon.Host = "127.0.0.1:8790"
	rwServeIcon := httptest.NewRecorder()
	h.ServeHTTP(rwServeIcon, reqServeIcon)
	if rwServeIcon.Code != http.StatusOK || rwServeIcon.Header().Get("Cross-Origin-Resource-Policy") != "same-origin" {
		t.Fatalf("expected 200 serving icon with same-origin policy, got %d", rwServeIcon.Code)
	}

	reqCross := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8790/service-icons/demohub-run", nil)
	reqCross.Host = "127.0.0.1:8790"
	reqCross.Header.Set("Sec-Fetch-Site", "cross-site")
	rwCross := httptest.NewRecorder()
	h.ServeHTTP(rwCross, reqCross)
	if rwCross.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on cross-site icon fetch, got %d", rwCross.Code)
	}

	reqPref := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8790/api/v1/groups/proj-demohub/preferred", bytes.NewBufferString(`{"serviceId":"demohub-run-lan"}`))
	reqPref.Host = "127.0.0.1:8790"
	reqPref.Header.Set("Authorization", "Bearer "+d.UIToken)
	rwPref := httptest.NewRecorder()
	h.ServeHTTP(rwPref, reqPref)
	if rwPref.Code != http.StatusOK || d.Config.PreferredScripts["proj-demohub"] != "demohub-run-lan" {
		t.Fatalf("expected preferred script persisted, code=%d val=%q", rwPref.Code, d.Config.PreferredScripts["proj-demohub"])
	}

	reqSplit := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8790/api/v1/services/demohub-run-lan/grouping", bytes.NewBufferString(`{"action":"split"}`))
	reqSplit.Host = "127.0.0.1:8790"
	reqSplit.Header.Set("Authorization", "Bearer "+d.UIToken)
	rwSplit := httptest.NewRecorder()
	h.ServeHTTP(rwSplit, reqSplit)
	if rwSplit.Code != http.StatusOK {
		t.Fatalf("expected 200 splitting card, got %d: %s", rwSplit.Code, rwSplit.Body.String())
	}
	if s, ok := d.findService("demohub-run-lan"); !ok || s.GroupKey != "" {
		t.Fatalf("expected split service to have empty GroupKey, got %+v", s)
	}

	// 6. Updating an existing stack preset preserves order and deduplicates service IDs.
	reqStack := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8790/api/v1/stacks", bytes.NewBufferString(`{"id":"study-ai-stack","name":"Study + AI Updated","description":"Ordered","serviceIds":["demohub-run","demohub-run-lan","demohub-run"]}`))
	reqStack.Host = "127.0.0.1:8790"
	reqStack.Header.Set("Authorization", "Bearer "+d.UIToken)
	rwStack := httptest.NewRecorder()
	h.ServeHTTP(rwStack, reqStack)
	if rwStack.Code != http.StatusOK {
		t.Fatalf("expected 200 updating stack preset, got %d: %s", rwStack.Code, rwStack.Body.String())
	}
	var foundUpdated bool
	for _, st := range d.Config.Stacks {
		if st.ID == "study-ai-stack" {
			foundUpdated = true
			if st.Name != "Study + AI Updated" || len(st.ServiceIDs) != 2 || st.ServiceIDs[0] != "demohub-run" || st.ServiceIDs[1] != "demohub-run-lan" {
				t.Fatalf("unexpected updated stack: %+v", st)
			}
		}
	}
	if !foundUpdated {
		t.Fatalf("updated stack not found in config")
	}
}
