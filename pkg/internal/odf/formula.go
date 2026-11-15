package odf

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/formula"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// MimeFormula is the media type of an OpenDocument formula (ODF 1.3,
// Part 3, 2.2.9 and Appendix C).
const MimeFormula = "application/vnd.oasis.opendocument.formula"

// NSMathML is the namespace of MathML, the content of a formula (ODF
// 1.3, Part 3, 14.6).
const NSMathML = "http://www.w3.org/1998/Math/MathML"

// Formula is the formula an embedded object holds (ODF 1.3, Part 3,
// 10.4.6.2 and 14.6): its presentation MathML as a tree of formula
// elements, and what the object's settings say of its fonts.
type Formula struct {
	// Root is the math element, nil when the object holds no
	// presentation markup, only an annotation such as its StarMath text.
	Root *formula.Element
	// Size is the base font size in points: the settings' BaseFontHeight,
	// 12 when they state none.
	Size float64
	// Families draw the formula's identifiers, numbers and text: the
	// settings' FontNameVariables, Liberation Serif when they name none.
	Families []string
	// Operators draw the formula's operators: the settings'
	// FontNameMath, OpenSymbol when they name none.
	Operators []string
	// ScriptScale is the size of a script relative to its base: the
	// settings' RelativeFontHeightIndices, 60 per cent when they state
	// none.
	ScriptScale float64
	// Warnings lists what the reading left out of the formula.
	Warnings []typeset.Warning
	// view is the visible area LibreOffice Math's layout gave the
	// formula, as its settings record it; zero when they do not.
	view formulaView
}

// formulaView is the box LibreOffice Math's own layout gives a formula,
// in points: its width and height, and its baseline below its top, 0
// when unknown.
type formulaView struct {
	width, height, baseline float64
}

// maxFormulaView bounds the extent a formula's settings record, in
// points. Anti-DoS guard, not spec-mandated: a scale of the formula, and
// no page is wider than 200 inches.
const maxFormulaView = 14400

// readView returns the visible area of a formula's settings: the
// ViewAreaWidth and ViewAreaHeight items, in twentieths of a point, and
// the BaseLine item, in hundredths of a millimetre, as LibreOffice Math
// writes them; zero when they are missing or out of bounds.
//
// # Corpus evidence
//
//   - probe, odt: e to the i pi plus one equals nought at 12 pt saves
//     ViewAreaWidth 886, ViewAreaHeight 301 and BaseLine 439 in its
//     settings; LibreOffice prints the same formula 44.02 pt wide and
//     14.97 pt tall in sd/formula.fodp of libreoffice/odp, its baseline
//     12.42 pt below its top.
func readView(settings *Element) formulaView {
	num := func(name string) float64 {
		v, ok := formulaConfig(settings, name)
		if !ok {
			return 0
		}
		n, err := intconv.Atoi32(strings.TrimSpace(v))
		if err != nil {
			return 0
		}
		return float64(n)
	}
	v := formulaView{width: num("ViewAreaWidth") / 20, height: num("ViewAreaHeight") / 20, baseline: float64(num("BaseLine")*72) / 2540}
	if !(v.width > 0 && v.width <= maxFormulaView && v.height > 0 && v.height <= maxFormulaView) {
		return formulaView{}
	}
	if !(v.baseline > 0 && v.baseline <= v.height) {
		v.baseline = 0
	}
	return v
}

// The fonts and size of a formula whose object states none, as
// LibreOffice Math writes them in a new object's settings.
//
// # Corpus evidence
//
//   - libreoffice/odp: sd/formula.fodp's formula, whose object has no
//     settings, prints at 11.991 pt in Liberation Serif.
//   - libreoffice/odp: sd/Math.fodp, with no settings, prints its
//     superscripts at 60 per cent of their bases, its letters in an
//     italic face and its operators in OpenSymbol.
//   - probe, odt: LibreOffice saves BaseFontHeight 12, FontNameVariables
//     "Liberation Serif", FontNameMath "OpenSymbol" and
//     RelativeFontHeightIndices 60 in the settings of every formula
//     object it creates, and prints scripts of scripts at 36 per cent.
const (
	defaultFormulaSize      = 12
	defaultFormulaFamily    = "Liberation Serif"
	defaultFormulaOperators = "OpenSymbol"
	defaultScriptPercent    = 60
)

// maxFormulaSize bounds the base font size a formula's settings set, in
// points. Anti-DoS guard, not spec-mandated: LibreOffice Math offers
// sizes up to 127 points, and a size is a scale of every glyph drawn.
const maxFormulaSize = 1000

// ReadFormula returns the formula the draw:object obj holds, nil when it
// holds something else: the inline math element or office:document of a
// flat document, or the object directory of the package its xlink:href
// names, whose manifest media type is a formula's or whose content is a
// math element. It fails as [Package.ParseFile] does on the object's
// content.
func ReadFormula(ctx context.Context, pkg *Package, obj *Element) (*Formula, error) {
	if obj == nil {
		return nil, nil
	}
	var math, settings *Element
	if m := obj.Child(NSMathML, "math"); m != nil {
		math = m
	} else if doc := obj.Child(NSOffice, "document"); doc != nil {
		if mt := doc.A(NSOffice, "mimetype"); mt != "" && mt != MimeFormula {
			return nil, nil
		}
		math = formulaMath(doc)
		settings = doc.Child(NSOffice, "settings")
	} else {
		dir := strings.TrimSuffix(cleanName(obj.A(NSXLink, "href")), "/")
		if dir == "" || dir == "." || strings.Contains(obj.A(NSXLink, "href"), "://") || pkg == nil || pkg.Flat {
			return nil, nil
		}
		formulaType := pkg.MediaType(dir+"/") == MimeFormula || pkg.MediaType(dir) == MimeFormula
		if !formulaType && pkg.MediaType(dir+"/") != "" {
			// An object of another type: a chart, a drawing, a document.
			return nil, nil
		}
		if !pkg.Has(path.Join(dir, "content.xml")) {
			return nil, nil
		}
		content, err := pkg.ParseFile(ctx, path.Join(dir, "content.xml"))
		if err != nil {
			return nil, fmt.Errorf("reading the formula %s: %w", dir, err)
		}
		math = formulaMath(content)
		if math == nil {
			if formulaType {
				return nil, fmt.Errorf("%w: the formula %s holds no math element", office.ErrMalformed, dir)
			}
			return nil, nil
		}
		if s, err := pkg.ParseFile(ctx, path.Join(dir, "settings.xml")); err == nil {
			// A formula reads without its settings, missing or not, at
			// LibreOffice Math's defaults.
			settings = s
		}
	}
	if math == nil {
		return nil, nil
	}
	f := &Formula{Size: defaultFormulaSize, Families: []string{defaultFormulaFamily}}
	if v, ok := formulaConfig(settings, "BaseFontHeight"); ok {
		if n, err := intconv.Atoi32(v); err == nil && n > 0 {
			f.Size = float64(min(n, maxFormulaSize))
		}
	}
	if v, ok := formulaConfig(settings, "FontNameVariables"); ok && strings.TrimSpace(v) != "" {
		f.Families = []string{strings.TrimSpace(v), defaultFormulaFamily}
	}
	f.Operators = []string{defaultFormulaOperators}
	if v, ok := formulaConfig(settings, "FontNameMath"); ok && strings.TrimSpace(v) != "" {
		f.Operators = []string{strings.TrimSpace(v), defaultFormulaOperators}
	}
	f.ScriptScale = float64(defaultScriptPercent) / 100
	if v, ok := formulaConfig(settings, "RelativeFontHeightIndices"); ok {
		if n, err := intconv.Atoi32(strings.TrimSpace(v)); err == nil && n > 0 && n <= 100 {
			f.ScriptScale = float64(n) / 100
		}
	}
	f.view = readView(settings)
	r := &mathReader{}
	f.Root = r.presentation(math)
	f.Warnings = r.warnings
	return f, nil
}

// formulaMath returns the math element of a formula document: its root,
// or the math element of its office:body's office:formula (ODF 1.3, Part
// 3, 3.8), nil when it has none.
func formulaMath(doc *Element) *Element {
	if doc.Is(NSMathML, "math") {
		return doc
	}
	body := Section(doc, "body")
	if m := body.Child(NSMathML, "math"); m != nil {
		return m
	}
	return body.Child(NSOffice, "formula").Child(NSMathML, "math")
}

// configNS is the namespace of the configuration items of a settings
// document (ODF 1.3, Part 3, 3.10).
const configNS = "urn:oasis:names:tc:opendocument:xmlns:config:1.0"

// maxConfigItems bounds the elements of a settings document searched
// for one item. Anti-DoS guard, not spec-mandated: a formula's settings
// hold about seventy items.
const maxConfigItems = 4096

// formulaConfig returns the configuration item name of a formula's
// settings, an office:document-settings root or an office:settings
// element, and whether it holds one.
func formulaConfig(settings *Element, name string) (string, bool) {
	if settings == nil {
		return "", false
	}
	stack := []*Element{settings}
	for steps := 0; len(stack) > 0 && steps < maxConfigItems; steps++ {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur.Is(configNS, "config-item") && cur.A(configNS, "name") == name {
			return cur.TextContent(), true
		}
		for _, c := range cur.Children {
			if !c.IsText() {
				stack = append(stack, c)
			}
		}
	}
	return "", false
}

// mathReader converts one formula's MathML into formula elements.
type mathReader struct {
	warnings []typeset.Warning
	warned   map[string]bool
	// count is the elements made, against [formula.MaxElements].
	count int
}

// warn records a warning once per message.
func (r *mathReader) warn(code warn.Code, effect warn.Effect, msg string) {
	if r.warned[msg] {
		return
	}
	if r.warned == nil {
		r.warned = map[string]bool{}
	}
	r.warned[msg] = true
	r.warnings = append(r.warnings, typeset.Warning{Code: code, Effect: effect, Message: msg})
}

// presentation returns the formula element of the math element m, nil
// when it holds no presentation markup: a math element whose content is
// a semantics element holding only annotations.
func (r *mathReader) presentation(m *Element) *formula.Element {
	root := r.element(m, 0)
	if root == nil || !hasPresentation(root) {
		return nil
	}
	return root
}

// hasPresentation reports a formula element that draws something or
// holds an element that does.
func hasPresentation(e *formula.Element) bool {
	switch e.Name {
	case "mi", "mn", "mo", "mtext", "ms", "mspace":
		return true
	}
	for _, c := range e.Children {
		if hasPresentation(c) {
			return true
		}
	}
	return false
}

// element returns the formula element of the MathML element el at
// nesting depth, nil for one that draws nothing: an annotation, an
// alignment mark. It maps the MathML 3 elements MathML Core lacks to
// what Core has where the mapping is exact, and says what is lost of
// the others.
func (r *mathReader) element(el *Element, depth int) *formula.Element {
	if depth > formula.MaxDepth {
		r.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("formula elements nested past %d levels are not drawn", formula.MaxDepth))
		return nil
	}
	r.count++
	if r.count > formula.MaxElements {
		r.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("formula elements past %d are not drawn", formula.MaxElements))
		return nil
	}
	name := strings.ToLower(el.Name.Local)
	switch name {
	case "annotation", "annotation-xml":
		return nil
	case "maligngroup", "malignmark":
		// Alignment marks draw nothing themselves (MathML 3, 3.5.5); the
		// alignment they ask of a table's columns is not made.
		r.warn(warn.ContentUnsupported, warn.Approximated, "the alignment marks of a formula are not aligned")
		return nil
	case "mglyph":
		// MathML 3, 3.2.1.2: a glyph drawn from an image or a font index.
		r.warn(warn.ContentUnsupported, warn.Omitted, "the mglyph images of a formula are not drawn")
		return nil
	case "mfenced":
		return r.fenced(el, depth)
	}
	e := &formula.Element{Name: name, Attrs: mathAttrs(el)}
	switch name {
	case "mi", "mn", "mo", "mtext", "ms":
		if el.Child(NSMathML, "mglyph") != nil {
			r.warn(warn.ContentUnsupported, warn.Omitted, "the mglyph images of a formula are not drawn")
		}
		e.Text = el.TextContent()
		if name == "mo" {
			e.Attrs = withOperatorSpace(e.Attrs)
		}
		return e
	case "mstyle":
		e.Attrs = styleAttrs(e.Attrs)
	case "menclose":
		// Drawn by libreOfficeEnclosure.
	case "mlabeledtr":
		// MathML 3, 3.5.3: the first cell is the row's label. The row is
		// laid out without it.
		r.warn(warn.ContentUnsupported, warn.Omitted, "the labels of a formula's numbered rows are not drawn")
		e.Name = "mtr"
		first := true
		for _, c := range el.Children {
			if c.Is(NSMathML, "mtd") && first {
				first = false
				continue
			}
			r.appendChild(e, c, depth)
		}
		return e
	case "semantics":
		// MathML 3, 5.1: the first child is the presentation, the rest
		// annotate it.
		for _, c := range el.Children {
			if c.Name.Space == NSMathML && !c.IsText() {
				r.appendChild(e, c, depth)
				break
			}
		}
		return e
	case "none":
		// An empty script of mmultiscripts (MathML 3, 3.4.7), an empty
		// mrow in MathML Core, 3.4.3.
		e.Name, e.Attrs = "mrow", nil
		return e
	}
	for _, c := range el.Children {
		r.appendChild(e, c, depth)
	}
	return e
}

// appendChild appends the formula element of c, a child of e's source,
// to e's children when it is a MathML element that draws.
func (r *mathReader) appendChild(e *formula.Element, c *Element, depth int) {
	if c.IsText() || c.Name.Space != NSMathML {
		return
	}
	if k := r.element(c, depth+1); k != nil {
		e.Children = append(e.Children, k)
	}
}

// fenced expands the mfenced element el into the mrow MathML 3, 3.3.8.1
// gives as its equivalent: the open fence, the arguments separated by
// the separators, the last separator repeating when they run out, and
// the close fence, each fence and separator an mo; the arguments are
// wrapped in an mrow of their own when there are more than one.
func (r *mathReader) fenced(el *Element, depth int) *formula.Element {
	open, close, seps := "(", ")", ","
	if v, ok := el.Lookup("", "open"); ok {
		open = strings.TrimSpace(v)
	}
	if v, ok := el.Lookup("", "close"); ok {
		close = strings.TrimSpace(v)
	}
	if v, ok := el.Lookup("", "separators"); ok {
		seps = v
	}
	sep := []rune(strings.Join(strings.Fields(seps), ""))
	row := &formula.Element{Name: "mrow"}
	for _, a := range mathAttrs(el) {
		switch a.Name {
		case "open", "close", "separators":
		default:
			row.Attrs = append(row.Attrs, a)
		}
	}
	if open != "" {
		row.Children = append(row.Children, &formula.Element{Name: "mo", Text: open, Attrs: withOperatorSpace([]formula.Attr{{Name: "fence", Value: "true"}, {Name: "form", Value: "prefix"}})})
	}
	var args []*formula.Element
	for _, c := range el.Children {
		if c.IsText() || c.Name.Space != NSMathML {
			continue
		}
		if k := r.element(c, depth+1); k != nil {
			args = append(args, k)
		}
	}
	switch {
	case len(args) == 1:
		row.Children = append(row.Children, args[0])
	case len(args) > 1:
		inner := &formula.Element{Name: "mrow"}
		for i, a := range args {
			if i > 0 && len(sep) > 0 {
				s := sep[min(i-1, len(sep)-1)]
				inner.Children = append(inner.Children, &formula.Element{Name: "mo", Text: string(s), Attrs: withOperatorSpace([]formula.Attr{{Name: "separator", Value: "true"}})})
			}
			inner.Children = append(inner.Children, a)
		}
		row.Children = append(row.Children, inner)
	}
	if close != "" {
		row.Children = append(row.Children, &formula.Element{Name: "mo", Text: close, Attrs: withOperatorSpace([]formula.Attr{{Name: "fence", Value: "true"}, {Name: "form", Value: "postfix"}})})
	}
	return row
}

// mathAttrs returns the attributes of a MathML element, those in no
// namespace or in MathML's, their names in lowercase.
func mathAttrs(el *Element) []formula.Attr {
	var out []formula.Attr
	for _, a := range el.Attr {
		if a.Name.Space == "" || a.Name.Space == NSMathML {
			out = append(out, formula.Attr{Name: strings.ToLower(a.Name.Local), Value: a.Value})
		}
	}
	return out
}

// styleAttrs maps the deprecated style attributes of an mstyle element
// to the attributes MathML Core reads: color to mathcolor, background to
// mathbackground, fontsize to mathsize, each unless the element also
// sets the newer attribute (MathML 3, 3.2.2.1 and 3.3.4.2).
func styleAttrs(attrs []formula.Attr) []formula.Attr {
	has := func(name string) bool {
		for _, a := range attrs {
			if a.Name == name {
				return true
			}
		}
		return false
	}
	out := make([]formula.Attr, 0, len(attrs))
	for _, a := range attrs {
		var to string
		switch a.Name {
		case "color":
			to = "mathcolor"
		case "background":
			to = "mathbackground"
		case "fontsize":
			to = "mathsize"
		}
		if to != "" {
			if !has(to) {
				out = append(out, formula.Attr{Name: to, Value: a.Value})
			}
			continue
		}
		out = append(out, a)
	}
	return out
}

// FittedFormula is a formula laid out and fitted to its frame.
type FittedFormula struct {
	box *formula.Box
	// width, ascent and descent are the formula's natural extent, the
	// box LibreOffice Math gives it, about its baseline, in points.
	width, ascent, descent float64
	// view is the visible area the object's settings record.
	view formulaView
	// AltText is the formula's alternate description: the tokens in
	// reading order (ISO 32000-1, 14.9.3).
	AltText string
	// Warnings lists what the layout could not draw.
	Warnings []typeset.Warning
}

// libreOfficeEnclosure is how LibreOffice Math draws an menclose: its
// overstrike, which it writes as the notation horizontalstrike, a line
// 0.034 em thick whose middle stands 0.298 em above the baseline whatever
// the content, across the content, which it does not pad. LibreOffice
// writes no other notation and draws none it reads from MathML: it draws
// a formula from the StarMath annotation it writes beside the MathML.
//
// # Corpus evidence
//
//   - probe, odt: overstrike {abc} and overstrike {x^2 + y} saved by
//     LibreOffice of gotenberg/gotenberg:9.4.1 as menclose
//     notation="horizontalstrike" over the content; its PDF at 48 pt draws
//     each strike 1.63 pt thick, its middle 14.3 and 14.4 pt above the
//     baseline, from the content's ink start to its ink end; at 12 pt
//     0.38 pt thick and 3.5 pt up. The formula's view is as wide and as
//     tall with the strike as without it.
//   - probe, odt: a formula of MathML alone, menclose notation box,
//     circle, the strikes, left, top, actuarial, madruwb, longdiv,
//     roundedbox or phasorangle over abc, draws abc and no line in
//     LibreOffice, which saves it without the menclose.
var libreOfficeEnclosure = formula.Enclosure{Rule: 0.034, StrikeRaise: 0.298, Notations: []string{"horizontalstrike"}}

// Lay lays f out with s at its base size, the extent of its box taken
// as LibreOffice Math takes it: at least the ascent and descent of its
// first font at that size. It returns nil when f draws nothing, and
// fails as [formula.Setter.Layout] does.
//
// # Corpus evidence
//
//   - libreoffice/odp: sd/formula.fodp's formula, e to the i pi plus one
//     equals nought, fills a frame 14.97 pt tall whose baseline lies
//     12.42 pt below its top: the superscript's ascent in Liberation
//     Serif at 7.2 pt raised 6.01 pt, and the descent of Liberation
//     Serif at 12 pt below.
func (f *Formula) Lay(ctx context.Context, s *formula.Setter, fonts *text.Fonts) (*FittedFormula, error) {
	if f == nil || f.Root == nil {
		return nil, nil
	}
	// LibreOffice Math draws a letter in the italic face of its font, an
	// operator in its operator font, and scales scripts by its own
	// percentage, which the formula style's options say.
	st := formula.Style{
		Families: f.Families, Size: f.Size, Weight: 400, Fallback: text.FallbackLibreOffice, Color: typeset.Color{A: 1},
		FontVariants: true, OperatorFamilies: f.Operators, ScriptScale: f.ScriptScale,
		Enclose: &libreOfficeEnclosure,
	}
	if v, ok := f.Root.Attr("display"); ok && strings.EqualFold(strings.TrimSpace(v), "block") {
		// math[display=block] lays its content out in math-style normal
		// (MathML Core, 2.1.1 and Appendix A). LibreOffice writes it on
		// every formula.
		st.Display = true
	}
	box, err := s.Layout(ctx, f.Root, st)
	if err != nil {
		return nil, err
	}
	ff := &FittedFormula{box: box, view: f.view, width: box.Width, ascent: box.Ascent, descent: box.Descent, AltText: formula.AltText(f.Root), Warnings: append(f.Warnings[:len(f.Warnings):len(f.Warnings)], box.Warnings...)}
	if fonts != nil {
		if fc := fonts.Match(text.Style{Families: f.Families, Size: f.Size, Weight: 400}); fc != nil {
			m := fc.Metrics(f.Size)
			ff.ascent, ff.descent = max(ff.ascent, m.Ascent), max(ff.descent, m.Descent)
		}
	}
	if ff.width <= 0 || ff.ascent+ff.descent <= 0 {
		return nil, nil
	}
	return ff, nil
}

// Size returns the formula's natural width and height, in points.
func (ff *FittedFormula) Size() (w, h float64) {
	return ff.width, ff.ascent + ff.descent
}

// Draw returns the fragments of the formula fitted to the frame r,
// clipped to it. LibreOffice scales the box its own layout gives a
// formula, its visible area, to the frame on each axis. When the
// object's settings record that area, the formula is drawn at that
// scale, made smaller on both axes alike as far as it is wider or taller
// than the area, at the frame's left and with its baseline on the
// baseline the settings record, kept inside the frame; else its own box
// is scaled to the frame on each axis.
//
// # Corpus evidence
//
//   - libreoffice/odp: sd/Math.fodp's formula, laid out at 12 pt and
//     with no settings, fills its 14 by 2 cm frame: its glyphs print at
//     45.496 pt, 3.79 times, and are stretched 2.011 times wider still,
//     all clipped to the frame.
//   - probe, odt: a formula in a frame wider and taller than the visible
//     area its settings record is stretched to the frame on each axis;
//     in a frame of that area's size its glyphs print at the base size,
//     its baseline the settings' BaseLine below the frame's top.
func (ff *FittedFormula) Draw(r typeset.Rect) []typeset.Fragment {
	if ff == nil || r.Width <= 0 || r.Height <= 0 {
		return nil
	}
	h := ff.ascent + ff.descent
	sx, sy := r.Width/ff.width, r.Height/h
	x, top := r.X, r.Y
	if v := ff.view; v.width > 0 && v.height > 0 {
		k := min(1, v.width/ff.width, v.height/h)
		sx, sy = float64(r.Width/v.width)*k, float64(r.Height/v.height)*k
		baseline := float64(r.Height*ff.ascent) / h
		if v.baseline > 0 && v.baseline <= v.height {
			baseline = float64(r.Height*v.baseline) / v.height
		}
		top = r.Y + min(max(baseline-float64(ff.ascent*sy), 0), r.Height-float64(h*sy))
	}
	return []typeset.Fragment{&typeset.Group{
		Clip: &typeset.Path{Ops: shape.RectOps(r)},
		Content: []typeset.Fragment{&typeset.Group{
			Transform: typeset.Matrix{A: sx, D: sy, E: x, F: top},
			Content:   ff.box.Draw(0, ff.ascent),
		}},
	}}
}

// operatorSpace is the space LibreOffice Math leaves on each side of an
// operator, in the em of the operator's font, where MathML Core's
// operator dictionary gives a fifth to a third of an em: the operators
// of its operator font carry their own side bearings.
//
// # Corpus evidence
//
//   - libreoffice/odp: sd/formula.fodp sets plus, one, equals, nought
//     with gaps of 0.71, 0.29 and 0.48 pt between the advances of
//     OpenSymbol's operators and Liberation Serif's digits at 12 pt.
const operatorSpace = "0.04em"

// withOperatorSpace returns the attributes of an mo element with lspace and
// rspace set to [operatorSpace] where the source sets neither.
func withOperatorSpace(attrs []formula.Attr) []formula.Attr {
	hasL, hasR := false, false
	for _, a := range attrs {
		hasL = hasL || a.Name == "lspace"
		hasR = hasR || a.Name == "rspace"
	}
	if !hasL {
		attrs = append(attrs, formula.Attr{Name: "lspace", Value: operatorSpace})
	}
	if !hasR {
		attrs = append(attrs, formula.Attr{Name: "rspace", Value: operatorSpace})
	}
	return attrs
}
