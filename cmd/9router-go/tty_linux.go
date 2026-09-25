//go:build linux

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// hasControllingTerminal reports whether f is backed by a terminal device
// (isatty). Linux spells the termios query TCGETS; /dev/null and other non-tty
// character devices are rejected here.
func hasControllingTerminal(f *os.File) bool {
	_, err := unix.IoctlGetTermios(int(f.Fd()), unix.TCGETS)
	return err == nil
}
