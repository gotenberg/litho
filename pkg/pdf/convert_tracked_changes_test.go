package pdf

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// wordDOCX returns a DOCX whose body is body.
func wordDOCX(t *testing.T, body string) []byte {
	t.Helper()
	w := `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`
	return zipParts(t, map[string]string{
		"[Content_Types].xml": `<Types ` + ctNS + `>` + ctXML +
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":       `<Relationships ` + relsNS + `><Relationship Id="rId1" Type="` + relDoc + `" Target="word/document.xml"/></Relationships>`,
		"word/document.xml": `<w:document ` + w + `><w:body>` + body + `</w:body></w:document>`,
	})
}

// wordODT returns a flat ODT of automatic styles auto and text body body.
func wordODT(auto, body string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><office:document ` + odfNS +
		` xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:number="urn:oasis:names:tc:opendocument:xmlns:datastyle:1.0"` +
		` office:version="1.3" office:mimetype="application/vnd.oasis.opendocument.text">` +
		`<office:automatic-styles>` + auto + `</office:automatic-styles><office:body><office:text>` + body + `</office:text></office:body></office:document>`
}

// convertedText converts c and returns the words of its first page.
func convertedText(t *testing.T, c *Conversion) string {
	t.Helper()
	ctx := context.Background()
	var out bytes.Buffer
	if _, err := c.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	r, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	got, err := r.ExtractText(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	return strings.Join(strings.Fields(got), " ")
}

// TestConvert_TrackedChangesViews checks that each view of a DOCX and an
// ODT source prints its own text: the accepted one the insertions alone,
// the rejected one the deletions alone, the marked one both.
func TestConvert_TrackedChangesViews(t *testing.T) {
	t.Parallel()
	fonts := WithConvertFontDirs(convertFontDir(t))
	docx := wordDOCX(t, `<w:p><w:r><w:t xml:space="preserve">the </w:t></w:r>`+
		`<w:ins w:id="1" w:author="Ada"><w:r><w:t xml:space="preserve">quick </w:t></w:r></w:ins>`+
		`<w:del w:id="2" w:author="Ada"><w:r><w:delText xml:space="preserve">slow </w:delText></w:r></w:del>`+
		`<w:r><w:t>fox</w:t></w:r></w:p>`)
	odt := wordODT("", `<text:tracked-changes>`+
		`<text:changed-region text:id="i"><text:insertion><office:change-info><dc:creator>Ada</dc:creator></office:change-info></text:insertion></text:changed-region>`+
		`<text:changed-region text:id="d"><text:deletion><office:change-info><dc:creator>Ada</dc:creator></office:change-info><text:p>slow </text:p></text:deletion></text:changed-region>`+
		`</text:tracked-changes><text:p>the <text:change-start text:change-id="i"/>quick <text:change-end text:change-id="i"/><text:change text:change-id="d"/>fox</text:p>`)
	for _, tc := range []struct {
		view TrackedChanges
		want string
	}{
		{TrackedChangesAccepted, "the quick fox"},
		{TrackedChangesRejected, "the slow fox"},
		{TrackedChangesMarked, "the quick slow fox"},
	} {
		if got := convertedText(t, FromDOCX(bytes.NewReader(docx), fonts, WithTrackedChanges(tc.view))); got != tc.want {
			t.Errorf("DOCX view %d: %q, want %q", tc.view, got, tc.want)
		}
		if got := convertedText(t, FromODT(strings.NewReader(odt), fonts, WithTrackedChanges(tc.view))); got != tc.want {
			t.Errorf("ODT view %d: %q, want %q", tc.view, got, tc.want)
		}
	}
	if got := convertedText(t, FromDOCX(bytes.NewReader(docx), fonts)); got != "the quick fox" {
		t.Errorf("DOCX default view: %q, want the accepted text", got)
	}
}
