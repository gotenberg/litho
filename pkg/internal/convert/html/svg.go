package html

import (
	"context"
	"errors"
	"fmt"
	"strings"

	dom "golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/css"
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
		Fonts: r.svg.fonts,
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
	doc, err := svg.FromNode(ctx, n, b.svgOptions(st))
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
	return []*layout.Node{node}
}

// svgOptions returns the options an svg element of the document styled
// st draws with: the document's style elements apply to its content, and
// it inherits the element's colour and font.
func (b *builder) svgOptions(st *css.Style) svg.Options {
	var sheets []string
	for _, s := range b.scan.sheets {
		if s.Namespace == "" && s.Data == "style" {
			sheets = append(sheets, textContent(s))
		}
	}
	return svg.Options{
		Fonts: b.res.svg.fonts,
		Image: func(ctx context.Context, href string) (*svg.Raster, error) {
			return b.res.svgRaster(ctx, resolveRef(b.scan.resourceBase, href))
		},
		Context: svg.Context{
			Color:    typeset.Color{R: st.Color.R, G: st.Color.G, B: st.Color.B, A: st.Color.A},
			Families: st.FontFamilies,
			FontSize: st.FontSize / css.Pt,
			Weight:   st.FontWeight,
			Italic:   st.FontStyle != css.FontNormal,
			Lang:     b.scan.lang,
		},
		Sheets: sheets,
	}
}

// definition returns the drawing of the svg element that holds the
// clipPath or mask element of kind url names, and that element's id:
// url names an element of the document by its fragment (CSS Masking 1,
// 5.1 and 6.2). A url into another document names none.
func (b *builder) definition(ctx context.Context, url, kind string) (*svg.Document, string, bool) {
	i := strings.LastIndexByte(url, '#')
	if i < 0 || url[:i] != "" && url[:i] != b.scan.resourceBase && url[:i] != b.scan.docURL {
		return nil, "", false
	}
	id := url[i+1:]
	def := b.scan.defs[id]
	if def == nil || def.Data != kind {
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
	doc, err := svg.FromNode(ctx, owner, b.svgOptions(st))
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
					op.P[j] = typeset.Point{X: box.X + op.P[j].X*css.Pt, Y: box.Y + op.P[j].Y*css.Pt}
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
