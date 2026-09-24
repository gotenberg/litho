package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestPredefinedCMapTable116 pins that every predefined CMap of ISO 32000-2,
// Table 116 is exempt from the embedding requirement.
//
// ISO 14289-2, 8.4.5.4 requires embedding for "all CMaps used within a file
// ... except those listed in ISO 32000-2:2020, Table 116", and ISO 19005-2's
// 6.2.11.3.3 is worded the same way. litho restricted the PDF/UA-2 exemption to
// the two Identity CMaps, and its table reached the Unicode families and
// Identity but none of the legacy CJK ones, so a font encoded with GB-EUC-H,
// 90ms-RKSJ-H or the bare H was reported as neither predefined nor embedded.
//
// PDF/A-1 keeps its own narrower rule: ISO 19005-1, 6.3.3.3 admits only the
// Identity CMaps, so a predefined CJK name still fails there.
//
// # Corpus evidence
//
//   - pdfjs issue18466.pdf, mixedfonts.pdf: use UniGB-UTF16-H and
//     UniJIS-UTF16-H respectively, the two Table 116 names the corpora carry,
//     and veraPDF passes both.
func TestPredefinedCMapTable116(t *testing.T) {
	t.Parallel()

	// pdf builds a page showing one code through a Type 0 font whose /Encoding
	// names the given CMap without embedding it.
	pdf := func(cmapName string) []byte {
		const stream = "BT /F1 12 Tf 10 50 Td <0001> Tj ET"
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 7 0 R /MarkInfo << /Marked true >> >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
				"/Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
			"<< /Type /Font /Subtype /Type0 /BaseFont /Test /Encoding /"+cmapName+" "+
				"/DescendantFonts [6 0 R] >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
			"<< /Type /Font /Subtype /CIDFontType0 /BaseFont /Test /DW 1000 "+
				"/CIDSystemInfo << /Registry (Adobe) /Ordering (Japan1) /Supplement 6 >> "+
				"/FontDescriptor 8 0 R >>",
			"<< /Type /StructTreeRoot >>",
			"<< /Type /FontDescriptor /FontName /Test /Flags 4 /ItalicAngle 0 /Ascent 700 "+
				"/Descent -200 /CapHeight 700 /StemV 80 /FontBBox [0 0 1000 1000] >>",
		)
	}

	fires := func(t *testing.T, data []byte, profile models.PDFAProfile, clause string) bool {
		t.Helper()
		doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		viols, err := ValidatePDFA(context.Background(), doc, profile)
		if err != nil {
			t.Fatalf("ValidatePDFA: %v", err)
		}
		for _, e := range viols {
			if v, ok := e.(Violation); ok && v.Rule.Clause == clause {
				return true
			}
		}
		return false
	}

	firesUA2 := func(t *testing.T, data []byte) bool {
		t.Helper()
		doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		viols, err := ValidatePDFUA(context.Background(), doc, models.PDFUA2)
		if err != nil {
			t.Fatalf("ValidatePDFUA: %v", err)
		}
		for _, e := range viols {
			if v, ok := e.(Violation); ok && v.Rule.Clause == "8.4.5.4" {
				return true
			}
		}
		return false
	}

	// One name from each family Table 116 lists, including UniGB-UTF16-H,
	// UniJIS-UTF16-H and the bare H that no prefix rule would ever match.
	predefined := []string{
		"UniGB-UTF16-H", "UniJIS-UTF16-H", "UniKS-UCS2-V", "UniCNS-UTF16-H",
		"GB-EUC-H", "GBK2K-V", "B5pc-H", "ETen-B5-V", "90ms-RKSJ-H",
		"Ext-RKSJ-V", "KSCms-UHC-HW-H", "H", "V", "Identity-H",
	}
	for _, name := range predefined {
		t.Run(name+" is predefined at UA-2", func(t *testing.T) {
			t.Parallel()
			if firesUA2(t, pdf(name)) {
				t.Errorf("%s is listed in ISO 32000-2 Table 116 and needs no embedding", name)
			}
		})
		t.Run(name+" is predefined at A-2b", func(t *testing.T) {
			t.Parallel()
			if fires(t, pdf(name), models.PDFA2b, "6.2.11.3.3") {
				t.Errorf("%s is listed in ISO 32000-2 Table 116 and needs no embedding", name)
			}
		})
	}

	t.Run("a name outside Table 116 still fires", func(t *testing.T) {
		t.Parallel()
		if !firesUA2(t, pdf("NotACMapName-H")) {
			t.Error("a CMap Table 116 does not list must be embedded")
		}
	})

	// ISO 19005-1, 6.3.3.3 is narrower than Table 116 and admits only Identity.
	t.Run("PDF/A-1 admits only Identity", func(t *testing.T) {
		t.Parallel()
		if !fires(t, pdf("UniJIS-UTF16-H"), models.PDFA1b, "6.3.3.3") {
			t.Error("ISO 19005-1 6.3.3.3 requires every CMap but Identity to be embedded")
		}
	})
}
