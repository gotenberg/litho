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
