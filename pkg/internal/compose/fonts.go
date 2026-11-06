package compose

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/typography/embed"
)

// fontEntry is one embedded font: the program of a [Face.Key], the subset
// every page draws into, and the Type0 font object they share.
type fontEntry struct {
	font   *embed.Font
	subset *embed.Subset
	ref    ast.PdfIndirectRef
	// unitsPerEm is the program's, 1000 when it leaves it unset, as embed
	// writes it.
	unitsPerEm float64
}

// advance returns the advance of gid in text space units at font size 1,
// the width the font's /W array gives a reader (ISO 32000-1, 9.7.4.3):
// embed writes each width in whole thousandths of an em, so the pen
// follows that rounding rather than the program's exact width, and a glyph
// placed at its natural advance needs no adjustment. It reports false for
// a glyph the program does not hold.
func (fe *fontEntry) advance(gid uint16) (float64, bool) {
	w, ok := fe.font.Width(gid)
	if !ok {
		return 0, false
	}
	return float64(int(float64(w)*1000/fe.unitsPerEm)) / 1000, true
}

// font returns the entry of face, loading its program on first use. Two
// faces with one Key share the entry of the first: the model makes the key
// the identity of the program. The Type0 font is written once per entry,
// after every page, so its subset holds every glyph the document draws.
func (w *docWriter) font(face *Face) (*fontEntry, error) {
	if fe, ok := w.fonts[face.Key]; ok {
		return fe, nil
	}
	f, err := embed.Load(face.Program)
	if err != nil {
		return nil, fmt.Errorf("%w: face %q: %w", ErrInvalidFont, face.Key, err)
	}
	upem := float64(f.UnitsPerEm())
	if upem == 0 {
		upem = 1000
	}
	fe := &fontEntry{
		font:       f,
		subset:     f.NewSubset(),
		ref:        w.alloc.Alloc(ast.PdfNull{}),
		unitsPerEm: upem,
	}
	w.fonts[face.Key] = fe
	w.fontOrder = append(w.fontOrder, fe)
	return fe, nil
}
