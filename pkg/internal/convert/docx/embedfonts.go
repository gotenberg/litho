package docx

import (
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// embedElements are the elements of a w:font that embed one of its faces
// (ECMA-376 Part 1, 17.8.3.3 to 17.8.3.6), with the style each names.
var embedElements = []struct {
	local        string
	bold, italic bool
}{
	{"embedRegular", false, false},
	{"embedBold", true, false},
	{"embedItalic", false, true},
	{"embedBoldItalic", true, true},
}

// loadEmbeddedFonts loads the faces the font table root of the part
// fontPart embeds (ECMA-376 Part 1, 17.8.1 and 17.8.3) and lays the
// document out with them: a run in a family the document embeds draws
// with the embedded face of its style. Each embedding element names the
// font part by its r:id and, for an obfuscated program, the key in
// w:fontKey. A face that does not load is left out with a warning, and
// its runs take the family's installed face or its fallback.
//
// # Corpus evidence
//
//   - litho/docx embedded-fonts: Word's reference draws the line set in
//     Ahem, embedded obfuscated with a w:fontKey, with the embedded program
//     (a ___WRD_EMBED_SUB face), and Ahem is not installed.
func (d *document) loadEmbeddedFonts(fontPart string, root *ooxml.Element) {
	if root == nil {
		return
	}
	var set ooxml.EmbeddedFonts
	for _, f := range root.Kids() {
		if !f.Is(w, "font") {
			continue
		}
		family := strings.TrimSpace(attrOr(f, "name"))
		if family == "" {
			continue
		}
		for _, el := range embedElements {
			e := f.Child(w, el.local)
			if e == nil {
				continue
			}
			err := d.loadEmbeddedFace(&set, fontPart, e, family, el.bold, el.italic)
			if err == nil {
				continue
			}
			if d.stop(err) {
				return
			}
			d.warn(warn.FontNotLoaded, warn.Approximated, fmt.Sprintf("the font %q the document embeds does not load (%v); its text prints in another face", family, err))
		}
	}
	if fonts := set.Apply(d.fonts); fonts != d.fonts {
		d.fonts = fonts
		clear(d.lineFactors)
	}
}

// loadEmbeddedFace reads the font part the embedding element e names and
// loads it into set as family's face in the style bold and italic name.
func (d *document) loadEmbeddedFace(set *ooxml.EmbeddedFonts, fontPart string, e *ooxml.Element, family string, bold, italic bool) error {
	id, _ := e.AttrNS(ooxml.NSR, "id")
	rel, ok := d.pkg.RelationshipByID(d.ctx, fontPart, id)
	if !ok || rel.External {
		return fmt.Errorf("no font part for relationship %q", id)
	}
	data, err := d.pkg.Read(rel.Target)
	if err != nil {
		return err
	}
	// A key marks an obfuscated program (17.8.1); a part without one holds
	// the program as it is.
	if key := attrOr(e, "fontKey"); key != "" {
		if err := ooxml.DeobfuscateFont(data, key); err != nil {
			return err
		}
	}
	return set.Load(d.ctx, data, family, bold, italic)
}
