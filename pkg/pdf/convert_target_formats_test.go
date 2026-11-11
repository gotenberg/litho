package pdf

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// zipParts returns a ZIP package of parts, written in name order, the
// mimetype first and stored when it is one of them, as ODF 1.3 Part 2, 3.3
// requires.
func zipParts(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	names := make([]string, 0, len(parts))
	for name := range parts {
		names = append(names, name)
	}
	slices.Sort(names)
	if i := slices.Index(names, "mimetype"); i > 0 {
		names = append([]string{"mimetype"}, slices.Delete(names, i, i+1)...)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range names {
		method := zip.Deflate
		if name == "mimetype" {
			method = zip.Store
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(parts[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// The OOXML namespaces and relationship types the test packages use.
const (
	ctNS   = `xmlns="http://schemas.openxmlformats.org/package/2006/content-types"`
	relsNS = `xmlns="http://schemas.openxmlformats.org/package/2006/relationships"`
	relDoc = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"
	relR   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
	ctXML  = `<Default Extension="xml" ContentType="application/xml"/>` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`
)

// testDOCX returns a DOCX of two paragraphs.
func testDOCX(t *testing.T) []byte {
	t.Helper()
	w := `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`
	return zipParts(t, map[string]string{
		"[Content_Types].xml": `<Types ` + ctNS + `>` + ctXML +
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels": `<Relationships ` + relsNS + `><Relationship Id="rId1" Type="` + relDoc + `" Target="word/document.xml"/></Relationships>`,
		"word/document.xml": `<w:document ` + w + `><w:body>` +
			`<w:p><w:r><w:t>Quarterly report</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>Sales rose in every region.</w:t></w:r></w:p>` +
			`</w:body></w:document>`,
	})
}

// testXLSX returns an XLSX of one sheet holding two cells.
func testXLSX(t *testing.T) []byte {
	t.Helper()
	ns := `xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`
	return zipParts(t, map[string]string{
		"[Content_Types].xml": `<Types ` + ctNS + `>` + ctXML +
			`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
			`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`,
		"_rels/.rels":                `<Relationships ` + relsNS + `><Relationship Id="rId1" Type="` + relDoc + `" Target="xl/workbook.xml"/></Relationships>`,
		"xl/workbook.xml":            `<workbook ` + ns + `><sheets><sheet name="Sales" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships ` + relsNS + `><Relationship Id="rId1" Type="` + relR + `worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml": `<worksheet ` + ns + `><sheetData>` +
			`<row r="1"><c r="A1" t="inlineStr"><is><t>Region</t></is></c><c r="B1" t="inlineStr"><is><t>Total</t></is></c></row>` +
			`<row r="2"><c r="A2" t="inlineStr"><is><t>North</t></is></c><c r="B2"><v>42</v></c></row>` +
			`</sheetData></worksheet>`,
	})
}

// testPPTX returns a PPTX of one slide holding one text box.
func testPPTX(t *testing.T) []byte {
	t.Helper()
	ns := `xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"`
	group := `<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/>`
	box := `<p:sp><p:nvSpPr><p:cNvPr id="2" name="Text 1"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr>` +
		`<p:spPr><a:xfrm><a:off x="457200" y="457200"/><a:ext cx="8229600" cy="1143000"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr>` +
		`<p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:r><a:rPr sz="2400"/><a:t>Roadmap</a:t></a:r></a:p></p:txBody></p:sp>`
	clrMap := `<p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/>`
	theme := `<a:theme ` + ns + ` name="T"><a:themeElements><a:clrScheme name="T">` +
		`<a:dk1><a:srgbClr val="000000"/></a:dk1><a:lt1><a:srgbClr val="FFFFFF"/></a:lt1><a:dk2><a:srgbClr val="1F497D"/></a:dk2><a:lt2><a:srgbClr val="EEECE1"/></a:lt2>` +
		`<a:accent1><a:srgbClr val="4F81BD"/></a:accent1><a:accent2><a:srgbClr val="C0504D"/></a:accent2><a:accent3><a:srgbClr val="9BBB59"/></a:accent3>` +
		`<a:accent4><a:srgbClr val="8064A2"/></a:accent4><a:accent5><a:srgbClr val="4BACC6"/></a:accent5><a:accent6><a:srgbClr val="F79646"/></a:accent6>` +
		`<a:hlink><a:srgbClr val="0000FF"/></a:hlink><a:folHlink><a:srgbClr val="800080"/></a:folHlink></a:clrScheme>` +
		`<a:fontScheme name="T"><a:majorFont><a:latin typeface="Convert Serif"/><a:ea typeface=""/><a:cs typeface=""/></a:majorFont>` +
		`<a:minorFont><a:latin typeface="Convert Serif"/><a:ea typeface=""/><a:cs typeface=""/></a:minorFont></a:fontScheme>` +
		`<a:fmtScheme name="T"><a:fillStyleLst/><a:lnStyleLst/><a:effectStyleLst/><a:bgFillStyleLst/></a:fmtScheme></a:themeElements></a:theme>`
	return zipParts(t, map[string]string{
		"[Content_Types].xml": `<Types ` + ctNS + `>` + ctXML +
			`<Override PartName="/ppt/presentation.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"/>` +
			`<Override PartName="/ppt/slides/slide1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/>` +
			`<Override PartName="/ppt/slideLayouts/slideLayout1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml"/>` +
			`<Override PartName="/ppt/slideMasters/slideMaster1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideMaster+xml"/>` +
			`<Override PartName="/ppt/theme/theme1.xml" ContentType="application/vnd.openxmlformats-officedocument.theme+xml"/></Types>`,
		"_rels/.rels": `<Relationships ` + relsNS + `><Relationship Id="rId1" Type="` + relDoc + `" Target="ppt/presentation.xml"/></Relationships>`,
		"ppt/presentation.xml": `<p:presentation ` + ns + `><p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst>` +
			`<p:sldIdLst><p:sldId id="256" r:id="rId2"/></p:sldIdLst><p:sldSz cx="9144000" cy="6858000"/><p:notesSz cx="6858000" cy="9144000"/></p:presentation>`,
		"ppt/_rels/presentation.xml.rels": `<Relationships ` + relsNS + `><Relationship Id="rId1" Type="` + relR + `slideMaster" Target="slideMasters/slideMaster1.xml"/>` +
			`<Relationship Id="rId2" Type="` + relR + `slide" Target="slides/slide1.xml"/><Relationship Id="rId3" Type="` + relR + `theme" Target="theme/theme1.xml"/></Relationships>`,
		"ppt/theme/theme1.xml": theme,
		"ppt/slideMasters/slideMaster1.xml": `<p:sldMaster ` + ns + `><p:cSld><p:spTree>` + group + `</p:spTree></p:cSld>` + clrMap +
			`<p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst></p:sldMaster>`,
		"ppt/slideMasters/_rels/slideMaster1.xml.rels": `<Relationships ` + relsNS + `><Relationship Id="rId1" Type="` + relR + `slideLayout" Target="../slideLayouts/slideLayout1.xml"/>` +
			`<Relationship Id="rId2" Type="` + relR + `theme" Target="../theme/theme1.xml"/></Relationships>`,
		"ppt/slideLayouts/slideLayout1.xml":            `<p:sldLayout ` + ns + `><p:cSld><p:spTree>` + group + `</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>`,
		"ppt/slideLayouts/_rels/slideLayout1.xml.rels": `<Relationships ` + relsNS + `><Relationship Id="rId1" Type="` + relR + `slideMaster" Target="../slideMasters/slideMaster1.xml"/></Relationships>`,
		"ppt/slides/slide1.xml":                        `<p:sld ` + ns + `><p:cSld><p:spTree>` + group + box + `</p:spTree></p:cSld></p:sld>`,
		"ppt/slides/_rels/slide1.xml.rels":             `<Relationships ` + relsNS + `><Relationship Id="rId1" Type="` + relR + `slideLayout" Target="../slideLayouts/slideLayout1.xml"/></Relationships>`,
	})
}

// targetODT returns an ODT of one paragraph.
func targetODT(t *testing.T) []byte {
	t.Helper()
	ns := `xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" xmlns:style="urn:oasis:names:tc:opendocument:xmlns:style:1.0" ` +
		`xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0" xmlns:fo="urn:oasis:names:tc:opendocument:xmlns:xsl-fo-compatible:1.0" office:version="1.3"`
	return zipParts(t, map[string]string{
		"mimetype": "application/vnd.oasis.opendocument.text",
		"META-INF/manifest.xml": `<manifest:manifest xmlns:manifest="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0" manifest:version="1.3">` +
			`<manifest:file-entry manifest:full-path="/" manifest:version="1.3" manifest:media-type="application/vnd.oasis.opendocument.text"/>` +
			`<manifest:file-entry manifest:full-path="content.xml" manifest:media-type="text/xml"/>` +
			`<manifest:file-entry manifest:full-path="styles.xml" manifest:media-type="text/xml"/></manifest:manifest>`,
		"styles.xml":  `<office:document-styles ` + ns + `><office:styles><style:style style:name="Standard" style:family="paragraph"/></office:styles></office:document-styles>`,
		"content.xml": `<office:document-content ` + ns + `><office:body><office:text><text:p text:style-name="Standard">Minutes of the meeting.</text:p></office:text></office:body></office:document-content>`,
	})
}

// testPNG returns a 2 by 2 opaque PNG as a data URL.
func testPNGDataURL(t *testing.T) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	img.Set(0, 0, color.NRGBA{R: 0x20, G: 0x60, B: 0xa0, A: 0xff})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// catalogLang returns the catalog /Lang of r, "" for none.
func catalogLang(t *testing.T, r *Reader) string {
	t.Helper()
	catalog, err := document.ResolveCatalog(context.Background(), r.doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	s, err := document.ResolveString(context.Background(), r.doc, catalog.Dict["Lang"])
	if err != nil {
		return ""
	}
	return document.DecodePdfString(s)
}

// convertTargetCase is one target of the per-format matrix: a version, and
// the profiles a later pipeline stage remediates the conversion to.
type convertTargetCase struct {
	name    string
	version string
	pdfa    models.PDFAProfile
	pdfua   models.PDFUAProfile
}

// convertTargets are the targets every input format is converted to.
var convertTargets = []convertTargetCase{
	{name: "PDF 1.4", version: "1.4"},
	{name: "PDF 2.0", version: "2.0"},
	{name: "PDF/A-1b", version: "1.4", pdfa: models.PDFA1b},
	{name: "PDF/A-2a + PDF/UA-1", version: "1.7", pdfa: models.PDFA2a, pdfua: models.PDFUA1},
	{name: "PDF/A-4 + PDF/UA-2", version: "2.0", pdfa: models.PDFA4, pdfua: models.PDFUA2},
}

// convertToTarget converts src as format at tc's version with opts and, when
// tc names a profile, remediates the result to it in a later pipeline
// stage, with lang as the language of the remediation when not empty. The
// report is nil without a profile.
func convertToTarget(t *testing.T, format testFormat, src []byte, tc convertTargetCase, lang string, opts ...ConvertOption) ([]byte, *RemediationReport) {
	t.Helper()
	ctx := context.Background()
	c := fromFormat(format, bytes.NewReader(src), append(opts, WithConvertVersion(tc.version))...)
	if tc.pdfa == "" && tc.pdfua == "" {
		var out bytes.Buffer
		if _, err := c.WriteTo(ctx, &out); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		return out.Bytes(), nil
	}
	data, rep, err := convertAndRemediate(ctx, c, tc.version, tc.pdfa, tc.pdfua, lang)
	if err != nil {
		t.Fatalf("converting and remediating: %v", err)
	}
	return data, rep
}

// checkTarget opens data and checks what tc asks of it: one revision whose
// header names the version, no catalog /Version, the language, and a file
// both litho validators pass when the report says nothing is left.
func checkTarget(t *testing.T, data []byte, rep *RemediationReport, tc convertTargetCase, lang string) {
	t.Helper()
	if got := bytes.Count(data, []byte("%%EOF")); got != 1 {
		t.Errorf("%d %%%%EOF markers, want one revision", got)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-"+tc.version+"\n")) && !bytes.HasPrefix(data, []byte("%PDF-"+tc.version+"\r")) {
		t.Errorf("header %q, want %%PDF-%s", data[:min(len(data), 9)], tc.version)
	}
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close() //nolint:errcheck // test reader
	if got := r.Version(); got != tc.version {
		t.Errorf("Reader.Version = %q, want %q: a catalog /Version overrides the header", got, tc.version)
	}
	if got := catalogLang(t, r); got != lang {
		t.Errorf("Language = %q, want %q", got, lang)
	}
	if tc.pdfa == "" && tc.pdfua == "" {
		return
	}
	if rep == nil {
		t.Fatal("remediation report = nil, want a report")
	}
	var found []error
	if tc.pdfa != "" {
		violations, err := r.ValidatePDFA(ctx, tc.pdfa)
		if err != nil {
			t.Fatalf("ValidatePDFA: %v", err)
		}
		found = append(found, violations...)
	}
	if tc.pdfua != "" {
		violations, err := r.ValidatePDFUA(ctx, tc.pdfua)
		if err != nil {
			t.Fatalf("ValidatePDFUA: %v", err)
		}
		found = append(found, violations...)
	}
	if (len(rep.Residual) == 0) != (len(found) == 0) {
		t.Errorf("Residual %v disagrees with the validators, which find %v", rep.Residual, found)
	}
}

// TestConvert_EveryFormatToEveryTarget converts a small document of each
// input format to each target, with a default language, and checks the
// version, the single revision, the language, and that the report is empty
// and agrees with the validators.
func TestConvert_EveryFormatToEveryTarget(t *testing.T) {
	dir := convertFontDir(t)
	sources := []struct {
		format testFormat
		data   []byte
	}{
		{formatHTML, []byte(`<!doctype html><title>Notes</title><h1>Notes</h1><p>One paragraph of text.</p><img src="` + testPNGDataURL(t) + `" alt="A blue square">`)},
		{formatDOCX, testDOCX(t)},
		{formatXLSX, testXLSX(t)},
		{formatPPTX, testPPTX(t)},
		{formatODT, targetODT(t)},
	}
	for _, src := range sources {
		for _, tc := range convertTargets {
			t.Run(string(src.format)+"/"+tc.name, func(t *testing.T) {
				data, rep := convertToTarget(t, src.format, src.data, tc, "en-GB", WithConvertFontDirs(dir), WithDefaultLanguage("en-GB"))
				checkTarget(t, data, rep, tc, "en-GB")
				if rep != nil && len(rep.Residual) > 0 {
					t.Errorf("Residual = %v, want none", rep.Residual)
				}
			})
		}
	}
}

// TestConvert_ReportsTheMissingAltText checks that a picture the source
// gives no text for leaves the remediated file with a residual naming the
// missing alternate text, at both PDF/UA parts, and that the file is
// written all the same.
func TestConvert_ReportsTheMissingAltText(t *testing.T) {
	dir := convertFontDir(t)
	src := `<!doctype html><html lang="en"><title>Notes</title><p>Text.</p><img src="` + testPNGDataURL(t) + `"></html>`
	for _, tc := range convertTargets[3:] {
		t.Run(tc.name, func(t *testing.T) {
			data, rep := convertToTarget(t, formatHTML, []byte(src), tc, "", WithConvertFontDirs(dir))
			checkTarget(t, data, rep, tc, "en")
			alt := false
			for _, v := range rep.ResidualViolations() {
				if strings.Contains(strings.ToLower(v.Description), "alt") {
					alt = true
				}
			}
			if !alt {
				t.Errorf("Residual = %v, want the missing alternate text", rep.Residual)
			}
		})
	}
}

// TestConvert_DefaultLanguageNeverReplacesTheSources checks that the
// default language fills a source that names none and leaves one that does.
func TestConvert_DefaultLanguageNeverReplacesTheSources(t *testing.T) {
	dir := convertFontDir(t)
	for src, want := range map[string]string{
		`<!doctype html><p>Text.</p>`:                 `fr-CA`,
		`<!doctype html><html lang="de"><p>Text.</p>`: `de`,
	} {
		var out bytes.Buffer
		if _, err := FromHTML(strings.NewReader(src), WithConvertFontDirs(dir), WithDefaultLanguage("fr-CA")).WriteTo(context.Background(), &out); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		r, err := Open(context.Background(), bytes.NewReader(out.Bytes()), int64(out.Len()))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if got := catalogLang(t, r); got != want {
			t.Errorf("%s: Language = %q, want %q", src, got, want)
		}
		r.Close() //nolint:errcheck,gosec // test reader
	}
	for _, lang := range []string{"en_US", " en", "en-", "123", "en--US"} {
		var out bytes.Buffer
		_, err := FromText(strings.NewReader("A"), WithConvertFontDirs(dir), WithDefaultLanguage(lang)).WriteTo(context.Background(), &out)
		if err == nil || !strings.Contains(err.Error(), "language") {
			t.Errorf("WithDefaultLanguage(%q): error = %v, want a refused language", lang, err)
		}
	}
}
