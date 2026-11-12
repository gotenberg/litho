package css

import (
	"math"
	"reflect"
	"sync"
)

// Styles is a table of computed styles that lets equal styles be one
// value: [Styles.Share] returns the style the table already holds for a
// value equal to the one it is given. Most boxes of a document share a
// handful of distinct styles, the paragraphs of one class, the runs of one
// character style, the anonymous blocks of one parent, so a document holds
// that handful instead of one [Style] a box.
//
// Two styles are equal when every field holds the same value: numbers bit
// for bit, strings and the elements of slices by content, a nil slice
// apart from an empty one, and pointers by the address they hold. Equal
// styles lay out alike, so sharing one changes no output.
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

// share is [Styles.Share], also reporting whether t adopted st, a style
// its caller then accounts as held.
func (t *Styles) share(st *Style) (*Style, bool) {
	if t == nil || st == nil {
		return st, false
	}
	h := t.hash(st)
	t.mu.Lock()
	defer t.mu.Unlock()
	if old, ok := t.byHash[h]; ok {
		if old == st || equalStyles(old, st) {
			return old, false
		}
		for _, o := range t.more[h] {
			if o == st || equalStyles(o, st) {
				return o, false
			}
		}
		if t.more == nil {
			t.more = map[uint64][]*Style{}
		}
		t.more[h] = append(t.more[h], st)
	} else {
		t.byHash[h] = st
	}
	t.held++
	return st, true
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

// hash returns a hash of st's value consistent with [equalStyles]: equal
// styles hash alike. It reads the properties that most often tell two
// styles apart, not every field, so it stays cheap; styles that differ
// elsewhere alone share a hash and [equalStyles] tells them apart.
func (t *Styles) hash(st *Style) uint64 {
	h := uint64(fnvOffset64)
	dim := func(d Dimension) {
		h = mix(h, uint64(d.Kind))
		h = mix(h, math.Float64bits(d.Value))
		h = mix(h, math.Float64bits(d.Fraction))
	}
	col := func(c Color) {
		h = mix(h, math.Float64bits(c.R))
		h = mix(h, math.Float64bits(c.G))
		h = mix(h, math.Float64bits(c.B))
		h = mix(h, math.Float64bits(c.A))
	}
	h = mix(h, uint64(st.Display)<<16|uint64(st.Position)<<8|uint64(st.Float))
	h = mix(h, math.Float64bits(st.FontSize))
	h = mix(h, uint64(st.FontWeight)<<8|uint64(st.FontStyle)) //nolint:gosec // the bits of the weight, not a size
	for _, f := range st.FontFamilies {
		h = mix(h, uint64(len(f)))
		for i := 0; i < len(f); i++ {
			h = (h ^ uint64(f[i])) * fnvPrime64
		}
	}
	dim(st.Width)
	dim(st.Height)
	dim(st.Left)
	dim(st.Top)
	dim(st.LineHeight)
	dim(st.TextIndent)
	for i := range 4 {
		dim(st.Margin[i])
		dim(st.Padding[i])
		h = mix(h, math.Float64bits(st.Border[i].Width))
	}
	col(st.Color)
	col(st.BackgroundColor)
	h = mix(h, uint64(st.TextAlign)<<8|uint64(st.VerticalAlign.Kind))
	return h
}

// equalStyles reports whether a and b hold equal values, as [Styles]
// defines them.
func equalStyles(a, b *Style) bool {
	return equalValues(reflect.ValueOf(a).Elem(), reflect.ValueOf(b).Elem())
}

// mix folds the word u into the hash h, FNV-1a fashion a word at a time.
func mix(h, u uint64) uint64 {
	return (h ^ u) * fnvPrime64
}

// equalValues reports whether a and b, of one type, hold equal values:
// numbers bit for bit, strings and slice elements by content, a nil slice
// apart from an empty one, pointers by address. A map, func, channel or
// interface is equal to another only when both are nil, so a field of
// such a kind never makes two styles share wrongly.
func equalValues(a, b reflect.Value) bool {
	switch a.Kind() {
	case reflect.Bool:
		return a.Bool() == b.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return a.Int() == b.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return a.Uint() == b.Uint()
	case reflect.Float32, reflect.Float64:
		return math.Float64bits(a.Float()) == math.Float64bits(b.Float())
	case reflect.String:
		return a.String() == b.String()
	case reflect.Array:
		for i := range a.Len() {
			if !equalValues(a.Index(i), b.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Slice:
		if a.IsNil() || b.IsNil() {
			return a.IsNil() == b.IsNil()
		}
		if a.Len() != b.Len() {
			return false
		}
		for i := range a.Len() {
			if !equalValues(a.Index(i), b.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Struct:
		for i := range a.NumField() {
			if !equalValues(a.Field(i), b.Field(i)) {
				return false
			}
		}
		return true
	case reflect.Complex64, reflect.Complex128:
		x, y := a.Complex(), b.Complex()
		return math.Float64bits(real(x)) == math.Float64bits(real(y)) && math.Float64bits(imag(x)) == math.Float64bits(imag(y))
	case reflect.Pointer:
		return a.Pointer() == b.Pointer()
	case reflect.Map, reflect.Func, reflect.Chan, reflect.Interface, reflect.UnsafePointer:
		return a.IsNil() && b.IsNil()
	case reflect.Invalid:
	}
	return false
}
