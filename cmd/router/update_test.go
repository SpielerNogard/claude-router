package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"testing"
)

func TestLatestReleaseSkipsDevBuild(t *testing.T) {
	rel, err := latestRelease(context.Background(), "dev")
	if rel != nil || err != nil {
		t.Errorf("latestRelease(dev) = %v, %v; want nil, nil", rel, err)
	}
}

func TestRunUpdateRefusesDevBuild(t *testing.T) {
	err := runUpdate(context.Background(), "dev", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "dev build") {
		t.Errorf("runUpdate(dev) = %v; want error mentioning dev build", err)
	}
}

func TestPermissionHint(t *testing.T) {
	perm := fmt.Errorf("replace: %w", &fs.PathError{Op: "open", Path: "/usr/local/bin/claude-router", Err: fs.ErrPermission})
	got := permissionHint(perm)
	if !strings.Contains(got.Error(), "sudo claude-router update") {
		t.Errorf("hint missing: %v", got)
	}
	if !errors.Is(got, fs.ErrPermission) {
		t.Error("hint dropped the wrapped permission error")
	}
	other := errors.New("boom")
	if permissionHint(other) != other {
		t.Error("non-permission error was changed")
	}
	if permissionHint(nil) != nil {
		t.Error("nil error became non-nil")
	}
}
