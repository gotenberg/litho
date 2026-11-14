package dictbreak

// A Dictionary is a word list packed as a minimal deterministic acyclic
// automaton: one state per set of word endings, its edges labelled by
// letters, a word read by following its letters from the start state.
//
// Edges are four bytes each, a state's edges side by side in label
// order and the start state's first. Byte 0 holds the label, the letter
// less Base, in its low seven bits, and in its high bit whether the edge
// is its state's last. Byte 1 holds in its high bit whether a word ends
// after the edge, and in its low seven bits the high bits of the target,
// whose lower sixteen bits bytes 2 and 3 hold, high byte first: the
// index of the first edge of the state the edge leads to, 0 for a state
// with no edges.
type Dictionary struct {
	// Base is the letter one below the lowest a label stands for.
	Base rune
	// Edges are the packed edges.
	Edges string
}

// edgeSize is the size of one packed edge in bytes.
const edgeSize = 4

// maxLabel is the highest label an edge holds: seven bits.
const maxLabel = 0x7f

// Prefixes writes to lens, in ascending order, the lengths of the words
// of d that text starts with, at most len(lens) of them, and returns how
// many it wrote and reach: the length of the longest start of text that
// starts a word of d, a whole word or not.
func (d *Dictionary) Prefixes(text []rune, lens []int) (n, reach int) {
	if d == nil || len(d.Edges) < edgeSize {
		return 0, 0
	}
	state := 0
	for i, r := range text {
		label := r - d.Base
		if label <= 0 || label > maxLabel {
			break
		}
		e, ok := d.find(state, byte(label))
		if !ok {
			break
		}
		reach = i + 1
		if d.Edges[e+1]&0x80 != 0 && n < len(lens) {
			lens[n] = i + 1
			n++
		}
		state = d.target(e)
		if state == 0 {
			break
		}
	}
	return n, reach
}

// find returns the offset of the edge of the state whose edges start at
// edge index state labelled label.
func (d *Dictionary) find(state int, label byte) (int, bool) {
	for off := state * edgeSize; off+edgeSize <= len(d.Edges); off += edgeSize {
		b := d.Edges[off]
		switch l := b & maxLabel; {
		case l == label:
			return off, true
		case l > label:
			return 0, false
		}
		if b&0x80 != 0 {
			break
		}
	}
	return 0, false
}

// target returns the edge index of the state the edge at off leads to.
func (d *Dictionary) target(off int) int {
	return int(d.Edges[off+1]&0x7f)<<16 | int(d.Edges[off+2])<<8 | int(d.Edges[off+3])
}
