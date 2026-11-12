//go:build !unix

package main

// costRusage is the CPU time and peak resident memory of this process so
// far.
type costRusage struct {
	user, sys float64
	maxRSS    int64
}

// readCostRusage returns zero: the OS reports no resource usage here.
func readCostRusage() costRusage { return costRusage{} }
