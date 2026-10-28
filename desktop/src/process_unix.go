//go:build !windows

package main

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func configureProcGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcessTree(cmd *exec.Cmd) {
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	_ = cmd.Wait()
}

func listeningPIDs(port int) []int {
	out, err := exec.Command(
		"lsof", "-nP",
		"-iTCP:"+strconv.Itoa(port),
		"-sTCP:LISTEN",
		"-t",
	).Output()
	if err != nil {
		return nil
	}
	var pids []int
	seen := map[int]struct{}{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pid, err := strconv.Atoi(line)
		if err != nil || pid <= 0 {
			continue
		}
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}
		pids = append(pids, pid)
	}
	return pids
}

func terminatePID(pid int) {
	// Prefer the process group (portable launch uses Setpgid); fall back to the pid.
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	_ = syscall.Kill(pid, syscall.SIGTERM)
}

// processCommandLine returns argv joined by spaces (macOS/Linux via ps).
func processCommandLine(pid int) string {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
