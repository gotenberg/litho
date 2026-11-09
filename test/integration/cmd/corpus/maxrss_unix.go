//go:build unix

package main

import (
	"os"
	"runtime"
	"syscall"
)

// maxRSS returns the peak resident set size of an exited process, in bytes,
// or 0 when the OS does not report it.
func maxRSS(ps *os.ProcessState) int64 {
	ru, ok := ps.SysUsage().(*syscall.Rusage)
	if !ok {
		return 0
	}
	// getrusage(2) reports ru_maxrss in bytes on Darwin, in kilobytes on
	// Linux and the BSDs.
	if runtime.GOOS == "darwin" || runtime.GOOS == "ios" {
		return ru.Maxrss
	}
	return ru.Maxrss * 1024
}
