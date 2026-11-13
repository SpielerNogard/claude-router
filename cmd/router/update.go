package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/creativeprojects/go-selfupdate"
	"github.com/spf13/cobra"

	"github.com/SpielerNogard/claude-router/internal/config"
)

const updateRepo = "SpielerNogard/claude-router"

func newUpdater() (*selfupdate.Updater, error) {
	return selfupdate.NewUpdater(selfupdate.Config{
		Validator: &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
	})
}

// latestRelease returns nil when there is nothing to install; dev builds never ask GitHub.
func latestRelease(ctx context.Context, current string) (*selfupdate.Release, error) {
	if current == "dev" {
		return nil, nil
	}
	up, err := newUpdater()
	if err != nil {
		return nil, err
	}
	rel, found, err := up.DetectLatest(ctx, selfupdate.ParseSlug(updateRepo))
	if err != nil {
		return nil, err
	}
	if !found || !rel.GreaterThan(current) {
		return nil, nil
	}
	return rel, nil
}

func applyUpdate(ctx context.Context, rel *selfupdate.Release) error {
	exe, err := selfupdate.ExecutablePath()
	if err != nil {
		return err
	}
	up, err := newUpdater()
	if err != nil {
		return err
	}
	return permissionHint(up.UpdateTo(ctx, rel, exe))
}

func permissionHint(err error) error {
	if errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf("%w\nno write access to the binary — try: sudo claude-router update", err)
	}
	return err
}

func runUpdate(ctx context.Context, current string, out io.Writer) error {
	if current == "dev" {
		return errors.New("this is a dev build — install a release binary to use update")
	}
	rel, err := latestRelease(ctx, current)
	if err != nil {
		return err
	}
	if rel == nil {
		fmt.Fprintf(out, "already up to date (v%s)\n", current)
		return nil
	}
	if err := applyUpdate(ctx, rel); err != nil {
		return err
	}
	fmt.Fprintf(out, "updated v%s → v%s\nrestart a running router to use the new version\n", current, rel.Version())
	return nil
}

func newUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "update",
		Short:         "update claude-router to the latest release",
		Long:          "Downloads the latest release from GitHub, verifies it against checksums.txt and replaces this binary.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate(cmd.Context(), version, cmd.OutOrStdout())
		},
	}
}

func confirmUpdate(in io.Reader, out io.Writer, latest string) bool {
	fmt.Fprintf(out, "Update to v%s? [y/N] ", latest)
	line, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}

// startupUpdateCheck runs before the port is bound, so a re-exec never collides with our own listener or PID file.
func startupUpdateCheck(cfg *config.Config, interactive bool) {
	if !cfg.UpdateCheckEnabled() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	rel, err := latestRelease(ctx, version)
	cancel()
	if err != nil || rel == nil {
		return
	}
	if !interactive {
		log.Printf("v%s available – run claude-router update", rel.Version())
		return
	}
	if !confirmUpdate(os.Stdin, os.Stdout, rel.Version()) {
		return
	}
	if err := applyUpdate(context.Background(), rel); err != nil {
		log.Printf("update failed: %v — starting v%s", err, version)
		return
	}
	if exe, err := selfupdate.ExecutablePath(); err == nil {
		syscall.Exec(exe, os.Args, os.Environ())
	}
	// Only reached when re-exec is unsupported (Windows) or failed.
	fmt.Printf("updated v%s → v%s – please restart claude-router\n", version, rel.Version())
	os.Exit(0)
}
