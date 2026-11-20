package models

import "fmt"

// FontRefusal records a font a remediation left unembedded although a
// [github.com/gotenberg/litho/pkg/plugins.FontProvider] was configured. Values
// are safe to read concurrently.
type FontRefusal struct {
	// BaseFont is the font's /BaseFont.
	BaseFont string
	// Object is the object number of the font dictionary in the file the
	// pass that last judged it read: the document for the first pass, the
	// output of the pass before for a later one. An output written as an
	// incremental update keeps the document's numbers, and one rewritten
	// whole numbers its objects anew. It is 0 for a font dictionary written
	// inline in a resource dictionary.
	Object int
	// Err says why, wrapping
	// [github.com/gotenberg/litho/pkg/pdf.ErrFontNotSupplied],
	// [github.com/gotenberg/litho/pkg/pdf.ErrFontProgramInvalid],
	// [github.com/gotenberg/litho/pkg/pdf.ErrFontEmbeddingRestricted],
	// [github.com/gotenberg/litho/pkg/pdf.ErrFontKind],
	// [github.com/gotenberg/litho/pkg/pdf.ErrFontCoverage],
	// [github.com/gotenberg/litho/pkg/pdf.ErrFontWidths],
	// [github.com/gotenberg/litho/pkg/pdf.ErrFontRejected],
	// [github.com/gotenberg/litho/pkg/pdf.ErrFontUnwritable] or
	// [github.com/gotenberg/litho/pkg/pdf.ErrFontRolledBack].
	Err error
}

// String describes the refusal.
func (r FontRefusal) String() string {
	return fmt.Sprintf("font %s (object %d): %v", r.BaseFont, r.Object, r.Err)
}

// FontRequest describes a font a document shows without embedding its
// program. Values are safe to read concurrently.
type FontRequest struct {
	// BaseFont is the font's /BaseFont as the file writes it, a subset tag
	// included; a composite font's is its descendant CIDFont's.
	BaseFont string
	// Subtype is the font's /Subtype: Type1, MMType1 or TrueType for a
	// simple font, the descendant's CIDFontType0 or CIDFontType2 for a
	// composite one.
	Subtype string
	// Flags is the font descriptor's /Flags (ISO 32000-1, 9.8.2), 0 when the
	// font has no descriptor. Bit 3 (value 4) marks a symbolic font, bit 7
	// (64) an italic one.
	Flags int
	// Encoding names the font's encoding: the predefined simple encoding its
	// /Encoding or /BaseEncoding names, or the CMap a composite font's
	// /Encoding names. It is empty when the font names none.
	Encoding string
	// Ordering is a composite font's character collection, Registry and
	// Ordering joined by a hyphen (Adobe-Japan1), and empty for a simple
	// font.
	Ordering string
	// Codes lists what the document shows with the font, in code order.
	Codes []FontCode
}

// FontCode is one code a document shows with a font. Values are safe to read
// concurrently.
type FontCode struct {
	// Code is a simple font's character code, or the CID a composite font's
	// code selects.
	Code uint32
	// Glyph is the glyph name a simple font's encoding gives the code, empty
	// when it gives none or the font is composite.
	Glyph string
	// Unicode is the character the glyph stands for, 0 when unknown.
	Unicode rune
	// Width is the width the font declares for the code, in thousandths of
	// an em. The supplied program's advance must agree with it within one
	// unit, or the font is refused.
	Width float64
}
