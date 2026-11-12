package docx

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// embedKey is the w:fontKey of the test's embedded font.
const embedKey = "{4C1A2B3D-5E6F-4071-8293-A4B5C6D7E8F9}"

// embedSans builds an "Embed Sans" face no index holds, every glyph a full
// em wide.
func embedSans() []byte {
	glyphs := []fontbuild.Glyph{{Name: "space", Unicode: ' ', Advance: 1000, Empty: true}}
	for r := rune('!'); r <= '~'; r++ {
		glyphs = append(glyphs, fontbuild.Glyph{Name: fmt.Sprintf("uni%04X", r), Unicode: r, Advance: 1000})
	}
	return fontbuild.TrueType{PostScriptName: "EmbedSans-Regular", Family: "Embed Sans", Subfamily: "Regular", NotdefAdvance: 1000, Glyphs: glyphs}.Build()
}

// obfuscate XORs the head of program with key as Word writes an embedded
// font part (ECMA-376 Part 1, 17.8.1), from the key's last byte to its
// first.
func obfuscate(program []byte, key []byte) []byte {
	out := bytes.Clone(program)
	for i := range 32 {
		out[i] ^= key[len(key)-1-i%len(key)]
	}
	return out
}

// embedDocx returns a DOCX whose font table embeds program as the regular
// face of Embed Sans, with fontKey when it is not empty, and whose one run
// is set in that family.
func embedDocx(t *testing.T, program []byte, fontKey string) []byte {
	t.Helper()
	key := ""
	if fontKey != "" {
		key = ` w:fontKey="` + fontKey + `"`
	}
	files := pkgFiles(`<w:p><w:r><w:rPr><w:rFonts w:ascii="Embed Sans" w:hAnsi="Embed Sans"/></w:rPr><w:t>Hi</w:t></w:r></w:p>`, nil)
	files["word/fontTable.xml"] = `<w:fonts ` + nsDecl + `><w:font w:name="Embed Sans"><w:embedRegular r:id="rId1"` + key + `/></w:font></w:fonts>`
	files["word/_rels/fontTable.xml.rels"] = `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/font" Target="fonts/font1.odttf"/></Relationships>`
	files["word/fonts/font1.odttf"] = string(program)
	files["word/_rels/document.xml.rels"] = strings.Replace(files["word/_rels/document.xml.rels"], `</Relationships>`,
		`<Relationship Id="rIdfonts" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/fontTable" Target="fontTable.xml"/></Relationships>`, 1)
	return zipFiles(t, files)
}

// textRuns returns the text runs of a document's pages.
func textRuns(doc *typeset.Document) []*typeset.Text {
	var out []*typeset.Text
	var walk func([]typeset.Fragment)
	walk = func(frags []typeset.Fragment) {
		for _, f := range frags {
			switch v := f.(type) {
			case *typeset.Text:
				out = append(out, v)
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	for _, pg := range doc.Pages {
		walk(pg.Content)
	}
	return out
}

// TestEmbeddedFontDrawsTheRun locks that a run in a family the font table
// embeds draws with the embedded program, deobfuscated by its w:fontKey
// (ECMA-376 Part 1, 17.8.1) or read as it is without one, a full em per
// glyph where the installed test face draws half an em.
func TestEmbeddedFontDrawsTheRun(t *testing.T) {
	program := embedSans()
	key := []byte{0x4C, 0x1A, 0x2B, 0x3D, 0x5E, 0x6F, 0x40, 0x71, 0x82, 0x93, 0xA4, 0xB5, 0xC6, 0xD7, 0xE8, 0xF9}
	for _, tc := range []struct {
		name, fontKey string
		part          []byte
	}{
		{"obfuscated", embedKey, obfuscate(program, key)},
		{"plain", "", program},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := convertBytes(t, embedDocx(t, tc.part, tc.fontKey), office.Options{})
			runs := textRuns(doc)
			if len(runs) == 0 {
				t.Fatal("no text run")
			}
			r := runs[0]
			if !bytes.Equal(r.Face.Program, program) {
				t.Fatalf("the run draws with a %d-byte program, want the embedded %d-byte one", len(r.Face.Program), len(program))
			}
			if len(r.Glyphs) != 2 || r.Glyphs[1].X-r.Glyphs[0].X != r.Size {
				t.Errorf("glyphs %+v at %g pt: want two, a full em apart", r.Glyphs, r.Size)
			}
			if len(doc.Warnings) != 0 {
				t.Errorf("warnings %v, want none", doc.Warnings)
			}
		})
	}
}

// TestEmbeddedFontThatDoesNotLoadWarns locks that an embedded font whose
// key is not a GUID, or whose program does not parse, is left out with a
// font-not-loaded warning and its run drawn in an installed face.
func TestEmbeddedFontThatDoesNotLoadWarns(t *testing.T) {
	for _, tc := range []struct {
		name, fontKey string
		part          []byte
	}{
		{"bad key", "{not-a-guid}", embedSans()},
		{"not a font", "", bytes.Repeat([]byte{0x55}, 64)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := convertBytes(t, embedDocx(t, tc.part, tc.fontKey), office.Options{})
			if !strings.Contains(docText(doc), "Hi") {
				t.Fatalf("text %q, want the run drawn", docText(doc))
			}
			if len(doc.Warnings) != 1 || doc.Warnings[0].Code != warn.FontNotLoaded || doc.Warnings[0].Effect != warn.Approximated {
				t.Errorf("warnings %v, want one font-not-loaded, approximated", doc.Warnings)
			}
		})
	}
}
