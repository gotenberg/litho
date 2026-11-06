//go:build !race

package text

// raceEnabled reports a build with the race detector, which slows indexing
// past the timing the footprint test asserts.
const raceEnabled = false
