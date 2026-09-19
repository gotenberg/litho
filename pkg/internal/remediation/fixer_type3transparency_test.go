package remediation

import (
	"context"
	"sort"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestFixType3FontTransparency_ReachesTheFontsOwnExtGState covers the gap.
//
// veraPDF's 6-2-10-t04-fail-a and 6-2-9-t04-fail-a declare /Resources inline on
// the Type 3 font dict (obj 10) with /ExtGState /GS0 << /ca 0.5 >>, and carry no
// /ExtGState on the page, so every existing walk finds nothing.
func TestFixType3FontTransparency_ReachesTheFontsOwnExtGState(t *testing.T) {
	for _, tc := range []struct {
		name string
		gs   ast.PdfDict
		key  ast.PdfName
		want ast.PdfObject
	}{
		{"ca (6.4 t6)", ast.PdfDict{"ca": ast.PdfReal(0.5)}, "ca", ast.PdfReal(1.0)},
		{"CA (6.4 t5)", ast.PdfDict{"CA": ast.PdfReal(0.5)}, "CA", ast.PdfReal(1.0)},
		{"BM (6.4 t4)", ast.PdfDict{"BM": ast.PdfName("Multiply")}, "BM", ast.PdfName("Normal")},
		{"SMask (6.4 t1)", ast.PdfDict{"SMask": ast.PdfName("Alpha")}, "SMask", ast.PdfName("None")},
	} {
		pdf := type3FontPDF(t, tc.gs)
		doc, alloc := newTestDoc(t, pdf)
		pageRes := ast.PdfDict{"Font": ast.PdfDict{"F1": ast.PdfIndirectRef{ObjectNum: 10}}}

		n := fixType3FontTransparency(context.Background(), doc, alloc, pageRes, make(map[int]bool), 0)
		if n == 0 {
			t.Errorf("%s: an ExtGState inside a Type 3 font must be repaired", tc.name)
			continue
		}
		gs := pendingDict(t, alloc, 12)
		if got := gs[tc.key]; got != tc.want {
			t.Errorf("%s: /%s = %v, want %v", tc.name, tc.key, got, tc.want)
		}
	}
}

// TestFixType3FontTransparency_IgnoresNonType3Fonts is the scope control.
//
// A Type 1 or TrueType font has no /Resources of its own in this sense, and
// walking one would take the repair outside the shape compliance flags.
func TestFixType3FontTransparency_IgnoresNonType3Fonts(t *testing.T) {
	pdf := type3FontPDFWithSubtype(t, ast.PdfDict{"ca": ast.PdfReal(0.5)}, "Type1")
	doc, alloc := newTestDoc(t, pdf)
	pageRes := ast.PdfDict{"Font": ast.PdfDict{"F1": ast.PdfIndirectRef{ObjectNum: 10}}}

	if n := fixType3FontTransparency(context.Background(), doc, alloc, pageRes, make(map[int]bool), 0); n != 0 {
		t.Errorf("a non-Type3 font must be left alone, repaired %d", n)
	}
	if _, ok := alloc.Pending()[12]; ok {
		t.Error("nothing may be written for a non-Type3 font")
	}
}

// TestFixType3FontTransparency_LeavesConformingValuesAlone pins idempotence:
// ca already 1.0 and BM already Normal are conforming, so a second remediation
// pass must not rewrite them and inflate the file forever.
func TestFixType3FontTransparency_LeavesConformingValuesAlone(t *testing.T) {
	pdf := type3FontPDF(t, ast.PdfDict{"ca": ast.PdfReal(1.0), "BM": ast.PdfName("Normal")})
	doc, alloc := newTestDoc(t, pdf)
	pageRes := ast.PdfDict{"Font": ast.PdfDict{"F1": ast.PdfIndirectRef{ObjectNum: 10}}}

	if n := fixType3FontTransparency(context.Background(), doc, alloc, pageRes, make(map[int]bool), 0); n != 0 {
		t.Errorf("conforming values must not be rewritten, repaired %d", n)
	}
}

// TestFixType3FontTransparency_EmptyInputRepairsNothing asserts that a page
// with no resources, and one with no /Font, repair nothing.
//
// HONEST LIMIT: this is NOT a pin on the `if resources == nil` guard. Deleting
// that guard was tried by mutation and this test still PASSED, because reading
// a key from a nil map is legal Go and yields nil, so the ResolveDict below
// errors and the function returns 0 regardless. The guard is kept for symmetry
// with fixPatternTransparency and fixXObjectTransparency, which carry the same
// line, not because this test defends it. What the test does defend is the
// behaviour: empty input must not be treated as "walk the page instead".
func TestFixType3FontTransparency_EmptyInputRepairsNothing(t *testing.T) {
	doc, alloc := newTestDoc(t, minimalCatalogPDF(""))
	if n := fixType3FontTransparency(context.Background(), doc, alloc, nil, make(map[int]bool), 0); n != 0 {
		t.Errorf("nil resources must repair nothing, got %d", n)
	}
	if n := fixType3FontTransparency(context.Background(), doc, alloc, ast.PdfDict{}, make(map[int]bool), 0); n != 0 {
		t.Errorf("resources without /Font must repair nothing, got %d", n)
	}
}

// type3FontPDF builds a one-page PDF whose page /Resources declares only /Font,
// and whose Type 3 font (obj 10) declares /Resources inline with an /ExtGState
// (obj 11) holding /GS0 (obj 12). That mirrors the corpus carriers exactly.
func type3FontPDF(t *testing.T, gs ast.PdfDict) []byte {
	t.Helper()
	return type3FontPDFWithSubtype(t, gs, "Type3")
}

func type3FontPDFWithSubtype(t *testing.T, gs ast.PdfDict, subtype string) []byte {
	t.Helper()
	var keys []string
	for k := range gs {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	var sb string
	for _, k := range keys {
		switch val := gs[ast.PdfName(k)].(type) {
		case ast.PdfReal:
			sb += " /" + k + " " + formatReal(float64(val))
		case ast.PdfName:
			sb += " /" + k + " /" + string(val)
		}
	}
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/Resources << /Font << /F1 10 0 R >> >> >>\nendobj\n" +
			"10 0 obj\n<< /Type /Font /Subtype /" + subtype + " /Resources 11 0 R >>\nendobj\n" +
			"11 0 obj\n<< /ExtGState << /GS0 12 0 R >> >>\nendobj\n" +
			"12 0 obj\n<<" + sb + " >>\nendobj\n" +
			"trailer\n<< /Size 13 /Root 1 0 R >>\n%%EOF\n")
}

// formatReal writes a PDF real exactly, with no hand-coded cases: a fallback
// that silently substitutes 0 would mis-serialise any future fixture value.
func formatReal(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}
