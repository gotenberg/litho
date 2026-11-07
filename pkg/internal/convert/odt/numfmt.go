package odt

import (
	"strconv"
	"strings"
)

// formatNumber writes n in the style:num-format token format
// (ODF 1.3, Part 3, 19.500 and 19.340.2 of the XSL format token): "1"
// arabic, "a" and "A" letters (a to z, then aa, bb as Writer counts with
// num-letter-sync, else as base 26), "i" and "I" roman numerals, "" none;
// "01, 02, 03, ..." to "00001, 00002, 00003, ..." arabic padded with
// zeros to the width of its first number, "𐳺, 𐳺𐳺, 𐳺𐳺𐳺, ..." Old Hungarian
// numerals (the names LibreOffice writes for its numbering types beyond
// the specification's, ODF 1.3 Part 3, 19.500 leaving the token open);
// any other token arabic. A number below 1 outside the arabic format is
// written in arabic.
func formatNumber(n int, format string) string {
	if first, _, ok := strings.Cut(format, ", "); ok && n >= 0 {
		switch {
		case strings.Trim(first, "0") == "1" && strings.HasPrefix(first, "0"):
			s := strconv.Itoa(n)
			return strings.Repeat("0", max(len(first)-len(s), 0)) + s
		case first == "𐳺" && n >= 1:
			return oldHungarian(n)
		}
	}
	switch format {
	case "":
		return ""
	case "a", "A":
		if n < 1 {
			return strconv.Itoa(n)
		}
		s := alpha(n)
		if format == "A" {
			s = strings.ToUpper(s)
		}
		return s
	case "i", "I":
		if n < 1 || n >= 4000 {
			return strconv.Itoa(n)
		}
		s := roman(n)
		if format == "i" {
			s = strings.ToLower(s)
		}
		return s
	}
	return strconv.Itoa(n)
}

// alpha returns n, at least 1, in bijective base 26: a to z, aa to az,
// ba..., the sequence Writer numbers a list in "a" with
// text:num-letter-sync false.
func alpha(n int) string {
	var b []byte
	for n > 0 {
		n--
		b = append(b, byte('a'+n%26))
		n /= 26
	}
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}

// alphaSync returns n, at least 1, as Writer counts letters with
// text:num-letter-sync true: a to z, then aa to zz, then aaa.
func alphaSync(n int) string {
	n--
	return strings.Repeat(string(rune('a'+n%26)), n/26+1)
}

// roman returns n, from 1 to 3999, in upper-case roman numerals.
func roman(n int) string {
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"M", "CM", "D", "CD", "C", "XC", "L", "XL", "X", "IX", "V", "IV", "I"}
	var b strings.Builder
	for i, v := range vals {
		for n >= v {
			b.WriteString(syms[i])
			n -= v
		}
	}
	return b.String()
}

// oldHungarian returns n, at least 1, in Old Hungarian numerals as
// LibreOffice writes them: the signs of one thousand, one hundred, fifty,
// ten, five and one repeated, the largest first (Unicode 8.0, 10C80 to
// 10CFF, Old Hungarian numbers). A number past maxOldHungarian is written
// in arabic.
func oldHungarian(n int) string {
	if n > maxOldHungarian {
		return strconv.Itoa(n)
	}
	vals := []int{1000, 100, 50, 10, 5, 1}
	syms := []string{"\U00010CFF", "\U00010CFE", "\U00010CFD", "\U00010CFC", "\U00010CFB", "\U00010CFA"}
	var b strings.Builder
	for i, v := range vals {
		for n >= v {
			b.WriteString(syms[i])
			n -= v
		}
	}
	return b.String()
}

// maxOldHungarian bounds the numbers written in Old Hungarian numerals:
// 9999, ten signs of a thousand at most. Anti-DoS guard, not
// spec-mandated: a list or page number is far below it, and the signs of
// a larger number repeat without bound.
const maxOldHungarian = 9999
