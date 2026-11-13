package css

import (
	"slices"
	"sync"
)

//go:generate go run ../../../cmd/gen-style-equal

// Styles is a table of computed styles that lets equal styles be one
// value: [Styles.Share] returns the style the table already holds for a
// value equal to the one it is given. Most boxes of a document share a
// handful of distinct styles, the paragraphs of one class, the runs of one
// character style, the anonymous blocks of one parent, so a document holds
// that handful instead of one [Style] a box.
//
// Two styles are equal when every field holds the same value: numbers bit
// for bit, strings and the elements of slices by content, a nil slice
// apart from an empty one, pointers by the address they hold, and a map,
// func, channel or interface as equal to another only when both are nil.
// Equal styles lay out alike, so sharing one changes no output. The
// comparison and the hash are generated from the fields of [Style]
// (cmd/gen-style-equal): a new field joins both.
//
// A shared style is read and never written: the code that shares a style
// gives up writing it, and code that needs another value copies it first.
//
// A table lives as long as the styles of the tree it shares, one
// conversion's at most, and is safe for the goroutines of that
// conversion. The nil *Styles shares nothing: [Styles.Share] returns its
// argument.
type Styles struct {
	mu sync.Mutex
	// byHash holds the first style of each hash; more holds the others
	// of a hash two unequal styles share, which almost never happens.
	byHash map[uint64]*Style
	more   map[uint64][]*Style
	held   int
}

// NewStyles returns an empty table.
func NewStyles() *Styles {
	return &Styles{byHash: map[uint64]*Style{}}
}

// Share returns the style t holds equal to st, or adopts st and returns
// it when t holds none. The caller writes neither st nor the result after
// the call. A nil st or a nil t returns st.
func (t *Styles) Share(st *Style) *Style {
	shared, _ := t.share(st)
	return shared
}

// ShareCopy returns the style t holds equal to st, or adopts a copy of st
// and returns the copy when t holds none: st stays the caller's, to write
// again, and costs no copy when t holds its value. The caller writes the
// result no more. A nil st returns nil; a nil t returns a copy of st.
func (t *Styles) ShareCopy(st *Style) *Style {
	shared, _ := t.shareCopy(st)
	return shared
}

// share is [Styles.Share], also reporting whether t adopted st, a style
// its caller then accounts as held.
func (t *Styles) share(st *Style) (*Style, bool) {
	if st == nil || t == nil {
		return st, false
	}
	h := t.hash(st)
	t.mu.Lock()
	defer t.mu.Unlock()
	if old := t.lookup(h, st); old != nil {
		return old, false
	}
	t.hold(h, st)
	return st, true
}

// shareCopy is [Styles.ShareCopy], also reporting whether t adopted a
// copy. It only reads st, so a style the caller keeps on its stack stays
// there.
func (t *Styles) shareCopy(st *Style) (*Style, bool) {
	if st == nil {
		return nil, false
	}
	if t == nil {
		c := new(Style)
		*c = *st
		return c, false
	}
	h := t.hash(st)
	t.mu.Lock()
	defer t.mu.Unlock()
	if old := t.lookup(h, st); old != nil {
		return old, false
	}
	c := new(Style)
	*c = *st
	t.hold(h, c)
	return c, true
}

// lookup returns the style t holds equal to st, whose hash is h, nil for
// none. The caller holds t.mu.
func (t *Styles) lookup(h uint64, st *Style) *Style {
	old, found := t.byHash[h]
	if !found {
		return nil
	}
	if old == st || equalStyle(old, st) {
		return old
	}
	for _, o := range t.more[h] {
		if o == st || equalStyle(o, st) {
			return o
		}
	}
	return nil
}

// hold adopts st, whose hash is h and which t holds no equal of. The
// caller holds t.mu.
func (t *Styles) hold(h uint64, st *Style) {
	if _, found := t.byHash[h]; found {
		if t.more == nil {
			t.more = map[uint64][]*Style{}
		}
		t.more[h] = append(t.more[h], st)
	} else {
		t.byHash[h] = st
	}
	t.held++
}

// Intact reports whether every style t holds still has the value it was
// adopted with, as far as its hash tells: a style written after it was
// shared, which every node holding it would see, makes it false. Tests
// call it on the table of a finished conversion.
func (t *Styles) Intact() bool {
	if t == nil {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for h, st := range t.byHash {
		if t.hash(st) != h {
			return false
		}
	}
	for h, more := range t.more {
		for _, st := range more {
			if t.hash(st) != h {
				return false
			}
		}
	}
	return true
}

// holds reports whether t holds st itself, not only a style equal to it.
func (t *Styles) holds(st *Style) bool {
	if t == nil || st == nil {
		return false
	}
	h := t.hash(st)
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.byHash[h] == st {
		return true
	}
	return slices.Contains(t.more[h], st)
}

// Len returns the number of distinct styles t holds.
func (t *Styles) Len() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.held
}

// hash returns a hash of st's value consistent with [equalStyle]: equal
// styles hash alike. It folds every field, so styles that differ almost
// never share a hash.
func (t *Styles) hash(st *Style) uint64 {
	return hashStyle(fnvOffset64, st)
}

// mix folds the word u into the hash h, FNV-1a fashion a word at a time.
func mix(h, u uint64) uint64 {
	return (h ^ u) * fnvPrime64
}

// mixBool folds b into the hash h.
func mixBool(h uint64, b bool) uint64 {
	if b {
		return mix(h, 1)
	}
	return mix(h, 0)
}

// mixString folds the length and the bytes of s into the hash h.
func mixString(h uint64, s string) uint64 {
	h = mix(h, uint64(len(s)))
	for i := 0; i < len(s); i++ {
		h = (h ^ uint64(s[i])) * fnvPrime64
	}
	return h
}
