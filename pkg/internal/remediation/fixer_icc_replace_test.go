package remediation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// pdfWithGreyIntents builds a PDF whose /OutputIntents array holds the given
// entries, each naming object 5, a grey ICC profile of the given major version
// and device class. Page 3 paints through an ICCBased space over the same
// object. Object 4 is a spare intent dict entries may reference.
func pdfWithGreyIntents(version byte, class, entries string) []byte {
	icc := make([]byte, 132)
	icc[8] = version
	copy(icc[12:16], class)
	copy(icc[16:20], "GRAY")
	copy(icc[36:40], "acsp")

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /OutputIntents [" + entries + "] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
			" /Resources << /ColorSpace << /CS0 [/ICCBased 5 0 R] >> >> >>",
		"<< /Type /OutputIntent /S /GTS_PDFX /OutputConditionIdentifier (grey) /DestOutputProfile 5 0 R >>",
	}
	offsets := make([]int, 6)
	for i, body := range objs {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	offsets[5] = buf.Len()
	fmt.Fprintf(&buf, "5 0 obj\n<< /N 1 /Length %d >>\nstream\n", len(icc))
	buf.Write(icc)
	buf.WriteString("\nendstream\nendobj\n")
	xrefStart := buf.Len()
	buf.WriteString("xref\n0 6\n0000000000 65535 f\r\n")
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefStart)
	return buf.Bytes()
}

// replacedIntentProfile returns the pending replacement the output intent at
// object oiNum reads after [fixCorruptICC]: through the rewritten intent when
// the fixer repointed it, through the source profile's own object number when
// the fixer replaced the profile in place.
func replacedIntentProfile(ctx context.Context, t *testing.T, alloc *writer.ObjAllocator, doc *document.Document, oiNum int) ast.PdfStream {
	t.Helper()
	oi, ok := alloc.Pending()[oiNum].(ast.PdfDict)
	if !ok {
		var err error
		oi, err = document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: oiNum})
		if err != nil {
			t.Fatalf("resolving the output intent, object %d: %v", oiNum, err)
		}
	}
	ref, ok := oi["DestOutputProfile"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("/DestOutputProfile is %T, want an indirect reference", oi["DestOutputProfile"])
	}
	stream, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
	if !ok {
		t.Fatalf("the intent reads object %d, which is %T, want a pending replacement stream",
			ref.ObjectNum, alloc.Pending()[ref.ObjectNum])
	}
	return stream
}

// pendingStreams returns the object numbers of the streams the allocator will
// write.
func pendingStreams(pending map[int]ast.PdfObject) []int {
	var nums []int
	for num, obj := range pending {
		if _, ok := obj.(ast.PdfStream); ok {
			nums = append(nums, num)
		}
	}
	return nums
}

// assertGreyReplacement checks that ref names the one pending replacement
// stream, litho's grey profile with /N 1.
func assertGreyReplacement(ctx context.Context, t *testing.T, pending map[int]ast.PdfObject, ref ast.PdfObject) {
	t.Helper()
	dpRef, ok := ref.(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("/DestOutputProfile is %T, want an indirect reference", ref)
	}
	stream, ok := pending[dpRef.ObjectNum].(ast.PdfStream)
	if !ok {
		t.Fatalf("/DestOutputProfile names object %d, which is %T, want the pending replacement stream",
			dpRef.ObjectNum, pending[dpRef.ObjectNum])
	}
	data, err := stream.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading the replacement profile: %v", err)
	}
	if !bytes.Equal(data, sGrayICCProfile) {
		t.Error("the replacement is not litho's grey profile")
	}
	if n, _ := stream.Dict["N"].(ast.PdfInt); n != 1 {
		t.Errorf("the replacement declares /N %v, want 1", stream.Dict["N"])
	}
}

// TestFixCorruptICC_ReplacesDirectIntentProfile pins the replacement of a
// profile whose output intent sits directly in /OutputIntents. The profile is
// a scanner profile, a device class ISO 19005-2, 6.2.3 t1 rejects in an output
// intent and 6.2.4.2 admits in an ICCBased space. The intent has
// no object number of its own, so the fixer used to overwrite the profile
// stream at its object number, after allocating the same stream a second time
// under a number nothing referenced: every output carried an orphan copy of
// the 2.7 KB grey profile, and the ICCBased space sharing the profile, where
// ISO 19005-2, 6.2.4.2 admits an input profile, lost its own colours to the
// replacement. The fixer now rewrites the intents array in the catalog and
// leaves the source profile to the page.
func TestFixCorruptICC_ReplacesDirectIntentProfile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, pdfWithGreyIntents(2, "scnr",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (grey) /DestOutputProfile 5 0 R >>"))

	fixes, err := fixCorruptICC(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixCorruptICC: %v", err)
	}
	if len(fixes) != 1 {
		t.Fatalf("fixes = %v, want the one replacement", fixes)
	}

	pending := alloc.Pending()
	if _, ok := pending[5]; ok {
		t.Error("the source profile, object 5, was overwritten: the page's ICCBased space lost its own profile")
	}
	if streams := pendingStreams(pending); len(streams) != 1 {
		t.Errorf("pending streams %v, want exactly the one replacement profile", streams)
	}
	catalog := pendingDict(t, alloc, 1)
	intents, ok := catalog["OutputIntents"].(ast.PdfArray)
	if !ok || len(intents) != 1 {
		t.Fatalf("catalog /OutputIntents is %v, want the rewritten one-entry array", catalog["OutputIntents"])
	}
	intent, ok := intents[0].(ast.PdfDict)
	if !ok {
		t.Fatalf("the intent is %T, want it kept as a direct dictionary", intents[0])
	}
	if s, _ := intent["S"].(ast.PdfName); s != "GTS_PDFA1" {
		t.Errorf("the intent /S is %v, want GTS_PDFA1 kept", intent["S"])
	}
	assertGreyReplacement(ctx, t, pending, intent["DestOutputProfile"])
}

// TestFixCorruptICC_SharedProfileReplacedOnce pins that two output intents
// sharing one rejected profile come back sharing one replacement. ISO 19005-2,
// 6.2.3 t2 asks every intent's /DestOutputProfile to be the same indirect
// object, and the fixer used to allocate a replacement per intent, so the
// intents it repaired broke that test, and a later fixer unifying them left
// the first copy orphaned in the output.
func TestFixCorruptICC_SharedProfileReplacedOnce(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, pdfWithGreyIntents(2, "scnr",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (grey) /DestOutputProfile 5 0 R >> 4 0 R"))

	fixes, err := fixCorruptICC(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixCorruptICC: %v", err)
	}
	if len(fixes) != 2 {
		t.Fatalf("fixes = %v, want one per intent", fixes)
	}

	pending := alloc.Pending()
	if _, ok := pending[5]; ok {
		t.Error("the source profile, object 5, was overwritten: the page's ICCBased space lost its own profile")
	}
	if streams := pendingStreams(pending); len(streams) != 1 {
		t.Errorf("pending streams %v, want one replacement the two intents share", streams)
	}
	catalog := pendingDict(t, alloc, 1)
	intents, ok := catalog["OutputIntents"].(ast.PdfArray)
	if !ok || len(intents) != 2 {
		t.Fatalf("catalog /OutputIntents is %v, want the rewritten two-entry array", catalog["OutputIntents"])
	}
	direct, ok := intents[0].(ast.PdfDict)
	if !ok {
		t.Fatalf("the first intent is %T, want it kept as a direct dictionary", intents[0])
	}
	if ref, ok := intents[1].(ast.PdfIndirectRef); !ok || ref.ObjectNum != 4 {
		t.Errorf("the second intent is %v, want it kept as 4 0 R", intents[1])
	}
	indirect := pendingDict(t, alloc, 4)
	assertGreyReplacement(ctx, t, pending, direct["DestOutputProfile"])
	if direct["DestOutputProfile"] != indirect["DestOutputProfile"] {
		t.Errorf("the intents name %v and %v, want one shared replacement (ISO 19005-2, 6.2.3 t2)",
			direct["DestOutputProfile"], indirect["DestOutputProfile"])
	}
}

// TestFixCorruptICC_ReplacesUnadmittedVersionInPlace pins that a profile whose
// version no part admits is replaced at its own object number. The page's
// ICCBased space shares it and fails ISO 19005-2, 6.2.4.2 t1 for the same
// version, so the stock profile repairs the space along with the intents, the
// choice the version 2 re-encoding makes too, and the intents stay as they
// are.
func TestFixCorruptICC_ReplacesUnadmittedVersionInPlace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, pdfWithGreyIntents(5, "mntr",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (grey) /DestOutputProfile 5 0 R >> 4 0 R"))

	fixes, err := fixCorruptICC(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixCorruptICC: %v", err)
	}
	if len(fixes) != 2 {
		t.Fatalf("fixes = %v, want one per intent", fixes)
	}

	pending := alloc.Pending()
	if streams := pendingStreams(pending); len(streams) != 1 {
		t.Errorf("pending streams %v, want the one replacement at object 5", streams)
	}
	for _, num := range []int{1, 4} {
		if _, ok := pending[num]; ok {
			t.Errorf("object %d was rewritten, want the intents kept as they are", num)
		}
	}
	assertGreyReplacement(ctx, t, pending, ast.PdfIndirectRef{ObjectNum: 5})
}
