package writer

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// streamPayload is shared by the duplicate fixtures so they serialize
// identically, which is the only thing dedup keys on.
var streamPayload = []byte("shared image bytes, long enough to be worth merging")

func dupStream() ast.PdfStream {
	return ast.NewStream(ast.PdfDict{
		"Type":    ast.PdfName("XObject"),
		"Subtype": ast.PdfName("Image"),
		"Width":   ast.PdfInt(8),
	}, streamPayload)
}

// TestDedupStreams_MergesIdenticalAndRepointsReferences is the core contract:
// the duplicate leaves the pending set and everything that named it now names
// the survivor. A merge that dropped the object without repointing would leave
// references to an object the file no longer contains.
func TestDedupStreams_MergesIdenticalAndRepointsReferences(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	alloc := NewObjAllocator(0)

	first := alloc.Alloc(dupStream())
	second := alloc.Alloc(dupStream())
	other := alloc.Alloc(ast.NewStream(ast.PdfDict{"Type": ast.PdfName("XObject")}, []byte("different")))
	holder := alloc.Alloc(ast.PdfDict{
		"A":      first,
		"B":      second,
		"C":      other,
		"Nested": ast.PdfArray{second, ast.PdfInt(1)},
	})

	removed, err := DedupStreams(ctx, alloc, ast.PdfDict{})
	if err != nil {
		t.Fatalf("DedupStreams: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed %d objects, want 1", removed)
	}

	pending := alloc.Pending()
	if _, still := pending[second.ObjectNum]; still {
		t.Errorf("duplicate object %d is still pending", second.ObjectNum)
	}
	// The survivor is the LOWEST number, so the choice cannot depend on map
	// iteration order.
	if _, ok := pending[first.ObjectNum]; !ok {
		t.Errorf("survivor %d was removed instead", first.ObjectNum)
	}
	if _, ok := pending[other.ObjectNum]; !ok {
		t.Error("a stream that is not a duplicate was removed")
	}

	dict, ok := pending[holder.ObjectNum].(ast.PdfDict)
	if !ok {
		t.Fatalf("holder is %T", pending[holder.ObjectNum])
	}
	if got := dict["B"]; got != first {
		t.Errorf("dict /B = %v, want the survivor %v", got, first)
	}
	if got := dict["A"]; got != first {
		t.Errorf("dict /A = %v, want %v (unchanged)", got, first)
	}
	if got := dict["C"]; got != other {
		t.Errorf("dict /C = %v, want the untouched stream %v", got, other)
	}
	arr, ok := dict["Nested"].(ast.PdfArray)
	if !ok || len(arr) != 2 {
		t.Fatalf("nested array is %v", dict["Nested"])
	}
	if arr[0] != first {
		t.Errorf("nested array ref = %v, want the survivor %v", arr[0], first)
	}
}

// TestDedupStreams_RepointsInsideAStreamDict covers the case that makes this
// more than an optimization. A stream dictionary can name another stream, an
// /SMask or an ICC-based /ColorSpace among them, so failing to rewrite those
// leaves a dangling reference to an object dedup just deleted.
//
// It also proves the survivor and the rebuilt holder still yield their bytes:
// PdfStream hides its payload behind an unexported lazy loader, and rebuilding
// one with a new dictionary must carry that loader across.
func TestDedupStreams_RepointsInsideAStreamDict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	alloc := NewObjAllocator(0)

	first := alloc.Alloc(dupStream())
	second := alloc.Alloc(dupStream())
	holderData := []byte("holder body")
	holder := alloc.Alloc(ast.NewStream(ast.PdfDict{
		"Type":  ast.PdfName("XObject"),
		"SMask": second,
	}, holderData))

	if _, err := DedupStreams(ctx, alloc, ast.PdfDict{}); err != nil {
		t.Fatalf("DedupStreams: %v", err)
	}

	stream, ok := alloc.Pending()[holder.ObjectNum].(ast.PdfStream)
	if !ok {
		t.Fatalf("holder is %T, want a stream", alloc.Pending()[holder.ObjectNum])
	}
	if got := stream.Dict["SMask"]; got != first {
		t.Errorf("/SMask = %v, want the survivor %v; the deleted object is dangling", got, first)
	}
	got, err := stream.Bytes(ctx)
	if err != nil {
		t.Fatalf("rebuilt holder lost its payload: %v", err)
	}
	if !bytes.Equal(got, holderData) {
		t.Errorf("rebuilt holder bytes = %q, want %q", got, holderData)
	}

	survivor, ok := alloc.Pending()[first.ObjectNum].(ast.PdfStream)
	if !ok {
		t.Fatalf("survivor is %T", alloc.Pending()[first.ObjectNum])
	}
	sb, err := survivor.Bytes(ctx)
	if err != nil {
		t.Fatalf("survivor lost its payload: %v", err)
	}
	if !bytes.Equal(sb, streamPayload) {
		t.Errorf("survivor bytes = %q, want %q", sb, streamPayload)
	}
}

// TestDedupStreams_RepointsTheTrailer: the trailer names /Root and /Info
// directly, so leaving it alone would point the document at a deleted object.
func TestDedupStreams_RepointsTheTrailer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	alloc := NewObjAllocator(0)

	first := alloc.Alloc(dupStream())
	second := alloc.Alloc(dupStream())
	trailer := ast.PdfDict{"Metadata": second}

	if _, err := DedupStreams(ctx, alloc, trailer); err != nil {
		t.Fatalf("DedupStreams: %v", err)
	}
	if got := trailer["Metadata"]; got != first {
		t.Errorf("trailer /Metadata = %v, want the survivor %v", got, first)
	}
}

// TestDedupStreams_LeavesIdenticalDictsAlone pins the deliberate scope limit.
// Structure elements, form fields and annotations are resolved by object
// number, so merging two identical dictionaries changes what the document
// means rather than how it is stored.
func TestDedupStreams_LeavesIdenticalDictsAlone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	alloc := NewObjAllocator(0)

	a := alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("StructElem"), "S": ast.PdfName("P")})
	b := alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("StructElem"), "S": ast.PdfName("P")})

	removed, err := DedupStreams(ctx, alloc, ast.PdfDict{})
	if err != nil {
		t.Fatalf("DedupStreams: %v", err)
	}
	if removed != 0 {
		t.Errorf("removed %d identical dictionaries; object identity is meaningful for them", removed)
	}
	for _, ref := range []ast.PdfIndirectRef{a, b} {
		if _, ok := alloc.Pending()[ref.ObjectNum]; !ok {
			t.Errorf("dictionary %d was merged away", ref.ObjectNum)
		}
	}
}

// TestWriteRewrite_RefusesToDedupWhenEncrypted mirrors the packing refusal.
// Encryption derives a per-object key from the object number, so identical
// plaintext streams already differ as ciphertext by the time the writer sees
// them. The control matters: the same fixture without /Encrypt must dedup, or
// the refusal proves nothing.
func TestWriteRewrite_RefusesToDedupWhenEncrypted(t *testing.T) {
	t.Parallel()
	build := func(encrypted bool) (string, *ObjAllocator) {
		alloc := NewObjAllocator(0)
		catalog := alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("Catalog")})
		alloc.Alloc(dupStream())
		alloc.Alloc(dupStream())
		trailer := ast.PdfDict{"Root": catalog}
		if encrypted {
			trailer["Encrypt"] = alloc.Alloc(ast.PdfDict{"Filter": ast.PdfName("Standard")})
		}
		var buf bytes.Buffer
		if err := WriteRewrite(context.Background(), &buf, RewriteConfig{
			Allocator:    alloc,
			Trailer:      trailer,
			DedupStreams: true,
		}); err != nil {
			t.Fatalf("WriteRewrite: %v", err)
		}
		return buf.String(), alloc
	}

	encOut, _ := build(true)
	if n := strings.Count(encOut, "shared image bytes"); n != 2 {
		t.Errorf("encrypted document kept %d copies of the payload, want 2 (dedup refused)", n)
	}
	plainOut, _ := build(false)
	if n := strings.Count(plainOut, "shared image bytes"); n != 1 {
		t.Errorf("unencrypted document kept %d copies, want 1; the refusal above proves nothing", n)
	}
}
