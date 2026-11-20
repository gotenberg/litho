package models

import "fmt"

// FontRefusal records a font a remediation left unembedded although a
// [github.com/gotenberg/litho/pkg/pdf.FontProvider] was configured. Values are
// safe to read concurrently.
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
