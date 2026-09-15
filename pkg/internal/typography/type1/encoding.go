package type1

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// eexecMarker is the PostScript keyword that begins the encrypted
// binary section of a Type 1 font program. Bounding the regex search
// to the clear-text portion ahead of this marker avoids false matches
// against binary bytes that happen to resemble PostScript tokens.
var eexecMarker = []byte("currentfile eexec")

// dupPutRe matches a single "dup N /glyphname put" assignment inside
// a Type 1 /Encoding vector. The byte index is captured in group 1
// and the glyph name in group 2.
var dupPutRe = regexp.MustCompile(`dup\s+(\d+)\s+/(\S+)\s+put`)

// dupPutRadixRe is [dupPutRe] with the index also allowed to be a
// PostScript radix number such as 8#101 (PostScript Language Reference,
// 3.2.2), which some producers write the whole vector in.
var dupPutRadixRe = regexp.MustCompile(`dup\s+(\d+(?:#[0-9A-Za-z]+)?)\s+/(\S+)\s+put`)

// parseCode reads an encoding vector index: a decimal integer or a
// PostScript radix number "base#digits" with a base from 2 to 36.
func parseCode(tok string) (int, error) {
	base, digits, radix := strings.Cut(tok, "#")
	if !radix {
		return strconv.Atoi(tok)
	}
	b, err := strconv.Atoi(base)
	if err != nil || b < 2 || b > 36 {
		return 0, fmt.Errorf("type1: radix %q out of range", base)
	}
	v, err := strconv.ParseInt(digits, b, 32)
	return int(v), err
}

// namedEncodingRe captures the case where /Encoding references a
// predefined encoding by name (e.g. "/Encoding StandardEncoding
// def"). The name is returned in group 1.
var namedEncodingRe = regexp.MustCompile(`/Encoding\s+(\S+)\s+def`)

// ParseEncoding returns the byte-to-glyph-name mapping declared in a
// Type 1 font's clear-text /Encoding directive. Unassigned bytes come
// back as the empty string. When the font references a predefined
// encoding by name (StandardEncoding, ISOLatin1Encoding, etc.) the
// result is the name repeated across every byte so callers can
// recognise the reference and load the corresponding table
// themselves. Fonts without a recognisable /Encoding produce an all-
// empty result.
//
// Indices are read as decimal integers, as pdf.js's Type1Parser reads them
// (parseInt, base 10), so text extraction keeps agreeing with the pdf.js
// reference it is measured against. [ParseEncodingPS] reads radix numbers
// too, as a PostScript interpreter does.
func ParseEncoding(data []byte) [256]string {
	return parseEncoding(data, dupPutRe)
}

// ParseEncodingPS is [ParseEncoding] reading the vector's indices as a
// PostScript interpreter does, radix numbers such as 8#101 included, which is
// how veraPDF reads them: govdocs thread_100_100480.pdf writes its whole vector
// that way.
func ParseEncodingPS(data []byte) [256]string {
	return parseEncoding(data, dupPutRadixRe)
}

// parseEncoding implements [ParseEncoding] with the given dup/put matcher.
func parseEncoding(data []byte, entryRe *regexp.Regexp) [256]string {
	var enc [256]string
	if len(data) == 0 {
		return enc
	}

	region := clearTextRegion(data)

	// Predefined-encoding reference. The form is short, greedy matches
	// against the clear-text region catch "/Encoding StandardEncoding
	// def" while skipping the more common explicit vector pattern.
	if m := namedEncodingRe.FindSubmatch(region); m != nil {
		name := string(m[1])
		switch name {
		case "StandardEncoding", "ISOLatin1Encoding", "MacRomanEncoding",
			"WinAnsiEncoding", "MacExpertEncoding", "SymbolEncoding":
			for i := range enc {
				enc[i] = name
			}
			return enc
		}
	}

	for _, m := range entryRe.FindAllSubmatch(region, -1) {
		idx, err := parseCode(string(m[1]))
		if err != nil || idx < 0 || idx > 255 {
			continue
		}
		enc[idx] = string(m[2])
	}
	return enc
}

// clearTextRegion trims the font data to the portion before the
// eexec-encrypted section. The /Encoding vector is always in the
// clear-text preamble, so bounding the regex search saves time and
// eliminates false positives on binary bytes that happen to form
// PostScript-like sequences.
func clearTextRegion(data []byte) []byte {
	if before, _, ok := bytes.Cut(data, eexecMarker); ok {
		return before
	}
	return data
}
