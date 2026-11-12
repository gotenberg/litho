package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/gotenberg/litho/pkg/pdf"
)

// metricReferenceFontsMissing counts the fixtures the diff cell leaves
// unscored because their reference draws a face no fixture font draws as it
// does (see [missingReferenceFaces]). An inventory, not gated.
const metricReferenceFontsMissing = "reference_fonts_missing"

// baseFontPattern matches the /BaseFont entry of a font dictionary written
// outside an object stream, capturing the name's bytes as the file spells
// them (ISO 32000-1, 7.3.5: a name ends at white space or a delimiter).
var baseFontPattern = regexp.MustCompile(`/BaseFont\s*/([^\s/\[\]<>(){}%]+)`)

// referenceFonts lists the faces a reference PDF's fonts name by their
// /BaseFont (ISO 32000-1, 9.6 and 9.7), without the subset tag (9.6.4),
// distinct and sorted. litho reads the file and rewrites it whole, which
// writes every font dictionary outside an object stream, where the names
// are read from.
func referenceFonts(ctx context.Context, path string) ([]string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // provisioned reference under the corpus testdata root
	if err != nil {
		return nil, err
	}
	r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer r.Close()
	w := pdf.NewWriter(r, pdf.WithFullRewrite())
	defer w.Close() //nolint:errcheck // Close removes temp files; a failure leaves one behind and changes no output
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		return nil, fmt.Errorf("rewrite: %w", err)
	}
	seen := map[string]bool{}
	var faces []string
	for _, m := range baseFontPattern.FindAllSubmatch(out.Bytes(), -1) {
		name := stripSubsetTag(decodeName(string(m[1])))
		if name != "" && !seen[name] {
			seen[name] = true
			faces = append(faces, name)
		}
	}
	slices.Sort(faces)
	return faces, nil
}

// decodeName decodes the #xx escapes of a name's bytes (ISO 32000-1,
// 7.3.5). A malformed escape is kept as written.
func decodeName(s string) string {
	if !strings.Contains(s, "#") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '#' && i+2 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
				b.WriteByte(byte(v))
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// stripSubsetTag removes the six capital letters and plus sign a subset
// font's name starts with (ISO 32000-1, 9.6.4).
func stripSubsetTag(name string) string {
	if len(name) > 7 && name[6] == '+' {
		for i := range 6 {
			if name[i] < 'A' || name[i] > 'Z' {
				return name
			}
		}
		return name[7:]
	}
	return name
}

// fourStyles are the styles a metric-compatible stand-in covers: regular,
// bold, italic and bold italic, as a PostScript name's style part reads
// once folded.
var fourStyles = []string{"", "bold", "italic", "bolditalic", "oblique", "boldoblique"}

// standIn is the fixture family that draws a face the fixture fonts lack,
// as a PostScript name's family part reads once folded, and the styles it
// stands in for.
type standIn struct {
	family string
	styles []string
}

// standIns maps the faces the fixture fonts have a metric-compatible stand-in
// for, by folded family: Carlito for Calibri and Caladea for Cambria, each in
// four styles only (Calibri Light is another family's metrics), Liberation
// Sans, Serif and Mono for Arial, Times New Roman and Courier New, and for
// Helvetica, Times and Courier, the standard 14 fonts whose metrics those
// three share. These are the substitutions of the Gotenberg image's
// fontconfig (conf.d/30-metric-aliases.conf).
var standIns = map[string]standIn{
	"calibri":       {"carlito", fourStyles},
	"cambria":       {"caladea", fourStyles},
	"arial":         {"liberationsans", fourStyles},
	"timesnewroman": {"liberationserif", fourStyles},
	"couriernew":    {"liberationmono", fourStyles},
	"helvetica":     {"liberationsans", fourStyles},
	"times":         {"liberationserif", append([]string{"roman"}, fourStyles...)},
	"courier":       {"liberationmono", fourStyles},
}

// symbolFaces are the symbol fonts the converters recode into OpenSymbol,
// as LibreOffice does (the families pkg/internal/symbolfont's tables name):
// a glyph of theirs is drawn from OpenSymbol, a fixture font.
var symbolFaces = []string{
	"dingbats", "itczapfdingbats", "monotypesorts", "mtextra", "standardsymbols", "standardsymbolsl",
	"starbats", "starmath", "symbol", "webdings", "wingdings", "wingdings2", "wingdings3", "zapfdingbats",
}

// embeddedSourceFont is the prefix Word gives a font the document embeds in
// itself: the source carries the program, so the converter draws the face
// the reference draws.
const embeddedSourceFont = "___WRD_EMBED"

// foldFace splits a PostScript font name into its family and style,
// lower-cased: the family before the first hyphen or comma (Calibri-Bold,
// Arial,BoldItalic), with a trailing "MT", "PSMT" or "PS" dropped
// (ArialMT, TimesNewRomanPSMT), and the style after it with "MT" and
// "Regular" dropped (TimesNewRomanPS-BoldMT, Wingdings-Regular).
func foldFace(name string) (family, style string) {
	family, style, _ = strings.Cut(name, "-")
	if f, s, ok := strings.Cut(family, ","); ok {
		family, style = f, s
	}
	family = strings.ToLower(strings.ReplaceAll(family, " ", ""))
	for _, suffix := range []string{"psmt", "mt", "ps"} {
		if f, ok := strings.CutSuffix(family, suffix); ok && f != "" {
			family = f
			break
		}
	}
	style = strings.ToLower(strings.ReplaceAll(style, " ", ""))
	style = strings.TrimSuffix(style, "mt")
	style = strings.TrimSuffix(style, "regular")
	return family, style
}

// faceCovered reports whether the fixture fonts draw a face as the
// reference draws it: a font the source document embeds, a symbol font the
// converters recode, or a face of the fixture fonts' own families or a
// metric-compatible stand-in in a style it has, which holds every character
// of drawn, the text the reference draws in the face. An empty drawn, a face
// whose text the reference draws only where it cannot be attributed, judges
// the face by its name alone.
func faceCovered(name string, drawn []rune, fixture *fixtureFaces) bool {
	if strings.HasPrefix(name, embeddedSourceFont) {
		return true
	}
	family, style := foldFace(name)
	if slices.Contains(symbolFaces, family) {
		return true
	}
	if !fixture.has(family) {
		in, ok := standIns[family]
		if !ok || !slices.Contains(in.styles, style) || !fixture.has(in.family) {
			return false
		}
		family = in.family
	}
	cover := fixture.face(family, style)
	for _, r := range drawn {
		if r >= ' ' && r != unicode.ReplacementChar && !cover.covers(r) {
			return false
		}
	}
	return true
}

// referenceMissingFaces reads the reference PDF at path and returns the faces
// it draws that no face of fixture draws as it does (see
// [missingReferenceFaces]), the characters of each face read with qpdf at
// qpdf; an empty qpdf judges every face by its name alone.
func referenceMissingFaces(ctx context.Context, qpdf, path string, fixture *fixtureFaces) ([]string, error) {
	faces, err := referenceFonts(ctx, path)
	if err != nil {
		return nil, err
	}
	var drawn map[string][]rune
	if qpdf != "" {
		if drawn, err = referenceFaceText(ctx, qpdf, path); err != nil {
			return nil, fmt.Errorf("read the text of each face: %w", err)
		}
	}
	return missingReferenceFaces(faces, drawn, fixture), nil
}

// missingReferenceFaces returns the faces of a reference no fixture font
// draws as the reference does (see [faceCovered]), in the order of faces,
// the faces the reference names; drawn holds the characters it draws in
// each. No converter can match such a reference's metrics or glyphs, so the
// diff cell leaves its fixture unscored rather than holding litho to it.
//
// # Corpus evidence
//
//   - The Microsoft references that draw a face with no stand-in: litho/docx
//     26 of 162, litho/xlsx 9 of 138, litho/pptx 13 of 133,
//     libreoffice/docx 58 of 341, libreoffice/xlsx 24 of 252,
//     libreoffice/pptx 40 of 311, poi/docx 14 of 118, poi/xlsx 25 of 185,
//     poi/pptx 10 of 52, openxmlsdk/docx 46 of 249, openxmlsdk/xlsx 5 of
//     54, openxmlsdk/pptx 34 of 182: 304. By family, the references each
//     draws in and those it alone keeps unscored: Verdana 35 and 14,
//     Calibri Light 31 and 28, SimSun 27 and 18, MS Gothic 20 and 9,
//     Cambria Math 20 and 16, MS Mincho 20 and 6, Tahoma 19 and 14, Aptos
//     15 and 15, Segoe UI 11 and 10, Arial Black 11 and 10, of 73 families.
//   - The same references, advance widths compared glyph by glyph between
//     the programs they embed and the fixture fonts: Carlito matches
//     Calibri on all 288 shared glyphs but Calibri Light on 17 of 86; no
//     fixture font comes within a unit per em on more than a third of the
//     glyphs of Verdana, Tahoma, Segoe UI, Aptos, Georgia, Arial Black,
//     Trebuchet MS or Cambria Math. Noto Sans Mono CJK matches the advances
//     of MS Gothic (321 of 321), SimSun (2445 of 2449) and MS Mincho (690
//     of 697), but its line is 1.448 em high where theirs is 1 em.
//   - The same corpora, each face's characters read from its ToUnicode
//     CMap and checked against the Gotenberg image's fonts: 11 more
//     references draw, in a face with a stand-in, a character the stand-in
//     lacks, 315 in all. Hebrew or Arabic in Calibri, Arial or Times New
//     Roman: litho/xlsx alignment and rtl_sheet, litho/pptx table-large,
//     libreoffice/pptx oox/tdf125085_WordArtFontText.pptx and
//     WordArtFontTheme.pptx, poi/docx document/rtl.docx, openxmlsdk/docx
//     Comment028, Comment044, Comment045 and Comment066; Arabic ligatures
//     in Calibri: poi/xlsx spreadsheet/DataValidationEvaluations.xlsx.
//     Outside them, govdocs/ppt 000/000725.ppt, 100/100756.ppt,
//     100/100779.ppt, 200/200105.ppt and 400/400354.ppt and poi/ppt
//     slideshow/bug58516.ppt draw a private-use bullet in Calibri. No
//     reference went the other way.
func missingReferenceFaces(faces []string, drawn map[string][]rune, fixture *fixtureFaces) []string {
	var out []string
	for _, f := range faces {
		if !faceCovered(f, drawn[f], fixture) {
			out = append(out, f)
		}
	}
	return out
}
