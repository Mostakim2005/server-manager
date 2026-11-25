//go:build !windows

package supervisor

import (
	"os/exec"
	"syscall"
)

func configureHiddenCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminatePIDTree(pid int) error {
	if pid <= 0 {
		return nil
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		return syscall.Kill(pid, syscall.SIGKILL)
	}
	return nil
}

// ListPortOwners returns an empty map on non-Windows CI test hosts (TCP dial still probes live ports).
func ListPortOwners() map[int][]int {
	return map[int][]int{}
}
