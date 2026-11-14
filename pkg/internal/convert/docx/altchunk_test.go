package docx

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// TestAltChunk locks ECMA-376 Part 1, 17.17.2.1 altChunk: an HTML chunk
// flows in place in the body, its heading in the document's heading
// style, its paragraphs, list items and table cells as Word imports
// them; a chunk of another kind is left out with a warning naming it.
func TestAltChunk(t *testing.T) {
	t.Parallel()
	html := `<html><head><style>p{color:red}</style><title>T</title></head><body><h2>Chunk title</h2>` +
		`<p>An <b>HTML</b> fragment with a <a href="x">link</a>.</p><ul><li>first</li><li>second</li></ul>` +
		`<table><tr><td>a</td><td>b</td></tr></table></body></html>`
	body := `<w:p><w:r><w:t>Before.</w:t></w:r></w:p><w:altChunk r:id="rIdH"/><w:altChunk r:id="rIdR"/><w:p><w:r><w:t>After.</w:t></w:r></w:p>`
	styles := `<w:styles ` + nsDecl + `><w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:rPr><w:b/></w:rPr></w:style></w:styles>`
	files := pkgFiles(body, map[string]string{"word/styles.xml": styles, "word/chunk.htm": html, "word/chunk.rtf": `{\rtf1 x}`})
	files["word/_rels/document.xml.rels"] = strings.Replace(files["word/_rels/document.xml.rels"], `</Relationships>`,
		`<Relationship Id="rIdH" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/aFChunk" Target="chunk.htm"/>`+
			`<Relationship Id="rIdR" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/aFChunk" Target="chunk.rtf"/></Relationships>`, 1)
	pkg, err := ooxml.Open(t.Context(), zipFiles(t, files))
	if err != nil {
		t.Fatal(err)
	}
	d, err := load(context.Background(), pkg, testFonts(t), office.Options{})
	if err != nil {
		t.Fatal(err)
	}
	root := &layout.Node{Kind: layout.Element, Style: d.blockStyle()}
	for _, s := range d.body(d.root) {
		root.Children = append(root.Children, d.sectionNode(s, nil))
	}
	got := textOf(root)
	for _, want := range []string{"Before.", "Chunk title", "An HTML fragment with a link.", "first", "second", "a", "b", "After."} {
		if !strings.Contains(got, want) {
			t.Errorf("text %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "color:red") || strings.Contains(got, "T"+"Chunk") {
		t.Errorf("head content drawn: %q", got)
	}
	if strings.Index(got, "Before.") > strings.Index(got, "Chunk title") || strings.Index(got, "second") > strings.Index(got, "After.") {
		t.Errorf("chunk out of place: %q", got)
	}
	if len(role(root, "Table")) != 1 {
		t.Errorf("%d tables, want the chunk's", len(role(root, "Table")))
	}
	rtf := false
	for _, w := range d.warnings {
		rtf = rtf || strings.Contains(w.Message, "of RTF is left out")
	}
	if !rtf {
		t.Errorf("warnings %+v lack the RTF chunk's", d.warnings)
	}
}

// TestChunkKind locks the kinds a chunk is named by: its content type,
// else its extension.
func TestChunkKind(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ ct, name, want string }{
		{"text/html", "a.bin", "HTML"},
		{"", "/word/afchunk.htm", "HTML"},
		{"application/rtf", "x", "RTF"},
		{"message/rfc822", "x", "MHTML"},
		{"application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml", "x", "WordprocessingML"},
		{"", "a.docx", "WordprocessingML"},
		{"application/x-foo", "a", "type application/x-foo"},
	} {
		if got := chunkKind(tc.ct, tc.name); got != tc.want {
			t.Errorf("chunkKind(%q, %q) = %q, want %q", tc.ct, tc.name, got, tc.want)
		}
	}
}
