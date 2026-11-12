package main

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gotenberg/litho/test/integration/fixture"
)

// testCorpusRoot is the committed corpus directory, from this package's
// directory, where go test runs.
var testCorpusRoot = filepath.Join("..", "..", "corpus") //nolint:gochecknoglobals // a constant path

// buildPaperCorpus writes the paper documents of a build corpus into a
// temporary directory and returns it.
func buildPaperCorpus(t *testing.T, corpus string) string {
	t.Helper()
	out := t.TempDir()
	format := filepath.Base(corpus)
	n, err := buildPaperDocs(context.Background(), corpus, filepath.Join(testCorpusRoot, filepath.FromSlash(corpus), "parts"), out, format)
	if err != nil {
		t.Fatalf("buildPaperDocs(%s): %v", corpus, err)
	}
	if n != len(paperDocs(corpus)) {
		t.Fatalf("buildPaperDocs(%s) wrote %d documents, want %d", corpus, n, len(paperDocs(corpus)))
	}
	return out
}

// zipEntry returns one entry of the zip at path.
func zipEntry(t *testing.T, path, name string) string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s in %s: %v", name, path, err)
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read %s in %s: %v", name, path, err)
		}
		return string(data)
	}
	t.Fatalf("%s has no %s", path, name)
	return ""
}

// TestPaperDocsStatePageSetup checks the page setup each generated document
// states: the pgSz of each DOCX section, the paperSize code and orientation
// of each XLSX sheet, the sldSz of each PPTX deck.
func TestPaperDocsStatePageSetup(t *testing.T) {
	t.Parallel()
	cases := []struct {
		corpus, name, part string
		want               []string
	}{
		{"litho/docx", "paper-a4-portrait", docxPaperPart, []string{`<w:pgSz w:w="11906" w:h="16838"/>`}},
		{"litho/docx", "paper-a3-landscape", docxPaperPart, []string{`<w:pgSz w:w="23811" w:h="16838" w:orient="landscape"/>`}},
		{"litho/docx", "paper-a5-portrait", docxPaperPart, []string{`<w:pgSz w:w="8391" w:h="11906"/>`}},
		{"litho/docx", "paper-b5-landscape", docxPaperPart, []string{`<w:pgSz w:w="14173" w:h="9978" w:orient="landscape"/>`}},
		{"litho/docx", "paper-letter-portrait", docxPaperPart, []string{`<w:pgSz w:w="12240" w:h="15840"/>`}},
		{"litho/docx", "paper-legal-landscape", docxPaperPart, []string{`<w:pgSz w:w="20160" w:h="12240" w:orient="landscape"/>`}},
		{"litho/docx", "paper-tabloid-portrait", docxPaperPart, []string{`<w:pgSz w:w="15840" w:h="24480"/>`}},
		{"litho/docx", "paper-executive-portrait", docxPaperPart, []string{`<w:pgSz w:w="10440" w:h="15120"/>`}},
		{"litho/docx", "paper-mixed-orientation", docxPaperPart, []string{`<w:pgSz w:w="12240" w:h="15840"/>`, `<w:pgSz w:w="15840" w:h="12240" w:orient="landscape"/>`}},
		{"litho/docx", "paper-mixed-sizes", docxPaperPart, []string{`<w:pgSz w:w="11906" w:h="16838"/>`, `<w:pgSz w:w="8391" w:h="11906"/>`}},
		{"litho/docx", "paper-default-portrait", docxPaperPart, []string{`<w:footerReference w:type="default" r:id="rId7"/><w:pgMar w:top="1440"`}},
		{"litho/docx", "paper-margins-none-a5-landscape", docxPaperPart, []string{`<w:pgSz w:w="11906" w:h="8391" w:orient="landscape"/><w:pgMar w:top="0" w:right="0" w:bottom="0" w:left="0" w:header="0" w:footer="0" w:gutter="0"/>`}},
		{"litho/docx", "paper-margins-uneven-letter-portrait", docxPaperPart, []string{`<w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="360" w:bottom="1080" w:left="720" w:header="720" w:footer="360" w:gutter="0"/>`}},
		{"litho/xlsx", "paper-grid-b5-portrait", xlsxPaperPart, []string{`paperSize="13" orientation="portrait"`}},
		{"litho/xlsx", "paper-grid-tabloid-landscape", xlsxPaperPart, []string{`paperSize="3" orientation="landscape"`}},
		{"litho/xlsx", "paper-fit-a3-landscape", xlsxPaperPart, []string{`paperSize="8" orientation="landscape" fitToWidth="1" fitToHeight="0"`}},
		{"litho/xlsx", "paper-grid-default-landscape", xlsxPaperPart, []string{`<pageSetup orientation="landscape"/>`}},
		{"litho/xlsx", "paper-grid-margins-narrow-a3-portrait", xlsxPaperPart, []string{`<pageMargins left="0.5" right="0.5" top="0.5" bottom="0.5" header="0.25" footer="0.25"/>`, `<pageSetup paperSize="8" orientation="portrait"/>`}},
		{"litho/xlsx", "paper-grid-margins-uneven-letter-landscape", xlsxPaperPart, []string{`<pageMargins left="0.5" right="0.25" top="1" bottom="0.75" header="0.5" footer="0.25"/>`, `<pageSetup paperSize="1" orientation="landscape"/>`}},
		{"litho/pptx", "paper-deck-a3", pptxPaperPart, []string{`<p:sldSz cx="15120000" cy="10692000" type="A3"/>`}},
		{"litho/pptx", "paper-deck-hagaki", pptxPaperPart, []string{`<p:sldSz cx="3600000" cy="5328000" type="hagakiCard"/>`}},
		{"litho/pptx", "paper-deck-widescreen", pptxPaperPart, []string{`<p:sldSz cx="12192000" cy="6858000"/>`}},
	}
	outs := map[string]string{}
	for _, c := range cases {
		if outs[c.corpus] == "" {
			outs[c.corpus] = buildPaperCorpus(t, c.corpus)
		}
		doc := filepath.Join(outs[c.corpus], c.name, c.name+"."+filepath.Base(c.corpus))
		part := zipEntry(t, doc, c.part)
		at := 0
		for _, w := range c.want {
			i := strings.Index(part[at:], w)
			if i < 0 {
				t.Errorf("%s: %s does not state %s after offset %d", c.name, c.part, w, at)
				break
			}
			at += i + len(w)
		}
	}
}

// TestPaperDocsSectionsKeepHeaderAndFooter checks that the first section of
// a mixed document keeps the header and footer the base's section names.
func TestPaperDocsSectionsKeepHeaderAndFooter(t *testing.T) {
	t.Parallel()
	out := buildPaperCorpus(t, "litho/docx")
	for _, name := range []string{"paper-mixed-orientation", "paper-mixed-sizes"} {
		part := zipEntry(t, filepath.Join(out, name, name+".docx"), docxPaperPart)
		sects := regexp.MustCompile(`<w:sectPr>.*?</w:sectPr>`).FindAllString(part, -1)
		if len(sects) != 2 {
			t.Fatalf("%s: %d sections, want 2", name, len(sects))
		}
		for i, s := range sects {
			if !strings.Contains(s, `<w:headerReference w:type="default"`) || !strings.Contains(s, `<w:footerReference w:type="default"`) {
				t.Errorf("%s: section %d names no header or footer: %s", name, i+1, s)
			}
		}
	}
}

// TestPaperDocsOptionsBorrowTheirPaper checks that each option fixture
// names, in an options.json the fixture package reads, the paper and the
// orientation of the generated document whose reference it borrows.
func TestPaperDocsOptionsBorrowTheirPaper(t *testing.T) {
	t.Parallel()
	for _, corpus := range []string{"litho/docx", "litho/xlsx"} {
		out := buildPaperCorpus(t, corpus)
		names := map[string]bool{}
		for _, d := range paperDocs(corpus) {
			names[d.name] = true
		}
		options := 0
		for _, d := range paperDocs(corpus) {
			if d.options == nil {
				continue
			}
			options++
			src, err := fixture.LoadOffice(filepath.Join(out, d.name))
			if err != nil {
				t.Fatalf("%s: %v", d.name, err)
			}
			o := src.Options
			if !names[o.ReferenceOf] || o.ReferenceOf == d.name {
				t.Errorf("%s: reference_of %q names no other paper document", d.name, o.ReferenceOf)
			}
			if o.PaperWidth == nil || o.PaperHeight == nil {
				t.Fatalf("%s: no paper", d.name)
			}
			parts := strings.Split(o.ReferenceOf, "-")
			key, orient := parts[len(parts)-2], parts[len(parts)-1]
			p := paperByKey(key)
			if corpus == "litho/xlsx" {
				p = sheetPaper(p)
			}
			if *o.PaperWidth != inches(p.w) || *o.PaperHeight != inches(p.h) || o.Landscape != (orient == "landscape") {
				t.Errorf("%s: paper %v by %v landscape %v, want %s %s", d.name, *o.PaperWidth, *o.PaperHeight, o.Landscape, key, orient)
			}
		}
		if want := map[string]int{"litho/docx": 16, "litho/xlsx": 22}[corpus]; options != want {
			t.Errorf("%s: %d option fixtures, want %d", corpus, options, want)
		}
	}
}

// TestPaperDocsDeterministic checks that two builds write the same bytes.
func TestPaperDocsDeterministic(t *testing.T) {
	t.Parallel()
	for _, corpus := range []string{"litho/docx", "litho/xlsx", "litho/pptx"} {
		a, b := buildPaperCorpus(t, corpus), buildPaperCorpus(t, corpus)
		for _, name := range paperDocNames(corpus) {
			p := filepath.Join(name, name+"."+filepath.Base(corpus))
			da, err := os.ReadFile(filepath.Join(a, p))
			if err != nil {
				t.Fatal(err)
			}
			db, err := os.ReadFile(filepath.Join(b, p))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(da, db) {
				t.Errorf("%s: two builds differ", name)
			}
		}
	}
}

// TestPaperDocsNamesAreUnique checks that no paper document takes the name
// of a committed fixture or of another paper document of its corpus.
func TestPaperDocsNamesAreUnique(t *testing.T) {
	t.Parallel()
	for _, corpus := range []string{"litho/docx", "litho/xlsx", "litho/pptx", "litho/odt", "litho/ods", "litho/odp", "litho/html", "litho/csv"} {
		seen := map[string]bool{}
		dir := "parts"
		if !buildFormats[filepath.Base(corpus)] {
			dir = "sources"
		}
		entries, err := os.ReadDir(filepath.Join(testCorpusRoot, filepath.FromSlash(corpus), dir))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			seen[e.Name()] = true
		}
		for _, name := range paperDocNames(corpus) {
			if seen[name] {
				t.Errorf("%s: %s is named twice", corpus, name)
			}
			seen[name] = true
		}
	}
}

// buildFlatPaperCorpus writes the paper documents of a corpus with
// committed sources into a temporary directory and returns it.
func buildFlatPaperCorpus(t *testing.T, corpus string) string {
	t.Helper()
	out := t.TempDir()
	n, err := writeFlatPaperDocs(context.Background(), corpus, filepath.Join(testCorpusRoot, filepath.FromSlash(corpus), "sources"), out)
	if err != nil {
		t.Fatalf("writeFlatPaperDocs(%s): %v", corpus, err)
	}
	if n != len(paperDocs(corpus)) {
		t.Fatalf("writeFlatPaperDocs(%s) wrote %d documents, want %d", corpus, n, len(paperDocs(corpus)))
	}
	return out
}

// TestFlatPaperDocsStatePageSetup checks the page setup each OpenDocument
// and HTML paper document states, and the options it converts with.
func TestFlatPaperDocsStatePageSetup(t *testing.T) {
	t.Parallel()
	cases := []struct {
		corpus, name, file string
		want               []string
	}{
		{"litho/odt", "paper-a5-landscape", odtPaperPart, []string{`style:name="pm1"><style:page-layout-properties fo:page-width="210mm" fo:page-height="148mm" style:print-orientation="landscape"`}},
		{"litho/odt", "paper-executive-portrait", odtPaperPart, []string{`fo:page-width="184.15mm" fo:page-height="266.7mm" style:print-orientation="portrait"`}},
		{"litho/odt", "paper-mixed-orientation", odtPaperPart, []string{`style:name="pm2"><style:page-layout-properties fo:page-width="279.4mm" fo:page-height="215.9mm" style:print-orientation="landscape"`, `style:name="PartTwo" style:family="paragraph" style:parent-style-name="Heading_20_1" style:master-page-name="Second"/>`}},
		{"litho/odt", "paper-option-a3-landscape", fixture.OptionsFile, []string{`"paperWidth": 11.6929`, `"paperHeight": 16.5354`, `"landscape": true`}},
		{"litho/odt", "paper-default-landscape", odtPaperPart, []string{`style:name="pm1"><style:page-layout-properties style:print-orientation="landscape" fo:margin-top="0.5in"`}},
		{"litho/odt", "paper-margins-uneven-a4-landscape", odtPaperPart, []string{`style:name="pm1"><style:page-layout-properties fo:page-width="297mm" fo:page-height="210mm" style:print-orientation="landscape" fo:margin-top="1in" fo:margin-bottom="0.75in" fo:margin-left="0.5in" fo:margin-right="0.25in"/>`}},
		{"litho/ods", "paper-grid-b5-portrait", odsGridPart, []string{`fo:page-width="176mm" fo:page-height="250mm" style:print-orientation="portrait"`}},
		{"litho/ods", "paper-fit-a4-landscape", odsFitPart, []string{`fo:page-width="297mm" fo:page-height="210mm" style:print-orientation="landscape"`, `style:scale-to-X="1" style:scale-to-Y="0"`}},
		{"litho/ods", "paper-grid-default-portrait", odsGridPart, []string{`style:name="pm1"><style:page-layout-properties style:print-orientation="portrait" fo:margin-top="0.75in"`}},
		{"litho/ods", "paper-grid-margins-none-a5-portrait", odsGridPart, []string{`fo:page-width="148mm" fo:page-height="210mm" style:print-orientation="portrait" fo:margin-top="0in" fo:margin-bottom="0in" fo:margin-left="0in" fo:margin-right="0in"/>`}},
		{"litho/csv", "paper-table-option-executive-landscape", fixture.OptionsFile, []string{`"paperWidth": 7.25`, `"paperHeight": 10.5`, `"landscape": true`}},
		{"litho/csv", "paper-table-default-landscape", fixture.OptionsFile, []string{"{\n  \"landscape\": true\n}\n"}},
		{"litho/odp", "paper-deck-dia", odpPaperPart, []string{`fo:page-width="3.6cm" fo:page-height="2.4cm" style:print-orientation="landscape"`}},
		{"litho/odp", "paper-deck-postcard", odpPaperPart, []string{`fo:page-width="10cm" fo:page-height="14.8cm" style:print-orientation="portrait"`}},
		{"litho/html", "paper-option-legal-landscape", fixture.OptionsFile, []string{`"paperWidth": 8.5`, `"paperHeight": 14`, `"landscape": true`}},
		{"litho/html", "paper-css-jis-b4-landscape-paper", htmlPaperPart, []string{"@page { size: JIS-B4 landscape; }"}},
		{"litho/html", "paper-css-jis-b4-landscape-paper", fixture.OptionsFile, []string{`"preferCssPageSize": false`}},
		{"litho/html", "paper-css-lengths-wide-prefer", htmlPaperPart, []string{"@page { size: 25cm 15cm; }"}},
		{"litho/html", "paper-markdown-a5-portrait", fixture.OptionsFile, []string{`"paperWidth": 5.8268`, `"paperHeight": 8.2677`}},
		{"litho/html", "paper-default-landscape", fixture.OptionsFile, []string{"{\n  \"landscape\": true\n}\n"}},
		{"litho/html", "paper-margins-uneven-a3-landscape", fixture.OptionsFile, []string{"{\n  \"paperWidth\": 11.6929,\n  \"paperHeight\": 16.5354,\n  \"landscape\": true,\n  \"marginTop\": 1,\n  \"marginRight\": 0.25,\n  \"marginBottom\": 0.75,\n  \"marginLeft\": 0.5\n}\n"}},
		{"litho/html", "paper-margins-none-a4-portrait", fixture.OptionsFile, []string{"{\n  \"marginTop\": 0,\n  \"marginRight\": 0,\n  \"marginBottom\": 0,\n  \"marginLeft\": 0\n}\n"}},
		{"litho/html", "paper-markdown-margins-narrow-landscape", fixture.OptionsFile, []string{"{\n  \"landscape\": true,\n  \"marginTop\": 0.5,\n  \"marginRight\": 0.5,\n  \"marginBottom\": 0.5,\n  \"marginLeft\": 0.5\n}\n"}},
	}
	outs := map[string]string{}
	for _, c := range cases {
		if outs[c.corpus] == "" {
			outs[c.corpus] = buildFlatPaperCorpus(t, c.corpus)
		}
		data, err := os.ReadFile(filepath.Join(outs[c.corpus], c.name, c.file))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		for _, w := range c.want {
			if !strings.Contains(string(data), w) {
				t.Errorf("%s: %s does not state %s", c.name, c.file, w)
			}
		}
	}
}

// TestFlatPaperDocsLoad checks that every OpenDocument and HTML paper
// document loads as the fixture package reads a source, with options the
// route accepts, and that two builds write the same bytes.
func TestFlatPaperDocsLoad(t *testing.T) {
	t.Parallel()
	for _, corpus := range []string{"litho/odt", "litho/ods", "litho/odp", "litho/html", "litho/csv"} {
		a, b := buildFlatPaperCorpus(t, corpus), buildFlatPaperCorpus(t, corpus)
		for _, name := range paperDocNames(corpus) {
			dir := filepath.Join(a, name)
			var err error
			if corpus == "litho/html" {
				_, err = fixture.Load(dir)
			} else {
				_, err = fixture.LoadOffice(dir)
			}
			if err != nil {
				t.Errorf("%s: %v", name, err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				da, errA := os.ReadFile(filepath.Join(dir, e.Name()))
				db, errB := os.ReadFile(filepath.Join(b, name, e.Name()))
				if errA != nil || errB != nil || !bytes.Equal(da, db) {
					t.Errorf("%s: %s differs between two builds", name, e.Name())
				}
			}
		}
	}
}

// TestPaperDocsMatrix locks the size of the paper matrix and the names of
// its documents per format: each name matches exactly one row of its
// corpus, and each row holds the count of documents it states.
func TestPaperDocsMatrix(t *testing.T) {
	t.Parallel()
	const (
		papers = `(a3|a4|a5|b5|letter|legal|tabloid|executive)`
		orient = `(portrait|landscape)`
		sets   = `(none|narrow|uneven)`
	)
	type row struct {
		pattern string
		count   int
	}
	cases := []struct {
		corpus string
		rows   []row
	}{
		{"litho/docx", []row{
			{`paper-` + papers + `-` + orient, 16},
			{`paper-option-` + papers + `-` + orient, 16},
			{`paper-mixed-(orientation|sizes)`, 2},
			{`paper-default-portrait`, 1},
			{`paper-margins-` + sets + `-(a5|letter|a3)-` + orient, 18},
		}},
		{"litho/xlsx", []row{
			{`paper-grid-` + papers + `-` + orient, 16},
			{`paper-fit-(a5|letter|a3)-` + orient, 6},
			{`paper-grid-default-` + orient, 2},
			{`paper-grid-option-` + papers + `-` + orient, 16},
			{`paper-fit-option-(a5|letter|a3)-` + orient, 6},
			{`paper-grid-margins-` + sets + `-(a5|letter|a3)-` + orient, 18},
		}},
		{"litho/odt", []row{
			{`paper-` + papers + `-` + orient, 16},
			{`paper-option-` + papers + `-` + orient, 16},
			{`paper-mixed-(orientation|sizes)`, 2},
			{`paper-default-` + orient, 2},
			{`paper-margins-` + sets + `-(a5|a4|a3)-` + orient, 18},
		}},
		{"litho/ods", []row{
			{`paper-grid-` + papers + `-` + orient, 16},
			{`paper-fit-(a5|a4|a3)-` + orient, 6},
			{`paper-grid-default-` + orient, 2},
			{`paper-grid-option-` + papers + `-` + orient, 16},
			{`paper-fit-option-(a5|a4|a3)-` + orient, 6},
			{`paper-grid-margins-` + sets + `-(a5|a4|a3)-` + orient, 18},
		}},
		{"litho/csv", []row{
			{`paper-table-option-` + papers + `-` + orient, 16},
			{`paper-table-default-landscape`, 1},
		}},
		{"litho/html", []row{
			{`paper-option-` + papers + `-` + orient, 16},
			{`paper-default-landscape`, 1},
			{`paper-css-(a5|a4|a3|b5|b4|jis-b5|jis-b4|letter|legal|ledger)-` + orient + `-(prefer|paper)`, 40},
			{`paper-css-lengths-(narrow|wide)-(prefer|paper)`, 4},
			{`paper-markdown-` + papers + `-` + orient, 16},
			{`paper-markdown-default-landscape`, 1},
			{`paper-margins-` + sets + `-(a5|a4|a3)-` + orient, 18},
			{`paper-markdown-margins-` + sets + `-` + orient, 6},
		}},
		{"litho/pptx", []row{{`paper-deck-[a-z0-9-]+`, 18}}},
		{"litho/odp", []row{{`paper-deck-[a-z0-9-]+`, 17}}},
	}
	for _, c := range cases {
		names := paperDocNames(c.corpus)
		total := 0
		for _, r := range c.rows {
			re := regexp.MustCompile(`^` + r.pattern + `$`)
			n := 0
			for _, name := range names {
				if re.MatchString(name) {
					n++
				}
			}
			if n != r.count {
				t.Errorf("%s: %d documents match %s, want %d", c.corpus, n, r.pattern, r.count)
			}
			total += r.count
		}
		if len(names) != total {
			t.Errorf("%s: %d paper documents, want %d", c.corpus, len(names), total)
		}
	}
}
