package compliance2

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
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
// inline images for PDF/A conformance.
//
//nolint:gochecknoglobals // package-level lookup table.
var forbiddenInlineImageFilters = map[string]bool{
	"LZW":       true,
	"LZWDecode": true,
	"Crypt":     true,
}

// checkContentGroup validates content stream operators for PDF/A conformance.
// It checks for unknown operators, q/Q nesting depth, and forbidden inline
// image filters. For PDF/UA profiles, no content stream checks are performed.
func checkContentGroup(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	if isPDFUA(profile) {
		slog.Debug("compliance_2/content: skipping for PDF/UA profile", "profile", profile)
		return nil
	}

	part := pdfaPart(profile)
	if part == 0 {
		slog.Debug("compliance_2/content: unknown profile part", "profile", profile)
		return nil
	}

	slog.Debug("compliance_2/content: starting checks", "profile", profile, "part", part)

	pages := collectPages(ctx, doc, catalog)
	var results []Result

	for i, page := range pages {
		if ctx.Err() != nil {
			slog.Debug("compliance_2/content: context cancelled during page iteration")
			return results
		}

		slog.Debug("compliance_2/content: checking page", "page", i+1)

		pageResults := checkPageContent(ctx, doc, page, part, i+1)
		results = append(results, pageResults...)
	}

	slog.Debug("compliance_2/content: checks complete", "violations", len(results))
	return results
}

// checkPageContent validates the content streams of a single page.
func checkPageContent(ctx context.Context, doc *document.Document, page ast.PdfDict, part, pageNum int) []Result {
	// Get raw decoded content stream bytes for inline image scanning.
	streamData, err := getPageContentBytes(ctx, doc, page)
	if err != nil {
		slog.Debug("compliance_2/content: failed to get content stream bytes",
			"page", pageNum, "error", err)
		return nil
	}

	if len(streamData) == 0 {
		slog.Debug("compliance_2/content: empty content stream", "page", pageNum)
		return nil
	}

	// Parse into operations for operator validation and q/Q nesting.
	ops, err := content.ParseStreamData(streamData)
	if err != nil {
		slog.Debug("compliance_2/content: failed to parse content stream",
			"page", pageNum, "error", err)
		return nil
	}

	slog.Debug("compliance_2/content: parsed operations", "page", pageNum, "count", len(ops))

	var results []Result

	// Check operators and q/Q nesting.
	qDepth := 0
	maxDepthSeen := 0

	for idx, op := range ops {
		// Check context every ~100 operators.
		if idx%100 == 0 && ctx.Err() != nil { //nolint:mnd // per-spec checkpoint interval
			slog.Debug("compliance_2/content: context cancelled during operator walk",
				"page", pageNum, "operator_index", idx)
			return results
		}

		// Check for unknown operators.
		if !validPDFOperators[op.Operator] {
			slog.Debug("compliance_2/content: unknown operator",
				"page", pageNum, "operator", op.Operator)
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
			slog.Debug("compliance_2/content: q/Q nesting exceeded",
				"page", pageNum, "depth", maxDepthSeen)
			results = append(results, failf(rule,
				"page %d: q/Q nesting depth %d exceeds %d", pageNum, maxDepthSeen, maxQNesting)...)
		}
	}

	// Check inline image filters in raw bytes.
	results = append(results, checkInlineImageFilters(streamData, part, pageNum)...)

	return results
}

// getPageContentBytes decodes and concatenates the content streams of a page,
// returning the raw bytes suitable for both parsing and inline image scanning.
func getPageContentBytes(ctx context.Context, doc *document.Document, page ast.PdfDict) ([]byte, error) {
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

	var allData []byte
	for _, s := range streams {
		data, err := filter.DecodeStream(s)
		if err != nil {
			return nil, fmt.Errorf("decoding content stream: %w", err)
		}
		allData = append(allData, data...)
		allData = append(allData, '\n')
	}

	return allData, nil
}

// checkInlineImageFilters scans raw content stream bytes for BI (Begin Inline
// Image) operators and checks whether any forbidden filters (LZW, Crypt) are
// used. The scan looks for the BI keyword followed by inline image dictionary
// key/value pairs, checking /F and /Filter entries.
func checkInlineImageFilters(data []byte, part, pageNum int) []Result {
	var results []Result

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
		filterVal := extractInlineImageFilter(dictBytes)
		if filterVal != "" {
			if forbiddenInlineImageFilters[filterVal] {
				slog.Debug("compliance_2/content: forbidden inline image filter",
					"page", pageNum, "filter", filterVal)
				results = append(results, failf(inlineImageFilterRule(part),
					"page %d: inline image uses forbidden filter %q", pageNum, filterVal)...)
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
func extractInlineImageFilter(dictBytes []byte) string {
	// Tokenize the dictionary bytes looking for /F or /Filter keys.
	// Inline image dictionaries use abbreviated key names.
	tokens := tokenizeInlineDict(dictBytes)
	for i := 0; i < len(tokens)-1; i++ {
		key := tokens[i]
		if key == "/F" || key == "/Filter" {
			val := tokens[i+1]
			// Strip leading '/' from name values.
			if len(val) > 0 && val[0] == '/' {
				return val[1:]
			}
			return val
		}
	}
	return ""
}

// tokenizeInlineDict performs a simple tokenization of inline image dictionary
// bytes, splitting on whitespace and recognizing /Name tokens.
func tokenizeInlineDict(data []byte) []string {
	var tokens []string
	i := 0
	for i < len(data) {
		// Skip whitespace.
		if isPDFWhitespace(data[i]) {
			i++
			continue
		}

		// Name token.
		if data[i] == '/' {
			start := i
			i++
			for i < len(data) && !isPDFWhitespace(data[i]) && data[i] != '/' {
				i++
			}
			tokens = append(tokens, string(data[start:i]))
			continue
		}

		// Other token (value).
		start := i
		for i < len(data) && !isPDFWhitespace(data[i]) && data[i] != '/' {
			i++
		}
		if i > start {
			tokens = append(tokens, string(data[start:i]))
		}
	}
	return tokens
}

// isPDFWhitespace returns true if b is a PDF whitespace character (SP, TAB, CR, LF, FF, NUL).
func isPDFWhitespace(b byte) bool {
	switch b {
	case ' ', '\t', '\r', '\n', '\f', 0:
		return true
	}
	return false
}

// --- Rule selectors by PDF/A part ---

// unknownOperatorRule returns the correct rule for unknown content stream
// operators based on the PDF/A part.
func unknownOperatorRule(part int) Rule {
	switch part {
	case 1:
		return Rule_19005_1_6_2_2_t01
	case 2:
		return Rule_19005_2_6_2_2_t01
	case 3:
		return Rule_19005_3_6_2_2_t01
	case 4:
		return Rule_19005_4_6_2_2_t01
	}
	return Rule{}
}

// qNestingRule returns the correct rule for q/Q nesting depth violations
// based on the PDF/A part. PDF/A-4 does not define a q/Q nesting limit,
// so an empty rule is returned.
func qNestingRule(part int) Rule {
	switch part {
	case 1:
		return Rule_19005_1_6_1_12_t08
	case 2:
		return Rule_19005_2_6_1_13_t08
	case 3:
		return Rule_19005_3_6_1_13_t08
	}
	return Rule{}
}

// inlineImageFilterRule returns the correct rule for forbidden inline image
// filters based on the PDF/A part.
func inlineImageFilterRule(part int) Rule {
	switch part {
	case 1:
		return Rule_19005_1_6_1_10_t02
	case 2:
		return Rule_19005_2_6_1_10_t01
	case 3:
		return Rule_19005_3_6_1_10_t01
	case 4:
		return Rule_19005_4_6_1_9_t01
	}
	return Rule{}
}
