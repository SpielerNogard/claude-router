package main

import (
	"fmt"
	"os"
	"runtime"
	"syscall"

	"github.com/SpielerNogard/claude-router/internal/config"
)

func runStop(dataDir string) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("stop is not supported on Windows: there is no SIGTERM,\n" +
			"and a hard kill would bypass the clean shutdown.\n" +
			"Stop the router in the foreground with Ctrl+C instead")
	}
	pid, err := resolveStopTarget(dataDir)
	if err != nil {
		// No running router is not an error — otherwise it gets in the way in scripts.
		if errorsIsNotRunning(err) {
			fmt.Println(paint(styleDim, err.Error()))
			return nil
		}
		return err
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("process %d: %w", pid, err)
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("process %d: %w", pid, err)
	}
	fmt.Printf("stopping claude-router (process %s).\n", paint(styleName, fmt.Sprint(pid)))
	return nil
}

func runCheckCmd(configPath, only string) error {
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return fmt.Errorf("%s", configMissingMessage(configPath))
	}
	// Unvalidated on purpose: check should report the errors that would block startup.
	cfg, err := config.LoadUnvalidated(configPath)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "%s %v\n\n",
			paint(styleSkip, "Note: the router would not start with this configuration:"), err)
	}
	if runCheck(cfg, only, os.Stdout) {
		return nil
	}
	return failedError{fmt.Errorf("at least one provider failed")}
}
