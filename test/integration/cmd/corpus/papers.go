package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture"
)

// paperDoc is one document `corpus build` generates from a committed base
// of its corpus by changing its page setup alone, so that one document,
// laid out on every paper, is judged against the reference engine on each.
// The same base always gives the same bytes.
type paperDoc struct {
	name string
	// base names the committed fixture the document starts from: a parts
	// directory of a build corpus, a sources directory of another.
	base string
	// edit rewrites the base's files, keyed by their slash path, in place.
	// nil leaves them as they are: an option fixture changes its options
	// alone.
	edit func(files map[string][]byte) error
	// options is the document's options.json, nil when it has none.
	options []byte
}

// paperSize is a paper the paper documents are laid out on, its portrait
// sides in millimetres.
type paperSize struct {
	// key names the paper in a fixture name.
	key  string
	w, h float64
	// code is the paperSize code a workbook names the paper with (ECMA-376
	// Part 1, 18.3.1.63, the table of paperSize values).
	code int
	// css is the size keyword CSS Paged Media names the paper with (CSS
	// Paged Media Level 3, 7.2.1, Table 1), "" when it names none.
	css string
}

// paperSizes are the papers every format is laid out on: the ISO 216 A3,
// A4 and A5, the ISO 216 B5, and the North American Letter, Legal, Tabloid
// (ANSI B) and Executive, at the sides ISO 216 and ANSI/ASME Y14.1 give
// them. Their SpreadsheetML codes are 8, 9, 11, 13, 1, 5, 3 and 7.
//
// A workbook's B5 is the JIS one, 182 by 257 mm, which Excel prints for
// code 13. SpreadsheetML names the ISO size as an envelope alone, code 34,
// and Excel's export prints a paper it lacks on Letter, so a workbook at
// that code would judge the export's paper list, not the converter.
//
// # Corpus evidence
//
//   - litho/xlsx paper_sizes: Excel prints codes 1, 3, 5, 8, 9 and 11 on
//     their papers and code 13 on 515.88 by 728.52 points, JIS B5; it
//     prints codes 20 and 27 (envelopes #10 and DL), 43 (Japanese
//     postcard) and 70 (A6) on Letter, turned as the sheet asks.
//   - probe, xlsx: paper-grid at code 34 came back from Excel as 9 pages
//     of 612 by 792 points; litho lays it on 12 pages of 176 by 250 mm.
var paperSizes = []paperSize{
	{key: "a3", w: 297, h: 420, code: 8, css: "A3"},
	{key: "a4", w: 210, h: 297, code: 9, css: "A4"},
	{key: "a5", w: 148, h: 210, code: 11, css: "A5"},
	{key: "b5", w: 176, h: 250, code: 13, css: "B5"},
	{key: "letter", w: 215.9, h: 279.4, code: 1, css: "letter"},
	{key: "legal", w: 215.9, h: 355.6, code: 5, css: "legal"},
	{key: "tabloid", w: 279.4, h: 431.8, code: 3, css: "ledger"},
	{key: "executive", w: 184.15, h: 266.7, code: 7},
}

// The paper each reference engine prints a document on when the document
// names none, as the fixture names spell it.
//
// # Corpus evidence
//
//   - litho/odt paper-default-portrait: the LibreOffice route of
//     gotenberg/gotenberg:9.4.1 prints a page layout with no fo:page-width or fo:page-height on
//     A4, 595.3 by 841.9 points.
//   - litho/csv paper-table: the same route prints a CSV file on A4.
//   - litho/html paper-base: the corpus posts A4 (210 by 297 mm) to the
//     Chromium route for a source that names no paper, so Chromium's own
//     default (Letter) never applies.
const (
	// defaultPaperMicrosoft is Letter, the paper of the account that builds
	// the Microsoft references (fixture.ReferencePaper).
	defaultPaperMicrosoft = "letter"
	// defaultPaperLibreOffice is A4: the pinned image leaves LibreOffice's
	// locale empty.
	defaultPaperLibreOffice = "a4"
	// defaultPaperChromium is A4, the paper the corpus posts for a source
	// without options.json.
	defaultPaperChromium = "a4"
)

// paperSpan returns the papers a dimension of the matrix that does not
// need every paper is laid out on: A5, the smallest, the engine's default
// paper, and A3, the largest ISO paper. A content box that holds on these
// holds on the papers between, which add documents and no information.
func paperSpan(defaultPaper string) []string { return []string{"a5", defaultPaper, "a3"} }

// orientations are the two ways a paper is turned, as fixture names spell
// them.
var orientations = []string{"portrait", "landscape"}

// marginSet is a set of page margins the margin documents are laid out
// with, in inches: the four sides and the distances from the paper's edge
// to the header and to the footer, which a DOCX section and a sheet state
// beside their margins.
type marginSet struct {
	key                      string
	top, right, bottom, left float64
	header, footer           float64
}

// marginSets are the margins of the margin documents: none at all, a
// narrow set even on the four sides, and an uneven one whose four sides
// differ, so a side read for another (top for bottom, left for right)
// moves the content box. Every side is a whole multiple of a quarter inch,
// 18 points or 24 CSS pixels, which Chromium lays out exactly (see
// fixture.Source.Margins), and a whole number of twentieths of a point.
//
// # Corpus evidence
//
//   - libreoffice/docx ooxmlimport/bnc779620: a section with w:top="0"
//     prints its first line 2.25 points below the paper's edge in Word's
//     reference, so Word keeps a zero margin.
//   - libreoffice/xlsx sc/tdf145054: a sheet with left="0" prints its first
//     column's text 1.29 points from the paper's edge in Excel's reference,
//     so Excel keeps a zero margin.
var marginSets = []marginSet{
	{key: "none"},
	{key: "narrow", top: 0.5, right: 0.5, bottom: 0.5, left: 0.5, header: 0.25, footer: 0.25},
	{key: "uneven", top: 1, right: 0.25, bottom: 0.75, left: 0.5, header: 0.5, footer: 0.25},
}

// paperByKey returns the paper named key.
func paperByKey(key string) paperSize {
	for _, p := range paperSizes {
		if p.key == key {
			return p
		}
	}
	return paperSize{}
}

// twips returns millimetres in twentieths of a point, rounded.
func twips(mm float64) int { return int(math.Round(mm / 25.4 * 1440)) }

// inches returns millimetres in inches, rounded to a ten-thousandth, as an
// options.json spells a paper side.
func inches(mm float64) float64 { return math.Round(mm/25.4*10000) / 10000 }

// errPaperBase reports a base whose page setup is not the one the generator
// rewrites: the base changed without its generator.
var errPaperBase = errors.New("the base's page setup is not the one the paper generator rewrites")

// replaceOnce replaces the one occurrence of old in files[part] with new,
// and fails when old occurs another number of times.
func replaceOnce(files map[string][]byte, part, old, replacement string) error {
	data, ok := files[part]
	if !ok {
		return fmt.Errorf("%w: no %s", errPaperBase, part)
	}
	if n := bytes.Count(data, []byte(old)); n != 1 {
		return fmt.Errorf("%w: %s holds %q %d times", errPaperBase, part, old, n)
	}
	files[part] = bytes.Replace(data, []byte(old), []byte(replacement), 1)
	return nil
}

// insertPaperBefore inserts text before the nth (1-based) occurrence of anchor
// in files[part].
func insertPaperBefore(files map[string][]byte, part, anchor string, nth int, text string) error {
	data, ok := files[part]
	if !ok {
		return fmt.Errorf("%w: no %s", errPaperBase, part)
	}
	at := -1
	for i, from := 0, 0; i < nth; i++ {
		j := bytes.Index(data[from:], []byte(anchor))
		if j < 0 {
			return fmt.Errorf("%w: %s holds %q fewer than %d times", errPaperBase, part, anchor, nth)
		}
		at = from + j
		from = at + len(anchor)
	}
	out := make([]byte, 0, len(data)+len(text))
	out = append(out, data[:at]...)
	out = append(out, text...)
	files[part] = append(out, data[at:]...)
	return nil
}

// paperOptions returns an options.json naming paper (portrait sides, turned
// by landscape) and, when ref is set, the fixture whose reference stands
// for the document's, its keys in a fixed order.
func paperOptions(p paperSize, landscape bool, ref string) []byte {
	return pageOptions(&p, landscape, nil, ref)
}

// pageOptions returns an options.json naming paper, nil to leave the
// corpus's default paper, landscape when it is set, the margins of m, nil
// for none, and ref under reference_of when it is set, its keys in a fixed
// order.
func pageOptions(paper *paperSize, landscape bool, m *marginSet, ref string) []byte {
	var fields [][2]string
	if paper != nil {
		fields = append(fields, [2]string{"paperWidth", formatInches(paper.w)}, [2]string{"paperHeight", formatInches(paper.h)})
	}
	if landscape {
		fields = append(fields, [2]string{"landscape", "true"})
	}
	if m != nil {
		fields = append(fields,
			[2]string{"marginTop", formatFloat(m.top)}, [2]string{"marginRight", formatFloat(m.right)},
			[2]string{"marginBottom", formatFloat(m.bottom)}, [2]string{"marginLeft", formatFloat(m.left)})
	}
	if ref != "" {
		fields = append(fields, [2]string{fixture.ReferenceOfName, strconv.Quote(ref)})
	}
	var b strings.Builder
	b.WriteString("{")
	for i, f := range fields {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "\n  %q: %s", f[0], f[1])
	}
	b.WriteString("\n}\n")
	return []byte(b.String())
}

// formatFloat spells a number as an options.json or a page setup does: the
// fewest digits that read back as the same float64.
func formatFloat(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// formatInches spells a paper side in inches as an options.json does.
func formatInches(mm float64) string { return formatFloat(inches(mm)) }

// paperDocs returns the paper documents of a corpus, in name order within
// each group, nil for a corpus without any.
func paperDocs(corpus string) []paperDoc {
	switch corpus {
	case "litho/docx":
		return docxPaperDocs()
	case "litho/xlsx":
		return xlsxPaperDocs()
	case "litho/pptx":
		return pptxPaperDocs()
	case "litho/odt":
		return odtPaperDocs()
	case "litho/ods":
		return odsPaperDocs()
	case "litho/odp":
		return odpPaperDocs()
	case "litho/html":
		return htmlPaperDocs()
	case "litho/csv":
		return csvPaperDocs()
	}
	return nil
}

// paperDocNames returns the names of a corpus's paper documents.
func paperDocNames(corpus string) []string {
	docs := paperDocs(corpus)
	names := make([]string, 0, len(docs))
	for _, d := range docs {
		names = append(names, d.name)
	}
	return names
}

// The page setup of the DOCX base, paper-base: one section, portrait
// Letter, one-inch margins and half-inch header and footer distances,
// whose sectPr closes the body (ECMA-376 Part 1, 17.6.17).
const (
	docxPaperBase     = "paper-base"
	docxPaperPart     = "word/document.xml"
	docxBasePgSz      = `<w:pgSz w:w="12240" w:h="15840"/>`
	docxBasePgMar     = `<w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720" w:gutter="0"/>`
	docxHeading1      = `<w:p><w:pPr><w:pStyle w:val="Heading1"/>`
	docxSectPrOpen    = "<w:sectPr>"
	docxSectPrClose   = "</w:sectPr>"
	docxSecondHeading = 2
)

// docxPgSz returns the pgSz of a paper (ECMA-376 Part 1, 17.6.13): w and h
// in twentieths of a point as the page is turned, and orient="landscape"
// when it is, which Word writes beside the swapped sides.
func docxPgSz(p paperSize, landscape bool) string {
	w, h := twips(p.w), twips(p.h)
	if landscape {
		return fmt.Sprintf(`<w:pgSz w:w="%d" w:h="%d" w:orient="landscape"/>`, h, w)
	}
	return fmt.Sprintf(`<w:pgSz w:w="%d" w:h="%d"/>`, w, h)
}

// docxPgMar returns the pgMar of a margin set (ECMA-376 Part 1, 17.6.11):
// the four sides and the header and footer distances in twentieths of a
// point, no gutter.
func docxPgMar(m marginSet) string {
	t := func(in float64) int { return twips(in * 25.4) }
	return fmt.Sprintf(`<w:pgMar w:top="%d" w:right="%d" w:bottom="%d" w:left="%d" w:header="%d" w:footer="%d" w:gutter="0"/>`,
		t(m.top), t(m.right), t(m.bottom), t(m.left), t(m.header), t(m.footer))
}

// docxPaperDocs are the DOCX paper documents: paper-base on each paper in
// each orientation; the base with each of those papers named as an option,
// borrowing the reference of the document of that paper; two documents of
// two sections, the second starting at the second Heading 1; the base with
// no pgSz, which Word prints on its default paper; and the base with each
// margin set on the papers of [paperSpan] around Word's default, in each
// orientation.
func docxPaperDocs() []paperDoc {
	var docs []paperDoc
	for _, p := range paperSizes {
		for _, o := range orientations {
			pgSz := docxPgSz(p, o == "landscape")
			docs = append(docs, paperDoc{
				name: fmt.Sprintf("paper-%s-%s", p.key, o),
				base: docxPaperBase,
				edit: func(files map[string][]byte) error {
					return replaceOnce(files, docxPaperPart, docxBasePgSz, pgSz)
				},
			})
		}
	}
	for _, p := range paperSizes {
		for _, o := range orientations {
			docs = append(docs, paperDoc{
				name:    fmt.Sprintf("paper-option-%s-%s", p.key, o),
				base:    docxPaperBase,
				options: paperOptions(p, o == "landscape", fmt.Sprintf("paper-%s-%s", p.key, o)),
			})
		}
	}
	letter, a4, a5 := paperByKey("letter"), paperByKey("a4"), paperByKey("a5")
	docs = append(docs,
		paperDoc{name: "paper-mixed-orientation", base: docxPaperBase, edit: docxTwoSections(docxPgSz(letter, false), docxPgSz(letter, true))},
		paperDoc{name: "paper-mixed-sizes", base: docxPaperBase, edit: docxTwoSections(docxPgSz(a4, false), docxPgSz(a5, false))},
		// A sectPr may leave pgSz out (17.6.17): the application then
		// chooses the paper.
		paperDoc{name: "paper-default-portrait", base: docxPaperBase, edit: func(files map[string][]byte) error {
			return replaceOnce(files, docxPaperPart, docxBasePgSz, "")
		}},
	)
	for _, m := range marginSets {
		for _, key := range paperSpan(defaultPaperMicrosoft) {
			for _, o := range orientations {
				setup := docxPgSz(paperByKey(key), o == "landscape") + docxPgMar(m)
				docs = append(docs, paperDoc{
					name: fmt.Sprintf("paper-margins-%s-%s-%s", m.key, key, o),
					base: docxPaperBase,
					edit: func(files map[string][]byte) error {
						return replaceOnce(files, docxPaperPart, docxBasePgSz+docxBasePgMar, setup)
					},
				})
			}
		}
	}
	return docs
}

// docxTwoSections splits paper-base into two sections at its second
// Heading 1: the first on first, the second on second. The first section
// ends with a paragraph whose pPr holds a copy of the closing sectPr
// (ECMA-376 Part 1, 17.6.18), its header and footer references included,
// since a section without them would print none.
func docxTwoSections(first, second string) func(map[string][]byte) error {
	return func(files map[string][]byte) error {
		data := files[docxPaperPart]
		open := bytes.LastIndex(data, []byte(docxSectPrOpen))
		end := bytes.LastIndex(data, []byte(docxSectPrClose))
		if open < 0 || end < open {
			return fmt.Errorf("%w: %s has no closing sectPr", errPaperBase, docxPaperPart)
		}
		sect := string(data[open : end+len(docxSectPrClose)])
		if strings.Count(sect, docxBasePgSz) != 1 {
			return fmt.Errorf("%w: the closing sectPr has no %s", errPaperBase, docxBasePgSz)
		}
		firstSect := strings.Replace(sect, docxBasePgSz, first, 1)
		if err := replaceOnce(files, docxPaperPart, docxBasePgSz, second); err != nil {
			return err
		}
		return insertPaperBefore(files, docxPaperPart, docxHeading1, docxSecondHeading, "<w:p><w:pPr>"+firstSect+"</w:pPr></w:p>")
	}
}

// The page setup of the XLSX bases: one sheet each, portrait Letter
// (paperSize 1), Excel's default margins, paper-fit fitted to one page wide
// (fitToWidth 1, fitToHeight 0: as many pages down as it needs, ECMA-376
// Part 1, 18.3.1.63).
const (
	xlsxGridBase       = "paper-grid"
	xlsxFitBase        = "paper-fit"
	xlsxPaperPart      = "xl/worksheets/sheet1.xml"
	xlsxBasePageSetup  = `paperSize="1" orientation="portrait"`
	xlsxBasePageMargin = `<pageMargins left="0.7" right="0.7" top="0.75" bottom="0.75" header="0.3" footer="0.3"/>`
)

// xlsxPageMargins returns the pageMargins of a margin set (ECMA-376 Part 1,
// 18.3.1.62), in inches.
func xlsxPageMargins(m marginSet) string {
	return fmt.Sprintf(`<pageMargins left="%s" right="%s" top="%s" bottom="%s" header="%s" footer="%s"/>`,
		formatFloat(m.left), formatFloat(m.right), formatFloat(m.top), formatFloat(m.bottom), formatFloat(m.header), formatFloat(m.footer))
}

// xlsxPaperDocs are the XLSX paper documents: paper-grid on each paper in
// each orientation, paper-fit on the papers of [paperSpan] around Excel's
// default in each, paper-grid with no paperSize in each orientation, which
// Excel prints on its default paper, paper-grid with each paper and
// paper-fit with the papers of its span named as options, borrowing the
// reference of the workbook of that paper, and paper-grid with each margin
// set on the papers of the span in each orientation.
func xlsxPaperDocs() []paperDoc {
	span := paperSpan(defaultPaperMicrosoft)
	var docs []paperDoc
	sheet := func(base, name, setup, margins string) paperDoc {
		return paperDoc{
			name: name,
			base: base,
			edit: func(files map[string][]byte) error {
				if err := replaceOnce(files, xlsxPaperPart, xlsxBasePageSetup, setup); err != nil {
					return err
				}
				if margins == "" {
					return nil
				}
				return replaceOnce(files, xlsxPaperPart, xlsxBasePageMargin, margins)
			},
		}
	}
	setup := func(p paperSize, o string) string { return fmt.Sprintf(`paperSize="%d" orientation="%s"`, p.code, o) }
	for _, p := range paperSizes {
		for _, o := range orientations {
			docs = append(docs, sheet(xlsxGridBase, fmt.Sprintf("%s-%s-%s", xlsxGridBase, p.key, o), setup(p, o), ""))
		}
	}
	for _, key := range span {
		for _, o := range orientations {
			docs = append(docs, sheet(xlsxFitBase, fmt.Sprintf("%s-%s-%s", xlsxFitBase, key, o), setup(paperByKey(key), o), ""))
		}
	}
	// paperSize is optional (18.3.1.63): a sheet without it prints on the
	// application's default paper.
	for _, o := range orientations {
		docs = append(docs, sheet(xlsxGridBase, fmt.Sprintf("%s-default-%s", xlsxGridBase, o), fmt.Sprintf(`orientation="%s"`, o), ""))
	}
	for _, b := range []struct {
		base   string
		papers []string
	}{{xlsxGridBase, paperKeys()}, {xlsxFitBase, span}} {
		for _, key := range b.papers {
			for _, o := range orientations {
				docs = append(docs, paperDoc{
					name:    fmt.Sprintf("%s-option-%s-%s", b.base, key, o),
					base:    b.base,
					options: paperOptions(sheetPaper(paperByKey(key)), o == "landscape", fmt.Sprintf("%s-%s-%s", b.base, key, o)),
				})
			}
		}
	}
	for _, m := range marginSets {
		for _, key := range span {
			for _, o := range orientations {
				docs = append(docs, sheet(xlsxGridBase, fmt.Sprintf("%s-margins-%s-%s-%s", xlsxGridBase, m.key, key, o), setup(paperByKey(key), o), xlsxPageMargins(m)))
			}
		}
	}
	return docs
}

// jisB5 is the paper Excel prints paperSize code 13 on: JIS B5, 182 by 257
// mm (see [paperSizes]).
var jisB5 = paperSize{key: "b5", w: 182, h: 257, code: 13}

// sheetPaper returns the paper a workbook's sheet prints p on: JIS B5 for
// B5, p for the others. An option fixture names it, so that it asks for
// the paper of the workbook whose reference it borrows.
//
// # Corpus evidence
//
//   - litho/xlsx paper-grid-b5-portrait: Excel prints code 13 on 515.88 by
//     728.52 points, so an option of ISO B5 (498.9 by 708.66) scores 0 on
//     pages against its reference, whatever the layout.
func sheetPaper(p paperSize) paperSize {
	if p.code == jisB5.code {
		return jisB5
	}
	return p
}

// paperKeys returns the keys of [paperSizes], in their order.
func paperKeys() []string {
	keys := make([]string, 0, len(paperSizes))
	for _, p := range paperSizes {
		keys = append(keys, p.key)
	}
	return keys
}

// slideSize is a slide size a deck is generated on: its sldSz type
// (ECMA-376 Part 1, 19.7.18 ST_SlideSizeType; "" for a custom size) and its
// sides in EMU.
type slideSize struct {
	key    string
	typ    string
	cx, cy int
}

// emuPerMM is the EMU in a millimetre (ECMA-376 Part 1, 20.1.2.1).
const emuPerMM = 36000

// slideSizes are the decks' slide sizes: one per ST_SlideSizeType value
// but custom, and three common custom sizes. sldSz's cx and cy size the
// slide, its type only names the preset (19.2.1.36). The screen, 35mm,
// overhead and banner presets take PowerPoint's sides; the paper presets
// take their paper's sides, landscape, as PowerPoint's A4 preset does (297
// by 210 mm); the Hagaki card is the 100 by 148 mm Japanese postcard,
// portrait.
var slideSizes = []slideSize{
	{"screen4x3", "screen4x3", 9144000, 6858000},
	{"letter", "letter", 279.4 * emuPerMM, 215.9 * emuPerMM},
	{"a4", "A4", 297 * emuPerMM, 210 * emuPerMM},
	{"35mm", "35mm", 10287000, 6858000},
	{"overhead", "overhead", 9144000, 6858000},
	{"banner", "banner", 7315200, 914400},
	{"ledger", "ledger", 431.8 * emuPerMM, 279.4 * emuPerMM},
	{"a3", "A3", 420 * emuPerMM, 297 * emuPerMM},
	{"b4iso", "B4ISO", 353 * emuPerMM, 250 * emuPerMM},
	{"b5iso", "B5ISO", 250 * emuPerMM, 176 * emuPerMM},
	{"b4jis", "B4JIS", 364 * emuPerMM, 257 * emuPerMM},
	{"b5jis", "B5JIS", 257 * emuPerMM, 182 * emuPerMM},
	{"hagaki", "hagakiCard", 100 * emuPerMM, 148 * emuPerMM},
	{"screen16x9", "screen16x9", 9144000, 5143500},
	{"screen16x10", "screen16x10", 9144000, 5715000},
	{"widescreen", "", 12192000, 6858000},
	{"a4-portrait", "", 210 * emuPerMM, 297 * emuPerMM},
	{"letter-portrait", "", 6858000, 9144000},
}

// The slide size of the PPTX base, paper-deck: 4:3 On-screen Show.
const (
	pptxPaperBase = "paper-deck"
	pptxPaperPart = "ppt/presentation.xml"
	pptxBaseSldSz = `<p:sldSz cx="9144000" cy="6858000" type="screen4x3"/>`
)

// pptxPaperDocs are the PPTX paper documents: paper-deck on each slide size
// of [slideSizes]. None takes an option: no engine re-papers a deck.
func pptxPaperDocs() []paperDoc {
	docs := make([]paperDoc, 0, len(slideSizes))
	for _, s := range slideSizes {
		sldSz := fmt.Sprintf(`<p:sldSz cx="%d" cy="%d"/>`, s.cx, s.cy)
		if s.typ != "" {
			sldSz = fmt.Sprintf(`<p:sldSz cx="%d" cy="%d" type="%s"/>`, s.cx, s.cy, s.typ)
		}
		docs = append(docs, paperDoc{
			name: "paper-deck-" + s.key,
			base: pptxPaperBase,
			edit: func(files map[string][]byte) error {
				return replaceOnce(files, pptxPaperPart, pptxBaseSldSz, sldSz)
			},
		})
	}
	return docs
}

// buildPaperDocs writes the paper documents of a build corpus into
// <out>/<name>/<name>.<format>, each zipped from its base's parts under
// partsDir as edited, with its options.json beside it, and returns how many
// it wrote.
func buildPaperDocs(ctx context.Context, corpus, partsDir, out, format string) (int, error) {
	docs := paperDocs(corpus)
	bases := map[string]map[string][]byte{}
	for _, d := range docs {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		base, ok := bases[d.base]
		if !ok {
			var err error
			if base, err = readParts(filepath.Join(partsDir, d.base)); err != nil {
				return 0, fmt.Errorf("%s starts from %s: %w", d.name, d.base, err)
			}
			bases[d.base] = base
		}
		data, err := paperDocBytes(d, base)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", d.name, err)
		}
		if err := writeBuilt(out, d.name, format, data); err != nil {
			return 0, err
		}
		if d.options != nil {
			if err := os.WriteFile(filepath.Join(out, d.name, fixture.OptionsFile), d.options, 0o644); err != nil { //nolint:gosec // testdata is world-readable
				return 0, err
			}
		}
	}
	return len(docs), nil
}

// paperDocBytes returns the package of a paper document: its base's parts,
// edited, zipped as [zipPackage] zips them. base is left unchanged.
func paperDocBytes(d paperDoc, base map[string][]byte) ([]byte, error) {
	files := cloneParts(base)
	if d.edit != nil {
		if err := d.edit(files); err != nil {
			return nil, err
		}
	}
	return zipPackage(files)
}

// flatPapersDir is the directory, under a corpus's testdata root, a corpus
// whose sources are committed rather than built gets its paper documents
// in, one directory each.
const flatPapersDir = "papers"

// hasFlatPapers reports whether m's paper documents are written from its
// committed sources (an OpenDocument or HTML corpus) rather than zipped
// beside the documents of a build corpus.
func hasFlatPapers(m *Manifest) bool {
	return !isBuildCorpus(m) && len(paperDocs(m.Name)) > 0
}

// flatPapersOut returns the directory m's flat paper documents are written
// into.
func flatPapersOut(m *Manifest) string {
	return filepath.Join(m.TestdataRoot(), flatPapersDir)
}

// buildFlatPaperDocs writes the paper documents of a corpus with committed
// sources into <testdata root>/papers/ (see [writeFlatPaperDocs]),
// replacing what an earlier build wrote there.
func buildFlatPaperDocs(ctx context.Context, m *Manifest) error {
	out := flatPapersOut(m)
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	n, err := writeFlatPaperDocs(ctx, m.Name, filepath.Join(m.SourceDir(), "sources"), out)
	if err != nil {
		return err
	}
	fmt.Printf("%s: %d paper documents built into %s\n", m.Name, n, out)
	return nil
}

// writeFlatPaperDocs writes each paper document of corpus into
// <out>/<name>/: every file of its base's directory under sourcesDir but
// options.json, as edited, and its own options.json, and returns how many
// it wrote.
func writeFlatPaperDocs(ctx context.Context, corpus, sourcesDir, out string) (int, error) {
	docs := paperDocs(corpus)
	bases := map[string]map[string][]byte{}
	for _, d := range docs {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		base, ok := bases[d.base]
		if !ok {
			var err error
			if base, err = readParts(filepath.Join(sourcesDir, d.base)); err != nil {
				return 0, fmt.Errorf("%s starts from %s: %w", d.name, d.base, err)
			}
			bases[d.base] = base
		}
		files := cloneParts(base)
		if d.edit != nil {
			if err := d.edit(files); err != nil {
				return 0, fmt.Errorf("%s: %w", d.name, err)
			}
		}
		if d.options != nil {
			files[fixture.OptionsFile] = d.options
		}
		dir := filepath.Join(out, d.name)
		for _, name := range packageOrder(files) {
			p := filepath.Join(dir, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return 0, err
			}
			if err := os.WriteFile(p, files[name], 0o644); err != nil { //nolint:gosec // testdata is world-readable
				return 0, err
			}
		}
	}
	return len(docs), nil
}

// flatPaperFixtures lists the paper documents of a corpus with committed
// sources, each the directory it was built into, refusing one not built
// yet.
func flatPaperFixtures(m *Manifest) ([]Fixture, error) {
	if !hasFlatPapers(m) {
		return nil, nil
	}
	names := paperDocNames(m.Name)
	fixtures := make([]Fixture, 0, len(names))
	for _, name := range names {
		dir := filepath.Join(flatPapersOut(m), name)
		if _, err := os.Stat(dir); err != nil {
			return nil, fmt.Errorf("%w: %s is missing (run make corpus-build CORPUS=%s)", errInputsUnfetched, dir, m.Name)
		}
		fixtures = append(fixtures, Fixture{ID: name, PDFPath: dir})
	}
	return fixtures, nil
}

// flatPaperReferenceJobs lists the paper documents of a corpus with
// committed sources that have a reference of their own, the ones only names
// when set.
func flatPaperReferenceJobs(m *Manifest, only []string) []referenceJob {
	if !hasFlatPapers(m) {
		return nil
	}
	var jobs []referenceJob
	for _, name := range paperDocNames(m.Name) {
		if len(only) > 0 && !slices.Contains(only, name) {
			continue
		}
		dir := filepath.Join(flatPapersOut(m), name)
		borrows, litho, fonts, locale := sourceReference(dir)
		if borrows {
			continue
		}
		jobs = append(jobs, referenceJob{name, dir, optionsPath(dir), flatPapersDir + "/" + name, litho, fonts, locale})
	}
	return jobs
}

// The page setup of the OpenDocument bases: the page layout pm1, portrait
// Letter, that the Standard (text) or Default (spreadsheet) master page
// uses; paper-base also holds pm2, of the master page Second, which no
// paragraph uses until a mixed document's PartTwo style names it (ODF 1.3
// Part 3, 16.5 style:page-layout, 16.9 style:master-page-name). The page
// margins follow the orientation in each page layout.
const (
	odfBasePageLayout = `style:name="%s"><style:page-layout-properties fo:page-width="8.5in" fo:page-height="11in" style:print-orientation="portrait"`
	odtPaperBase      = "paper-base"
	odtPaperPart      = "paper-base.fodt"
	odtBaseMargins    = ` fo:margin-top="0.5in" fo:margin-bottom="0.5in" fo:margin-left="1in" fo:margin-right="1in"`
	odtPartTwo        = `<style:style style:name="PartTwo" style:family="paragraph" style:parent-style-name="Heading_20_1"/>`
	odsGridPart       = "paper-grid.fods"
	odsFitPart        = "paper-fit.fods"
	odsBaseMargins    = ` fo:margin-top="0.75in" fo:margin-bottom="0.75in" fo:margin-left="0.7in" fo:margin-right="0.7in"`
)

// odfPageLayout returns the start of a page layout named name on paper p,
// turned by landscape: fo:page-width and fo:page-height as the page is
// turned, and style:print-orientation (ODF 1.3 Part 3, 17.2
// style:page-layout-properties).
func odfPageLayout(name string, p paperSize, landscape bool) string {
	w, h, o := p.w, p.h, "portrait"
	if landscape {
		w, h, o = p.h, p.w, "landscape"
	}
	return fmt.Sprintf(`style:name="%s"><style:page-layout-properties fo:page-width="%smm" fo:page-height="%smm" style:print-orientation="%s"`,
		name, formatFloat(w), formatFloat(h), o)
}

// odfRepaper returns an edit that lays the page layout name of part out on
// p, turned by landscape.
func odfRepaper(part, name string, p paperSize, landscape bool) func(map[string][]byte) error {
	return func(files map[string][]byte) error {
		return replaceOnce(files, part, fmt.Sprintf(odfBasePageLayout, name), odfPageLayout(name, p, landscape))
	}
}

// odfMargins returns the page margins of a margin set as a page layout
// states them, fo:margin-top, -bottom, -left and -right (ODF 1.3 Part 3,
// 17.2 style:page-layout-properties). The set's header and footer distances
// have no counterpart: an OpenDocument header lies inside the page margins,
// its height and spacing taken from the body (17.4
// style:header-footer-properties), so a margin document keeps the base's
// header and footer styles.
func odfMargins(m marginSet) string {
	return fmt.Sprintf(` fo:margin-top="%sin" fo:margin-bottom="%sin" fo:margin-left="%sin" fo:margin-right="%sin"`,
		formatFloat(m.top), formatFloat(m.bottom), formatFloat(m.left), formatFloat(m.right))
}

// odfRemargin returns an edit that lays the page layout pm1 of part out on
// p, turned by landscape, with the margins of m in place of base's.
func odfRemargin(part, base string, p paperSize, landscape bool, m marginSet) func(map[string][]byte) error {
	return func(files map[string][]byte) error {
		layout := odfPageLayout("pm1", p, landscape)
		if err := odfRepaper(part, "pm1", p, landscape)(files); err != nil {
			return err
		}
		return replaceOnce(files, part, layout+base, layout+odfMargins(m))
	}
}

// odfUnpaper returns an edit that drops fo:page-width and fo:page-height
// from the page layout pm1 of part, keeping its orientation, turned by
// landscape: both are optional attributes of
// style:page-layout-properties (ODF 1.3 Part 3, 17.2), so the application
// chooses the paper.
//
// # Corpus evidence
//
//   - litho/odt paper-default-landscape and litho/ods
//     paper-grid-default-landscape: the LibreOffice route of
//     gotenberg/gotenberg:9.4.1 prints them on A4 portrait, as the
//     portrait documents: an orientation without a size turns nothing.
func odfUnpaper(part string, landscape bool) func(map[string][]byte) error {
	o := "portrait"
	if landscape {
		o = "landscape"
	}
	return func(files map[string][]byte) error {
		return replaceOnce(files, part, fmt.Sprintf(odfBasePageLayout, "pm1"),
			`style:name="pm1"><style:page-layout-properties style:print-orientation="`+o+`"`)
	}
}

// odtPaperDocs are the ODT paper documents: paper-base on each paper in
// each orientation, the base with each of those papers named as options,
// two documents of two page styles, the second from the second Heading 1,
// the base with no paper in each orientation, which LibreOffice prints on
// its default paper, and the base with each margin set on the papers of
// [paperSpan] around LibreOffice's default in each orientation. The
// LibreOffice route takes the options, so an option fixture has a
// reference of its own.
func odtPaperDocs() []paperDoc {
	var docs []paperDoc
	for _, p := range paperSizes {
		for _, o := range orientations {
			docs = append(docs, paperDoc{
				name: fmt.Sprintf("paper-%s-%s", p.key, o),
				base: odtPaperBase,
				edit: odfRepaper(odtPaperPart, "pm1", p, o == "landscape"),
			})
		}
	}
	for _, p := range paperSizes {
		for _, o := range orientations {
			docs = append(docs, paperDoc{
				name:    fmt.Sprintf("paper-option-%s-%s", p.key, o),
				base:    odtPaperBase,
				options: paperOptions(p, o == "landscape", ""),
			})
		}
	}
	twoStyles := func(first, second paperSize, secondLandscape bool) func(map[string][]byte) error {
		return func(files map[string][]byte) error {
			if err := odfRepaper(odtPaperPart, "pm1", first, false)(files); err != nil {
				return err
			}
			if err := odfRepaper(odtPaperPart, "pm2", second, secondLandscape)(files); err != nil {
				return err
			}
			return replaceOnce(files, odtPaperPart, odtPartTwo, strings.Replace(odtPartTwo, "/>", ` style:master-page-name="Second"/>`, 1))
		}
	}
	letter, a4, a5 := paperByKey("letter"), paperByKey("a4"), paperByKey("a5")
	docs = append(docs,
		paperDoc{name: "paper-mixed-orientation", base: odtPaperBase, edit: twoStyles(letter, letter, true)},
		paperDoc{name: "paper-mixed-sizes", base: odtPaperBase, edit: twoStyles(a4, a5, false)},
	)
	for _, o := range orientations {
		docs = append(docs, paperDoc{name: "paper-default-" + o, base: odtPaperBase, edit: odfUnpaper(odtPaperPart, o == "landscape")})
	}
	for _, m := range marginSets {
		for _, key := range paperSpan(defaultPaperLibreOffice) {
			for _, o := range orientations {
				docs = append(docs, paperDoc{
					name: fmt.Sprintf("paper-margins-%s-%s-%s", m.key, key, o),
					base: odtPaperBase,
					edit: odfRemargin(odtPaperPart, odtBaseMargins, paperByKey(key), o == "landscape", m),
				})
			}
		}
	}
	return docs
}

// odsPaperDocs are the ODS paper documents, as [xlsxPaperDocs] makes the
// XLSX ones on LibreOffice's default paper rather than Excel's, the option
// fixtures with references of their own. paper-fit is fitted to one page
// wide (style:scale-to-X 1, style:scale-to-Y 0).
func odsPaperDocs() []paperDoc {
	span := paperSpan(defaultPaperLibreOffice)
	var docs []paperDoc
	for _, p := range paperSizes {
		for _, o := range orientations {
			docs = append(docs, paperDoc{name: fmt.Sprintf("%s-%s-%s", xlsxGridBase, p.key, o), base: xlsxGridBase, edit: odfRepaper(odsGridPart, "pm1", p, o == "landscape")})
		}
	}
	for _, key := range span {
		for _, o := range orientations {
			docs = append(docs, paperDoc{name: fmt.Sprintf("%s-%s-%s", xlsxFitBase, key, o), base: xlsxFitBase, edit: odfRepaper(odsFitPart, "pm1", paperByKey(key), o == "landscape")})
		}
	}
	for _, o := range orientations {
		docs = append(docs, paperDoc{name: fmt.Sprintf("%s-default-%s", xlsxGridBase, o), base: xlsxGridBase, edit: odfUnpaper(odsGridPart, o == "landscape")})
	}
	for _, b := range []struct {
		base   string
		papers []string
	}{{xlsxGridBase, paperKeys()}, {xlsxFitBase, span}} {
		for _, key := range b.papers {
			for _, o := range orientations {
				docs = append(docs, paperDoc{
					name:    fmt.Sprintf("%s-option-%s-%s", b.base, key, o),
					base:    b.base,
					options: paperOptions(paperByKey(key), o == "landscape", ""),
				})
			}
		}
	}
	for _, m := range marginSets {
		for _, key := range span {
			for _, o := range orientations {
				docs = append(docs, paperDoc{
					name: fmt.Sprintf("%s-margins-%s-%s-%s", xlsxGridBase, m.key, key, o),
					base: xlsxGridBase,
					edit: odfRemargin(odsGridPart, odsBaseMargins, paperByKey(key), o == "landscape", m),
				})
			}
		}
	}
	return docs
}

// odpSize is a slide size Impress offers in its slide properties, in
// centimetres.
type odpSize struct {
	key  string
	w, h float64
}

// odpSizes are the ODP decks' slide sizes: Impress's screen formats (4:3,
// 16:9, 16:10, Widescreen and the On-screen Show ones), its paper formats
// in landscape (A3, A4, A5, B5 (ISO), Letter, Legal, Tabloid), the 36 by 24
// mm Dia Slide, and the Japanese Postcard and A4 portrait.
var odpSizes = []odpSize{
	{"screen-4x3", 28, 21},
	{"screen-16x9", 28, 15.75},
	{"screen-16x10", 28, 17.5},
	{"widescreen", 33.867, 19.05},
	{"onscreen-4x3", 25.4, 19.05},
	{"onscreen-16x9", 25.4, 14.288},
	{"onscreen-16x10", 25.4, 15.875},
	{"a3", 42, 29.7},
	{"a4", 29.7, 21},
	{"a5", 21, 14.8},
	{"b5iso", 25, 17.6},
	{"letter", 27.94, 21.59},
	{"legal", 35.56, 21.59},
	{"tabloid", 43.18, 27.94},
	{"dia", 3.6, 2.4},
	{"postcard", 10, 14.8},
	{"a4-portrait", 21, 29.7},
}

// The slide size of the ODP base, paper-deck: Screen 4:3, the page layout
// of its Default master page.
const (
	odpPaperBase   = "paper-deck"
	odpPaperPart   = "paper-deck.fodp"
	odpBaseSlideSz = `fo:page-width="28cm" fo:page-height="21cm" style:print-orientation="landscape"`
)

// odpPaperDocs are the ODP paper documents: paper-deck on each slide size of
// [odpSizes]. None takes an option: Impress prints a slide on a page of its
// own size whatever the route asks.
func odpPaperDocs() []paperDoc {
	docs := make([]paperDoc, 0, len(odpSizes))
	for _, s := range odpSizes {
		o := "landscape"
		if s.h > s.w {
			o = "portrait"
		}
		size := fmt.Sprintf(`fo:page-width="%scm" fo:page-height="%scm" style:print-orientation="%s"`,
			strconv.FormatFloat(s.w, 'f', -1, 64), strconv.FormatFloat(s.h, 'f', -1, 64), o)
		docs = append(docs, paperDoc{
			name: "paper-deck-" + s.key,
			base: odpPaperBase,
			edit: func(files map[string][]byte) error {
				return replaceOnce(files, odpPaperPart, odpBaseSlideSz, size)
			},
		})
	}
	return docs
}

// cssPageSizes are the page-size keywords of CSS Paged Media Level 3, 7.2.1
// (Table 1), as an @page size names them.
var cssPageSizes = []string{"A5", "A4", "A3", "B5", "B4", "JIS-B5", "JIS-B4", "letter", "legal", "ledger"}

// cssLengthSizes are two @page sizes in lengths: a narrow page in inches
// and a wide one in centimetres (CSS Paged Media Level 3, 7.2.1).
var cssLengthSizes = []struct{ key, size string }{
	{"lengths-narrow", "6in 9in"},
	{"lengths-wide", "25cm 15cm"},
}

// The page rule of the HTML base, paper-base, and the Markdown base.
const (
	htmlPaperBase     = "paper-base"
	htmlPaperPart     = "index.html"
	htmlBasePageRule  = "@page { size: auto; }"
	markdownPaperBase = "paper-markdown"
)

// htmlPaperDocs are the HTML and Markdown paper documents: paper-base on
// each paper in each orientation through the route's paperWidth,
// paperHeight and landscape, and on the corpus's default paper turned by
// landscape alone; paper-base with an @page size of each keyword, with and
// without landscape, and of two sizes in lengths, each once with
// preferCssPageSize and once without; paper-markdown alike on each paper
// and on the default one turned; paper-base with each margin set, through
// marginTop and its siblings, on the papers of [paperSpan] around the
// corpus's default, which it leaves out of the options, in each
// orientation; and paper-markdown with each margin set on the default
// paper in each orientation, since Markdown reaches the page through the
// same layout as HTML.
func htmlPaperDocs() []paperDoc {
	var docs []paperDoc
	for _, p := range paperSizes {
		for _, o := range orientations {
			docs = append(docs, paperDoc{
				name:    fmt.Sprintf("paper-option-%s-%s", p.key, o),
				base:    htmlPaperBase,
				options: paperOptions(p, o == "landscape", ""),
			})
		}
	}
	docs = append(docs, paperDoc{name: "paper-default-landscape", base: htmlPaperBase, options: pageOptions(nil, true, nil, "")})
	css := func(key, size string) {
		rule := "@page { size: " + size + "; }"
		for _, prefer := range []bool{true, false} {
			mode := "prefer"
			if !prefer {
				mode = "paper"
			}
			docs = append(docs, paperDoc{
				name: fmt.Sprintf("paper-css-%s-%s", key, mode),
				base: htmlPaperBase,
				edit: func(files map[string][]byte) error {
					return replaceOnce(files, htmlPaperPart, htmlBasePageRule, rule)
				},
				options: []byte(fmt.Sprintf("{\n  \"preferCssPageSize\": %t\n}\n", prefer)),
			})
		}
	}
	for _, kw := range cssPageSizes {
		key := strings.ToLower(kw)
		css(key+"-portrait", kw)
		css(key+"-landscape", kw+" landscape")
	}
	for _, l := range cssLengthSizes {
		css(l.key, l.size)
	}
	for _, p := range paperSizes {
		for _, o := range orientations {
			docs = append(docs, paperDoc{
				name:    fmt.Sprintf("paper-markdown-%s-%s", p.key, o),
				base:    markdownPaperBase,
				options: paperOptions(p, o == "landscape", ""),
			})
		}
	}
	docs = append(docs, paperDoc{name: "paper-markdown-default-landscape", base: markdownPaperBase, options: pageOptions(nil, true, nil, "")})
	for _, m := range marginSets {
		for _, key := range paperSpan(defaultPaperChromium) {
			var paper *paperSize
			if key != defaultPaperChromium {
				p := paperByKey(key)
				paper = &p
			}
			for _, o := range orientations {
				docs = append(docs, paperDoc{
					name:    fmt.Sprintf("paper-margins-%s-%s-%s", m.key, key, o),
					base:    htmlPaperBase,
					options: pageOptions(paper, o == "landscape", &m, ""),
				})
			}
		}
	}
	for _, m := range marginSets {
		for _, o := range orientations {
			docs = append(docs, paperDoc{
				name:    fmt.Sprintf("paper-markdown-margins-%s-%s", m.key, o),
				base:    markdownPaperBase,
				options: pageOptions(nil, o == "landscape", &m, ""),
			})
		}
	}
	return docs
}

// The CSV base, paper-table: the cells of paper-grid, 20 columns by 130
// rows, as a CSV file, which names no paper.
const csvPaperBase = "paper-table"

// csvPaperDocs are the CSV paper documents: paper-table with each paper
// named as options in each orientation, and on the default paper turned by
// landscape alone. A CSV file states no page setup (RFC 4180 defines
// records and fields alone), so it has no paper documents of its own and no
// margins, and the LibreOffice route has no margin field: the options are
// the only paper a CSV conversion takes, and the LibreOffice route takes
// them, so each option fixture has a reference of its own.
func csvPaperDocs() []paperDoc {
	var docs []paperDoc
	for _, p := range paperSizes {
		for _, o := range orientations {
			docs = append(docs, paperDoc{
				name:    fmt.Sprintf("%s-option-%s-%s", csvPaperBase, p.key, o),
				base:    csvPaperBase,
				options: paperOptions(p, o == "landscape", ""),
			})
		}
	}
	return append(docs, paperDoc{name: csvPaperBase + "-default-landscape", base: csvPaperBase, options: pageOptions(nil, true, nil, "")})
}
