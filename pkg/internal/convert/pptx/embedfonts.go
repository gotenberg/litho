package pptx

import (
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// embedStyles are the elements of a p:embeddedFont that name one of its
// faces (ECMA-376 Part 1, 19.2.1.9), with the style each names.
var embedStyles = []struct {
	local        string
	bold, italic bool
}{
	{"regular", false, false},
	{"bold", true, false},
	{"italic", false, true},
	{"boldItalic", true, true},
}

// loadEmbeddedFonts loads the faces the presentation root embeds
// (ECMA-376 Part 1, 19.2.1.9 embeddedFont and 19.2.1.10 embeddedFontLst)
// and lays the slides out with them: a run in a typeface the presentation
// embeds draws with the embedded face of its style. Each face is a font
// part the presentation part relates by r:id, an sfnt program or an
// Embedded OpenType file ([ooxml.EmbeddedFontProgram]). A face that does
// not load is left out with a warning, and its runs take the typeface's
// installed face or its fallback.
//
// # Corpus evidence
//
//   - libreoffice/pptx: sd-data/BoldonseFontEmbedded.pptx and
//     sd/multiplelayoutfooter.pptx embed EOT files whose FontData
//     MicroType Express compresses; PowerPoint's reference of the first
//     draws its text with the embedded face, under the name "3".
func (r *reader) loadEmbeddedFonts(root *ooxml.Element) {
	var set ooxml.EmbeddedFonts
	for _, ef := range root.Child(nsP, "embeddedFontLst").Kids() {
		if !ef.Is(nsP, "embeddedFont") {
			continue
		}
		family := strings.TrimSpace(attr(ef.Child(nsP, "font"), "typeface"))
		if family == "" {
			continue
		}
		for _, st := range embedStyles {
			e := ef.Child(nsP, st.local)
			if e == nil {
				continue
			}
			err := r.loadEmbeddedFace(&set, e, family, st.bold, st.italic)
			if err == nil {
				continue
			}
			if r.stop(err) {
				return
			}
			r.warn(warn.FontNotLoaded, warn.Approximated, fmt.Sprintf("the font %q the presentation embeds does not load (%v); its text prints in another face", family, err))
		}
	}
	r.fonts = set.Apply(r.fonts)
}

// loadEmbeddedFace reads the font part e names and loads it into set as
// family's face in the style bold and italic name.
func (r *reader) loadEmbeddedFace(set *ooxml.EmbeddedFonts, e *ooxml.Element, family string, bold, italic bool) error {
	id, _ := e.AttrNS(ooxml.NSR, "id")
	rel, ok := r.pkg.RelationshipByID(r.ctx, r.main, id)
	if !ok || rel.External {
		return fmt.Errorf("no font part for relationship %q", id)
	}
	data, err := r.pkg.Read(rel.Target)
	if err != nil {
		return err
	}
	program, err := ooxml.EmbeddedFontProgram(data)
	if err != nil {
		return err
	}
	return set.Load(r.ctx, program, family, bold, italic)
}
