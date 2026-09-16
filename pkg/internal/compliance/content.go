package compliance

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// validPDFOperators is the set of all operators defined in ISO 32000.
//
//nolint:gochecknoglobals // package-level lookup table for content stream validation.
var validPDFOperators = map[string]bool{
	// General graphics state.
	"q": true, "Q": true, "cm": true, "w": true, "J": true, "j": true,
	"M": true, "d": true, "ri": true, "i": true, "gs": true,
	// Path construction.
	"m": true, "l": true, "c": true, "v": true, "y": true, "h": true,
	"re": true,
	// Path painting.
	"S": true, "s": true, "f": true, "F": true, "f*": true,
	"B": true, "B*": true, "b": true, "b*": true, "n": true,
	// Clipping.
	"W": true, "W*": true,
	// Color.
	"CS": true, "cs": true, "SC": true, "SCN": true, "sc": true, "scn": true,
	"G": true, "g": true, "RG": true, "rg": true, "K": true, "k": true,
	// Text.
	"BT": true, "ET": true, "Tc": true, "Tw": true, "Tz": true, "TL": true,
	"Tf": true, "Tr": true, "Ts": true, "Td": true, "TD": true, "Tm": true,
	"T*": true, "Tj": true, "TJ": true, "'": true, "\"": true,
	// XObject.
	"Do": true,
	// Inline image.
	"BI": true, "ID": true, "EI": true,
	// Marked content.
	"MP": true, "DP": true, "BMC": true, "BDC": true, "EMC": true,
	// Compatibility.
	"BX": true, "EX": true,
	// Shading.
	"sh": true,
	// Type 3 font operators.
	"d0": true, "d1": true,
}

// forbiddenInlineImageFilters lists filter names that are not permitted in
// inline images for PDF/A conformance. ISO 19005-1 §6.1.10 forbids
// LZWDecode (and its inline abbreviation LZW) and Crypt; the abbreviated
// filter names are those of ISO 32000-1 Table 92 (and the full names of
// Table 6). Parts 2/3 use the positive allowedInlineImageFilters set.
//
//nolint:gochecknoglobals // package-level lookup table.
var forbiddenInlineImageFilters = map[string]bool{
	"LZW":       true,
	"LZWDecode": true,
	"Crypt":     true,
}

// allowedInlineImageFilters lists the inline-image filter abbreviations and
// full names permitted by PDF/A (ISO 32000-1 Table 6 / ISO 32000-2 Table 92).
// A value mapped to false (LZW) is a valid abbreviation that PDF/A forbids; a
// value absent from the map (e.g. "Crypt" or a mis-cased "Lzw") is "not listed
// in Table 6" and is likewise forbidden. Used for parts 2/3/4, whose §6.1.10 /
// §6.2.10.x rules require the value to be a valid, non-LZW Table-6 entry.
//
//nolint:gochecknoglobals // package-level lookup table.
var allowedInlineImageFilters = map[string]bool{
	"AHx": true, "ASCIIHexDecode": true,
	"A85": true, "ASCII85Decode": true,
	"LZW": false, "LZWDecode": false, // explicitly forbidden
	"Fl": true, "FlateDecode": true,
	"RL": true, "RunLengthDecode": true,
	"CCF": true, "CCITTFaxDecode": true,
	"DCT": true, "DCTDecode": true,
	"JPXDecode":   true,
	"JBIG2Decode": true,
	"Crypt":       false, // explicitly forbidden
}

// checkContentGroup validates content stream operators for PDF/A conformance.
// It checks for unknown operators, q/Q nesting depth, and forbidden inline
// image filters. For PDF/UA profiles, no content stream checks are performed.
func checkContentGroup(ctx context.Context, vctx *validationCtx) []Violation {
	doc := vctx.doc
	profile := vctx.profile

	if vctx.ua {
		return nil
	}

	part := vctx.part
	if part == 0 {
		return nil
	}

	doc.Logger.Debug("compliance.content: starting checks", "profile", profile, "part", part)

	var results []Violation

	for i := range vctx.pages {
		if ctx.Err() != nil {
			return results
		}

		pageResults := checkPageContent(ctx, doc, &vctx.pages[i], part, i+1)
		results = append(results, pageResults...)
	}

	doc.Logger.Debug("compliance.content: checks complete", "violations", len(results))
	return results
}

// checkPageContent validates the content streams of a single page.
func checkPageContent(ctx context.Context, doc *document.Document, page *pageCtx, part, pageNum int) []Violation {
	if len(page.content) == 0 {
		return nil
	}

	// Use lazily-parsed operations for operator validation and q/Q nesting.
	ops := page.parsedOps(ctx)
	if ops == nil {
		doc.Logger.Debug("compliance.content: failed to parse content stream",
			"page", pageNum)
		return nil
	}

	var results []Violation

	// Check operators and q/Q nesting.
	qDepth := 0
	maxDepthSeen := 0

	for idx, op := range ops {
		// Check context every ~100 operators.
		if idx%100 == 0 && ctx.Err() != nil {
			return results
		}

		// Check for unknown operators.
		if !validPDFOperators[op.Operator] {
			results = append(results, failf(unknownOperatorRule(part),
				"page %d: operator %q", pageNum, op.Operator)...)
		}

		// Track q/Q nesting depth.
		switch op.Operator {
		case "q":
			qDepth++
			if qDepth > maxDepthSeen {
				maxDepthSeen = qDepth
			}
		case "Q":
			if qDepth > 0 {
				qDepth--
			}
		}
	}

	// Report q/Q nesting violation if exceeded.
	if maxDepthSeen > maxQNesting {
		rule := qNestingRule(part)
		if rule.Clause != "" {
			results = append(results, failf(rule,
				"page %d: q/Q nesting depth %d exceeds %d", pageNum, maxDepthSeen, maxQNesting)...)
		}
	}

	// Check inline image filters in raw bytes.
	results = append(results, checkInlineImageFilters(page.content, part, pageNum)...)

	return results
}

// resolvePageContentBytes decodes and concatenates the content streams of a page,
// returning the raw bytes suitable for both parsing and inline image scanning.
func resolvePageContentBytes(ctx context.Context, doc *document.Document, page ast.PdfDict) ([]byte, error) {
	contentsObj, ok := page["Contents"]
	if !ok {
		return nil, nil
	}

	resolved, err := document.ResolveObject(ctx, doc, contentsObj)
	if err != nil {
		return nil, fmt.Errorf("resolving /Contents: %w", err)
	}

	var streams []ast.PdfStream

	switch v := resolved.(type) {
	case ast.PdfStream:
		streams = append(streams, v)
	case ast.PdfArray:
		for _, item := range v {
			stream, err := document.ResolveStream(ctx, doc, item)
			if err != nil {
				return nil, fmt.Errorf("resolving item in /Contents array: %w", err)
			}
			streams = append(streams, stream)
		}
	default:
		return nil, fmt.Errorf("unexpected /Contents type: %T", resolved)
	}

	var (
		allData         []byte
		decoded         int
		lastErr         error
		undecodablePart bool
	)
	for _, s := range streams {
		// A stream whose /Filter chain contains an undecodable filter (e.g. a
		// misspelled "Flatedecode") stays filter-encoded after DecodeStream.
		// Parsing those bytes as content operators yields a run of spurious
		// "undefined operator" violations; the bad filter itself is reported
		// separately (ISO 19005 §6.1.6.2). veraPDF likewise does not parse an
		// undecodable content stream.
		//
		// It skips that part only, not the page: ISO 32000-1, 7.7.3.3 makes the
		// array one content stream split into parts, and veraPDF judges the
		// parts it can read. pdfjs bomb_giant.pdf follows its text part with
		// three /JBIG2Decode parts, and veraPDF reports the text's unembedded
		// font and untagged content while litho dropped the page. Measured
		// against veraPDF with the undecodable part before and after the
		// readable one, and with an unknown filter name in its place.
		if doc.StreamHasUndecodableFilter(s) {
			undecodablePart = true
			continue
		}
		data, err := doc.DecodeStream(ctx, s)
		if err != nil {
			// ISO 32000-1, 7.7.3.3 lets a page split its content across an
			// array of streams, and one truncated part says nothing about the
			// rest. Losing the whole page for it threw away every operator the
			// other parts hold: pdfjs issue17190_1.pdf carries 77 parts, one of
			// them unreadable, and litho reported none of the colour and font
			// defects veraPDF raises from the other 76. Viewers render what
			// they can, and [content.ParsePageContent] already skips the part
			// and keeps going.
			doc.Logger.Debug("compliance: content stream part failed to decode; skipping",
				slog.Any("err", err))
			lastErr = err
			continue
		}
		decoded++
		allData = append(allData, data...)
		allData = append(allData, '\n')

		// Bounded for the reason given on [content.MaxPageContentSize]: the
		// per-stream decompression cap does not bound the sum across a
		// /Contents array, and this buffer feeds every page-scoped check.
		// Stopping here leaves the page looking unreadable, which the callers
		// already answer conservatively, rather than letting a crafted page
		// grow the buffer with the file's object count.
		if len(allData) > content.MaxPageContentSize {
			return nil, fmt.Errorf("page content exceeds %d MB limit", content.MaxPageContentSize>>20)
		}
	}

	// Nothing decoded at all is a page whose content litho cannot read, which
	// the caller answers conservatively. A page held back by an undecodable
	// filter keeps its own sentinel, whatever else failed beside it.
	if decoded == 0 && undecodablePart {
		return nil, errUndecodableContent
	}
	if decoded == 0 && lastErr != nil {
		return nil, fmt.Errorf("decoding content stream: %w", lastErr)
	}

	return allData, nil
}

// checkInlineImageFilters scans raw content stream bytes for BI (Begin Inline
// Image) operators and checks whether any forbidden filters (LZW, Crypt) are
// used. The scan looks for the BI keyword followed by inline image dictionary
// key/value pairs, checking /F and /Filter entries.
func checkInlineImageFilters(data []byte, part, pageNum int) []Violation {
	var results []Violation

	// Scan for BI tokens in the raw byte stream. BI must appear as a
	// standalone keyword (preceded by whitespace or start of stream).
	offset := 0
	for offset < len(data) {
		idx := bytes.Index(data[offset:], []byte("BI"))
		if idx < 0 {
			break
		}

		absIdx := offset + idx
		offset = absIdx + 2

		// Verify BI is a standalone keyword: preceded by whitespace (or start)
		// and followed by whitespace.
		if absIdx > 0 && !isPDFWhitespace(data[absIdx-1]) {
			continue
		}
		if offset < len(data) && !isPDFWhitespace(data[offset]) {
			continue
		}

		// Extract the inline image dictionary (between BI and ID).
		idIdx := findInlineImageID(data, offset)
		if idIdx < 0 {
			break
		}

		dictBytes := data[offset:idIdx]
		for _, filterVal := range extractInlineImageFilters(dictBytes) {
			if filterVal == "" {
				continue
			}
			var forbidden bool
			if part == 1 {
				// PDF/A-1 forbids only the named filters (no "not in Table 6"
				// clause for inline images).
				forbidden = forbiddenInlineImageFilters[filterVal]
			} else {
				// Parts 2/3/4: the /F value must be a valid Table-6/92
				// abbreviation and not LZW. A non-Table-6 name (e.g. "Lzw")
				// fails "a value not listed in Table 6" (verified live on
				// /F /Lzw in 6-1-9-t01-fail-c remediated to A-2a).
				allowed, known := allowedInlineImageFilters[filterVal]
				forbidden = !known || !allowed
			}
			if forbidden {
				results = append(results, failf(inlineImageFilterRule(part),
					"page %d: inline image uses forbidden filter %q", pageNum, filterVal)...)
				break
			}
		}

		// Skip past ID...EI.
		offset = idIdx + 2
	}

	return results
}

// findInlineImageID locates the ID keyword that terminates the inline image
// dictionary. ID must be preceded and followed by whitespace.
func findInlineImageID(data []byte, start int) int {
	for i := start; i < len(data)-1; i++ {
		if data[i] == 'I' && data[i+1] == 'D' {
			// Verify standalone keyword.
			if i > 0 && !isPDFWhitespace(data[i-1]) {
				continue
			}
			if i+2 < len(data) && !isPDFWhitespace(data[i+2]) {
				continue
			}
			return i
		}
	}
	return -1
}

// extractInlineImageFilter extracts the filter value from inline image
// dictionary bytes. It looks for /F or /Filter keys and returns the
// corresponding name value.
func extractInlineImageFilters(dictBytes []byte) []string {
	// Tokenize the dictionary bytes looking for /F or /Filter keys.
	// Inline image dictionaries use abbreviated key names. The value may be a
	// single name (/AHx) or a filter-chain array ([/AHx /Fl]); both are legal.
	tokens := tokenizeInlineDict(dictBytes)
	// A dictionary that writes both the abbreviation and the full name is
	// judged by the abbreviation, whichever comes first: veraPDF reports
	// /F /AHx /Filter /LZWDecode and /Filter /LZWDecode /F /AHx alike as
	// conforming, and /F /LZW /Filter /AHx as not.
	for _, want := range []string{"/F", "/Filter"} {
		for i := 0; i < len(tokens)-1; i++ {
			if tokens[i] != want {
				continue
			}
			val := tokens[i+1]
			if val != "[" {
				return []string{inlineFilterName(val)}
			}
			// Filter-chain array: collect the names up to the closing ']'.
			var names []string
			for j := i + 2; j < len(tokens) && tokens[j] != "]"; j++ {
				if n := inlineFilterName(tokens[j]); n != "" {
					names = append(names, n)
				}
			}
			return names
		}
	}
	return nil
}

// inlineFilterName normalises an inline-image filter token to a bare filter
// name by dropping its leading '/'.
func inlineFilterName(tok string) string {
	return strings.TrimPrefix(tok, "/")
}

// tokenizeInlineDict performs a simple tokenization of inline image dictionary
// bytes, splitting on whitespace and recognizing /Name tokens.
func tokenizeInlineDict(data []byte) []string {
	var tokens []string
	i := 0
	for i < len(data) {
		c := data[i]
		switch {
		case isPDFWhitespace(c):
			i++
		case c == '%':
			// A comment runs to the end of the line (ISO 32000-1, 7.2.4); a
			// key written inside one is no key. pdfjs issue14256.pdf comments
			// out lines of its inline image dictionaries.
			for i < len(data) && data[i] != '\n' && data[i] != '\r' {
				i++
			}
		case c == '[' || c == ']':
			// Array delimiters stand alone, so /F [/AHx] /L 3 ends the array at
			// the ']' glued to /AHx rather than reading /L as a filter.
			tokens = append(tokens, string(c))
			i++
		default:
			// A name or any other token runs to the next white space or
			// delimiter (ISO 32000-1, 7.2.2).
			start := i
			i++
			for i < len(data) && !isPDFWhitespace(data[i]) && !isInlineDictDelimiter(data[i]) {
				i++
			}
			tokens = append(tokens, string(data[start:i]))
		}
	}
	return tokens
}

// isInlineDictDelimiter reports whether b is a PDF delimiter character that
// ends a token in an inline image dictionary (ISO 32000-1, 7.2.2, Table 2).
func isInlineDictDelimiter(b byte) bool {
	switch b {
	case '/', '[', ']', '(', ')', '<', '>', '{', '}', '%':
		return true
	}
	return false
}

// isPDFWhitespace returns true if b is a PDF whitespace character (SP, TAB, CR, LF, FF, NUL).
func isPDFWhitespace(b byte) bool {
	switch b {
	case ' ', '\t', '\r', '\n', '\f', 0:
		return true
	}
	return false
}

// unknownOperatorRule returns the correct rule for unknown content stream
// operators based on the PDF/A part.
func unknownOperatorRule(part int) Rule {
	// ISO 19005-1 numbers the undefined-operator rule §6.2.10 (Op_Undefined);
	// §6.2.2 t1 in part 1 is the unrelated OutputIntent rule. Parts 2-4 renumber
	// it to §6.2.2 t1.
	return ruleByPart(part, Rule_19005_1_6_2_10_t01, Rule_19005_2_6_2_2_t01, Rule_19005_3_6_2_2_t01, Rule_19005_4_6_2_2_t01)
}

// qNestingRule returns the correct rule for q/Q nesting depth violations
// based on the PDF/A part. PDF/A-4 does not define a q/Q nesting limit,
// so an empty rule is returned.
func qNestingRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_1_12_t08, Rule_19005_2_6_1_13_t08, Rule_19005_3_6_1_13_t08, Rule{})
}

// inlineImageFilterRule returns the correct rule for forbidden inline image
// filters based on the PDF/A part.
func inlineImageFilterRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_1_10_t02, Rule_19005_2_6_1_10_t01, Rule_19005_3_6_1_10_t01, Rule_19005_4_6_1_9_t01)
}
