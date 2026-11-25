package cli

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestCLIHelpAndHealthJSON(t *testing.T) {
	t.Setenv("SERVER_MANAGER_DATA_DIR", t.TempDir())
	var out, errBuf bytes.Buffer

	if code := Run([]string{"--help"}, nil, &out, &errBuf); code != 0 {
		t.Fatalf("help exited %d", code)
	}

	out.Reset()
	if code := Run([]string{"health", "--json"}, nil, &out, &errBuf); code != 0 {
		t.Fatalf("health --json exited %d: %s", code, errBuf.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json output: %v (%s)", err, out.String())
	}
	if payload["service"] != "server-manager" {
		t.Fatalf("unexpected service field: %+v", payload)
	}
}

func TestCLIStartupListJSON(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SERVER_MANAGER_DATA_DIR", tmp)
	t.Setenv("SERVER_MANAGER_STARTUP_DIR", tmp)
	var out, errBuf bytes.Buffer

	if code := Run([]string{"startup", "list", "--json"}, nil, &out, &errBuf); code != 0 {
		t.Fatalf("startup list --json exited %d: %s", code, errBuf.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid startup list JSON: %v (%s)", err, out.String())
	}
	if _, ok := payload["self"]; !ok {
		t.Fatalf("expected self key in startup list JSON: %+v", payload)
	}
	if _, ok := payload["services"]; !ok {
		t.Fatalf("expected services key in startup list JSON: %+v", payload)
	}
}
