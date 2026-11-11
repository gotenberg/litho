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

	"github.com/gotenberg/litho/pkg/pdf"
)

// metricReferenceFontsMissing counts the fixtures the diff cell leaves
// unscored because their reference draws a face no fixture font stands in
// for (see [missingReferenceFaces]). An inventory, not gated.
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

// fixtureFontFamilies are the families the fixture fonts install (see
// fixture.FontFamilies), as a PostScript name's family part reads once
// folded (see [foldFace]): any face of them is drawn as the reference draws
// it.
//
//nolint:gochecknoglobals // static family table
var fixtureFontFamilies = []string{
	"dejavusans", "dejavusanscondensed", "dejavusansmono", "dejavuserif", "dejavuserifcondensed",
	"liberationsans", "liberationsansnarrow", "liberationserif", "liberationmono",
	"carlito", "caladea", "opensymbol",
	"notosans", "notoserif", "notosanscjksc", "notosanscjktc", "notosanscjkjp", "notosanscjkkr", "notosanscjkhk",
	"notosansthai", "notosansdevanagari",
}

// fourStyles are the styles a metric-compatible stand-in covers: regular,
// bold, italic and bold italic, as a PostScript name's style part reads
// once folded.
//
//nolint:gochecknoglobals // static style table
var fourStyles = []string{"", "bold", "italic", "bolditalic", "oblique", "boldoblique"}

// standIns maps the faces the fixture fonts have a metric-compatible stand-in
// for, by folded family, to the styles it covers: Carlito for Calibri and
// Caladea for Cambria, each in four styles only (Calibri Light is another
// family's metrics), Liberation Sans, Serif and Mono for Arial, Times New
// Roman and Courier New, and for Helvetica, Times and Courier, the standard
// 14 fonts whose metrics those three share.
//
//nolint:gochecknoglobals // static stand-in table
var standIns = map[string][]string{
	"calibri":       fourStyles,
	"cambria":       fourStyles,
	"arial":         fourStyles,
	"timesnewroman": fourStyles,
	"couriernew":    fourStyles,
	"helvetica":     fourStyles,
	"times":         append([]string{"roman"}, fourStyles...),
	"courier":       fourStyles,
}

// symbolFaces are the symbol fonts the converters recode into OpenSymbol,
// as LibreOffice does (the families pkg/internal/symbolfont's tables name):
// a glyph of theirs is drawn from OpenSymbol, a fixture font.
//
//nolint:gochecknoglobals // static family table
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
// reference draws it: a face of their own families, a metric-compatible
// stand-in in a style it has, a symbol font the converters recode, or a font
// the source document embeds.
func faceCovered(name string) bool {
	if strings.HasPrefix(name, embeddedSourceFont) {
		return true
	}
	family, style := foldFace(name)
	if slices.Contains(fixtureFontFamilies, family) || slices.Contains(symbolFaces, family) {
		return true
	}
	styles, ok := standIns[family]
	return ok && slices.Contains(styles, style)
}

// missingReferenceFaces returns the faces of a reference no fixture font
// draws as the reference does (see [faceCovered]), sorted. No converter can
// match such a reference's metrics, so the diff cell leaves its fixture
// unscored rather than holding litho to it.
//
// # Corpus evidence
//
//   - litho/docx: 114 of 119 Microsoft references draw Calibri Light or
//     Aptos, the headings' theme fonts.
//   - litho/pptx: 32 of 109; libreoffice/pptx: 23 of 311.
func missingReferenceFaces(faces []string) []string {
	var out []string
	for _, f := range faces {
		if !faceCovered(f) {
			out = append(out, f)
		}
	}
	return out
}
