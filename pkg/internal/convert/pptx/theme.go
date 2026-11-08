package pptx

import (
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// theme is a master's theme (ECMA-376 Part 1, 20.1.6): its colour and font
// schemes, read by ooxml, and its format scheme's style matrix (20.1.4.1.14
// fmtScheme), which p:style references index.
type theme struct {
	*ooxml.Theme
	// fills, lines and bgFills are the children of fillStyleLst,
	// lnStyleLst and bgFillStyleLst, in order.
	fills, lines, bgFills []*ooxml.Element
	// effects are the children of effectStyleLst, in order.
	effects []*ooxml.Element
	// part is the theme part, whose relationships its picture fills name.
	part string
}

// officeColors is the colour scheme of the Office theme, which a master
// without a theme takes, as LibreOffice does.
var officeColors = map[string]css.Color{
	"dk1": hex(0x000000), "lt1": hex(0xffffff), "dk2": hex(0x44546a), "lt2": hex(0xe7e6e6),
	"accent1": hex(0x4472c4), "accent2": hex(0xed7d31), "accent3": hex(0xa5a5a5),
	"accent4": hex(0xffc000), "accent5": hex(0x5b9bd5), "accent6": hex(0x70ad47),
	"hlink": hex(0x0563c1), "folHlink": hex(0x954f72),
}

// hex returns the opaque colour 0xRRGGBB.
func hex(v uint32) css.Color {
	return css.Color{R: float64(v>>16&0xff) / 255, G: float64(v>>8&0xff) / 255, B: float64(v&0xff) / 255, A: 1}
}

// themeOf returns the theme of the master part, read once, the Office
// theme when the master relates none.
func (r *reader) themeOf(master string) *theme {
	name := r.related(master, ooxml.RelTheme)
	if t, ok := r.themes[name]; ok {
		return t
	}
	t := &theme{Theme: &ooxml.Theme{Colors: officeColors, Major: ooxml.FontScheme{Latin: "Calibri Light"}, Minor: ooxml.FontScheme{Latin: "Calibri"}}}
	if name != "" {
		root, err := r.pkg.ParseXML(r.ctx, name, understood)
		if err == nil {
			th := ooxml.ParseTheme(root)
			if len(th.Colors) > 0 {
				t.Theme = th
			}
			fs := root.Child(ooxml.NSA, "themeElements").Child(ooxml.NSA, "fmtScheme")
			t.fills = fs.Child(ooxml.NSA, "fillStyleLst").Kids()
			t.lines = fs.Child(ooxml.NSA, "lnStyleLst").Kids()
			t.bgFills = fs.Child(ooxml.NSA, "bgFillStyleLst").Kids()
			t.effects = fs.Child(ooxml.NSA, "effectStyleLst").Kids()
		} else {
			r.warn("malformed-part", fmt.Sprintf("theme %s does not parse: the Office theme stands in: %v", name, err))
		}
	}
	t.part = name
	r.themes[name] = t
	return t
}

// font resolves a DrawingML typeface (21.1.2.3.7 latin): a theme font
// reference "+mj-lt", "+mn-ea", "+mn-cs"... to the theme's typeface, ""
// when the theme leaves it empty, any other name as itself.
func (t *theme) font(name string) string {
	ref, ok := strings.CutPrefix(name, "+")
	if !ok {
		return name
	}
	fs := t.Minor
	if strings.HasPrefix(ref, "mj") {
		fs = t.Major
	}
	switch {
	case strings.HasSuffix(ref, "-ea"):
		return fs.EastAsian
	case strings.HasSuffix(ref, "-cs"):
		return fs.ComplexScript
	}
	return fs.Latin
}

// colorMap is a colour mapping (ECMA-376 Part 1, 19.3.1.6 clrMap): the
// theme colour each of bg1, tx1, bg2, tx2, accent1 to accent6, hlink and
// folHlink stands for.
type colorMap map[string]string

// defaultColorMap is the mapping a master without p:clrMap takes, the
// light background one PowerPoint writes.
var defaultColorMap = colorMap{"bg1": "lt1", "tx1": "dk1", "bg2": "lt2", "tx2": "dk2"}

// readColorMap reads a p:clrMap or an a:overrideClrMapping over base.
func readColorMap(e *ooxml.Element, base colorMap) colorMap {
	if e == nil {
		return base
	}
	m := colorMap{}
	for k, v := range base {
		m[k] = v
	}
	for _, a := range e.Attrs {
		if a.Name.Space == "" {
			m[a.Name.Local] = a.Value
		}
	}
	return m
}

// scope is what resolves the colours and fonts of one layer: its theme
// and its colour mapping.
type scope struct {
	theme *theme
	cmap  colorMap
}

// resolver returns the scheme resolver of the scope, with phClr standing
// for ph when ph is not nil (20.1.4.1 the style matrix's placeholder
// colour).
func (sc scope) resolver(ph *css.Color) ooxml.SchemeResolver {
	return func(name string) (css.Color, bool) {
		if name == "phClr" {
			if ph != nil {
				return *ph, true
			}
			return css.Color{}, false
		}
		if mapped, ok := sc.cmap[name]; ok {
			name = mapped
		}
		switch name {
		case "bg1":
			name = "lt1"
		case "tx1":
			name = "dk1"
		case "bg2":
			name = "lt2"
		case "tx2":
			name = "dk2"
		}
		c, ok := sc.theme.Colors[name]
		return c, ok
	}
}

// color returns the colour a colour container holds, phClr as ph.
func (sc scope) color(e *ooxml.Element, ph *css.Color) (css.Color, bool) {
	return ooxml.DrawingColor(e, sc.resolver(ph))
}
