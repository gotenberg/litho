package writer

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// buildHybridSource returns a minimal hybrid-reference PDF (ISO 32000-1
// §7.5.8.4) together with the offset of its cross-reference table, the offset
// of the catalog, and the offset of its cross-reference stream. The plain table
// and the cross-reference stream both describe the same single revision, as a
// conforming producer emits them.
func buildHybridSource() (data []byte, xrefOffset, catalogOffset, xrefStmOffset int64) {
	var buf bytes.Buffer

	buf.WriteString("%PDF-1.5\n")

	off1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	off2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")

	off3 := buf.Len()

	// Cross-reference stream entries for objects 0-3, /W [1 3 1].
	const entrySize = 5
	streamData := make([]byte, 4*entrySize)
	writeEntry := func(idx, typ int, field2 int64, field3 int) {
		base := idx * entrySize
		streamData[base] = byte(typ)
		streamData[base+1] = byte(field2 >> 16)
		streamData[base+2] = byte(field2 >> 8)
		streamData[base+3] = byte(field2)
		streamData[base+4] = byte(field3)
	}
	writeEntry(0, 0, 0, 255)
	writeEntry(1, 1, int64(off1), 0)
	writeEntry(2, 1, int64(off2), 0)
	writeEntry(3, 1, int64(off3), 0)

	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	_, _ = zw.Write(streamData)
	_ = zw.Close()

	fmt.Fprintf(&buf, "3 0 obj\n<< /Type /XRef /Size 4 /W [1 3 1] /Root 1 0 R /Filter /FlateDecode /Length %d >>\nstream\n", compressed.Len())
	buf.Write(compressed.Bytes())
	buf.WriteString("\nendstream\nendobj\n")

	xrefOff := buf.Len()
	buf.WriteString("xref\n0 4\n")
	buf.WriteString("0000000000 65535 f \n")
	fmt.Fprintf(&buf, "%010d 00000 n \n", off1)
	fmt.Fprintf(&buf, "%010d 00000 n \n", off2)
	fmt.Fprintf(&buf, "%010d 00000 n \n", off3)
	fmt.Fprintf(&buf, "trailer\n<< /Size 4 /Root 1 0 R /XRefStm %d >>\n", off3)
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF\n", xrefOff)

	return buf.Bytes(), int64(xrefOff), int64(off1), int64(off3)
}

// appendHybridUpdate appends an incremental update that rewrites the catalog,
// mirroring how the public writer seeds IncrementalConfig: it drops the keys
// the incremental engine manages (/Prev, /Size) and forwards the rest of the
// source trailer verbatim.
func appendHybridUpdate(t *testing.T, src []byte, xrefOffset, xrefStmOffset int64) []byte {
	t.Helper()

	alloc := NewObjAllocator(3)
	alloc.AllocAt(1, ast.PdfDict{
		"Type":        ast.PdfName("Catalog"),
		"Pages":       ast.PdfIndirectRef{ObjectNum: 2},
		"LithoMarker": ast.PdfBool(true),
	})

	var out bytes.Buffer
	err := WriteIncremental(context.Background(), &out, IncrementalConfig{
		Source:          bytes.NewReader(src),
		SourceSize:      int64(len(src)),
		StartXRefOffset: xrefOffset,
		Allocator:       alloc,
		Trailer: ast.PdfDict{
			"Root":    ast.PdfIndirectRef{ObjectNum: 1},
			"XRefStm": ast.PdfInt(xrefStmOffset),
		},
	})
	if err != nil {
		t.Fatalf("WriteIncremental failed: %v", err)
	}

	return out.Bytes()
}

// TestWriteIncremental_DropsSourceXRefStm guards against carrying a
// hybrid-reference /XRefStm into the appended cross-reference section. Per ISO
// 32000-1 §7.5.8.4 the key is section-local, so a copied value points at an
// older revision's cross-reference stream and resurrects that revision's
// entries for objects the update just rewrote.
func TestWriteIncremental_DropsSourceXRefStm(t *testing.T) {
	src, xrefOffset, _, xrefStmOffset := buildHybridSource()
	out := appendHybridUpdate(t, src, xrefOffset, xrefStmOffset)

	appended := out[len(src):]
	if bytes.Contains(appended, []byte("/XRefStm")) {
		t.Error("appended trailer carries /XRefStm; it must be dropped (ISO 32000-1 §7.5.8.4)")
	}
	if !bytes.Contains(appended, []byte("/Prev")) {
		t.Error("appended trailer is missing /Prev")
	}
}

// TestWriteIncremental_HybridSourceUpdateWins reads the appended file back and
// asserts the update's catalog wins. A stale /XRefStm in the new trailer makes
// the reader resolve the pre-update catalog instead, which is what made litho
// report its own remediation output as non-compliant.
func TestWriteIncremental_HybridSourceUpdateWins(t *testing.T) {
	src, xrefOffset, catalogOffset, xrefStmOffset := buildHybridSource()
	out := appendHybridUpdate(t, src, xrefOffset, xrefStmOffset)

	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(out), int64(len(out)), nil)
	if err != nil {
		t.Fatalf("NewDocument failed: %v", err)
	}
	if doc.Rebuilt {
		t.Fatal("expected Rebuilt=false; the appended file must parse through its xref chain")
	}

	entry, ok := doc.XRefEntry(1)
	if !ok {
		t.Fatal("catalog object 1 missing from the xref index")
	}
	if entry.Offset == catalogOffset {
		t.Fatalf("catalog resolves to the pre-update offset %d; the stale /XRefStm overrode the update", catalogOffset)
	}

	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog failed: %v", err)
	}
	if _, ok := catalog.Dict["LithoMarker"]; !ok {
		t.Error("catalog is the pre-update revision: /LithoMarker missing")
	}
}
