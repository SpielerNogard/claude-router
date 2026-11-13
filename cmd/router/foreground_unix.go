//go:build !windows

package main

import "golang.org/x/sys/unix"

// foreground reports whether this process owns the terminal; a backgrounded TUI ("claude-router &") would get SIGTTIN and stop the router.
func foreground(fd uintptr) bool {
	pgrp, err := unix.IoctlGetInt(int(fd), unix.TIOCGPGRP)
	return err == nil && pgrp == unix.Getpgrp()
}
