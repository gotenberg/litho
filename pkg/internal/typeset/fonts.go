package typeset

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/typography/embed"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
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
	// box is the program's glyph bounding box in ems, [xMin yMin xMax
	// yMax], once boxRead is set; boxOK reports whether the program gives
	// one that holds an area.
	box            [4]float64
	boxRead, boxOK bool
}

// glyphBox returns the box that holds every glyph of the program, in ems
// from the glyph origin, y up: the head table's xMin, yMin, xMax and yMax
// (OpenType, head), which embed writes as the FontBBox (ISO 32000-1,
// 9.8.1, Table 122). It reports false for a program whose head table is
// missing or holds an empty box.
func (fe *fontEntry) glyphBox() ([4]float64, bool) {
	if fe.boxRead {
		return fe.box, fe.boxOK
	}
	fe.boxRead = true
	data := fe.font.Program()
	records, err := truetype.ParseTableDirectory(data)
	if err != nil {
		return fe.box, false
	}
	head, err := truetype.TableData(data, records, "head")
	if err != nil || len(head) < 44 {
		return fe.box, false
	}
	for i := range fe.box {
		// The four FWORDs are signed 16-bit (OpenType, head).
		v := int16(binary.BigEndian.Uint16(head[36+2*i:])) //nolint:gosec // reinterprets the 16 bits of a signed FWORD
		fe.box[i] = float64(v) / fe.unitsPerEm
	}
	fe.boxOK = fe.box[2] > fe.box[0] && fe.box[3] > fe.box[1]
	return fe.box, fe.boxOK
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
func (w *docWriter) font(ctx context.Context, face *Face) (*fontEntry, error) {
	if fe, ok := w.fonts[face.Key]; ok {
		return fe, nil
	}
	f, err := embed.Load(ctx, face.Program)
	if err != nil {
		if ctx.Err() != nil {
			return nil, err // the context's error, not the program's
		}
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
