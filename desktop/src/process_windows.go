//go:build windows

package main

import (
	"io"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
}

func configureProcGroup(cmd *exec.Cmd) {
	hideConsole(cmd)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
}

func killProcessTree(cmd *exec.Cmd) {
	kill := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid))
	hideConsole(kill)
	_ = kill.Run()
}

func listeningPIDs(port int) []int {
	out, err := exec.Command("netstat", "-ano", "-p", "tcp").Output()
	if err != nil {
		return nil
	}
	suffix := ":" + strconv.Itoa(port)
	var pids []int
	seen := map[int]struct{}{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		if !strings.EqualFold(fields[3], "LISTENING") {
			continue
		}
		local := fields[1]
		if !strings.HasSuffix(local, suffix) {
			continue
		}
		pid, err := strconv.Atoi(fields[4])
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
	kill := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
	hideConsole(kill)
	_ = kill.Run()
}

// jobHandle is created once and intentionally never closed: Windows terminates
// every process in a job when its last handle closes, which is exactly the
// guarantee we want — the shell dies (quit, crash, Task Manager "end task"),
// the Octop server dies with it. Without this the server survives the shell,
// keeps holding 8088/8089 and the next launch attaches to the old runtime.
var jobHandle windows.Handle

func adoptProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if jobHandle == 0 {
		handle, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			return
		}
		limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
		limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		if _, err := windows.SetInformationJobObject(
			handle,
			windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&limits)),
			uint32(unsafe.Sizeof(limits)),
		); err != nil {
			_ = windows.CloseHandle(handle)
			return
		}
		jobHandle = handle
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return
	}
	defer func() { _ = windows.CloseHandle(process) }()
	_ = windows.AssignProcessToJobObject(jobHandle, process)
}

// processCommandLine returns the full command line for pid. wmic is fast and
// present on Win10/11; PowerShell CIM is the fallback when it is deprecated or
// removed. Both failing means "unknown", which keeps the caller from killing
// anything it cannot prove is ours.
func processCommandLine(pid int) string {
	id := strconv.Itoa(pid)
	wmic := exec.Command("wmic", "process", "where", "processid="+id, "get", "commandline", "/value")
	hideConsole(wmic)
	if out, err := wmic.Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			value, ok := strings.CutPrefix(line, "CommandLine=")
			if ok && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	ps := exec.Command(
		"powershell", "-NoProfile", "-NonInteractive", "-Command",
		"(Get-CimInstance Win32_Process -Filter \"ProcessId="+id+"\").CommandLine",
	)
	hideConsole(ps)
	out, err := ps.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
