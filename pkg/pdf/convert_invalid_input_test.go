package pdf

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// TestConvert_DocumentCausedErrorsAreInvalidInput locks the classification
// of what a document's bytes cause: a paragraph past the text a shaping
// call takes is [ErrConvertInputTooLarge], and a DOCX page side past the
// PDF page limit keeps the default size rather than failing.
func TestConvert_DocumentCausedErrorsAreInvalidInput(t *testing.T) {
	t.Parallel()
	dir := convertFontDir(t)

	long := "<p>" + strings.Repeat("a", 1<<20+1)
	_, err := FromHTML(strings.NewReader(long), WithConvertFontDirs(dir)).WriteTo(context.Background(), io.Discard)
	if !errors.Is(err, ErrConvertInputTooLarge) || !IsInvalidInput(err) {
		t.Errorf("a paragraph past the shaping cap: %v, want ErrConvertInputTooLarge", err)
	}

	w := `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`
	huge := zipParts(t, map[string]string{
		"[Content_Types].xml": `<Types ` + ctNS + `>` + ctXML +
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels": `<Relationships ` + relsNS + `><Relationship Id="rId1" Type="` + relDoc + `" Target="word/document.xml"/></Relationships>`,
		"word/document.xml": `<w:document ` + w + `><w:body><w:p><w:r><w:t>Huge page</w:t></w:r></w:p>` +
			`<w:sectPr><w:pgSz w:w="2147483647" w:h="2147483647"/></w:sectPr></w:body></w:document>`,
	})
	var out bytes.Buffer
	if _, err := FromDOCX(bytes.NewReader(huge), WithConvertFontDirs(dir)).WriteTo(context.Background(), &out); err != nil {
		t.Fatalf("a DOCX page past the PDF page limit: %v, want a PDF", err)
	}
	r, err := Open(context.Background(), bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	p, err := r.Page(context.Background(), 1)
	if err != nil || p.Width > 14400 || p.Height > 14400 {
		t.Errorf("page 1: %+v, %v; want a side within 14,400 points", p, err)
	}
}
