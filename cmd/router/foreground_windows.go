package main

// Windows has no job control, so a terminal process is always in the foreground.
func foreground(uintptr) bool { return true }
