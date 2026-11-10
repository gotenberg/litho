package mutations

import (
	"context"
	"errors"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/internal/typography/cff"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// inkGlyph is one glyph of a line of text, in text space at a font size of 1:
// how far it moves the pen, and where its ink lies from the pen.
type inkGlyph struct {
	advance float64
	// box is llx, lly, urx and ury; inked is false for a glyph that draws
	// nothing, such as a space.
	box   [4]float64
	inked bool
}

// lineInkBox returns the box bounding the ink of glyphs set one after the
// other from the origin, at fontSize, as Tj sets them with no character or
// word spacing (ISO 32000-1, 9.4.4). A line that inks nothing, all spaces,
// takes the box its advances span on the baseline, so a placed stamp still
// has an extent to place.
func lineInkBox(glyphs []inkGlyph, fontSize float64) [4]float64 {
	var (
		pen   float64
		box   [4]float64
		inked bool
	)
	for _, g := range glyphs {
		if g.inked {
			b := [4]float64{pen + g.box[0], g.box[1], pen + g.box[2], g.box[3]}
			if !inked {
				box, inked = b, true
			} else {
				box = [4]float64{min(box[0], b[0]), min(box[1], b[1]), max(box[2], b[2]), max(box[3], b[3])}
			}
		}
		pen += g.advance
	}
	if !inked {
		box = [4]float64{min(0, pen), 0, max(0, pen), 0}
	}
	for i := range box {
		box[i] *= fontSize
	}
	return box
}

// standardInkGlyphs returns the glyphs the codes draw in the standard font
// baseFont, as [StampText] writes it, bounded by the font's Adobe Font
// Metrics: a font a PDF does not embed has no outlines of its own, and a
// viewer draws it with a program whose glyphs match those metrics (ISO
// 32000-1, 9.6.2.2).
func standardInkGlyphs(baseFont string, codes []byte) []inkGlyph {
	glyphs := make([]inkGlyph, len(codes))
	for i, code := range codes {
		name := typography.Standard14CodeGlyph(baseFont, code)
		w, _ := typography.Standard14Width(baseFont, name)
		g := inkGlyph{advance: w / 1000}
		if b, ok := typography.Standard14GlyphBounds(baseFont, name); ok && b != [4]float64{} {
			g.box = [4]float64{b[0] / 1000, b[1] / 1000, b[2] / 1000, b[3] / 1000}
			g.inked = true
		}
		glyphs[i] = g
	}
	return glyphs
}

// glyphOutliner returns the outline of a glyph in text space at a font size
// of 1.
type glyphOutliner func(ctx context.Context, gid int) (geom.Path, error)

// embeddedInkGlyphs returns the glyphs text draws in the TrueType or OpenType
// program f, the one the stamp embeds, bounded by their outlines. cffData is
// the program's CFF table, nil for glyf outlines. A character the program
// lacks draws glyph 0, as [encodeTextAsGIDs] encodes it. A program whose
// outlines do not read bounds each glyph by its advance and the font's ascent
// and descent instead, which still places the stamp, a little less tightly.
func embeddedInkGlyphs(ctx context.Context, doc *document.Document, f *embeddedTTF, cffData []byte, text string) ([]inkGlyph, error) {
	upem := float64(f.unitsPerEm)
	if upem == 0 {
		upem = 1000
	}
	// A program whose outlines do not read leaves outline nil, and each glyph
	// then says so in the log as it falls back (see [outlineOf]).
	outline, err := newGlyphOutliner(f.raw, cffData)
	if err != nil {
		outline = nil
	}

	cache := make(map[uint16]inkGlyph)
	var glyphs []inkGlyph
	for i, r := range text {
		if i%100 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		gid := f.unicodeToGID[r]
		g, seen := cache[gid]
		if !seen {
			g = inkGlyph{advance: float64(f.widths[gid]) / upem}
			path, err := outlineOf(ctx, outline, gid)
			switch {
			case ctx.Err() != nil:
				return nil, ctx.Err()
			case err != nil:
				if doc != nil {
					doc.Logger.Debug("stamp: bounding a glyph by the font's ascent and descent, its outline does not read",
						slog.Int("gid", int(gid)), slog.String("err", err.Error()),
					)
				}
				g.box = [4]float64{0, float64(f.descent) / upem, g.advance, float64(f.ascent) / upem}
				g.inked = true
			default:
				if b, ok := path.Bounds(); ok {
					g.box = [4]float64{b.Min.X, b.Min.Y, b.Max.X, b.Max.Y}
					g.inked = true
				}
			}
			cache[gid] = g
		}
		glyphs = append(glyphs, g)
	}
	return glyphs, nil
}

// errNoOutlines stands for a program whose outlines do not read.
var errNoOutlines = errors.New("the font's outlines do not read")

// outlineOf returns the outline of glyph gid, or [errNoOutlines] when outline
// is nil.
func outlineOf(ctx context.Context, outline glyphOutliner, gid uint16) (geom.Path, error) {
	if outline == nil {
		return geom.Path{}, errNoOutlines
	}
	return outline(ctx, int(gid))
}

// newGlyphOutliner reads the outlines of a program: its CFF table when
// cffData holds one, its glyf table otherwise.
func newGlyphOutliner(raw, cffData []byte) (glyphOutliner, error) {
	if len(cffData) > 0 {
		f, err := cff.ParseFont(cffData)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, gid int) (geom.Path, error) {
			path, err := f.GlyphOutline(ctx, gid)
			if err != nil {
				return geom.Path{}, err
			}
			return path.Transform(f.FontMatrix(gid)), nil
		}, nil
	}
	f, err := truetype.ParseFont(raw)
	if err != nil {
		return nil, err
	}
	m := f.FontMatrix()
	return func(ctx context.Context, gid int) (geom.Path, error) {
		path, err := f.GlyphOutline(ctx, gid)
		if err != nil {
			return geom.Path{}, err
		}
		return path.Transform(m), nil
	}, nil
}
