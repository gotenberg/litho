package compliance

import "cmp"

// lowestOffender picks one representative out of a set of rule violations so
// that the same file always reports the same one.
//
// Several rules fire once per font or per object while the thing that breaks
// them is one of many: a CID absent from /CIDSet, a glyph missing from
// /CharSet, a code mapping to .notdef. Those sets are Go maps, whose iteration
// order is randomized, so returning on the first offender the loop happened to
// meet made the reported value change between runs of the same bytes. The
// verdict never moved, but the message did, which makes a litho-vs-veraPDF
// clause diff unrepeatable and breaks the promise that identical input yields
// identical output.
//
// Taking the lowest offender is deterministic, costs one comparison per
// candidate, and needs no sort or allocation on what is a hot per-page path.
type lowestOffender[T cmp.Ordered] struct {
	value T
	found bool
}

// offer records v when it is the smallest offender seen so far.
func (l *lowestOffender[T]) offer(v T) {
	if !l.found || v < l.value {
		l.value, l.found = v, true
	}
}

// result returns the lowest offered value, and whether anything was offered.
func (l *lowestOffender[T]) result() (T, bool) {
	return l.value, l.found
}
