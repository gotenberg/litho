package cmap

import (
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
)

//go:generate go run ../../../../cmd/gen-adobe-ucs2

// adobeUCS2FS holds Adobe's current PDF-to-Unicode mapping resources, the
// "Registry-Ordering-UCS2" CMaps ISO 32000-1, 9.10.2 obtains a CIDFont's
// Unicode values from. See adobe_ucs2_sources.go for their provenance.
//
// They supplement, for the conformance checks only, the 2009 copies PDF.js
// ships as binary CMaps under bcmaps/, which text extraction keeps reading so
// that it stays equal to PDF.js. veraPDF judges a /ToUnicode CMap built on one
// of these names against Adobe's current resources, which map codes the PDF.js
// copies leave out and drop or revise others. Against those copies the current
// Adobe-GB1-UCS2, Adobe-CNS1-UCS2 and Adobe-Japan1-UCS2 hold 8, 94 and 19 more
// single mappings (Japan1 gains 56 and loses 37), and PDF.js ships no
// Adobe-KR-UCS2 at all.
//
// # Corpus evidence
//
//   - All corpora: no verdict moves at PDF/A-2u. pdfjs issue5010.pdf, the one
//     file whose /ToUnicode builds on another CMap, names Adobe-Korea1-UCS2,
//     where the two sources map the same codes.
//
//go:embed adobeucs2/*.gz
var adobeUCS2FS embed.FS

// maxAdobeUCS2Bytes caps the decompressed size of one embedded resource.
// Anti-DoS guard against a corrupt regeneration, not a format limit: the
// largest resource, Adobe-Japan1-UCS2, is 294111 bytes.
const maxAdobeUCS2Bytes = 1 << 20

var (
	errAdobeUCS2TooLarge = errors.New("adobe UCS2 resource larger than its cap")
	errAdobeUCS2Syntax   = errors.New("unexpected token in adobe UCS2 resource")
)

// adobeUCS2Source returns the decompressed text of the embedded Adobe resource
// name, and false when litho embeds no such resource.
func adobeUCS2Source(name string) ([]byte, bool, error) {
	if _, ok := adobeUCS2Digests[name]; !ok {
		return nil, false, nil
	}
	gz, err := adobeUCS2FS.ReadFile("adobeucs2/" + name + ".gz")
	if err != nil {
		return nil, true, fmt.Errorf("reading embedded %s: %w", name, err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, true, fmt.Errorf("opening embedded %s: %w", name, err)
	}
	defer zr.Close()
	data, err := io.ReadAll(io.LimitReader(zr, maxAdobeUCS2Bytes+1))
	if err != nil {
		return nil, true, fmt.Errorf("decompressing embedded %s: %w", name, err)
	}
	if len(data) > maxAdobeUCS2Bytes {
		return nil, true, fmt.Errorf("%s: %w", name, errAdobeUCS2TooLarge)
	}
	return data, true, nil
}

// parseAdobeUCS2 decodes the bfchar and bfrange sections of one of Adobe's
// PDF-to-Unicode mapping resources, in the shape [PredefinedUnicode] returns:
// chars holds each bfchar mapping and all those plus every code a bfrange
// covers, keyed by the two-byte source code with the Unicode string it maps
// to.
//
// A section is read up to its end keyword, whatever count precedes its begin
// keyword. The resources never use the array form of a bfrange destination,
// which is refused rather than skipped, so a new resource cannot lose codes
// unnoticed.
func parseAdobeUCS2(data []byte) (chars, all map[string]string, err error) {
	chars = make(map[string]string)
	ranges := make(map[string]string)
	rangeCodes := 0
	section := ""
	var operands [][]byte
	for pos := 0; pos < len(data); {
		c := data[pos]
		switch {
		case c == '%':
			for pos < len(data) && data[pos] != '\n' && data[pos] != '\r' {
				pos++
			}
		case isCMapSpace(c):
			pos++
		case c == '<':
			end := bytes.IndexByte(data[pos:], '>')
			if end < 0 {
				return nil, nil, fmt.Errorf("%w: unterminated hex string", errAdobeUCS2Syntax)
			}
			if section != "" {
				b, err := hex.DecodeString(string(data[pos+1 : pos+end]))
				if err != nil {
					return nil, nil, fmt.Errorf("%w: %w", errAdobeUCS2Syntax, err)
				}
				operands = append(operands, b)
			}
			pos += end + 1
		case c == '[' || c == ']':
			if section != "" {
				return nil, nil, fmt.Errorf("%w: array in %s", errAdobeUCS2Syntax, section)
			}
			pos++
		default:
			start := pos
			for pos < len(data) && !isCMapSpace(data[pos]) && data[pos] != '<' && data[pos] != '[' && data[pos] != ']' && data[pos] != '%' {
				pos++
			}
			switch word := string(data[start:pos]); word {
			case "beginbfchar", "beginbfrange":
				section, operands = word[len("begin"):], nil
			case "endbfchar":
				if section != "bfchar" || len(operands)%2 != 0 {
					return nil, nil, fmt.Errorf("%w: bfchar section", errAdobeUCS2Syntax)
				}
				for i := 0; i < len(operands); i += 2 {
					if len(operands[i]) != bcmapTwoByteSource {
						return nil, nil, fmt.Errorf("%w: bfchar source width", errAdobeUCS2Syntax)
					}
					chars[string(operands[i])] = decodeUTF16BE(operands[i+1])
				}
				section, operands = "", nil
			case "endbfrange":
				if section != "bfrange" || len(operands)%3 != 0 {
					return nil, nil, fmt.Errorf("%w: bfrange section", errAdobeUCS2Syntax)
				}
				for i := 0; i < len(operands); i += 3 {
					if len(operands[i]) != bcmapTwoByteSource || len(operands[i+1]) != bcmapTwoByteSource {
						return nil, nil, fmt.Errorf("%w: bfrange source width", errAdobeUCS2Syntax)
					}
					rangeCodes += mapBFRange(operands[i], operands[i+1], operands[i+2], ranges)
					if rangeCodes > maxBCMapRangeCodes {
						return nil, nil, errBCMapRangesTooLarge
					}
				}
				section, operands = "", nil
			}
		}
	}
	if section != "" {
		return nil, nil, fmt.Errorf("%w: unterminated %s section", errAdobeUCS2Syntax, section)
	}
	all = maps.Clone(chars)
	maps.Copy(all, ranges)
	return chars, all, nil
}

// isCMapSpace reports whether c is a PostScript white-space character.
func isCMapSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '\f', 0:
		return true
	}
	return false
}
