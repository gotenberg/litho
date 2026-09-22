package writer

import (
	"bytes"
	"compress/zlib"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// substantialDicts allocates n dictionaries big enough that packing them pays
// for the object stream's own overhead.
//
// The never-worse guard declines a batch whose packed form would cost more
// than writing those objects directly, so a fixture of two or three short
// dictionaries exercises the GUARD rather than the packing. Several tests here
// were written before that guard existed: two started failing and two kept
// passing for the wrong reason, asserting that nothing was packed in a
// situation where nothing would have been packed regardless.
func substantialDicts(alloc *ObjAllocator, n int) []ast.PdfIndirectRef {
	refs := make([]ast.PdfIndirectRef, 0, n)
	for i := range n {
		refs = append(refs, alloc.Alloc(ast.PdfDict{
			"Type":  ast.PdfName("Annot"),
			"Index": ast.PdfInt(i),
			"Note":  ast.PdfString(strings.Repeat("padding for a realistic dictionary body ", 6)),
		}))
	}
	return refs
}

// packedEntry is a decoded cross-reference entry including field 3, which
// carries the index within an object stream for a type-2 entry. The decoder in
// xref_writer_test.go keeps only the type and field 2, so it cannot tell a
// correctly placed packed object from one pointing at the wrong slot.
type packedEntry struct {
	typ    int64
	field2 int64
	field3 int64
}

// decodePackedXRef inflates a serialized cross-reference stream and returns its
// entries keyed by object number.
func decodePackedXRef(t *testing.T, out string) map[int]packedEntry {
	t.Helper()
	// The LAST stream, not the first: a packed file writes its object
	// streams before the cross-reference stream, so Index would inflate an
	// ObjStm payload and parse it as xref entries. "endstream" is followed
	// by a bare newline, so it cannot produce a false "stream\r\n" match.
	start := strings.LastIndex(out, "stream\r\n")
	if start < 0 {
		t.Fatal("no stream marker")
	}
	start += len("stream\r\n")
	end := strings.Index(out[start:], "\r\nendstream")
	if end < 0 {
		t.Fatal("no endstream marker")
	}
	zr, err := zlib.NewReader(strings.NewReader(out[start : start+end]))
	if err != nil {
		t.Fatalf("zlib reader: %v", err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("inflate: %v", err)
	}

	w1, w2 := parseWidths(t, out)
	idx := parseIndex(t, out)
	entrySize := 1 + w1 + w2
	if len(raw)%entrySize != 0 {
		t.Fatalf("decoded length %d is not a multiple of entry size %d", len(raw), entrySize)
	}
	readBE := func(b []byte) int64 {
		var v int64
		for _, c := range b {
			v = v<<8 | int64(c)
		}
		return v
	}

	entries := make(map[int]packedEntry)
	pos := 0
	for i := 0; i+1 < len(idx); i += 2 {
		objNum, count := idx[i], idx[i+1]
		for c := range count {
			b := raw[pos : pos+entrySize]
			entries[objNum+c] = packedEntry{
				typ:    readBE(b[:1]),
				field2: readBE(b[1 : 1+w1]),
				field3: readBE(b[1+w1:]),
			}
			pos += entrySize
		}
	}
	return entries
}

// TestPackObjectStreams_PacksDictsAndEmptiesPending checks the core contract:
// a packed object leaves the pending set, because anything still pending is
// written directly and would then exist twice in the output.
func TestPackObjectStreams_PacksDictsAndEmptiesPending(t *testing.T) {
	t.Parallel()
	alloc := NewObjAllocator(0)
	refs := substantialDicts(alloc, 12)

	placed, err := PackObjectStreams(context.Background(), alloc)
	if err != nil {
		t.Fatalf("PackObjectStreams: %v", err)
	}
	if len(placed) != len(refs) {
		t.Fatalf("packed %d objects, want %d", len(placed), len(refs))
	}

	pending := alloc.Pending()
	for _, ref := range refs {
		if _, still := pending[ref.ObjectNum]; still {
			t.Errorf("object %d is packed but still pending; it would be written twice", ref.ObjectNum)
		}
	}
	if len(pending) != 1 {
		t.Fatalf("pending holds %d objects, want just the object stream", len(pending))
	}
	for num, obj := range pending {
		stream, ok := obj.(ast.PdfStream)
		if !ok {
			t.Fatalf("pending object %d is %T, want an object stream", num, obj)
		}
		if got := stream.Dict["Type"]; got != ast.PdfName("ObjStm") {
			t.Errorf("/Type = %v, want ObjStm", got)
		}
		if got := stream.Dict["N"]; got != ast.PdfInt(len(refs)) {
			t.Errorf("/N = %v, want %d", got, len(refs))
		}
		if _, ok := stream.Dict["First"]; !ok {
			t.Error("object stream has no /First; a reader cannot locate the bodies")
		}
	}

	// Indices are assigned in ascending object-number order, which is what
	// makes the output deterministic.
	for i, p := range placed {
		if p.ObjStmIndex != i {
			t.Errorf("placement %d has index %d, want %d", i, p.ObjStmIndex, i)
		}
	}
}

// TestPackObjectStreams_LeavesStreamsUnpacked pins the spec rule that a stream
// cannot nest inside an object stream (ISO 32000-1, 7.5.7). Packing one would
// produce a file no reader can resolve.
//
// The dictionaries alongside it must actually be packed, or this passes
// because nothing was packed at all.
func TestPackObjectStreams_LeavesStreamsUnpacked(t *testing.T) {
	t.Parallel()
	alloc := NewObjAllocator(0)
	substantialDicts(alloc, 12)
	streamRef := alloc.Alloc(ast.NewStream(ast.PdfDict{"Length": ast.PdfInt(2)}, []byte("hi")))

	placed, err := PackObjectStreams(context.Background(), alloc)
	if err != nil {
		t.Fatalf("PackObjectStreams: %v", err)
	}
	if len(placed) == 0 {
		t.Fatal("nothing was packed, so this proves nothing about streams")
	}
	for _, p := range placed {
		if p.ObjNum == streamRef.ObjectNum {
			t.Fatalf("stream object %d was packed", streamRef.ObjectNum)
		}
	}
	if _, still := alloc.Pending()[streamRef.ObjectNum]; !still {
		t.Error("the stream left the pending set, so it would not be written at all")
	}
}

// TestPackObjectStreams_RefusesFewerThanTwo: one object costs a stream dict
// and a Flate header to save a single "N 0 obj"/"endobj" pair, so packing it
// makes the file larger.
func TestPackObjectStreams_RefusesFewerThanTwo(t *testing.T) {
	t.Parallel()
	alloc := NewObjAllocator(0)
	substantialDicts(alloc, 1)

	placed, err := PackObjectStreams(context.Background(), alloc)
	if err != nil {
		t.Fatalf("PackObjectStreams: %v", err)
	}
	if len(placed) != 0 {
		t.Errorf("packed %d objects from a single candidate", len(placed))
	}
}

// TestPackObjectStreams_DeclinesWhenItWouldNotShrink covers the never-worse
// guard. Measured on the corpus, two roughly 1 KB files of five short objects
// each came out 42 bytes LARGER packed, because the stream dictionary and
// Flate header cost more than the object wrappers they replace. Growth is the
// regression this packing exists to remove.
func TestPackObjectStreams_DeclinesWhenItWouldNotShrink(t *testing.T) {
	t.Parallel()
	alloc := NewObjAllocator(0)
	a := alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("Catalog")})
	b := alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("Pages")})

	placed, err := PackObjectStreams(context.Background(), alloc)
	if err != nil {
		t.Fatalf("PackObjectStreams: %v", err)
	}
	if len(placed) != 0 {
		t.Errorf("packed %d tiny objects; the output would grow", len(placed))
	}
	// Declining must leave them written directly, not drop them.
	for _, ref := range []ast.PdfIndirectRef{a, b} {
		if _, still := alloc.Pending()[ref.ObjectNum]; !still {
			t.Errorf("object %d left the pending set after packing was declined", ref.ObjectNum)
		}
	}
}

// TestWriteRewrite_NeverWorseThanUnpacked states the guard's promise in the
// form a caller cares about: asking for packing on a document too small to
// benefit produces exactly the bytes it would have produced anyway.
func TestWriteRewrite_NeverWorseThanUnpacked(t *testing.T) {
	t.Parallel()
	build := func(pack bool) string {
		alloc := NewObjAllocator(0)
		catalog := alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("Catalog")})
		alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("Pages")})
		var buf bytes.Buffer
		if err := WriteRewrite(context.Background(), &buf, RewriteConfig{
			Allocator:         alloc,
			Trailer:           ast.PdfDict{"Root": catalog},
			PackObjectStreams: pack,
		}); err != nil {
			t.Fatalf("WriteRewrite: %v", err)
		}
		return buf.String()
	}
	if packed, plain := build(true), build(false); packed != plain {
		t.Errorf("packing a document it cannot help changed the output (%d vs %d bytes)",
			len(packed), len(plain))
	}
}

// TestWriteRewrite_PackedOutputCarriesType2Entries walks the whole path and
// decodes the result: the packed objects must be named by type-2 entries
// pointing at the object stream and the right index, and the object stream
// itself must keep a type-1 entry, since a reader reaches it by offset.
func TestWriteRewrite_PackedOutputCarriesType2Entries(t *testing.T) {
	t.Parallel()
	alloc := NewObjAllocator(0)
	catalog := alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("Catalog")})
	refs := substantialDicts(alloc, 12)

	var buf bytes.Buffer
	err := WriteRewrite(context.Background(), &buf, RewriteConfig{
		Allocator:         alloc,
		Trailer:           ast.PdfDict{"Root": catalog},
		PackObjectStreams: true,
	})
	if err != nil {
		t.Fatalf("WriteRewrite: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "/Type /ObjStm") {
		t.Fatalf("output carries no object stream:\n%s", out[:min(len(out), 400)])
	}
	// Packing implies a cross-reference stream: a traditional table has no
	// type field and cannot name a compressed object.
	if !strings.Contains(out, "/Type /XRef") {
		t.Error("packed output did not switch to a cross-reference stream")
	}
	if strings.Contains(out, "\nxref\n") {
		t.Error("packed output still wrote a traditional xref table")
	}

	entries := decodePackedXRef(t, out)
	seen := make(map[int64]bool)
	for _, ref := range refs {
		e, ok := entries[ref.ObjectNum]
		if !ok {
			t.Fatalf("no xref entry for packed object %d", ref.ObjectNum)
		}
		if e.typ != 2 {
			t.Errorf("object %d entry type = %d, want 2 (compressed)", ref.ObjectNum, e.typ)
		}
		stm, ok := entries[int(e.field2)]
		if !ok {
			t.Fatalf("object %d names container %d, which has no xref entry", ref.ObjectNum, e.field2)
		}
		if stm.typ != 1 {
			t.Errorf("container %d entry type = %d, want 1; it is reached by offset", e.field2, stm.typ)
		}
		if seen[e.field3] {
			t.Errorf("index %d in the object stream is claimed twice", e.field3)
		}
		seen[e.field3] = true
	}
}

// TestWriteRewrite_RefusesToPackWhenEncrypted is the security-shaped case.
// Encryption runs over the pending objects before serialization, so packing
// afterwards would place ciphertext strings inside a stream that is encrypted
// again, and ISO 32000-1, 7.5.7 forbids encrypting strings in an object
// stream separately. The file must come out unpacked rather than unreadable.
//
// The control matters as much as the assertion: the same fixture without
// /Encrypt must pack, or this test would pass on a document that was never
// going to be packed anyway.
func TestWriteRewrite_RefusesToPackWhenEncrypted(t *testing.T) {
	t.Parallel()
	build := func(encrypted bool) string {
		alloc := NewObjAllocator(0)
		catalog := alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("Catalog")})
		substantialDicts(alloc, 12)
		trailer := ast.PdfDict{"Root": catalog}
		if encrypted {
			trailer["Encrypt"] = alloc.Alloc(ast.PdfDict{"Filter": ast.PdfName("Standard")})
		}
		var buf bytes.Buffer
		if err := WriteRewrite(context.Background(), &buf, RewriteConfig{
			Allocator:         alloc,
			Trailer:           trailer,
			PackObjectStreams: true,
		}); err != nil {
			t.Fatalf("WriteRewrite: %v", err)
		}
		return buf.String()
	}

	if out := build(true); strings.Contains(out, "/Type /ObjStm") {
		t.Error("an encrypted document was packed; its strings would be encrypted twice")
	} else if !strings.Contains(out, "/Type /Catalog") {
		t.Error("the catalog is missing from the unpacked output")
	}
	if out := build(false); !strings.Contains(out, "/Type /ObjStm") {
		t.Error("the same fixture does not pack without /Encrypt, so the refusal proves nothing")
	}
}

// TestWriteRewrite_PackingIsDeterministic guards the reproducible-output rule:
// packing iterates a map, so it must sort before grouping or two runs of the
// same input differ.
func TestWriteRewrite_PackingIsDeterministic(t *testing.T) {
	t.Parallel()
	build := func() string {
		alloc := NewObjAllocator(0)
		catalog := alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("Catalog")})
		substantialDicts(alloc, 40)
		var buf bytes.Buffer
		if err := WriteRewrite(context.Background(), &buf, RewriteConfig{
			Allocator:         alloc,
			Trailer:           ast.PdfDict{"Root": catalog},
			PackObjectStreams: true,
		}); err != nil {
			t.Fatalf("WriteRewrite: %v", err)
		}
		return buf.String()
	}
	if a, b := build(), build(); a != b {
		t.Errorf("packing is non-deterministic (len a=%d b=%d)", len(a), len(b))
	}
}
