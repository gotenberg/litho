package odp

import (
	"context"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/odf"
)

// builtinXML holds the styles Impress gives a document that defines
// none: the graphic default style (its item pool's defaults) and the
// "standard" graphic style a new shape takes, as LibreOffice writes them
// into a presentation's styles.xml. A flat document of draw:page
// elements alone prints its shapes with them.
const builtinXML = `<office:document-styles xmlns:office="` + odf.NSOffice + `" xmlns:style="` + odf.NSStyle +
	`" xmlns:draw="` + odf.NSDraw + `" xmlns:fo="` + odf.NSFO + `" xmlns:svg="` + odf.NSSVG + `"><office:styles>` +
	`<style:default-style style:family="graphic"><style:graphic-properties svg:stroke-color="#3465a4" draw:fill-color="#729fcf" fo:wrap-option="no-wrap"/>` +
	`<style:paragraph-properties style:writing-mode="lr-tb"/>` +
	`<style:text-properties fo:font-family="'Liberation Serif'" fo:font-size="24pt" fo:language="en" fo:country="US" style:font-family-asian="'DejaVu Sans'" style:font-family-complex="'DejaVu Sans'"/></style:default-style>` +
	`<style:style style:name="standard" style:family="graphic"><style:graphic-properties draw:stroke="solid" svg:stroke-width="0.106cm" svg:stroke-color="#127a02" draw:fill="solid" draw:fill-color="#18a303" draw:textarea-horizontal-align="justify" fo:padding-top="0.125cm" fo:padding-bottom="0.125cm" fo:padding-left="0.25cm" fo:padding-right="0.25cm" fo:wrap-option="wrap" draw:shadow="hidden"/>` +
	`<style:paragraph-properties fo:margin-left="0cm" fo:margin-right="0cm" fo:margin-top="0cm" fo:margin-bottom="0cm" fo:line-height="100%" fo:text-indent="0cm"/>` +
	`<style:text-properties style:use-window-font-color="true" fo:font-family="'Liberation Sans'" fo:font-size="18pt" fo:font-style="normal" fo:font-weight="normal" style:text-underline-style="none" style:text-line-through-style="none" style:font-family-asian="'Noto Sans CJK SC'" style:font-family-complex="'Noto Sans Devanagari'"/></style:style>` +
	`</office:styles></office:document-styles>`

// builtinStyles parses [builtinXML].
func builtinStyles() *odf.Styles {
	root, err := odf.ParseXML(context.Background(), []byte(builtinXML))
	if err != nil {
		return odf.NewStyles(&odf.Package{})
	}
	return odf.NewStyles(&odf.Package{Styles: root, Content: root, Flat: true})
}

// layer is the properties one style of a shape's chain sets: its
// graphic, paragraph and text properties.
type layer struct {
	graphic, para, text odf.Props
}

// layerOf returns the properties of the style st.
func layerOf(st *odf.Style) layer {
	return layer{graphic: st.Props(odf.GraphicProps), para: st.Props(odf.ParagraphProps), text: st.Props(odf.TextProps)}
}

// baseLayer returns the graphic default style's properties (ODF 1.3,
// Part 3, 16.3): the document's, else Impress's built-in one.
func (r *reader) baseLayer() layer {
	l := layer{
		graphic: r.styles.Default(odf.FamilyGraphic, odf.GraphicProps),
		para:    r.styles.Default(odf.FamilyGraphic, odf.ParagraphProps),
		text:    r.styles.Default(odf.FamilyGraphic, odf.TextProps),
	}
	if l.graphic.Empty() && l.para.Empty() && l.text.Empty() {
		l.graphic = r.builtin.Default(odf.FamilyGraphic, odf.GraphicProps)
		l.para = r.builtin.Default(odf.FamilyGraphic, odf.ParagraphProps)
		l.text = r.builtin.Default(odf.FamilyGraphic, odf.TextProps)
	}
	return l
}

// styleLayers returns the layers of a shape's style family:name as scope
// sees it, in merge order: the graphic default style, then, for a
// graphic style whose chain names no common style, the "standard"
// style Impress gives every shape it creates (the document's, else the
// built-in one), then the chain from the farthest. A presentation style
// takes no "standard": its chain ends at the master's presentation
// styles, which Impress keeps apart.
func (r *reader) styleLayers(scope odf.Scope, family, name string) []layer {
	out := []layer{r.baseLayer()}
	chain := r.styles.Chain(scope, family, name)
	if family == odf.FamilyGraphic && (len(chain) == 0 || chain[0].Auto && chain[0].Parent == "") {
		std := r.styles.Chain(odf.ScopeStyles, odf.FamilyGraphic, "standard")
		if len(std) == 0 {
			std = r.builtin.Chain(odf.ScopeStyles, odf.FamilyGraphic, "standard")
		}
		for _, st := range std {
			out = append(out, layerOf(st))
		}
	}
	for _, st := range chain {
		out = append(out, layerOf(st))
	}
	return out
}

// textLayers returns the paragraph and text properties of a paragraph or
// text style family:name, the default style and the chain from the
// farthest, as scope sees it; nil for an empty name.
func (r *reader) textLayers(scope odf.Scope, family, name string) []layer {
	if name == "" {
		return nil
	}
	var out []layer
	for _, st := range r.styles.Chain(scope, family, name) {
		out = append(out, layerOf(st))
	}
	return out
}

// shapeStyle is what a shape's styles resolve to: its merged graphic
// properties, and the layers its text inherits from, by outline level
// for an outline placeholder.
type shapeStyle struct {
	scope   odf.Scope
	class   string
	graphic odf.Props
	// text are the layers every paragraph inherits: the shape's style,
	// then its draw:text-style-name.
	text []layer
	// outline holds, for an outline placeholder whose style descends from
	// a master's outline1 presentation style, the layers of each level
	// (ODF 1.3, Part 3, 16.2 and 19.393: the outline levels are the
	// master's styles "<master>-outline1" to "-outline9"), index 0 for
	// level 1; nil otherwise.
	outline [][]layer
}

// maxOutlineLevels is the outline levels of a presentation's outline
// placeholder: the master's outline1 to outline9 styles.
const maxOutlineLevels = 9

// shapeStyle resolves the styles of a shape: its draw:style-name in the
// graphic family, or its presentation:style-name in the presentation
// family (ODF 1.3, Part 3, 19.219 and 19.426), with its
// draw:text-style-name (19.227) for the text.
func (r *reader) shapeStyle(el *odf.Element, pc *pageCtx) *shapeStyle {
	ss := &shapeStyle{scope: pc.scope, class: el.A(odf.NSPresentation, "class")}
	family, name := odf.FamilyGraphic, el.A(odf.NSDraw, "style-name")
	if pn := el.A(odf.NSPresentation, "style-name"); pn != "" && name == "" {
		family, name = odf.FamilyPresentation, pn
	}
	ls := r.styleLayers(pc.scope, family, name)
	chain := r.styles.Chain(pc.scope, family, name)
	if family == odf.FamilyPresentation {
		if base := r.placeholderBase(chain, ss.class, pc); len(base) > 0 {
			// The master's style of the class goes under the shape's own.
			lead := []layer{ls[0]}
			for _, st := range base {
				lead = append(lead, layerOf(st))
			}
			ls = append(lead, ls[1:]...)
			chain = append(base, chain...)
		}
	}
	for _, l := range ls {
		ss.graphic = ss.graphic.Over(l.graphic)
	}
	textStyle := r.textLayers(pc.scope, odf.FamilyParagraph, el.A(odf.NSDraw, "text-style-name"))
	ss.text = append(append([]layer{}, ls...), textStyle...)
	if family != odf.FamilyPresentation {
		return ss
	}
	for i, st := range chain {
		prefix, ok := strings.CutSuffix(st.Name, "outline1")
		if !ok || st.Auto {
			continue
		}
		ss.outline = make([][]layer, maxOutlineLevels)
		for lv := range maxOutlineLevels {
			styles := r.styles.Chain(odf.ScopeStyles, family, prefix+"outline"+string(rune('1'+lv)))
			if len(styles) == 0 {
				ss.outline[lv] = ss.text
				continue
			}
			lvl := []layer{r.baseLayer()}
			for _, s := range styles {
				lvl = append(lvl, layerOf(s))
			}
			for _, s := range chain[i+1:] {
				lvl = append(lvl, layerOf(s))
			}
			ss.outline[lv] = append(lvl, textStyle...)
		}
		break
	}
	return ss
}

// placeholderBase returns the style chain of the master page's
// presentation style for a placeholder of class (title, subtitle,
// outline, notes) whose own presentation style chain holds no common
// style: Impress bases every presentation object on its master's style
// of its class, "<master>-title", "-subtitle", "-outline1" or "-notes"
// (measured on the pinned LibreOffice: sd/tdf169979.odp's title, whose
// automatic style names no parent, prints in its master's title font,
// centred). nil when the chain reaches a common style already.
func (r *reader) placeholderBase(chain []*odf.Style, class string, pc *pageCtx) []*odf.Style {
	suffix := map[string]string{"title": "title", "subtitle": "subtitle", "outline": "outline1", "notes": "notes"}[class]
	if suffix == "" || pc.mp == nil {
		return nil
	}
	for _, st := range chain {
		if !st.Auto {
			return nil
		}
	}
	return r.styles.Chain(odf.ScopeStyles, odf.FamilyPresentation, pc.mp.Name+"-"+suffix)
}

// levelLayers returns the layers a paragraph of list level lv, 0 for the
// first, inherits.
func (ss *shapeStyle) levelLayers(lv int) []layer {
	if ss.outline == nil {
		return ss.text
	}
	return ss.outline[min(max(lv, 0), maxOutlineLevels-1)]
}
