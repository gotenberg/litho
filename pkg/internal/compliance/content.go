package compliance

import (
	"context"
	"fmt"
	"log/slog"

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
		if content.IsUndefined(op.Operator) {
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

	results = append(results, checkInlineImageFilters(ops, part, pageNum)...)

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

// checkInlineImageFilters states the inline image filter rule, ISO 19005-1,
// 6.1.10 t2 and ISO 19005-2/3, 6.1.10 t1 and ISO 19005-4, 6.1.9 t1, for the
// inline images of a content stream whose operations are ops, once for each
// image whose filters include one the part forbids. [checkPageContent] states
// it for a page's content, and [checkDrawnStream] for every form XObject,
// tiling pattern, Type3 glyph and annotation appearance the pages draw, once
// for each stream as [drawnStreamWalk] reaches it. Measured with veraPDF 1.30
// at PDF/A-1b and PDF/A-2b, an LZW inline image fails the rule once in each
// of those streams, once for a form XObject two pages paint or one page
// paints twice, and not at all in a form XObject no Do paints.
//
// An inline image veraPDF reads as part of the check of the EI before it has
// no BI operation of its own (see [content.ParseStreamData]), so no rule
// judges it.
//
// # Corpus evidence
//
//   - verapdf 6-1-9-t01-fail-c: its inline image writes /F /Lzw, and once the
//     file is remediated to PDF/A-2a veraPDF fails it as "a value not listed in
//     Table 6" (verified live), so a name outside Table 6 is forbidden in parts
//     2 to 4.
//   - All corpora: judging the dictionaries of the BI operations, rather
//     than the raw bytes of the stream, changes no count of the rule at
//     PDF/A-1b or PDF/A-2b, veraPDF files at their own profile.
func checkInlineImageFilters(ops []content.Operation, part, pageNum int) []Violation {
	var results []Violation
	for _, op := range ops {
		if op.Operator != "BI" || len(op.Operands) == 0 {
			continue
		}
		dict, ok := op.Operands[0].(ast.PdfDict)
		if !ok {
			continue
		}
		for _, filterVal := range inlineImageFilters(dict) {
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

// inlineImageFilters returns the names of the filters an inline image
// dictionary applies, as veraPDF reads them (PDInlineImage.getCOSFilters): its
// /F entry, or its /Filter entry when it has none, since an abbreviation the
// dictionary holds comes before the full key (ISO 32000-1, 8.9.7, Table 93),
// holding a name or an array of names (ISO 32000-1, 7.4.1). A value of any
// other type, or an array holding anything but names, applies none.
//
// # Corpus evidence
//
//   - pdfjs issue14256.pdf: comments out lines of its inline image
//     dictionaries, so a key written inside a comment is no key.
func inlineImageFilters(dict ast.PdfDict) []string {
	v, ok := dict["F"]
	if !ok {
		v = dict["Filter"]
	}
	switch f := v.(type) {
	case ast.PdfName:
		return []string{string(f)}
	case ast.PdfArray:
		names := make([]string, 0, len(f))
		for _, elem := range f {
			name, ok := elem.(ast.PdfName)
			if !ok {
				return nil
			}
			names = append(names, string(name))
		}
		return names
	}
	return nil
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
