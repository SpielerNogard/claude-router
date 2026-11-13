package main

import (
	"bytes"
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

func TestConfirmUpdate(t *testing.T) {
	cases := map[string]bool{"y\n": true, "YES\n": true, " y \n": true, "\n": false, "": false, "n\n": false, "j\n": false}
	for in, want := range cases {
		var out bytes.Buffer
		if got := confirmUpdate(strings.NewReader(in), &out, "0.3.0"); got != want {
			t.Errorf("confirmUpdate(%q) = %v, want %v", in, got, want)
		}
		if out.String() != "Update to v0.3.0? [y/N] " {
			t.Errorf("prompt = %q", out.String())
		}
	}
}

func TestNonReleaseVersionNeverChecks(t *testing.T) {
	for _, v := range []string{"local", "72607f9"} {
		if rel, err := latestRelease(context.Background(), v); rel != nil || err != nil {
			t.Errorf("latestRelease(%q) = %v, %v; want nil, nil", v, rel, err)
		}
		if err := runUpdate(context.Background(), v, io.Discard); err == nil || !strings.Contains(err.Error(), "dev build") {
			t.Errorf("runUpdate(%q) = %v; want dev build error", v, err)
		}
	}
}
