package type1

import (
	"bytes"
	"regexp"
	"strconv"
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
func ParseEncoding(data []byte) [256]string {
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

	for _, m := range dupPutRe.FindAllSubmatch(region, -1) {
		idx, err := strconv.Atoi(string(m[1]))
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
	if idx := bytes.Index(data, eexecMarker); idx >= 0 {
		return data[:idx]
	}
	return data
}
