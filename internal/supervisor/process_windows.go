//go:build windows

package supervisor

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

func configureHiddenCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NO_WINDOW | windows.CREATE_NEW_PROCESS_GROUP,
	}
}

func terminatePIDTree(pid int) error {
	if pid <= 0 {
		return nil
	}
	c := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
	configureHiddenCmd(c)
	return c.Run()
}

// ListPortOwners inspects Windows TCP listening ports via netstat -ano -p TCP.
func ListPortOwners() map[int][]int {
	res := make(map[int][]int)
	c := exec.Command("netstat", "-ano", "-p", "TCP")
	configureHiddenCmd(c)
	out, err := c.Output()
	if err != nil {
		return res
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		if !strings.EqualFold(fields[3], "LISTENING") {
			continue
		}
		idx := strings.LastIndex(fields[1], ":")
		if idx < 0 {
			continue
		}
		port, err1 := strconv.Atoi(fields[1][idx+1:])
		pid, err2 := strconv.Atoi(fields[4])
		if err1 == nil && err2 == nil && port > 0 && pid > 0 {
			res[port] = append(res[port], pid)
		}
	}
	return res
}
