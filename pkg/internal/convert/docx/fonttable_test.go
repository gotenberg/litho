package docx

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// TestFallbackFor_TriesTheAlternateNameFirst locks that a family the font
// table gives an alternate name falls back to that name before the faces
// of its class, and that a family without one keeps its class's faces.
func TestFallbackFor_TriesTheAlternateNameFirst(t *testing.T) {
	files := pkgFiles(`<w:p><w:r><w:t>Hi</w:t></w:r></w:p>`, nil)
	files["word/fontTable.xml"] = `<w:fonts ` + nsDecl + `>` +
		`<w:font w:name="Kings Text"><w:altName w:val="Times New Roman"/><w:family w:val="roman"/></w:font>` +
		`<w:font w:name="Plain Sans"><w:family w:val="swiss"/></w:font>` +
		`<w:font w:name="Self Named"><w:altName w:val="self named"/><w:family w:val="roman"/></w:font></w:fonts>`
	files["word/_rels/document.xml.rels"] = strings.Replace(files["word/_rels/document.xml.rels"], `</Relationships>`,
		`<Relationship Id="rIdfonts" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/fontTable" Target="fontTable.xml"/></Relationships>`, 1)
	pkg, err := ooxml.Open(t.Context(), zipFiles(t, files))
	if err != nil {
		t.Fatalf("ooxml.Open: %v", err)
	}
	d, err := load(context.Background(), pkg, testFonts(t), office.Options{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, tc := range []struct {
		family string
		want   []string
	}{
		{"Kings Text", []string{"Times New Roman", fallbackSerif, "DejaVu Serif"}},
		{"Plain Sans", []string{fallbackSans, "DejaVu Sans"}},
		{"Self Named", []string{fallbackSerif, "DejaVu Serif"}},
	} {
		if got := d.fallbackFor(tc.family); !slices.Equal(got, tc.want) {
			t.Errorf("fallbackFor(%q) = %v, want %v", tc.family, got, tc.want)
		}
	}
}
