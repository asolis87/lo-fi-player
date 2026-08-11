package main

import "errors"

// errStubTUI is a sentinel used by play_test.go to simulate a
// failing Bubble Tea program run.
var errStubTUI = errors.New("stub: simulated TUI failure")
