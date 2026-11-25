package supervisor

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/Mostakim2005/server-manager/internal/discovery"
)

func TestStartStopAndLogsLifecycle(t *testing.T) {
	mgr := NewManager(nil)
	defer mgr.ShutdownAll()

	cmd := "sleep 5"
	if runtime.GOOS == "windows" {
		cmd = "powershell -NoProfile -Command \"Write-Output 'hello-sm'; Start-Sleep -Seconds 5\""
	} else {
		cmd = "echo hello-sm; sleep 5"
	}

	entry := discovery.ServiceEntry{
		ID:       "test-sleeper",
		Name:     "Test Sleeper",
		StartCmd: cmd,
	}

	st, err := mgr.Start(entry)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if st.State != "running" {
		t.Fatalf("expected running state, got %+v", st)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(mgr.Logs(entry.ID)) >= 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(mgr.Logs(entry.ID)) == 0 {
		t.Fatalf("expected captured logs")
	}

	stAfter, err := mgr.Stop(context.Background(), entry)
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if stAfter.State != "stopped" {
		t.Fatalf("expected stopped state after Stop, got %+v", stAfter)
	}
}
