package ccitt

import "fmt"

// bitReader reads data most significant bit first.
type bitReader struct {
	data []byte
	pos  int
}

// bit returns the next bit, and false past the end of the data.
func (r *bitReader) bit() (int, bool) {
	if r.pos >= len(r.data)*8 {
		return 0, false
	}
	b := int(r.data[r.pos>>3]>>(7-r.pos&7)) & 1
	r.pos++
	return b, true
}

// align moves to the next byte boundary.
func (r *bitReader) align() {
	r.pos = (r.pos + 7) &^ 7
}

// zeros counts the zero bits from the current position, up to limit, and
// reports whether a one bit follows them within the data.
func (r *bitReader) zeros(limit int) (int, bool) {
	n := 0
	for p := r.pos; p < len(r.data)*8 && n < limit; p++ {
		if r.data[p>>3]>>(7-p&7)&1 == 1 {
			return n, true
		}
		n++
	}
	return n, false
}

// endOfLine consumes an end-of-line code, eleven or more zero bits then a one
// (T.4, 4.1.2: fill bits of any length may precede it), and reports whether
// one was there.
func (r *bitReader) endOfLine() bool {
	n, one := r.zeros(1 << 30)
	if !one || n < 11 {
		return false
	}
	r.pos += n + 1
	return true
}

// more reports whether a row can still begin here. Twelve zero bits with no
// end-of-line code are padding: every code T.4 and T.6 define holds a one bit
// within its first eight.
func (r *bitReader) more() bool {
	n, one := r.zeros(12)
	return one && n < 12
}

// decode reads one code of t.
func (r *bitReader) decode(t *tree) (int, error) {
	n := 0
	for {
		b, ok := r.bit()
		if !ok {
			return 0, fmt.Errorf("%w: data ends inside a code", ErrMalformed)
		}
		n = int(t.next[n][b])
		if n < 0 {
			return 0, fmt.Errorf("%w: invalid code at bit %d", ErrMalformed, r.pos)
		}
		if t.leaf[n] {
			return int(t.value[n]), nil
		}
	}
}

// run reads one run length of the given colour: make-up codes, then the
// terminating code that ends every run (T.4, 4.1.1).
func (r *bitReader) run(black bool) (int, error) {
	t := whiteTree
	if black {
		t = blackTree
	}
	total := 0
	for {
		v, err := r.decode(t)
		if err != nil {
			return 0, err
		}
		total += v
		if v < 64 {
			return total, nil
		}
		if total > maxColumns {
			return 0, fmt.Errorf("%w: run of more than %d pixels", ErrMalformed, maxColumns)
		}
	}
}
