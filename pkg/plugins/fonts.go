package plugins

import (
	"context"

	"github.com/gotenberg/litho/pkg/models"
)

// FontProvider supplies the programs of the fonts a document shows without
// embedding them, which every PDF/A, PDF/UA and WTPDF profile requires
// embedded (ISO 19005-2, 6.2.11.4.1 and ISO 14289-1, 7.21.4.1 among them).
// litho ships no font program, so a remediation embeds only what a provider
// supplies, and the licence to embed it is the caller's.
// [github.com/gotenberg/litho/pkg/pdf.WithFontProvider] registers one.
//
// FontProgram returns a TrueType or OpenType program, or a bare CFF one, for
// the font req describes, or nil to supply none. A returned error aborts the
// remediation. A provider shared by concurrent remediations must be safe for
// concurrent use.
type FontProvider interface {
	FontProgram(ctx context.Context, req models.FontRequest) ([]byte, error)
}

// FontProviderFunc adapts a function to a [FontProvider].
type FontProviderFunc func(ctx context.Context, req models.FontRequest) ([]byte, error)

// FontProgram calls f.
func (f FontProviderFunc) FontProgram(ctx context.Context, req models.FontRequest) ([]byte, error) {
	return f(ctx, req)
}
