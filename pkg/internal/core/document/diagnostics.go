package document

import (
	"context"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

// StreamDiagnostic identifies an in-use stream object whose /Filter chain
// contains one or more filters litho cannot decode.
type StreamDiagnostic struct {
	// ObjectNum is the stream's object number.
	ObjectNum int
	// Filters are the undecodable filter names in the chain, such as a
	// filter name no decoder knows, or a filter whose built-in decoder
	// refuses a feature the stream uses.
	Filters []string
}

// UndecodableStreams reports in-use stream objects whose /Filter chain
// contains a filter litho cannot decode: one neither a built-in decoder nor a
// registered plugin knows, such as an unknown filter, or one whose built-in
// decoder refuses a feature the stream uses. It is the whole-document
// equivalent of the skip [filter.DecodeStreamChecked] reports as incomplete,
// and lists exactly the filters decoding skips (see
// [Document.UndecodableFilters]). Damaged data, which DecodeStreamChecked also
// reports as incomplete, is not listed: its filter decodes, and hands on what
// the data holds before the damage. Results are sorted by object number.
//
// A stream whose chain holds a filter that can refuse a feature is decoded to
// find out, so the call costs a decode of every such stream.
//
// # Corpus evidence
//
//   - All corpora: no stream uses a feature its built-in decoder refuses, so
//     decoding changed the answer for no stream.
//   - All corpora: decoding the JPXDecode, JBIG2Decode, CCITTFaxDecode and
//     Crypt streams raised the summed time of the call from 5.5 s to 44 s.
//   - pdfjs issue17289.pdf: the slowest file, from under 10 ms to 3.8 s.
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
// coding, a Crypt filter other than Identity). A caller that interprets the
// bytes must refuse instead of reading them. See
// [filter.Decoder.UndecodableFilters] for which streams are decoded to answer.
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
