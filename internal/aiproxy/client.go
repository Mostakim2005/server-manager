package aiproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Mostakim2005/server-manager/internal/config"
	"github.com/Mostakim2005/server-manager/internal/discovery"
	"github.com/Mostakim2005/server-manager/internal/security"
)

var ErrProxyOffline = errors.New("CLIProxyAPI is offline on 127.0.0.1:8317")

// Status describes CLIProxyAPI reachability and available models.
type Status struct {
	Online      bool     `json:"online"`
	URL         string   `json:"url"`
	Models      []string `json:"models"`
	AutoStarted bool     `json:"autoStarted,omitempty"`
	Error       string   `json:"error,omitempty"`
}

// Client interacts with local CLIProxyAPI (OpenAI-compatible /v1 API).
type Client struct {
	BaseURL     string
	CLIProxyDir string
	HTTP        *http.Client
}

// NewClient creates a CLIProxyAPI client targeting baseURL.
func NewClient(baseURL, cliProxyDir string) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = config.DefaultCLIProxyURL
	}
	return &Client{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		CLIProxyDir: cliProxyDir,
		HTTP:        &http.Client{Timeout: 45 * time.Second},
	}
}

// Probe checks /v1/models quickly without launching anything.
func (c *Client) Probe(ctx context.Context) Status {
	pCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(pCtx, http.MethodGet, c.BaseURL+"/v1/models", nil)
	if err != nil {
		return Status{Online: false, URL: c.BaseURL, Error: err.Error()}
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Status{Online: false, URL: c.BaseURL, Error: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Status{Online: false, URL: c.BaseURL, Error: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return Status{Online: true, URL: c.BaseURL, Error: err.Error()}
	}
	models := make([]string, 0, len(payload.Data))
	for _, m := range payload.Data {
		if m.ID != "" {
			models = append(models, m.ID)
		}
	}
	return Status{Online: true, URL: c.BaseURL, Models: models}
}

// EnsureRunning probes CLIProxyAPI and auto-starts it silently if offline and autoStart is true (Delta 4).
func (c *Client) EnsureRunning(ctx context.Context, autoStart bool) (Status, error) {
	st := c.Probe(ctx)
	if st.Online {
		return st, nil
	}
	if !autoStart {
		return st, ErrProxyOffline
	}
	if err := c.startProxyHidden(); err != nil {
		st.Error = fmt.Sprintf("auto-start failed: %v", err)
		return st, fmt.Errorf("%w: %v", ErrProxyOffline, err)
	}
	deadline := time.Now().Add(9 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return st, ctx.Err()
		case <-time.After(450 * time.Millisecond):
		}
		st = c.Probe(ctx)
		if st.Online {
			st.AutoStarted = true
			// CLIProxyAPI opens :8317 ~1.5s before finishing cold-start OAuth token refresh.
			select {
			case <-ctx.Done():
				return st, ctx.Err()
			case <-time.After(1800 * time.Millisecond):
			}
			return st, nil
		}
	}
	return st, fmt.Errorf("%w (timed out waiting after auto-start)", ErrProxyOffline)
}

func (c *Client) startProxyHidden() error {
	if runtime.GOOS != "windows" {
		return errors.New("CLIProxyAPI auto-start launcher is Windows-specific")
	}
	vbs := filepath.Join(c.CLIProxyDir, "start-cliproxy-hidden.vbs")
	if _, err := os.Stat(vbs); err == nil {
		return exec.Command("wscript.exe", "//B", vbs).Start()
	}
	exe := filepath.Join(c.CLIProxyDir, "cli-proxy-api.exe")
	cfg := filepath.Join(c.CLIProxyDir, "config.yaml")
	if _, err := os.Stat(exe); err == nil {
		cmd := exec.Command(exe, "-config", cfg)
		cmd.Dir = c.CLIProxyDir
		return cmd.Start()
	}
	return fmt.Errorf("missing %s and %s", vbs, exe)
}

// GenerateReport calls CLIProxyAPI to explain what a discovered service/script does in plain English.
func (c *Client) GenerateReport(ctx context.Context, entry discovery.ServiceEntry, preferredModel string, autoStart bool) (config.AIReport, Status, error) {
	st, err := c.EnsureRunning(ctx, autoStart)
	if err != nil {
		return config.AIReport{}, st, err
	}

	scriptSnippet := readLimitedRedacted(entry.ScriptPath, 6000)
	stopSnippet := readLimitedRedacted(entry.StopScriptPath, 3000)
	readmeSnippet := readReadmeRedacted(entry.WorkingDir, 2500)

	prompt := fmt.Sprintf(`Analyze this local Windows launcher/service and return ONLY valid JSON with keys:
{
  "summary": "2-3 plain-English sentences explaining what this app/script does and how it is used.",
  "commandBreakdown": ["Short plain-English bullet explaining each major command or flag"],
  "ports": ["e.g. 8000 -> http://127.0.0.1:8000 (FastAPI web UI)"],
  "dependencies": ["e.g. Python / uvicorn", "Node.js", "Ollama"],
  "risksOrNotes": ["e.g. Ends with pause", "Binds 0.0.0.0 on LAN", "None"]
}

Service Name: %s
Working Dir: %s
Start Command: %s
Detected Ports: %v
Web URL: %s

Start Script:
%s

Stop Script:
%s

Project README Excerpt:
%s`, entry.Name, entry.WorkingDir, security.RedactSecrets(entry.StartCmd), entry.Ports, entry.WebURL, scriptSnippet, stopSnippet, readmeSnippet)

	candidates := pickCandidateModels(st.Models, preferredModel)
	rawJSON, usedModel, err := c.chatJSONWithFallback(ctx, candidates, prompt)
	if err != nil {
		return config.AIReport{}, st, err
	}

	var parsed struct {
		Summary          string   `json:"summary"`
		CommandBreakdown []string `json:"commandBreakdown"`
		Ports            []string `json:"ports"`
		Dependencies     []string `json:"dependencies"`
		RisksOrNotes     []string `json:"risksOrNotes"`
	}
	if err := json.Unmarshal([]byte(extractJSONBlock(rawJSON)), &parsed); err != nil {
		// Graceful fallback if the model answered in plain prose instead of strict JSON.
		parsed.Summary = strings.TrimSpace(rawJSON)
	}
	report := config.AIReport{
		ServiceID:        entry.ID,
		ScriptHash:       entry.ScriptHash,
		Model:            usedModel,
		GeneratedAt:      time.Now().Format(time.RFC3339),
		Summary:          parsed.Summary,
		CommandBreakdown: nonNilSlice(parsed.CommandBreakdown),
		Ports:            nonNilSlice(parsed.Ports),
		Dependencies:     nonNilSlice(parsed.Dependencies),
		RisksOrNotes:     nonNilSlice(parsed.RisksOrNotes),
	}
	return report, st, nil
}

// AnalyzeProjectFolder inspects a folder or script path and proposes a CustomService via CLIProxyAPI.
func (c *Client) AnalyzeProjectFolder(ctx context.Context, targetPath, preferredModel string, autoStart bool) (config.CustomService, Status, error) {
	st, err := c.EnsureRunning(ctx, autoStart)
	if err != nil {
		return config.CustomService{}, st, err
	}
	cleanTarget := filepath.Clean(strings.TrimSpace(targetPath))
	info, err := os.Stat(cleanTarget)
	if err != nil {
		return config.CustomService{}, st, fmt.Errorf("cannot inspect %q: %w", cleanTarget, err)
	}

	folder := cleanTarget
	var contextParts []string
	if !info.IsDir() {
		folder = filepath.Dir(cleanTarget)
		contextParts = append(contextParts, fmt.Sprintf("Target Script (%s):\n%s", filepath.Base(cleanTarget), readLimitedRedacted(cleanTarget, 5000)))
	} else {
		entries, _ := os.ReadDir(folder)
		var names []string
		for _, de := range entries {
			names = append(names, de.Name())
			low := strings.ToLower(de.Name())
			if strings.HasSuffix(low, ".bat") || strings.HasSuffix(low, ".cmd") || strings.HasSuffix(low, ".ps1") ||
				low == "package.json" || low == "pyproject.toml" || low == "go.mod" {
				contextParts = append(contextParts, fmt.Sprintf("--- %s ---\n%s", de.Name(), readLimitedRedacted(filepath.Join(folder, de.Name()), 2500)))
			}
		}
		contextParts = append(contextParts, "Directory Entries: "+strings.Join(names, ", "))
	}
	if rm := readReadmeRedacted(folder, 2500); rm != "" {
		contextParts = append(contextParts, "--- README ---\n"+rm)
	}

	prompt := fmt.Sprintf(`Inspect this local project folder and propose a clean Server Manager launcher config.
Return ONLY valid JSON with keys:
{
  "name": "Short human-friendly project title",
  "category": "server | desktop | custom",
  "description": "Plain-English 1-2 sentence explanation of what this app does and how to use it",
  "workingDir": %q,
  "startCmd": "Windows command line to start it cleanly",
  "stopCmd": "Optional Windows command to stop it, or empty string",
  "scriptPath": "Full path to primary .bat/.cmd/.ps1 if one exists, else empty string",
  "ports": [8000],
  "webUrl": "http://127.0.0.1:8000 or empty string",
  "tags": ["web-gui", "python"]
}

Folder / File Context:
%s`, folder, strings.Join(contextParts, "\n\n"))

	candidates := pickCandidateModels(st.Models, preferredModel)
	rawJSON, _, err := c.chatJSONWithFallback(ctx, candidates, prompt)
	if err != nil {
		return config.CustomService{}, st, err
	}

	var prop config.CustomService
	if err := json.Unmarshal([]byte(extractJSONBlock(rawJSON)), &prop); err != nil {
		return config.CustomService{}, st, fmt.Errorf("AI returned unreadable JSON: %w", err)
	}
	if strings.TrimSpace(prop.WorkingDir) == "" {
		prop.WorkingDir = folder
	}
	if strings.TrimSpace(prop.Name) == "" {
		prop.Name = filepath.Base(folder)
	}
	prop.AddedByAI = true
	prop.UpdatedAt = time.Now().Format(time.RFC3339)
	return prop, st, nil
}

func (c *Client) chatJSONWithFallback(ctx context.Context, models []string, userPrompt string) (string, string, error) {
	var lastErr error
	retriedWarmup := false
	for i := 0; i < len(models); i++ {
		model := models[i]
		body := map[string]any{
			"model":       model,
			"temperature": 0.2,
			"messages": []map[string]string{
				{"role": "system", "content": "You are a concise local developer assistant. Respond with clean JSON only."},
				{"role": "user", "content": userPrompt},
			},
		}
		b, _ := json.Marshal(body)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/chat/completions", bytes.NewReader(b))
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		_ = resp.Body.Close()

		if resp.StatusCode == http.StatusServiceUnavailable && !retriedWarmup && strings.Contains(string(respBytes), "auth_unavailable") {
			retriedWarmup = true
			select {
			case <-ctx.Done():
				return "", model, ctx.Err()
			case <-time.After(2 * time.Second):
			}
			i--
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 || resp.StatusCode == http.StatusBadRequest {
			lastErr = fmt.Errorf("model %s HTTP %d: %s", model, resp.StatusCode, strings.TrimSpace(string(respBytes)))
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return "", model, fmt.Errorf("model %s HTTP %d: %s", model, resp.StatusCode, strings.TrimSpace(string(respBytes)))
		}
		var out struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(respBytes, &out); err != nil || len(out.Choices) == 0 {
			lastErr = fmt.Errorf("model %s malformed response", model)
			continue
		}
		return out.Choices[0].Message.Content, model, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no AI models available on CLIProxyAPI")
	}
	return "", "", lastErr
}

func pickCandidateModels(available []string, preferred string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(m string) {
		m = strings.TrimSpace(m)
		if m != "" && !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	add(preferred)
	// Prefer fast text/JSON Flash models first, skipping image/embedding models.
	for _, m := range available {
		low := strings.ToLower(m)
		if strings.Contains(low, "embed") || strings.Contains(low, "image") {
			continue
		}
		if strings.Contains(low, "flash") || strings.Contains(low, "gemini") {
			add(m)
		}
	}
	for _, m := range available {
		low := strings.ToLower(m)
		if !strings.Contains(low, "embed") && !strings.Contains(low, "image") {
			add(m)
		}
	}
	if len(out) == 0 {
		out = []string{"gemini-3.8-flash-high", "gemini-3-flash", "gpt-oss-120b-medium"}
	}
	return out
}

func extractJSONBlock(raw string) string {
	s := strings.TrimSpace(raw)
	if start := strings.Index(s, "{"); start >= 0 {
		if end := strings.LastIndex(s, "}"); end > start {
			return s[start : end+1]
		}
	}
	return s
}

func readLimitedRedacted(path string, maxBytes int) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if len(raw) > maxBytes {
		raw = raw[:maxBytes]
	}
	return security.RedactSecrets(string(raw))
}

func readReadmeRedacted(dir string, maxBytes int) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	for _, name := range []string{"README.md", "readme.md"} {
		if txt := readLimitedRedacted(filepath.Join(dir, name), maxBytes); txt != "" {
			return txt
		}
	}
	return ""
}

func nonNilSlice(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
