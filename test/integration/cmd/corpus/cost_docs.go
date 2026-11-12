package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"html"
	"math/rand/v2"
	"strings"
	"time"
)

// The measurement documents of `corpus cost` that no corpus holds: a report
// of headings, paragraphs, lists and tables in HTML, Markdown, DOCX and ODT,
// a sales workbook in XLSX and ODS, and a slide deck in ODP. Each is drawn
// from a fixed seed, so a build writes the same bytes every time.

// costVocabulary is the words the measurement documents' prose is made of:
// words of a business report, short and long, so lines break as prose does.
var costVocabulary = []string{
	"the", "of", "and", "to", "in", "a", "for", "on", "with", "by", "is", "was", "are", "were", "from", "at",
	"quarter", "revenue", "customers", "region", "growth", "margin", "service", "delivery", "contract", "budget",
	"forecast", "operations", "inventory", "suppliers", "shipments", "warehouse", "agreement", "procurement",
	"performance", "infrastructure", "maintenance", "compliance", "assessment", "development", "management",
	"increased", "decreased", "remained", "improved", "reported", "expected", "approved", "reviewed",
	"significant", "moderate", "regional", "national", "quarterly", "annual", "projected", "estimated",
	"team", "board", "office", "market", "product", "volume", "price", "cost", "risk", "plan", "rate", "share",
}

// costRegions, costAccounts and costProducts fill the tables' text columns.
var (
	costRegions  = []string{"North", "South", "East", "West", "Central", "Coastal", "Highlands", "Islands"}
	costAccounts = []string{"Acme Corp", "Globex", "Initech", "Umbrella", "Hooli", "Vandelay", "Stark Industries", "Wayne Enterprises", "Soylent", "Tyrell", "Cyberdyne", "Wonka"}
	costProducts = []string{"Widget", "Gadget", "Sprocket", "Gizmo", "Bracket", "Flange", "Valve", "Bearing", "Coupling", "Gear"}
)

// costEpoch is the first date the tables and the workbook print.
var costEpoch = time.Date(2025, time.January, 6, 0, 0, 0, 0, time.UTC)

// costSentence returns a sentence of 8 to 22 words drawn from r, its first
// word capitalized, ending in a full stop.
func costSentence(r *rand.Rand) string {
	n := 8 + r.IntN(15)
	var b strings.Builder
	for i := range n {
		w := costVocabulary[r.IntN(len(costVocabulary))]
		if i == 0 {
			w = strings.ToUpper(w[:1]) + w[1:]
		} else {
			b.WriteByte(' ')
		}
		b.WriteString(w)
		if i > 2 && i < n-2 && r.IntN(9) == 0 {
			b.WriteByte(',')
		}
	}
	b.WriteByte('.')
	return b.String()
}

// costParagraph returns a paragraph of 3 to 7 sentences drawn from r.
func costParagraph(r *rand.Rand) string {
	n := 3 + r.IntN(5)
	s := make([]string, n)
	for i := range s {
		s[i] = costSentence(r)
	}
	return strings.Join(s, " ")
}

// costTable is a table of a report: a header row and rows of text and
// numbers.
type costTable struct {
	head []string
	rows [][]string
	// numeric marks the columns that hold numbers, aligned right.
	numeric []bool
}

// costSection is one section of a report: a heading, paragraphs, and an
// optional list and table.
type costSection struct {
	title string
	paras []string
	list  []string
	table *costTable
}

// costReport returns a report of n sections drawn from a fixed seed: every
// second section holds a table of 8 to 30 rows, every third a list.
func costReport(n int) []costSection {
	r := rand.New(rand.NewPCG(11, 12)) //nolint:gosec // a fixed seed, for the same bytes each build
	sections := make([]costSection, n)
	for i := range sections {
		s := costSection{title: fmt.Sprintf("%d. %s", i+1, strings.TrimSuffix(costSentence(r), "."))}
		if len(s.title) > 60 {
			s.title = strings.TrimRight(s.title[:60], " ,")
		}
		for range 2 + r.IntN(3) {
			s.paras = append(s.paras, costParagraph(r))
		}
		if i%3 == 2 {
			for range 3 + r.IntN(4) {
				s.list = append(s.list, costSentence(r))
			}
		}
		if i%2 == 1 {
			s.table = costRandomTable(r, i)
		}
		sections[i] = s
	}
	return sections
}

// costRandomTable returns a table of 8 to 30 rows of orders drawn from r,
// section the section it belongs to.
func costRandomTable(r *rand.Rand, section int) *costTable {
	t := &costTable{
		head:    []string{"Ref", "Account", "Region", "Date", "Quantity", "Amount"},
		numeric: []bool{false, false, false, false, true, true},
	}
	for j := range 8 + r.IntN(23) {
		qty := 1 + r.IntN(500)
		price := 5 + r.IntN(9500)
		t.rows = append(t.rows, []string{
			fmt.Sprintf("R-%04d-%02d", section+1, j+1),
			costAccounts[r.IntN(len(costAccounts))],
			costRegions[r.IntN(len(costRegions))],
			costEpoch.AddDate(0, 0, r.IntN(365)).Format("2006-01-02"),
			fmt.Sprintf("%d", qty),
			fmt.Sprintf("%d.%02d", qty*price/100, qty*price%100),
		})
	}
	return t
}

// costReportHTML returns the report of n sections as an HTML page.
func costReportHTML(n int) []byte {
	var b bytes.Buffer
	b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Quarterly report</title><style>` +
		`body{font-family:"Liberation Serif",serif;font-size:11pt;line-height:1.35}h1,h2{font-family:"Liberation Sans",sans-serif}` +
		`table{border-collapse:collapse;width:100%;margin:6pt 0;font-size:9.5pt}th,td{border:0.5pt solid #777;padding:2pt 4pt}` +
		`th{background:#e4e4e4;text-align:left}td.n{text-align:right}tr:nth-child(even) td{background:#f6f6f6}</style></head><body>` + "\n")
	b.WriteString("<h1>Quarterly report</h1>\n")
	for _, s := range costReport(n) {
		fmt.Fprintf(&b, "<h2>%s</h2>\n", html.EscapeString(s.title))
		for _, p := range s.paras {
			fmt.Fprintf(&b, "<p>%s</p>\n", html.EscapeString(p))
		}
		if len(s.list) > 0 {
			b.WriteString("<ul>\n")
			for _, item := range s.list {
				fmt.Fprintf(&b, "<li>%s</li>\n", html.EscapeString(item))
			}
			b.WriteString("</ul>\n")
		}
		if t := s.table; t != nil {
			b.WriteString("<table><thead><tr>")
			for _, h := range t.head {
				fmt.Fprintf(&b, "<th>%s</th>", h)
			}
			b.WriteString("</tr></thead><tbody>\n")
			for _, row := range t.rows {
				b.WriteString("<tr>")
				for c, v := range row {
					if t.numeric[c] {
						fmt.Fprintf(&b, `<td class="n">%s</td>`, html.EscapeString(v))
					} else {
						fmt.Fprintf(&b, "<td>%s</td>", html.EscapeString(v))
					}
				}
				b.WriteString("</tr>\n")
			}
			b.WriteString("</tbody></table>\n")
		}
	}
	b.WriteString("</body></html>\n")
	return b.Bytes()
}

// costReportMarkdown returns the report of n sections as Markdown, its
// tables in the pipe syntax of GitHub Flavored Markdown.
func costReportMarkdown(n int) []byte {
	var b bytes.Buffer
	b.WriteString("# Quarterly report\n\n")
	for _, s := range costReport(n) {
		fmt.Fprintf(&b, "## %s\n\n", s.title)
		for i, p := range s.paras {
			if i == 1 {
				// One emphasis a section, as prose carries.
				if k := strings.IndexByte(p, ' '); k > 0 {
					p = "**" + p[:k] + "**" + p[k:]
				}
			}
			fmt.Fprintf(&b, "%s\n\n", p)
		}
		for _, item := range s.list {
			fmt.Fprintf(&b, "- %s\n", item)
		}
		if len(s.list) > 0 {
			b.WriteString("\n")
		}
		if t := s.table; t != nil {
			fmt.Fprintf(&b, "| %s |\n|", strings.Join(t.head, " | "))
			for _, num := range t.numeric {
				if num {
					b.WriteString("---:|")
				} else {
					b.WriteString("---|")
				}
			}
			b.WriteString("\n")
			for _, row := range t.rows {
				fmt.Fprintf(&b, "| %s |\n", strings.Join(row, " | "))
			}
			b.WriteString("\n")
		}
	}
	return b.Bytes()
}

// The parts of the WordprocessingML report every size shares.
const (
	costWordNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`
	costWordTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
		`<Default Extension="xml" ContentType="application/xml"/>` +
		`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
		`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
		`<Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/>` +
		`</Types>`
	costWordRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`
	costWordDocRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>` +
		`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/></Relationships>`
	costWordStyles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:styles ` + costWordNS + `>` +
		`<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Liberation Serif" w:hAnsi="Liberation Serif" w:cs="Liberation Serif"/><w:sz w:val="22"/><w:lang w:val="en-US"/></w:rPr></w:rPrDefault>` +
		`<w:pPrDefault><w:pPr><w:spacing w:after="120" w:line="264" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>` +
		`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>` +
		`<w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:basedOn w:val="Normal"/><w:pPr><w:spacing w:after="240"/></w:pPr><w:rPr><w:rFonts w:ascii="Liberation Sans" w:hAnsi="Liberation Sans"/><w:b/><w:sz w:val="40"/></w:rPr></w:style>` +
		`<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="240" w:after="120"/><w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:rFonts w:ascii="Liberation Sans" w:hAnsi="Liberation Sans"/><w:b/><w:sz w:val="28"/></w:rPr></w:style>` +
		`<w:style w:type="paragraph" w:styleId="ListParagraph"><w:name w:val="List Paragraph"/><w:basedOn w:val="Normal"/><w:pPr><w:ind w:left="720"/><w:spacing w:after="40"/></w:pPr></w:style>` +
		`<w:style w:type="table" w:styleId="TableGrid"><w:name w:val="Table Grid"/><w:tblPr><w:tblBorders>` +
		`<w:top w:val="single" w:sz="4" w:space="0" w:color="777777"/><w:left w:val="single" w:sz="4" w:space="0" w:color="777777"/>` +
		`<w:bottom w:val="single" w:sz="4" w:space="0" w:color="777777"/><w:right w:val="single" w:sz="4" w:space="0" w:color="777777"/>` +
		`<w:insideH w:val="single" w:sz="4" w:space="0" w:color="777777"/><w:insideV w:val="single" w:sz="4" w:space="0" w:color="777777"/>` +
		`</w:tblBorders><w:tblCellMar><w:left w:w="80" w:type="dxa"/><w:right w:w="80" w:type="dxa"/></w:tblCellMar></w:tblPr></w:style>` +
		`</w:styles>`
	costWordNumbering = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:numbering ` + costWordNS + `>` +
		`<w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="bullet"/><w:lvlText w:val="&#8226;"/><w:lvlJc w:val="left"/>` +
		`<w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr></w:lvl></w:abstractNum><w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num></w:numbering>`
)

// costReportDocx returns the report of n sections as a WordprocessingML
// document: styled headings, bulleted lists and bordered tables.
func costReportDocx(n int) ([]byte, error) {
	var body strings.Builder
	esc := html.EscapeString
	body.WriteString(`<w:p><w:pPr><w:pStyle w:val="Title"/></w:pPr><w:r><w:t>Quarterly report</w:t></w:r></w:p>`)
	widths := []int{1300, 1900, 1300, 1300, 1200, 1500}
	for _, s := range costReport(n) {
		fmt.Fprintf(&body, `<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>%s</w:t></w:r></w:p>`, esc(s.title))
		for _, p := range s.paras {
			fmt.Fprintf(&body, `<w:p><w:r><w:t xml:space="preserve">%s</w:t></w:r></w:p>`, esc(p))
		}
		for _, item := range s.list {
			fmt.Fprintf(&body, `<w:p><w:pPr><w:pStyle w:val="ListParagraph"/><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t xml:space="preserve">%s</w:t></w:r></w:p>`, esc(item))
		}
		if t := s.table; t != nil {
			body.WriteString(`<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/><w:tblW w:w="5000" w:type="pct"/></w:tblPr><w:tblGrid>`)
			for _, w := range widths {
				fmt.Fprintf(&body, `<w:gridCol w:w="%d"/>`, w)
			}
			body.WriteString(`</w:tblGrid><w:tr><w:trPr><w:tblHeader/></w:trPr>`)
			for c, h := range t.head {
				fmt.Fprintf(&body, `<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/><w:shd w:val="clear" w:color="auto" w:fill="E4E4E4"/></w:tcPr><w:p><w:pPr><w:spacing w:after="0"/></w:pPr><w:r><w:rPr><w:b/><w:sz w:val="19"/></w:rPr><w:t>%s</w:t></w:r></w:p></w:tc>`, widths[c], h)
			}
			body.WriteString(`</w:tr>`)
			for _, row := range t.rows {
				body.WriteString(`<w:tr>`)
				for c, v := range row {
					jc := ""
					if t.numeric[c] {
						jc = `<w:jc w:val="right"/>`
					}
					fmt.Fprintf(&body, `<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/></w:tcPr><w:p><w:pPr><w:spacing w:after="0"/>%s</w:pPr><w:r><w:rPr><w:sz w:val="19"/></w:rPr><w:t>%s</w:t></w:r></w:p></w:tc>`, widths[c], jc, esc(v))
				}
				body.WriteString(`</w:tr>`)
			}
			body.WriteString(`</w:tbl><w:p/>`)
		}
	}
	return zipPackage(map[string][]byte{
		contentTypesPart:               []byte(costWordTypes),
		"_rels/.rels":                  []byte(costWordRels),
		"word/_rels/document.xml.rels": []byte(costWordDocRels),
		"word/styles.xml":              []byte(costWordStyles),
		"word/numbering.xml":           []byte(costWordNumbering),
		"word/document.xml": []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document ` + costWordNS + `><w:body>` + body.String() +
			`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1134" w:right="1134" w:bottom="1134" w:left="1134" w:header="567" w:footer="567" w:gutter="0"/></w:sectPr></w:body></w:document>`),
	})
}

// The namespaces of the OpenDocument parts.
const costODFNS = `xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" ` +
	`xmlns:style="urn:oasis:names:tc:opendocument:xmlns:style:1.0" ` +
	`xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0" ` +
	`xmlns:table="urn:oasis:names:tc:opendocument:xmlns:table:1.0" ` +
	`xmlns:draw="urn:oasis:names:tc:opendocument:xmlns:drawing:1.0" ` +
	`xmlns:presentation="urn:oasis:names:tc:opendocument:xmlns:presentation:1.0" ` +
	`xmlns:fo="urn:oasis:names:tc:opendocument:xmlns:xsl-fo-compatible:1.0" ` +
	`xmlns:svg="urn:oasis:names:tc:opendocument:xmlns:svg-compatible:1.0" ` +
	`xmlns:number="urn:oasis:names:tc:opendocument:xmlns:datastyle:1.0" ` +
	`xmlns:of="urn:oasis:names:tc:opendocument:xmlns:of:1.2" ` +
	`xmlns:xlink="http://www.w3.org/1999/xlink" office:version="1.3"`

// costODFFonts declares the faces the OpenDocument measurement documents
// name.
const costODFFonts = `<office:font-face-decls><style:font-face style:name="Liberation Serif" svg:font-family="'Liberation Serif'" style:font-family-generic="roman"/>` +
	`<style:font-face style:name="Liberation Sans" svg:font-family="'Liberation Sans'" style:font-family-generic="swiss"/></office:font-face-decls>`

// odfPackage zips an OpenDocument package of mimetype: the mimetype entry
// first and stored, as ODF 1.3 Part 2, 3.3 requires, then the manifest, the
// content and the styles, every entry at [zipEpoch].
func odfPackage(mimetype, content, styles string) ([]byte, error) {
	manifest := `<?xml version="1.0" encoding="UTF-8"?><manifest:manifest xmlns:manifest="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0" manifest:version="1.3">` +
		`<manifest:file-entry manifest:full-path="/" manifest:media-type="` + mimetype + `"/>` +
		`<manifest:file-entry manifest:full-path="content.xml" manifest:media-type="text/xml"/>` +
		`<manifest:file-entry manifest:full-path="styles.xml" manifest:media-type="text/xml"/></manifest:manifest>`
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entries := []struct {
		name   string
		method uint16
		data   string
	}{
		{"mimetype", zip.Store, mimetype},
		{"META-INF/manifest.xml", zip.Deflate, manifest},
		{"content.xml", zip.Deflate, content},
		{"styles.xml", zip.Deflate, styles},
	}
	for _, e := range entries {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: e.method, Modified: zipEpoch})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(e.data)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// costReportODT returns the report of n sections as an OpenDocument text:
// headings of outline level 1, bulleted lists and bordered tables.
func costReportODT(n int) ([]byte, error) {
	esc := html.EscapeString
	var body strings.Builder
	body.WriteString(`<text:p text:style-name="Title">Quarterly report</text:p>`)
	for i, s := range costReport(n) {
		fmt.Fprintf(&body, `<text:h text:style-name="Heading_20_1" text:outline-level="1">%s</text:h>`, esc(s.title))
		for _, p := range s.paras {
			fmt.Fprintf(&body, `<text:p text:style-name="Text_20_body">%s</text:p>`, esc(p))
		}
		if len(s.list) > 0 {
			body.WriteString(`<text:list text:style-name="L1">`)
			for _, item := range s.list {
				fmt.Fprintf(&body, `<text:list-item><text:p text:style-name="Text_20_body">%s</text:p></text:list-item>`, esc(item))
			}
			body.WriteString(`</text:list>`)
		}
		if t := s.table; t != nil {
			fmt.Fprintf(&body, `<table:table table:name="Table%d" table:style-name="Tbl"><table:table-column table:number-columns-repeated="%d"/><table:table-header-rows><table:table-row>`, i+1, len(t.head))
			for _, h := range t.head {
				fmt.Fprintf(&body, `<table:table-cell table:style-name="Head" office:value-type="string"><text:p text:style-name="Cell">%s</text:p></table:table-cell>`, h)
			}
			body.WriteString(`</table:table-row></table:table-header-rows>`)
			for _, row := range t.rows {
				body.WriteString(`<table:table-row>`)
				for c, v := range row {
					style := "Cell"
					if t.numeric[c] {
						style = "Num"
					}
					fmt.Fprintf(&body, `<table:table-cell table:style-name="Box" office:value-type="string"><text:p text:style-name="%s">%s</text:p></table:table-cell>`, style, esc(v))
				}
				body.WriteString(`</table:table-row>`)
			}
			body.WriteString(`</table:table>`)
		}
	}
	const border = `fo:border="0.5pt solid #777777" fo:padding="0.05cm"`
	content := `<?xml version="1.0" encoding="UTF-8"?><office:document-content ` + costODFNS + `>` + costODFFonts +
		`<office:automatic-styles>` +
		`<style:style style:name="Tbl" style:family="table"><style:table-properties style:width="17cm" table:align="margins" fo:margin-top="0.15cm" fo:margin-bottom="0.15cm"/></style:style>` +
		`<style:style style:name="Head" style:family="table-cell"><style:table-cell-properties ` + border + ` fo:background-color="#e4e4e4"/></style:style>` +
		`<style:style style:name="Box" style:family="table-cell"><style:table-cell-properties ` + border + `/></style:style>` +
		`<text:list-style style:name="L1"><text:list-level-style-bullet text:level="1" text:bullet-char="&#8226;"><style:list-level-properties text:list-level-position-and-space-mode="label-alignment">` +
		`<style:list-level-label-alignment text:label-followed-by="listtab" text:list-tab-stop-position="1.27cm" fo:text-indent="-0.635cm" fo:margin-left="1.27cm"/></style:list-level-properties></text:list-level-style-bullet></text:list-style>` +
		`</office:automatic-styles><office:body><office:text>` + body.String() + `</office:text></office:body></office:document-content>`
	styles := `<?xml version="1.0" encoding="UTF-8"?><office:document-styles ` + costODFNS + `>` + costODFFonts +
		`<office:styles>` +
		`<style:default-style style:family="paragraph"><style:text-properties style:font-name="Liberation Serif" fo:font-size="11pt" fo:language="en" fo:country="US"/></style:default-style>` +
		`<style:style style:name="Standard" style:family="paragraph" style:class="text"/>` +
		`<style:style style:name="Text_20_body" style:display-name="Text body" style:family="paragraph" style:parent-style-name="Standard"><style:paragraph-properties fo:margin-bottom="0.21cm" fo:line-height="115%"/></style:style>` +
		`<style:style style:name="Title" style:family="paragraph" style:parent-style-name="Standard"><style:paragraph-properties fo:margin-bottom="0.42cm"/><style:text-properties style:font-name="Liberation Sans" fo:font-size="20pt" fo:font-weight="bold"/></style:style>` +
		`<style:style style:name="Heading_20_1" style:display-name="Heading 1" style:family="paragraph" style:parent-style-name="Standard" style:default-outline-level="1">` +
		`<style:paragraph-properties fo:margin-top="0.42cm" fo:margin-bottom="0.21cm" fo:keep-with-next="always"/><style:text-properties style:font-name="Liberation Sans" fo:font-size="14pt" fo:font-weight="bold"/></style:style>` +
		`<style:style style:name="Cell" style:family="paragraph" style:parent-style-name="Standard"><style:text-properties fo:font-size="9.5pt"/></style:style>` +
		`<style:style style:name="Num" style:family="paragraph" style:parent-style-name="Cell"><style:paragraph-properties fo:text-align="end"/></style:style>` +
		`</office:styles><office:automatic-styles><style:page-layout style:name="PL1"><style:page-layout-properties fo:page-width="21cm" fo:page-height="29.7cm" ` +
		`fo:margin-top="2cm" fo:margin-bottom="2cm" fo:margin-left="2cm" fo:margin-right="2cm" style:print-orientation="portrait"/></style:page-layout></office:automatic-styles>` +
		`<office:master-styles><style:master-page style:name="Standard" style:page-layout-name="PL1"/></office:master-styles></office:document-styles>`
	return odfPackage("application/vnd.oasis.opendocument.text", content, styles)
}

// costSalesColumns are the columns of a sheet of the sales workbook: the
// last is the product of the two before it, a formula.
var costSalesColumns = []string{"Order", "Date", "Region", "Account", "Product", "Units", "Unit price", "Total"}

// costSaleRow is one row of the sales workbook.
type costSaleRow struct {
	order                    int
	day                      int // days after costEpoch
	region, account, product string
	units                    int
	priceCents, totalCents   int
}

// costSales returns the sales workbook, drawn from a fixed seed: as many
// sheets as sheets asks, each of as many rows as rows asks.
func costSales(sheets, rows int) [][]costSaleRow {
	r := rand.New(rand.NewPCG(13, 14)) //nolint:gosec // a fixed seed, for the same bytes each build
	out := make([][]costSaleRow, sheets)
	for s := range out {
		out[s] = make([]costSaleRow, rows)
		for i := range out[s] {
			units := 1 + r.IntN(400)
			price := 99 + r.IntN(50_000)
			out[s][i] = costSaleRow{
				order: 10_000*(s+1) + i + 1, day: r.IntN(365),
				region: costRegions[r.IntN(len(costRegions))], account: costAccounts[r.IntN(len(costAccounts))],
				product: costProducts[r.IntN(len(costProducts))], units: units, priceCents: price, totalCents: units * price,
			}
		}
	}
	return out
}

// costSheetName names sheet s of the sales workbook.
func costSheetName(s int) string { return fmt.Sprintf("Q%d %d", s%4+1, 2025+s/4) }

// cents writes c hundredths as a decimal number.
func cents(c int) string { return fmt.Sprintf("%d.%02d", c/100, c%100) }

// excelSerial returns the spreadsheet serial number of the day days after
// costEpoch, in the 1900 date system of ECMA-376 Part 1, 18.17.4.1.
func excelSerial(days int) int {
	return int(costEpoch.Sub(time.Date(1899, time.December, 30, 0, 0, 0, 0, time.UTC)).Hours()/24) + days
}

// sheetColumn returns the column letter of the 0-based column c, below 26.
func sheetColumn(c int) string { return string(rune('A' + c)) } //nolint:gosec // c is a column of costSalesColumns, below 26

// costSalesXlsx returns the sales workbook as SpreadsheetML: a bold header
// row, dates, shared strings, a formula a row with its cached value and a
// totals row.
func costSalesXlsx(sheets, rows int) ([]byte, error) {
	data := costSales(sheets, rows)
	strs := map[string]int{}
	var shared []string
	str := func(s string) int {
		if i, ok := strs[s]; ok {
			return i
		}
		strs[s] = len(shared)
		shared = append(shared, s)
		return strs[s]
	}
	parts := map[string][]byte{}
	var types, sheetList, wbRels strings.Builder
	for s, sheet := range data {
		var b strings.Builder
		b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
		b.WriteString(`<cols><col min="1" max="1" width="9" customWidth="1"/><col min="2" max="3" width="11" customWidth="1"/><col min="4" max="4" width="18" customWidth="1"/>` +
			`<col min="5" max="5" width="11" customWidth="1"/><col min="6" max="6" width="7" customWidth="1"/><col min="7" max="8" width="12" customWidth="1"/></cols><sheetData>`)
		b.WriteString(`<row r="1">`)
		for c, h := range costSalesColumns {
			fmt.Fprintf(&b, `<c r="%s1" t="s" s="1"><v>%d</v></c>`, sheetColumn(c), str(h))
		}
		b.WriteString(`</row>`)
		for i, row := range sheet {
			n := i + 2
			fmt.Fprintf(&b, `<row r="%d"><c r="A%d"><v>%d</v></c><c r="B%d" s="2"><v>%d</v></c><c r="C%d" t="s"><v>%d</v></c><c r="D%d" t="s"><v>%d</v></c><c r="E%d" t="s"><v>%d</v></c>`+
				`<c r="F%d"><v>%d</v></c><c r="G%d" s="3"><v>%s</v></c><c r="H%d" s="3"><f>F%d*G%d</f><v>%s</v></c></row>`,
				n, n, row.order, n, excelSerial(row.day), n, str(row.region), n, str(row.account), n, str(row.product),
				n, row.units, n, cents(row.priceCents), n, n, n, cents(row.totalCents))
		}
		last := len(sheet) + 1
		total := 0
		for _, row := range sheet {
			total += row.totalCents
		}
		fmt.Fprintf(&b, `<row r="%d"><c r="A%d" t="s" s="1"><v>%d</v></c><c r="H%d" s="4"><f>SUM(H2:H%d)</f><v>%s</v></c></row>`, last+1, last+1, str("Total"), last+1, last, cents(total))
		b.WriteString(`</sheetData><pageMargins left="0.7" right="0.7" top="0.75" bottom="0.75" header="0.3" footer="0.3"/><pageSetup paperSize="9" orientation="portrait"/></worksheet>`)
		name := fmt.Sprintf("sheet%d.xml", s+1)
		parts["xl/worksheets/"+name] = []byte(b.String())
		fmt.Fprintf(&types, `<Override PartName="/xl/worksheets/%s" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, name)
		fmt.Fprintf(&sheetList, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, costSheetName(s), s+1, s+1)
		fmt.Fprintf(&wbRels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/%s"/>`, s+1, name)
	}
	fmt.Fprintf(&wbRels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`, sheets+1)
	fmt.Fprintf(&wbRels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings" Target="sharedStrings.xml"/>`, sheets+2)
	var sst strings.Builder
	fmt.Fprintf(&sst, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="%d" uniqueCount="%d">`, len(shared), len(shared))
	for _, s := range shared {
		fmt.Fprintf(&sst, `<si><t>%s</t></si>`, html.EscapeString(s))
	}
	sst.WriteString(`</sst>`)
	parts["xl/sharedStrings.xml"] = []byte(sst.String())
	parts["xl/styles.xml"] = []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
		`<numFmts count="1"><numFmt numFmtId="164" formatCode="#,##0.00"/></numFmts>` +
		`<fonts count="2"><font><sz val="11"/><name val="Liberation Sans"/></font><font><b/><sz val="11"/><name val="Liberation Sans"/></font></fonts>` +
		`<fills count="3"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill><fill><patternFill patternType="solid"><fgColor rgb="FFE4E4E4"/></patternFill></fill></fills>` +
		`<borders count="2"><border><left/><right/><top/><bottom/><diagonal/></border><border><left/><right/><top style="thin"/><bottom style="double"/><diagonal/></border></borders>` +
		`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
		`<cellXfs count="5"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>` +
		`<xf numFmtId="0" fontId="1" fillId="2" borderId="0" xfId="0" applyFont="1" applyFill="1"/>` +
		`<xf numFmtId="14" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>` +
		`<xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>` +
		`<xf numFmtId="164" fontId="1" fillId="0" borderId="1" xfId="0" applyNumberFormat="1" applyFont="1" applyBorder="1"/></cellXfs>` +
		`<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`)
	parts["xl/workbook.xml"] = []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" ` +
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>` + sheetList.String() + `</sheets></workbook>`)
	parts["xl/_rels/workbook.xml.rels"] = []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + wbRels.String() + `</Relationships>`)
	parts["_rels/.rels"] = []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`)
	parts[contentTypesPart] = []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>` +
		`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
		`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>` +
		`<Override PartName="/xl/sharedStrings.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml"/>` +
		types.String() + `</Types>`)
	return zipPackage(parts)
}

// costSalesODS returns the sales workbook as an OpenDocument spreadsheet,
// with the same cells, formulas and styles as [costSalesXlsx].
func costSalesODS(sheets, rows int) ([]byte, error) {
	var body strings.Builder
	for s, sheet := range costSales(sheets, rows) {
		fmt.Fprintf(&body, `<table:table table:name="%s"><table:table-column table:style-name="co1"/><table:table-column table:style-name="co2" table:number-columns-repeated="2"/>`+
			`<table:table-column table:style-name="co3"/><table:table-column table:style-name="co2"/><table:table-column table:style-name="co1"/><table:table-column table:style-name="co2" table:number-columns-repeated="2"/><table:table-row>`, costSheetName(s))
		for _, h := range costSalesColumns {
			fmt.Fprintf(&body, `<table:table-cell table:style-name="Head" office:value-type="string"><text:p>%s</text:p></table:table-cell>`, h)
		}
		body.WriteString(`</table:table-row>`)
		total := 0
		for i, row := range sheet {
			n := i + 2
			date := costEpoch.AddDate(0, 0, row.day)
			total += row.totalCents
			fmt.Fprintf(&body, `<table:table-row><table:table-cell office:value-type="float" office:value="%d"><text:p>%d</text:p></table:table-cell>`+
				`<table:table-cell table:style-name="Date" office:value-type="date" office:date-value="%s"><text:p>%s</text:p></table:table-cell>`+
				`<table:table-cell office:value-type="string"><text:p>%s</text:p></table:table-cell><table:table-cell office:value-type="string"><text:p>%s</text:p></table:table-cell>`+
				`<table:table-cell office:value-type="string"><text:p>%s</text:p></table:table-cell><table:table-cell office:value-type="float" office:value="%d"><text:p>%d</text:p></table:table-cell>`+
				`<table:table-cell table:style-name="Money" office:value-type="float" office:value="%s"><text:p>%s</text:p></table:table-cell>`+
				`<table:table-cell table:style-name="Money" table:formula="of:=[.F%d]*[.G%d]" office:value-type="float" office:value="%s"><text:p>%s</text:p></table:table-cell></table:table-row>`,
				row.order, row.order, date.Format("2006-01-02"), date.Format("01/02/06"), row.region, html.EscapeString(row.account), row.product,
				row.units, row.units, cents(row.priceCents), cents(row.priceCents), n, n, cents(row.totalCents), cents(row.totalCents))
		}
		fmt.Fprintf(&body, `<table:table-row><table:table-cell table:style-name="Head" office:value-type="string"><text:p>Total</text:p></table:table-cell><table:table-cell table:number-columns-repeated="6"/>`+
			`<table:table-cell table:style-name="Sum" table:formula="of:=SUM([.H2:.H%d])" office:value-type="float" office:value="%s"><text:p>%s</text:p></table:table-cell></table:table-row></table:table>`,
			len(sheet)+1, cents(total), cents(total))
	}
	content := `<?xml version="1.0" encoding="UTF-8"?><office:document-content ` + costODFNS + `>` + costODFFonts +
		`<office:automatic-styles>` +
		`<style:style style:name="co1" style:family="table-column"><style:table-column-properties style:column-width="1.8cm"/></style:style>` +
		`<style:style style:name="co2" style:family="table-column"><style:table-column-properties style:column-width="2.4cm"/></style:style>` +
		`<style:style style:name="co3" style:family="table-column"><style:table-column-properties style:column-width="3.8cm"/></style:style>` +
		`<number:date-style style:name="N1"><number:month number:style="long"/><number:text>/</number:text><number:day number:style="long"/><number:text>/</number:text><number:year/></number:date-style>` +
		`<number:number-style style:name="N2"><number:number number:decimal-places="2" number:min-decimal-places="2" number:min-integer-digits="1" number:grouping="true"/></number:number-style>` +
		`<style:style style:name="Head" style:family="table-cell"><style:table-cell-properties fo:background-color="#e4e4e4"/><style:text-properties fo:font-weight="bold"/></style:style>` +
		`<style:style style:name="Date" style:family="table-cell" style:data-style-name="N1"/>` +
		`<style:style style:name="Money" style:family="table-cell" style:data-style-name="N2"/>` +
		`<style:style style:name="Sum" style:family="table-cell" style:data-style-name="N2"><style:table-cell-properties fo:border-top="0.5pt solid #000000" fo:border-bottom="1.5pt double #000000"/><style:text-properties fo:font-weight="bold"/></style:style>` +
		`</office:automatic-styles><office:body><office:spreadsheet>` + body.String() + `</office:spreadsheet></office:body></office:document-content>`
	styles := `<?xml version="1.0" encoding="UTF-8"?><office:document-styles ` + costODFNS + `>` + costODFFonts +
		`<office:styles><style:default-style style:family="table-cell"><style:text-properties style:font-name="Liberation Sans" fo:font-size="10pt" fo:language="en" fo:country="US"/></style:default-style></office:styles>` +
		`<office:automatic-styles><style:page-layout style:name="PL1"><style:page-layout-properties fo:page-width="21cm" fo:page-height="29.7cm" fo:margin-top="2cm" fo:margin-bottom="2cm" fo:margin-left="2cm" fo:margin-right="2cm" style:print-orientation="portrait"/></style:page-layout></office:automatic-styles>` +
		`<office:master-styles><style:master-page style:name="Default" style:page-layout-name="PL1"/></office:master-styles></office:document-styles>`
	return odfPackage("application/vnd.oasis.opendocument.spreadsheet", content, styles)
}

// costDeckODP returns an OpenDocument presentation of n slides, each a
// title and a bulleted list in placeholder frames of one master page.
func costDeckODP(n int) ([]byte, error) {
	r := rand.New(rand.NewPCG(15, 16)) //nolint:gosec // a fixed seed, for the same bytes each build
	esc := html.EscapeString
	var body strings.Builder
	for i := range n {
		fmt.Fprintf(&body, `<draw:page draw:name="Slide %d" draw:style-name="dp1" draw:master-page-name="Default" presentation:presentation-page-layout-name="AL1">`, i+1)
		fmt.Fprintf(&body, `<draw:frame presentation:style-name="pr1" presentation:class="title" svg:x="1.4cm" svg:y="0.8cm" svg:width="25.2cm" svg:height="2.6cm"><draw:text-box><text:p>Slide %d: %s</text:p></draw:text-box></draw:frame>`,
			i+1, esc(strings.TrimSuffix(costSentence(r), ".")))
		body.WriteString(`<draw:frame presentation:style-name="pr2" presentation:class="outline" svg:x="1.4cm" svg:y="3.8cm" svg:width="25.2cm" svg:height="10.8cm"><draw:text-box><text:list text:style-name="L1">`)
		for range 3 + r.IntN(3) {
			fmt.Fprintf(&body, `<text:list-item><text:p>%s</text:p></text:list-item>`, esc(costSentence(r)))
		}
		body.WriteString(`</text:list></draw:text-box></draw:frame></draw:page>`)
	}
	content := `<?xml version="1.0" encoding="UTF-8"?><office:document-content ` + costODFNS + `>` + costODFFonts +
		`<office:automatic-styles><style:style style:name="dp1" style:family="drawing-page"/>` +
		`<style:style style:name="pr1" style:family="presentation"><style:graphic-properties draw:fill="none" draw:stroke="none"/><style:text-properties style:font-name="Liberation Sans" fo:font-size="32pt" fo:font-weight="bold"/></style:style>` +
		`<style:style style:name="pr2" style:family="presentation"><style:graphic-properties draw:fill="none" draw:stroke="none"/><style:text-properties style:font-name="Liberation Sans" fo:font-size="20pt"/></style:style>` +
		`<text:list-style style:name="L1"><text:list-level-style-bullet text:level="1" text:bullet-char="&#8226;"><style:list-level-properties text:space-before="0cm" text:min-label-width="0.9cm"/></text:list-level-style-bullet></text:list-style>` +
		`</office:automatic-styles><office:body><office:presentation>` + body.String() + `</office:presentation></office:body></office:document-content>`
	styles := `<?xml version="1.0" encoding="UTF-8"?><office:document-styles ` + costODFNS + `>` + costODFFonts +
		`<office:styles><style:default-style style:family="graphic"><style:text-properties style:font-name="Liberation Sans" fo:font-size="18pt" fo:language="en" fo:country="US"/></style:default-style>` +
		`<style:presentation-page-layout style:name="AL1"><presentation:placeholder presentation:object="title" svg:x="1.4cm" svg:y="0.8cm" svg:width="25.2cm" svg:height="2.6cm"/>` +
		`<presentation:placeholder presentation:object="outline" svg:x="1.4cm" svg:y="3.8cm" svg:width="25.2cm" svg:height="10.8cm"/></style:presentation-page-layout></office:styles>` +
		`<office:automatic-styles><style:page-layout style:name="PM1"><style:page-layout-properties fo:margin-top="0cm" fo:margin-bottom="0cm" fo:margin-left="0cm" fo:margin-right="0cm" fo:page-width="28cm" fo:page-height="15.75cm" style:print-orientation="landscape"/></style:page-layout>` +
		`<style:style style:name="dp0" style:family="drawing-page"><style:drawing-page-properties draw:fill="solid" draw:fill-color="#ffffff"/></style:style></office:automatic-styles>` +
		`<office:master-styles><style:master-page style:name="Default" style:page-layout-name="PM1" draw:style-name="dp0"/></office:master-styles></office:document-styles>`
	return odfPackage("application/vnd.oasis.opendocument.presentation", content, styles)
}
