package document

import (
	"context"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

// StreamDiagnostic identifies an in-use stream object and the filters of its
// /Filter chain a diagnostic reports: the filters litho cannot decode
// ([Document.UndecodableStreams]), or those whose data decoding salvages
// ([Document.SalvagedStreams]).
type StreamDiagnostic struct {
	// ObjectNum is the stream's object number.
	ObjectNum int
	// Filters are the reported filter names in the chain, such as a filter
	// name no decoder knows, a filter whose built-in decoder refuses a
	// feature the stream uses, or a filter whose data is damaged.
	Filters []string
}

// UndecodableStreams reports in-use stream objects whose /Filter chain
// contains a filter litho cannot decode: one neither a built-in decoder nor a
// registered plugin knows, such as an unknown filter, or one whose built-in
// decoder refuses a feature the stream uses. It is the whole-document
// equivalent of the skip [filter.DecodeStreamChecked] reports as incomplete,
// and lists the filters decoding skips (see [Document.UndecodableFilters]).
// Damaged data, which DecodeStreamChecked also reports as incomplete, is not
// listed: its filter decodes, and hands on what the data holds before the
// damage. Results are sorted by object number.
//
// A stream whose chain holds a filter that can refuse a feature has the
// headers of its data read to find out, and its image data is not decoded
// (see [filter.Decoder.UndecodableFilters]).
//
// # Corpus evidence
//
//   - All corpora: no stream uses a feature its built-in decoder refuses, and
//     reading the headers gives the answer decoding every such stream gives,
//     on every file.
//   - All corpora: 8705 streams name a filter that can refuse a feature
//     (8061 CCITTFaxDecode, 337 JPXDecode, 301 JBIG2Decode, 6 Crypt), and
//     the headers settle every one, so the call decodes none of them.
//   - All corpora: with a refused construct planted in their JPXDecode,
//     JBIG2Decode and CCITTFaxDecode streams, 10,115 variants, the headers
//     agree with decoding on all but 25 JBIG2 streams that fail decoding
//     before the planted segment, 5 past the page-size cap and 20 malformed.
//   - All corpora: decoding those streams raised the summed time of the call
//     six- to tenfold. Reading their headers keeps it within the run-to-run
//     noise of reading the filter names alone.
//   - pdfjs issue17289.pdf: the slowest file to decode, 6.3 s, takes 3 ms.
//   - All corpora: 7,557 streams name DCTDecode, and the JPEG headers settle
//     every one. With a refused coding planted in the 6,363 whose only filter
//     it is, an arithmetic or 12-bit frame or a second frame before or after
//     the first scan, 25,452 variants, the call agrees with decoding on every
//     one: the headers settle all but a second frame past the first scan,
//     which the call decodes.
//   - All corpora: reading the JPEG headers raised the summed time of the
//     call from 6.95 s to 8.05 s, the least of two runs each. The largest
//     rise on one file, 54 ms, decodes the LZWDecode layer its JPEGs sit
//     behind (govdocs thread_400_400358.pdf).
func (d *Document) UndecodableStreams(ctx context.Context) ([]StreamDiagnostic, error) {
	var diags []StreamDiagnostic
	for objNum, entry := range d.XRefEntriesSnapshot() {
		if err := ctx.Err(); err != nil {
			return diags, err
		}
		if entry.Free || entry.Type != xref.XRefTypeNormal {
			continue
		}
		obj, err := d.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: objNum, GenNum: entry.Generation})
		if err != nil {
			continue
		}
		stream, ok := obj.(ast.PdfStream)
		if !ok {
			continue
		}
		bad, err := d.undecodableFilters(ctx, stream)
		if err != nil {
			return diags, err
		}
		if len(bad) > 0 {
			diags = append(diags, StreamDiagnostic{ObjectNum: objNum, Filters: bad})
		}
	}
	sort.Slice(diags, func(i, j int) bool { return diags[i].ObjectNum < diags[j].ObjectNum })
	return diags, nil
}

// SalvagedStreams reports in-use stream objects whose data decoding salvages:
// a filter of the chain met damaged data and kept what it decoded before the
// damage, or decoded the data whole although it breaks a rule of its format
// that does not delimit it, such as a zlib checksum missing or wrong or
// LZWDecode data without its EOD code. Filters lists those filters (see
// [filter.Decoder.SalvagedFilters]). Results are sorted by object number.
//
// litho reads such a stream as a viewer renders it, so nothing else reports
// it: its decoded bytes are whatever the data holds, and they may lack
// content the producer wrote. Compliance issues no verdict on it either. ISO
// 19005-2, 6.1.7 constrains the dictionary of a stream and the filters it may
// name, not the soundness of its encoded data, and veraPDF reads the data
// leniently too.
//
// The chain of each stream is decoded up to its first image codec, so the call
// decodes the content, fonts and other streams of the document, and the
// general filters in front of its images, but not the images themselves.
//
// # Corpus evidence
//
//   - All corpora: the 148,245 filtered streams, cut before their image
//     codec, decode to the bytes they decoded to before salvage was recorded,
//     eagerly and through the streaming reader.
//   - pdfjs, pdfcpu: 91 streams in 18 files are salvaged, all in pdfjs but
//     one: 56 FlateDecode streams miss their zlib checksum, 16 carry a wrong
//     one, 16 stop at a sync flush, 1 has a damaged zlib header, and 2
//     LZWDecode streams end without their EOD code.
//   - pdfjs issue17169.pdf, pdfcpu pkg_testdata__Paclitaxel.pdf: the two
//     LZWDecode streams without their EOD code.
//   - pdfjs, verapdf: 148 streams keep the prefix of damaged data, 109 behind
//     an ASCII filter and 39 behind FlateDecode; verapdf
//     PDF_A-2b__veraPDF_test_suite_6-2-3-t01-fail-b.pdf and
//     PDF_A-4__veraPDF_test_suite_6-2-3-t01-fail-b.pdf hold the two outside
//     pdfjs.
//   - govdocs, facturx, litho, pdf20: no stream is salvaged or damaged.
//   - All corpora: the call lists exactly the streams whose decode reports
//     a damaged or salvaged filter.
func (d *Document) SalvagedStreams(ctx context.Context) ([]StreamDiagnostic, error) {
	var diags []StreamDiagnostic
	for objNum, entry := range d.XRefEntriesSnapshot() {
		if err := ctx.Err(); err != nil {
			return diags, err
		}
		if entry.Free || entry.Type != xref.XRefTypeNormal {
			continue
		}
		obj, err := d.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: objNum, GenNum: entry.Generation})
		if err != nil {
			continue
		}
		stream, ok := obj.(ast.PdfStream)
		if !ok {
			continue
		}
		names, err := d.Filters.SalvagedFilters(ctx, d.resolveStreamFilterKeys(ctx, stream))
		if err != nil {
			return diags, err
		}
		if len(names) > 0 {
			bad := make([]string, len(names))
			for i, n := range names {
				bad[i] = string(n)
			}
			diags = append(diags, StreamDiagnostic{ObjectNum: objNum, Filters: bad})
		}
	}
	sort.Slice(diags, func(i, j int) bool { return diags[i].ObjectNum < diags[j].ObjectNum })
	return diags, nil
}

// StreamHasUndecodableFilter reports whether the stream's /Filter chain contains
// a filter litho cannot decode (see [Document.UndecodableFilters]), so
// [Document.DecodeStream] would leave the bytes filter-encoded. Content-analysis
// callers use it to skip operator parsing on such a stream, whose still-encoded
// bytes would otherwise be misread as a run of undefined operators. A cancelled
// ctx answers false.
func (d *Document) StreamHasUndecodableFilter(ctx context.Context, stream ast.PdfStream) bool {
	bad, err := d.undecodableFilters(ctx, stream)
	return err == nil && len(bad) > 0
}

// UndecodableFilters returns, in chain order, the names in the stream's
// /Filter chain that [Document.DecodeStream] skips because litho cannot decode
// them, handing back their input still encoded: a filter neither a built-in
// decoder nor a registered plugin knows, and a built-in one whose decoder
// refuses a feature the stream uses (a JPEG 2000 or JBIG2 construct, a CCITT
// or JPEG coding, a Crypt filter other than Identity). A caller that interprets the
// bytes must refuse instead of reading them. See
// [filter.Decoder.UndecodableFilters] for how the headers of such a stream's
// data answer, and for the rare stream decoded instead.
//
// A filter a registered plugin claims is listed only if the plugin declines
// the stream and the built-in decoder then refuses it. A declined stream whose
// filter litho has no decoder for fails with [filter.ErrUnsupportedFilter]
// rather than come back still encoded, so it is not listed.
//
// An indirect /Filter is resolved first, exactly as the decode path resolves
// it, so the answer describes the chain that decode would actually run. A
// cancelled ctx answers nil.
func (d *Document) UndecodableFilters(ctx context.Context, stream ast.PdfStream) []string {
	bad, err := d.undecodableFilters(ctx, stream)
	if err != nil {
		return nil
	}
	return bad
}

// undecodableFilters is [Document.UndecodableFilters], reporting a cancelled
// ctx as its error.
func (d *Document) undecodableFilters(ctx context.Context, stream ast.PdfStream) ([]string, error) {
	names, err := d.Filters.UndecodableFilters(ctx, d.resolveStreamFilterKeys(ctx, stream))
	if err != nil {
		return nil, err
	}
	var bad []string
	for _, name := range names {
		bad = append(bad, string(name))
	}
	return bad, nil
}
