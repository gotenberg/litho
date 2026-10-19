package typography

import (
	"context"
	"log/slog"
	"strings"
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
	return embeddedEncodingCIDMapAt(ctx, doc, encObj, false, make(map[int]bool), 0)
}

// ConformanceEncodingCIDMap is [EmbeddedEncodingCIDMap] for the conformance
// checks: a predefined base, named by usecmap or /UseCMap, resolves through
// the CMaps veraPDF 1.30.2 bundles ([cmap.ConformanceNamedBase]), as
// veraPDF's PDCMap and CMapParser load them, so that a base only veraPDF
// bundles (Adobe-KR-9, Adobe-Japan1-7) maps the codes it maps there. Text
// extraction keeps to [EmbeddedEncodingCIDMap].
func ConformanceEncodingCIDMap(ctx context.Context, doc *document.Document, encObj ast.PdfObject) *cmap.CIDMap {
	return embeddedEncodingCIDMapAt(ctx, doc, encObj, true, make(map[int]bool), 0)
}

// embeddedEncodingCIDMapAt is [EmbeddedEncodingCIDMap], reading predefined
// bases through the conformance set when conformance is set, and carrying the
// CMap streams the chain has already read, and how deep it is.
func embeddedEncodingCIDMapAt(ctx context.Context, doc *document.Document, encObj ast.PdfObject, conformance bool, visited map[int]bool, depth int) *cmap.CIDMap {
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
			if conformance {
				base = cmap.ConformanceNamedBase(string(v))
			} else {
				base = cmap.NamedBase(string(v))
			}
		case ast.PdfStream:
			base.Map = embeddedEncodingCIDMapAt(ctx, doc, useObj, conformance, visited, depth+1)
			base.Unresolved = base.Map == nil
		}
	}
	if conformance {
		return cmap.ParseConformanceCIDMapOver(ctx, data, base)
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
		if isCollectionCMapName(string(enc)) {
			if m := cmap.PredefinedCIDBase(string(enc)); m != nil {
				return &encodingCIDs{cids: m}
			}
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

// isCollectionCMapName reports whether name is the CMap of an Adobe character
// collection named in ISO 32000-1, 9.10.2 at one of its supplements, such as
// Adobe-Korea1-2: an identity over the collection's CIDs, declaring that
// CIDSystemInfo. The name lies outside 9.7.5.2, Table 118, but PDF.js bundles
// the file and decodes a font's codes through it (CMapFactory, builtInCMap),
// and so does veraPDF.
func isCollectionCMapName(name string) bool {
	rest, ok := strings.CutPrefix(name, "Adobe-")
	if !ok {
		return false
	}
	ordering, supplement, ok := strings.Cut(rest, "-")
	if !ok || supplement == "" || strings.IndexFunc(supplement, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return false
	}
	switch ordering {
	case "GB1", "CNS1", "Japan1", "Korea1":
		return true
	}
	return false
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
// The CID-keyed tables text falls back on are read at the same key
// ([Font.nextEncodingCode]), so a glyph's width and its character come from
// one CID.
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
	cid, _ := f.encodingCID(code, charCode)
	return cid
}

// encodingCID is [Font.metricCID], also reporting whether a mapping of the
// encoding CMap, cidchar, cidrange, bfchar or bfrange, gave the CID. When none
// did, cid is charCode.
func (f *Font) encodingCID(code []byte, charCode int) (cid int, mapped bool) {
	if f.encodingCIDs == nil {
		return charCode, false
	}
	if f.encodingCIDs.cids != nil && len(code) <= maxEncodingCodeBytes {
		// The code's own value, not charCode, which carries two bytes at
		// most: a three-byte UTF-8 code is one code of its CMap.
		value := 0
		for _, b := range code {
			value = value<<8 | int(b)
		}
		if cid, ok := f.encodingCIDs.cids.MappedCID(value); ok {
			return cid, true
		}
	}
	if f.encodingCIDs.bf != nil {
		if dst, ok := f.encodingCIDs.bf.Mapping[string(code)]; ok {
			if r := []rune(dst); len(r) == 1 && r[0] != utf8.RuneError {
				return int(r[0]), true
			}
		}
	}
	return charCode, false
}

// encodingCIDKeyed reports whether a Type 0 font's CID-keyed fallback tables
// ([Font.GIDToUnicode]) are read through its encoding CMap's cidchar and
// cidrange mappings ([Font.nextEncodingCode]) rather than at the two-byte
// code, which is the CID only under Identity-H and Identity-V (ISO 32000-1,
// 9.7.5.2, Table 118).
func (f *Font) encodingCIDKeyed() bool {
	return f.encodingCIDs != nil && f.encodingCIDs.cids != nil
}

// encodingCode is one character code a Type 0 font's encoding CMap splits
// off a shown string ([Font.nextEncodingCode]).
type encodingCode struct {
	// n is the byte length of the code and value its big-endian value.
	n, value int
	// cid is the CID the code selects, read as its metrics are
	// ([Font.metricCID]).
	cid int
	// mapped reports whether a mapping of the encoding CMap gave cid
	// ([Font.encodingCID]); when none did, cid is the code's value.
	mapped bool
	// undefined marks bytes no codespace range holds (ISO 32000-1, 9.7.6.3),
	// read one byte at a time.
	undefined bool
}

// nextEncodingCode splits the character code at the front of data by the
// codespace ranges of a Type 0 font's encoding CMap (ISO 32000-1, 9.7.6.2)
// and returns it with the CID it selects, read as its metrics are
// ([Font.metricCID]), so a code nothing maps keeps its value as PDF.js reads
// it. ok is false when [Font.encodingCIDKeyed] does not hold: an encoding
// CMap written with bfchar alone declares no CID, and the caller keeps
// reading a two-byte code.
func (f *Font) nextEncodingCode(data []byte) (code encodingCode, ok bool) {
	if !f.encodingCIDKeyed() || len(data) == 0 {
		return encodingCode{}, false
	}
	n, undefined := f.encodingCIDs.cids.CodeLength(data)
	code.n, code.undefined = min(n, maxEncodingCodeBytes), undefined
	for _, b := range data[:code.n] {
		code.value = code.value<<8 | int(b)
	}
	code.cid, code.mapped = f.encodingCID(data[:code.n], code.value)
	return code, true
}

// EncodingCode is one character code of a shown string as a Type 0 font's
// encoding CMap splits it ([Font.MappedEncodingCodes]).
type EncodingCode struct {
	// Code holds the bytes of the code.
	Code []byte
	// CID is the CID the CMap's cidchar and cidrange mappings give the code,
	// meaningful only when Mapped holds.
	CID int
	// Mapped reports whether such a mapping covers the code.
	Mapped bool
}

// MappedEncodingCodes splits data into the character codes of a Type 0 font's
// encoding CMap, by its codespace ranges (ISO 32000-1, 9.7.6.2), and gives
// each the CID its cidchar and cidrange mappings, or those of its bases, state
// ([cmap.CIDMap.MappedCID]).
//
// It is the split [Font.nextEncodingCode] performs, without the readings text
// extraction adds for a code no such mapping covers: the destination of a
// bfchar entry read as a CID, and the code's own value. Those follow PDF.js,
// not the file, so a code they would give a CID is returned with Mapped
// false, and so are bytes no codespace range holds. ok is false when the
// encoding states no CIDs litho reads: an Identity CMap, whose code is the CID
// (9.7.5.2, Table 118), an embedded CMap written with bfchar alone, or an
// encoding litho cannot read.
func (f *Font) MappedEncodingCodes(data []byte) (codes []EncodingCode, ok bool) {
	if !f.encodingCIDKeyed() {
		return nil, false
	}
	for len(data) > 0 {
		n, undefined := f.encodingCIDs.cids.CodeLength(data)
		n = min(max(n, 1), len(data))
		code := EncodingCode{Code: data[:n]}
		if !undefined && n <= maxEncodingCodeBytes {
			value := 0
			for _, b := range data[:n] {
				value = value<<8 | int(b)
			}
			code.CID, code.Mapped = f.encodingCIDs.cids.MappedCID(value)
		}
		codes = append(codes, code)
		data = data[n:]
	}
	return codes, true
}

// EncodingCodeSpaceRanges returns the codespace ranges of a Type 0 font's
// encoding CMap when [Font.MappedEncodingCodes] reads it, and nil otherwise.
func (f *Font) EncodingCodeSpaceRanges() []cmap.CodeSpaceRange {
	if !f.encodingCIDKeyed() {
		return nil
	}
	return f.encodingCIDs.cids.CodeSpaceRanges()
}
