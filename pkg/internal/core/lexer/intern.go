package lexer

// internSlots is the number of texts [Lexer.WithInterning] remembers. A
// power of two, so a text's slot is its hash masked.
const internSlots = 256

// maxInternedLen is the longest text [Lexer.WithInterning] remembers. The
// operators, numbers and resource names a content stream repeats are short;
// a longer text is copied for its token as it is without interning.
const maxInternedLen = 32

// WithInterning has the lexer hand out one copy of each short keyword, number
// and name it reads again and again, rather than a new copy for each token:
// it remembers the last text read into each of [internSlots] slots, chosen by
// a hash of the text, and returns the remembered copy when the bytes read
// match it. A token's text is the same either way. Interning costs a table of
// [internSlots] strings, which pays for itself on a stream that repeats its
// operators and operands, as a content stream does, and not on one object.
func (l *Lexer) WithInterning() *Lexer {
	l.interned = new([internSlots]string)
	return l
}

// text returns b as a string: the copy the lexer remembers for those bytes,
// under [Lexer.WithInterning], or a new one.
func (l *Lexer) text(b []byte) string {
	if l.interned == nil || len(b) > maxInternedLen {
		return string(b)
	}
	// FNV-1a.
	h := uint32(2166136261)
	for _, c := range b {
		h ^= uint32(c)
		h *= 16777619
	}
	slot := &l.interned[h&(internSlots-1)]
	if *slot == string(b) {
		return *slot
	}
	*slot = string(b)
	return *slot
}
