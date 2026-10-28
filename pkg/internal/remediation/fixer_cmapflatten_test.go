package remediation

import (
	"bytes"
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

// namedUseCMapPDF builds an embedded CMap whose /UseCMap is a NAME rather than
// a reference, so there is no second stream to inline.
//
// # Corpus evidence
//
//   - All corpora: this is the shape of the Adobe-Korea1-2 carriers.
func namedUseCMapPDF(useName string) []byte {
	prog := chainCMapProgram("MyCMap", 0x0000, 0x00FF, 0)
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /Font << /F1 4 0 R >> >> >>",
		"<< /Type /Font /Subtype /Type0 /BaseFont /Probe /Encoding 6 0 R "+
			"/DescendantFonts [5 0 R] >>",
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Probe "+
			"/CIDSystemInfo << /Registry (Adobe) /Ordering (Korea1) /Supplement 2 >> >>",
		fmt.Sprintf("<< /Type /CMap /CMapName /MyCMap /WMode 0 /UseCMap /%s "+
			"/CIDSystemInfo << /Registry (Adobe) /Ordering (Korea1) /Supplement 2 >> "+
			"/Length %d >>\nstream\n%s\nendstream", useName, len(prog), prog),
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
// what a validator cannot: a validator is satisfied by the reference merely
// being gone, so only this asserts the mapping it contributed survived.
//
// # Corpus evidence
//
//   - All corpora: the corpus gate cannot check that the mapping survived,
//     because a validator is satisfied by the reference merely being gone.
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

// TestFixCMapFlatten_DropsOnlyAnUnresolvableUseCMapName is the arm that keeps
// this repair honest.
//
// A /UseCMap naming a character collection CMap no reader holds resolves to
// nothing, so dropping it costs no mapping. A /UseCMap naming a CMap a reader
// resolves contributes mappings, and dropping it would take them away: a
// Table 118 name stays, and a collection CMap veraPDF bundles (Adobe-Korea1-2,
// Adobe-KR-9) is inlined, every code keeping its CID. veraPDF cannot tell the
// three apart: it is satisfied by the entry being gone either way, and under
// ISO 19005-1, 6.3.3.3 it reports even the legitimate names. A fixer that
// deleted every name would therefore score HIGHER against veraPDF while
// corrupting files. Only this test separates them.
//
// # Corpus evidence
//
//   - All corpora: a fixer that deleted every name would score HIGHER on the
//     corpus while corrupting files.
//   - verapdf PDF_UA-1__7.21.3.3-t03-fail-a.pdf and
//     PDF_UA-2__8.4.5.4-t03-fail-a.pdf: name Adobe-Korea1-2, and remediate to
//     a file both tools call conforming at all thirteen levels whether the
//     base is dropped or inlined; only inlining keeps the codes' CIDs.
func TestFixCMapFlatten_DropsOnlyAnUnresolvableUseCMapName(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		useName string
		drop    bool
		inline  bool
	}{
		{"a collection CMap no reader holds names nothing", "Adobe-Foo1-3", true, false},
		{"a collection CMap veraPDF bundles is inlined", "Adobe-Korea1-2", false, true},
		{"a collection CMap only veraPDF bundles is inlined", "Adobe-KR-9", false, true},
		// ISO 32000-1, Table 118 names all of these, and litho bundles H and V.
		{"H is a real predefined CMap", "H", false, false},
		{"Identity-H is predefined", "Identity-H", false, false},
		{"UniKS-UTF16-H is predefined", "UniKS-UTF16-H", false, false},
		// Shaped like a collection but not Adobe's, so not the case this claims.
		{"a three-part name that is not Adobe's", "Foo-Bar-2", false, false},
	} {
		src := namedUseCMapPDF(tc.useName)
		doc, alloc := newTestDoc(t, src)

		fixes, err := fixCMapFlatten(ctx, alloc, doc, Options{}, nil, "PDF/UA-1")
		if err != nil {
			t.Errorf("%s: fixCMapFlatten: %v", tc.name, err)
			continue
		}

		stream, written := flattenedCMap(t, alloc)
		if tc.inline {
			if !written {
				t.Errorf("%s: /UseCMap /%s was left in place", tc.name, tc.useName)
				continue
			}
			if _, still := stream.Dict["UseCMap"]; still {
				t.Errorf("%s: the program was inlined but /UseCMap survived", tc.name)
			}
			got, err := stream.Bytes(ctx)
			if err != nil {
				t.Errorf("%s: reading the rewritten CMap: %v", tc.name, err)
				continue
			}
			mapping, err := ParseCIDForTest(ctx, got)
			if err != nil {
				t.Errorf("%s: parsing the rewritten CMap: %v", tc.name, err)
				continue
			}
			// The program maps 0000 to 00FF itself; the base maps 0100 to CID
			// 256, as the identity over the collection's CIDs.
			if mapping[0x0010] != 0x10 || mapping[0x0100] != 0x100 {
				t.Errorf("%s: codes 0010 and 0100 map to %d and %d, want 16 and 256",
					tc.name, mapping[0x0010], mapping[0x0100])
			}
			continue
		}
		if !tc.drop {
			if written {
				t.Errorf("%s: /UseCMap /%s was removed, taking the mappings it resolves to with it",
					tc.name, tc.useName)
			}
			if len(fixes) != 0 {
				t.Errorf("%s: reported a fix for a CMap it must leave alone: %v", tc.name, fixes)
			}
			continue
		}
		if !written {
			t.Errorf("%s: /UseCMap /%s names no CMap and was left in place", tc.name, tc.useName)
			continue
		}
		if _, still := stream.Dict["UseCMap"]; still {
			t.Errorf("%s: the dictionary was rewritten but /UseCMap survived", tc.name)
		}
		// A dictionary-only change: the body is carried across untouched, so
		// the entries describing how it is stored must stay with it.
		for _, keep := range []ast.PdfName{"Length", "CMapName", "CIDSystemInfo", "WMode"} {
			if _, present := stream.Dict[keep]; !present {
				t.Errorf("%s: %s was dropped by a change that only removes /UseCMap", tc.name, keep)
			}
		}
		got, err := stream.Bytes(ctx)
		if err != nil {
			t.Errorf("%s: reading the rewritten CMap: %v", tc.name, err)
			continue
		}
		want := chainCMapProgram("MyCMap", 0x0000, 0x00FF, 0)
		if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace([]byte(want))) {
			t.Errorf("%s: the CMap program changed; dropping a name must not touch the body", tc.name)
		}
	}
}

// TestFixCMapFlatten_InlinesAUseCMapOperand pins the repair of a usecmap
// operator whose operand is no Table 118 CMap, the shape ISO 19005-2,
// 6.2.11.3.3 test 3 rejects in litho's reading: Adobe-KR-9, which veraPDF
// 1.30.2 loads, is inlined, so the program maps what the base mapped and runs
// no usecmap. A Table 118 operand is left alone.
//
// # Corpus evidence
//
//   - litho cmapverabase_2b.pdf: K1 to K3 run usecmap on Adobe-KR-9 and
//     Adobe-Japan1-7. Without the repair litho kept 6.2.11.3.3 t3 after
//     remediation at eleven levels, where veraPDF, which judges /UseCMap
//     only, called the base gone.
func TestFixCMapFlatten_InlinesAUseCMapOperand(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, tc := range []struct {
		operand string
		inline  bool
	}{
		{"Adobe-KR-9", true},
		{"Identity-H", false},
	} {
		prog := "/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n" +
			"/CIDSystemInfo 3 dict dup begin /Registry (Adobe) def /Ordering (KR) def /Supplement 9 def end def\n" +
			"/CMapName /MyCMap def\n/CMapType 1 def\n/WMode 0 def\n" +
			"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n" +
			"/" + tc.operand + " usecmap\n1 beginbfchar\n<0041> <0041>\nendbfchar\n" +
			"endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n"
		src := buildStructPDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
				"/Resources << /Font << /F1 4 0 R >> >> >>",
			"<< /Type /Font /Subtype /Type0 /BaseFont /Probe /Encoding 6 0 R "+
				"/DescendantFonts [5 0 R] >>",
			"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Probe "+
				"/CIDSystemInfo << /Registry (Adobe) /Ordering (KR) /Supplement 9 >> >>",
			fmt.Sprintf("<< /Type /CMap /CMapName /MyCMap /WMode 0 "+
				"/CIDSystemInfo << /Registry (Adobe) /Ordering (KR) /Supplement 9 >> "+
				"/Length %d >>\nstream\n%s\nendstream", len(prog), prog),
		)
		doc, alloc := newTestDoc(t, src)
		if _, err := fixCMapFlatten(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
			t.Fatalf("%s: fixCMapFlatten: %v", tc.operand, err)
		}
		stream, written := flattenedCMap(t, alloc)
		if !tc.inline {
			if written {
				t.Errorf("%s: a Table 118 operand was rewritten", tc.operand)
			}
			continue
		}
		if !written {
			t.Fatalf("%s: the usecmap operand was left in place", tc.operand)
		}
		got, err := stream.Bytes(ctx)
		if err != nil {
			t.Fatalf("%s: reading the rewritten CMap: %v", tc.operand, err)
		}
		if strings.Contains(string(got), "usecmap") {
			t.Errorf("%s: a usecmap invocation survived the inlining", tc.operand)
		}
		mapping, err := ParseCIDForTest(ctx, got)
		if err != nil {
			t.Fatalf("%s: parsing the rewritten CMap: %v", tc.operand, err)
		}
		if mapping[0x0005] != 5 || mapping[0x5970] != 22896 {
			t.Errorf("%s: codes 0005 and 5970 map to %d and %d, want 5 and 22896",
				tc.operand, mapping[0x0005], mapping[0x5970])
		}
		if _, mapped := mapping[0x5971]; mapped {
			t.Errorf("%s: code 5971, past Adobe-KR-9's CIDs, gained a mapping", tc.operand)
		}
	}
}

// TestMatchCMapFlattenClaimsBothUseCMapRules pins the context gate. The same
// clauses also carry the WMode disagreement, which a different fixer settles,
// and claiming it here would run this fixer on documents it cannot help.
func TestMatchCMapFlattenClaimsBothUseCMapRules(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		clause  string
		context string
		want    bool
	}{
		// The contexts are the strings fonts_cmap.go actually emits.
		{
			"ISO 19005-2 chain", "6.2.11.3.3",
			`font F1 on page 1: CMap chain references non-predefined CMap "Adobe-Japan1-4"`, true,
		},
		{
			"ISO 19005-4 chain", "6.2.10.3.3",
			`font F1 on page 1: CMap chain references non-predefined CMap "Adobe-Japan1-4"`, true,
		},
		{
			"ISO 14289-1 chain", "7.21.3.3",
			`font F1 on page 1: CMap chain references non-predefined CMap "Adobe-Korea1-2"`, true,
		},
		{
			"ISO 14289-2 chain", "8.4.5.4",
			`font F1 on page 1: CMap chain references non-predefined CMap "Adobe-Korea1-2"`, true,
		},
		{
			"ISO 19005-1 embedding", "6.3.3.3",
			`font F1 on page 1: CMap uses non-embedded CMap "Adobe-Korea1-2" via /UseCMap`, true,
		},
		{
			"ISO 19005-2 embedding", "6.2.11.3.3",
			`font F1 on page 1: CMap uses non-embedded CMap "Adobe-Korea1-2" via /UseCMap`, true,
		},
		{
			"ISO 14289-2 embedding", "8.4.5.4",
			`font F1 on page 1: CMap uses non-embedded CMap "Adobe-Korea1-2" via /UseCMap`, true,
		},
		{
			"ISO 19005-2 named encoding", "6.2.11.3.3",
			`font F1 on page 1: CMap "UniAKR-UTF16-H" is not predefined and not embedded`, true,
		},
		{
			"ISO 19005-1 named encoding", "6.3.3.3",
			`font F1 on page 1: CMap "UniAKR-UTF16-H" is not predefined and not embedded`, true,
		},
		{
			"the WMode disagreement under the same clause", "6.2.11.3.3",
			"font F1 on page 1: CMap dict WMode=1 but stream WMode=0", false,
		},
		{
			"an unrelated font clause", "6.2.11.5",
			`font F1 on page 1: CMap chain references non-predefined CMap "X"`, false,
		},
	} {
		v := compliance.Violation{
			Rule:    compliance.Rule{Clause: tc.clause, TestNumber: 1},
			Context: tc.context,
		}
		if got := matchCMapFlatten(v); got != tc.want {
			t.Errorf("%s: matchCMapFlatten = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// ParseCIDForTest exposes the CID mapping of a CMap program.
func ParseCIDForTest(ctx context.Context, data []byte) (map[int64]int, error) {
	c, err := cmap.ParseCID(ctx, data)
	if err != nil {
		return nil, err
	}
	return c.Mapping, nil
}
