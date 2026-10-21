package pdf

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestRemediatePDFA1FlattensTransparency pins that a PDF/A-1 remediation
// removes the transparency ISO 19005-1, 6.4 forbids by flattening it, in both
// modes: the output holds no 6.4 violation, its text extracts as before, and
// the page draws as the source does. Where the half-transparent red square of
// [transparentPDF] overlaps the blue backdrop, the source shows their blend;
// setting the square opaque, the repair that ran before, painted it pure red.
func TestRemediatePDFA1FlattensTransparency(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	in := transparentPDF()
	src := openColorPDF(t, in)
	before := renderRGBA(t, src, 72)
	text, err := src.ExtractText(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	// User space (50, 60) lies in the red square and on the blue backdrop;
	// the page is 200 points high, so it is device row 140 at 72 dpi.
	const x, y = 50, 140
	sr, sg, sb, _ := before.At(x, y).RGBA()
	if sr>>8 < 64 || sb>>8 < 64 || sg>>8 > 64 {
		t.Fatalf("precondition: the source overlap is not a red-blue blend: %d %d %d", sr>>8, sg>>8, sb>>8)
	}

	for _, aggressive := range []bool{false, true} {
		for _, profile := range []models.PDFAProfile{models.PDFA1b, models.PDFA1a} {
			w := NewWriter(openColorPDF(t, in))
			var opts []RemediateOption
			if aggressive {
				opts = append(opts, WithAggressiveFixes())
			}
			report, err := w.RemediatePDFA(ctx, profile, opts...)
			if err != nil {
				t.Fatalf("%s aggressive=%v: RemediatePDFA: %v", profile, aggressive, err)
			}
			if !slicesContainPrefix(report.Fixed, "flattened transparency on 1 page(s)") {
				t.Errorf("%s aggressive=%v: no flattening reported: %q", profile, aggressive, report.Fixed)
			}
			var buf bytes.Buffer
			if err := w.WriteTo(ctx, &buf); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			_ = w.Close()
			out := openColorPDF(t, buf.Bytes())
			violations, err := out.ValidatePDFAReport(ctx, profile)
			if err != nil {
				t.Fatalf("ValidatePDFAReport: %v", err)
			}
			for _, v := range violations {
				if v.Clause == "6.4" {
					t.Errorf("%s aggressive=%v: transparency left: %v", profile, aggressive, v)
				}
			}
			got, err := out.ExtractText(ctx, 1)
			if err != nil {
				t.Fatalf("ExtractText: %v", err)
			}
			if strings.Join(strings.Fields(got), "") != strings.Join(strings.Fields(text), "") {
				t.Errorf("%s aggressive=%v: text changed from %q to %q", profile, aggressive, text, got)
			}
			after := renderRGBA(t, out, 72)
			if share := pixelsApart(before, after, 24); share > 0.01 {
				t.Errorf("%s aggressive=%v: %.2f%% of the pixels draw apart from the source", profile, aggressive, 100*share)
			}
			r, g, b, _ := after.At(x, y).RGBA()
			if d := max(absDiff(r>>8, sr>>8), absDiff(g>>8, sg>>8), absDiff(b>>8, sb>>8)); d > 8 {
				t.Errorf("%s aggressive=%v: the overlap draws %d %d %d, the source %d %d %d",
					profile, aggressive, r>>8, g>>8, b>>8, sr>>8, sg>>8, sb>>8)
			}
		}
	}
}

// TestRemediatePDFA1FallsBackWhenFlatteningRegresses pins that a PDF/A-1
// output the integrity probe refuses with its transparency flattened is
// remediated again with the transparency set opaque. Flattening draws text
// shown at constant alpha 0 in rendering mode 3, which the probe's extraction
// leaves out: here most of the page's text.
func TestRemediatePDFA1FallsBackWhenFlatteningRegresses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	in := colorFixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R " +
			"/Resources << /Font << /F1 5 0 R >> /ExtGState << /Zero 6 0 R >> >> >>",
		colorStream("", "q /Zero gs BT /F1 12 Tf 10 100 Td (Searchable text nobody sees) Tj ET Q "+
			"BT /F1 12 Tf 10 50 Td (Shown) Tj ET"),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		"<< /Type /ExtGState /ca 0 >>",
	})
	w := NewWriter(openColorPDF(t, in))
	t.Cleanup(func() { _ = w.Close() })
	report, err := w.RemediatePDFA(ctx, models.PDFA1b)
	if err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	if !slicesContainPrefix(report.RolledBack, "flattened transparency on 1 page(s)") {
		t.Errorf("the abandoned flattening is not reported: RolledBack = %q", report.RolledBack)
	}
	if slicesContainPrefix(report.Fixed, "flattened transparency") {
		t.Errorf("Fixed lists the abandoned flattening: %q", report.Fixed)
	}
	var buf bytes.Buffer
	if err := w.WriteTo(ctx, &buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	out := openColorPDF(t, buf.Bytes())
	violations, err := out.ValidatePDFAReport(ctx, models.PDFA1b)
	if err != nil {
		t.Fatalf("ValidatePDFAReport: %v", err)
	}
	for _, v := range violations {
		if v.Clause == "6.4" {
			t.Errorf("transparency left: %v", v)
		}
	}
	text, err := out.ExtractText(ctx, 1)
	if err != nil || !strings.Contains(text, "Searchable text nobody sees") {
		t.Errorf("text lost: %q, %v", text, err)
	}
}

// slicesContainPrefix reports whether some line of lines starts with prefix.
func slicesContainPrefix(lines []string, prefix string) bool {
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

// absDiff returns |a - b|.
func absDiff(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}
