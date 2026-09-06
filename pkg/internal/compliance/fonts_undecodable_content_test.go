package compliance

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestFontEmbeddingCheckedWhenContentWillNotDecode pins that a page whose
// content stream cannot be decoded still has its declared fonts judged.
//
// The per-font skip exists to match veraPDF, which builds a font object only
// for a font a content operator selects, and it answers conservatively when the
// content is present but unparseable, because litho cannot prove non-use. A
// stream that fails to DECODE never reached that fallback: the error was
// discarded and the page looked like one with no content at all, so every font
// on it was skipped as unused. pdfjs issue17190_1.pdf declares one unembedded
// TrueType font behind a truncated Flate stream, and veraPDF 1.30.2 reports the
// embedding failure while litho reported nothing at all.
func TestFontEmbeddingCheckedWhenContentWillNotDecode(t *testing.T) {
	t.Parallel()

	// /Filter /FlateDecode over bytes that are not a valid Flate stream, so
	// decoding fails rather than yielding empty content.
	const brokenFlate = "<< /Length 9 /Filter /FlateDecode >>\nstream\nnotflate!\nendstream"

	data := cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R"+
			" /Resources << /Font << /F1 5 0 R >> >> >>",
		brokenFlate,
		"<< /Type /Font /Subtype /TrueType /BaseFont /Arial /FirstChar 32 /LastChar 32"+
			" /Widths [500] /Encoding /WinAnsiEncoding /FontDescriptor 6 0 R >>",
		// A descriptor with no /FontFile2: the font program is not embedded.
		"<< /Type /FontDescriptor /FontName /Arial /Flags 32 /FontBBox [0 0 1000 1000]"+
			" /ItalicAngle 0 /Ascent 900 /Descent -200 /CapHeight 700 /StemV 80 >>",
	)

	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFA(context.Background(), doc, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}

	got := 0
	for _, e := range viols {
		if v, ok := e.(Violation); ok && v.Rule.Clause == "6.2.11.4.1" {
			got++
		}
	}
	if got == 0 {
		t.Error("no 6.2.11.4.1 violation: a font declared on a page whose content will not decode was skipped as unused")
	}
}

// TestFontEmbeddingSkippedForUndecodableFilter pins the other half: a content
// stream whose /Filter names something litho cannot decode at all is left
// unparsed on purpose, and its fonts stay out of scope.
//
// veraPDF does not parse that stream either, so checking every declared font
// against it would report defects veraPDF never raises. The bad filter has its
// own rule (ISO 19005-2, 6.1.7.2).
func TestFontEmbeddingSkippedForUndecodableFilter(t *testing.T) {
	t.Parallel()

	const badFilter = "<< /Length 5 /Filter /Flatedecode >>\nstream\nhello\nendstream"

	data := cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R"+
			" /Resources << /Font << /F1 5 0 R >> >> >>",
		badFilter,
		"<< /Type /Font /Subtype /TrueType /BaseFont /Arial /FirstChar 32 /LastChar 32"+
			" /Widths [500] /Encoding /WinAnsiEncoding /FontDescriptor 6 0 R >>",
		"<< /Type /FontDescriptor /FontName /Arial /Flags 32 /FontBBox [0 0 1000 1000]"+
			" /ItalicAngle 0 /Ascent 900 /Descent -200 /CapHeight 700 /StemV 80 >>",
	)

	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFA(context.Background(), doc, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}

	for _, e := range viols {
		if v, ok := e.(Violation); ok && v.Rule.Clause == "6.2.11.4.1" {
			t.Error("6.2.11.4.1 reported for a stream litho deliberately does not parse")
			return
		}
	}
}
