package typography

import (
	"context"
	"log/slog"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
)

// MaxUseCMapChainDepth bounds how far an embedded encoding CMap's usecmap and
// /UseCMap chain is followed when reading attacker-controlled CMap stream
// data. Anti-DoS guard, not spec-mandated: real usecmap chains are 1-2 deep.
// The compliance package reads the same chains under the same cap.
const MaxUseCMapChainDepth = 5

// EmbeddedEncodingCIDMap reads the embedded encoding CMap stream encObj of a
// Type 0 font into the code-to-CID map veraPDF builds for it
// (PDCMap.getCMapFile): the stream's own mappings over the bases it names,
// in-program by usecmap and in its dictionary by /UseCMap (ISO 32000-1,
// 9.7.5.3, Table 120). A /UseCMap name is a predefined CMap
// ([cmap.NamedBase]); a /UseCMap stream is read the same way, recursively, up
// to [MaxUseCMapChainDepth].
//
// It returns nil when encObj is no stream litho can decode. A base litho
// cannot read, past the depth cap or reached again through a cycle, leaves
// the codes it could map unknown rather than unmapped
// ([cmap.CIDMap.UnmappedNotdef]): veraPDF raises a LoopedException on the
// cycle, and litho does not guess its verdict.
func EmbeddedEncodingCIDMap(ctx context.Context, doc *document.Document, encObj ast.PdfObject) *cmap.CIDMap {
	return embeddedEncodingCIDMapAt(ctx, doc, encObj, make(map[int]bool), 0)
}

// embeddedEncodingCIDMapAt is [EmbeddedEncodingCIDMap] carrying the CMap
// streams the chain has already read, and how deep it is.
func embeddedEncodingCIDMapAt(ctx context.Context, doc *document.Document, encObj ast.PdfObject, visited map[int]bool, depth int) *cmap.CIDMap {
	if ctx.Err() != nil || depth > MaxUseCMapChainDepth || document.VisitedBefore(encObj, visited) {
		return nil
	}
	stream, err := document.ResolveStream(ctx, doc, encObj)
	if err != nil {
		doc.Logger.Debug("typography: cannot resolve Type0 encoding CMap", slog.Any("err", err))
		return nil
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		doc.Logger.Debug("typography: cannot decode Type0 encoding CMap", slog.Any("err", err))
		return nil
	}
	var base cmap.Base
	if useObj, ok := stream.Dict["UseCMap"]; ok {
		resolved, err := document.ResolveObject(ctx, doc, useObj)
		if err != nil {
			// An unresolvable /UseCMap names no CMap, as below.
			doc.Logger.Debug("typography: cannot resolve /UseCMap", slog.Any("err", err))
		}
		// veraPDF's PDCMap reads any value but a name or a stream, a missing
		// object included, as naming no CMap, and CMap.useCMap ignores it.
		switch v := resolved.(type) {
		case ast.PdfName:
			base = cmap.NamedBase(string(v))
		case ast.PdfStream:
			base.Map = embeddedEncodingCIDMapAt(ctx, doc, useObj, visited, depth+1)
			base.Unresolved = base.Map == nil
		}
	}
	return cmap.ParseEmbeddedCIDMapOver(ctx, data, base)
}

// maxEncodingCodeBytes is the widest character code a CMap codespace can
// declare: Adobe Technical Note #5014, begincodespacerange, allows one to
// four bytes.
const maxEncodingCodeBytes = 4

// encodingCIDs is the code-to-CID reading of a Type 0 font's encoding CMap
// that its glyph metrics are looked up through ([Font.metricCID]).
type encodingCIDs struct {
	// cids holds the cidchar and cidrange mappings, those of the bases
	// included.
	cids *cmap.CIDMap
	// bf holds the bfchar and bfrange mappings of an embedded CMap stream,
	// which ISO 32000-1, 9.10.3 gives to ToUnicode CMaps and producers still
	// write into encoding CMaps; nil when the stream has none.
	bf *cmap.CMap
}

// resolveEncodingCIDs returns the code-to-CID reading of a Type 0 font's
// /Encoding when its codes are not its CIDs: an embedded CMap stream
// ([EmbeddedEncodingCIDMap]) or a predefined CMap other than Identity-H and
// Identity-V ([cmap.LoadPredefinedCIDMap]). It returns nil for the Identity
// CMaps, whose two-byte code is the CID (ISO 32000-1, 9.7.5.2, Table 118), and
// for an encoding litho cannot read, so [Font.metricCID] keeps the code.
func resolveEncodingCIDs(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) *encodingCIDs {
	encObj, ok := fontDict["Encoding"]
	if !ok {
		return nil
	}
	resolved, err := document.ResolveObject(ctx, doc, encObj)
	if err != nil {
		doc.Logger.Debug("resolving Type0 /Encoding for CIDs", slog.Any("err", err))
		return nil
	}
	switch enc := resolved.(type) {
	case ast.PdfName:
		if enc == "Identity-H" || enc == "Identity-V" {
			return nil
		}
		if m := cmap.LoadPredefinedCIDMap(string(enc)); m != nil {
			return &encodingCIDs{cids: m}
		}
	case ast.PdfStream:
		out := &encodingCIDs{cids: EmbeddedEncodingCIDMap(ctx, doc, encObj)}
		if data, err := doc.DecodeStream(ctx, enc); err == nil {
			if bf, err := cmap.Parse(ctx, data); err == nil && bf != nil && len(bf.Mapping) > 0 {
				out.bf = bf
			}
		}
		if out.cids != nil || out.bf != nil {
			return out
		}
	}
	return nil
}

// metricCID returns the key a Type 0 font's glyph metrics are looked up by
// for the character code code, which processText reads as charCode, the
// value of its first two bytes at most. The /W and
// /W2 arrays of a CIDFont are indexed by CID (ISO 32000-1, 9.7.4.3), and the
// font's encoding CMap maps the code to the CID (9.7.6.2), so a code its
// cidchar or cidrange mappings cover takes that CID. A code only a bfchar or
// bfrange entry maps takes the entry's destination read as a CID, and a code
// nothing maps keeps its own value: 9.7.6.3 sends it to CID 0, but text
// extraction is measured against PDF.js, which reads both cases this way
// (fonts.js, _charToGlyph and convertCidString), and so does charCode, which
// also comes back unchanged for a simple font and under an Identity CMap.
//
// # Corpus evidence
//
//   - pdfjs issue20232.pdf: Haru writes a UTF-8 encoding CMap mapping
//     <D09F> to CID 1055, and litho read the width of code 0xD09F, which /W
//     does not name, so every Cyrillic glyph took /DW 687.
//   - pdfjs issue15292.pdf: under embedded encoding CMaps whose CIDs are not
//     their codes, widths read at the code made "Re: A new straightforward
//     ..." one word. issue4402_reduced.pdf, issue9534_reduced.pdf (embedded) and
//     issue13343.pdf (90ms-RKSJ-H) lost or glued words the same way.
//   - pdfjs bug920426.pdf: an encoding CMap written with bfchar alone maps
//     <0020> to <0003>. Read as CID 0, which veraPDF gives it, every glyph
//     took the width of CID 0 and "Checkliste Service" lost "rvice" past the
//     page edge.
//   - All corpora: words match PDF.js on 4 more files and on none fewer.
func (f *Font) metricCID(code []byte, charCode int) int {
	if f.encodingCIDs == nil {
		return charCode
	}
	if f.encodingCIDs.cids != nil && len(code) <= maxEncodingCodeBytes {
		// The code's own value, not charCode, which carries two bytes at
		// most: a three-byte UTF-8 code is one code of its CMap.
		value := 0
		for _, b := range code {
			value = value<<8 | int(b)
		}
		if cid, ok := f.encodingCIDs.cids.MappedCID(value); ok {
			return cid
		}
	}
	if f.encodingCIDs.bf != nil {
		if dst, ok := f.encodingCIDs.bf.Mapping[string(code)]; ok {
			if r := []rune(dst); len(r) == 1 && r[0] != utf8.RuneError {
				return int(r[0])
			}
		}
	}
	return charCode
}
