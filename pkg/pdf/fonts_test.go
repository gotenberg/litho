package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/gotenberg/litho/pkg/internal/remediation"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
	"github.com/gotenberg/litho/pkg/models"
)

// unembeddedFontsDoc builds one page showing "AB" with each font, which its
// resources name F1, F2 and so on.
func unembeddedFontsDoc(fonts ...string) []byte {
	var names, text strings.Builder
	for i := range fonts {
		fmt.Fprintf(&names, " /F%d %d 0 R", i+1, i+5)
		fmt.Fprintf(&text, "/F%d 12 Tf 0 -20 Td (AB) Tj ", i+1)
	}
	content := "BT 72 700 Td " + text.String() + "ET"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font <<" + names.String() + " >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}
	return buildPDF(append(objs, fonts...))
}

// abProgram returns a TrueType program named family and style drawing "A" and
// "B" at the given advances.
func abProgram(psName, family, style string, a, b uint16) []byte {
	return fontbuild.TrueType{PostScriptName: psName, Family: family, Subfamily: style, Glyphs: []fontbuild.Glyph{
		{Name: "A", Unicode: 'A', Advance: a}, {Name: "B", Unicode: 'B', Advance: b},
	}}.Build()
}

// remediateFonts remediates data at PDF/A-2b with opts and returns the report
// and the residual findings of the rule requiring embedded programs.
func remediateFonts(t *testing.T, data []byte, opts ...RemediateOption) (*RemediationReport, int) {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	w := NewWriter(r)
	t.Cleanup(func() { _ = w.Close() })
	report, err := w.RemediatePDFA(ctx, models.PDFA2b, opts...)
	if err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	t.Cleanup(func() { _ = r2.Close() })
	vs, err := r2.ValidatePDFA(ctx, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	unembedded := 0
	for _, v := range toViolations(vs) {
		if v.Clause == "6.2.11.4.1" {
			unembedded++
		}
	}
	return report, unembedded
}

func TestWithFontFiles_EmbedsMatchingPrograms(t *testing.T) {
	t.Parallel()
	data := unembeddedFontsDoc(
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold >>",
		"<< /Type /Font /Subtype /TrueType /BaseFont /ABCDEF+Arial,Bold /Encoding /WinAnsiEncoding /FirstChar 65 /LastChar 66"+
			" /Widths [722 722] /FontDescriptor << /Type /FontDescriptor /FontName /ABCDEF+Arial,Bold /Flags 32"+
			" /FontBBox [0 0 1000 800] /ItalicAngle 0 /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 >> >>",
	)
	fonts := fstest.MapFS{
		"sans/LiberationSans-Bold.ttf":    {Data: abProgram("LiberationSans-Bold", "Liberation Sans", "Bold", 722, 722)},
		"sans/LiberationSans-Regular.ttf": {Data: abProgram("LiberationSans", "Liberation Sans", "Regular", 667, 667)},
		"README.txt":                      {Data: []byte("not a font")},
	}
	if _, before := remediateFonts(t, data); before != 2 {
		t.Fatalf("precondition: %d unembedded fonts without fonts, want 2", before)
	}
	report, after := remediateFonts(t, data, WithFontFiles(fonts))
	if after != 0 {
		t.Errorf("%d fonts stay unembedded: %v", after, report.FontRefusals)
	}
	embedded := 0
	for _, f := range report.Fixed {
		if strings.Contains(f, "embedded LiberationSans-Bold") {
			embedded++
		}
	}
	if embedded != 2 {
		t.Errorf("want LiberationSans-Bold embedded twice, got %v", report.Fixed)
	}
}

func TestWithFontProvider_ReportsRefusals(t *testing.T) {
	t.Parallel()
	data := unembeddedFontsDoc("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	var asked []FontRequest
	provider := FontProviderFunc(func(_ context.Context, req FontRequest) ([]byte, error) {
		asked = append(asked, req)
		return nil, nil
	})
	report, after := remediateFonts(t, data, WithFontProvider(provider))
	if after != 1 {
		t.Errorf("%d unembedded fonts, want the one no program was supplied for", after)
	}
	if len(asked) == 0 || asked[0].BaseFont != "Helvetica" || len(asked[0].Codes) != 2 || asked[0].Codes[1].Glyph != "B" {
		t.Errorf("requests %+v", asked)
	}
	if len(report.FontRefusals) != 1 || !errors.Is(report.FontRefusals[0].Err, ErrFontNotSupplied) {
		t.Errorf("refusals %v, want one ErrFontNotSupplied", report.FontRefusals)
	}
}

func TestWithFontProvider_AbortsOnProviderError(t *testing.T) {
	t.Parallel()
	data := unembeddedFontsDoc("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	failure := errors.New("font store down")
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	_, err = w.RemediatePDFA(ctx, models.PDFA2b, WithFontProvider(FontProviderFunc(func(context.Context, FontRequest) ([]byte, error) {
		return nil, failure
	})))
	if !errors.Is(err, failure) {
		t.Errorf("RemediatePDFA error = %v, want the provider's", err)
	}
}

func TestMatchFontFile(t *testing.T) {
	t.Parallel()
	files := []fontFile{
		{path: "a.ttf", psName: "arial", family: "arial"},
		{path: "b.ttf", psName: "arialbold", family: "arial", bold: true},
		{path: "c.ttf", psName: "liberationserifitalic", family: "liberationserif", italic: true},
		{path: "d.otf", psName: "nimbusromanitalic", family: "nimbusroman", italic: true, cffOutlines: true},
		{path: "e.ttf", psName: "wingdings", family: "wingdings"},
	}
	for _, tc := range []struct {
		req  FontRequest
		want string
	}{
		{FontRequest{BaseFont: "ArialMT", Subtype: "TrueType"}, "a.ttf"},
		{FontRequest{BaseFont: "Arial-BoldMT", Subtype: "TrueType"}, "b.ttf"},
		{FontRequest{BaseFont: "QWERTY+Arial,Bold", Subtype: "TrueType"}, "b.ttf"},
		{FontRequest{BaseFont: "Times-Italic", Subtype: "Type1"}, "d.otf"},
		{FontRequest{BaseFont: "TimesNewRomanPS-ItalicMT", Subtype: "TrueType"}, "c.ttf"},
		{FontRequest{BaseFont: "Courier", Subtype: "Type1"}, ""},
		{FontRequest{BaseFont: "Wingdings-Regular", Subtype: "TrueType"}, "e.ttf"},
	} {
		got := ""
		if f := matchFontFile(files, tc.req); f != nil {
			got = f.path
		}
		if got != tc.want {
			t.Errorf("%s (%s): matched %q, want %q", tc.req.BaseFont, tc.req.Subtype, got, tc.want)
		}
	}
}

// TestWithFontProvider_ReportsTheFinalState pins [RemediationReport.FontRefusals]
// to the fonts the output leaves unembedded, across the rounds a remediation
// runs: a font the first round refuses and a later round embeds is not listed,
// and one every round refuses is listed once.
func TestWithFontProvider_ReportsTheFinalState(t *testing.T) {
	t.Parallel()
	data := unembeddedFontsDoc("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	program := abProgram("TestSans", "Test Sans", "Regular", 667, 667)

	var calls atomic.Int32
	late := FontProviderFunc(func(context.Context, FontRequest) ([]byte, error) {
		if calls.Add(1) == 1 {
			return nil, nil
		}
		return program, nil
	})
	report, after := remediateFonts(t, data, WithFontProvider(late))
	if calls.Load() < 2 {
		t.Fatalf("the provider was asked %d times, want a second round to ask again", calls.Load())
	}
	if after != 0 || len(report.FontRefusals) != 0 {
		t.Errorf("a font embedded in the second round: %d unembedded, refusals %v, want none", after, report.FontRefusals)
	}

	never := FontProviderFunc(func(context.Context, FontRequest) ([]byte, error) { return nil, nil })
	report, after = remediateFonts(t, data, WithFontProvider(never))
	if after != 1 || len(report.FontRefusals) != 1 || !errors.Is(report.FontRefusals[0].Err, ErrFontNotSupplied) {
		t.Errorf("a font every round refuses: %d unembedded, refusals %v, want it listed once", after, report.FontRefusals)
	}
}

// TestFontVerdict_RolledBack pins the fonts a rolled-back pass embedded to the
// refusals, with [ErrFontRolledBack], and a pass that did not judge the fonts
// to no verdict at all.
func TestFontVerdict_RolledBack(t *testing.T) {
	t.Parallel()
	rep := &remediation.Report{
		FontsJudged:   true,
		FontRefusals:  []remediation.FontRefusal{{BaseFont: "Courier", Object: 7, Err: remediation.ErrFontNotSupplied}},
		FontsEmbedded: []remediation.FontEmbed{{BaseFont: "Helvetica", Object: 5}},
	}
	kept, judged := fontVerdict(rep, false)
	if !judged || len(kept) != 1 {
		t.Errorf("kept pass: %v (judged %v), want Courier's refusal", kept, judged)
	}
	rolled, _ := fontVerdict(rep, true)
	if len(rolled) != 2 || rolled[1].BaseFont != "Helvetica" || !errors.Is(rolled[1].Err, ErrFontRolledBack) {
		t.Errorf("rolled-back pass: %v, want Helvetica refused as rolled back", rolled)
	}
	if _, judged := fontVerdict(&remediation.Report{FontRefusals: rep.FontRefusals}, false); judged {
		t.Errorf("a pass that did not judge the fonts gave a verdict")
	}
}
