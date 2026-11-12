//go:build unix

package main

import (
	"runtime"
	"syscall"
	"time"
)

// costRusage is the CPU time and peak resident memory of this process so
// far.
type costRusage struct {
	user, sys float64
	maxRSS    int64
}

// readCostRusage returns this process's resource usage, zero when the OS
// does not report it.
func readCostRusage() costRusage {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return costRusage{}
	}
	rss := ru.Maxrss
	// getrusage(2) reports ru_maxrss in bytes on Darwin, in kilobytes on
	// Linux and the BSDs.
	if runtime.GOOS != "darwin" && runtime.GOOS != "ios" {
		rss *= 1024
	}
	return costRusage{
		user:   time.Duration(ru.Utime.Nano()).Seconds(),
		sys:    time.Duration(ru.Stime.Nano()).Seconds(),
		maxRSS: rss,
	}
}
