package docx

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/text"
)

func TestConvert_Errors(t *testing.T) {
	t.Parallel()
	good := docx(t, `<w:p><w:r><w:t>x</w:t></w:r></w:p>`, nil)
	noMain := pkgFiles(`<w:p/>`, nil)
	delete(noMain, "word/document.xml")
	notWord := pkgFiles(`<w:p/>`, nil)
	notWord["word/document.xml"] = `<x:sheet xmlns:x="urn:x"/>`
	for _, tc := range []struct {
		name  string
		data  []byte
		fonts *text.Fonts
		opts  office.Options
		want  error
	}{
		{"nil fonts", good, nil, office.Options{}, office.ErrInvalidOptions},
		{"half a page size", good, testFonts(t), office.Options{PageWidth: 100}, office.ErrInvalidOptions},
		{"malformed compound file", append([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, make([]byte, 600)...), testFonts(t), office.Options{}, office.ErrMalformed},
		{"not a zip", []byte("plain text"), testFonts(t), office.Options{}, office.ErrMalformed},
		{"no main part", zipFiles(t, noMain), testFonts(t), office.Options{}, office.ErrMalformed},
		{"not a document", zipFiles(t, notWord), testFonts(t), office.Options{}, office.ErrMalformed},
		{"too large", good, testFonts(t), office.Options{MaxInputBytes: 10}, office.ErrInputTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Convert(context.Background(), bytes.NewReader(tc.data), tc.fonts, tc.opts)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Convert = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestConvert_Password opens Gotenberg's password-protected document, a
// Standard-encrypted package (MS-OFFCRYPTO, 2.3.4.5) whose password is
// foo, and refuses it without the password or with another one.
func TestConvert_Password(t *testing.T) {
	t.Parallel()
	path := findSource(paritySources(), "protected_page_1.docx")
	if path == "" {
		t.Skip("protected_page_1.docx not in the docx corpus sources")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, password string
		want           error
	}{
		{"right password", "foo", nil},
		{"wrong password", "bar", office.ErrEncrypted},
		{"no password", "", office.ErrEncrypted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, err := Convert(context.Background(), bytes.NewReader(data), testFonts(t), office.Options{Password: tc.password})
			if !errors.Is(err, tc.want) {
				t.Fatalf("Convert = %v, want %v", err, tc.want)
			}
			if tc.want == nil && (len(doc.Pages) != 1 || pageText(doc.Pages[0]) != "Page 1") {
				t.Errorf("%d pages: %q, want one page with Page 1", len(doc.Pages), docText(doc))
			}
		})
	}
}

func TestConvert_Cancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Convert(ctx, bytes.NewReader(docx(t, `<w:p/>`, nil)), testFonts(t), office.Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Convert = %v, want context.Canceled", err)
	}
}

func TestConvert_TextAndPages(t *testing.T) {
	t.Parallel()
	body := `<w:p><w:r><w:t>First page</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t xml:space="preserve">Before </w:t></w:r><w:r><w:br w:type="page"/></w:r><w:r><w:t>after the break</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:pageBreakBefore/></w:pPr><w:r><w:t>Third page</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>Deleted</w:t></w:r></w:p>`
	body = strings.Replace(body, `<w:p><w:r><w:t>Deleted</w:t></w:r></w:p>`, `<w:p><w:del w:id="1" w:author="a"><w:r><w:delText>GONE</w:delText></w:r></w:del><w:ins w:id="2" w:author="a"><w:r><w:t>kept</w:t></w:r></w:ins></w:p>`, 1)
	doc := convertBytes(t, docx(t, body, nil), office.Options{})
	if len(doc.Pages) != 3 {
		t.Fatalf("%d pages, want 3: %q", len(doc.Pages), docText(doc))
	}
	for i, want := range []string{"First page\nBefore", "after the break", "Third page\nkept"} {
		if got := pageText(doc.Pages[i]); got != want {
			t.Errorf("page %d = %q, want %q", i+1, got, want)
		}
	}
	if strings.Contains(docText(doc), "GONE") {
		t.Error("deleted text drawn")
	}
}

func TestConvert_PageOptions(t *testing.T) {
	t.Parallel()
	body := `<w:p><w:r><w:t>x</w:t></w:r></w:p><w:sectPr><w:pgSz w:w="11906" w:h="16838"/></w:sectPr>`
	for _, tc := range []struct {
		name string
		opts office.Options
		w, h float64
	}{
		{"section size", office.Options{}, 595.3, 841.9},
		{"landscape", office.Options{Landscape: true}, 841.9, 595.3},
		{"paper size", office.Options{PageWidth: 612, PageHeight: 792}, 612, 792},
		{"paper size landscape", office.Options{PageWidth: 612, PageHeight: 792, Landscape: true}, 792, 612},
	} {
		doc := convertBytes(t, docx(t, body, nil), tc.opts)
		pg := doc.Pages[0]
		if abs(pg.Width-tc.w) > 0.1 || abs(pg.Height-tc.h) > 0.1 {
			t.Errorf("%s: page %v x %v, want %v x %v", tc.name, pg.Width, pg.Height, tc.w, tc.h)
		}
	}
}

func TestConvert_Metadata(t *testing.T) {
	t.Parallel()
	extra := map[string]string{
		"docProps/core.xml": `<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/">` +
			`<dc:title>The title</dc:title><dc:creator>Ann Author</dc:creator><dc:description>About it</dc:description><cp:keywords>a, b</cp:keywords><dc:language>fr-FR</dc:language></cp:coreProperties>`,
	}
	doc := convertBytes(t, docx(t, `<w:p><w:r><w:t>x</w:t></w:r></w:p>`, extra), office.Options{})
	m := doc.Metadata
	if m.Title != "The title" || m.Author != "Ann Author" || m.Subject != "About it" || m.Keywords != "a, b" || m.Lang != "fr-FR" {
		t.Errorf("metadata = %+v", m)
	}
}

func TestConvert_Outline(t *testing.T) {
	t.Parallel()
	styles := stylesPart(`<w:style w:type="paragraph" w:styleId="H1"><w:pPr><w:outlineLvl w:val="0"/></w:pPr></w:style>` +
		`<w:style w:type="paragraph" w:styleId="H2"><w:pPr><w:outlineLvl w:val="1"/></w:pPr></w:style>`)
	body := `<w:p><w:pPr><w:pStyle w:val="H1"/></w:pPr><w:r><w:t>One</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:pStyle w:val="H2"/></w:pPr><w:r><w:t>One A</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:pStyle w:val="H1"/></w:pPr><w:r><w:t>Two</w:t></w:r></w:p>`
	data := docx(t, body, map[string]string{"word/styles.xml": styles})
	doc := convertBytes(t, data, office.Options{})
	if len(doc.Outline) != 2 || doc.Outline[0].Title != "One" || len(doc.Outline[0].Kids) != 1 || doc.Outline[0].Kids[0].Title != "One A" || doc.Outline[1].Title != "Two" {
		t.Fatalf("outline = %+v", doc.Outline)
	}
	if doc := convertBytes(t, data, office.Options{OmitOutline: true}); doc.Outline != nil {
		t.Error("outline kept with OmitOutline")
	}
	b := build(t, body, map[string]string{"word/styles.xml": styles})
	if len(role(b.root, "H1")) != 2 || len(role(b.root, "H2")) != 1 {
		t.Error("heading roles not set from the outline levels")
	}
}

func TestHeadingTitle_LeavesOutAnchoredText(t *testing.T) {
	t.Parallel()
	inline := css.Initial()
	float := css.Initial()
	float.Float = css.FloatLeft
	abs := css.Initial()
	abs.Position = css.Absolute
	txtNode := func(st *css.Style, s string) *layout.Node {
		return &layout.Node{Kind: layout.Element, Style: st, Children: []*layout.Node{{Kind: layout.Text, Style: st, Text: s}}}
	}
	h := &layout.Node{Kind: layout.Element, Style: inline, Children: []*layout.Node{
		txtNode(inline, "Contents:"), txtNode(float, "Box"), txtNode(abs, "Frame"),
		{Kind: layout.Replaced, Style: abs, Alt: "image2"},
		{Kind: layout.Replaced, Style: float, Alt: "image3"},
	}}
	if got := headingTitle(h); got != "Contents:" {
		t.Errorf("title %q, want Contents:", got)
	}
	// A picture in the line is part of the heading: its description reads
	// in the title.
	h.Children = append(h.Children, &layout.Node{Kind: layout.Replaced, Style: inline, Alt: "logo"})
	if got := headingTitle(h); got != "Contents:logo" {
		t.Errorf("title %q, want Contents:logo", got)
	}
}

func TestOutline_MarkerThenSpace(t *testing.T) {
	t.Parallel()
	st := css.Initial()
	marker := &layout.Node{Kind: layout.Element, Style: st, Children: []*layout.Node{{Kind: layout.Text, Style: st, Text: "1.1"}}}
	n := &layout.Node{Kind: layout.Element, Style: st, Marker: marker, Children: []*layout.Node{{Kind: layout.Text, Style: st, Text: "Section"}}}
	if got := headingTitle(n); got != "1.1 Section" {
		t.Errorf("title %q, want %q", got, "1.1 Section")
	}
}
