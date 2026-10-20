package pdf

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
	"github.com/gotenberg/litho/pkg/internal/mutations"
)

// objStmRedactBody draws the redacted word and a word that stays.
const objStmRedactBody = "BT /F1 12 Tf 100 700 Td (SECRET) Tj ET BT /F1 12 Tf 100 600 Td (KEEP) Tj ET"

// packedPrivateCopyPDF returns a one-page document whose catalog names a
// private dictionary holding note, rewritten so that dictionary is a member of
// an object stream rather than a top-level object.
//
// Nothing in the redaction scrubs a private catalog key, so the scan over the
// output is the only thing between a copy there and a leak. Packing it is the
// point: a member's bytes sit inside a compressed stream, where no byte grep of
// the file sees them.
func packedPrivateCopyPDF(t *testing.T, note string) []byte {
	t.Helper()
	ctx := context.Background()

	src := plantedPDF(objStmRedactBody, plant{
		catalog: "/LithoPrivate 7 0 R",
		objects: "7 0 obj\n<< /Note (" + note + ") >>\nendobj\n",
	})
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	var packed bytes.Buffer
	if err := NewWriter(r).WriteTo(ctx, &packed, WithFullRewrite(), WithObjectStreams()); err != nil {
		t.Fatalf("packing the source: %v", err)
	}
	return packed.Bytes()
}

// privateDictMember returns the object number of the catalog's /LithoPrivate
// dictionary in doc, failing the test unless the cross-reference index stores
// it compressed inside an object stream (ISO 32000-1, 7.5.7 and Table 18, type
// 2 entries). A fixture whose copy is a top-level object proves nothing about
// object streams.
func privateDictMember(t *testing.T, doc *document.Document) int {
	t.Helper()
	ctx := context.Background()

	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	ref, ok := catalog.Dict["LithoPrivate"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("the catalog's /LithoPrivate is %T, want a reference", catalog.Dict["LithoPrivate"])
	}
	entry, ok := doc.XRefEntry(ref.ObjectNum)
	if !ok || entry.Type != xref.XRefTypeCompressed {
		t.Fatalf("object %d is not an object-stream member (entry %+v, found %v), so this fixture tests nothing",
			ref.ObjectNum, entry, ok)
	}
	return ref.ObjectNum
}

// TestWriter_Redact_RefusesOverACopyInsideAnObjectStream pins that the output
// scan reads object-stream members. The removed word's only other copy sits in
// a dictionary packed into an /ObjStm, where a byte grep of the source cannot
// see it. A scan that skipped members would read the document as clean and
// write the copy out.
func TestWriter_Redact_RefusesOverACopyInsideAnObjectStream(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	src := packedPrivateCopyPDF(t, "SECRET")
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	privateDictMember(t, r.doc)

	w := NewWriter(r)
	_, err = w.Redact(ctx, []RedactRegion{coversSecret})
	if err == nil {
		var out bytes.Buffer
		err = w.WriteTo(ctx, &out)
	}
	if !errors.Is(err, ErrRedactionIncomplete) {
		t.Fatalf("err = %v, want ErrRedactionIncomplete over the packed copy", err)
	}
}

// TestWriter_Redact_PackedSourceWithoutACopySucceeds is the control for the
// refusal above: the same packed document with an innocent note redacts, so
// the refusal comes from the packed copy and not from packing itself.
func TestWriter_Redact_PackedSourceWithoutACopySucceeds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	src := packedPrivateCopyPDF(t, "KEEPSAKE")
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	privateDictMember(t, r.doc)

	w := NewWriter(r)
	if _, err := w.Redact(ctx, []RedactRegion{coversSecret}); err != nil {
		t.Fatalf("Redact: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
}

// TestScanOutputForTokens_ReadsMembersOfACompactOutput scans a redacted file
// written with object streams, as `litho redact --compact` writes it. The
// surviving private note is packed into an /ObjStm in that output, so its bytes
// are compressed and a byte grep finds nothing: only a scan that reads members
// sees it.
func TestScanOutputForTokens_ReadsMembersOfACompactOutput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	src := packedPrivateCopyPDF(t, "KEEPSAKE")
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	if _, err := w.Redact(ctx, []RedactRegion{coversSecret}); err != nil {
		t.Fatalf("Redact: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out, WithObjectStreams()); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if bytes.Contains(out.Bytes(), []byte("KEEPSAKE")) {
		t.Fatal("the note is readable in the output bytes, so it is not packed and this test proves nothing")
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening the compact output: %v", err)
	}
	defer r2.Close()
	member := privateDictMember(t, r2.doc)

	scan, err := mutations.ScanOutputForTokens(ctx, nil, r2.doc, []string{"KEEPSAKE", "SECRET"})
	if err != nil {
		t.Fatalf("ScanOutputForTokens: %v", err)
	}
	want := mutations.TokenHit{ObjectNum: member, Token: "KEEPSAKE", Where: "string"}
	found := false
	for _, hit := range scan.Hits {
		if hit == want {
			found = true
		}
		if hit.Token == "SECRET" {
			t.Errorf("the redacted word survives in the compact output: %+v", hit)
		}
	}
	if !found {
		t.Errorf("the scan missed the note packed in object %d: %+v", member, scan.Hits)
	}
	if len(scan.Unreadable) != 0 {
		t.Errorf("Unreadable = %v, want none", scan.Unreadable)
	}
}
