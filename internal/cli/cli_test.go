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
