package embed

import (
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// WriteIdentity writes the Type0 font at ref, an object the caller
// allocated for it, as a CIDFontType2 whose codes are glyph IDs: its
// CIDToGIDMap is Identity (ISO 32000-1, 9.7.4.2), so a string selects glyph
// gid by the code gid.
//
// The program keeps every glyph ID: the outlines of the glyphs outside keep,
// .notdef and the components of composite glyphs aside, are emptied in
// place, and the /W array and the ToUnicode CMap list every glyph the
// program has. The program is written uncompressed, as FontFile2, whatever
// its outlines, so only a program with glyf outlines draws: pass a
// TrueType program. A program that fails to subset is embedded whole, which
// logger reports at Debug. A nil logger discards.
//
// WriteIdentity is the text stamp's shape. A caller that can renumber codes
// takes a [Subset], which writes a much smaller font.
func (f *Font) WriteIdentity(alloc *writer.ObjAllocator, logger *slog.Logger, ref ast.PdfIndirectRef, keep []uint16) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	keepSet := make(map[uint16]bool, len(keep))
	for _, gid := range keep {
		keepSet[gid] = true
	}
	// On any subsetting failure the font is embedded whole so it still
	// renders. Correctness over size.
	program := f.raw
	if subset, err := subsetTTF(f.raw, keepSet); err == nil {
		program = subset
	} else {
		logger.Debug("TTF subsetting failed, falling back to full embedding", "err", err)
	}

	// FontFile2 stream: the TrueType bytes with /Length1 set to their
	// length. The stream is uncompressed; the core writer writes it
	// verbatim.
	fontFileRef := alloc.Alloc(ast.NewStream(ast.PdfDict{"Length1": ast.PdfInt(len(program))}, program))
	upe, _ := f.scaler()
	descriptorRef := f.writeDescriptor(alloc, f.postScriptName, "FontFile2", fontFileRef)
	cidFontRef := alloc.Alloc(ast.PdfDict{
		"Type":           ast.PdfName("Font"),
		"Subtype":        ast.PdfName("CIDFontType2"),
		"BaseFont":       ast.PdfName(f.postScriptName),
		"CIDSystemInfo":  identitySystemInfo(),
		"FontDescriptor": descriptorRef,
		"CIDToGIDMap":    ast.PdfName("Identity"),
		"W":              cidWidthArray(f.widths, upe),
	})
	toUnicode := make(map[uint16]string, len(f.gidToUnicode))
	for gid, r := range f.gidToUnicode {
		if r != 0 {
			toUnicode[gid] = string(r)
		}
	}
	cmapRef := alloc.Alloc(toUnicodeCMap(toUnicode))
	writeType0(alloc, ref, f.postScriptName, cidFontRef, cmapRef)
}
