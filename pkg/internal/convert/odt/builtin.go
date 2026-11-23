package odt

import (
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// builtinStyle is a paragraph style Writer has built in, which a document
// may name without defining: its parent, and its paragraph and text
// properties as attributes.
type builtinStyle struct {
	parent     string
	para, text [][2]string
}

// builtins are the built-in paragraph styles of Writer 24.8 that a
// heading or a note without a style of its own takes (sw/source/core/
// doc/DocumentStylePoolManager.cxx): "Heading" in Liberation Sans 14pt
// with its spacing and keep-with-next, the headings of levels 1 to 3 in
// bold at 18, 16 and 14pt, and the note styles at 10pt with a hanging
// indent of 0.6 cm, and the title of a table of contents at 16pt bold.
var builtins = map[string]builtinStyle{
	"Heading": {
		para: [][2]string{{"fo:margin-top", "0.423cm"}, {"fo:margin-bottom", "0.212cm"}, {"fo:keep-with-next", "always"}},
		text: [][2]string{{"style:font-name", "Liberation Sans"}, {"fo:font-family", "'Liberation Sans'"}, {"fo:font-size", "14pt"}},
	},
	"Heading_20_1":        {parent: "Heading", text: [][2]string{{"fo:font-size", "18pt"}, {"fo:font-weight", "bold"}}},
	"Heading_20_2":        {parent: "Heading", text: [][2]string{{"fo:font-size", "16pt"}, {"fo:font-weight", "bold"}}},
	"Heading_20_3":        {parent: "Heading", text: [][2]string{{"fo:font-size", "14pt"}, {"fo:font-weight", "bold"}}},
	"Contents_20_Heading": {parent: "Heading", text: [][2]string{{"fo:font-size", "16pt"}, {"fo:font-weight", "bold"}}},
	"Footnote": {
		para: [][2]string{{"fo:margin-left", "0.5985cm"}, {"fo:text-indent", "-0.5985cm"}},
		text: [][2]string{{"fo:font-size", "10pt"}},
	},
	"Endnote": {
		para: [][2]string{{"fo:margin-left", "0.5985cm"}, {"fo:text-indent", "-0.5985cm"}},
		text: [][2]string{{"fo:font-size", "10pt"}},
	},
}

// maxBuiltinChain bounds the parents of a built-in style followed: the
// table is two deep.
const maxBuiltinChain = 4

// builtinLayers returns the property layers of kind of the paragraph style
// name when the document does not define it and Writer has it built in,
// the farthest parent first; nil otherwise.
func (b *builder) builtinLayers(name, kind string) []odf.Props {
	if b.styles.Style(b.scope, odf.FamilyParagraph, name) != nil {
		return nil
	}
	var chain []builtinStyle
	for range maxBuiltinChain {
		bs, ok := builtins[name]
		if !ok {
			break
		}
		chain = append([]builtinStyle{bs}, chain...)
		if bs.parent == "" || b.styles.Style(b.scope, odf.FamilyParagraph, bs.parent) != nil {
			break
		}
		name = bs.parent
	}
	var out []odf.Props
	for _, bs := range chain {
		attrs := bs.text
		if kind == odf.ParagraphProps {
			attrs = bs.para
		}
		el := &odf.Element{}
		for _, a := range attrs {
			space, local := splitKey(a[0])
			el.Attr = append(el.Attr, xmlAttr(space, local, a[1]))
		}
		out = append(out, odf.PropsOf(el))
	}
	return out
}

// builtinFrame holds the graphic properties of Writer's built-in style
// Frame (sw/source/core/doc/DocumentStylePoolManager.cxx,
// RES_POOLFRM_FRAME), which a frame's style may name as its parent
// without the document defining it: 0.201 cm margins, a 0.15 cm padding
// inside a 0.06 pt black border, parallel wrap, at the top of its
// paragraph's content and centred across it.
//
// # Corpus evidence
//
//   - libreoffice/odt: odfimport/bnc800714.fodt, which defines no
//     style, prints its frame of parent Frame in a black hairline 0.2 cm
//     inside its line and 0.15 cm outside its text.
//   - probe, odt: through gotenberg/gotenberg:9.4.1, a 4 by 2 cm frame of
//     parent Frame anchored to the second paragraph of a document that
//     defines no style prints 0.2 cm below the paragraph's top, centred
//     between the margins, outlined in black, its text 0.15 cm inside,
//     the paragraph's words on both sides of it.
var builtinFrame = [][2]string{
	{"fo:margin-left", "0.201cm"},
	{"fo:margin-right", "0.201cm"},
	{"fo:margin-top", "0.201cm"},
	{"fo:margin-bottom", "0.201cm"},
	{"fo:padding", "0.15cm"},
	{"fo:border", "0.06pt solid #000000"},
	{"style:wrap", "parallel"},
	{"style:number-wrapped-paragraphs", "no-limit"},
	{"style:vertical-pos", "top"},
	{"style:vertical-rel", "paragraph-content"},
	{"style:horizontal-pos", "center"},
	{"style:horizontal-rel", "paragraph-content"},
}

// graphicProps returns the graphic properties of the graphic style name
// ([odf.Styles.Resolve]), Writer's built-in Frame ([builtinFrame]) under
// its chain when the chain ends in that name and the document does not
// define it.
func (b *builder) graphicProps(name string) odf.Props {
	chain := b.styles.Chain(b.scope, odf.FamilyGraphic, name)
	root := name
	if len(chain) > 0 {
		root = chain[0].Parent
	}
	if root != "Frame" || b.styles.Style(b.scope, odf.FamilyGraphic, root) != nil {
		return b.styles.Resolve(b.scope, odf.FamilyGraphic, name, odf.GraphicProps)
	}
	el := &odf.Element{}
	for _, a := range builtinFrame {
		space, local := splitKey(a[0])
		el.Attr = append(el.Attr, xmlAttr(space, local, a[1]))
	}
	p := b.styles.Default(odf.FamilyGraphic, odf.GraphicProps).Over(odf.PropsOf(el))
	for _, st := range chain {
		p = p.Over(st.Props(odf.GraphicProps))
	}
	return p
}

// splitKey returns the namespace and local name of a property key.
func splitKey(key string) (string, string) {
	for i := 0; i < len(key); i++ {
		if key[i] == ':' {
			switch key[:i] {
			case "fo":
				return odf.NSFO, key[i+1:]
			case "style":
				return odf.NSStyle, key[i+1:]
			}
		}
	}
	return "", key
}
