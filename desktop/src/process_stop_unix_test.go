//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// TestStopListeningOctopKillsHalfStartedPortablePython covers the case the old
// health-only check could not: a python from ~/.octop/portable that holds the
// port but never became healthy (crash mid-boot, UI killed while starting).
func TestStopListeningOctopKillsHalfStartedPortablePython(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}
	home := t.TempDir()
	t.Setenv("OCTOP_HOME", home)
	script := filepath.Join(home, "portable", "runtime", "bin", "hold_port.py")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "import socket, sys, time\n" +
		"s = socket.socket()\n" +
		"s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)\n" +
		"s.bind(('127.0.0.1', int(sys.argv[1])))\n" +
		"s.listen(8)\n" +
		"time.sleep(300)\n"
	if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	port := freeTCPPort(t)
	cmd := exec.Command(python, script, strconv.Itoa(port))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	waitForListener(t, port)

	stopListeningOctop("127.0.0.1", port)

	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("half-started portable python was not terminated")
	}
	if inUse, err := listenProbe("127.0.0.1", port); err == nil && inUse {
		t.Fatal("port is still in use after cleanup")
	}
}

func waitForListener(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		inUse, err := listenProbe("127.0.0.1", port)
		if err == nil && inUse {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("helper process never listened on the port")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
