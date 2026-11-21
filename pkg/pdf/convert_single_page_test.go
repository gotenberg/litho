package pdf

import (
	"bytes"
	"context"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
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

// TestWithSinglePage_TallPageIsWhole holds the single page rule: a page
// sized from its content passes the 14,400 points of ISO 32000-1, Annex
// C.2, an informative limit that ISO 32000-2 drops, and holds the whole
// document at every version but PDF 1.4, whose real numbers stop at
// 32,767 (PDF Reference 1.4, Appendix C): there the content past that is
// left out, and an HTML, a Markdown, a DOCX and an ODT source report it.
func TestWithSinglePage_TallPageIsWhole(t *testing.T) {
	t.Parallel()
	fonts := WithConvertFontDirs(convertFontDir(t))
	ctx := context.Background()
	var docx, odt strings.Builder
	for range 3000 {
		docx.WriteString(`<w:p><w:r><w:t>line</w:t></w:r></w:p>`)
		odt.WriteString(`<text:p>line</text:p>`)
	}
	docx.WriteString(`<w:p><w:r><w:t>theend</w:t></w:r></w:p>`)
	odt.WriteString(`<text:p>theend</text:p>`)
	sources := map[string]func(...SinglePageOption) *Conversion{
		"HTML": func(o ...SinglePageOption) *Conversion {
			opts := []HTMLOption{fonts}
			for _, x := range o {
				opts = append(opts, x)
			}
			return FromHTML(strings.NewReader(`<div style="height:40000pt"></div><p>theend</p>`), opts...)
		},
		"Markdown": func(o ...SinglePageOption) *Conversion {
			opts := []MarkdownOption{fonts}
			for _, x := range o {
				opts = append(opts, x)
			}
			return FromMarkdown(strings.NewReader(strings.Repeat("line\n\n", 1800)+"theend\n"), opts...)
		},
		"DOCX": func(o ...SinglePageOption) *Conversion {
			opts := []WordProcessingOption{fonts}
			for _, x := range o {
				opts = append(opts, x)
			}
			return FromDOCX(bytes.NewReader(wordDOCX(t, docx.String())), opts...)
		},
		"ODT": func(o ...SinglePageOption) *Conversion {
			opts := []WordProcessingOption{fonts}
			for _, x := range o {
				opts = append(opts, x)
			}
			return FromODT(strings.NewReader(wordODT("", odt.String())), opts...)
		},
	}
	for name, src := range sources {
		for _, version := range []string{"1.7", "1.4"} {
			var out bytes.Buffer
			report, err := src(WithSinglePage(true), WithConvertVersion(version)).WriteTo(ctx, &out)
			if err != nil {
				t.Fatalf("%s at %s: WriteTo: %v", name, version, err)
			}
			r, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
			if err != nil {
				t.Fatalf("%s at %s: Open: %v", name, version, err)
			}
			n, _ := r.PageCount(ctx)
			pg, _ := r.Page(ctx, 1)
			text, _ := r.ExtractText(ctx, 1)
			r.Close()
			cut := false
			for _, w := range report.Warnings {
				cut = cut || w.Code == models.WarningLimitReached && w.Effect == models.EffectOmitted
			}
			whole := strings.Contains(text, "theend")
			switch {
			case n != 1 || pg.UserUnit != 1:
				t.Errorf("%s at %s: %d pages, user unit %v; want one page in points", name, version, n, pg.UserUnit)
			case version == "1.7" && (pg.Height <= 30000 || !whole || cut):
				t.Errorf("%s at 1.7: page %v tall, whole %v, cut reported %v; want the whole document past 30,000 points", name, pg.Height, whole, cut)
			case version == "1.4" && (pg.Height > 32767 || pg.Height < 32767-100 || whole || !cut):
				t.Errorf("%s at 1.4: page %v tall, whole %v, cut reported %v; want about 32,767 points and the cut reported", name, pg.Height, whole, cut)
			}
		}
	}
}

// TestWithSinglePage_TallPageUnderTheProfiles holds what each profile makes
// of a single page past 14,400 points. PDF/A-2 and PDF/A-3 bound a page
// boundary at 14,400 units (ISO 19005-2, 6.1.13): their remediation writes
// the page with a /UserUnit of 2, its boxes and its link halved, at the
// size it had. PDF/A-1, PDF/A-4 and PDF/UA-1 set no such bound and keep
// the page as the conversion wrote it. Each conforms, its text whole.
func TestWithSinglePage_TallPageUnderTheProfiles(t *testing.T) {
	t.Parallel()
	fonts := WithConvertFontDirs(convertFontDir(t))
	ctx := context.Background()
	src := `<html lang="en"><head><title>Tall</title></head><body><p><a href="#end">to the end</a></p><div style="height:20000pt"></div><h1 id="end">theend</h1></body></html>`
	for _, tc := range []struct {
		name    string
		version string
		unit    float64
		apply   func(context.Context, *Writer) (*models.RemediationReport, error)
	}{
		{"PDF/A-1b", "1.4", 1, func(ctx context.Context, w *Writer) (*models.RemediationReport, error) { return w.RemediatePDFA(ctx, models.PDFA1b) }},
		{"PDF/A-2b", "1.7", 2, func(ctx context.Context, w *Writer) (*models.RemediationReport, error) { return w.RemediatePDFA(ctx, models.PDFA2b) }},
		{"PDF/A-3b", "1.7", 2, func(ctx context.Context, w *Writer) (*models.RemediationReport, error) { return w.RemediatePDFA(ctx, models.PDFA3b) }},
		{"PDF/A-4", "2.0", 1, func(ctx context.Context, w *Writer) (*models.RemediationReport, error) { return w.RemediatePDFA(ctx, models.PDFA4) }},
		{"PDF/UA-1", "1.7", 1, func(ctx context.Context, w *Writer) (*models.RemediationReport, error) { return w.RemediatePDFUA(ctx, models.PDFUA1) }},
	} {
		var base, out bytes.Buffer
		if _, err := FromHTML(strings.NewReader(src), fonts, WithSinglePage(true), WithConvertVersion(tc.version)).WriteTo(ctx, &base); err != nil {
			t.Fatalf("%s: converting: %v", tc.name, err)
		}
		var report *models.RemediationReport
		_, err := NewPipeline().Convert(FromHTML(strings.NewReader(src), fonts, WithSinglePage(true), WithConvertVersion(tc.version))).Apply(func(ctx context.Context, w *Writer) error {
			var err error
			report, err = tc.apply(ctx, w)
			return err
		}).WriteTo(ctx, &out, WithFullRewrite(), WithVersion(tc.version))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if len(report.Residual) > 0 {
			t.Errorf("%s: residual %v", tc.name, report.Residual)
		}
		page := func(data []byte) (models.Page, string, [4]float64) {
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("%s: Open: %v", tc.name, err)
			}
			defer r.Close()
			pg, _ := r.Page(ctx, 1)
			text, _ := r.ExtractText(ctx, 1)
			annots, _ := r.ExtractAnnotations(ctx, 1)
			var link [4]float64
			for _, a := range annots {
				if a.Subtype == "Link" {
					for i, v := range a.Rect {
						link[i] = v * pg.UserUnit
					}
				}
			}
			return pg, text, link
		}
		bp, _, blink := page(base.Bytes())
		rp, text, rlink := page(out.Bytes())
		if rp.UserUnit != tc.unit || math.Abs(rp.Width-bp.Width) > 0.01 || math.Abs(rp.Height-bp.Height) > 0.01 || bp.Height <= 20000 {
			t.Errorf("%s: page %vx%v at user unit %v, want %vx%v at %v", tc.name, rp.Width, rp.Height, rp.UserUnit, bp.Width, bp.Height, tc.unit)
		}
		if !strings.Contains(text, "theend") {
			t.Errorf("%s: the end of the page is missing", tc.name)
		}
		for i := range rlink {
			if math.Abs(rlink[i]-blink[i]) > 0.01 {
				t.Errorf("%s: link at %v, want %v", tc.name, rlink, blink)
				break
			}
		}
	}
}
