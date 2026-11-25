//go:build !windows

package security

// JobSupervisor is a no-op stub on non-Windows platforms.
type JobSupervisor struct{}

// NewJobSupervisor creates a stub JobSupervisor on non-Windows systems.
func NewJobSupervisor() (*JobSupervisor, error) {
	return &JobSupervisor{}, nil
}

// AssignPID is a no-op on non-Windows systems.
func (j *JobSupervisor) AssignPID(pid int) error {
	return nil
}

// Close is a no-op on non-Windows systems.
func (j *JobSupervisor) Close() error {
	return nil
}
