package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

// odfNS declares the namespaces of the flat OpenDocument test sources.
const odfNS = `xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" ` +
	`xmlns:style="urn:oasis:names:tc:opendocument:xmlns:style:1.0" ` +
	`xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0" ` +
	`xmlns:table="urn:oasis:names:tc:opendocument:xmlns:table:1.0" ` +
	`xmlns:draw="urn:oasis:names:tc:opendocument:xmlns:drawing:1.0" ` +
	`xmlns:presentation="urn:oasis:names:tc:opendocument:xmlns:presentation:1.0" ` +
	`xmlns:fo="urn:oasis:names:tc:opendocument:xmlns:xsl-fo-compatible:1.0" ` +
	`xmlns:svg="urn:oasis:names:tc:opendocument:xmlns:svg-compatible:1.0"`

// flatODF returns a flat OpenDocument file of mime type kind with the given
// automatic styles, master styles and body.
func flatODF(kind, auto, master, body string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><office:document ` + odfNS +
		` office:version="1.3" office:mimetype="application/vnd.oasis.opendocument.` + kind + `">` +
		`<office:automatic-styles>` + auto + `</office:automatic-styles>` +
		`<office:master-styles>` + master + `</office:master-styles>` +
		`<office:body>` + body + `</office:body></office:document>`
}

// pageLayout returns a page layout named name of w by h, "" for a side the
// layout does not name, with extra attributes.
func pageLayout(name, w, h, extra string) string {
	size := ""
	if w != "" {
		size = ` fo:page-width="` + w + `" fo:page-height="` + h + `"`
	}
	return `<style:page-layout style:name="` + name + `"><style:page-layout-properties` + size +
		` fo:margin-top="1cm" fo:margin-bottom="1cm" fo:margin-left="1cm" fo:margin-right="1cm"` + extra + `/></style:page-layout>`
}

// testODT is a text document of one section of w by h.
func testODT(w, h string) string {
	return flatODF("text", pageLayout("pm1", w, h, ""), `<style:master-page style:name="Standard" style:page-layout-name="pm1"/>`,
		`<office:text><text:p>Page setup</text:p></office:text>`)
}

// testODS is a workbook of one sheet of rows rows by 12 columns on a page
// layout of w by h with extra attributes.
func testODS(w, h, extra string, rows int) string {
	var b strings.Builder
	for r := range rows {
		b.WriteString(`<table:table-row>`)
		for c := range 12 {
			fmt.Fprintf(&b, `<table:table-cell office:value-type="string"><text:p>r%dc%d text</text:p></table:table-cell>`, r, c)
		}
		b.WriteString(`</table:table-row>`)
	}
	return flatODF("spreadsheet",
		pageLayout("pm1", w, h, extra)+`<style:style style:name="ta1" style:family="table" style:master-page-name="M1"/>`,
		`<style:master-page style:name="M1" style:page-layout-name="pm1"/>`,
		`<office:spreadsheet><table:table table:name="S" table:style-name="ta1">`+b.String()+`</table:table></office:spreadsheet>`)
}

// testODP is a deck of two slides of w by h, the first with notes.
func testODP(w, h string) string {
	notes := `<presentation:notes><draw:frame presentation:class="notes" svg:x="2cm" svg:y="15cm" svg:width="17cm" svg:height="5cm"><draw:text-box><text:p>Notes</text:p></draw:text-box></draw:frame></presentation:notes>`
	slide := func(n int, notes string) string {
		return fmt.Sprintf(`<draw:page draw:name="S%d" draw:master-page-name="Default"><draw:frame svg:x="1cm" svg:y="1cm" svg:width="10cm" svg:height="3cm"><draw:text-box><text:p>Slide %d</text:p></draw:text-box></draw:frame>%s</draw:page>`, n, n, notes)
	}
	return flatODF("presentation", pageLayout("PM1", w, h, ""),
		`<style:master-page style:name="Default" style:page-layout-name="PM1"/>`,
		`<office:presentation>`+slide(1, notes)+slide(2, "")+`</office:presentation>`)
}

// convertSizes converts src from format with the test face and opts and
// returns the report and each page's size.
func convertSizes(t *testing.T, format Format, src string, opts ...ConvertOption) (*ConvertReport, [][2]float64) {
	t.Helper()
	ctx := context.Background()
	var out bytes.Buffer
	rep, err := Convert(ctx, strings.NewReader(src), format, &out, append([]ConvertOption{WithConvertFontDirs(convertFontDir(t))}, opts...)...)
	if err != nil {
		t.Fatalf("Convert %s: %v", format, err)
	}
	r, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	n, err := r.PageCount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sizes := make([][2]float64, 0, n)
	for i := 1; i <= n; i++ {
		p, err := r.Page(ctx, i)
		if err != nil {
			t.Fatal(err)
		}
		sizes = append(sizes, [2]float64{p.Width, p.Height})
	}
	return rep, sizes
}

// checkSizes fails the test unless every page is w by h, to a hundredth of
// a point, and there are want pages, or any number for want 0.
func checkSizes(t *testing.T, name string, sizes [][2]float64, want int, w, h float64) {
	t.Helper()
	if want > 0 && len(sizes) != want {
		t.Errorf("%s: %d pages, want %d", name, len(sizes), want)
	}
	for i, s := range sizes {
		if math.Abs(s[0]-w) > 0.01 || math.Abs(s[1]-h) > 0.01 {
			t.Errorf("%s: page %d is %v by %v, want %v by %v", name, i+1, s[0], s[1], w, h)
		}
	}
}

const (
	a4W, a4H         = 595.276, 841.89
	letterW, letterH = 612.0, 792.0
)

// TestConvert_DefaultPaper checks rule 3 of the page size: a source that
// names no size takes WithDefaultPaperSize, else A4, in every format.
func TestConvert_DefaultPaper(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		format Format
		src    string
	}{
		{"text", FormatText, "A line"},
		{"html", FormatHTML, "<p>A line</p>"},
		{"markdown", FormatMarkdown, "A line"},
		{"odt", FormatODT, testODT("", "")},
		{"ods", FormatODS, testODS("", "", "", 2)},
	} {
		_, sizes := convertSizes(t, tc.format, tc.src)
		checkSizes(t, tc.name, sizes, 0, a4W, a4H)
		_, sizes = convertSizes(t, tc.format, tc.src, WithDefaultPaperSize(letterW, letterH))
		checkSizes(t, tc.name+" Letter default", sizes, 0, letterW, letterH)
		// A size the source names wins over the default.
		_, sizes = convertSizes(t, tc.format, tc.src, WithDefaultPaperSize(letterW, letterH), WithPaperSize(400, 500))
		checkSizes(t, tc.name+" paper", sizes, 0, 400, 500)
	}
	_, sizes := convertSizes(t, FormatODT, testODT("148mm", "210mm"), WithDefaultPaperSize(letterW, letterH))
	checkSizes(t, "odt A5", sizes, 1, 419.528, 595.276)
}

// TestConvert_HTMLPageSizeWithoutPaper checks rules 1 and 3 for HTML: the
// @page size is the page without a caller's paper, landscape included as
// Chromium decides, and the default paper only stands in for none.
func TestConvert_HTMLPageSizeWithoutPaper(t *testing.T) {
	t.Parallel()
	src := `<style>@page { size: A5 }</style><p>x`
	_, sizes := convertSizes(t, FormatHTML, src)
	checkSizes(t, "A5", sizes, 1, 419.528, 595.276)
	_, sizes = convertSizes(t, FormatHTML, src, WithLandscape(true))
	checkSizes(t, "A5 landscape asked", sizes, 1, 419.528, 595.276)
	_, sizes = convertSizes(t, FormatHTML, src, WithPaperSize(letterW, letterH))
	checkSizes(t, "A5 on Letter", sizes, 1, letterW, letterH)
	_, sizes = convertSizes(t, FormatHTML, src, WithPaperSize(letterW, letterH), WithPreferCSSPageSize(true))
	checkSizes(t, "A5 preferred over Letter", sizes, 1, 419.528, 595.276)
	_, sizes = convertSizes(t, FormatHTML, `<p>x`, WithLandscape(true))
	checkSizes(t, "default turned", sizes, 1, a4H, a4W)
}

// TestConvert_PaperReplacesOrientation checks rule 2: a caller's paper
// replaces a section's orientation with its own, and WithLandscape turns
// every portrait page, text included.
func TestConvert_PaperReplacesOrientation(t *testing.T) {
	t.Parallel()
	land := testODT("11in", "8.5in")
	_, sizes := convertSizes(t, FormatODT, land)
	checkSizes(t, "own", sizes, 1, letterH, letterW)
	_, sizes = convertSizes(t, FormatODT, land, WithPaperSize(letterW, letterH))
	checkSizes(t, "Letter", sizes, 1, letterW, letterH)
	_, sizes = convertSizes(t, FormatODT, land, WithPaperSize(letterW, letterH), WithLandscape(true))
	checkSizes(t, "Letter landscape", sizes, 1, letterH, letterW)
	_, sizes = convertSizes(t, FormatText, "A line", WithLandscape(true))
	checkSizes(t, "text landscape", sizes, 1, a4H, a4W)
	_, sizes = convertSizes(t, FormatText, "A line", WithLandscape(true), WithPaperSize(letterH, letterW))
	checkSizes(t, "text landscape paper", sizes, 1, letterH, letterW)
	_, sizes = convertSizes(t, FormatODS, testODS("210mm", "297mm", "", 2), WithPaperSize(letterW, letterH), WithLandscape(true))
	checkSizes(t, "sheet", sizes, 0, letterH, letterW)
}

// TestConvert_SheetRefitsToThePaper checks rule 4: a sheet fitted to one
// page is fitted to the caller's paper.
func TestConvert_SheetRefitsToThePaper(t *testing.T) {
	t.Parallel()
	src := testODS("210mm", "297mm", ` style:scale-to-X="1" style:scale-to-Y="1"`, 120)
	_, sizes := convertSizes(t, FormatODS, src)
	checkSizes(t, "A4", sizes, 1, 595.276, 841.89)
	_, sizes = convertSizes(t, FormatODS, src, WithPaperSize(396, 612), WithLandscape(true))
	checkSizes(t, "small landscape", sizes, 1, 612, 396)
}

// TestConvert_SlidesFitThePaper checks that a slide is scaled to fit a
// caller's paper, landscape turns a portrait slide, and an ODP notes page
// prints on Letter, or on the caller's paper.
func TestConvert_SlidesFitThePaper(t *testing.T) {
	t.Parallel()
	wide := testODP("28cm", "15.75cm")
	_, sizes := convertSizes(t, FormatODP, wide)
	checkSizes(t, "slides", sizes, 2, 793.701, 446.457)
	_, sizes = convertSizes(t, FormatODP, wide, WithPaperSize(letterW, letterH))
	checkSizes(t, "slides on Letter", sizes, 2, letterW, letterH)
	_, sizes = convertSizes(t, FormatODP, testODP("19.05cm", "25.4cm"), WithLandscape(true))
	checkSizes(t, "portrait slides turned", sizes, 2, 720, 540)
	_, sizes = convertSizes(t, FormatODP, wide, WithNotesPages(true))
	checkSizes(t, "notes", sizes[2:], 2, letterW, letterH)
	_, sizes = convertSizes(t, FormatODP, wide, WithNotesPages(true), WithPaperSize(a4W, a4H))
	checkSizes(t, "notes on A4", sizes, 4, a4W, a4H)
}

// TestConvert_ReportsIgnoredOptions checks rule 6: an option set to other
// than its default that the format does not take adds one option-ignored
// warning naming it; an option at its default adds none.
func TestConvert_ReportsIgnoredOptions(t *testing.T) {
	t.Parallel()
	ignored := func(rep *ConvertReport) []string {
		var names []string
		for _, w := range rep.Warnings {
			if w.Code == CodeOptionIgnored {
				names = append(names, strings.Fields(w.Message)[0])
			}
		}
		return names
	}
	for _, tc := range []struct {
		name   string
		format Format
		src    string
		opts   []ConvertOption
		want   []string
	}{
		{
			"odt", FormatODT, testODT("", ""),
			[]ConvertOption{
				WithMargins(10, 10, 10, 10), WithFontFamilies("serif"), WithFontSize(14), WithMediaType("screen"),
				WithPrintScale(1.5), WithSinglePage(true), WithPrintBackground(false), WithNotesPages(true),
				WithPrintDate(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), WithBaseURL("https://example.com/"),
				WithHeaderTemplate("<p>h</p>"), WithMediaFeatures(map[string]string{"prefers-color-scheme": "dark"}),
			},
			[]string{
				"WithMargins", "WithFontFamilies", "WithFontSize", "WithMediaType", "WithPrintScale", "WithSinglePage",
				"WithPrintBackground", "WithNotesPages", "WithHeaderTemplate", "WithPrintDate", "WithBaseURL", "WithMediaFeatures",
			},
		},
		{
			"text", FormatText, "A line",
			[]ConvertOption{WithOutline(false), WithConvertPassword("x"), WithHiddenSlides(true), WithPreferCSSPageSize(true), WithShrinkToFit(false)},
			[]string{"WithPreferCSSPageSize", "WithShrinkToFit", "WithOutline", "WithConvertPassword", "WithHiddenSlides"},
		},
		{
			"html", FormatHTML, "<p>x",
			[]ConvertOption{WithFontSize(14), WithConvertPassword("x"), WithSinglePage(true), WithPageRanges("1")},
			[]string{"WithFontSize", "WithPageRanges", "WithConvertPassword"},
		},
		{
			"ods single page", FormatODS, testODS("", "", "", 2),
			[]ConvertOption{WithSinglePage(true), WithPaperSize(letterW, letterH), WithLandscape(true), WithFractionalFontMetrics(true)},
			[]string{"WithPaperSize", "WithLandscape", "WithFractionalFontMetrics"},
		},
		{
			"odp", FormatODP, testODP("28cm", "15.75cm"),
			[]ConvertOption{WithDefaultPaperSize(letterW, letterH), WithHiddenSlides(true)},
			[]string{"WithDefaultPaperSize"},
		},
		{
			"defaults", FormatODT, testODT("", ""),
			[]ConvertOption{
				WithMargins(56.693, 56.693, 56.693, 56.693), WithMediaType("print"), WithPrintScale(1), WithPrintBackground(true),
				WithShrinkToFit(true), WithOutline(true), WithSinglePage(false), WithLandscape(false), WithFontFamilies(),
			},
			nil,
		},
	} {
		rep, _ := convertSizes(t, tc.format, tc.src, tc.opts...)
		if got := ignored(rep); strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("%s: ignored %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestConvert_ValidatesEveryOption checks that an option out of range
// fails whatever the format, with the conversion sentinel of its own.
func TestConvert_ValidatesEveryOption(t *testing.T) {
	t.Parallel()
	dir := convertFontDir(t)
	for name, opt := range map[string]ConvertOption{
		"scale":           WithPrintScale(5),
		"zero scale":      WithPrintScale(0),
		"negative margin": WithMargins(-1, 0, 0, 0),
		"no room":         WithMargins(500, 0, 500, 0),
		"zero font size":  WithFontSize(0),
		"media":           WithMediaType("tv"),
		"base URL":        WithBaseURL("relative"),
		"feature":         WithMediaFeatures(map[string]string{"Color": "x"}),
		"default paper":   WithDefaultPaperSize(-1, 5),
		"paper":           WithPaperSize(20000, 5),
	} {
		for _, format := range []Format{FormatODT, FormatText, FormatHTML, FormatPPTX} {
			var out bytes.Buffer
			_, err := Convert(context.Background(), strings.NewReader(testODT("", "")), format, &out, WithConvertFontDirs(dir), opt)
			if !errors.Is(err, ErrInvalidConvertOptions) {
				t.Errorf("%s, %s: %v, want ErrInvalidConvertOptions", name, format, err)
				continue
			}
			if strings.Contains(err.Error(), "plain text") {
				t.Errorf("%s, %s: %q names plain text", name, format, err)
			}
		}
	}
}
