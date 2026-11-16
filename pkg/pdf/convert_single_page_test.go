package pdf

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestWithSinglePage_KeepsItsHTMLCallers holds the callers of the option
// written before text documents took it: kept in an HTMLOption or a
// MarkdownOption, passed to FromHTML or FromMarkdown, it compiles and
// works as before; FromDOCX and FromODT take it too.
func TestWithSinglePage_KeepsItsHTMLCallers(t *testing.T) {
	t.Parallel()
	var html HTMLOption = WithSinglePage(true)
	var md MarkdownOption = WithSinglePage(true)
	var word WordProcessingOption = WithSinglePage(true)
	opts := []HTMLOption{WithSinglePage(true), html}
	fonts := WithConvertFontDirs(convertFontDir(t))
	ctx := context.Background()
	body := strings.Repeat("<p style='break-after:page'>x</p>", 3)
	page := `<w:p><w:r><w:t>x</w:t></w:r><w:r><w:br w:type="page"/></w:r><w:r><w:t>y</w:t></w:r></w:p>`
	for name, c := range map[string]*Conversion{
		"HTML":     FromHTML(strings.NewReader(body), fonts, opts[0], opts[1]),
		"Markdown": FromMarkdown(strings.NewReader("# a\n\nb"), fonts, md),
		"DOCX":     FromDOCX(bytes.NewReader(wordDOCX(t, page)), fonts, word),
		"ODT":      FromODT(strings.NewReader(wordODT(`<style:style style:name="B" style:family="paragraph"><style:paragraph-properties fo:break-before="page"/></style:style>`, `<text:p>x</text:p><text:p text:style-name="B">y</text:p>`)), fonts, WithSinglePage(true)),
	} {
		var out bytes.Buffer
		if _, err := c.WriteTo(ctx, &out); err != nil {
			t.Fatalf("%s: WriteTo: %v", name, err)
		}
		r, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
		if err != nil {
			t.Fatalf("%s: Open: %v", name, err)
		}
		n, err := r.PageCount(ctx)
		r.Close()
		if err != nil || n != 1 {
			t.Errorf("%s: %d pages, %v; want one", name, n, err)
		}
	}
}

// TestWithSinglePage_TextDocumentsOnOnePage holds the page of a DOCX and
// an ODT source: one page as wide as the first section's, holding the
// text of every page in order.
func TestWithSinglePage_TextDocumentsOnOnePage(t *testing.T) {
	t.Parallel()
	fonts := WithConvertFontDirs(convertFontDir(t))
	ctx := context.Background()
	var docx, odt strings.Builder
	for _, w := range []string{"alpha", "beta", "gamma", "delta"} {
		docx.WriteString(`<w:p><w:pPr><w:pageBreakBefore/></w:pPr><w:r><w:t>` + w + `</w:t></w:r></w:p>`)
		odt.WriteString(`<text:p text:style-name="B">` + w + `</text:p>`)
	}
	auto := `<style:style style:name="B" style:family="paragraph"><style:paragraph-properties fo:break-before="page"/></style:style>`
	for name, src := range map[string]func(...WordProcessingOption) *Conversion{
		"DOCX": func(o ...WordProcessingOption) *Conversion {
			return FromDOCX(bytes.NewReader(wordDOCX(t, docx.String())), append(o, fonts)...)
		},
		"ODT": func(o ...WordProcessingOption) *Conversion {
			return FromODT(strings.NewReader(wordODT(auto, odt.String())), append(o, fonts)...)
		},
	} {
		var out bytes.Buffer
		if _, err := src(WithSinglePage(true)).WriteTo(ctx, &out); err != nil {
			t.Fatalf("%s: WriteTo: %v", name, err)
		}
		r, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
		if err != nil {
			t.Fatalf("%s: Open: %v", name, err)
		}
		n, _ := r.PageCount(ctx)
		pg, _ := r.Page(ctx, 1)
		text, _ := r.ExtractText(ctx, 1)
		r.Close()
		if n != 1 || strings.Join(strings.Fields(text), " ") != "alpha beta gamma delta" {
			t.Errorf("%s: %d pages, the first reading %q", name, n, text)
		}
		if pg.Height >= pg.Width*1.414 {
			t.Errorf("%s: page %vx%v, want shorter than its paper", name, pg.Width, pg.Height)
		}
	}
}
