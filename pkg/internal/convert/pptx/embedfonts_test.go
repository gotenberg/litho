package pptx

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// embedSans builds an "Embed Sans" face no index holds, every glyph a full
// em wide.
func embedSans() []byte {
	glyphs := []fontbuild.Glyph{{Name: "space", Unicode: ' ', Advance: 1000, Empty: true}}
	for r := rune('!'); r <= '~'; r++ {
		glyphs = append(glyphs, fontbuild.Glyph{Name: fmt.Sprintf("uni%04X", r), Unicode: r, Advance: 1000})
	}
	return fontbuild.TrueType{PostScriptName: "EmbedSans-Regular", Family: "Embed Sans", Subfamily: "Regular", NotdefAdvance: 1000, Glyphs: glyphs}.Build()
}

// eot wraps program in a version 0x00010000 Embedded OpenType file with
// empty names and the flags given, XOR-encrypting it when they say so (W3C
// Member Submission, Embedded OpenType File Format, 3.1 and 4.4).
func eot(program []byte, flags uint32) []byte {
	const header = 36 + 16 + 8 + 4 + 16 + 2 + 4*4
	data := bytes.Clone(program)
	if flags&0x10000000 != 0 {
		for i := range data {
			data[i] ^= 0x50
		}
	}
	out := make([]byte, header, header+len(data))
	binary.LittleEndian.PutUint32(out[0:], uint32(header+len(data)))
	binary.LittleEndian.PutUint32(out[4:], uint32(len(data)))
	binary.LittleEndian.PutUint32(out[8:], 0x00010000)
	binary.LittleEndian.PutUint32(out[12:], flags)
	binary.LittleEndian.PutUint32(out[28:], 400)
	binary.LittleEndian.PutUint16(out[34:], 0x504C)
	return append(out, data...)
}

// embedDeck returns a presentation whose one slide sets "Hi" in Embed
// Sans and which embeds part as that typeface's regular face.
func embedDeck(t *testing.T, part []byte) []byte {
	t.Helper()
	run := `<a:bodyPr/><a:lstStyle/><a:p><a:r><a:rPr lang="en-US" sz="2000"><a:latin typeface="Embed Sans"/></a:rPr><a:t>Hi</a:t></a:r></a:p>`
	files := deckFiles(testDeck{
		slides:    []testSlide{{tree: phShape("Box", "", -1, 0, 0, 4000000, 1000000, run)}},
		presExtra: `<p:embeddedFontLst><p:embeddedFont><p:font typeface="Embed Sans"/><p:regular r:id="rIdF"/></p:embeddedFont></p:embeddedFontLst>`,
	})
	files["ppt/fonts/font1.fntdata"] = string(part)
	files["ppt/_rels/presentation.xml.rels"] = strings.Replace(files["ppt/_rels/presentation.xml.rels"], `</Relationships>`,
		`<Relationship Id="rIdF" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/font" Target="fonts/font1.fntdata"/></Relationships>`, 1)
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

// TestEmbeddedFontDrawsTheRun locks that a run in a typeface the
// presentation embeds draws with the embedded program, stored as an sfnt
// program, an Embedded OpenType file, or one whose font data is
// XOR-encrypted.
func TestEmbeddedFontDrawsTheRun(t *testing.T) {
	program := embedSans()
	for _, tc := range []struct {
		name string
		part []byte
	}{
		{"sfnt", program},
		{"eot", eot(program, 0)},
		{"eot xor", eot(program, 0x10000000)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := Convert(context.Background(), bytes.NewReader(embedDeck(t, tc.part)), testFonts(t), office.Options{})
			if err != nil {
				t.Fatal(err)
			}
			var hi *typeset.Text
			for _, r := range textRuns(doc) {
				if len(r.Glyphs) > 0 && r.Glyphs[0].Text == "H" {
					hi = r
				}
			}
			if hi == nil {
				t.Fatal("no run of Hi")
			}
			if !bytes.Equal(hi.Face.Program, program) {
				t.Fatalf("the run draws with a %d-byte program, want the embedded %d-byte one", len(hi.Face.Program), len(program))
			}
			if len(doc.Warnings) != 0 {
				t.Errorf("warnings %v, want none", doc.Warnings)
			}
		})
	}
}

// TestEmbeddedFontThatDoesNotLoadWarns locks that an embedded font
// compressed with MicroType Express, or forbidden to embed by its fsType,
// is left out with a font-not-loaded warning and its run drawn in an
// installed face.
func TestEmbeddedFontThatDoesNotLoadWarns(t *testing.T) {
	glyphs := []fontbuild.Glyph{{Name: "space", Unicode: ' ', Advance: 1000, Empty: true}, {Name: "H", Unicode: 'H', Advance: 1000}, {Name: "i", Unicode: 'i', Advance: 1000}}
	restricted := fontbuild.TrueType{PostScriptName: "EmbedSans-Regular", Family: "Embed Sans", Subfamily: "Regular", NotdefAdvance: 1000, Glyphs: glyphs, FSType: 0x0002}.Build()
	for _, tc := range []struct {
		name, reason string
		part         []byte
	}{
		{"compressed", "MicroType Express", eot(embedSans(), 0x00000004)},
		{"restricted", "fsType 0x0002", restricted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := Convert(context.Background(), bytes.NewReader(embedDeck(t, tc.part)), testFonts(t), office.Options{})
			if err != nil {
				t.Fatal(err)
			}
			n := 0
			for _, w := range doc.Warnings {
				if w.Code == warn.FontNotLoaded && w.Effect == warn.Approximated && strings.Contains(w.Message, tc.reason) {
					n++
				}
			}
			if n != 1 {
				t.Errorf("warnings %v, want one font-not-loaded naming %q", doc.Warnings, tc.reason)
			}
			for _, r := range textRuns(doc) {
				if bytes.Equal(r.Face.Program, tc.part) {
					t.Error("a run draws with the font that does not load")
				}
			}
		})
	}
}
