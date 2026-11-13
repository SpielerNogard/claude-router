//go:build !windows

package main

import (
	"os"
	"testing"
)

func TestForegroundFalseForPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if foreground(r.Fd()) {
		t.Error("a pipe has no foreground process group")
	}
}
