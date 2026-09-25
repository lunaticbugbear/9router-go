//go:build windows

package main

import (
	"golang.org/x/sys/windows"
	"os"
)

// hasControllingTerminal reports whether f is attached to a Windows console.
func hasControllingTerminal(f *os.File) bool {
	var mode uint32
	if err := windows.GetConsoleMode(windows.Handle(f.Fd()), &mode); err != nil {
		return false
	}
	return mode != 0
}
