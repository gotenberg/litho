package jpx

// tagTree decodes a two-dimensional array of non-negative integers coded as a
// tag tree (T.800, B.10.2). Each node records the lower bound established so
// far for its value, and whether that bound is the value.
type tagTree struct {
	// levels holds the tree from the leaves up: level l is ceil(w/2^l) by
	// ceil(h/2^l), and the last level is the single root.
	levels []tagLevel
}

// tagLevel is one level of a tag tree.
type tagLevel struct {
	w     int
	low   []int
	known []bool
}

func newTagTree(w, h int) *tagTree {
	t := &tagTree{}
	for {
		t.levels = append(t.levels, tagLevel{w: w, low: make([]int, w*h), known: make([]bool, w*h)})
		if w == 1 && h == 1 {
			return t
		}
		w, h = (w+1)/2, (h+1)/2
	}
}

// bitSource supplies the bits of a packet header.
type bitSource interface {
	bit() (int, error)
}

// decode reports whether the value of leaf (x, y) is below threshold, reading
// from src only the bits T.800, B.10.2 says are needed to answer: questions
// about nodes on the leaf's path that earlier calls answered are not coded
// again. Each 0 bit raises the node's bound by one and a 1 bit fixes its value
// at the bound; a node's value is at least its parent's.
func (t *tagTree) decode(src bitSource, x, y, threshold int) (int, bool, error) {
	low := 0
	for l := len(t.levels) - 1; l >= 0; l-- {
		lv := &t.levels[l]
		i := (y>>l)*lv.w + x>>l
		if lv.low[i] < low {
			lv.low[i] = low
		}
		for !lv.known[i] && lv.low[i] < threshold {
			b, err := src.bit()
			if err != nil {
				return 0, false, err
			}
			if b == 1 {
				lv.known[i] = true
			} else {
				lv.low[i]++
			}
		}
		low = lv.low[i]
		if !lv.known[i] || low >= threshold {
			return 0, false, nil
		}
	}
	return low, true, nil
}
