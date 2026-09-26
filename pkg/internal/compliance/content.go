package compliance

import (
	"context"
	"fmt"
	"log/slog"
	"maps"

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
// A part veraPDF reads no operator from ([decodeContentStream]) adds nothing,
// and the page reads the parts it can: ISO 32000-1, 7.7.3.3 makes the array
// one content stream cut into parts, and one part that does not decode says
// nothing about the others. So does an entry of the array that is no stream,
// which veraPDF 1.30 skips as it concatenates the parts (COSArray.getData),
// and a /Contents that is neither a stream nor an array, from which it reads
// no content at all (PDPage.initializeContents): ISO 32000-1, 7.7.3.3, Table
// 30 allows only those two, and 7.3.9 makes a null value an absent entry. A
// page none of whose parts decodes has no content, and no rule judges an
// operator of it, as in veraPDF. The error is kept for a /Contents that does
// not resolve and for content past the page budget, which the callers answer
// conservatively.
//
// # Corpus evidence
//
//   - pdfjs bomb_giant.pdf: follows its text part with three /JBIG2Decode
//     parts, and veraPDF reports the text's unembedded font and untagged
//     content while litho dropped the page. Measured against veraPDF with the
//     undecodable part before and after the readable one, and with an unknown
//     filter name in its place.
//   - pdfjs issue17190_1.pdf: carries 77 parts, 37 of them /FlateDecode
//     streams with no data, which decode to nothing, and litho reported none
//     of the colour and font defects veraPDF raises from the other 40 while a
//     part that did not decode lost the whole page.
//   - All corpora: govdocs thread_200_200738.pdf is the only file whose
//     /Contents array lists a part twice, on 10 of its pages, and no count
//     moves at PDF/A-1b or PDF/A-2b.
//   - pdfjs issue18973.pdf: page 45 lists an entry that is no stream beside
//     two content streams, and litho judged every font of the page as if it
//     drew them and read none of its operators. Reading the two streams, the
//     file fails the embedding rule 31 times, where litho counted 29, and its
//     DeviceGray rule 2744 times, where litho counted 2725, at PDF/A-1b and
//     PDF/A-2b, as veraPDF does.
//   - pdfjs issue15803.pdf: page 1 has a null /Contents, which litho judged
//     as unreadable content showing text with no /Lang at PDF/UA-1. veraPDF
//     cannot parse the file.
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
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				doc.Logger.Debug("compliance: /Contents array entry is no stream; skipping",
					slog.Any("err", err))
				continue
			}
			streams = append(streams, stream)
		}
	default:
		return nil, nil
	}

	var allData []byte
	for _, s := range streams {
		data, reads := decodeContentStream(ctx, doc, s)
		if !reads {
			continue
		}
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

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return allData, nil
}

// contentFilters lists the filters veraPDF 1.30 decodes a stream through
// (COSFilterRegistry): the standard filters of ISO 32000-1, 7.4.1, Table 6
// that are no image codec, and Crypt, which it reads as the identity. The
// abbreviations ISO 32000-1, 8.9.7, Table 94 gives those filters for inline
// images, the image codecs (CCITTFaxDecode, JBIG2Decode, DCTDecode,
// JPXDecode), BrotliDecode and every other name are none of them.
//
//nolint:gochecknoglobals // package-level lookup table.
var contentFilters = map[ast.PdfName]bool{
	"ASCIIHexDecode":  true,
	"ASCII85Decode":   true,
	"LZWDecode":       true,
	"FlateDecode":     true,
	"RunLengthDecode": true,
	"Crypt":           true,
}

// decodeContentStream returns the bytes of the content stream s that veraPDF
// reads operators from, and false when it reads none: a page's content or a
// part of it, a form XObject, a tiling pattern, a Type 3 glyph description or
// an annotation appearance. Every compliance walk that judges the operators of
// a content stream reads it here, so that no rule judges an operator veraPDF
// does not read.
//
// ISO 19005 judges the operators a content stream holds and says nothing of a
// stream a reader cannot decode. litho follows veraPDF 1.30, which parses what
// the filters of the stream decode (GFPDContentStream.parseOperators) and
// reads:
//
//   - nothing when the last filter of the chain is one it does not decode
//     ([contentFilters]): its stand-in for that filter reads as an empty
//     stream. A filter it does not decode earlier in the chain hands its
//     input on unchanged to the next one, so the chain is read without it.
//   - nothing when decoding fails before the first byte, and the bytes
//     decoded before the damage when it fails later, as the filter chain
//     keeps them ([document.Document.DecodeStreamReport]).
//
// Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on a page per case,
// each selecting an unembedded font, filling in DeviceRGB and running an
// undefined operator: /FlateDecode data that is no zlib stream or whose first
// block is invalid, /AHx, /A85 and /Fl, /DCTDecode, an unknown filter, and
// [/FlateDecode /Foo] fail none of the three rules, where [/Foo /FlateDecode]
// over Flate data, Flate data cut short or damaged after the operators, and
// ASCIIHexDecode data damaged after them fail all three.
//
// # Corpus evidence
//
//   - pdfjs poppler-90-0-fuzzed.pdf: pages 7, 12 and 16 hold a stream that
//     fails to decode before its first byte, /A85 on page 7, and litho judged
//     every font the pages declare as if they drew it: 18 glyph widths and 7
//     unembedded fonts veraPDF does not report there, one of them R20, which
//     it reports on page 14, where a stream it reads draws it. Pages 9 and 10
//     are /AHx, from which litho read two undefined operators and two
//     DeviceGray uses that veraPDF does not report either. Every count now
//     matches veraPDF's on every page at PDF/A-1b, but for one 6.2.8 test on
//     page 1 this reading does not move, and so does every moved count at
//     PDF/A-2b and PDF/UA-1.
//   - pdfjs poppler-395-0-fuzzed.pdf: its one content stream fails to decode
//     before its first byte, and litho judged the unembedded font the page
//     declares, which veraPDF does not report.
//   - pdfjs Brotli-Prototype-FileA.pdf: 25 pages of /BrotliDecode content, from
//     which litho read 69185 device colour uses at PDF/A-1b and PDF/A-2b, and
//     four font findings at PDF/A-1b, five at PDF/A-2b. veraPDF cannot parse
//     the file, whose cross reference stream is itself Brotli data.
//   - pdfjs PDFBOX-3148-2-fuzzed.pdf: a page and a form in /A85, from which
//     litho read two DeviceGray uses. veraPDF cannot parse the file.
//   - All corpora: no other count moves at PDF/A-1b or PDF/A-2b, and no file
//     writes a filter veraPDF does not decode before the last of a chain,
//     which litho undecodable_2b.pdf plants.
func decodeContentStream(ctx context.Context, doc *document.Document, s ast.PdfStream) ([]byte, bool) {
	s, reads := veraPDFFilterChain(ctx, doc, s)
	if !reads {
		doc.Logger.Debug("compliance: content stream ends with a filter veraPDF does not decode; reading none")
		return nil, false
	}
	data, err := doc.DecodeStream(ctx, s)
	if err != nil {
		doc.Logger.Debug("compliance: content stream does not decode; reading none", slog.Any("err", err))
		return nil, false
	}
	return data, true
}

// veraPDFFilterChain returns s with the filters of its chain that veraPDF
// passes its input through unchanged left out (see [decodeContentStream]),
// and false when veraPDF reads nothing from it: the chain ends with a filter
// veraPDF does not decode, or its parameters do not fit it
// ([veraPDFDecodeParms]). A chain of [contentFilters] alone comes back as it
// is.
func veraPDFFilterChain(ctx context.Context, doc *document.Document, s ast.PdfStream) (ast.PdfStream, bool) {
	filterObj, err := document.ResolveObject(ctx, doc, s.Dict["Filter"])
	if err != nil {
		// The decode resolves /Filter again, and fails the stream for it.
		doc.Logger.Debug("compliance: cannot resolve content stream /Filter", slog.Any("err", err))
		return s, true
	}
	var chain ast.PdfArray
	switch f := filterObj.(type) {
	case ast.PdfName:
		chain = ast.PdfArray{f}
	case ast.PdfArray:
		chain = f
	default:
		return s, true
	}
	if len(chain) == 0 {
		return s, true
	}
	parms, fits := veraPDFDecodeParms(ctx, doc, s.Dict["DecodeParms"], len(chain))
	if !fits {
		return s, false
	}
	kept := make(ast.PdfArray, 0, len(chain))
	keptParms := make(ast.PdfArray, 0, len(chain))
	for i, item := range chain {
		name, err := document.ResolveName(ctx, doc, item)
		if err != nil || !contentFilters[name] {
			if i == len(chain)-1 {
				return s, false
			}
			continue
		}
		kept = append(kept, name)
		if parms != nil {
			keptParms = append(keptParms, parms[i])
		}
	}
	if len(kept) == len(chain) {
		return s, true
	}
	dict := make(ast.PdfDict, len(s.Dict))
	maps.Copy(dict, s.Dict)
	dict["Filter"] = kept
	if parms != nil {
		dict["DecodeParms"] = keptParms
	}
	s.Dict = dict
	return s, true
}

// veraPDFDecodeParms returns the /DecodeParms obj of a chain of n filters as
// one entry per filter, nil when obj gives none, and false when veraPDF reads
// nothing from the stream for them: veraPDF 1.30 (COSFilters.getInputStream)
// fails to decode a stream whose parameters, a dictionary or an array of
// them, count other than its filters, or whose array holds an entry that is
// neither a dictionary nor null. ISO 32000-1, 7.3.8.2, Table 5 gives the
// array one entry per filter, and a single dictionary to a single filter.
// Measured with veraPDF 1.30 at PDF/A-2b over Flate data: under [/Foo
// /FlateDecode], a dictionary holding a PNG predictor reads nothing, and an
// array giving it to /Foo and null to /FlateDecode reads the stream; under
// /FlateDecode, [<< >> << >>] and [5] read nothing, and [null] and 5 read it.
func veraPDFDecodeParms(ctx context.Context, doc *document.Document, obj ast.PdfObject, n int) (ast.PdfArray, bool) {
	parms, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		// Parameters that do not resolve give no filter any, as in the decode.
		doc.Logger.Debug("compliance: cannot resolve content stream /DecodeParms", slog.Any("err", err))
		return nil, true
	}
	switch p := parms.(type) {
	case ast.PdfDict:
		return ast.PdfArray{p}, n == 1
	case ast.PdfArray:
		for _, entry := range p {
			resolved, err := document.ResolveObject(ctx, doc, entry)
			if err != nil {
				continue
			}
			switch resolved.(type) {
			case ast.PdfDict, ast.PdfNull, nil:
			default:
				return nil, false
			}
		}
		return p, len(p) == n
	}
	return nil, true
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
