package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const pidFileName = "router.pid"

var errNotRunning = errors.New("no running claude-router found")

func errorsIsNotRunning(err error) bool { return errors.Is(err, errNotRunning) }

func pidFilePath(dataDir string) string { return filepath.Join(dataDir, pidFileName) }

func writePidFile(dataDir string) error {
	return os.WriteFile(pidFilePath(dataDir), []byte(strconv.Itoa(os.Getpid())), 0o600)
}

func removePidFile(dataDir string) {
	if err := os.Remove(pidFilePath(dataDir)); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "removing PID file: %v\n", err)
	}
}

func readPidFile(dataDir string) (int, error) {
	raw, err := os.ReadFile(pidFilePath(dataDir))
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return 0, fmt.Errorf("%s does not contain a process ID: %w", pidFilePath(dataDir), err)
	}
	return pid, nil
}

// Signal 0 triggers nothing; it only checks whether the PID is still in use.
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

// resolveStopTarget also removes a stale PID file left behind by a hard kill.
func resolveStopTarget(dataDir string) (int, error) {
	pid, err := readPidFile(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, fmt.Errorf("%w (no %s)", errNotRunning, pidFilePath(dataDir))
		}
		return 0, err
	}
	if !processAlive(pid) {
		removePidFile(dataDir)
		return 0, fmt.Errorf("%w (process %d is no longer running, removed stale PID file)", errNotRunning, pid)
	}
	return pid, nil
}
