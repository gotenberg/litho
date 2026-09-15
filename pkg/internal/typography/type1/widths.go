package type1

import (
	"bytes"
	"regexp"
	"strconv"
)

// charStringKey is the initial value of the RC4-like cipher that
// protects each individual charstring. Adobe Type 1 Font Format
// Appendix A.1 specifies R₀ = 4330 for this cipher; the c₁/c₂
// constants are shared with eexec.
const charStringKey uint32 = 4330

// defaultLenIV is the default number of random salt bytes prefixed to
// each charstring when the font's /Private dict does not override
// /lenIV. Spec: Adobe Type 1 Font Format §7.2.
const defaultLenIV = 4

// lenIVRe captures a /Private-dict /lenIV declaration. Some TeX-
// generated fonts set it to 0 to save bytes; others omit the entry
// entirely.
var lenIVRe = regexp.MustCompile(`/lenIV\s+(-?\d+)\s+def`)

// fontMatrixRe captures the first (x-scale) element of a Type 1
// /FontMatrix declaration, which lives in the cleartext header before
// eexec. The standard matrix is [0.001 0 0 0.001 0 0]; subset fonts
// sometimes use a different scale, so charstring advances (in the
// font's own design units) must be multiplied by this scale to reach
// text space.
var fontMatrixRe = regexp.MustCompile(`/FontMatrix\s*\[\s*([0-9.eE+-]+)`)

// FontMatrixScale returns the x-scale of the Type 1 program's
// /FontMatrix (its first element), which converts a charstring advance
// width from design units to text space. Returns (0.001, false) when
// the matrix is absent or unparseable so callers can fall back to the
// 1000-unit default. A non-positive scale is rejected as unusable.
func FontMatrixScale(data []byte) (float64, bool) {
	m := fontMatrixRe.FindSubmatch(data)
	if m == nil {
		return 0.001, false
	}
	scale, err := strconv.ParseFloat(string(m[1]), 64)
	if err != nil || scale <= 0 {
		return 0.001, false
	}
	return scale, true
}

// decryptCharString removes the lenIV-byte random salt prefix that
// every Type 1 charstring carries and returns the plaintext operator
// bytes. The same (c₁, c₂) constants as eexec are used but the key is
// seeded with [charStringKey] (4330) rather than 55665.
func decryptCharString(data []byte, lenIV int) []byte {
	if lenIV < 0 {
		lenIV = defaultLenIV
	}
	if len(data) <= lenIV {
		return nil
	}
	plain := make([]byte, len(data))
	r := charStringKey
	for i, c := range data {
		plain[i] = c ^ byte(r>>8)
		r = (uint32(c)+r)*eexecC1 + eexecC2
	}
	return plain[lenIV:]
}

// parseLenIV returns the /lenIV value declared in the decrypted
// /Private dict. Absent or malformed declarations fall back to the
// [defaultLenIV] spec default of 4.
func parseLenIV(plain []byte) int {
	if m := lenIVRe.FindSubmatch(plain); m != nil {
		if v, err := strconv.Atoi(string(m[1])); err == nil && v >= 0 {
			return v
		}
	}
	return defaultLenIV
}

// ParseCharStringWidths extracts the advance width (in font design
// units, i.e. 1/1000 em for a 1000-unit FontMatrix) of every glyph
// defined in the Type 1 font's /CharStrings dictionary. The returned
// map is keyed by glyph name and includes entries only for
// charstrings whose leading operator is [hsbw] or [sbw]; charstrings
// that begin with anything else (which is spec-non-compliant for
// Type 1) are skipped silently so a partial map still gets back to
// the caller.
func ParseCharStringWidths(data []byte) map[string]float64 {
	if len(data) == 0 {
		return nil
	}
	plain := decryptEexec(eexecRegion(data))
	if plain == nil {
		return nil
	}
	cs := bytes.Index(plain, charStringsMarker)
	if cs < 0 {
		return nil
	}
	lenIV := parseLenIV(plain)

	pos := cs + len(charStringsMarker)
	if beginIdx := bytes.Index(plain[pos:], []byte("begin")); beginIdx >= 0 {
		pos += beginIdx + len("begin")
	}

	widths := make(map[string]float64)
	for pos < len(plain) {
		pos = skipWhitespace(plain, pos)
		if pos >= len(plain) || bytes.HasPrefix(plain[pos:], []byte("end")) {
			break
		}
		if plain[pos] != '/' {
			pos++
			continue
		}
		name, next := readName(plain, pos+1)
		if name == "" {
			pos++
			continue
		}
		pos = skipWhitespace(plain, next)
		length, after := readInt(plain, pos)
		if after == pos {
			continue
		}
		pos = skipWhitespace(plain, after)
		op, afterOp := readReaderOp(plain, pos)
		if op == nil {
			continue
		}
		pos = afterOp
		if pos < len(plain) && isPostScriptWhitespace(plain[pos]) {
			pos++
		}
		if pos+length > len(plain) {
			break
		}
		body := plain[pos : pos+length]
		pos += length
		if w, ok := extractAdvanceWidth(decryptCharString(body, lenIV)); ok {
			if _, exists := widths[name]; !exists {
				widths[name] = w
			}
		}
	}
	return widths
}

// Type 1 charstring operator numbers used for width extraction. The
// full operator set is much larger; text extraction only needs the
// two that carry the advance width, so the interpreter in
// [extractAdvanceWidth] intentionally stops once it sees one.
const (
	csOpHstem          = 1  //nolint:unused // listed for context
	csOpVstem          = 3  //nolint:unused // listed for context
	csOpVmoveTo        = 4  //nolint:unused // listed for context
	csOpRlineTo        = 5  //nolint:unused // listed for context
	csOpHlineTo        = 6  //nolint:unused // listed for context
	csOpVlineTo        = 7  //nolint:unused // listed for context
	csOpRrcurveTo      = 8  //nolint:unused // listed for context
	csOpClosePath      = 9  //nolint:unused // listed for context
	csOpCallSubr       = 10 //nolint:unused // listed for context
	csOpReturn         = 11 //nolint:unused // listed for context
	csOpEscape         = 12
	csOpHsbw           = 13
	csOpEndChar        = 14 //nolint:unused // listed for context
	csOpRmoveTo        = 21 //nolint:unused // listed for context
	csOpHmoveTo        = 22 //nolint:unused // listed for context
	csOpVhcurveTo      = 30 //nolint:unused // listed for context
	csOpHvcurveTo      = 31 //nolint:unused // listed for context
	csOpEscapeSbw byte = 7  // follows csOpEscape to form the two-byte sbw operator
	csOpEscapeDiv byte = 12 // follows csOpEscape to form the two-byte div operator
)

// extractAdvanceWidth walks the decrypted charstring bytes, decoding
// the Type 1 integer encoding until it hits either hsbw (operator
// 13) or sbw (12 7). Returns the advance width pushed immediately
// before that operator. A div (12 12) on the way is evaluated, since a
// fractional advance can only be written as a quotient (Adobe Type 1 Font
// Format, 6.5): TeX's Computer Modern subsets set "78 113889 100 div hsbw".
// Any other operator or malformed byte stream returns (0, false) so the
// caller can skip the glyph.
func extractAdvanceWidth(plain []byte) (float64, bool) {
	if len(plain) == 0 {
		return 0, false
	}
	const stackLimit = 24
	var stack [stackLimit]float64
	top := 0

	push := func(v float64) bool {
		if top >= stackLimit {
			return false
		}
		stack[top] = v
		top++
		return true
	}

	for i := 0; i < len(plain); {
		b := plain[i]
		switch {
		case b >= 32 && b <= 246:
			if !push(float64(int(b) - 139)) {
				return 0, false
			}
			i++
		case b >= 247 && b <= 250:
			if i+1 >= len(plain) {
				return 0, false
			}
			if !push(float64((int(b)-247)*256 + int(plain[i+1]) + 108)) {
				return 0, false
			}
			i += 2
		case b >= 251 && b <= 254:
			if i+1 >= len(plain) {
				return 0, false
			}
			if !push(float64(-(int(b)-251)*256 - int(plain[i+1]) - 108)) {
				return 0, false
			}
			i += 2
		case b == 255:
			if i+4 >= len(plain) {
				return 0, false
			}
			v := int32(plain[i+1])<<24 | int32(plain[i+2])<<16 | int32(plain[i+3])<<8 | int32(plain[i+4])
			if !push(float64(v)) {
				return 0, false
			}
			i += 5
		case b == csOpHsbw:
			if top < 2 {
				return 0, false
			}
			return stack[top-1], true
		case b == csOpEscape:
			if i+1 >= len(plain) {
				return 0, false
			}
			next := plain[i+1]
			if next == csOpEscapeSbw {
				if top < 4 {
					return 0, false
				}
				return stack[top-2], true
			}
			if next == csOpEscapeDiv {
				if top < 2 || stack[top-1] == 0 {
					return 0, false
				}
				stack[top-2] /= stack[top-1]
				top--
				i += 2
				continue
			}
			// Any other escaped operator before hsbw/sbw is a spec
			// violation for Type 1 charstrings; abandon rather than
			// fabricate a width.
			return 0, false
		default:
			// Any other single-byte operator means we passed the
			// metrics prologue without finding a width, which is a
			// malformed charstring for our purposes.
			return 0, false
		}
	}
	return 0, false
}
