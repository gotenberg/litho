package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
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
	// code is the paperSize code SpreadsheetML gives the paper (ECMA-376
	// Part 1, 18.3.1.63, the table of paperSize values).
	code int
	// css is the size keyword CSS Paged Media names the paper with (CSS
	// Paged Media Level 3, 7.2.1, Table 1), "" when it names none.
	css string
}

// paperSizes are the papers every format is laid out on: the ISO 216 A3,
// A4 and A5, the ISO 216 B5, and the North American Letter, Legal, Tabloid
// (ANSI B) and Executive, at the sides ISO 216 and ANSI/ASME Y14.1 give
// them. Their SpreadsheetML codes are 8, 9, 11, 34, 1, 5, 3 and 7.
//
//nolint:gochecknoglobals // static table
var paperSizes = []paperSize{
	{key: "a3", w: 297, h: 420, code: 8, css: "A3"},
	{key: "a4", w: 210, h: 297, code: 9, css: "A4"},
	{key: "a5", w: 148, h: 210, code: 11, css: "A5"},
	{key: "b5", w: 176, h: 250, code: 34, css: "B5"},
	{key: "letter", w: 215.9, h: 279.4, code: 1, css: "letter"},
	{key: "legal", w: 215.9, h: 355.6, code: 5, css: "legal"},
	{key: "tabloid", w: 279.4, h: 431.8, code: 3, css: "ledger"},
	{key: "executive", w: 184.15, h: 266.7, code: 7},
}

// paperSpan are the papers whose layout is drawn and compared through the
// options and the fitted sheets: the smallest, the references' default and
// the largest common paper, so a layout that holds on these holds between.
//
//nolint:gochecknoglobals // static list
var paperSpan = []string{"a5", "letter", "a3"}

// orientations are the two ways a paper is turned, as fixture names spell
// them.
//
//nolint:gochecknoglobals // static list
var orientations = []string{"portrait", "landscape"}

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
	var b strings.Builder
	fmt.Fprintf(&b, "{\n  \"paperWidth\": %s,\n  \"paperHeight\": %s", formatInches(p.w), formatInches(p.h))
	if landscape {
		b.WriteString(",\n  \"landscape\": true")
	}
	if ref != "" {
		fmt.Fprintf(&b, ",\n  \"reference_of\": %q", ref)
	}
	b.WriteString("\n}\n")
	return []byte(b.String())
}

// formatInches spells a paper side in inches as an options.json does.
func formatInches(mm float64) string { return strconv.FormatFloat(inches(mm), 'f', -1, 64) }

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
// Letter, whose sectPr closes the body (ECMA-376 Part 1, 17.6.17).
const (
	docxPaperBase     = "paper-base"
	docxPaperPart     = "word/document.xml"
	docxBasePgSz      = `<w:pgSz w:w="12240" w:h="15840"/>`
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

// docxPaperDocs are the DOCX paper documents: paper-base on each paper in
// each orientation, the base with the paper of [paperSpan] named as an
// option, borrowing the reference of the document of that paper, and two
// documents of two sections, the second starting at the second Heading 1.
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
	for _, key := range paperSpan {
		for _, o := range orientations {
			docs = append(docs, paperDoc{
				name:    fmt.Sprintf("paper-option-%s-%s", key, o),
				base:    docxPaperBase,
				options: paperOptions(paperByKey(key), o == "landscape", fmt.Sprintf("paper-%s-%s", key, o)),
			})
		}
	}
	letter, a4, a5 := paperByKey("letter"), paperByKey("a4"), paperByKey("a5")
	docs = append(docs,
		paperDoc{name: "paper-mixed-orientation", base: docxPaperBase, edit: docxTwoSections(docxPgSz(letter, false), docxPgSz(letter, true))},
		paperDoc{name: "paper-mixed-sizes", base: docxPaperBase, edit: docxTwoSections(docxPgSz(a4, false), docxPgSz(a5, false))},
	)
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
// (paperSize 1), paper-fit fitted to one page wide (fitToWidth 1,
// fitToHeight 0: as many pages down as it needs, ECMA-376 Part 1,
// 18.3.1.63).
const (
	xlsxGridBase      = "paper-grid"
	xlsxFitBase       = "paper-fit"
	xlsxPaperPart     = "xl/worksheets/sheet1.xml"
	xlsxBasePageSetup = `paperSize="1" orientation="portrait"`
)

// xlsxPaperDocs are the XLSX paper documents: paper-grid on each paper in
// each orientation, paper-fit on the papers of [paperSpan] in each, and
// both bases with those papers named as options, borrowing the reference of
// the workbook of that paper.
func xlsxPaperDocs() []paperDoc {
	var docs []paperDoc
	sheet := func(base string, p paperSize, o string) paperDoc {
		setup := fmt.Sprintf(`paperSize="%d" orientation="%s"`, p.code, o)
		return paperDoc{
			name: fmt.Sprintf("%s-%s-%s", base, p.key, o),
			base: base,
			edit: func(files map[string][]byte) error {
				return replaceOnce(files, xlsxPaperPart, xlsxBasePageSetup, setup)
			},
		}
	}
	for _, p := range paperSizes {
		for _, o := range orientations {
			docs = append(docs, sheet(xlsxGridBase, p, o))
		}
	}
	for _, key := range paperSpan {
		for _, o := range orientations {
			docs = append(docs, sheet(xlsxFitBase, paperByKey(key), o))
		}
	}
	for _, base := range []string{xlsxGridBase, xlsxFitBase} {
		for _, key := range paperSpan {
			for _, o := range orientations {
				docs = append(docs, paperDoc{
					name:    fmt.Sprintf("%s-option-%s-%s", base, key, o),
					base:    base,
					options: paperOptions(paperByKey(key), o == "landscape", fmt.Sprintf("%s-%s-%s", base, key, o)),
				})
			}
		}
	}
	return docs
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
//
//nolint:gochecknoglobals // static table
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
