package saslprep

import (
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// runeRange is an inclusive range of code points, the unit of every
// generated table.
type runeRange struct{ lo, hi rune }

// in reports whether r falls in table, whose ranges are sorted and disjoint.
func in(table []runeRange, r rune) bool {
	i := sort.Search(len(table), func(i int) bool { return table[i].hi >= r })
	return i < len(table) && table[i].lo <= r
}

// Query prepares s with SASLprep as a query string (RFC 3454, 7): a code
// point Unicode 3.2 leaves unassigned passes through. Use it for a password a
// reader matches against a document.
//
// It maps the non-ASCII spaces of RFC 3454, Table C.1.2 to U+0020 and the
// characters of Table B.1 to nothing (RFC 4013, 2.1), normalizes the result
// to form KC (2.2), and rejects a result holding a character 2.3 prohibits
// ([ErrProhibited]) or failing the bidirectional checks of RFC 3454, 6
// ([ErrBidi]). Input that is not valid UTF-8 yields [ErrInvalidUTF8]. A
// rejection returns no string, as RFC 3454, 6 requires.
//
// U+200B ZERO WIDTH SPACE is in both Table C.1.2 and Table B.1. RFC 4013, 2.1
// lists the space mapping first and is read here as applying it first, so
// U+200B becomes U+0020, as libidn and PDF.js's saslPrep map it.
//
// Normalization uses the current Unicode version, not the Unicode 3.2 RFC
// 4013, 2 names: golang.org/x/text ships no 3.2 tables. The Unicode
// normalization stability policy freezes form KC for encoded characters, so
// the two agree on strings of code points assigned in 3.2, except the five
// CJK compatibility ideographs Unicode Corrigendum #4 corrected after 3.2.
// They can differ on code points 3.2 leaves unassigned, which a stored string
// cannot hold.
//
// The errors never name the offending character: the input is a password.
func Query(s string) (string, error) {
	return prepare(s, false)
}

// Stored prepares s with SASLprep as a stored string: it is [Query], but a
// code point Unicode 3.2 leaves unassigned yields [ErrUnassigned], since RFC
// 3454, 7 forbids one in a stored string. Use it for a password written into
// a new document.
func Stored(s string) (string, error) {
	return prepare(s, true)
}

func prepare(s string, stored bool) (string, error) {
	if !utf8.ValidString(s) {
		return "", ErrInvalidUTF8
	}
	if isPrintableASCII(s) {
		// Printable ASCII is left unchanged by every step and passes every
		// check, and it is nearly every password.
		return s, nil
	}

	var mapped strings.Builder
	mapped.Grow(len(s))
	for _, r := range s {
		// RFC 3454, 7: checked on the input, before normalization can turn a
		// code point unassigned in Unicode 3.2 into an assigned one.
		if stored && in(unassigned, r) {
			return "", ErrUnassigned
		}
		switch {
		case in(nonASCIISpace, r):
			mapped.WriteByte(' ')
		case in(mappedToNothing, r):
		default:
			mapped.WriteRune(r)
		}
	}
	out := norm.NFKC.String(mapped.String())

	var hasRandAL, hasL bool
	for _, r := range out {
		if in(prohibited, r) {
			return "", ErrProhibited
		}
		if in(randALCat, r) {
			hasRandAL = true
		} else if in(lCat, r) {
			hasL = true
		}
	}
	if hasRandAL {
		// RFC 3454, 6, requirements 2 and 3. Requirement 1 is Table C.8,
		// already in prohibited.
		first, _ := utf8.DecodeRuneInString(out)
		last, _ := utf8.DecodeLastRuneInString(out)
		if hasL || !in(randALCat, first) || !in(randALCat, last) {
			return "", ErrBidi
		}
	}
	return out, nil
}

// isPrintableASCII reports whether s holds only U+0020 to U+007E.
func isPrintableASCII(s string) bool {
	for i := range len(s) {
		if s[i] < 0x20 || s[i] > 0x7E {
			return false
		}
	}
	return true
}
