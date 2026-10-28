package dct

// bitReader reads the entropy-coded data of a scan (ITU-T T.81, F.1.2.3):
// bytes where 0xFF is followed by a stuffed 0x00, up to the marker that ends
// the scan. Past that marker, or past the end of the data, it reads zero
// bits and counts them in overrun, so a scan cut short can be stopped where
// its data ran out.
type bitReader struct {
	data []byte
	// start is where the scan's data begins, pos the next byte to read, and
	// acc the bits read ahead of the ones consumed, n of them, most
	// significant first.
	start   int
	pos     int
	acc     uint32
	n       int
	stopped bool
	overrun int
}

// fill tops acc up to more than 24 bits.
func (r *bitReader) fill() {
	for r.n <= 24 {
		var b byte
		switch {
		case r.stopped || r.pos >= len(r.data):
			r.stopped = true
		case r.data[r.pos] != 0xFF:
			b = r.data[r.pos]
			r.pos++
		case r.pos+1 < len(r.data) && r.data[r.pos+1] == 0x00:
			b = 0xFF
			r.pos += 2
		default:
			// A marker, or 0xFF at the very end: the scan's data ends here.
			r.stopped = true
		}
		r.acc |= uint32(b) << (24 - r.n)
		r.n += 8
		if r.stopped {
			r.overrun += 8
		}
	}
}

// bit reads one bit.
func (r *bitReader) bit() int {
	if r.n == 0 {
		r.fill()
	}
	b := int(r.acc >> 31) // fits int: one bit
	r.acc <<= 1
	r.n--
	return b
}

// bits reads n bits, at most 16.
func (r *bitReader) bits(n int) int {
	if r.n < n {
		r.fill()
	}
	v := int(r.acc >> (32 - n)) // fits int: at most 16 bits
	r.acc <<= n
	r.n -= n
	return v
}

// exhausted reports whether more bits were read than the scan holds.
func (r *bitReader) exhausted() bool {
	return r.stopped && r.overrun > r.n
}

// restart discards the bits of a partial byte and moves past the restart
// marker that ends the interval (T.81, F.1.2.3 and E.1.4). Bytes before the
// marker that the interval did not use are skipped, which resynchronises a
// damaged interval on the next one.
func (r *bitReader) restart() {
	r.acc, r.n, r.overrun = 0, 0, 0
	r.stopped = false
	for p := r.pos; p+1 < len(r.data); p++ {
		if r.data[p] != 0xFF {
			continue
		}
		n := r.data[p+1]
		switch {
		case n >= 0xD0 && n <= 0xD7:
			r.pos = p + 2
			return
		case n != 0x00 && n != 0xFF:
			// Another marker: the scan ends without the restart.
			r.pos = p
			return
		}
	}
}

// nextMarker returns the position of the marker that ends the scan: the
// first 0xFF followed by a byte that is neither a stuffed 0x00, a restart
// marker nor fill. The search starts a little behind the read position,
// which the read-ahead has passed; entropy-coded data holds no marker, so
// starting early finds the same one.
func (r *bitReader) nextMarker() int {
	for p := max(r.pos-8, r.start); p+1 < len(r.data); p++ {
		if r.data[p] != 0xFF {
			continue
		}
		n := r.data[p+1]
		if n != 0x00 && n != 0xFF && (n < 0xD0 || n > 0xD7) {
			return p
		}
	}
	return len(r.data)
}
