package remediation

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/hex"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// matchInlineImageFilter claims the inline image filter rule in every spelling
// the parts give it: ISO 19005-1, 6.1.10 test 2, ISO 19005-2 and 19005-3,
// 6.1.10 test 1, and ISO 19005-4, 6.1.9 test 1. A clause number alone is no
// rule id ([matchLZW]): ISO 19005-1 6.1.10 test 1 is the stream filter rule,
// which [fixLZW] repairs.
func matchInlineImageFilter(v compliance.Violation) bool {
	spec, clause, test := v.Rule.Specification, v.Rule.Clause, v.Rule.TestNumber
	switch {
	case spec == "ISO_19005_1" && clause == "6.1.10" && test == 2:
		return true
	case spec == "ISO_19005_2" && clause == "6.1.10" && test == 1:
		return true
	case spec == "ISO_19005_3" && clause == "6.1.10" && test == 1:
		return true
	case spec == "ISO_19005_4" && clause == "6.1.9" && test == 1:
		return true
	}
	return false
}

// inlineFilterNames maps every filter name an inline image may spell, the
// abbreviations of ISO 32000-1, 8.9.7, Table 94 and the full names of 7.4,
// Table 6, to the name the decoder reads it as. A name outside it, Brotli
// among them, is one the repair cannot prove it decodes.
//
//nolint:gochecknoglobals // static spec table, ISO 32000-1 Table 6 and Table 94
var inlineFilterNames = []ast.PdfName{
	"AHx", "ASCIIHexDecode",
	"A85", "ASCII85Decode",
	"LZW", "LZWDecode",
	"Fl", "FlateDecode",
	"RL", "RunLengthDecode",
	"CCF", "CCITTFaxDecode",
	"DCT", "DCTDecode",
	"JPXDecode", "JBIG2Decode",
	"Crypt",
}

// inlineCodecFilters are the filters of [inlineFilterNames] whose output is
// image samples rather than the bytes another filter reads: decoding one hands
// on pixels that no general-purpose filter can stand in for, so no filter
// before the last one the repair decodes may be one of them.
//
//nolint:gochecknoglobals // static spec table, ISO 32000-1 Table 6
var inlineCodecFilters = []ast.PdfName{
	"CCF", "CCITTFaxDecode",
	"DCT", "DCTDecode",
	"JPXDecode", "JBIG2Decode",
}

// inlinePart1Forbidden lists the inline image filters ISO 19005-1, 6.1.10
// forbids: LZWDecode, in either spelling. It mirrors compliance's
// forbiddenInlineImageFilters, the authority for the verdict, as Crypt does,
// which no decode of an inline image can stand for.
//
//nolint:gochecknoglobals // static spec table, mirrors compliance.forbiddenInlineImageFilters
var inlinePart1Forbidden = []ast.PdfName{"LZW", "LZWDecode", "Crypt"}

// inlineAllowed lists the inline image filters ISO 19005-2/3, 6.1.10 and ISO
// 19005-4, 6.1.9 leave a conforming file: the filters ISO 32000-2, 8.9.7,
// Table 92 abbreviates for an inline image, under either name, but LZWDecode.
// JPXDecode and JBIG2Decode are no inline image's ([inlineXObjectCodecs]). It
// mirrors compliance's allowedInlineImageFilters, the authority for the
// verdict, so the two must stay in sync.
//
//nolint:gochecknoglobals // static spec table, mirrors compliance.allowedInlineImageFilters
var inlineAllowed = []ast.PdfName{
	"AHx", "ASCIIHexDecode",
	"A85", "ASCII85Decode",
	"Fl", "FlateDecode",
	"RL", "RunLengthDecode",
	"CCF", "CCITTFaxDecode",
	"DCT", "DCTDecode",
}

// inlineFilterForbidden reports whether part of ISO 19005 forbids the filter
// name in an inline image: ISO 19005-1, 6.1.10 names LZWDecode alone, while
// ISO 19005-2/3, 6.1.10 and ISO 19005-4, 6.1.9 forbid every value outside
// [inlineAllowed], a name misspelled by case among them (PDF names are
// case-sensitive, 7.3.5).
func inlineFilterForbidden(name ast.PdfName, part int) bool {
	if part == 1 {
		return slices.Contains(inlinePart1Forbidden, name)
	}
	return !slices.Contains(inlineAllowed, name)
}

// canonicalInlineFilter returns the name of [inlineFilterNames] name spells,
// ignoring case, and false for a name outside it.
func canonicalInlineFilter(name ast.PdfName) (ast.PdfName, bool) {
	for _, want := range inlineFilterNames {
		if strings.EqualFold(string(name), string(want)) {
			return want, true
		}
	}
	return "", false
}

// fixInlineImageFilters re-encodes, in every content stream the pages of doc
// draw, each inline image whose filters include one the part of ISO 19005 the
// violations name forbids ([inlineFilterForbidden]): ISO 19005-1, 6.1.10
// forbids LZWDecode, and ISO 19005-2/3, 6.1.10 and ISO 19005-4, 6.1.9 forbid
// LZWDecode, Crypt and every value outside the abbreviations of ISO 32000-2,
// 8.9.7, Table 92. veraPDF judges each inline image as an image of its own, so
// a stream repair reaches what an object repair ([fixLZW]) does not: the
// image's dictionary and its data change together ([reencodeInlineImages]).
//
// No clause bounds the size of an inline image: ISO 32000-1, 8.9.7 recommends
// at most 4 KB, and veraPDF checks no size, so the image stays inline. The
// FlateDecode data that replaces LZWDecode data is never much larger.
//
// An image applying JPXDecode or JBIG2Decode moves to an image XObject instead
// ([moveInlineImage]): those filters are an image XObject's (ISO 32000-1,
// 7.4.9), and the move keeps the coded data byte for byte, so every viewer
// decodes the samples it decoded before. Decoding the image and re-encoding it
// with Flate would instead write one decoder's reading of a lossy JPEG 2000
// codestream into the file, and trade the codec's compression for Flate's.
//
// # Corpus evidence
//
//   - verapdf PDF_A-2b 6-1-10-t01-fail-a.pdf and fail-b.pdf, PDF_A-4
//     6-1-9-t01-fail-a.pdf to fail-d.pdf: inline images writing /LZW,
//     /LZWDecode, /Lzw and [/LZW]. Their 132 PDF/A remediations, default and
//     aggressive, at every PDF/A level, kept the finding on litho and veraPDF
//     1.30.2 alike, and now keep none; all but the PDF/A-4f ones, which keep
//     the embedded file that level requires, pass on both tools.
//   - pdfjs issue8613.pdf: two 1300-pixel-wide RGB images writing /Filter
//     [/ASCII85Decode /LZWDecode], 2.4 MB decoded. Their findings go at every
//     level, and every output draws the samples the source draws.
//   - litho a4syntax_4.pdf: its LZW image writes no EOD code (see
//     [decodeInlineData]); the finding goes at every level.
//   - All corpora: no govdocs, pdfcpu, pdf20 or facturx file holds an inline
//     image any part forbids, and no output gains a rule on either tool.
//   - litho inlinelzw_2b.pdf: holds such images in a form XObject, a Type 3
//     glyph, a tiling pattern cell and an annotation appearance, which no
//     other file does.
//   - litho inlinejpx_2b.pdf: the only file whose inline images apply
//     JPXDecode or JBIG2Decode, on its page and in a form XObject. Its five
//     findings go at every PDF/A level of parts 2 to 4, default and
//     aggressive, on litho and veraPDF 1.30.2 alike. Decoding the images and
//     re-encoding them inline with Flate, crafted by hand, passed veraPDF at
//     PDF/A-2b and PDF/A-4 too, so the tools do not choose between the
//     repairs.
func fixInlineImageFilters(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, violations []compliance.Violation, _ string) ([]string, error) {
	part := inlineFilterPart(violations)
	if part == 0 {
		return nil, nil
	}
	dec := filter.NewDecoder(nil, doc.Limits())
	var reencoded, moved int
	streams, err := walkContentStreamsWithResources(ctx, alloc, doc, "inline-filter", func(ctx context.Context, data []byte, res *resourceEdit) ([]byte, func()) {
		out, n, m := reencodeInlineImages(ctx, data, part, dec, res)
		if n+m == 0 {
			return nil, nil
		}
		return out, func() { reencoded += n; moved += m }
	})
	if err != nil {
		return nil, fmt.Errorf("re-encoding inline images: %w", err)
	}
	var fixes []string
	if reencoded > 0 {
		fixes = append(fixes, fmt.Sprintf("re-encoded %d inline image(s) with a forbidden filter as FlateDecode", reencoded))
	}
	if moved > 0 {
		fixes = append(fixes, fmt.Sprintf("moved %d inline image(s) with a forbidden filter to image XObjects", moved))
	}
	if len(fixes) == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("%s in %d content stream(s)", strings.Join(fixes, ", and "), streams)}, nil
}

// inlineFilterPart returns the part of ISO 19005 the violations
// [matchInlineImageFilter] claims judge, or 0 when they name none.
func inlineFilterPart(violations []compliance.Violation) int {
	for _, v := range violations {
		switch v.Rule.Specification {
		case "ISO_19005_1":
			return 1
		case "ISO_19005_2":
			return 2
		case "ISO_19005_3":
			return 3
		case "ISO_19005_4":
			return 4
		}
	}
	return 0
}

// inlineImageRewrite is one inline image [reencodeInlineImages] writes again:
// the bytes [start, end) of the stream, from its BI through its EI, the text
// written in their place, and the dictionary the text gives the image, or the
// name of the image XObject a Do in the text paints in its place.
type inlineImageRewrite struct {
	start, end int
	text       []byte
	dict       ast.PdfDict
	name       ast.PdfName
}

// replaces reports whether op, read where the image's BI was, is what the
// rewrite wrote: a BI with the dictionary it gave the image, or Do of the
// image XObject it moved the image to.
func (rw inlineImageRewrite) replaces(op content.Operation) bool {
	if len(op.Operands) != 1 {
		return false
	}
	if rw.name != "" {
		return op.Operator == "Do" && op.Operands[0] == ast.PdfObject(rw.name)
	}
	return beginsInlineImage(op) && reflect.DeepEqual(op.Operands[0], ast.PdfObject(rw.dict))
}

// reencodeInlineImages returns data with every inline image whose filters
// include one part forbids written again, and how many images it re-encoded
// ([reencodeInlineImage]) and moved to an image XObject res names
// ([moveInlineImage]). An image it cannot rewrite keeps its bytes, its finding
// a residual for validation to report.
//
// Each image is written whole, from its BI through its EI, and every other byte
// of the stream is carried across as it was. The result is read back both as a
// repair reads it and as validation reads it ([content.ParseStreamData]), and
// must hold the operations data holds, save the dictionaries of the images it
// rewrote, each as written; the repair's reading must also end each rewritten
// image at the EI written for it, so no reader takes a byte of what follows as
// data or a byte of the data as operators. Otherwise data comes back unchanged
// with counts of 0.
func reencodeInlineImages(ctx context.Context, data []byte, part int, dec *filter.Decoder, res *resourceEdit) ([]byte, int, int) {
	if !bytes.Contains(data, []byte("ID")) {
		return data, 0, 0
	}
	ops, ranges, err := content.ParseStreamDataWithOffsets(ctx, data, content.ForRepair())
	if err != nil || len(ops) != len(ranges) {
		return data, 0, 0
	}
	var rewrites []inlineImageRewrite
	moved := 0
	for i, op := range ops {
		if i%spliceCheckInterval == 0 && ctx.Err() != nil {
			return data, 0, 0
		}
		if !beginsInlineImage(op) || len(op.Operands) != 1 {
			continue
		}
		dict, ok := op.Operands[0].(ast.PdfDict)
		if !ok || !slices.ContainsFunc(inlineImageFilterNames(dict), func(n ast.PdfName) bool { return inlineFilterForbidden(n, part) }) {
			continue
		}
		if movesToXObject(dict, part) {
			if res == nil {
				continue
			}
			if rw, ok := moveInlineImage(ctx, data, ranges[i], dict, part, dec, res); ok {
				rewrites = append(rewrites, rw)
				moved++
			}
			continue
		}
		if rw, ok := reencodeInlineImage(ctx, data, ranges[i], dict, part, dec); ok {
			rewrites = append(rewrites, rw)
		}
	}
	if len(rewrites) == 0 {
		return data, 0, 0
	}

	var out bytes.Buffer
	out.Grow(len(data))
	prev := 0
	starts := make([]int, len(rewrites))
	for i, rw := range rewrites {
		out.Write(data[prev:rw.start])
		starts[i] = out.Len()
		out.Write(rw.text)
		prev = rw.end
	}
	out.Write(data[prev:])
	result := out.Bytes()

	if !rereadsRewritten(ctx, data, result, rewrites, starts) {
		return data, 0, 0
	}
	return result, len(rewrites) - moved, moved
}

// rereadsRewritten reports whether out, data with rewrites written at starts,
// reads as data does, save the rewritten images, each read as written
// ([inlineImageRewrite.replaces]), both as a repair reads it and as validation
// reads it, and whether the repair's reading spans each image written again
// inline exactly from its BI through the EI written for it.
func rereadsRewritten(ctx context.Context, data, out []byte, rewrites []inlineImageRewrite, starts []int) bool {
	want := make(map[int]ast.PdfDict, len(rewrites))
	for _, rw := range rewrites {
		want[rw.start] = rw.dict
	}
	for _, opts := range [][]content.Option{{content.ForRepair()}, nil} {
		before, err := content.ParseStreamData(ctx, data, opts...)
		if err != nil {
			return false
		}
		after, err := content.ParseStreamData(ctx, out, opts...)
		if err != nil || len(after) != len(before) {
			return false
		}
		changed := 0
		for i, op := range before {
			if sameOperation(after[i], op) {
				continue
			}
			if !beginsInlineImage(op) || !slices.ContainsFunc(rewrites, func(rw inlineImageRewrite) bool {
				return rw.replaces(after[i])
			}) {
				return false
			}
			changed++
		}
		if changed > len(rewrites) {
			return false
		}
	}

	var inline []inlineImageRewrite
	var inlineStarts []int
	for i, rw := range rewrites {
		if rw.name == "" {
			inline = append(inline, rw)
			inlineStarts = append(inlineStarts, starts[i])
		}
	}
	rewrites, starts = inline, inlineStarts
	ops, ranges, err := content.ParseStreamDataWithOffsets(ctx, out, content.ForRepair())
	if err != nil || len(ops) != len(ranges) {
		return false
	}
	next := 0
	for i, op := range ops {
		if next == len(starts) {
			break
		}
		if !beginsInlineImage(op) || ranges[i].End <= starts[next] {
			continue
		}
		rw := rewrites[next]
		end, ok := inlineImageEnd(out, ranges[i])
		if !ok || end != starts[next]+len(rw.text) || !bytes.HasSuffix(out[ranges[i].Start:end], rw.text) {
			return false
		}
		next++
	}
	return next == len(starts)
}

// reencodeInlineImage returns the rewrite of the inline image the operation
// spanning r of data begins, whose dictionary is dict: its data decoded
// through the last filter part forbids, encoded again with FlateDecode, and
// the filters after it kept with their parameters. A filter before that one
// misspelled by case is decoded under the name it spells, and one after it is
// renamed to that name when part forbids the misspelling. It returns false
// when the image cannot be rewritten so that it draws the same pixels:
//
//   - a filter is no name of ISO 32000-1, Table 6 or Table 94, whatever its
//     case;
//   - a filter up to the last forbidden one is an image codec
//     ([inlineCodecFilters]), whose samples no general-purpose filter stands
//     in for;
//   - the decode is incomplete ([filter.DecodeReport.Incomplete]), or the
//     data written does not decode to the bytes the kept filters read.
//
// The data that replaces it holds no "EI": readers find the end of inline
// image data by scanning for one (ISO 32000-1, 8.9.7), so FlateDecode data
// that happens to hold one is wrapped in ASCIIHexDecode, whose alphabet holds
// no I (7.4.2). A dictionary that states the data length, /L or /Length (ISO
// 32000-2, 8.9.7, Table 91), states the new one.
func reencodeInlineImage(ctx context.Context, data []byte, r content.ByteRange, dict ast.PdfDict, part int, dec *filter.Decoder) (inlineImageRewrite, bool) {
	start, end, raw, ok := inlineImageBody(ctx, data, r, dict)
	if !ok {
		return inlineImageRewrite{}, false
	}

	filterKey, parmsKey, ok := inlineFilterKeys(dict)
	if !ok {
		return inlineImageRewrite{}, false
	}
	names := inlineImageFilterNames(dict)
	parms, ok := inlineDecodeParms(dict[parmsKey], len(names))
	if !ok {
		return inlineImageRewrite{}, false
	}

	canon := make([]ast.PdfName, len(names))
	last := -1
	for i, n := range names {
		c, known := canonicalInlineFilter(n)
		if !known {
			return inlineImageRewrite{}, false
		}
		canon[i] = c
		if inlineFilterForbidden(n, part) && inlineFilterForbidden(c, part) {
			last = i
		}
	}
	for _, c := range canon[:last+1] {
		if slices.Contains(inlineCodecFilters, c) {
			return inlineImageRewrite{}, false
		}
	}

	var prefix []ast.PdfName
	var encoded []byte
	if last >= 0 {
		decoded, ok := decodeInlineData(ctx, dec, raw, canon[:last+1], parms[:last+1])
		if !ok {
			return inlineImageRewrite{}, false
		}
		prefix, encoded, ok = encodeInlineData(decoded)
		if !ok {
			return inlineImageRewrite{}, false
		}
		// The proof the kept filters read the bytes they read before: the
		// data written decodes, under the filters written, to them.
		back, ok := decodeInlineChain(ctx, dec, encoded, prefix, make([]ast.PdfObject, len(prefix)))
		if !ok || !bytes.Equal(back, decoded) {
			return inlineImageRewrite{}, false
		}
	} else {
		encoded = raw
	}

	newNames := slices.Clone(prefix)
	newParms := make([]ast.PdfObject, len(prefix))
	renamed := false
	for i := last + 1; i < len(names); i++ {
		n := names[i]
		if inlineFilterForbidden(n, part) {
			n = canon[i]
			renamed = true
		}
		newNames = append(newNames, n)
		newParms = append(newParms, parms[i])
	}
	if last < 0 && !renamed {
		return inlineImageRewrite{}, false
	}
	if renamed {
		// A name respelled is proven by the data decoding whole under it, as
		// [fixFilterNameCase] proves a stream's.
		if _, ok := decodeInlineData(ctx, dec, encoded, canonicalNames(newNames), newParms); !ok {
			return inlineImageRewrite{}, false
		}
	}

	newDict := maps.Clone(dict)
	setInlineFilters(newDict, filterKey, parmsKey, newNames, newParms)
	for _, k := range []ast.PdfName{"L", "Length"} {
		if _, ok := newDict[k]; ok {
			newDict[k] = ast.PdfInt(len(encoded))
		}
	}
	text, ok := inlineImageText(newDict, encoded)
	if !ok {
		return inlineImageRewrite{}, false
	}
	return inlineImageRewrite{start: start, end: end, text: text, dict: newDict}, true
}

// inlineImageEnd returns the offset just past the EI that ends the inline
// image of the operation spanning r of data: the reading of a stream ends an
// operation past the white space after its operator.
func inlineImageEnd(data []byte, r content.ByteRange) (int, bool) {
	end := r.End
	for end > r.Start && isWhiteSpaceByte(data[end-1]) {
		end--
	}
	if end-r.Start < len("EI") || string(data[end-len("EI"):end]) != "EI" {
		return 0, false
	}
	return end, true
}

// canonicalNames returns names with each spelled as [canonicalInlineFilter]
// reads it, for the decoder.
func canonicalNames(names []ast.PdfName) []ast.PdfName {
	out := make([]ast.PdfName, len(names))
	for i, n := range names {
		c, ok := canonicalInlineFilter(n)
		if !ok {
			c = n
		}
		out[i] = c
	}
	return out
}

// inlineImageDataStart returns the offset in image, the bytes of an inline
// image from its BI through its EI, of its data: past the ID that ends its
// dictionary, read as a repair reads it ([content.ForRepair]), and past the
// white-space byte that follows ID (ISO 32000-1, 8.9.7). It returns false when
// the dictionary read there is not dict, the one the reading of the stream
// gave the image.
func inlineImageDataStart(ctx context.Context, image []byte, dict ast.PdfDict) (int, bool) {
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(image), int64(len(image)), 0)).WithVeraPDFTokens().WithRepairTokens()
	p := parser.NewParser(lex).WithOffsetTracking()
	tok, err := p.Peek(0)
	if err != nil || tok.Type != lexer.TokenKeyword || tok.Value != "BI" {
		return 0, false
	}
	if _, err := p.Consume(); err != nil {
		return 0, false
	}
	got, err := p.ParseInlineImageHeader(ctx)
	if err != nil || !reflect.DeepEqual(got, dict) {
		return 0, false
	}
	idEnd := int(p.Position())
	if idEnd < len(image) && isWhiteSpaceByte(image[idEnd]) {
		idEnd++
	}
	if idEnd > len(image)-len("EI") {
		return 0, false
	}
	return idEnd, true
}

// isWhiteSpaceByte reports whether c is white space (ISO 32000-1, 7.2.2,
// Table 1).
func isWhiteSpaceByte(c byte) bool {
	switch c {
	case 0, '\t', '\n', '\f', '\r', ' ':
		return true
	}
	return false
}

// inlineImageFilterNames returns the names of the filters an inline image
// dictionary applies, as veraPDF and compliance read them: its /F entry, or
// /Filter when it has none (ISO 32000-1, 8.9.7, Table 93), a name or an array
// of names. A value of any other type, or an array holding anything but
// names, applies none.
func inlineImageFilterNames(dict ast.PdfDict) []ast.PdfName {
	v, ok := dict["F"]
	if !ok {
		v = dict["Filter"]
	}
	switch f := v.(type) {
	case ast.PdfName:
		return []ast.PdfName{f}
	case ast.PdfArray:
		names := make([]ast.PdfName, 0, len(f))
		for _, elem := range f {
			name, ok := elem.(ast.PdfName)
			if !ok {
				return nil
			}
			names = append(names, name)
		}
		return names
	}
	return nil
}

// inlineFilterKeys returns the keys an inline image dictionary spells its
// filters and their parameters with, the abbreviations /F and /DP or the full
// /Filter and /DecodeParms (ISO 32000-1, 8.9.7, Table 93). It returns false
// for a dictionary that writes a key both ways: veraPDF reads the
// abbreviation and a viewer may read either, so no rewrite of one keeps them
// agreeing.
func inlineFilterKeys(dict ast.PdfDict) (filterKey, parmsKey ast.PdfName, ok bool) {
	_, f := dict["F"]
	_, filterFull := dict["Filter"]
	_, dp := dict["DP"]
	_, dpFull := dict["DecodeParms"]
	if (f && filterFull) || (dp && dpFull) {
		return "", "", false
	}
	filterKey, parmsKey = "F", "DP"
	if filterFull {
		filterKey = "Filter"
	}
	if dpFull || (!dp && filterFull) {
		parmsKey = "DecodeParms"
	}
	return filterKey, parmsKey, true
}

// inlineDecodeParms returns the decode parameters of each of n filters from
// v, an inline image's /DP or /DecodeParms value: a dictionary for a single
// filter, or an array of a dictionary or null per filter (ISO 32000-1, 7.3.8.2,
// Table 5). A missing value gives every filter none. It returns false for any
// other shape, which no decode can be proven to read as a viewer does.
func inlineDecodeParms(v ast.PdfObject, n int) ([]ast.PdfObject, bool) {
	parms := make([]ast.PdfObject, n)
	switch p := v.(type) {
	case nil, ast.PdfNull:
		return parms, true
	case ast.PdfDict:
		if n != 1 {
			return nil, false
		}
		parms[0] = p
		return parms, true
	case ast.PdfArray:
		if len(p) != n {
			return nil, false
		}
		for i, e := range p {
			switch e.(type) {
			case ast.PdfDict:
				parms[i] = e
			case ast.PdfNull, nil:
			default:
				return nil, false
			}
		}
		return parms, true
	}
	return nil, false
}

// decodeInlineData decodes raw, the bytes of an inline image from past its ID
// up to its EI, through names, and reports whether the decode is complete. A
// white-space byte before the EI is the separator readers look for (ISO
// 32000-1, 8.9.7), which a filter with no end-of-data marker of its own, such
// as LZWDecode data that writes no EOD code, reads as damage: such data is
// decoded without it, and is complete only when the decode without it gives
// the bytes the decode with it gives up to the damage, so a viewer that reads
// the byte as data draws the same pixels.
//
// # Corpus evidence
//
//   - litho a4syntax_4.pdf: its LZW image's data ends with no EOD code, and
//     the space before its EI read as data made the decode damaged.
func decodeInlineData(ctx context.Context, dec *filter.Decoder, raw []byte, names []ast.PdfName, parms []ast.PdfObject) ([]byte, bool) {
	if decoded, ok := decodeInlineChain(ctx, dec, raw, names, parms); ok {
		return decoded, true
	}
	if len(raw) == 0 || !isWhiteSpaceByte(raw[len(raw)-1]) {
		return nil, false
	}
	trimmed, ok := decodeInlineChain(ctx, dec, raw[:len(raw)-1], names, parms)
	if !ok {
		return nil, false
	}
	prefix, err := dec.DecodeStream(ctx, inlineChainStream(raw, names, parms))
	if err != nil || !bytes.Equal(prefix, trimmed) {
		return nil, false
	}
	return trimmed, true
}

// decodeInlineChain decodes raw through names, each with its parameters, and
// reports whether the decode is complete ([filter.DecodeReport.Incomplete]).
func decodeInlineChain(ctx context.Context, dec *filter.Decoder, raw []byte, names []ast.PdfName, parms []ast.PdfObject) ([]byte, bool) {
	decoded, report, err := dec.DecodeStreamReport(ctx, inlineChainStream(raw, names, parms))
	if err != nil || report.Incomplete() {
		return nil, false
	}
	return decoded, true
}

// inlineChainStream returns raw as the body of a stream filtered by names,
// each with its parameters, for the decoder.
func inlineChainStream(raw []byte, names []ast.PdfName, parms []ast.PdfObject) ast.PdfStream {
	filters := make(ast.PdfArray, len(names))
	dps := make(ast.PdfArray, len(names))
	for i, n := range names {
		filters[i] = n
		dps[i] = ast.PdfNull{}
		if parms[i] != nil {
			dps[i] = parms[i]
		}
	}
	return ast.NewStream(ast.PdfDict{"Filter": filters, "DecodeParms": dps}, raw)
}

// encodeInlineData returns decoded encoded with FlateDecode, and the filters
// that decode it, abbreviated as ISO 32000-1, 8.9.7, Table 94 spells them in
// an inline image. Data that holds "EI" is wrapped in ASCIIHexDecode as well
// (see [reencodeInlineImage]).
func encodeInlineData(decoded []byte) ([]ast.PdfName, []byte, bool) {
	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	if _, err := w.Write(decoded); err != nil {
		return nil, nil, false
	}
	if err := w.Close(); err != nil {
		return nil, nil, false
	}
	if !bytes.Contains(compressed.Bytes(), []byte("EI")) {
		return []ast.PdfName{"Fl"}, compressed.Bytes(), true
	}
	encoded := make([]byte, hex.EncodedLen(compressed.Len())+1)
	hex.Encode(encoded, compressed.Bytes())
	encoded[len(encoded)-1] = '>'
	return []ast.PdfName{"AHx", "Fl"}, bytes.ToUpper(encoded), true
}

// setInlineFilters writes names and their parameters into dict under
// filterKey and parmsKey: a single filter as a name, and its parameters as a
// dictionary, several as arrays, and no parameters key when no filter takes
// any.
func setInlineFilters(dict ast.PdfDict, filterKey, parmsKey ast.PdfName, names []ast.PdfName, parms []ast.PdfObject) {
	delete(dict, filterKey)
	delete(dict, parmsKey)
	if len(names) == 0 {
		return
	}
	hasParms := slices.ContainsFunc(parms, func(p ast.PdfObject) bool { return p != nil })
	if len(names) == 1 {
		dict[filterKey] = names[0]
		if hasParms {
			dict[parmsKey] = parms[0]
		}
		return
	}
	arr := make(ast.PdfArray, len(names))
	for i, n := range names {
		arr[i] = n
	}
	dict[filterKey] = arr
	if hasParms {
		dps := make(ast.PdfArray, len(parms))
		for i, p := range parms {
			dps[i] = ast.PdfNull{}
			if p != nil {
				dps[i] = p
			}
		}
		dict[parmsKey] = dps
	}
}

// inlineImageText returns the inline image dict and data spell: BI, the
// dictionary's entries in key order, ID and a space, the data, and an EI after
// a line feed, the white space readers look for before it (ISO 32000-1,
// 8.9.7).
func inlineImageText(dict ast.PdfDict, data []byte) ([]byte, bool) {
	var buf bytes.Buffer
	buf.WriteString("BI")
	for _, k := range slices.Sorted(maps.Keys(dict)) {
		buf.WriteByte(' ')
		if _, err := writer.SerializeName(&buf, k); err != nil {
			return nil, false
		}
		buf.WriteByte(' ')
		if err := writeOperand(&buf, dict[k]); err != nil {
			return nil, false
		}
	}
	buf.WriteString(" ID ")
	buf.Write(data)
	buf.WriteString("\nEI")
	return buf.Bytes(), true
}
