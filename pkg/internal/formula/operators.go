package formula

import (
	"sort"
	"strings"
	"unicode/utf16"
)

// form is the form of an embellished operator (MathML Core, 3.2.4.2).
type form uint8

// The forms.
const (
	infix form = iota
	prefix
	postfix
)

// category is a category of the operator dictionary (MathML Core, B.1,
// Figure 26).
type category uint8

// The categories of Figure 26. Default and ForceDefault share their
// values; L and M have no encoding.
const (
	catDefault category = iota
	catForceDefault
	catA
	catB
	catC
	catD
	catE
	catF
	catG
	catH
	catI
	catJ
	catK
	catL
	catM
)

// categoryOfEncoding maps the 4-bit encoding of Figure 26 to its
// category.
var categoryOfEncoding = [16]category{
	0x0: catA, 0x4: catB, 0x8: catC, 0x1: catD, 0x2: catE, 0x5: catF,
	0x6: catG, 0x9: catH, 0xA: catI, 0xD: catJ, 0xC: catK,
}

// operatorDefaults are the values Figure 26 of MathML Core, B.1 gives a
// category: lspace and rspace in em, and the properties it lists.
type operatorDefaults struct {
	lspace, rspace                              float64
	stretchy, symmetric, largeop, movablelimits bool
}

// Spaces of Figure 26, in em.
const (
	thickSpace  = 0.2777777777777778
	mediumSpace = 0.2222222222222222
	thinSpace   = 0.16666666666666666
)

// categoryDefaults is Figure 26 of MathML Core, B.1.
var categoryDefaults = [...]operatorDefaults{
	catDefault:      {lspace: thickSpace, rspace: thickSpace},
	catForceDefault: {lspace: thickSpace, rspace: thickSpace},
	catA:            {lspace: thickSpace, rspace: thickSpace, stretchy: true},
	catB:            {lspace: mediumSpace, rspace: mediumSpace},
	catC:            {lspace: thinSpace, rspace: thinSpace},
	catD:            {},
	catE:            {},
	catF:            {stretchy: true, symmetric: true},
	catG:            {stretchy: true, symmetric: true},
	catH:            {lspace: thinSpace, rspace: thinSpace, symmetric: true, largeop: true},
	catI:            {stretchy: true},
	catJ:            {lspace: thinSpace, rspace: thinSpace, symmetric: true, largeop: true, movablelimits: true},
	catK:            {},
	catL:            {lspace: thinSpace},
	catM:            {rspace: thinSpace},
}

// twoASCII is Operators_2_ascii_chars of MathML Core, B.1, Figure 24: an
// operator of these two characters is looked up as U+0320 plus its
// index.
var twoASCII = [...]string{"!!", "!=", "&&", "**", "*=", "++", "+=", "--", "-=", "->", "//", "/=", ":=", "<=", "<>", "==", ">=", "||"}

// categoryOf runs the algorithm to determine the category of an operator
// (Content, Form) of MathML Core, B.1.
func categoryOf(content string, f form) category {
	units := utf16.Encode([]rune(content))
	if len(units) != 1 && len(units) != 2 {
		return catDefault
	}
	var c rune
	if len(units) == 1 {
		c = rune(units[0])
		if c >= 0x0320 && c <= 0x03FF {
			return catDefault
		}
	} else {
		rs := []rune(content)
		switch {
		case len(rs) == 1 && (rs[0] == 0x1EEF0 || rs[0] == 0x1EEF1) && f == postfix:
			return catI
		case len(rs) == 2 && (rs[1] == 0x0338 || rs[1] == 0x20D2):
			c = rs[0]
		default:
			i := -1
			for k, s := range twoASCII {
				if s == content {
					i = k
					break
				}
			}
			if i < 0 {
				return catDefault
			}
			c = rune(0x0320 + i)
		}
	}
	if f == infix && (c == 0x007C || c == 0x223C) {
		return catForceDefault
	}
	if cat, ok := unencoded(c, f); ok {
		return cat
	}
	var key int
	switch {
	case c <= 0x03FF:
		key = int(c)
	case c >= 0x2000 && c <= 0x2BFF:
		key = int(c) - 0x1C00
	default:
		return catDefault
	}
	key += 0x1000 * int(f)
	i := sort.Search(len(operatorEntries), func(i int) bool {
		return int(operatorEntries[i][1])%0x4000 >= key
	})
	if i == len(operatorEntries) {
		return catDefault
	}
	first, last := int(operatorEntries[i][0]), int(operatorEntries[i][1])
	if key < first%0x4000 || key > last%0x4000 {
		return catDefault
	}
	return categoryOfEncoding[first/0x1000]
}

// unencoded returns the categories L and M of Figure 25, which have no
// encoding in Figure 26 and so no entry in Figure 27.
func unencoded(c rune, f form) (category, bool) {
	switch {
	case f == prefix && (c == 0x2145 || c == 0x2146 || c == 0x2202 || c >= 0x221A && c <= 0x221C):
		return catL, true
	case f == infix && (c == 0x002C || c == 0x003A || c == 0x003B):
		return catM, true
	}
	return 0, false
}

// dictionaryCategory returns the category the dictionary gives the
// operator of text content in form f (MathML Core, 3.2.4.2): when f was
// not given by a form attribute and the dictionary has no entry for it,
// the infix, the postfix and the prefix forms are tried in turn.
func dictionaryCategory(content string, f form, explicit bool) category {
	cat := categoryOf(content, f)
	if cat != catDefault || explicit {
		return cat
	}
	for _, g := range [...]form{infix, postfix, prefix} {
		if cat = categoryOf(content, g); cat != catDefault {
			return cat
		}
	}
	return catDefault
}

// stretchesInline reports a character whose intrinsic stretch axis is
// inline (MathML Core, B.1, Figure 28).
func stretchesInline(c rune) bool {
	i := sort.Search(len(inlineStretchAxis), func(i int) bool { return inlineStretchAxis[i] >= c })
	return i < len(inlineStretchAxis) && inlineStretchAxis[i] == c
}

// ItalicForm returns the mathematical italic form of c (MathML Core, 4.2
// and C.1), and whether it has one: what text-transform: math-auto draws
// a text of the one character c as.
func ItalicForm(c rune) (rune, bool) {
	i := sort.Search(len(italicMappings), func(i int) bool { return italicMappings[i][0] >= c })
	if i < len(italicMappings) && italicMappings[i][0] == c {
		return italicMappings[i][1], true
	}
	return c, false
}

// parseForm reads a form attribute (MathML Core, 3.2.4.2).
func parseForm(v string) (form, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "infix":
		return infix, true
	case "prefix":
		return prefix, true
	case "postfix":
		return postfix, true
	}
	return infix, false
}
