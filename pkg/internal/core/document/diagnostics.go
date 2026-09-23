package document

import (
	"context"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
	"github.com/gotenberg/litho/pkg/internal/filter"
)

// StreamDiagnostic identifies an in-use stream object whose /Filter chain
// contains one or more filters litho cannot decode.
type StreamDiagnostic struct {
	// ObjectNum is the stream's object number.
	ObjectNum int
	// Filters are the undecodable filter names in the chain (e.g.
	// "JPXDecode" when the jpx plugin is not registered).
	Filters []string
}

// UndecodableStreams reports in-use stream objects whose /Filter chain
// contains a filter that neither the built-in decoders nor a registered
// plugin can handle (the JPX-without-plugin and unknown-filter cases). It is the static, whole-document equivalent of
// [filter.DecodeStreamChecked]'s incomplete signal, computed from the
// filter names without decoding the streams. Results are sorted by
// object number.
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
		if bad := d.undecodableFilters(stream); len(bad) > 0 {
			diags = append(diags, StreamDiagnostic{ObjectNum: objNum, Filters: bad})
		}
	}
	sort.Slice(diags, func(i, j int) bool { return diags[i].ObjectNum < diags[j].ObjectNum })
	return diags, nil
}

// StreamHasUndecodableFilter reports whether the stream's /Filter chain contains
// a filter that neither a built-in decoder nor a registered plugin can decode, so
// [Document.DecodeStream] would leave the bytes filter-encoded. Content-analysis
// callers use it to skip operator parsing on such a stream, whose still-encoded
// bytes would otherwise be misread as a run of undefined operators.
func (d *Document) StreamHasUndecodableFilter(stream ast.PdfStream) bool {
	return len(d.undecodableFilters(stream)) > 0
}

// UndecodableFilters returns the names in the stream's /Filter chain that
// neither a built-in decoder nor a registered plugin can decode, in chain
// order. [Document.NewStreamReader] skips such a filter and hands back its
// input still encoded, so a caller that interprets the bytes must refuse
// instead of reading them.
//
// An indirect /Filter is resolved first, exactly as the decode path resolves
// it, so the answer describes the chain that decode would actually run.
func (d *Document) UndecodableFilters(ctx context.Context, stream ast.PdfStream) []string {
	return d.undecodableFilters(d.resolveStreamFilterKeys(ctx, stream))
}

// undecodableFilters returns the names in the stream's /Filter chain that
// no built-in decoder or registered plugin can decode.
func (d *Document) undecodableFilters(stream ast.PdfStream) []string {
	var bad []string
	for _, name := range streamFilterNames(stream) {
		if filter.Decodable(name) {
			continue
		}
		if d.Filters.Handles(name) {
			continue
		}
		bad = append(bad, string(name))
	}
	return bad
}

// streamFilterNames returns the /Filter chain as names, handling both the
// single-name and array forms. Indirect /Filter values are uncommon and
// are not resolved here.
func streamFilterNames(stream ast.PdfStream) []ast.PdfName {
	switch f := stream.Dict["Filter"].(type) {
	case ast.PdfName:
		return []ast.PdfName{f}
	case ast.PdfArray:
		names := make([]ast.PdfName, 0, len(f))
		for _, item := range f {
			if n, ok := item.(ast.PdfName); ok {
				names = append(names, n)
			}
		}
		return names
	}
	return nil
}
