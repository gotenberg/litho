package picture

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/svg"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// cssPixel is a CSS pixel in points (CSS Values 4, 6.2: 96 to the inch).
const cssPixel = 0.75

// The default size of a drawing that states neither a size nor a view
// box: CSS Images 3, 4.3 gives a replaced element without a natural size
// 300 by 150 CSS pixels.
const (
	defaultSVGWidth  = 300 * cssPixel
	defaultSVGHeight = 150 * cssPixel
)

// maxSVGDepth bounds the group nesting of a drawing placed on a page, as
// the layout bounds a vector picture's: the typeset model nests groups 24
// deep (limits.MaxGroupDepth), and the page's own group takes one.
// Anti-DoS guard of litho's stack, not spec-mandated.
const maxSVGDepth = 20

// maxSVGScanTokens bounds the XML tokens read to find the root's title and
// description. Anti-DoS guard, not spec-mandated: they are the root's
// first children in practice (SVG 2, 5.9), and the scan stops there.
const maxSVGScanTokens = 4096

// svg draws the SVG file data on a page of its own, as vector content
// tagged as a Figure: drawn by package svg as an HTML page draws an SVG
// picture, its text shaped in fonts and falling back as Chromium does, its
// image elements left out with a warning. The page is its width and
// height, else its view box, in CSS pixels; the root's title is the
// document's title and the figure's description, its desc the figure's
// description when it has no title (SVG 2, 5.9: title is the element's
// accessible name, desc its description).
func (c *converter) svg(ctx context.Context, data []byte, fonts *text.Fonts) error {
	doc, err := svg.Parse(ctx, data, svg.Options{Fonts: fonts, Fallback: text.FallbackChromium, Logger: c.opts.Logger})
	switch {
	case errors.Is(err, svg.ErrTooLarge):
		return fmt.Errorf("%w: %w", limits.ErrInputTooLarge, err)
	case errors.Is(err, svg.ErrNotSVG):
		return fmt.Errorf("%w: SVG: %w", office.ErrMalformed, err)
	case err != nil:
		return err
	}
	root := scanSVGRoot(data)
	w, h := svgSize(doc.Size(), root)
	c.pages++
	pw, ph, r := c.pageFor(w, h)
	fig := &typeset.StructElem{Role: "Figure", Alt: root.title}
	if fig.Alt == "" {
		fig.Alt = root.desc
	}
	c.doc.Metadata.Title = root.title
	var content []typeset.Fragment
	if frags := doc.Draw(ctx, r); len(frags) > 0 {
		if depthOf(frags) > maxSVGDepth {
			c.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("the drawing nests groups deeper than %d levels and is left out", maxSVGDepth))
		} else {
			content = []typeset.Fragment{&typeset.Group{Opacity: 1, Transform: typeset.Identity, Content: adopt(frags, fig)}}
		}
	}
	for _, w := range doc.Warnings() {
		w.Page = c.pages
		c.doc.Warnings = append(c.doc.Warnings, w)
	}
	return c.addPage(ctx, &typeset.Page{Width: pw, Height: ph, Content: content}, fig)
}

// svgSize returns a drawing's page size in points: its width and height,
// one of them through its ratio when the other is missing (CSS Images 3,
// 4.3), else its view box, in CSS pixels, else 300 by 150 CSS pixels.
func svgSize(s svg.Size, root svgRoot) (float64, float64) {
	w, h := defaultSVGWidth, defaultSVGHeight
	ratio := s.Ratio
	if root.viewW > 0 && root.viewH > 0 {
		w, h = root.viewW*cssPixel, root.viewH*cssPixel
		ratio = root.viewW / root.viewH
	}
	switch {
	case s.HasWidth && s.HasHeight:
		w, h = s.Width, s.Height
	case s.HasWidth:
		w = s.Width
		if ratio > 0 {
			h = w / ratio
		}
	case s.HasHeight:
		h = s.Height
		if ratio > 0 {
			w = h * ratio
		}
	}
	if !(w > 0) || !(h > 0) {
		w, h = defaultSVGWidth, defaultSVGHeight
	}
	return w, h
}

// svgRoot is what the root element of an SVG file states that the page
// needs: its view box's size and the text of its first title and desc
// children.
type svgRoot struct {
	viewW, viewH float64
	title, desc  string
}

// scanSVGRoot reads the root element of an SVG file and its first title
// and desc children. A file that does not read as XML gives what was read
// before the fault.
func scanSVGRoot(data []byte) svgRoot {
	var root svgRoot
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = false
	depth := 0
	var collect *string
	var text strings.Builder
	for range maxSVGScanTokens {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth == 1 {
				for _, a := range t.Attr {
					if a.Name.Local == "viewBox" {
						root.viewW, root.viewH = viewBoxSize(a.Value)
					}
				}
			}
			if depth == 2 && collect == nil {
				switch {
				case t.Name.Local == "title" && root.title == "":
					collect = &root.title
				case t.Name.Local == "desc" && root.desc == "":
					collect = &root.desc
				}
				text.Reset()
			}
		case xml.CharData:
			if collect != nil && text.Len() < maxTextBytes {
				text.Write(t)
			}
		case xml.EndElement:
			if depth == 2 && collect != nil {
				*collect = strings.Join(strings.Fields(text.String()), " ")
				collect = nil
			}
			depth--
			if depth == 0 {
				return root
			}
		}
	}
	return root
}

// maxTextBytes caps the text of a title or a description read. Anti-DoS
// guard, not spec-mandated: a description is read by people, a few
// hundred characters.
const maxTextBytes = 4096

// viewBoxSize returns the width and height of a viewBox attribute (SVG 2,
// 8.6: min-x, min-y, width, height, separated by white space or commas),
// 0, 0 when it does not parse or a side is not positive.
func viewBoxSize(v string) (float64, float64) {
	f := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' })
	if len(f) != 4 {
		return 0, 0
	}
	w, err1 := strconv.ParseFloat(f[2], 64)
	h, err2 := strconv.ParseFloat(f[3], 64)
	if err1 != nil || err2 != nil || !(w > 0) || !(h > 0) || w > 1e9 || h > 1e9 {
		return 0, 0
	}
	return w, h
}

// adopt returns a copy of frags whose leaves hang from parent.
func adopt(frags []typeset.Fragment, parent *typeset.StructElem) []typeset.Fragment {
	out := make([]typeset.Fragment, 0, len(frags))
	for _, f := range frags {
		switch v := f.(type) {
		case *typeset.Group:
			g := *v
			g.Content = adopt(v.Content, parent)
			out = append(out, &g)
		case *typeset.Text:
			c := *v
			c.Parent = parent
			out = append(out, &c)
		case *typeset.Image:
			c := *v
			c.Parent = parent
			out = append(out, &c)
		case *typeset.Path:
			c := *v
			c.Parent = parent
			out = append(out, &c)
		default:
			out = append(out, f)
		}
	}
	return out
}

// depthOf returns the group nesting of frags.
func depthOf(frags []typeset.Fragment) int {
	d := 0
	for _, f := range frags {
		if g, ok := f.(*typeset.Group); ok {
			d = max(d, 1+depthOf(g.Content))
		}
	}
	return d
}
