package supervisor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Mostakim2005/server-manager/internal/discovery"
	"github.com/Mostakim2005/server-manager/internal/security"
)

const maxLogLines = 600

// RuntimeStatus describes live process & port state for one service.
type RuntimeStatus struct {
	ServiceID   string `json:"serviceId"`
	State       string `json:"state"` // "running" | "running_external" | "stopped" | "error"
	Managed     bool   `json:"managed"`
	PID         int    `json:"pid,omitempty"`
	ActivePorts []int  `json:"activePorts,omitempty"`
	ExternalPID []int  `json:"externalPids,omitempty"`
	StartedAt   string `json:"startedAt,omitempty"`
	LastError   string `json:"lastError,omitempty"`
	LogCount    int    `json:"logCount"`
}

// Event is pushed over SSE to active Web UI tabs.
type Event struct {
	Type      string `json:"type"` // "status" | "log"
	ServiceID string `json:"serviceId"`
	Line      string `json:"line,omitempty"`
	Timestamp string `json:"ts"`
}

type managedProc struct {
	cmd       *exec.Cmd
	pid       int
	startedAt time.Time
	running   bool
	lastError string
	logs      []string
}

// Manager supervises launched scripts, probes listening ports, and streams SSE logs.
type Manager struct {
	mu      sync.RWMutex
	procs   map[string]*managedProc
	jobSup  *security.JobSupervisor
	subMu   sync.Mutex
	subs    map[chan Event]struct{}
	lastEvt time.Time
}

// NewManager creates a process supervisor backed by a Windows Job Object when available.
func NewManager(jobSup *security.JobSupervisor) *Manager {
	return &Manager{
		procs:  make(map[string]*managedProc),
		jobSup: jobSup,
		subs:   make(map[chan Event]struct{}),
	}
}

// Subscribe registers an SSE subscriber channel (0% overhead when zero subscribers exist).
func (m *Manager) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)
	m.subMu.Lock()
	m.subs[ch] = struct{}{}
	m.subMu.Unlock()
	return ch, func() {
		m.subMu.Lock()
		delete(m.subs, ch)
		close(ch)
		m.subMu.Unlock()
	}
}

// EmitStatus broadcasts a status refresh event to subscribed SSE tabs.
func (m *Manager) EmitStatus(serviceID string) {
	m.emit(Event{Type: "status", ServiceID: serviceID, Timestamp: time.Now().UTC().Format(time.RFC3339)})
}

// EmitUIReload broadcasts a Web UI reload trigger to subscribed SSE tabs.
func (m *Manager) EmitUIReload() {
	m.emit(Event{Type: "ui-reload", Timestamp: time.Now().UTC().Format(time.RFC3339)})
}

func (m *Manager) emit(ev Event) {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	if len(m.subs) == 0 {
		return
	}
	for ch := range m.subs {
		select {
		case ch <- ev:
		default:
			// Drop oldest burst on slow UI reader rather than blocking process stdout.
		}
	}
}

func (m *Manager) appendLog(serviceID, line string) {
	m.mu.Lock()
	mp, ok := m.procs[serviceID]
	if !ok {
		mp = &managedProc{}
		m.procs[serviceID] = mp
	}
	tsLine := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), line)
	mp.logs = append(mp.logs, tsLine)
	if len(mp.logs) > maxLogLines {
		mp.logs = append([]string(nil), mp.logs[len(mp.logs)-maxLogLines:]...)
	}
	m.mu.Unlock()
	m.emit(Event{Type: "log", ServiceID: serviceID, Line: tsLine, Timestamp: time.Now().UTC().Format(time.RFC3339)})
}

// Logs returns buffered stdout/stderr lines for a service.
func (m *Manager) Logs(serviceID string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	mp, ok := m.procs[serviceID]
	if !ok {
		return []string{}
	}
	out := make([]string, len(mp.logs))
	copy(out, mp.logs)
	return out
}

// Start launches a ServiceEntry inside a detached hidden process tree and captures its logs.
func (m *Manager) Start(entry discovery.ServiceEntry) (RuntimeStatus, error) {
	if strings.TrimSpace(entry.StartCmd) == "" && strings.TrimSpace(entry.ScriptPath) == "" {
		return RuntimeStatus{}, fmt.Errorf("service %q has no start command or script path", entry.Name)
	}
	m.mu.RLock()
	if mp, ok := m.procs[entry.ID]; ok && mp.running {
		m.mu.RUnlock()
		return m.StatusFor(entry, nil), nil
	}
	m.mu.RUnlock()

	// Do not double-start if its port is already active externally.
	for _, pt := range entry.Ports {
		if isPortOpen(pt) {
			m.appendLog(entry.ID, fmt.Sprintf("[INFO] Port :%d is already listening externally.", pt))
			return m.StatusFor(entry, nil), nil
		}
	}

	cmd, err := prepareServiceCmd(entry)
	if err != nil {
		return RuntimeStatus{}, err
	}

	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		_ = pr.Close()
		m.mu.Lock()
		m.procs[entry.ID] = &managedProc{lastError: err.Error()}
		m.mu.Unlock()
		return RuntimeStatus{}, fmt.Errorf("start %s: %w", entry.Name, err)
	}

	pid := cmd.Process.Pid
	if m.jobSup != nil {
		_ = m.jobSup.AssignPID(pid)
	}

	m.mu.Lock()
	prevLogs := []string(nil)
	if old, ok := m.procs[entry.ID]; ok {
		prevLogs = old.logs
	}
	mp := &managedProc{
		cmd:       cmd,
		pid:       pid,
		startedAt: time.Now().UTC(),
		running:   true,
		logs:      prevLogs,
	}
	m.procs[entry.ID] = mp
	m.mu.Unlock()

	m.appendLog(entry.ID, fmt.Sprintf("[STARTED] PID=%d cmd=%s", pid, entry.StartCmd))
	m.emit(Event{Type: "status", ServiceID: entry.ID, Timestamp: time.Now().UTC().Format(time.RFC3339)})

	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 16*1024), 256*1024)
		for sc.Scan() {
			m.appendLog(entry.ID, sc.Text())
		}
		_ = pr.Close()
	}()

	go func() {
		waitErr := cmd.Wait()
		_ = pw.Close()
		m.mu.Lock()
		cur := m.procs[entry.ID]
		if cur == mp {
			cur.running = false
			if waitErr != nil {
				cur.lastError = waitErr.Error()
			}
		}
		m.mu.Unlock()
		if waitErr != nil {
			m.appendLog(entry.ID, fmt.Sprintf("[EXITED] %v", waitErr))
		} else {
			m.appendLog(entry.ID, "[EXITED] clean exit (0)")
		}
		m.emit(Event{Type: "status", ServiceID: entry.ID, Timestamp: time.Now().UTC().Format(time.RFC3339)})
	}()

	return m.StatusFor(entry, nil), nil
}

// Stop terminates a running managed service (and optionally runs its paired stop script or clears port owners).
func (m *Manager) Stop(ctx context.Context, entry discovery.ServiceEntry) (RuntimeStatus, error) {
	// 1. Run paired stop script / explicit stop command if configured.
	if strings.TrimSpace(entry.StopScriptPath) != "" || strings.TrimSpace(entry.StopCmd) != "" {
		m.appendLog(entry.ID, "[STOP] Running paired stop command...")
		stopCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if scmd, err := buildStopExecCommand(stopCtx, entry); err == nil {
			configureHiddenCmd(scmd)
			if entry.WorkingDir != "" {
				scmd.Dir = entry.WorkingDir
			}
			scmd.Stdin = nil
			out, serr := scmd.CombinedOutput()
			if len(out) > 0 {
				for _, ln := range strings.Split(strings.TrimSpace(string(out)), "\n") {
					m.appendLog(entry.ID, "[stop-script] "+strings.TrimSpace(ln))
				}
			}
			if serr != nil {
				m.appendLog(entry.ID, fmt.Sprintf("[stop-script warning] %v", serr))
			}
		}
		cancel()
	}

	// 2. Kill managed process tree if still alive.
	m.mu.RLock()
	mp, ok := m.procs[entry.ID]
	var pid int
	var running bool
	if ok {
		pid = mp.pid
		running = mp.running
	}
	m.mu.RUnlock()

	if running && pid > 0 {
		m.appendLog(entry.ID, fmt.Sprintf("[STOP] Terminating process tree PID=%d...", pid))
		_ = terminatePIDTree(pid)
	}

	// 3. If an external process is still holding the primary port, terminate that owning PID.
	portOwners := ListPortOwners()
	for _, pt := range entry.Ports {
		for _, extPID := range portOwners[pt] {
			if extPID > 0 && extPID != pid {
				m.appendLog(entry.ID, fmt.Sprintf("[STOP] Terminating external listener PID=%d on port :%d...", extPID, pt))
				_ = terminatePIDTree(extPID)
			}
		}
	}

	m.mu.Lock()
	if cur, exists := m.procs[entry.ID]; exists {
		cur.running = false
	}
	m.mu.Unlock()
	m.emit(Event{Type: "status", ServiceID: entry.ID, Timestamp: time.Now().UTC().Format(time.RFC3339)})
	return m.StatusFor(entry, ListPortOwners()), nil
}

// StatusMap computes live status for all entries using a single port-owner pass.
func (m *Manager) StatusMap(entries []discovery.ServiceEntry) map[string]RuntimeStatus {
	owners := ListPortOwners()
	out := make(map[string]RuntimeStatus, len(entries))
	for _, e := range entries {
		out[e.ID] = m.StatusFor(e, owners)
	}
	return out
}

// StatusFor returns live state for one service.
func (m *Manager) StatusFor(entry discovery.ServiceEntry, portOwners map[int][]int) RuntimeStatus {
	if portOwners == nil {
		portOwners = ListPortOwners()
	}
	m.mu.RLock()
	mp := m.procs[entry.ID]
	var managed, running bool
	var pid, logCount int
	var startedAt, lastErr string
	if mp != nil {
		managed = mp.running
		running = mp.running
		pid = mp.pid
		logCount = len(mp.logs)
		lastErr = mp.lastError
		if !mp.startedAt.IsZero() {
			startedAt = mp.startedAt.Format(time.RFC3339)
		}
	}
	m.mu.RUnlock()

	var activePorts []int
	var extPIDs []int
	seenPID := map[int]bool{}
	for _, pt := range entry.Ports {
		pids := portOwners[pt]
		open := len(pids) > 0 || isPortOpen(pt)
		if open {
			activePorts = append(activePorts, pt)
			for _, p := range pids {
				if p > 0 && !seenPID[p] {
					seenPID[p] = true
					extPIDs = append(extPIDs, p)
				}
			}
		}
	}

	state := "stopped"
	if running {
		state = "running"
	} else if len(activePorts) > 0 {
		state = "running_external"
	} else if lastErr != "" {
		state = "error"
	}

	return RuntimeStatus{
		ServiceID:   entry.ID,
		State:       state,
		Managed:     managed,
		PID:         pid,
		ActivePorts: activePorts,
		ExternalPID: extPIDs,
		StartedAt:   startedAt,
		LastError:   lastErr,
		LogCount:    logCount,
	}
}

// ShutdownAll terminates all currently managed child process trees cleanly.
func (m *Manager) ShutdownAll() {
	m.mu.RLock()
	pids := make([]int, 0, len(m.procs))
	for _, mp := range m.procs {
		if mp.running && mp.pid > 0 {
			pids = append(pids, mp.pid)
		}
	}
	m.mu.RUnlock()
	for _, pid := range pids {
		_ = terminatePIDTree(pid)
	}
	if m.jobSup != nil {
		_ = m.jobSup.Close()
	}
}

// LaunchDetached spawns a service silently in the background (for Windows Startup shortcuts) and exits immediately.
func LaunchDetached(entry discovery.ServiceEntry, logPath string) (int, error) {
	if strings.TrimSpace(entry.StartCmd) == "" && strings.TrimSpace(entry.ScriptPath) == "" {
		return 0, fmt.Errorf("service %q has no start command or script path", entry.Name)
	}
	for _, pt := range entry.Ports {
		if isPortOpen(pt) {
			return 0, nil
		}
	}
	cmd, err := prepareServiceCmd(entry)
	if err != nil {
		return 0, err
	}
	var logFile *os.File
	if strings.TrimSpace(logPath) != "" {
		if mkErr := os.MkdirAll(filepath.Dir(logPath), 0o700); mkErr == nil {
			if f, oerr := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600); oerr == nil {
				logFile = f
				cmd.Stdout = f
				cmd.Stderr = f
			}
		}
	}
	if err := cmd.Start(); err != nil {
		if logFile != nil {
			_ = logFile.Close()
		}
		return 0, fmt.Errorf("launch %s: %w", entry.Name, err)
	}
	if logFile != nil {
		_ = logFile.Close()
	}
	pid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
		_ = cmd.Process.Release()
	}
	return pid, nil
}

func prepareServiceCmd(entry discovery.ServiceEntry) (*exec.Cmd, error) {
	cmd, err := buildExecCommand(entry)
	if err != nil {
		return nil, err
	}
	configureHiddenCmd(cmd)
	if entry.WorkingDir != "" {
		cmd.Dir = entry.WorkingDir
	}
	cmd.Stdin = nil
	cmd.Env = append(cmd.Environ(),
		"PYTHONUNBUFFERED=1",
		"PYTHONIOENCODING=utf-8",
		"PYTHONUTF8=1",
	)
	return cmd, nil
}

func buildExecCommand(entry discovery.ServiceEntry) (*exec.Cmd, error) {
	if runtime.GOOS == "windows" {
		if entry.ScriptPath != "" {
			ext := strings.ToLower(entry.ScriptPath)
			if strings.HasSuffix(ext, ".ps1") {
				return exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", entry.ScriptPath), nil
			}
			// Use `call` + closed stdin so trailing `pause` lines never block exit.
			return exec.Command("cmd.exe", "/c", "call", entry.ScriptPath), nil
		}
		return exec.Command("cmd.exe", "/c", entry.StartCmd), nil
	}
	if entry.ScriptPath != "" {
		return exec.Command("sh", entry.ScriptPath), nil
	}
	return exec.Command("sh", "-c", entry.StartCmd), nil
}

func buildStopExecCommand(ctx context.Context, entry discovery.ServiceEntry) (*exec.Cmd, error) {
	if runtime.GOOS == "windows" {
		if entry.StopScriptPath != "" {
			if strings.HasSuffix(strings.ToLower(entry.StopScriptPath), ".ps1") {
				return exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", entry.StopScriptPath), nil
			}
			return exec.CommandContext(ctx, "cmd.exe", "/c", "call", entry.StopScriptPath), nil
		}
		return exec.CommandContext(ctx, "cmd.exe", "/c", entry.StopCmd), nil
	}
	if entry.StopScriptPath != "" {
		return exec.CommandContext(ctx, "sh", entry.StopScriptPath), nil
	}
	return exec.CommandContext(ctx, "sh", "-c", entry.StopCmd), nil
}

func isPortOpen(port int) bool {
	if port <= 0 {
		return false
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 120*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
