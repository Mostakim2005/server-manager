//go:build windows

package security

import (
	"fmt"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// JobSupervisor binds spawned server trees to a Windows Job Object configured with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE so child processes terminate cleanly on exit.
type JobSupervisor struct {
	mu     sync.Mutex
	handle windows.Handle
}

// NewJobSupervisor creates a new kill-on-close Windows Job Object.
func NewJobSupervisor() (*JobSupervisor, error) {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create job object: %w", err)
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(
		h,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		_ = windows.CloseHandle(h)
		return nil, fmt.Errorf("configure job object: %w", err)
	}
	return &JobSupervisor{handle: h}, nil
}

// AssignPID attaches a running process PID to the Job Object.
func (j *JobSupervisor) AssignPID(pid int) error {
	if j == nil || pid <= 0 {
		return nil
	}
	j.mu.Lock()
	hJob := j.handle
	j.mu.Unlock()
	if hJob == 0 {
		return nil
	}
	hProc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("open pid %d: %w", pid, err)
	}
	defer windows.CloseHandle(hProc)
	if err := windows.AssignProcessToJobObject(hJob, hProc); err != nil {
		return fmt.Errorf("assign pid %d: %w", pid, err)
	}
	return nil
}

// Close closes the Job Object handle, terminating all assigned processes.
func (j *JobSupervisor) Close() error {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	h := j.handle
	j.handle = 0
	j.mu.Unlock()
	if h != 0 {
		return windows.CloseHandle(h)
	}
	return nil
}
