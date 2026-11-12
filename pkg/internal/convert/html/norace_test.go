//go:build !race

package html

// raceEnabled reports a build with the race detector, under which preparing
// a document of [MaxElements] elements takes minutes.
const raceEnabled = false
