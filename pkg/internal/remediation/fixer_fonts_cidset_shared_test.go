package remediation

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// sharedDescriptorPDF builds a page showing two Type 0 fonts (objects 4 and 8)
// whose CIDFontType2 descendants (5 and 9) name one font descriptor, object 6,
// over the shared cidfont_notdef_min.ttf program (object 7). Object 10 is an
// incomplete /CIDSet the descriptor carries when withCIDSet is set. mapA and
// mapB are the descendants' /CIDToGIDMap values, written inline; a map stream
// goes in objects 11 and 12. The descriptor's /FontName holds a byte that is
// not valid UTF-8, so name-utf8 queues the descriptor before the font repair
// reads it. Both fonts carry a valid /ToUnicode (object 13), so the repair
// allocates no CMap of its own.
func sharedDescriptorPDF(t *testing.T, withCIDSet bool, mapA, mapB []byte) []byte {
	t.Helper()

	ttf, err := os.ReadFile(notdefTTFPath)
	if err != nil {
		t.Fatalf("reading the font fixture: %v", err)
	}
	cidSet := ""
	if withCIDSet {
		cidSet = " /CIDSet 10 0 R"
	}
	cidMap := func(obj int, m []byte) string {
		if m == nil {
			return "/Identity"
		}
		return fmt.Sprintf("%d 0 R", obj)
	}
	stream := func(data []byte) string {
		return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(data), data)
	}
	cidFont := func(obj int, m []byte) string {
		return "<< /Type /Font /Subtype /CIDFontType2 /BaseFont /ABCDEF+X /CIDToGIDMap " + cidMap(obj, m) +
			" /FontDescriptor 6 0 R /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> >>"
	}

	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
			" /Resources << /Font << /F1 4 0 R /F2 8 0 R >> >> >>",
		"<< /Type /Font /Subtype /Type0 /BaseFont /ABCDEF+X /Encoding /Identity-H /DescendantFonts [5 0 R] /ToUnicode 13 0 R >>",
		cidFont(11, mapA),
		"<< /Type /FontDescriptor /FontName /ABCDEF+X#80 /Flags 4 /FontFile2 7 0 R" + cidSet + " >>",
		stream(ttf),
		"<< /Type /Font /Subtype /Type0 /BaseFont /ABCDEF+X /Encoding /Identity-H /DescendantFonts [9 0 R] /ToUnicode 13 0 R >>",
		cidFont(12, mapB),
		stream([]byte{0x40}),
		stream(mapA),
		stream(mapB),
		stream([]byte("/CIDInit /ProcSet findresource begin 12 dict begin begincmap " +
			"1 begincodespacerange <0000> <FFFF> endcodespacerange " +
			"1 beginbfchar <0001> <0041> endbfchar endcmap end end")),
	})
}

// newStreamsIn counts the streams allocated past the document's own objects.
func newStreamsIn(alloc *writer.ObjAllocator, highest int) []int {
	var nums []int
	for num, obj := range alloc.Pending() {
		if _, ok := obj.(ast.PdfStream); ok && num > highest {
			nums = append(nums, num)
		}
	}
	return nums
}

// TestFixFontIssues_SettlesASharedDescriptorsCIDSetOnce pins that two Type 0
// fonts naming one descriptor get one /CIDSet between them, built on the
// descriptor name-utf8 repaired.
//
// The repair ran per font and compared the set it built with the /CIDSet the
// document held. The first font queued a set and pointed the descriptor at it;
// the second read that descriptor through the allocator, could not find the
// queued set in the document, and wrote a duplicate over it, leaving the first
// stream in the output with nothing referring to it.
func TestFixFontIssues_SettlesASharedDescriptorsCIDSetOnce(t *testing.T) {
	t.Parallel()

	const fdObj = 6
	ctx := context.Background()
	doc, alloc := newTestDoc(t, sharedDescriptorPDF(t, true, nil, nil))
	highest := doc.HighestObjectNumber(ctx)

	if _, err := fixNameUTF8(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixNameUTF8: %v", err)
	}
	if got := pendingDict(t, alloc, fdObj)["FontName"]; got != ast.PdfName("ABCDEF+X_") {
		t.Fatalf("control: name-utf8 queued /FontName %v, want /ABCDEF+X_, so there is no earlier edit to lose", got)
	}

	fixes, err := fixFontIssues(ctx, alloc, doc, Options{},
		[]compliance.Violation{{Rule: compliance.Rule_19005_2_6_2_11_4_2_t02}}, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixFontIssues: %v", err)
	}

	created := newStreamsIn(alloc, highest)
	if len(created) != 1 {
		t.Fatalf("the repair allocated %d streams %v, want one /CIDSet for the shared descriptor; fixes=%v",
			len(created), created, fixes)
	}
	fd := pendingDict(t, alloc, fdObj)
	if ref, _ := fd["CIDSet"].(ast.PdfIndirectRef); ref.ObjectNum != created[0] {
		t.Errorf("the descriptor's /CIDSet is %v, want the one stream allocated, %d 0 R", fd["CIDSet"], created[0])
	}
	if got := fd["FontName"]; got != ast.PdfName("ABCDEF+X_") {
		t.Errorf("/FontName is %v, want /ABCDEF+X_: the /CIDSet repair rebuilt the descriptor from the document "+
			"and discarded name-utf8's write", got)
	}

	// A second run starts with a fresh record of settled descriptors, so only
	// the comparison with the queued set keeps it from writing again.
	again, err := fixFontIssues(ctx, alloc, doc, Options{},
		[]compliance.Violation{{Rule: compliance.Rule_19005_2_6_2_11_4_2_t02}}, "PDF/A-2b")
	if err != nil {
		t.Fatalf("second fixFontIssues: %v", err)
	}
	if n := len(newStreamsIn(alloc, highest)); n != 1 {
		t.Errorf("a second run left %d allocated streams, want the one /CIDSet: it compared against the "+
			"document's set rather than the queued one; fixes=%v", n, again)
	}
}

// TestFixFontIssues_ACIDSetCoversEverySharersMap pins that a /CIDSet supplied
// for a descriptor two CIDFonts share lists the CIDs each font's
// /CIDToGIDMap stream selects.
//
// The set was built from the program's glyph IDs, as if every CID were its
// GID. Under a map stream a CID selects the GID the map names, so ISO 19005-1,
// 6.3.5 t3 still found each font's rendered CIDs missing from the set it had
// just been given. Font A's map reaches CID 40 and font B's CID 60, both past
// the program's 38 glyphs, and B maps CIDs 1 to 40 to a glyph the program
// lacks, so only the union holds both fonts' CIDs.
//
// # Corpus evidence
//
//   - pdfjs issue20232.pdf: two TrueType CIDFonts with their own /CIDToGIDMap
//     streams name one descriptor, and the set built from glyph IDs left the
//     rendered CIDs 1042 and 8470 out.
func TestFixFontIssues_ACIDSetCoversEverySharersMap(t *testing.T) {
	t.Parallel()

	gidMap := func(entries map[int]int, n int) []byte {
		out := make([]byte, 2*n)
		for cid, gid := range entries {
			out[2*cid], out[2*cid+1] = byte(gid>>8), byte(gid)
		}
		return out
	}
	mapA := gidMap(map[int]int{40: 1}, 41)
	absent := map[int]int{60: 2}
	for cid := 1; cid <= 40; cid++ {
		absent[cid] = 999
	}
	mapB := gidMap(absent, 61)

	ctx := context.Background()
	doc, alloc := newTestDoc(t, sharedDescriptorPDF(t, false, mapA, mapB))
	highest := doc.HighestObjectNumber(ctx)

	fixes, err := fixFontIssues(ctx, alloc, doc, Options{},
		[]compliance.Violation{{Rule: compliance.Rule_19005_1_6_3_5_t03}}, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixFontIssues: %v", err)
	}
	created := newStreamsIn(alloc, highest)
	if len(created) != 1 {
		t.Fatalf("the repair allocated %d streams, want one /CIDSet; fixes=%v", len(created), fixes)
	}
	data := pendingStreamBytes(t, alloc, created[0])
	has := func(cid int) bool {
		return cid/8 < len(data) && data[cid/8]&(1<<(7-cid%8)) != 0
	}
	for _, cid := range []int{1, 40, 41, 60} {
		if !has(cid) {
			t.Errorf("CID %d is not in the /CIDSet, which a sharer's map selects a glyph of the program for", cid)
		}
	}
	if has(61) {
		t.Errorf("CID 61 is in the /CIDSet, which no sharer's map reaches")
	}
}
