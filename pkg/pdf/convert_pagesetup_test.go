package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

// convertSizes converts src as format with the test face and opts and
// returns the report and each page's size.
func convertSizes(t *testing.T, format testFormat, src string, opts ...ConvertOption) (*ConvertReport, [][2]float64) {
	t.Helper()
	return conversionSizes(t, fromFormat(format, strings.NewReader(src), append([]ConvertOption{WithConvertFontDirs(convertFontDir(t))}, opts...)...))
}

// conversionSizes runs c and returns the report and each page's size.
func conversionSizes(t *testing.T, c *Conversion) (*ConvertReport, [][2]float64) {
	t.Helper()
	ctx := context.Background()
	var out bytes.Buffer
	rep, err := c.WriteTo(ctx, &out)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
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
// names no size takes WithDefaultPaper, else A4, in every format.
func TestConvert_DefaultPaper(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		format testFormat
		src    string
	}{
		{"text", formatText, "A line"},
		{"html", formatHTML, "<p>A line</p>"},
		{"markdown", formatMarkdown, "A line"},
		{"odt", formatODT, testODT("", "")},
		{"ods", formatODS, testODS("", "", "", 2)},
	} {
		_, sizes := convertSizes(t, tc.format, tc.src)
		checkSizes(t, tc.name, sizes, 0, a4W, a4H)
		_, sizes = convertSizes(t, tc.format, tc.src, WithDefaultPaper(PaperLetter))
		checkSizes(t, tc.name+" Letter default", sizes, 0, letterW, letterH)
		// A size the source names wins over the default.
		_, sizes = convertSizes(t, tc.format, tc.src, WithDefaultPaper(PaperLetter), WithPaper(Paper{Width: 400, Height: 500}))
		checkSizes(t, tc.name+" paper", sizes, 0, 400, 500)
	}
	_, sizes := convertSizes(t, formatODT, testODT("148mm", "210mm"), WithDefaultPaper(PaperLetter))
	checkSizes(t, "odt A5", sizes, 1, 419.528, 595.276)
}

// TestConvert_HTMLPageSizeWithoutPaper checks rules 1 and 3 for HTML: the
// @page size is the page without a caller's paper, landscape included as
// Chromium decides, and the default paper only stands in for none.
func TestConvert_HTMLPageSizeWithoutPaper(t *testing.T) {
	t.Parallel()
	src := `<style>@page { size: A5 }</style><p>x`
	_, sizes := convertSizes(t, formatHTML, src)
	checkSizes(t, "A5", sizes, 1, 419.528, 595.276)
	_, sizes = convertSizes(t, formatHTML, src, WithOrientation(OrientationLandscape))
	checkSizes(t, "A5 landscape asked", sizes, 1, 419.528, 595.276)
	_, sizes = convertSizes(t, formatHTML, src, WithPaper(PaperLetter))
	checkSizes(t, "A5 on Letter", sizes, 1, letterW, letterH)
	_, sizes = convertSizes(t, formatHTML, src, WithDefaultPaper(PaperLetter))
	checkSizes(t, "A5 over a Letter default", sizes, 1, 419.528, 595.276)
	_, sizes = convertSizes(t, formatHTML, `<p>x`, WithOrientation(OrientationLandscape))
	checkSizes(t, "default turned", sizes, 1, a4H, a4W)
}

// TestConvert_PaperReplacesOrientation checks rule 2: a caller's paper
// replaces a section's orientation with its own, and
// OrientationLandscape turns every portrait page, text included.
func TestConvert_PaperReplacesOrientation(t *testing.T) {
	t.Parallel()
	land := testODT("11in", "8.5in")
	_, sizes := convertSizes(t, formatODT, land)
	checkSizes(t, "own", sizes, 1, letterH, letterW)
	_, sizes = convertSizes(t, formatODT, land, WithPaper(PaperLetter))
	checkSizes(t, "Letter", sizes, 1, letterW, letterH)
	_, sizes = convertSizes(t, formatODT, land, WithPaper(PaperLetter), WithOrientation(OrientationLandscape))
	checkSizes(t, "Letter landscape", sizes, 1, letterH, letterW)
	_, sizes = convertSizes(t, formatText, "A line", WithOrientation(OrientationLandscape))
	checkSizes(t, "text landscape", sizes, 1, a4H, a4W)
	_, sizes = convertSizes(t, formatText, "A line", WithOrientation(OrientationLandscape), WithPaper(Paper{Width: letterH, Height: letterW}))
	checkSizes(t, "text landscape paper", sizes, 1, letterH, letterW)
	_, sizes = convertSizes(t, formatODS, testODS("210mm", "297mm", "", 2), WithPaper(PaperLetter), WithOrientation(OrientationLandscape))
	checkSizes(t, "sheet", sizes, 0, letterH, letterW)
}

// TestConvert_PortraitTurnsLandscapePages checks that OrientationPortrait
// turns a landscape page to portrait in every family, whatever named its
// size: a paper, a default paper, a section, a sheet's page setup or a
// slide size.
func TestConvert_PortraitTurnsLandscapePages(t *testing.T) {
	t.Parallel()
	portrait := WithOrientation(OrientationPortrait)
	landA4 := Paper{Width: a4H, Height: a4W}
	for _, tc := range []struct {
		name   string
		format testFormat
		src    string
		opts   []ConvertOption
		w, h   float64
	}{
		{"text paper", formatText, "A line", []ConvertOption{WithPaper(landA4)}, a4W, a4H},
		{"text default paper", formatText, "A line", []ConvertOption{WithDefaultPaper(landA4)}, a4W, a4H},
		{"html", formatHTML, "<p>A line</p>", []ConvertOption{WithDefaultPaper(landA4)}, a4W, a4H},
		{"markdown", formatMarkdown, "A line", []ConvertOption{WithPaper(landA4)}, a4W, a4H},
		{"odt section", formatODT, testODT("11in", "8.5in"), nil, letterW, letterH},
		{"ods page setup", formatODS, testODS("297mm", "210mm", "", 2), nil, a4W, a4H},
		{"csv default paper", formatCSV, "a,b\n1,2\n", []ConvertOption{WithDefaultPaper(landA4)}, a4W, a4H},
		{"odp slides", formatODP, testODP("28cm", "15.75cm"), nil, 446.457, 793.701},
	} {
		_, sizes := convertSizes(t, tc.format, tc.src, tc.opts...)
		if len(sizes) == 0 || sizes[0][0] <= sizes[0][1] {
			t.Errorf("%s: pages %v, want landscape ones without the option", tc.name, sizes)
		}
		_, sizes = convertSizes(t, tc.format, tc.src, append(tc.opts, portrait)...)
		checkSizes(t, tc.name, sizes, 0, tc.w, tc.h)
	}
}

// TestConvert_SheetRefitsToThePaper checks rule 4: a sheet fitted to one
// page is fitted to the caller's paper.
func TestConvert_SheetRefitsToThePaper(t *testing.T) {
	t.Parallel()
	src := testODS("210mm", "297mm", ` style:scale-to-X="1" style:scale-to-Y="1"`, 120)
	_, sizes := convertSizes(t, formatODS, src)
	checkSizes(t, "A4", sizes, 1, 595.276, 841.89)
	_, sizes = convertSizes(t, formatODS, src, WithPaper(Paper{Width: 396, Height: 612}), WithOrientation(OrientationLandscape))
	checkSizes(t, "small landscape", sizes, 1, 612, 396)
}

// TestConvert_SlidesFitThePaper checks that a slide is scaled to fit a
// caller's paper, landscape turns a portrait slide, and an ODP notes page
// prints on Letter, or on the caller's paper.
func TestConvert_SlidesFitThePaper(t *testing.T) {
	t.Parallel()
	wide := testODP("28cm", "15.75cm")
	fonts := WithConvertFontDirs(convertFontDir(t))
	_, sizes := convertSizes(t, formatODP, wide)
	checkSizes(t, "slides", sizes, 2, 793.701, 446.457)
	_, sizes = convertSizes(t, formatODP, wide, WithPaper(PaperLetter))
	checkSizes(t, "slides on Letter", sizes, 2, letterW, letterH)
	_, sizes = convertSizes(t, formatODP, testODP("19.05cm", "25.4cm"), WithOrientation(OrientationLandscape))
	checkSizes(t, "portrait slides turned", sizes, 2, 720, 540)
	_, sizes = conversionSizes(t, FromODP(strings.NewReader(wide), fonts, WithNotesPages(true)))
	checkSizes(t, "notes", sizes[2:], 2, letterW, letterH)
	_, sizes = conversionSizes(t, FromODP(strings.NewReader(wide), fonts, WithNotesPages(true), WithPaper(PaperA4)))
	checkSizes(t, "notes on A4", sizes, 4, a4W, a4H)
}

// TestConvert_PPTXNotesPagesTakeTheNotesSize checks rule 5 for PPTX: a
// notes page is the notes size the deck names, 540 by 720 points for the
// notes-pages fixture, its slides 960 by 540.
func TestConvert_PPTXNotesPagesTakeTheNotesSize(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", "test", "integration", "testdata", "litho", "pptx", "sources", "notes-pages", "notes-pages.pptx")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("notes-pages.pptx is not built: make corpus-build CORPUS=litho/pptx")
	}
	if err != nil {
		t.Fatal(err)
	}
	_, sizes := conversionSizes(t, FromPPTX(bytes.NewReader(data), WithConvertFontDirs(convertFontDir(t)), WithNotesPages(true)))
	if len(sizes) != 6 {
		t.Fatalf("%d pages, want 3 slides and 3 notes pages", len(sizes))
	}
	checkSizes(t, "slides", sizes[:3], 3, 960, 540)
	checkSizes(t, "notes", sizes[3:], 3, 540, 720)
}
