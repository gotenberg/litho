package type1

import "regexp"

// notdefFillRe matches the loop a Type 1 program runs to name .notdef at every
// code of its encoding vector before it lists the codes it uses: "0 1 255 {1
// index exch /.notdef put} for" (Adobe Type 1 Font Format, 2.3).
var notdefFillRe = regexp.MustCompile(`\{\s*1\s+index\s+exch\s+/\.notdef\s+put\s*\}\s*for`)

// EncodingFillsNotdef reports whether the program fills its built-in encoding
// vector with .notdef before listing codes, so a code the vector does not list
// names .notdef rather than nothing.
func EncodingFillsNotdef(data []byte) bool {
	return notdefFillRe.Match(clearTextRegion(data))
}
