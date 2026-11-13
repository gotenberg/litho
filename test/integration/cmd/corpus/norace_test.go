//go:build !race

package main

// raceEnabled reports a build with the race detector, under which a
// remediation runs several times slower.
const raceEnabled = false
