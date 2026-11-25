package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	// ErrUnsafePath indicates path traversal, drive/UNC prefix, or reserved NTFS name.
	ErrUnsafePath = errors.New("unsafe or disallowed relative path")
	// ErrSymlinkEscape indicates a symlink or NTFS junction points outside confined root.
	ErrSymlinkEscape = errors.New("resolved path escapes confined root")
)

var ntfsReservedNames = map[string]struct{}{
	"CON": {}, "PRN": {}, "AUX": {}, "NUL": {},
	"COM1": {}, "COM2": {}, "COM3": {}, "COM4": {}, "COM5": {},
	"COM6": {}, "COM7": {}, "COM8": {}, "COM9": {},
	"LPT1": {}, "LPT2": {}, "LPT3": {}, "LPT4": {}, "LPT5": {},
	"LPT6": {}, "LPT7": {}, "LPT8": {}, "LPT9": {},
}

var (
	sensitiveKVRegex    = regexp.MustCompile(`(?i)((?:authorization[=:\s"]+bearer\s+|bearer\s+|(?:x-local-token|api[_-]?key|access_token|refresh_token|password|secret|groq_api_key|openai_api_key|gemini_api_key)[=:\s"]+))([^\s"',}]+)`)
	sensitiveTokenRegex = regexp.MustCompile(`(?:sk-[A-Za-z0-9_-]{16,}|gh[opusr]_[A-Za-z0-9]{20,}|AIza[0-9A-Za-z_-]{20,})`)
)

// GenerateToken returns a cryptographically random 256-bit URL-safe token.
func GenerateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// IsLoopbackBind asserts that addr binds strictly to 127.0.0.1, ::1, or localhost.
func IsLoopbackBind(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.TrimSpace(host)
	if host == "" || host == "0.0.0.0" || host == "::" || host == "*" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ValidateLoopbackHost ensures Host matches 127.0.0.1:<port> or localhost:<port>.
func ValidateLoopbackHost(hostHeader string, expectedPort int) bool {
	if hostHeader == "" {
		return false
	}
	if expectedPort == 0 {
		h, _, err := net.SplitHostPort(hostHeader)
		if err != nil {
			h = hostHeader
		}
		return h == "127.0.0.1" || strings.EqualFold(h, "localhost")
	}
	return strings.EqualFold(hostHeader, fmt.Sprintf("127.0.0.1:%d", expectedPort)) ||
		strings.EqualFold(hostHeader, fmt.Sprintf("localhost:%d", expectedPort))
}

// ValidateOrigin blocks cross-origin requests; strictNoOrigin rejects any Origin header.
func ValidateOrigin(origin string, expectedPort int, strictNoOrigin bool) bool {
	if origin == "" {
		return true
	}
	if strictNoOrigin {
		return false
	}
	if expectedPort == 0 {
		return strings.HasPrefix(origin, "http://127.0.0.1:") || strings.HasPrefix(origin, "http://localhost:")
	}
	return origin == fmt.Sprintf("http://127.0.0.1:%d", expectedPort) ||
		origin == fmt.Sprintf("http://localhost:%d", expectedPort)
}

// ExtractBearerToken reads Authorization: Bearer <tok> or X-Local-Token: <tok>.
func ExtractBearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(auth) > 7 && strings.EqualFold(auth[:7], "Bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	if tok := strings.TrimSpace(r.Header.Get("X-Local-Token")); tok != "" {
		return tok
	}
	return ""
}

// ConstantTimeTokenMatch compares candidate against allowed tokens in constant time.
func ConstantTimeTokenMatch(candidate string, validTokens ...string) bool {
	if candidate == "" {
		return false
	}
	candBytes := []byte(candidate)
	matched := false
	for _, tok := range validTokens {
		if tok == "" {
			continue
		}
		if subtle.ConstantTimeCompare(candBytes, []byte(tok)) == 1 {
			matched = true
		}
	}
	return matched
}

// SanitizeRelPath validates a relative path for confined asset serving.
func SanitizeRelPath(raw string) (string, error) {
	if raw == "" || strings.ContainsRune(raw, 0) {
		return "", ErrUnsafePath
	}
	for _, r := range raw {
		if r < 32 {
			return "", ErrUnsafePath
		}
	}
	if strings.HasPrefix(raw, `\\`) || strings.HasPrefix(raw, `//`) ||
		strings.HasPrefix(raw, `/`) || strings.HasPrefix(raw, `\`) {
		return "", ErrUnsafePath
	}
	if len(raw) >= 2 && ((raw[0] >= 'a' && raw[0] <= 'z') || (raw[0] >= 'A' && raw[0] <= 'Z')) && raw[1] == ':' {
		return "", ErrUnsafePath
	}

	normalized := strings.ReplaceAll(raw, `\`, `/`)
	parts := strings.Split(normalized, "/")
	cleanParts := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", ErrUnsafePath
		}
		if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || strings.ContainsAny(part, `<>:"|?*`) {
			return "", ErrUnsafePath
		}
		stem := part
		if dot := strings.IndexByte(stem, '.'); dot >= 0 {
			stem = stem[:dot]
		}
		if _, reserved := ntfsReservedNames[strings.ToUpper(stem)]; reserved {
			return "", ErrUnsafePath
		}
		cleanParts = append(cleanParts, part)
	}
	return strings.Join(cleanParts, "/"), nil
}

// ResolveConfinedPath ensures relPath resolves strictly inside root.
func ResolveConfinedPath(root, relPath string) (string, error) {
	cleanRel, err := SanitizeRelPath(relPath)
	if err != nil {
		return "", err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if evalRoot, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = evalRoot
	}
	candidate := filepath.Join(absRoot, filepath.FromSlash(cleanRel))
	if !isWithinRoot(absRoot, candidate) {
		return "", ErrSymlinkEscape
	}
	cur := candidate
	for {
		if _, lerr := os.Lstat(cur); lerr == nil {
			resolved, rerr := filepath.EvalSymlinks(cur)
			if rerr != nil || !isWithinRoot(absRoot, resolved) {
				return "", ErrSymlinkEscape
			}
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return candidate, nil
}

func isWithinRoot(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// RedactSecrets masks bearer tokens, API keys, and credentials before logging or sending to AI prompts.
func RedactSecrets(input string) string {
	if input == "" {
		return ""
	}
	out := sensitiveKVRegex.ReplaceAllString(input, "${1}[REDACTED]")
	return sensitiveTokenRegex.ReplaceAllString(out, "[REDACTED_TOKEN]")
}
