package glyph

import (
	"context"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// fontFileKeys are the font descriptor entries that embed a font program
// (ISO 32000-1, 9.9, Table 126), in the order they are read.
var fontFileKeys = [...]ast.PdfName{"FontFile", "FontFile2", "FontFile3"}

// embeddedProgram returns the decoded bytes and the parsed program a font
// descriptor embeds, or nil when it embeds none litho can read. A program
// that does not parse is logged and treated as absent, so the font draws as
// one the document does not embed.
func embeddedProgram(ctx context.Context, doc *document.Document, desc ast.PdfDict) ([]byte, program) {
	if desc == nil {
		return nil, nil
	}
	for _, key := range fontFileKeys {
		obj, ok := desc[key]
		if !ok {
			continue
		}
		data := decodeProgram(ctx, doc, obj)
		if len(data) == 0 {
			continue
		}
		prog, err := parseProgram(ctx, data)
		if err != nil {
			doc.Logger.Debug("glyph: embedded font program does not parse", slog.String("key", string(key)), slog.Any("err", err))
			continue
		}
		return data, prog
	}
	return nil, nil
}

// decodeProgram decodes the font program stream obj, through the document's
// font program cache when obj is an indirect reference. The cache bounds what
// it holds at [limits.Limits.MaxFontProgCacheBytes]. A nil entry, which the
// compliance checks record for a program whose deflate data is corrupt, is
// decoded again: a viewer draws what the filter recovers.
func decodeProgram(ctx context.Context, doc *document.Document, obj ast.PdfObject) []byte {
	ref, indirect := obj.(ast.PdfIndirectRef)
	if indirect {
		if data, ok := doc.CachedFontProgram(ref.ObjectNum); ok && data != nil {
			return data
		}
	}
	stream, err := document.ResolveStream(ctx, doc, obj)
	if err != nil || stream.Dict == nil {
		doc.Logger.Debug("glyph: resolving font program stream", slog.Any("err", err))
		return nil
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		// The filter keeps what it decoded before damage; a program cut
		// short still draws the glyphs it holds.
		doc.Logger.Debug("glyph: decoding font program", slog.Any("err", err))
	}
	if indirect && len(data) > 0 {
		doc.SetCachedFontProgram(ref.ObjectNum, data)
	}
	return data
}

// maxCIDs is the number of CIDs a CIDFont can address: ISO 32000-1, 9.7.4.3
// makes a CID a 16-bit value, so a /CIDToGIDMap stream holds at most 65536
// entries. A spec bound, doing anti-DoS duty on the table [parseCIDToGIDMap]
// allocates.
const maxCIDs = 1 << 16

// resolveCIDToGIDMap reads a CIDFontType2 font's /CIDToGIDMap (ISO 32000-1,
// 9.7.4.2, Table 117): nil for /Identity or an absent entry, which Table 117
// makes the default, and the table a stream holds otherwise.
func resolveCIDToGIDMap(ctx context.Context, doc *document.Document, obj ast.PdfObject) []uint16 {
	if obj == nil {
		return nil
	}
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		doc.Logger.Debug("glyph: resolving /CIDToGIDMap", slog.Any("err", err))
		return nil
	}
	stream, ok := resolved.(ast.PdfStream)
	if !ok {
		return nil
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		doc.Logger.Debug("glyph: decoding /CIDToGIDMap", slog.Any("err", err))
	}
	return parseCIDToGIDMap(data)
}

// parseCIDToGIDMap reads a /CIDToGIDMap stream's bytes: the glyph index of
// CID n is the big-endian value of bytes 2n and 2n+1 (ISO 32000-1, 9.7.4.2,
// Table 117). An odd trailing byte is dropped, and entries past [maxCIDs]
// name no CID. An empty stream gives an empty, non-nil table, under which
// every CID selects .notdef.
func parseCIDToGIDMap(data []byte) []uint16 {
	n := min(len(data)/2, maxCIDs)
	out := make([]uint16, n)
	for i := range out {
		out[i] = uint16(data[2*i])<<8 | uint16(data[2*i+1])
	}
	return out
}
