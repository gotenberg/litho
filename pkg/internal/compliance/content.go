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
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// forbiddenInlineImageFilters lists filter names that are not permitted in
// inline images for PDF/A conformance. ISO 19005-1 §6.1.10 forbids
// LZWDecode (and its inline abbreviation LZW) and Crypt; the abbreviated
// filter names are those of ISO 32000-1 Table 92 (and the full names of
// Table 6). Parts 2 to 4 use the positive [allowedInlineImageFilters] set.
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
		// A content stream an earlier page shares is judged there alone; see
		// [markJudgedContents].
		if vctx.pages[i].contentJudged {
			continue
		}

		pageResults := checkPageContent(ctx, doc, &vctx.pages[i], part, i+1)
		results = append(results, pageResults...)
	}

	doc.Logger.Debug("compliance.content: checks complete", "violations", len(results))
	return results
}

// checkPageContent validates the content streams of a single page.
//
// The q/Q nesting limit, ISO 19005-1, 6.1.12 t8 and ISO 19005-2/3, 6.1.13 t8,
// fails once for every q that opens a level past [maxQNesting]: veraPDF states
// it over each q operator (Op_q_gsave, "nestingLevel <= 28"). Measured with
// veraPDF 1.30 at PDF/A-1b and PDF/A-2b, 29 nested levels fail it once, 31
// levels three times, and two runs of 29 levels twice.
//
// # Corpus evidence
//
//   - litho limits_2b.pdf: nests 30 levels, and veraPDF fails the test twice at
//     PDF/A-1b and PDF/A-2b, where litho failed it once.
//   - pdfjs bug1130815.pdf: runs 124 q past the limit on its first page, 145
//     levels deep at most, which litho stated once. veraPDF cannot parse the
//     file (it finds no cross-reference table), so it has no count to compare.
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
	nestingRule := qNestingRule(part)

	for idx, op := range ops {
		// Check context every ~100 operators.
		if idx%100 == 0 && ctx.Err() != nil {
			return results
		}

		// Check for unknown operators.
		if !content.IsOperator(op.Operator) {
			results = append(results, failf(unknownOperatorRule(part),
				"page %d: operator %q", pageNum, op.Operator)...)
		}

		// Track q/Q nesting depth.
		switch op.Operator {
		case "q":
			qDepth++
			if qDepth > maxQNesting && nestingRule.Clause != "" {
				results = append(results, failf(nestingRule,
					"page %d: q/Q nesting depth %d exceeds %d", pageNum, qDepth, maxQNesting)...)
			}
		case "Q":
			if qDepth > 0 {
				qDepth--
			}
		}
	}

	// Check inline image filters in raw bytes.
	results = append(results, checkInlineImageFilters(page.content, part, pageNum)...)

	return results
}

// resolvePageContentBytes decodes and concatenates the content streams of a page,
// returning the raw bytes suitable for both parsing and inline image scanning.
//
// A stream the /Contents array lists more than once is read where the array
// first lists it, and only there. Measured with veraPDF 1.30 at PDF/A-1b and
// PDF/A-2b on a page whose array lists one stream twice, directly or through
// an indirect array, or a stream, a second one and the first again: every
// content-stream rule fails as often as it does when the array lists each
// stream once, the q/Q nesting limit included, which 15 unclosed q in the
// stream listed twice would reach if its operators were read twice.
//
// # Corpus evidence
//
//   - pdfjs bomb_giant.pdf: follows its text part with three /JBIG2Decode
//     parts, and veraPDF reports the text's unembedded font and untagged
//     content while litho dropped the page. Measured against veraPDF with the
//     undecodable part before and after the readable one, and with an unknown
//     filter name in its place.
//   - pdfjs issue17190_1.pdf: carries 77 parts, one of them unreadable, and
//     litho reported none of the colour and font defects veraPDF raises from
//     the other 76 while a truncated part lost the whole page.
//   - All corpora: govdocs thread_200_200738.pdf is the only file whose
//     /Contents array lists a part twice, on 10 of its pages, and no count
//     moves at PDF/A-1b or PDF/A-2b.
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
		listed := make(map[int]bool, len(v))
		for _, item := range v {
			// ISO 32000-1, 7.7.3.3 makes the array one content stream cut
			// into parts, and veraPDF reads a part the array lists again as
			// no part at all: it judges the page as if the array held each
			// stream once, where it first lists it.
			if ref, ok := item.(ast.PdfIndirectRef); ok {
				if listed[ref.ObjectNum] {
					continue
				}
				listed[ref.ObjectNum] = true
			}
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
		// parts it can read.
		if doc.StreamHasUndecodableFilter(ctx, s) {
			undecodablePart = true
			continue
		}
		data, err := doc.DecodeStream(ctx, s)
		if err != nil {
			// ISO 32000-1, 7.7.3.3 lets a page split its content across an
			// array of streams, and one truncated part says nothing about the
			// rest. Losing the whole page for it threw away every operator the
			// other parts hold. Viewers render what they can, and
			// content.ParsePageContent already skips the part and keeps
			// going.
			doc.Logger.Debug("compliance: content stream part failed to decode; skipping",
				slog.Any("err", err))
			lastErr = err
			continue
		}
		decoded++
		allData = append(allData, data...)
		allData = append(allData, '\n')

		// Bounded for the reason given on limits.Limits.MaxPageContentBytes:
		// the per-stream budget does not bound the sum across a /Contents
		// array, and this buffer feeds every page-scoped check. Stopping here
		// leaves the page looking unreadable, which the callers already answer
		// conservatively, rather than letting a crafted page grow the buffer
		// with the file's object count.
		if limit := doc.Limits().MaxPageContentBytes(); int64(len(allData)) > limit {
			return nil, fmt.Errorf("%w of %d bytes", limits.ErrPageContentTooLarge, limit)
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
//
// An inline image whose BI follows the EI of the one before it, with nothing
// but white space and comments between, is not judged. veraPDF reads the
// token after an EI to confirm the image ends there (PDFStreamParser), and
// when that token is a BI it reads the whole next image as part of that
// check and never gives it an operator of its own, so none of its rules
// reaches it. Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b: two, or
// three, LZW inline images in a row fail ISO 19005-1, 6.1.10 t2 and ISO
// 19005-2, 6.1.10 t1 once, whether their dictionaries and data match or
// not, and whether a comment, or the end of one /Contents part, stands
// between them. A plain inline image followed by an LZW one fails neither,
// and an LZW one followed by a plain DeviceRGB one reports no DeviceRGB. An
// operator between two LZW inline images, even q, Q or n, makes them fail
// twice. veraPDF resumes after the swallowed image at an offset that depends
// on the two images' data lengths, which can leave an EI it reports as an
// undefined operator; that reading is not reproduced here.
//
// # Corpus evidence
//
//   - verapdf 6-1-9-t01-fail-c: its inline image writes /F /Lzw, and once the
//     file is remediated to PDF/A-2a veraPDF fails it as "a value not listed in
//     Table 6" (verified live), so a name outside Table 6 is forbidden in parts
//     2 to 4.
//   - All corpora: no page content holds an inline image in a row after
//     another, so no count moves at PDF/A-1b or PDF/A-2b.
func checkInlineImageFilters(data []byte, part, pageNum int) []Violation {
	var results []Violation

	// prevEnd is the offset just past the EI of the previous inline image, or
	// -1 when that image found no EI. noEnd records that one found none, so
	// none after it can (see [inlineImageEnd]).
	prevEnd := -1
	noEnd := false

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
		swallowed := prevEnd >= 0 && onlySpaceAndComments(data[prevEnd:absIdx])
		// The image data runs to its EI, so the next BI is searched past it.
		offset, prevEnd = idIdx+2, -1
		if !noEnd {
			prevEnd = inlineImageEnd(data, offset)
			noEnd = prevEnd < 0
		}
		if prevEnd >= 0 {
			offset = prevEnd
		}
		if swallowed {
			continue
		}

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
				// fails "a value not listed in Table 6".
				allowed, known := allowedInlineImageFilters[filterVal]
				forbidden = !known || !allowed
			}
			if forbidden {
				results = append(results, failf(inlineImageFilterRule(part),
					"page %d: inline image uses forbidden filter %q", pageNum, filterVal)...)
				break
			}
		}
	}

	return results
}

// inlineImageEnd returns the offset just past the EI that ends the data of an
// inline image whose ID keyword ends at start, or -1 when no EI does. As
// veraPDF reads it (PDFStreamParser), the data begins after the one white-space
// byte following ID (ISO 32000-1, 8.9.7) and ends at the first EI followed by
// white space or the end of the stream, unless the keyword after that EI names
// no operator, which makes the EI part of the data (see [nextKeyword]).
//
// When no EI after start ends the data, none after a later start does
// either, so a caller that gets -1 need not ask again further on.
func inlineImageEnd(data []byte, start int) int {
	if start < len(data) && isPDFWhitespace(data[start]) {
		start++
	}
	for i := start; i+1 < len(data); i++ {
		if data[i] != 'E' || data[i+1] != 'I' {
			continue
		}
		end := i + 2
		if end < len(data) && !isPDFWhitespace(data[end]) {
			continue
		}
		if kw := nextKeyword(data[end:]); kw != "" && !content.IsOperator(kw) {
			continue
		}
		return end
	}
	return -1
}

// nextKeyword returns the run of regular characters the next token of b
// starts with, past white space, as veraPDF reads an operator: it ends at
// white space, a delimiter or a digit (ISO 32000-1, 7.2.2). It returns "" when
// the next token is a number, a name, a string, an array or a dictionary, when
// a comment comes first, or when b holds no token.
//
// veraPDF reads past a comment to the token after it. Stopping at the comment
// keeps [inlineImageEnd] linear: past it, every EI a long comment holds would
// read the rest of that comment again.
func nextKeyword(b []byte) string {
	i := 0
	for i < len(b) && isPDFWhitespace(b[i]) {
		i++
	}
	if i == len(b) || isInlineDictDelimiter(b[i]) || strings.IndexByte("+-.0123456789", b[i]) >= 0 {
		return ""
	}
	start := i
	for i < len(b) && !isPDFWhitespace(b[i]) && !isInlineDictDelimiter(b[i]) && (b[i] < '0' || b[i] > '9') {
		i++
	}
	// d0 and d1 keep their digit (ISO 32000-1, 9.6.5, Table 113).
	if i < len(b) && b[i-1] == 'd' && (b[i] == '0' || b[i] == '1') {
		i++
	}
	return string(b[start:i])
}

// onlySpaceAndComments reports whether b holds nothing but white space and
// comments (ISO 32000-1, 7.2.2 and 7.2.4).
func onlySpaceAndComments(b []byte) bool {
	for i := 0; i < len(b); i++ {
		switch {
		case isPDFWhitespace(b[i]):
		case b[i] == '%':
			for i < len(b) && b[i] != '\n' && b[i] != '\r' {
				i++
			}
		default:
			return false
		}
	}
	return true
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

// extractInlineImageFilters extracts the filter values from inline image
// dictionary bytes. It looks for /F or /Filter keys and returns the
// corresponding name values.
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
//
// # Corpus evidence
//
//   - pdfjs issue14256.pdf: comments out lines of its inline image
//     dictionaries, so a key written inside a comment must not be read.
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
			// key written inside one is no key.
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
