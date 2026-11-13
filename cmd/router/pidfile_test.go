package main

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestPidFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := writePidFile(dir); err != nil {
		t.Fatal(err)
	}
	pid, err := readPidFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if pid != os.Getpid() {
		t.Errorf("pid = %d, want %d", pid, os.Getpid())
	}
	removePidFile(dir)
	if _, err := readPidFile(dir); err == nil {
		t.Error("no file may be readable after removePidFile")
	}
}

func TestReadPidFileMissing(t *testing.T) {
	if _, err := readPidFile(t.TempDir()); err == nil {
		t.Fatal("a missing file must return an error")
	}
}

func TestReadPidFileGarbage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, pidFileName), []byte("not a pid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPidFile(dir); err == nil {
		t.Fatal("unreadable content must return an error")
	}
}

func TestProcessAlive(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Error("the own process must count as alive")
	}
	// Far above any common pid_max limit, practically never exists.
	if processAlive(4_000_000) {
		t.Error("an unused process ID must not count as alive")
	}
}

func TestResolveStopTargetReportsNoRouter(t *testing.T) {
	dir := t.TempDir()
	_, err := resolveStopTarget(dir)
	if err == nil {
		t.Fatal("without a PID file a notice must be returned")
	}
	if !errorsIsNotRunning(err) {
		t.Errorf("err = %v, want errNotRunning", err)
	}
}

func TestResolveStopTargetCleansStaleFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, pidFileName), []byte(strconv.Itoa(4_000_000)), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := resolveStopTarget(dir)
	if !errorsIsNotRunning(err) {
		t.Fatalf("err = %v, want errNotRunning", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, pidFileName)); !os.IsNotExist(statErr) {
		t.Error("the stale PID file should have been removed")
	}
}

func TestResolveStopTargetFindsLiveProcess(t *testing.T) {
	dir := t.TempDir()
	if err := writePidFile(dir); err != nil {
		t.Fatal(err)
	}
	pid, err := resolveStopTarget(dir)
	if err != nil {
		t.Fatal(err)
	}
	if pid != os.Getpid() {
		t.Errorf("pid = %d, want %d", pid, os.Getpid())
	}
}

func TestBindKeepsPidFileWhenPortBusy(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	dir := t.TempDir()
	if err := os.WriteFile(pidFilePath(dir), []byte("4242"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := bindAndWritePid(busy.Addr().String(), dir); err == nil {
		t.Fatal("binding a busy port succeeded")
	}
	if b, _ := os.ReadFile(pidFilePath(dir)); string(b) != "4242" {
		t.Errorf("PID file of the running router was overwritten with %q", b)
	}
}

func TestBindWritesPidFile(t *testing.T) {
	dir := t.TempDir()
	ln, err := bindAndWritePid("127.0.0.1:0", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if b, _ := os.ReadFile(pidFilePath(dir)); string(b) != strconv.Itoa(os.Getpid()) {
		t.Errorf("PID file = %q", b)
	}
}
