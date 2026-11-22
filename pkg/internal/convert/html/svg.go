package html

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	dom "golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/svg"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// errNestedSVG refuses an SVG image an SVG image's image element names:
// litho draws raster pictures there, which also keeps a drawing from
// including itself.
var errNestedSVG = errors.New("an SVG image inside an SVG image is not drawn")

// svgState is what the SVG pictures of one conversion share: the fonts
// their text is shaped with and the drawings whose warnings the conversion
// reports. It is safe for concurrent use.
type svgState struct {
	fonts *text.Fonts
	// images are the SVG images, read once every page is laid out; inline
	// are the svg elements of the document, read as the tree is built and
	// again once every page is laid out.
	images, inline svg.Collector
}

// svgCtxKey marks a context that is decoding an SVG picture.
type svgCtxKey struct{}

// decodeSVG decodes the SVG picture data, read from u, into a vector
// picture: drawn with the svg package at the size the layout gives it,
// its image elements resolved against u.
func (r *resources) decodeSVG(ctx context.Context, u string, data []byte) (*layout.Picture, error) {
	if ctx.Value(svgCtxKey{}) != nil {
		return nil, errNestedSVG
	}
	ctx = context.WithValue(ctx, svgCtxKey{}, true)
	doc, err := svg.Parse(ctx, data, svg.Options{
		Fonts:    r.svg.fonts,
		Fallback: text.FallbackChromium,
		Image: func(ctx context.Context, href string) (*svg.Raster, error) {
			return r.svgRaster(ctx, resolveRef(u, href))
		},
	})
	if err != nil {
		return nil, pictureDataError{fmt.Errorf("parsing the SVG image: %w", err)}
	}
	r.svg.images.Add(doc, fmt.Sprintf("in the SVG image %s: ", displayURL(u)))
	return vectorPicture(doc), nil
}

// svgRaster loads the raster picture an SVG image element names.
func (r *resources) svgRaster(ctx context.Context, u string) (*svg.Raster, error) {
	if u == "" {
		return nil, errBadURL
	}
	pic, err := r.picture(ctx, u)
	if err != nil {
		return nil, err
	}
	if pic.Vector != nil || pic.Source == nil {
		return nil, errNestedSVG
	}
	return &svg.Raster{Source: pic.Source, Width: pic.Width / css.Pt, Height: pic.Height / css.Pt}, nil
}

// errBadURL reports a reference that does not parse.
var errBadURL = errors.New("the URL does not parse")

// vectorPicture returns the layout picture of a drawing: its natural size,
// the missing dimensions filled as the default sizing algorithm does when
// nothing else sizes it (CSS Images 3, 4.3: through the ratio, else 300
// by 150 pixels), and its drawing.
func vectorPicture(doc *svg.Document) *layout.Picture {
	s := doc.Size()
	w, h := 300*css.Pt, 150*css.Pt
	switch {
	case s.HasWidth && s.HasHeight:
		w, h = s.Width, s.Height
	case s.HasWidth:
		w = s.Width
		if s.Ratio > 0 {
			h = w / s.Ratio
		}
	case s.HasHeight:
		h = s.Height
		if s.Ratio > 0 {
			w = h * s.Ratio
		}
	case s.Ratio > 0:
		h = w / s.Ratio
	}
	pic := &layout.Picture{
		Width: max(w, minPictureSide), Height: max(h, minPictureSide),
		Vector:         doc.Draw,
		NoNaturalWidth: !s.HasWidth, NoNaturalHeight: !s.HasHeight,
		NoRatio: s.Ratio <= 0,
	}
	if s.HasWidth && s.HasHeight && (s.Width <= 0 || s.Height <= 0) {
		// A drawing of zero size keeps its box and paints nothing.
		pic.Vector = func(context.Context, typeset.Rect) []typeset.Fragment { return nil }
	}
	return pic
}

// minPictureSide is the smallest side a picture takes, in points: the
// layout refuses a picture without a positive size, and an SVG of zero
// width or height still takes its box, which this keeps negligible.
const minPictureSide = 1e-3

// inlineSVG builds the replaced node of an svg element of the document,
// inline SVG (HTML Standard, 4.8.16), styled st: a vector picture drawn
// from the element's own tree, inheriting the element's colour and font,
// with the document's style elements applying to its content. Its width
// and height attributes size it as presentational hints do (SVG 2, 8.2).
func (b *builder) inlineSVG(ctx context.Context, n *dom.Node, st *css.Style) []*layout.Node {
	if st == nil || st.Display == css.DisplayNone {
		return nil
	}
	doc, err := svg.FromNode(ctx, n, b.svgOptions(n, st))
	if err != nil {
		if ctx.Err() == nil {
			b.warnings = append(b.warnings, typeset.Warning{Code: warningCode(pictureDataError{err}), Effect: warn.Omitted, Message: fmt.Sprintf("an inline SVG was left out: %v", err)})
		}
		return nil
	}
	b.res.svg.inline.Add(doc, "in an inline SVG: ")
	b.warnings = append(b.warnings, b.res.svg.inline.Take()...)
	sized := *st
	for i, key := range [2]string{"width", "height"} {
		v, ok := attr(n, key)
		v = strings.TrimSpace(v)
		if !ok || !strings.HasSuffix(v, "%") {
			continue
		}
		var pct float64
		if _, err := fmt.Sscanf(v, "%g%%", &pct); err != nil || pct < 0 {
			continue
		}
		d := css.Dimension{Kind: css.Percent, Value: pct / 100}
		if i == 0 && sized.Width.Kind == css.Auto {
			sized.Width = d
		}
		if i == 1 && sized.Height.Kind == css.Auto {
			sized.Height = d
		}
	}
	node := &layout.Node{Kind: layout.Replaced, Style: &sized, Picture: vectorPicture(doc)}
	node.ID, _ = attr(n, "id")
	node.Role = "Figure"
	node.Alt = svgTitle(n)
	if node.Alt == "" && b.derivedAlt {
		node.Alt = svgDerivedAlt(n)
	}
	return []*layout.Node{node}
}

// svgOptions returns the options the svg element n of the document,
// styled st, draws with: the document's style elements apply to its
// content, it inherits the element's colour, font and language, and its
// text falls back as Chromium's does.
func (b *builder) svgOptions(n *dom.Node, st *css.Style) svg.Options {
	var sheets []string
	for _, s := range b.scan.sheets {
		if s.Namespace == "" && s.Data == "style" {
			sheets = append(sheets, textContent(s))
		}
	}
	return svg.Options{
		Fonts:    b.res.svg.fonts,
		Fallback: text.FallbackChromium,
		Image: func(ctx context.Context, href string) (*svg.Raster, error) {
			return b.res.svgRaster(ctx, resolveRef(b.scan.resourceBase, href))
		},
		Context: svg.Context{
			Color:    typeset.Color{R: st.Color.R, G: st.Color.G, B: st.Color.B, A: st.Color.A},
			Families: st.FontFamilies,
			FontSize: st.FontSize / css.Pt,
			Weight:   st.FontWeight,
			Italic:   st.FontStyle != css.FontNormal,
			Lang:     b.langOf(n),
		},
		Sheets: sheets,
	}
}

// svgShapes are the SVG shape elements an offset-path names by url():
// the basic shapes and path (SVG 2, 9 and 10). Immutable lookup table.
var svgShapes = map[string]bool{
	"rect": true, "circle": true, "ellipse": true, "line": true, "polyline": true,
	"polygon": true, "path": true,
}

// shapeKind is the kind [builder.definition] takes for any element of
// [svgShapes].
const shapeKind = "shape"

// localURL reports a url that names an element of the document itself by
// its fragment, and returns the fragment.
func (b *builder) localURL(url string) (string, bool) {
	i := strings.LastIndexByte(url, '#')
	if i < 0 || url[:i] != "" && url[:i] != b.scan.resourceBase && url[:i] != b.scan.docURL {
		return "", false
	}
	return url[i+1:], true
}

// definition returns the drawing of the svg element that holds the
// clipPath, mask, filter or shape element of kind url names, and that
// element's id: url names an element of the document by its fragment (CSS
// Masking 1, 5.1 and 6.2). A url into another document names none.
// [shapeKind] takes any element of [svgShapes].
func (b *builder) definition(ctx context.Context, url, kind string) (*svg.Document, string, bool) {
	id, ok := b.localURL(url)
	if !ok {
		return nil, "", false
	}
	def := b.scan.defs[id]
	if def == nil || def.Data != kind && (kind != shapeKind || !svgShapes[def.Data]) {
		return nil, "", false
	}
	var owner *dom.Node
	for a := def.Parent; a != nil && a.Type == dom.ElementNode && a.Namespace == "svg"; a = a.Parent {
		if a.Data == "svg" {
			owner = a
		}
	}
	if owner == nil {
		return nil, "", false
	}
	b.defsMu.Lock()
	defer b.defsMu.Unlock()
	if doc, ok := b.defDocs[owner]; ok {
		return doc, id, doc != nil
	}
	st := b.computed.Style(owner)
	if st == nil {
		st = css.Initial()
	}
	doc, err := svg.FromNode(ctx, owner, b.svgOptions(owner, st))
	if err != nil {
		// The drawing that holds the definition does not parse, which its
		// own placement reports: the url names nothing, as one that names
		// no element does.
		doc = nil
	}
	if b.defDocs == nil {
		b.defDocs = map[*dom.Node]*svg.Document{}
	}
	b.defDocs[owner] = doc
	return doc, id, doc != nil
}

// offsetPath returns st with the url() of its offset-path, when it names
// an SVG shape element of the document, replaced by that shape's path, its
// geometry in its own user space, its transform attribute and those of
// its ancestors left out (CSS Motion Path 1, 2.1), laid as a path() is.
// A url that names anything else is path("m 0 0") (2.1), which the layout
// draws for a url it is left; one into another document is reported, as
// litho reads no other document for it.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 moves a box along
//     url() of a path, a circle under a transform attribute, a rect in a
//     translated group, a line, a polyline, an ellipse, a path in defs and
//     a path of pathLength 10 to the points the shapes' own geometry
//     gives, from the box's own top left, transforms and pathLength left
//     out, and keeps a box whose url names no element at its place.
func (b *builder) offsetPath(ctx context.Context, st *css.Style) *css.Style {
	m := st.Motion()
	if m == nil || m.Path.Kind != css.OffsetURL {
		return st
	}
	if _, ok := b.localURL(m.Path.URL); !ok {
		b.warnOnce(warn.ContentUnsupported, warn.Approximated, "offset-path url", "an offset-path url() naming another document's shape is not read: the box moves as on path(\"m 0 0\")")
		return st
	}
	doc, id, ok := b.definition(ctx, m.Path.URL, shapeKind)
	if !ok {
		return st
	}
	ops, found := doc.ShapePath(ctx, id)
	if !found {
		return st
	}
	c, mp := *st, *m
	mp.Path = css.OffsetPath{Kind: css.OffsetShape, Shape: &css.Shape{Kind: css.ShapePath, Path: pathData(ops)}, Box: m.Path.Box}
	c.MotionPath = &mp
	return &c
}

// pathData writes ops as SVG path data in absolute commands, a path()
// reads it back (SVG 2, 9.3): "M 0 0" for no operator.
func pathData(ops []typeset.PathOp) string {
	if len(ops) == 0 {
		return "M 0 0"
	}
	var sb strings.Builder
	num := func(v float64) {
		sb.WriteByte(' ')
		sb.WriteString(strconv.FormatFloat(v, 'g', -1, 64))
	}
	for _, op := range ops {
		switch op.Kind {
		case typeset.MoveTo:
			sb.WriteString(" M")
			num(op.P[0].X)
			num(op.P[0].Y)
		case typeset.LineTo:
			sb.WriteString(" L")
			num(op.P[0].X)
			num(op.P[0].Y)
		case typeset.CurveTo:
			sb.WriteString(" C")
			for _, p := range op.P {
				num(p.X)
				num(p.Y)
			}
		case typeset.ClosePath:
			sb.WriteString(" Z")
		}
	}
	return strings.TrimSpace(sb.String())
}

// clipSource returns the clipping regions of the clipPath element url
// names for an element whose border box is box, in points in the layout's
// coordinates: the clip path's user space has its origin at the box's top
// left and a unit of one CSS pixel (CSS Masking 1, 5.1).
func (b *builder) clipSource(ctx context.Context, url string, box typeset.Rect) ([][]*typeset.Path, bool) {
	doc, id, ok := b.definition(ctx, url, "clipPath")
	if !ok {
		return nil, false
	}
	regions, found := doc.ClipRegions(ctx, id, box.Width/css.Pt, box.Height/css.Pt)
	for _, reg := range regions {
		for i, p := range reg {
			ops := make([]typeset.PathOp, len(p.Ops))
			for k, op := range p.Ops {
				for j := range op.P {
					op.P[j] = typeset.Point{X: box.X + float64(op.P[j].X*css.Pt), Y: box.Y + float64(op.P[j].Y*css.Pt)}
				}
				ops[k] = op
			}
			reg[i] = &typeset.Path{Ops: ops, Fill: p.Fill}
		}
	}
	return regions, found
}

// maskSource returns the content of the mask element url names for an
// element whose border box is box, in points in the layout's coordinates:
// the mask's user space has its origin at the box's top left and a unit
// of one CSS pixel (CSS Masking 1, 7.1).
func (b *builder) maskSource(ctx context.Context, url string, box typeset.Rect) ([]typeset.Fragment, bool, bool) {
	doc, id, ok := b.definition(ctx, url, "mask")
	if !ok {
		return nil, false, false
	}
	content, alpha, found := doc.MaskContent(ctx, id, box.Width/css.Pt, box.Height/css.Pt)
	if len(content) == 0 {
		return nil, alpha, found
	}
	return []typeset.Fragment{&typeset.Group{
		Transform: typeset.Matrix{A: css.Pt, D: css.Pt, E: box.X, F: box.Y},
		Opacity:   1,
		Content:   content,
	}}, alpha, found
}

// filterSource returns the filter element url names for an element whose
// border box is box, in points in the layout's coordinates: the filter's
// user space has its origin at the box's top left and a unit of one CSS
// pixel, the box its bounding box (Filter Effects 1, 7).
func (b *builder) filterSource(ctx context.Context, url string, box typeset.Rect) (*layout.FilterRef, bool) {
	doc, id, ok := b.definition(ctx, url, "filter")
	if !ok {
		return nil, false
	}
	f, found := doc.Filter(ctx, id, box.Width/css.Pt, box.Height/css.Pt)
	if !found {
		return nil, false
	}
	return &layout.FilterRef{
		Region:  typeset.Rect{X: box.X + float64(f.X*css.Pt), Y: box.Y + float64(f.Y*css.Pt), Width: f.W * css.Pt, Height: f.H * css.Pt},
		Sigma:   f.Sigma * css.Pt,
		Skipped: f.Skipped,
		Apply: func(ctx context.Context, s *effect.Surface) (*effect.Surface, error) {
			k := s.Grid.Scale * css.Pt
			return f.Apply(ctx, s, k, (s.Grid.X-box.X)/css.Pt, (s.Grid.Y-box.Y)/css.Pt)
		},
	}, true
}

// svgDerivedAlt returns what an svg element without a title states of
// itself apart from it: its aria-label, which names it before its title
// does (SVG-AAM 1.0, the accessible name computation), else the text of its first desc child, its
// description (SVG 2, 5.9); "" for neither.
func svgDerivedAlt(n *dom.Node) string {
	if v, ok := attr(n, "aria-label"); ok {
		if v = collapse(v); v != "" {
			return bounded(v)
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == dom.ElementNode && c.Namespace == "svg" && c.Data == "desc" {
			return bounded(collapse(textContent(c)))
		}
	}
	return ""
}

// svgTitle returns the text of the first title child of an svg element,
// its accessible name (SVG 2, 5.9).
func svgTitle(n *dom.Node) string {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == dom.ElementNode && c.Namespace == "svg" && c.Data == "title" {
			return collapse(textContent(c))
		}
	}
	return ""
}
