package typography

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
	"github.com/gotenberg/litho/pkg/models"
)

// utf8CMapProgram is an encoding CMap of the shape Haru writes for UTF-8
// text (pdfjs issue20232.pdf): one-byte codes for ASCII, two-byte codes from
// <C280>, each range mapped to the CIDs of its code points.
const utf8CMapProgram = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap\n" +
	"3 begincodespacerange\n<00> <7F>\n<C280> <DFBF>\n<E0A080> <EFBFBF>\nendcodespacerange\n" +
	"3 begincidrange\n<00> <7F> 0\n<D080> <D0BF> 1024\n<E0A080> <E0A0BF> 2048\nendcidrange\n" +
	"endcmap end end\n"

// utf8ToUnicodeProgram maps the same codes to Unicode.
const utf8ToUnicodeProgram = "begincmap\n" +
	"3 begincodespacerange\n<00> <7F>\n<C280> <DFBF>\n<E0A080> <EFBFBF>\nendcodespacerange\n" +
	"3 beginbfchar\n<41> <0041>\n<D09F> <041F>\n<E0A085> <0805>\nendbfchar\n" +
	"endcmap\n"

// encodingCIDDocument builds a document whose object 5 is a CIDFontType2
// with /W naming CIDs 65 and 1055 and /DW 687, and whose objects 6 and 7 are
// the encoding and ToUnicode CMaps above.
func encodingCIDDocument(t *testing.T, w2 string) *document.Document {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")
	buf.WriteString("5 0 obj\n<< /Type /Font /Subtype /CIDFontType2 /BaseFont /GOST " +
		"/CIDSystemInfo << /Registry (Haru) /Ordering (Modern1) /Supplement 0 >> " +
		"/DW 687 /W [65 [500] 1055 [610] 2053 [720]]" + w2 + " >>\nendobj\n")
	fmt.Fprintf(&buf, "6 0 obj\n<< /Type /CMap /CMapName /Modern-UTF8-H /Length %d >>\nstream\n%s\nendstream\nendobj\n",
		len(utf8CMapProgram)+1, utf8CMapProgram)
	fmt.Fprintf(&buf, "7 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n",
		len(utf8ToUnicodeProgram)+1, utf8ToUnicodeProgram)
	buf.WriteString("trailer\n<< /Size 8 /Root 1 0 R >>\n%%EOF\n")
	data := buf.Bytes()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

func encodingCIDFont(t *testing.T, doc *document.Document, enc ast.PdfObject) *Font {
	t.Helper()
	font, err := ResolveFont(context.Background(), doc, ast.PdfDict{
		"Type":            ast.PdfName("Font"),
		"Subtype":         ast.PdfName("Type0"),
		"BaseFont":        ast.PdfName("GOST"),
		"Encoding":        enc,
		"ToUnicode":       ast.PdfIndirectRef{ObjectNum: 7},
		"DescendantFonts": ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 5}},
	})
	if err != nil {
		t.Fatalf("ResolveFont: %v", err)
	}
	return font
}

// showAt10 shows s in font at 10 pt from the origin and returns the elements
// and the text matrix's final X and Y.
func showAt10(t *testing.T, font *Font, s string) ([]models.TextElement, float64, float64) {
	t.Helper()
	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	state := machine.Current()
	state.Text.Tm = [6]float64{1, 0, 0, 1, 0, 0}
	state.Text.Tfs = 10
	var elements []models.TextElement
	if err := processText(context.Background(), ast.PdfString(s), font, machine, &elements, nil); err != nil {
		t.Fatalf("processText: %v", err)
	}
	return elements, state.Text.Tm[4], state.Text.Tm[5]
}

// TestProcessText_WidthsByCIDUnderEmbeddedCMap pins that a Type 0 font under
// an embedded encoding CMap takes each glyph's width from /W at the CID the
// CMap maps its code to (ISO 32000-1, 9.7.4.3 and 9.7.6.2), not at the code:
// <D09F> selects CID 1055, which /W gives 610, where the code 0xD09F, which
// /W does not name, took /DW 687.
func TestProcessText_WidthsByCIDUnderEmbeddedCMap(t *testing.T) {
	t.Parallel()

	doc := encodingCIDDocument(t, "")
	font := encodingCIDFont(t, doc, ast.PdfIndirectRef{ObjectNum: 6})

	elements, x, _ := showAt10(t, font, "A\xD0\x9F")
	if len(elements) != 2 || elements[0].Text != "A" || elements[1].Text != "П" {
		t.Fatalf("got %+v, want A then П", elements)
	}
	if want := 6.1; math.Abs(elements[1].Width-want) > 1e-9 {
		t.Errorf("П width = %v, want %v from /W at CID 1055", elements[1].Width, want)
	}
	if want := 5.0 + 6.1; math.Abs(x-want) > 1e-9 {
		t.Errorf("text matrix X after the string = %v, want %v", x, want)
	}

	// A three-byte code is one code too: <E0A085> selects CID 2053.
	elements, _, _ = showAt10(t, font, "\xE0\xA0\x85")
	if len(elements) != 1 {
		t.Fatalf("got %d elements for one three-byte code, want 1", len(elements))
	}
	if want := 7.2; math.Abs(elements[0].Width-want) > 1e-9 {
		t.Errorf("three-byte code width = %v, want %v from /W at CID 2053", elements[0].Width, want)
	}
}

// TestProcessText_VerticalMetricsByCIDUnderEmbeddedCMap is the /W2
// counterpart: a vertical font's advance comes from /W2 at the CID.
func TestProcessText_VerticalMetricsByCIDUnderEmbeddedCMap(t *testing.T) {
	t.Parallel()

	doc := encodingCIDDocument(t, " /WMode 1 /W2 [1055 [-800 305 880]]")
	font := encodingCIDFont(t, doc, ast.PdfIndirectRef{ObjectNum: 6})
	if font.WMode != 1 {
		t.Fatalf("WMode = %d, want 1", font.WMode)
	}

	_, _, y := showAt10(t, font, "\xD0\x9F")
	if want := -8.0; math.Abs(y-want) > 1e-9 {
		t.Errorf("text matrix Y after П = %v, want %v from /W2 at CID 1055", y, want)
	}
}

// TestProcessText_WidthsByCodeUnderIdentity is the control: under
// Identity-H the two-byte code is the CID, so /W is read at the code.
func TestProcessText_WidthsByCodeUnderIdentity(t *testing.T) {
	t.Parallel()

	doc := encodingCIDDocument(t, "")
	font := encodingCIDFont(t, doc, ast.PdfName("Identity-H"))
	if font.encodingCIDs != nil {
		t.Fatal("Identity-H resolved an encoding CID map")
	}
	if got := font.metricCID([]byte{0x04, 0x1F}, 0x041F); got != 0x041F {
		t.Errorf("metricCID under Identity-H = %d, want the code %d", got, 0x041F)
	}
}

// TestMetricCID_BfCharEncodingCMap pins the reading of an encoding CMap
// written with bfchar alone (pdfjs bug920426.pdf): <0020> maps to <0003>,
// which the metrics lookup reads as CID 3, as PDF.js does, and a code no
// entry names keeps its own value.
func TestMetricCID_BfCharEncodingCMap(t *testing.T) {
	t.Parallel()

	bf, err := cmap.Parse(context.Background(), []byte("begincmap\n"+
		"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n"+
		"1 beginbfchar\n<0020> <0003>\nendbfchar\nendcmap\n"))
	if err != nil {
		t.Fatalf("cmap.Parse: %v", err)
	}
	font := &Font{Subtype: "Type0", encodingCIDs: &encodingCIDs{bf: bf}}
	if got := font.metricCID([]byte{0x00, 0x20}, 0x20); got != 3 {
		t.Errorf("metricCID(<0020>) = %d, want 3", got)
	}
	if got := font.metricCID([]byte{0x00, 0x41}, 0x41); got != 0x41 {
		t.Errorf("metricCID(<0041>) = %d, want the code %d", got, 0x41)
	}
}
