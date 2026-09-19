package remediation

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
)

// chainCMapProgram is one CMap program of a usecmap chain.
func chainCMapProgram(name string, lo, hi, cid int) string {
	return "/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n" +
		"/CIDSystemInfo 3 dict dup begin /Registry (Adobe) def /Ordering (Japan1) def /Supplement 2 def end def\n" +
		"/CMapName /" + name + " def\n/CMapType 1 def\n/WMode 0 def\n" +
		"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n" +
		fmt.Sprintf("1 begincidrange\n<%04X> <%04X> %d\nendcidrange\n", lo, hi, cid) +
		"endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n"
}

// cmapChainPDF builds a Type 0 font whose /Encoding is an embedded CMap
// deferring to a second embedded CMap through /UseCMap, the shape ISO 19005-2,
// 6.2.11.3.3 test 3 rejects.
func cmapChainPDF() []byte {
	using := chainCMapProgram("UsingCMap", 0x0000, 0x000F, 900)
	base := chainCMapProgram("BaseCMap", 0x0000, 0x01FF, 0)
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /Font << /F1 4 0 R >> >> >>",
		"<< /Type /Font /Subtype /Type0 /BaseFont /Probe /Encoding 6 0 R "+
			"/DescendantFonts [5 0 R] >>",
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Probe "+
			"/CIDSystemInfo << /Registry (Adobe) /Ordering (Japan1) /Supplement 2 >> >>",
		fmt.Sprintf("<< /Type /CMap /CMapName /UsingCMap /WMode 0 /UseCMap 7 0 R "+
			"/CIDSystemInfo << /Registry (Adobe) /Ordering (Japan1) /Supplement 2 >> "+
			"/Length %d >>\nstream\n%s\nendstream", len(using), using),
		fmt.Sprintf("<< /Type /CMap /CMapName /BaseCMap /WMode 0 "+
			"/CIDSystemInfo << /Registry (Adobe) /Ordering (Japan1) /Supplement 4 >> "+
			"/Length %d >>\nstream\n%s\nendstream", len(base), base),
	)
}

// flattenedCMap reads back the encoding CMap the fixer persisted at object 6.
func flattenedCMap(t *testing.T, alloc *writer.ObjAllocator) (ast.PdfStream, bool) {
	t.Helper()

	pending, ok := alloc.Pending()[6]
	if !ok {
		return ast.PdfStream{}, false
	}
	stream, ok := pending.(ast.PdfStream)
	return stream, ok
}

// TestFixCMapFlatten_InlinesTheChain drives the fixer end to end and checks
// what the corpus cannot: a validator is satisfied by the reference merely
// being gone, so only this asserts the mapping it contributed survived.
func TestFixCMapFlatten_InlinesTheChain(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, cmapChainPDF())

	fixes, err := fixCMapFlatten(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixCMapFlatten: %v", err)
	}
	if len(fixes) != 1 {
		t.Fatalf("reported %d fixes, want 1: %v", len(fixes), fixes)
	}

	stream, ok := flattenedCMap(t, alloc)
	if !ok {
		t.Fatal("the encoding CMap was not rewritten")
	}
	if _, still := stream.Dict["UseCMap"]; still {
		t.Error("/UseCMap survived, so the reference the rule forbids is still there")
	}
	// Writing decoded bytes means the entries describing the old storage go.
	for _, gone := range []ast.PdfName{"Length", "Filter", "DecodeParms"} {
		if _, present := stream.Dict[gone]; present {
			t.Errorf("%s survived onto a stream carrying decoded bytes", gone)
		}
	}
	// The CMap must still say what it is.
	if name, _ := stream.Dict["CMapName"].(ast.PdfName); name != "UsingCMap" {
		t.Errorf("/CMapName is %q, want UsingCMap: renaming asserts a collection the glyphs lack", name)
	}

	data, err := stream.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading the rewritten CMap: %v", err)
	}
	got, err := ParseCIDForTest(ctx, data)
	if err != nil {
		t.Fatalf("the rewritten CMap does not parse: %v", err)
	}
	if len(got) != 0x200 {
		t.Errorf("the flattened CMap maps %d codes, want 512", len(got))
	}
	// 0x0100 exists only because the base contributed it. If inlining were
	// skipped and the reference merely dropped, every validator would still be
	// satisfied and this glyph would silently unmap.
	if got[0x0100] != 0x100 {
		t.Errorf("code 0x0100 maps to %d, want 256: the base's mapping was lost", got[0x0100])
	}
	if got[0x0000] != 900 {
		t.Errorf("code 0x0000 maps to %d, want 900: the using CMap must win", got[0x0000])
	}
	if strings.Contains(string(data), "usecmap") {
		t.Error("a usecmap invocation survived into the rewritten program")
	}
}

// TestMatchCMapFlattenClaimsOnlyTheChainRule pins the context gate. The same
// clauses also carry the WMode disagreement, which a different fixer settles,
// and claiming it here would run this fixer on documents it cannot help.
func TestMatchCMapFlattenClaimsOnlyTheChainRule(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		clause  string
		context string
		want    bool
	}{
		// The contexts are the strings fonts_cmap.go actually emits.
		{"ISO 19005-2 chain", "6.2.11.3.3",
			`font F1 on page 1: CMap chain references non-predefined CMap "Adobe-Japan1-4"`, true},
		{"ISO 19005-4 chain", "6.2.10.3.3",
			`font F1 on page 1: CMap chain references non-predefined CMap "Adobe-Japan1-4"`, true},
		{"ISO 14289-1 chain", "7.21.3.3",
			`font F1 on page 1: CMap chain references non-predefined CMap "Adobe-Korea1-2"`, true},
		{"ISO 14289-2 chain", "8.4.5.4",
			`font F1 on page 1: CMap chain references non-predefined CMap "Adobe-Korea1-2"`, true},
		{"the WMode disagreement under the same clause", "6.2.11.3.3",
			"font F1 on page 1: CMap dict WMode=1 but stream WMode=0", false},
		{"an unrelated font clause", "6.2.11.5",
			`font F1 on page 1: CMap chain references non-predefined CMap "X"`, false},
	} {
		v := compliance.Violation{
			Rule:    compliance.Rule{Clause: tc.clause, TestNumber: 3},
			Context: tc.context,
		}
		if got := matchCMapFlatten(v); got != tc.want {
			t.Errorf("%s: matchCMapFlatten = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// ParseCIDForTest exposes the CID mapping of a CMap program.
func ParseCIDForTest(ctx context.Context, data []byte) (map[int]int, error) {
	c, err := cmap.ParseCID(ctx, data)
	if err != nil {
		return nil, err
	}
	return c.Mapping, nil
}
