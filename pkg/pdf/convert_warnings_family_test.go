package pdf

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// TestConvertWarnings_Families converts, through the public API, one source
// for each family of codes and checks that the conversion reports the code
// with a message naming the construct.
func TestConvertWarnings_Families(t *testing.T) {
	t.Parallel()

	dir := convertFontDir(t)
	fonts := WithConvertFontDirs(dir)
	html := func(src string, opts ...HTMLOption) func() *Conversion {
		return func() *Conversion {
			base := []HTMLOption{fonts, WithResources(fstest.MapFS{"bad.png": {Data: []byte("\x89PNG\r\n\x1a\nbroken")}})}
			return FromHTML(strings.NewReader(src), append(base, opts...)...)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gone", http.StatusGone)
	}))
	t.Cleanup(server.Close)

	wmf := base64.StdEncoding.EncodeToString([]byte("\xd7\xcd\xc6\x9a0000000000000000"))
	frame := func(image string) string {
		return `<text:p><draw:frame text:anchor-type="as-char" svg:width="1cm" svg:height="1cm">` + image + `</draw:frame></text:p>`
	}
	odt := func(body string) func() *Conversion {
		return func() *Conversion { return FromODT(strings.NewReader(wordODT("", body)), fonts) }
	}

	for _, tc := range []struct {
		family string
		conv   func() *Conversion
		code   ConvertWarningCode
		about  string
	}{
		{"text and fonts", func() *Conversion { return FromText(strings.NewReader("café"), fonts) }, WarningMissingGlyph, "U+00E9"},
		{"text and fonts", html(`<style>@font-face { font-family: Gone; src: url(gone.woff) } p { font-family: Gone }</style><p>x`), WarningFontNotLoaded, "Gone"},
		{"style sheets", html(`<style>p { frobnicate: 1 }</style><p>x`), WarningCSSUnsupported, "frobnicate"},
		{"style sheets", html(`<style>p { width: red }</style><p>x`), WarningCSSInvalid, "width"},
		{"resources", func() *Conversion { return FromHTML(strings.NewReader(`<img src="a.png" alt=a>`), fonts) }, WarningResourceRefused, "a.png"},
		{"resources", html(`<img src="https://example.com/a.png" alt=a>`), WarningResourceNoNetwork, "example.com"},
		{"resources", html(`<img src="`+server.URL+`/a.png" alt=a>`, WithHTTPClient(server.Client())), WarningResourceHTTPStatus, "410"},
		{"resources", html(`<img src="gone.png" alt=a>`), WarningResourceFailed, "gone.png"},
		{"pictures", html(`<img src="bad.png" alt=a>`), WarningPictureUnreadable, "bad.png"},
		{"pictures", odt(frame(`<draw:image><office:binary-data>` + wmf + `</office:binary-data></draw:image>`)), WarningPictureFormatUnsupported, "WMF"},
		{"pictures", odt(frame(`<draw:image xlink:href="Pictures/gone.png" xmlns:xlink="http://www.w3.org/1999/xlink"/>`)), WarningPictureMissing, "Pictures/gone.png"},
		{"pictures", odt(frame(`<draw:image><office:binary-data>!!!</office:binary-data></draw:image>`)), WarningPictureUnreadable, "base64"},
		{"charts", func() *Conversion { return FromODS(strings.NewReader(chartODS), fonts) }, WarningChartNoCache, "no values"},
		{"packages", func() *Conversion { return FromXLSX(bytes.NewReader(brokenSheetXLSX(t)), fonts) }, WarningPartMalformed, "does not parse"},
		{"forms", html(`<p><input value=x>`), WarningFormWidgetOmitted, "form controls"},
		{"links", html(`<p><a href="#nowhere">x</a>`), WarningLinkUnresolved, "nowhere"},
		{"value formats", func() *Conversion { return FromXLSX(bytes.NewReader(brokenSheetXLSX(t)), fonts) }, WarningValueFormatUnsupported, "[Bogus]"},
		{"page setup", html(`<style>@page { size: 20000in }</style><p>x`), WarningPageSetupUnsupported, "@page size"},
		{"other content", html(`<p>a<iframe src="x.html">i</iframe>`), WarningContentUnsupported, "<iframe>"},
		{"limits", func() *Conversion { return FromText(strings.NewReader(strings.Repeat("a", 1<<16+1)), fonts) }, WarningLimitReached, ""},
	} {
		var out bytes.Buffer
		rep, err := tc.conv().WriteTo(context.Background(), &out)
		if err != nil {
			t.Errorf("%s, %s: %v", tc.family, tc.code, err)
			continue
		}
		found := false
		for _, w := range rep.Warnings {
			found = found || w.Code == tc.code && strings.Contains(w.Message, tc.about)
			if w.Effect != EffectOmitted && w.Effect != EffectApproximated {
				t.Errorf("%s: the %s warning %q has the effect %q", tc.family, w.Code, w.Message, w.Effect)
			}
		}
		if !found {
			t.Errorf("%s: no %s warning about %q in %+v", tc.family, tc.code, tc.about, rep.Warnings)
		}
	}
}

// TestConvertWarnings_EffectTellsLossFromApproximation converts two HTML
// documents whose warnings share a code: one whose picture does not load,
// which loses content, and one whose stylesheet does not load, which
// prints all its content without the styles. The effect alone tells them
// apart.
func TestConvertWarnings_EffectTellsLossFromApproximation(t *testing.T) {
	t.Parallel()

	fonts := WithConvertFontDirs(convertFontDir(t))
	effect := func(src string) ConvertWarningEffect {
		t.Helper()
		var out bytes.Buffer
		rep, err := FromHTML(strings.NewReader(src), fonts, WithResources(fstest.MapFS{})).WriteTo(context.Background(), &out)
		if err != nil {
			t.Fatal(err)
		}
		if len(rep.Warnings) != 1 || rep.Warnings[0].Code != WarningResourceFailed {
			t.Fatalf("want one %s warning, got %+v", WarningResourceFailed, rep.Warnings)
		}
		return rep.Warnings[0].Effect
	}
	if got := effect(`<p>a<img src="gone.png" alt="">`); got != EffectOmitted {
		t.Errorf("a picture left out: effect %q, want %q", got, EffectOmitted)
	}
	if got := effect(`<link rel="stylesheet" href="gone.css"><p>a`); got != EffectApproximated {
		t.Errorf("a stylesheet left out: effect %q, want %q", got, EffectApproximated)
	}
}

// chartODS is a flat ODS whose chart has a series over a sheet the
// document does not hold, so neither the chart nor the cells give its
// values.
var chartODS = flatODF("spreadsheet", "", "", `<office:spreadsheet><table:table table:name="S"><table:shapes>`+
	`<draw:frame svg:x="0cm" svg:y="0cm" svg:width="8cm" svg:height="5cm"><draw:object>`+
	`<office:document office:mimetype="application/vnd.oasis.opendocument.chart" xmlns:chart="urn:oasis:names:tc:opendocument:xmlns:chart:1.0">`+
	`<office:body><office:chart><chart:chart chart:class="chart:bar"><chart:plot-area>`+
	`<chart:series chart:values-cell-range-address="Nowhere.A1:Nowhere.A2"/></chart:plot-area></chart:chart></office:chart></office:body></office:document>`+
	`</draw:object></draw:frame></table:shapes><table:table-row><table:table-cell office:value-type="float" office:value="1"><text:p>1</text:p></table:table-cell></table:table-row>`+
	`</table:table></office:spreadsheet>`)

// brokenSheetXLSX returns a workbook of two sheets: the first holds a cell
// of a number format no reader knows, the second's part does not parse.
func brokenSheetXLSX(t *testing.T) []byte {
	t.Helper()
	const ns = `xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`
	return zipParts(t, map[string]string{
		"[Content_Types].xml": `<Types ` + ctNS + `>` + ctXML +
			`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/></Types>`,
		"_rels/.rels":     `<Relationships ` + relsNS + `><Relationship Id="rId1" Type="` + relDoc + `" Target="xl/workbook.xml"/></Relationships>`,
		"xl/workbook.xml": `<workbook ` + ns + `><sheets><sheet name="Good" sheetId="1" r:id="rId1"/><sheet name="Broken" sheetId="2" r:id="rId2"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships ` + relsNS + `>` +
			`<Relationship Id="rId1" Type="` + relR + `worksheet" Target="worksheets/sheet1.xml"/>` +
			`<Relationship Id="rId2" Type="` + relR + `worksheet" Target="worksheets/sheet2.xml"/>` +
			`<Relationship Id="rId3" Type="` + relR + `styles" Target="styles.xml"/></Relationships>`,
		"xl/styles.xml": `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
			`<numFmts count="1"><numFmt numFmtId="164" formatCode="[Bogus]0"/></numFmts>` +
			`<cellXfs count="2"><xf numFmtId="0"/><xf numFmtId="164" applyNumberFormat="1"/></cellXfs></styleSheet>`,
		"xl/worksheets/sheet1.xml": `<worksheet ` + ns + `><sheetData><row r="1"><c r="A1" s="1"><v>1</v></c></row></sheetData></worksheet>`,
		"xl/worksheets/sheet2.xml": `<worksheet ` + ns + `><sheetData><row`,
	})
}
