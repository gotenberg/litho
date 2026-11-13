package css

// InitialLetter is the initial-letter property (CSS Inline 3, 4.3): the
// size of an initial letter in lines and the lines it sinks. Size 0 is
// normal, no initial letter; otherwise Size is at least 1 and Sink at
// least 1.
//
// The property applies to a ::first-letter alone, as Chromium applies
// it: CSS Inline 3, 4.3.1 lets a first-most inline box and an inside
// ::marker take it too. initial-letter-align and initial-letter-wrap are
// not read, as Chromium does not parse them.
type InitialLetter struct {
	Size float64
	Sink int
}

// initialLetterProps returns the initial-letter property, not inherited.
func initialLetterProps() map[propID]propDef {
	return map[propID]propDef{
		pInitialLetter: {name: "initial-letter", parse: parseInitialLetter, compute: func(_ *styler, st *Style, v any) {
			if l, ok := v.(InitialLetter); ok {
				st.InitialLetter = l
			}
		}, copyFrom: field(func(s *Style) *InitialLetter { return &s.InitialLetter })},
	}
}

// parseInitialLetter parses initial-letter (CSS Inline 3, 4.3): normal, a
// number of at least 1 followed by an integer of at least 1, or a number
// of at least 1 and, in either order, drop or raise. drop, and a missing
// sink, sink the size floored; raise sinks 1.
func parseInitialLetter(v []token, _ *declCtx) (any, bool) {
	if k, ok := keyword(v); ok && k == "normal" {
		return InitialLetter{}, true
	}
	if len(v) == 0 || len(v) > 2 {
		return nil, false
	}
	size, sink, kw := 0.0, 0, ""
	for i, t := range v {
		if t.typ == tIdent {
			k := lower(t.value)
			if kw != "" || k != "drop" && k != "raise" {
				return nil, false
			}
			kw = k
			continue
		}
		if size == 0 && (i == 0 || kw != "") {
			n, ok := parseNumber(t)
			if !ok || n < 1 {
				return nil, false
			}
			size = n
			continue
		}
		n, ok := parseInteger(t)
		if !ok || n < 1 || kw != "" || size == 0 {
			return nil, false
		}
		sink = n
	}
	if size == 0 {
		return nil, false
	}
	switch {
	case kw == "raise":
		sink = 1
	case sink == 0:
		sink = int(min(size, MaxInteger)) // fits int: floored, at most MaxInteger
	}
	return InitialLetter{Size: size, Sink: sink}, true
}
