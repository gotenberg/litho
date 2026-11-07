package svg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/stroke"
	"github.com/gotenberg/litho/pkg/internal/typography/cff"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// The composition model strokes a path in one flat colour and clips with
// paths alone. What it cannot say directly, a gradient stroke, a text
// stroke unlike the text's fill and text in a clip path, becomes a filled
// outline here: the stroke's outline, or the glyphs' outlines.

// strokeTolerance is the flattening tolerance of a stroke outline as a
// fraction of the stroked shape's bounding box diagonal: 1/4000 of a
// drawing 300 pixels across is under a tenth of a pixel, below what
// anti-aliasing shows.
const strokeTolerance = 1.0 / 4000

// maxOutlineFaces caps the font programs one rendering parses for glyph
// outlines. Anti-DoS guard, not spec-mandated: a drawing's text uses one
// or two faces, and each parse reads a whole program.
const maxOutlineFaces = 16

// maxOutlineGlyphs caps the glyph outlines one rendering converts to
// paths. Anti-DoS guard, not spec-mandated: outlined text is a stroked
// label or a clipping word, a few hundred glyphs at most.
const maxOutlineGlyphs = 20_000

// strokeOutline returns the outline of ops stroked with s (SVG 1.1, 11.4),
// to be filled with the nonzero rule, in ops' coordinates, nil when the
// stroke paints nothing or the stroker refuses it.
func strokeOutline(ops []compose.PathOp, s *compose.Stroke) []compose.PathOp {
	b := opsBBox(ops)
	diag := math.Hypot(b.x1-b.x0, b.y1-b.y0) + s.Width
	if !b.ok || !(diag > 0) || !finite(diag) {
		return nil
	}
	miter := s.Miter
	if miter == 0 {
		miter = stroke.DefaultMiterLimit
	}
	st := stroke.Style{
		Width: s.Width, Cap: stroke.Cap(s.Cap), Join: stroke.Join(s.Join), MiterLimit: miter,
		Dash: s.Dash, DashPhase: s.DashPhase,
	}
	out, err := stroke.Outline(toGeom(ops), st, geom.Identity, diag*strokeTolerance)
	if err != nil {
		return nil
	}
	return fromGeom(out, geom.Identity)
}

// toGeom converts composition path operators to a render path.
func toGeom(ops []compose.PathOp) geom.Path {
	var p geom.Path
	pt := func(q compose.Point) geom.Point { return geom.Point{X: q.X, Y: q.Y} }
	for _, op := range ops {
		switch op.Kind {
		case compose.MoveTo:
			p.MoveTo(pt(op.P[0]))
		case compose.LineTo:
			p.LineTo(pt(op.P[0]))
		case compose.CurveTo:
			p.CubicTo(pt(op.P[0]), pt(op.P[1]), pt(op.P[2]))
		case compose.ClosePath:
			p.Close()
		}
	}
	return p
}

// fromGeom converts a render path to composition path operators, each
// point mapped through m. A path whose steps want more points than it
// holds ends where they run out.
func fromGeom(p geom.Path, m geom.Matrix) []compose.PathOp {
	out := make([]compose.PathOp, 0, len(p.Ops))
	i := 0
	pt := func() compose.Point {
		q := m.Apply(p.Pts[i])
		i++
		return compose.Point{X: q.X, Y: q.Y}
	}
	for _, op := range p.Ops {
		switch op {
		case geom.MoveTo, geom.LineTo:
			if i >= len(p.Pts) {
				return out
			}
			k := compose.MoveTo
			if op == geom.LineTo {
				k = compose.LineTo
			}
			out = append(out, compose.PathOp{Kind: k, P: [3]compose.Point{pt()}})
		case geom.CubicTo:
			if i+3 > len(p.Pts) {
				return out
			}
			a := pt()
			b := pt()
			out = append(out, compose.PathOp{Kind: compose.CurveTo, P: [3]compose.Point{a, b, pt()}})
		case geom.Close:
			out = append(out, compose.PathOp{Kind: compose.ClosePath})
		}
	}
	return out
}

// outlineFont is a font program parsed for its glyph outlines.
type outlineFont struct {
	tt  *truetype.Font
	cff *cff.Font
}

// errNoOutlines reports a font program whose outlines cannot be read.
var errNoOutlines = errors.New("svg: no glyph outlines")

// parseOutlineFont reads the outlines of a single sfnt program, TrueType
// or OpenType with CFF or CFF2 outlines (OpenType specification, glyf,
// CFF and CFF2 tables).
func parseOutlineFont(data []byte) (*outlineFont, error) {
	if !truetype.IsSfnt(data) {
		return nil, errNoOutlines
	}
	if bytes.HasPrefix(data, []byte("OTTO")) {
		if t, err := truetype.CFFTable(data); err == nil {
			f, err := cff.ParseFont(t)
			if err != nil {
				return nil, fmt.Errorf("%w: %w", errNoOutlines, err)
			}
			return &outlineFont{cff: f}, nil
		}
		t, err := truetype.CFF2Table(data)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", errNoOutlines, err)
		}
		f, err := cff.ParseCFF2(t)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", errNoOutlines, err)
		}
		return &outlineFont{cff: f}, nil
	}
	f, err := truetype.ParseFont(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errNoOutlines, err)
	}
	return &outlineFont{tt: f}, nil
}

// glyph returns the outline of glyph gid in glyph space and the matrix
// from glyph space to text space, where the em is one unit.
func (f *outlineFont) glyph(ctx context.Context, gid int) (geom.Path, geom.Matrix, error) {
	if f.cff != nil {
		p, err := f.cff.GlyphOutline(ctx, gid)
		return p, f.cff.FontMatrix(gid), err
	}
	p, err := f.tt.GlyphOutline(ctx, gid)
	return p, f.tt.FontMatrix(), err
}

// outlineFace returns the parsed outlines of face, parsing it on first
// use, nil when they cannot be read or the face cap is reached.
func (r *renderer) outlineFace(face *compose.Face) *outlineFont {
	if face == nil {
		return nil
	}
	if f, ok := r.outlines[face.Key]; ok {
		return f
	}
	if len(r.outlines) >= maxOutlineFaces {
		r.d.warn("svg-limit", fmt.Sprintf("glyph outlines of more than %d faces are not drawn", maxOutlineFaces))
		return nil
	}
	f, err := parseOutlineFont(face.Program)
	if err != nil {
		r.d.warn("svg-text", fmt.Sprintf("glyph outlines are not drawn: %v", err))
		f = nil
	}
	if r.outlines == nil {
		r.outlines = map[string]*outlineFont{}
	}
	r.outlines[face.Key] = f
	return f
}

// runOutline returns the outlines of the glyphs of t in t's coordinates,
// y downward, to be filled with the nonzero rule: what the glyphs paint,
// slanted by the run's skew. The synthetic emboldening stroke is left
// out.
func (r *renderer) runOutline(t *compose.Text) []compose.PathOp {
	f := r.outlineFace(t.Face)
	if f == nil {
		return nil
	}
	var out []compose.PathOp
	for _, g := range t.Glyphs {
		if r.glyphsOutlined >= maxOutlineGlyphs {
			r.d.warn("svg-limit", fmt.Sprintf("the outlines of more than %d glyphs are not drawn", maxOutlineGlyphs))
			return out
		}
		r.glyphsOutlined++
		p, fm, err := f.glyph(r.ctx, int(g.GID))
		if err != nil || p.Empty() {
			continue
		}
		// Text space is y upward at one unit per em; the run's space is y
		// downward in points (ISO 32000-1, 9.4.2).
		place := geom.Matrix{t.Size, 0, t.Skew * t.Size, -t.Size, t.Origin.X + g.X, t.Origin.Y + g.Y}
		out = append(out, fromGeom(p, fm.Mul(place))...)
	}
	return out
}
