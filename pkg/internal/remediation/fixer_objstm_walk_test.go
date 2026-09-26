package remediation

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
	"github.com/gotenberg/litho/pkg/internal/mutations"
)

// objStmDoc parses [objStmCIDFontPDF] around body, which the file stores as
// object 5 inside object stream 4, and fails unless the reader reports that
// entry as compressed: a rebuilt table would list it as type 1 and every
// assertion would pass without exercising the object-stream path.
func objStmDoc(t *testing.T, body string) ([]byte, *document.Document, *writer.ObjAllocator) {
	t.Helper()
	data := objStmCIDFontPDF(body)
	doc, alloc := newTestDoc(t, data)
	if got, _ := doc.XRefEntry(5); got.Type != xref.XRefTypeCompressed {
		t.Fatalf("fixture: object 5 has xref type %d, want %d (in an object stream)",
			got.Type, xref.XRefTypeCompressed)
	}
	return data, doc, alloc
}

// TestFixNameUTF8_RepairsAnObjectInsideAnObjectStream pins that the object
// walk reaches an object stored in an object stream (ISO 32000-1, 7.5.7). Its
// entry is type 2, which the walk skipped, so a name ISO 19005-2, 6.1.8 fails
// there was never repaired while validation judged it.
func TestFixNameUTF8_RepairsAnObjectInsideAnObjectStream(t *testing.T) {
	t.Parallel()

	_, doc, alloc := objStmDoc(t, "<< /Type /Probe /Tag /B#FFad >>")
	fixes, err := fixNameUTF8(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixNameUTF8: %v", err)
	}
	if len(fixes) != 1 {
		t.Fatalf("the name inside the object stream must be repaired, got %v", fixes)
	}
	if tag := pendingDict(t, alloc, 5)["Tag"]; tag != ast.PdfName("B_ad") {
		t.Errorf("/Tag is %v, want /B_ad", tag)
	}
}

// TestFixNameUTF8_PinsResourceKeysInsideAnObjectStream pins that a resource
// key held by an object stored in an object stream stays pinned while the walk
// repairs the other names of that object. The key scan and the walk must see
// the same objects: a scan that skipped the compressed object left its /Font
// key free for the walk to rename, and a content stream's Tf operand then
// named a font the page no longer has.
func TestFixNameUTF8_PinsResourceKeysInsideAnObjectStream(t *testing.T) {
	t.Parallel()

	_, doc, alloc := objStmDoc(t, "<< /Font << /F#FF1 3 0 R >> /Tag /B#FFad >>")
	if _, err := fixNameUTF8(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixNameUTF8: %v", err)
	}
	got := pendingDict(t, alloc, 5)
	if tag := got["Tag"]; tag != ast.PdfName("B_ad") {
		t.Errorf("/Tag is %v, want /B_ad", tag)
	}
	fonts, ok := got["Font"].(ast.PdfDict)
	if !ok {
		t.Fatalf("/Font is %T, want a dictionary", got["Font"])
	}
	if _, kept := fonts["F\xff1"]; !kept {
		t.Errorf("the /Font key was renamed, keys %v", fonts)
	}
}

// TestFixStringLimit_TruncatesAStringInsideAnObjectStream pins the string
// repair on an object stored in an object stream: ISO 19005-1, 6.1.12 t3 caps
// a string at 65535 bytes wherever the file stores it.
func TestFixStringLimit_TruncatesAStringInsideAnObjectStream(t *testing.T) {
	t.Parallel()

	_, doc, alloc := objStmDoc(t, "<< /Type /Probe /T ("+strings.Repeat("a", pdfA1StringLimit+10)+") >>")
	if _, err := fixStringLimit(context.Background(), alloc, doc, Options{}, nil, "PDF/A-1b"); err != nil {
		t.Fatalf("fixStringLimit: %v", err)
	}
	s, ok := pendingDict(t, alloc, 5)["T"].(ast.PdfString)
	if !ok || len(s) != pdfA1StringLimit {
		t.Errorf("/T holds %d bytes, want %d", len(s), pdfA1StringLimit)
	}
}

// TestObjectStreamRepair_ReachesTheWrittenFile pins that a repair queued at
// the number of an object stored in an object stream is the object both write
// paths emit. The incremental update lists it in a new cross-reference
// section, which supersedes the object stream's copy (ISO 32000-1, 7.5.8), and
// the full rewrite copies it in place of the source object.
func TestObjectStreamRepair_ReachesTheWrittenFile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	data, doc, alloc := objStmDoc(t, "<< /Type /Probe /Tag /B#FFad >>")
	// Reference object 5 from the catalog so the full rewrite copies it.
	alloc.AllocAt(1, ast.PdfDict{
		"Type":  ast.PdfName("Catalog"),
		"Pages": ast.PdfIndirectRef{ObjectNum: 2},
		"Probe": ast.PdfIndirectRef{ObjectNum: 5},
	})
	if _, err := fixNameUTF8(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixNameUTF8: %v", err)
	}

	startXRef, err := document.FindStartXRef(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("FindStartXRef: %v", err)
	}
	// The trailer the incremental update carries forward, as the writer builds
	// it from the source's.
	trailer := make(ast.PdfDict, len(doc.Index.Trailer))
	for k, v := range doc.Index.Trailer {
		if k != "Prev" && k != "Size" {
			trailer[k] = v
		}
	}
	var incremental bytes.Buffer
	if err := writer.WriteIncremental(ctx, &incremental, writer.IncrementalConfig{
		Source:          bytes.NewReader(data),
		SourceSize:      int64(len(data)),
		StartXRefOffset: startXRef,
		Allocator:       alloc,
		Trailer:         trailer,
		UseXRefStream:   true,
		Generation:      doc.ObjectGeneration,
	}); err != nil {
		t.Fatalf("WriteIncremental: %v", err)
	}
	var rewrite bytes.Buffer
	if err := mutations.FullRewrite(ctx, &rewrite, mutations.FullRewriteConfig{Doc: doc, Overrides: alloc.Pending()}); err != nil {
		t.Fatalf("FullRewrite: %v", err)
	}

	for name, out := range map[string][]byte{"incremental": incremental.Bytes(), "rewrite": rewrite.Bytes()} {
		reread, err := document.NewDocument(ctx, bytes.NewReader(out), int64(len(out)), nil)
		if err != nil {
			t.Fatalf("%s: reopening: %v", name, err)
		}
		catalog, err := document.ResolveCatalog(ctx, reread)
		if err != nil {
			t.Fatalf("%s: catalog: %v", name, err)
		}
		probe, err := document.ResolveDict(ctx, reread, catalog.Dict["Probe"])
		if err != nil {
			t.Fatalf("%s: resolving the repaired object: %v", name, err)
		}
		if tag := probe["Tag"]; tag != ast.PdfName("B_ad") {
			t.Errorf("%s: the written object has /Tag %v, want the repaired /B_ad", name, tag)
		}
	}
}
