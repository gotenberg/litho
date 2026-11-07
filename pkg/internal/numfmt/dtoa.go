package numfmt

import (
	"math"
	"strconv"
	"strings"
)

// floatFormat is a notation of [formatFloat], after rtl_math_StringFormat
// (LibreOffice sal/rtl/strtmpl.hxx, doubleToString).
type floatFormat uint8

const (
	// fmtF is fixed point.
	fmtF floatFormat = iota
	// fmtE2 is scientific with an exponent of at least two digits.
	fmtE2
	// fmtE is scientific with an exponent of at least three digits.
	fmtE
	// fmtAuto is fixed point for exponents in (-15, 15), else fmtE.
	fmtAuto
)

// maxDecimals stands for rtl_math_DecimalPlaces_Max: as many decimals as
// the notation's significance gives.
const maxDecimals = math.MaxInt32

// decimalParts returns the shortest decimal digits that read back as v,
// v positive and finite, and the power of ten of the last one: v is
// digits times 10^exp.
func decimalParts(v float64) (digits []byte, exp int) {
	s := strconv.FormatFloat(v, 'e', -1, 64)
	mant, e, _ := strings.Cut(s, "e")
	ex, _ := strconv.Atoi(e)
	for i := 0; i < len(mant); i++ {
		if c := mant[i]; c >= '0' && c <= '9' {
			digits = append(digits, c)
		}
	}
	return digits, ex - (len(digits) - 1)
}

// formatFloat writes v in the notation f with dec decimals (maxDecimals
// for the notation's own), trailing decimal zeros erased when erase is set,
// as LibreOffice's doubleToString does: the shortest representation of v,
// rounded half up to at most 15 significant digits, an integer below 2^53
// written whole in fixed and automatic notation.
func formatFloat(v float64, f floatFormat, dec int, erase bool) string {
	if math.IsNaN(v) {
		return "NaN"
	}
	neg := math.Signbit(v)
	if math.IsInf(v, 0) {
		if neg {
			return "-INF"
		}
		return "INF"
	}
	if neg {
		v = -v
	}
	var digits []byte
	pexp := 0
	if v != 0 {
		digits, pexp = decimalParts(v)
	} else {
		digits = []byte{'0'}
	}
	origDigits := len(digits)
	nExp := origDigits + pexp - 1
	roundDigits := 15
	if (f == fmtAuto || f == fmtF) && pexp >= 0 && v < 0x1p53 {
		f = fmtF
		if dec == maxDecimals {
			dec = 0
		} else {
			dec = max(-15, min(dec, 15))
		}
		if erase && dec > 0 {
			dec = 0
		}
		roundDigits = origDigits
	}
	if f == fmtAuto {
		if nExp <= -15 || nExp >= 15 {
			if dec == maxDecimals {
				dec = 14
			}
			f = fmtE
		} else {
			if dec == maxDecimals {
				if nExp < 14 {
					dec = 15 - nExp - 1
				} else {
					dec = 15
				}
			}
			f = fmtF
		}
	}
	dec = max(-309, min(dec, 309))
	nDigits := dec + 1
	if f == fmtF {
		nDigits += nExp
	}
	roundDigits = min(nDigits, roundDigits)
	if nDigits >= 0 && origDigits > roundDigits {
		// Round half up at roundDigits significant digits.
		up := digits[roundDigits] >= '5'
		for i := roundDigits; i < origDigits; i++ {
			digits[i] = '0'
		}
		if up {
			i := roundDigits - 1
			for ; i >= 0; i-- {
				if digits[i] == '9' {
					digits[i] = '0'
					continue
				}
				digits[i]++
				break
			}
			if i < 0 {
				digits = append([]byte{'1'}, digits...)
				digits = digits[:origDigits]
				nExp++
				if f == fmtF {
					nDigits++
				}
			}
		}
	}
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	hasDec := false
	decPos := 1
	if f == fmtF {
		if nExp < 0 {
			b.WriteByte('0')
			if dec > 0 {
				b.WriteByte('.')
				hasDec = true
			}
			n := -nExp - 1
			if nDigits <= 0 {
				n = dec
			}
			for ; n > 0; n-- {
				b.WriteByte('0')
			}
			decPos = 0
		} else {
			decPos = nExp + 1
		}
	}
	if nDigits > 0 {
		for i := 0; ; i++ {
			c := byte('0')
			if i < len(digits) {
				c = digits[i]
			}
			b.WriteByte(c)
			nDigits--
			if nDigits == 0 {
				break
			}
			if decPos > 0 {
				decPos--
				if decPos == 0 {
					b.WriteByte('.')
					hasDec = true
				}
			}
		}
	}
	if !hasDec && f == fmtF {
		for decPos--; decPos > 0; decPos-- {
			b.WriteByte('0')
		}
	}
	s := b.String()
	if erase && hasDec {
		s = strings.TrimRight(s, "0")
		s = strings.TrimSuffix(s, ".")
	}
	if f == fmtE || f == fmtE2 {
		if s == "" || s == "-" {
			s += "1"
		}
		var e strings.Builder
		e.WriteString(s)
		e.WriteByte('E')
		if nExp < 0 {
			nExp = -nExp
			e.WriteByte('-')
		} else {
			e.WriteByte('+')
		}
		if f == fmtE || nExp >= 100 {
			e.WriteByte(byte('0' + nExp/100%10))
		}
		nExp %= 100
		if f == fmtE || f == fmtE2 || nExp >= 10 {
			e.WriteByte(byte('0' + nExp/10))
		}
		e.WriteByte(byte('0' + nExp%10))
		s = e.String()
	}
	return s
}
