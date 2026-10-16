package remediation

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/internal/typography/cff"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
	"github.com/gotenberg/litho/pkg/models"
)

// subsetTagged matches a /BaseFont carrying a subset tag (ISO 32000-1, 9.6.4).
var subsetTagged = regexp.MustCompile(`^[A-Z]{6}\+`)

// shownText is what every font of [subsetFontsPDF] shows: A, B and Aacute
// (WinAnsiEncoding code 193).
const shownText = `AB\301`

// subsetFontsPDF builds one page showing [shownText] with each of the fonts,
// which the page's resources name F1, F2 and so on, as objects 5 onwards,
// followed by objects, which no resource names. When form is true, an
// interactive form's default resources name F1.
func subsetFontsPDF(form bool, fonts []string, objects ...string) []byte {
	var names, text strings.Builder
	for i := range fonts {
		fmt.Fprintf(&names, " /F%d %d 0 R", i+1, i+5)
		fmt.Fprintf(&text, "/F%d 12 Tf 0 20 Td (%s) Tj ", i+1, shownText)
	}
	stream := "BT 72 700 Td " + text.String() + "ET"
	catalog := "<< /Type /Catalog /Pages 2 0 R >>"
	if form {
		catalog = "<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [] /DR << /Font << /Helv 5 0 R >> >> >> >>"
	}
	bodies := []string{
		catalog,
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font <<" + names.String() + " >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
	}
	return buildStructPDF(append(append(bodies, fonts...), objects...)...)
}

// subsetTrueTypeFont returns a TrueType font over WinAnsiEncoding declaring
// width 667 for A, B and Aacute, with no program.
func subsetTrueTypeFont() string {
	widths := make([]string, 193-65+1)
	for i := range widths {
		widths[i] = "0"
	}
	widths[0], widths[1], widths[193-65] = "667", "667", "667"
	return "<< /Type /Font /Subtype /TrueType /BaseFont /ArialMT /Encoding /WinAnsiEncoding /FirstChar 65 /LastChar 193" +
		" /Widths [" + strings.Join(widths, " ") + "] /FontDescriptor << /Type /FontDescriptor /FontName /ArialMT /Flags 32" +
		" /FontBBox [0 0 1000 800] /ItalicAngle 0 /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 >> >>"
}

// subsetSans is a TrueType program holding more glyphs than the document
// shows: Aacute is a composite drawing A and acute.
func subsetSans(fsType uint16) []byte {
	return fontbuild.TrueType{
		PostScriptName: "TestSans", Family: "Test Sans", Subfamily: "Regular", FSType: fsType, NotdefAdvance: 500,
		Glyphs: []fontbuild.Glyph{
			{Name: "space", Unicode: ' ', Advance: 278, Empty: true},
			{Name: "A", Unicode: 'A', Advance: 667},
			{Name: "B", Unicode: 'B', Advance: 667},
			{Name: "C", Unicode: 'C', Advance: 722},
			{Name: "D", Unicode: 'D', Advance: 722},
			{Name: "acute", Unicode: 0xB4, Advance: 333},
			{Name: "Aacute", Unicode: 0xC1, Advance: 667, Components: []uint16{2, 6}},
		},
	}.Build()
}

// subsetSerif is a CFF program holding more glyphs than the document shows,
// at the Times-Roman widths: Aacute is a seac of A and acute.
func subsetSerif() []byte {
	return fontbuild.CFF{FontName: "TestSerif", Subrs: true, Glyphs: []fontbuild.Glyph{
		{Name: "A", Advance: 722},
		{Name: "B", Advance: 667},
		{Name: "C", Advance: 667},
		{Name: "acute", Advance: 333},
		{Name: "Aacute", Advance: 722, Seac: &[2]byte{65, 194}},
	}}.Build()
}

// subsetProvider supplies subsetSans, with fsType, and subsetSerif.
func subsetProvider(fsType uint16) FontProvider {
	return func(_ context.Context, req FontRequest) ([]byte, error) {
		switch req.BaseFont {
		case "Helvetica", "ArialMT":
			return subsetSans(fsType), nil
		case "Times-Roman":
			return subsetSerif(), nil
		}
		return nil, nil
	}
}

// remediateFontsAndReopen remediates data at profile with provider and returns the
// report and the rewritten output, reopened.
func remediateFontsAndReopen(t *testing.T, data []byte, profile string, provider FontProvider) (*Report, *document.Document, []byte) {
	t.Helper()
	return remediateWithOptionsAndReopen(t, data, profile, Options{PreserveContent: true, Fonts: provider})
}

// remediateWithOptionsAndReopen is [remediateFontsAndReopen] under opts.
func remediateWithOptionsAndReopen(t *testing.T, data []byte, profile string, opts Options) (*Report, *document.Document, []byte) {
	t.Helper()
	ctx := context.Background()
	doc, alloc := newTestDoc(t, data)
	var report *Report
	var err error
	if strings.HasPrefix(profile, "PDF/UA") {
		report, err = RemediatePDFUA(ctx, doc, alloc, models.PDFUAProfile(profile), opts)
	} else {
		report, err = RemediatePDFA(ctx, doc, alloc, models.PDFAProfile(profile), opts)
	}
	if err != nil {
		t.Fatalf("%s: remediating: %v", profile, err)
	}
	var out bytes.Buffer
	if err := mutations.FullRewrite(ctx, &out, mutations.FullRewriteConfig{Doc: doc, Overrides: alloc.Pending()}); err != nil {
		t.Fatalf("%s: FullRewrite: %v", profile, err)
	}
	reread, err := document.NewDocument(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), nil)
	if err != nil {
		t.Fatalf("%s: reopening the output: %v", profile, err)
	}
	return report, reread, out.Bytes()
}

// fontFindings returns the findings of the font clauses of ISO 19005-1
// (6.3), ISO 19005-2 (6.2.11) and ISO 14289-1 (7.21) doc fails at profile,
// keyed by rule.
func fontFindings(t *testing.T, doc *document.Document, profile string) map[compliance.Rule]int {
	t.Helper()
	ctx := context.Background()
	var errs []error
	var err error
	if strings.HasPrefix(profile, "PDF/UA") {
		errs, err = compliance.ValidatePDFUA(ctx, doc, models.PDFUAProfile(profile))
	} else {
		errs, err = compliance.ValidatePDFA(ctx, doc, models.PDFAProfile(profile))
	}
	if err != nil {
		t.Fatalf("%s: validating: %v", profile, err)
	}
	out := make(map[compliance.Rule]int)
	for _, v := range toViolations(errs) {
		for _, prefix := range []string{"6.3.", "6.2.11", "7.21"} {
			if strings.HasPrefix(v.Rule.Clause, prefix) {
				out[v.Rule]++
			}
		}
	}
	return out
}

// fontProgram returns the font's descriptor and its decoded program.
func fontProgram(t *testing.T, doc *document.Document, font ast.PdfDict) (ast.PdfDict, []byte) {
	t.Helper()
	ctx := context.Background()
	fd, err := document.ResolveDict(ctx, doc, font["FontDescriptor"])
	if err != nil {
		t.Fatalf("resolving the descriptor: %v", err)
	}
	for _, key := range []ast.PdfName{"FontFile2", "FontFile3"} {
		if obj, ok := fd[key]; ok {
			stream, err := document.ResolveStream(ctx, doc, obj)
			if err != nil {
				t.Fatalf("resolving %s: %v", key, err)
			}
			data, err := doc.DecodeStream(ctx, stream)
			if err != nil {
				t.Fatalf("decoding %s: %v", key, err)
			}
			return fd, data
		}
	}
	t.Fatalf("descriptor %v holds no program", fd)
	return nil, nil
}

func TestFixFontEmbed_SubsetsSuppliedPrograms(t *testing.T) {
	t.Parallel()
	data := subsetFontsPDF(false, []string{
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Times-Roman /Encoding /WinAnsiEncoding >>",
		subsetTrueTypeFont(),
	})
	for _, profile := range []string{string(models.PDFA1b), string(models.PDFA2b), string(models.PDFUA1)} {
		report, subsetDoc, _ := remediateFontsAndReopen(t, data, profile, subsetProvider(0))
		wholeReport, wholeDoc, _ := remediateFontsAndReopen(t, data, profile, subsetProvider(truetype.FSTypeNoSubsetting))
		if len(report.FontRefusals) != 0 || len(wholeReport.FontRefusals) != 0 {
			t.Fatalf("%s: refusals %v, %v", profile, report.FontRefusals, wholeReport.FontRefusals)
		}
		subsets, wholes := 0, 0
		for _, f := range report.Fixed {
			if strings.HasPrefix(f, "embedded ") && strings.Contains(f, "(subset of ") {
				subsets++
			}
		}
		for _, f := range wholeReport.Fixed {
			if strings.HasPrefix(f, "embedded ") && strings.Contains(f, wholeFSType) {
				wholes++
			}
		}
		// Times-Roman's CFF program carries no fsType, so it is subset in both
		// runs.
		if subsets != 3 || wholes != 2 {
			t.Errorf("%s: %d subsets and %d whole programs, want 3 and 2: %v / %v", profile, subsets, wholes, report.Fixed, wholeReport.Fixed)
		}

		for _, name := range []ast.PdfName{"F1", "F2", "F3"} {
			font := pageFont(t, subsetDoc, name)
			base, _ := font["BaseFont"].(ast.PdfName)
			fd, program := fontProgram(t, subsetDoc, font)
			if !subsetTagged.MatchString(string(base)) || fd["FontName"] != base {
				t.Errorf("%s %s: /BaseFont %q, /FontName %v, want one tagged name", profile, name, base, fd["FontName"])
			}
			_, wholeProgram := fontProgram(t, wholeDoc, pageFont(t, wholeDoc, name))
			if name != "F2" && len(program) >= len(wholeProgram) {
				t.Errorf("%s %s: subset of %d bytes, whole program %d", profile, name, len(program), len(wholeProgram))
			}
			// .notdef, A, B, Aacute and the acute it draws.
			glyphs := 0
			if name == "F2" {
				info, err := cff.ReadInfo(program)
				if err != nil {
					t.Fatalf("%s %s: %v", profile, name, err)
				}
				glyphs = info.Glyphs
				charSet, hasCharSet := fd["CharSet"]
				if wantCharSet := profile == string(models.PDFA1b); hasCharSet != wantCharSet {
					t.Errorf("%s %s: /CharSet %v, want one at PDF/A-1 alone", profile, name, charSet)
				} else if hasCharSet && charSet != ast.PdfString("/A/B/acute/Aacute") {
					t.Errorf("%s %s: /CharSet %v", profile, name, charSet)
				}
			} else if glyphs, _ = truetype.NumGlyphs(program); true {
				if _, ok := fd["CharSet"]; ok {
					t.Errorf("%s %s: a TrueType subset carries /CharSet", profile, name)
				}
			}
			if glyphs != 5 {
				t.Errorf("%s %s: %d glyphs in the subset, want 5", profile, name, glyphs)
			}
		}

		if embed, presence, width := embeddingFindingsAt(t, subsetDoc, profile); embed+presence+width != 0 {
			t.Errorf("%s: output fails embedding %d, presence %d, width %d times", profile, embed, presence, width)
		}
		whole := fontFindings(t, wholeDoc, profile)
		for rule, n := range fontFindings(t, subsetDoc, profile) {
			if n > whole[rule] {
				t.Errorf("%s: the subset fails %v %d times, the whole program %d", profile, rule, n, whole[rule])
			}
		}
	}
}

// TestFixFontEmbed_WholeFontsOption pins that [Options.WholeFonts] embeds
// every supplied program whole, untagged, where the default run subsets each
// of them, and that the whole programs fail no font rule.
func TestFixFontEmbed_WholeFontsOption(t *testing.T) {
	t.Parallel()
	data := subsetFontsPDF(false, []string{
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Times-Roman /Encoding /WinAnsiEncoding >>",
		subsetTrueTypeFont(),
	})
	for _, profile := range []string{string(models.PDFA1b), string(models.PDFA2b), string(models.PDFUA1)} {
		report, doc, _ := remediateWithOptionsAndReopen(t, data, profile,
			Options{PreserveContent: true, Fonts: subsetProvider(0), WholeFonts: true})
		if len(report.FontRefusals) != 0 {
			t.Fatalf("%s: refusals %v", profile, report.FontRefusals)
		}
		wholes := 0
		for _, f := range report.Fixed {
			if strings.Contains(f, "(subset of ") {
				t.Errorf("%s: a program was subset: %s", profile, f)
			}
			if strings.HasPrefix(f, "embedded ") && strings.Contains(f, wholeRequested) {
				wholes++
			}
		}
		if wholes != 3 {
			t.Errorf("%s: %d whole programs, want 3: %v", profile, wholes, report.Fixed)
		}
		for name, want := range map[ast.PdfName][]byte{"F1": subsetSans(0), "F2": subsetSerif(), "F3": subsetSans(0)} {
			font := pageFont(t, doc, name)
			if base, _ := font["BaseFont"].(ast.PdfName); subsetTagged.MatchString(string(base)) {
				t.Errorf("%s %s: a whole program's font is tagged %q", profile, name, base)
			}
			fd, program := fontProgram(t, doc, font)
			if !bytes.Equal(program, want) {
				t.Errorf("%s %s: embedded %d bytes, want the %d-byte program supplied", profile, name, len(program), len(want))
			}
			if _, ok := fd["CharSet"]; ok {
				t.Errorf("%s %s: a whole program carries /CharSet", profile, name)
			}
		}
		if embed, presence, width := embeddingFindingsAt(t, doc, profile); embed+presence+width != 0 {
			t.Errorf("%s: output fails embedding %d, presence %d, width %d times", profile, embed, presence, width)
		}
	}
}

// embeddingFindingsAt is [embeddingFindings] at a PDF/A or PDF/UA profile.
func embeddingFindingsAt(t *testing.T, doc *document.Document, profile string) (embed, presence, width int) {
	t.Helper()
	for rule, n := range fontFindings(t, doc, profile) {
		switch rule {
		case compliance.FontEmbeddingRule(profile):
			embed += n
		case compliance.GlyphPresenceRule(profile):
			presence += n
		case compliance.GlyphWidthRule(profile):
			width += n
		}
	}
	return embed, presence, width
}

func TestFixFontEmbed_SubsetsCompositeFonts(t *testing.T) {
	t.Parallel()
	program := func(fsType uint16) []byte {
		return fontbuild.TrueType{
			PostScriptName: "TestMincho", Family: "Test Mincho", Subfamily: "Regular", NotdefAdvance: 1000, FSType: fsType,
			Glyphs: []fontbuild.Glyph{
				{Name: "uni3041", Unicode: 0x3041, Advance: 1000},
				{Name: "uni3042", Unicode: 0x3042, Advance: 1000},
				{Name: "uni3044", Unicode: 0x3044, Advance: 1000},
			},
		}.Build()
	}
	provider := func(fsType uint16) FontProvider {
		return func(context.Context, FontRequest) ([]byte, error) { return program(fsType), nil }
	}
	data := compositePDF("UniJIS-UCS2-H", "Japan1")
	for _, profile := range []string{string(models.PDFA1b), string(models.PDFA2b)} {
		report, doc, _ := remediateFontsAndReopen(t, data, profile, provider(0))
		_, wholeDoc, _ := remediateFontsAndReopen(t, data, profile, provider(truetype.FSTypeNoSubsetting))
		if len(report.FontRefusals) != 0 {
			t.Fatalf("%s: refusals %v", profile, report.FontRefusals)
		}
		font := pageFont(t, doc, "F1")
		descendants, err := document.ResolveArray(context.Background(), doc, font["DescendantFonts"])
		if err != nil || len(descendants) != 1 {
			t.Fatalf("%s: resolving the descendants: %v", profile, err)
		}
		desc, err := document.ResolveDict(context.Background(), doc, descendants[0])
		if err != nil {
			t.Fatalf("%s: resolving the descendant: %v", profile, err)
		}
		base, _ := desc["BaseFont"].(ast.PdfName)
		if !subsetTagged.MatchString(string(base)) || font["BaseFont"] != base {
			t.Errorf("%s: descendant /BaseFont %q, Type0 /BaseFont %v, want one tagged name", profile, base, font["BaseFont"])
		}
		fd, sub := fontProgram(t, doc, desc)
		if n, _ := truetype.NumGlyphs(sub); n != 2 {
			t.Errorf("%s: %d glyphs in the subset, want .notdef and U+3042", profile, n)
		}
		if _, has := fd["CIDSet"]; has != (profile == string(models.PDFA1b)) {
			t.Errorf("%s: /CIDSet %v, want one at PDF/A-1 alone", profile, fd["CIDSet"])
		}
		whole := fontFindings(t, wholeDoc, profile)
		for rule, n := range fontFindings(t, doc, profile) {
			if n > whole[rule] {
				t.Errorf("%s: the subset fails %v %d times, the whole program %d", profile, rule, n, whole[rule])
			}
		}
	}
}

func TestFixFontEmbed_KeepsSomeProgramsWhole(t *testing.T) {
	t.Parallel()
	// F1 and F2 share one descriptor, and F1 is the interactive form's
	// default font, so neither takes a subset: F2 would draw from the program
	// F1 keeps whole. F3 shares nothing and takes one.
	const descriptor = "<< /Type /FontDescriptor /FontName /ArialMT /Flags 32 /FontBBox [0 0 1000 800] /ItalicAngle 0" +
		" /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 >>"
	sharing := strings.Replace(subsetTrueTypeFont(), "/FontDescriptor << /Type /FontDescriptor /FontName /ArialMT /Flags 32"+
		" /FontBBox [0 0 1000 800] /ItalicAngle 0 /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 >>", "/FontDescriptor 8 0 R", 1)
	data := subsetFontsPDF(true, []string{sharing, sharing,
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>"}, descriptor)
	report, doc, _ := remediateFontsAndReopen(t, data, string(models.PDFA2b), subsetProvider(0))
	if len(report.FontRefusals) != 0 {
		t.Fatalf("refusals %v", report.FontRefusals)
	}
	var form, shared, subsets int
	for _, f := range report.Fixed {
		switch {
		case strings.Contains(f, wholeFormFont):
			form++
		case strings.Contains(f, wholeShared):
			shared++
		case strings.Contains(f, "(subset of "):
			subsets++
		}
	}
	if form != 1 || shared != 1 || subsets != 1 {
		t.Errorf("%d form, %d shared and %d subset programs, want one each: %v", form, shared, subsets, report.Fixed)
	}
	for _, name := range []ast.PdfName{"F1", "F2"} {
		if base, _ := pageFont(t, doc, name)["BaseFont"].(ast.PdfName); subsetTagged.MatchString(string(base)) {
			t.Errorf("%s: a whole program's font is tagged %q", name, base)
		}
	}
	if base, _ := pageFont(t, doc, "F3")["BaseFont"].(ast.PdfName); !subsetTagged.MatchString(string(base)) {
		t.Errorf("F3: a subset's font is named %q", base)
	}
}

// TestFixFontEmbed_FontsShareOneSubset pins that fonts given the same program
// share one subset of it, holding what each of them shows, written once.
func TestFixFontEmbed_FontsShareOneSubset(t *testing.T) {
	t.Parallel()
	stream := "BT /F1 12 Tf 72 700 Td (A) Tj /F2 12 Tf 0 20 Td (B) Tj ET"
	data := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R /F2 6 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		trueTypeFont("ArialMT", 667, 667),
		trueTypeFont("Arial-BoldMT", 667, 667),
	)
	provider := func(context.Context, FontRequest) ([]byte, error) { return subsetSans(0), nil }
	report, doc, _ := remediateFontsAndReopen(t, data, string(models.PDFA2b), provider)
	if len(report.FontRefusals) != 0 {
		t.Fatalf("refusals %v", report.FontRefusals)
	}
	first, second := pageFont(t, doc, "F1"), pageFont(t, doc, "F2")
	fd1, program := fontProgram(t, doc, first)
	fd2, _ := fontProgram(t, doc, second)
	if fd1["FontFile2"] != fd2["FontFile2"] {
		t.Errorf("the fonts hold programs %v and %v, want one shared", fd1["FontFile2"], fd2["FontFile2"])
	}
	if n, _ := truetype.NumGlyphs(program); n != 3 {
		t.Errorf("%d glyphs in the shared subset, want .notdef, A and B", n)
	}
	tag1, _, _ := strings.Cut(string(first["BaseFont"].(ast.PdfName)), "+")
	tag2, _, _ := strings.Cut(string(second["BaseFont"].(ast.PdfName)), "+")
	if tag1 != tag2 || !subsetTagged.MatchString(tag1+"+") {
		t.Errorf("tags %q and %q, want one subset tag", tag1, tag2)
	}
}

// TestFixFontEmbed_EncodingsKeepOwnPrograms pins that fonts given one program
// under different encodings each hold a subset of their own, even when the
// subsets come out alike, since veraPDF reads a stream shared by both under the
// first font's encoding and judges the other font by it, and that a whole
// program stays one stream.
func TestFixFontEmbed_EncodingsKeepOwnPrograms(t *testing.T) {
	t.Parallel()
	stream := "BT /F1 12 Tf 72 700 Td (AB) Tj /F2 12 Tf 0 20 Td (AB) Tj /F3 12 Tf 0 20 Td (AB) Tj ET"
	data := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R /F2 6 0 R /F3 7 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		trueTypeFont("ArialMT", 667, 667),
		strings.Replace(trueTypeFont("Arial-BoldMT", 667, 667), "/WinAnsiEncoding", "/MacRomanEncoding", 1),
		trueTypeFont("Arial-ItalicMT", 667, 667),
	)
	provider := func(context.Context, FontRequest) ([]byte, error) { return subsetSans(0), nil }
	for _, whole := range []bool{false, true} {
		report, doc, _ := remediateWithOptionsAndReopen(t, data, string(models.PDFA2b), Options{PreserveContent: true, Fonts: provider, WholeFonts: whole})
		if len(report.FontRefusals) != 0 {
			t.Fatalf("whole=%v: refusals %v", whole, report.FontRefusals)
		}
		fd1, _ := fontProgram(t, doc, pageFont(t, doc, "F1"))
		fd2, _ := fontProgram(t, doc, pageFont(t, doc, "F2"))
		fd3, _ := fontProgram(t, doc, pageFont(t, doc, "F3"))
		if shared := fd1["FontFile2"] == fd2["FontFile2"]; shared != whole {
			t.Errorf("whole=%v: WinAnsiEncoding and MacRomanEncoding fonts hold programs %v and %v, want them shared only when whole", whole, fd1["FontFile2"], fd2["FontFile2"])
		}
		if fd1["FontFile2"] != fd3["FontFile2"] {
			t.Errorf("whole=%v: two WinAnsiEncoding fonts hold programs %v and %v, want one shared", whole, fd1["FontFile2"], fd3["FontFile2"])
		}
		if got := fontFindings(t, doc, string(models.PDFA2b)); len(got) != 0 {
			t.Errorf("whole=%v: font findings %v", whole, got)
		}
	}
}

// TestFixFontEmbed_InlineFontSharesSubset pins that a font written inline in
// a resource dictionary joins the subset of the program it shares with an
// indirect font: finish moves it to a new object, and both fonts draw from one
// subset holding what each shows, under one tag, reported as a subset with the
// inline site named.
func TestFixFontEmbed_InlineFontSharesSubset(t *testing.T) {
	t.Parallel()
	stream := "BT /F1 12 Tf 72 700 Td (A) Tj /F2 12 Tf 0 20 Td (B) Tj ET"
	data := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R /F2 "+
			trueTypeFont("Arial-BoldMT", 667, 667)+" >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		trueTypeFont("ArialMT", 667, 667),
	)
	provider := func(context.Context, FontRequest) ([]byte, error) { return subsetSans(0), nil }
	for _, profile := range []string{string(models.PDFA1b), string(models.PDFA2b)} {
		report, doc, _ := remediateFontsAndReopen(t, data, profile, provider)
		if len(report.FontRefusals) != 0 {
			t.Fatalf("%s: refusals %v", profile, report.FontRefusals)
		}
		want := []FontEmbed{{BaseFont: "ArialMT", Object: 5}, {BaseFont: "Arial-BoldMT", Object: 0}}
		if !slices.Equal(report.FontsEmbedded, want) {
			t.Errorf("%s: FontsEmbedded %v, want %v", profile, report.FontsEmbedded, want)
		}
		var subsets, inline int
		for _, f := range report.Fixed {
			if strings.Contains(f, "(subset of 3 glyphs, ") {
				subsets++
				if strings.Contains(f, "written inline as /F2, now object ") {
					inline++
				}
			}
		}
		if subsets != 2 || inline != 1 {
			t.Errorf("%s: %d subsets, %d of them inline, want 2 and 1: %v", profile, subsets, inline, report.Fixed)
		}
		if _, ok := pageFontEntry(t, doc, "F2").(ast.PdfIndirectRef); !ok {
			t.Errorf("%s: the page's /F2 is still written inline", profile)
		}
		first, second := pageFont(t, doc, "F1"), pageFont(t, doc, "F2")
		fd1, program := fontProgram(t, doc, first)
		fd2, _ := fontProgram(t, doc, second)
		if fd1["FontFile2"] != fd2["FontFile2"] {
			t.Errorf("%s: the fonts hold programs %v and %v, want one shared", profile, fd1["FontFile2"], fd2["FontFile2"])
		}
		if n, _ := truetype.NumGlyphs(program); n != 3 {
			t.Errorf("%s: %d glyphs in the shared subset, want .notdef, A and B", profile, n)
		}
		tag1, _, _ := strings.Cut(string(first["BaseFont"].(ast.PdfName)), "+")
		tag2, _, _ := strings.Cut(string(second["BaseFont"].(ast.PdfName)), "+")
		if tag1 != tag2 || !subsetTagged.MatchString(tag1+"+") {
			t.Errorf("%s: tags %q and %q, want one subset tag", profile, tag1, tag2)
		}
		if embed, presence, width := embeddingFindingsAt(t, doc, profile); embed+presence+width != 0 {
			t.Errorf("%s: output fails embedding %d, presence %d, width %d times", profile, embed, presence, width)
		}
	}
}

// TestFixFontEmbed_InlineFontKeepsSharedDescriptorWhole pins that a font
// written inline counts among the users of the descriptor it names: the
// indirect font sharing that descriptor keeps its program whole, since the
// inline font, which the provider does not serve, draws from it too.
func TestFixFontEmbed_InlineFontKeepsSharedDescriptorWhole(t *testing.T) {
	t.Parallel()
	const descriptor = "<< /Type /FontDescriptor /FontName /ArialMT /Flags 32 /FontBBox [0 0 1000 800] /ItalicAngle 0" +
		" /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 >>"
	font := func(base string) string {
		return "<< /Type /Font /Subtype /TrueType /BaseFont /" + base + " /Encoding /WinAnsiEncoding /FirstChar 65 /LastChar 66" +
			" /Widths [667 667] /FontDescriptor 6 0 R >>"
	}
	stream := "BT /F1 12 Tf 72 700 Td (A) Tj /F2 12 Tf 0 20 Td (B) Tj ET"
	data := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R /F2 "+
			font("Arial-BoldMT")+" >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		font("ArialMT"),
		descriptor,
	)
	provider := func(_ context.Context, req FontRequest) ([]byte, error) {
		if req.BaseFont == "ArialMT" {
			return subsetSans(0), nil
		}
		return nil, nil
	}
	report, _, _ := remediateFontsAndReopen(t, data, string(models.PDFA2b), provider)
	var shared int
	for _, f := range report.Fixed {
		if strings.Contains(f, "(subset of ") {
			t.Errorf("a font sharing its descriptor with an inline font took a subset: %s", f)
		}
		if strings.Contains(f, wholeShared) {
			shared++
		}
	}
	if shared != 1 {
		t.Errorf("%d whole programs kept for a shared descriptor, want 1: %v", shared, report.Fixed)
	}
}

// documentID matches the trailer's /ID, the seam CONTRIBUTING (Deterministic
// output) lets differ between runs.
var documentID = regexp.MustCompile(`/ID \[<[0-9A-F]*> <[0-9A-F]*>\]`)

// TestFixFontEmbed_SubsetIsDeterministic pins that the same input gives the
// same subsets, tags and output bytes on every run, the /ID aside
// (CONTRIBUTING, Deterministic output).
func TestFixFontEmbed_SubsetIsDeterministic(t *testing.T) {
	t.Parallel()
	data := subsetFontsPDF(false, []string{
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Times-Roman /Encoding /WinAnsiEncoding >>",
		subsetTrueTypeFont(),
	})
	_, _, first := remediateFontsAndReopen(t, data, string(models.PDFA1b), subsetProvider(0))
	first = documentID.ReplaceAll(first, nil)
	for range 3 {
		_, _, again := remediateFontsAndReopen(t, data, string(models.PDFA1b), subsetProvider(0))
		if !bytes.Equal(first, documentID.ReplaceAll(again, nil)) {
			t.Fatalf("two remediations of the same input differ")
		}
	}
}
