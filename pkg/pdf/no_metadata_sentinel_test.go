package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
)

// bareDocumentPDF builds a document carrying no metadata of any kind.
//
// semantics.Metadata returns its sentinel only when the map it assembles ends
// up empty, and it fills that map from three places: the trailer's /Info
// dictionary, an XMP packet on the catalog, and the catalog's /Lang (ISO
// 32000-2, 14.9.2). The fixture therefore omits all three. A /Lang left in by
// accident would populate "Language", the map would be non-empty, and the test
// would fail for a reason that has nothing to do with the sentinel.
func bareDocumentPDF() []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, xref)
	return buf.Bytes()
}

// TestMetadata_BareDocumentReturnsTheSentinel pins a bridge that nothing in
// this package asserted.
//
// [ErrNoMetadata] is not declared here: it is a RE-EXPORT of
// semantics.ErrNoMetadata, and [Reader.Metadata] is a pass-through that returns
// that error unwrapped. So the alias is the whole contract, and it can be
// severed - repointed at a fresh errors.New, or the error wrapped on its way
// out - without any internal test noticing, because semantics keeps asserting
// its own value and stays green.
//
// It is not decorative either: cmd/litho's metadata command branches on
// errors.Is(err, pdf.ErrNoMetadata) to tell "this document has none" from a
// real extraction failure, and no corpus gate reaches cmd/litho.
//
// Both directions are asserted deliberately. Checking only that a bare
// document errors would pass on any error at all; checking only that a
// metadata-bearing document succeeds would pass if the sentinel vanished.
func TestMetadata_BareDocumentReturnsTheSentinel(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := bareDocumentPDF()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	meta, err := r.Metadata(ctx)
	if err == nil {
		t.Fatalf("Metadata succeeded with %d entries, want the sentinel: the fixture "+
			"carries metadata after all, so it does not exercise the empty-map path", len(meta))
	}
	if !errors.Is(err, ErrNoMetadata) {
		t.Errorf("err = %v, want errors.Is(err, ErrNoMetadata): the re-export of "+
			"semantics.ErrNoMetadata is the only thing that makes this matchable", err)
	}

	// The other direction: a document that does carry metadata must not
	// report the sentinel, or the assertion above would pass even if
	// Metadata had started failing for everything.
	withInfo := threePagePDF()
	r2, err := Open(ctx, bytes.NewReader(withInfo), int64(len(withInfo)))
	if err != nil {
		t.Fatalf("Open (with /Info): %v", err)
	}
	defer r2.Close()

	meta2, err := r2.Metadata(ctx)
	if err != nil {
		t.Fatalf("Metadata on a document carrying /Info: %v, want it to succeed", err)
	}
	if len(meta2) == 0 {
		t.Error("Metadata returned an empty map with no error on a document carrying /Info")
	}
}
