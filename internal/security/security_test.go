package security

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoopbackHostAndOriginGuards(t *testing.T) {
	if !IsLoopbackBind("127.0.0.1:8790") || IsLoopbackBind("0.0.0.0:8790") {
		t.Fatalf("unexpected loopback validation result")
	}
	if !ValidateLoopbackHost("127.0.0.1:8790", 8790) || !ValidateLoopbackHost("localhost:8790", 8790) {
		t.Fatalf("valid loopback hosts rejected")
	}
	if ValidateLoopbackHost("evil.example:8790", 8790) {
		t.Fatalf("non-loopback Host header should be rejected")
	}
	if ValidateOrigin("https://evil.example", 8790, false) {
		t.Fatalf("cross-origin request should be rejected")
	}
	if ValidateOrigin("http://127.0.0.1:8790", 8790, true) {
		t.Fatalf("strictNoOrigin should reject all Origin headers")
	}
}

func TestTokenAndRedaction(t *testing.T) {
	tok, err := GenerateToken()
	if err != nil || len(tok) < 20 {
		t.Fatalf("GenerateToken failed: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	got := ExtractBearerToken(req)
	if !ConstantTimeTokenMatch(got, tok) {
		t.Fatalf("expected matching bearer token")
	}

	masked := RedactSecrets("GROQ_API_KEY=gsk_supersecret1234567890 sk-1234567890abcdef12345678")
	if strings.Contains(masked, "gsk_supersecret") || strings.Contains(masked, "sk-123456") {
		t.Fatalf("secrets were not redacted: %s", masked)
	}
}

func TestSanitizeRelPath(t *testing.T) {
	for _, bad := range []string{"../x", `C:\foo`, `\\server\share`, "CON.txt", "a\x00b"} {
		if _, err := SanitizeRelPath(bad); err == nil {
			t.Fatalf("expected error for unsafe path %q", bad)
		}
	}
	if clean, err := SanitizeRelPath("m3-tokens/m3-tokens.css"); err != nil || clean != "m3-tokens/m3-tokens.css" {
		t.Fatalf("valid path failed: %q, %v", clean, err)
	}
}
