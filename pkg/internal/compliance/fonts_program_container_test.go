package compliance

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestFontEmbedding_ProgramContainer pins that a /FontFile* entry embeds a font
// only when it holds a program of the kind its key declares. Per ISO 32000-1,
// Table 126, /FontFile is a Type 1 program and /FontFile2 a TrueType one, so a
// PFB container or a TrueType Collection embeds no single program a consumer
// can read. veraPDF reports each under ISO 19005-2 §6.2.11.4.1 t1.
//
// # Corpus evidence
//
//   - pdfjs bug860632.pdf, issue14462_reduced.pdf: embed PFB, and veraPDF
//     reports them.
//   - pdfjs issue13193.pdf, issue9262_reduced.pdf: embed a TrueType
//     Collection, and veraPDF reports them.
//   - pdfjs bug866395.pdf: embeds an empty stream, and veraPDF reports it.
//   - pdfjs issue15559.pdf: embeds a /FontFile2 that is no sfnt at all, and
//     veraPDF reports it.
func TestFontEmbedding_ProgramContainer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// fires reports whether the embedding rule fails for a simple font whose
	// descriptor carries key with the given program bytes.
	fires := func(t *testing.T, key, program string) bool {
		t.Helper()
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			"<< /Type /Font /Subtype /TrueType /BaseFont /Test /FirstChar 65 /LastChar 65"+
				" /Widths [722] /FontDescriptor 5 0 R >>",
			"<< /Type /FontDescriptor /FontName /Test /Flags 32 /"+key+" 6 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(program), program),
		)
		font, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
		if err != nil {
			t.Fatalf("resolve font: %v", err)
		}
		got := checkFontEmbedding(ctx, doc, font, "F1", 1, "PDF/A-2b", false)
		return hasRule(got, Rule_19005_2_6_2_11_4_1_t01)
	}

	sfnt := "\x00\x01\x00\x00rest-of-the-program"

	cases := []struct {
		name    string
		key     string
		program string
		want    bool
	}{
		{"a TrueType sfnt embeds the font", "FontFile2", sfnt, false},
		{"an OTTO sfnt embeds the font", "FontFile2", "OTTOrest-of-the-program", false},
		{"a TrueType Collection does not", "FontFile2", "ttcf\x00\x02\x00\x00rest", true},
		{"a non-sfnt /FontFile2 does not", "FontFile2", "\x01\x00\x04\x03rest", true},
		{"an empty program does not", "FontFile2", "", true},
		{"a raw Type 1 program embeds the font", "FontFile", "%!PS-AdobeFont-1.0\neexec\n", false},
		{"a PFB-wrapped Type 1 program does not", "FontFile", "\x80\x01\xae\x04\x00\x00%!Font", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := fires(t, tc.key, tc.program); got != tc.want {
				t.Errorf("/%s embedding violation = %v, want %v", tc.key, got, tc.want)
			}
		})
	}
}

// TestFontEmbedding_UndecodableProgramStaysEmbedded pins the conservative side:
// bytes that will not decode say nothing about which container they hold, and
// veraPDF still counts the font as embedded, so firing here would be a false
// positive. The filter chain keeps the byte decoded before the damage, and
// that fragment must not be judged as the program.
//
// # Corpus evidence
//
//   - pdfjs poppler-395-0-fuzzed.pdf: carries a /FontFile2 with corrupt
//     ASCIIHex data that veraPDF passes.
func TestFontEmbedding_UndecodableProgramStaysEmbedded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Font /Subtype /TrueType /BaseFont /Test /FirstChar 65 /LastChar 65"+
			" /Widths [722] /FontDescriptor 5 0 R >>",
		"<< /Type /FontDescriptor /FontName /Test /Flags 32 /FontFile2 6 0 R >>",
		"<< /Length 5 /Filter /ASCIIHexDecode >>\nstream\n48}9>\nendstream",
	)
	font, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
	if err != nil {
		t.Fatalf("resolve font: %v", err)
	}
	if got := checkFontEmbedding(ctx, doc, font, "F1", 1, "PDF/A-2b", false); len(got) != 0 {
		t.Errorf("an undecodable program must stay embedded, got %v", got)
	}
}

// TestFontEmbedding_CutShortDeflateProgramIsJudged pins that the bytes
// FlateDecode keeps before its data ends are judged as the program, though
// the filter reports the stream damaged: veraPDF reads a program whose
// deflate data merely ends early (see [deflateDataCorrupt]), so a cut sfnt
// embeds the font and a cut program of another container does not. Only an
// ASCII filter's fragment reads as a program that will not decode.
func TestFontEmbedding_CutShortDeflateProgramIsJudged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	filler := strings.Repeat("glyph outlines and hinting instructions ", 60)
	for _, tc := range []struct {
		name    string
		program string
		fires   bool
	}{
		{"a cut sfnt embeds the font", "\x00\x01\x00\x00" + filler, false},
		{"a cut non-sfnt does not", "\x01\x00\x04\x03" + filler, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var z bytes.Buffer
			w := zlib.NewWriter(&z)
			if _, err := w.Write([]byte(tc.program)); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			cut := z.Bytes()[:z.Len()/2]
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
				"<< /Type /Font /Subtype /TrueType /BaseFont /Test /FirstChar 65 /LastChar 65"+
					" /Widths [722] /FontDescriptor 5 0 R >>",
				"<< /Type /FontDescriptor /FontName /Test /Flags 32 /FontFile2 6 0 R >>",
				fmt.Sprintf("<< /Length %d /Filter /FlateDecode >>\nstream\n%s\nendstream", len(cut), cut),
			)
			stream, err := document.ResolveStream(ctx, doc, ast.PdfIndirectRef{ObjectNum: 6})
			if err != nil {
				t.Fatalf("resolve program: %v", err)
			}
			if data, report, err := doc.DecodeStreamReport(ctx, stream); err != nil || len(data) < 4 ||
				!slices.Equal(report.Damaged, []ast.PdfName{"FlateDecode"}) {
				t.Fatalf("DecodeStreamReport = %d bytes, %+v, %v; want a prefix, FlateDecode damaged", len(data), report, err)
			}
			font, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
			if err != nil {
				t.Fatalf("resolve font: %v", err)
			}
			got := checkFontEmbedding(ctx, doc, font, "F1", 1, "PDF/A-2b", false)
			if fired := hasRule(got, Rule_19005_2_6_2_11_4_1_t01); fired != tc.fires {
				t.Errorf("embedding violation = %v, want %v", fired, tc.fires)
			}
		})
	}
}
