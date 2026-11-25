package aiproxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Mostakim2005/server-manager/internal/discovery"
)

func TestGenerateReportWith429Fallback(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{
					{"id": "quota-exhausted-model"},
					{"id": "gemini-flash-ok"},
				},
			})
		case "/v1/chat/completions":
			if calls.Add(1) == 1 {
				http.Error(w, "429 quota", http.StatusTooManyRequests)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{"message": map[string]string{
						"content": `{"summary":"Runs FastAPI server on port 8000.","commandBreakdown":["uvicorn app.main:app"],"ports":["8000"],"dependencies":["Python"],"risksOrNotes":["None"]}`,
					}},
				},
			})
		}
	}))
	defer srv.Close()

	tmp := t.TempDir()
	scriptPath := filepath.Join(tmp, "run.bat")
	_ = os.WriteFile(scriptPath, []byte("@echo off\npython -m uvicorn app:app --port 8000\n"), 0o600)

	client := NewClient(srv.URL, tmp)
	report, st, err := client.GenerateReport(context.Background(), discovery.ServiceEntry{
		ID:         "test-run",
		Name:       "Test App",
		ScriptPath: scriptPath,
		WorkingDir: tmp,
		StartCmd:   scriptPath,
		Ports:      []int{8000},
		ScriptHash: "abc123",
	}, "quota-exhausted-model", false)
	if err != nil {
		t.Fatalf("GenerateReport failed: %v", err)
	}
	if !st.Online || report.Model != "gemini-flash-ok" || report.Summary == "" {
		t.Fatalf("unexpected report: %+v, status=%+v", report, st)
	}
}
