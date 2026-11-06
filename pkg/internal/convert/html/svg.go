package html

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	dom "golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/svg"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// errNestedSVG refuses an SVG image an SVG image's image element names:
// litho draws raster pictures there, which also keeps a drawing from
// including itself.
var errNestedSVG = errors.New("an SVG image inside an SVG image is not drawn")

// svgState is what the SVG pictures of one conversion share: the fonts
// their text is shaped with and the warnings they produce. It is safe for
// concurrent use.
type svgState struct {
	fonts *text.Fonts

	mu    sync.Mutex
	warns []compose.Warning
}

// addWarnings records the warnings of a drawing, the one at u.
func (s *svgState) addWarnings(u string, ws []compose.Warning) {
	if len(ws) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range ws {
		s.warns = append(s.warns, compose.Warning{Code: w.Code, Message: fmt.Sprintf("in the SVG image %s: %s", displayURL(u), w.Message)})
	}
}

// warnings returns the recorded warnings.
func (s *svgState) warnings() []compose.Warning {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]compose.Warning(nil), s.warns...)
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
		return nil, fmt.Errorf("parsing the SVG image: %w", err)
	}
	r.svg.addWarnings(u, doc.Warnings())
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
		pic.Vector = func(context.Context, compose.Rect) []compose.Fragment { return nil }
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
	var sheets []string
	for _, s := range b.scan.sheets {
		if s.Namespace == "" && s.Data == "style" {
			sheets = append(sheets, textContent(s))
		}
	}
	doc, err := svg.FromNode(ctx, n, svg.Options{
		Fonts: b.res.svg.fonts,
		Image: func(ctx context.Context, href string) (*svg.Raster, error) {
			return b.res.svgRaster(ctx, resolveRef(b.scan.resourceBase, href))
		},
		Context: svg.Context{
			Color:    compose.Color{R: st.Color.R, G: st.Color.G, B: st.Color.B, A: st.Color.A},
			Families: st.FontFamilies,
			FontSize: st.FontSize / css.Pt,
			Weight:   st.FontWeight,
			Italic:   st.FontStyle != css.FontNormal,
			Lang:     b.scan.lang,
		},
		Sheets: sheets,
	})
	if err != nil {
		if ctx.Err() == nil {
			b.warnings = append(b.warnings, compose.Warning{Code: CodeResourceFailed, Message: fmt.Sprintf("an inline SVG was left out: %v", err)})
		}
		return nil
	}
	for _, w := range doc.Warnings() {
		b.warnOnce(w.Code, w.Message, "in an inline SVG: "+w.Message)
	}
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
