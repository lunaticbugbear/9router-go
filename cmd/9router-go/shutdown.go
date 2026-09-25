package main

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// shutdownTrigger turns "we want to stop" into a broadcast so every interested
// party (the TTY menu blocking on stdin, the non-TTY babysitting wait) can
// observe the same single event. A buffered channel receive cannot do this:
// whoever reads first would consume the signal, leaving the other waiting for a
// second one — the bug that made a lone SIGTERM fail to stop the gateway.
//
// The first stop request (SIGINT/SIGTERM, or the TTY menu's Exit) closes Done.
// A second signal force-quits immediately, matching the historical behavior
// where a stuck drain could be escaped with a second ^C.
type shutdownTrigger struct {
	once     sync.Once
	done     chan struct{}
	signals  chan os.Signal
	notified bool
	mu       sync.Mutex
}

// newShutdownTrigger creates a trigger ready to be armed.
func newShutdownTrigger() *shutdownTrigger {
	return &shutdownTrigger{
		done:    make(chan struct{}),
		signals: make(chan os.Signal, 2),
	}
}

// Done is closed once the first stop request is observed.
func (s *shutdownTrigger) Done() <-chan struct{} {
	return s.done
}

// Request stops the gateway without a signal (the TTY menu's Exit path).
// It converges on the same shutdown the SIGTERM path uses.
func (s *shutdownTrigger) Request() {
	s.once.Do(func() { close(s.done) })
}

// Arm installs the OS signal handler and starts the watchdog. It is called once,
// before the fx app starts, so a stop request arriving mid-startup is not lost.
// When forceQuit is non-nil, a second signal calls it instead of exiting; tests
// use this to observe the escalation without killing the test process.
func (s *shutdownTrigger) Arm(forceQuit func()) {
	if forceQuit == nil {
		forceQuit = func() {
			fmt.Fprintln(os.Stdout, "\n  Force quitting...")
			os.Exit(1)
		}
	}

	s.mu.Lock()
	if s.notified {
		s.mu.Unlock()
		return
	}
	s.notified = true
	s.mu.Unlock()

	signal.Notify(s.signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-s.signals
		s.Request()
		<-s.signals
		forceQuit()
	}()
}
