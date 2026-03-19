package document

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"fmt"
	"testing"
)

// buildHybridPDF constructs a minimal hybrid-reference PDF where:
//   - Object 1: Catalog
//   - Object 2: Pages
//   - Object 3 (old): a stale indirect object at a known offset
//   - Object 3 (new): the correct indirect object at a different offset
//   - A legacy plain-text xref table lists the stale offset for object 3
//   - A compressed XRef stream (object 4) lists the correct offset for object 3
//   - The trailer's /XRefStm key points to the compressed stream
//
// After hybrid parsing, object 3 must resolve to the new offset.
func buildHybridPDF() ([]byte, int64, int64) {
	var buf bytes.Buffer

	// Header.
	buf.WriteString("%PDF-1.5\n")

	// Object 1: Catalog.
	off1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	// Object 2: Pages.
	off2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")

	// Object 3 (old / stale version).
	off3old := buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Font /BaseFont /OldFont >>\nendobj\n")

	// Object 3 (new / correct version).
	off3new := buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Font /BaseFont /NewFont >>\nendobj\n")

	// Object 4: Compressed XRef stream.
	// Build the stream data: entries for objects 0–4.
	// W = [1 3 1]: type=1byte, offset/objstm=3bytes, gen=1byte.
	entrySize := 5 // 1 + 3 + 1
	numObjs := 5   // objects 0–4
	streamData := make([]byte, numObjs*entrySize)

	writeEntry := func(idx, typ int, field2 int64, field3 int) {
		base := idx * entrySize
		streamData[base] = byte(typ)
		// field2 as 3-byte big-endian.
		streamData[base+1] = byte(field2 >> 16)
		streamData[base+2] = byte(field2 >> 8)
		streamData[base+3] = byte(field2)
		streamData[base+4] = byte(field3)
	}

	// Obj 0: free.
	writeEntry(0, 0, 0, 255)
	// Obj 1: normal at off1.
	writeEntry(1, 1, int64(off1), 0)
	// Obj 2: normal at off2.
	writeEntry(2, 1, int64(off2), 0)
	// Obj 3: normal at off3new (the CORRECT offset).
	writeEntry(3, 1, int64(off3new), 0)
	// Obj 4: this stream itself (will be filled in after we know the offset).
	// We'll use a placeholder and fix it after.

	off4 := buf.Len()
	// Fix object 4's entry to point to itself.
	writeEntry(4, 1, int64(off4), 0)

	// Compress the stream data.
	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	_, _ = w.Write(streamData)
	_ = w.Close()
	compressedBytes := compressed.Bytes()

	// Write object 4 (the XRef stream).
	fmt.Fprintf(&buf, "4 0 obj\n<< /Type /XRef /Size 5 /W [1 3 1] /Filter /FlateDecode /Length %d >>\nstream\n", len(compressedBytes))
	buf.Write(compressedBytes)
	buf.WriteString("\nendstream\nendobj\n")

	// Legacy plain-text xref table.
	// Lists the STALE offset for object 3.
	xrefOff := buf.Len()
	buf.WriteString("xref\n")
	buf.WriteString("0 5\n")
	buf.WriteString("0000000000 65535 f \n")
	fmt.Fprintf(&buf, "%010d 00000 n \n", off1)
	fmt.Fprintf(&buf, "%010d 00000 n \n", off2)
	fmt.Fprintf(&buf, "%010d 00000 n \n", off3old) // STALE offset
	fmt.Fprintf(&buf, "%010d 00000 n \n", off4)

	// Trailer with /XRefStm pointing to the compressed stream.
	fmt.Fprintf(&buf, "trailer\n<< /Size 5 /Root 1 0 R /XRefStm %d >>\n", off4)
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF\n", xrefOff)

	return buf.Bytes(), int64(off3old), int64(off3new)
}

func TestHybridXRef_CompressedOverridesLegacy(t *testing.T) {
	pdfData, off3old, off3new := buildHybridPDF()

	doc, err := NewDocument(context.Background(), bytes.NewReader(pdfData), int64(len(pdfData)), nil)
	if err != nil {
		t.Fatalf("NewDocument failed: %v", err)
	}
	if doc.Rebuilt {
		t.Fatal("expected Rebuilt=false for hybrid PDF")
	}

	entry, ok := doc.Index.Entries[3]
	if !ok {
		t.Fatal("object 3 not found in xref")
	}
	if entry.Type != 1 {
		t.Fatalf("expected Type=1, got %d", entry.Type)
	}
	if entry.Offset == off3old {
		t.Errorf("object 3 still points to stale offset %d; hybrid /XRefStm override failed", off3old)
	}
	if entry.Offset != off3new {
		t.Errorf("object 3 offset = %d, want %d (new)", entry.Offset, off3new)
	}
}

func TestHybridXRef_NonOverriddenEntriesPreserved(t *testing.T) {
	pdfData, _, _ := buildHybridPDF()

	doc, err := NewDocument(context.Background(), bytes.NewReader(pdfData), int64(len(pdfData)), nil)
	if err != nil {
		t.Fatalf("NewDocument failed: %v", err)
	}

	// Objects 1 and 2 should be present and valid.
	for _, objNum := range []int{1, 2} {
		entry, ok := doc.Index.Entries[objNum]
		if !ok {
			t.Errorf("object %d not found", objNum)
			continue
		}
		if entry.Type != 1 {
			t.Errorf("object %d: expected Type=1, got %d", objNum, entry.Type)
		}
	}
}

func TestHybridXRef_NoXRefStm_LegacyOnly(t *testing.T) {
	// A normal non-hybrid PDF should work unchanged.
	data := minimalPDF()
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument failed: %v", err)
	}
	if doc.Rebuilt {
		t.Error("expected Rebuilt=false")
	}
	if len(doc.Index.Entries) == 0 {
		t.Error("expected non-empty xref entries")
	}
}

// buildHybridPDF_Type2 constructs a hybrid PDF where the compressed XRef
// stream contains Type 2 (object stream) entries that are absent from the
// legacy table.
func buildHybridPDF_Type2() []byte {
	var buf bytes.Buffer

	buf.WriteString("%PDF-1.5\n")

	off1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	off2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")

	// Object 5 is an object stream containing object 3.
	off5 := buf.Len()
	// Object stream: /N 1 /First 4 means 1 object, data starts at byte 4.
	// Header: "3 0 " (objNum=3, offset=0 within data section)
	// Data: "<< /Type /Font /BaseFont /StreamFont >>"
	objStreamContent := "3 0 << /Type /Font /BaseFont /StreamFont >>"
	var compObjStm bytes.Buffer
	w := zlib.NewWriter(&compObjStm)
	_, _ = w.Write([]byte(objStreamContent))
	_ = w.Close()
	compObjStmBytes := compObjStm.Bytes()
	buf.WriteString(fmt.Sprintf("5 0 obj\n<< /Type /ObjStm /N 1 /First 4 /Filter /FlateDecode /Length %d >>\nstream\n", len(compObjStmBytes)))
	buf.Write(compObjStmBytes)
	buf.WriteString("\nendstream\nendobj\n")

	// Compressed XRef stream (object 4).
	// W = [1 3 1].
	entrySize := 5
	numObjs := 6 // 0–5
	streamData := make([]byte, numObjs*entrySize)

	writeEntry := func(idx, typ int, f2 int64, f3 int) {
		base := idx * entrySize
		streamData[base] = byte(typ)
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, uint32(f2))
		copy(streamData[base+1:base+4], b[1:4])
		streamData[base+4] = byte(f3)
	}

	writeEntry(0, 0, 0, 255)
	writeEntry(1, 1, int64(off1), 0)
	writeEntry(2, 1, int64(off2), 0)
	writeEntry(3, 2, 5, 0) // Type 2: in object stream 5, index 0
	off4 := buf.Len()
	writeEntry(4, 1, int64(off4), 0)
	writeEntry(5, 1, int64(off5), 0)

	var compXRef bytes.Buffer
	w2 := zlib.NewWriter(&compXRef)
	_, _ = w2.Write(streamData)
	_ = w2.Close()
	compXRefBytes := compXRef.Bytes()

	buf.WriteString(fmt.Sprintf("4 0 obj\n<< /Type /XRef /Size 6 /W [1 3 1] /Filter /FlateDecode /Length %d >>\nstream\n", len(compXRefBytes)))
	buf.Write(compXRefBytes)
	buf.WriteString("\nendstream\nendobj\n")

	// Legacy xref table (does NOT include object 3 — only 0,1,2,4,5).
	xrefOff := buf.Len()
	buf.WriteString("xref\n")
	buf.WriteString("0 3\n")
	buf.WriteString("0000000000 65535 f \n")
	buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off1))
	buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off2))
	buf.WriteString("4 2\n")
	buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off4))
	buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off5))

	buf.WriteString(fmt.Sprintf("trailer\n<< /Size 6 /Root 1 0 R /XRefStm %d >>\n", off4))
	buf.WriteString(fmt.Sprintf("startxref\n%d\n%%%%EOF\n", xrefOff))

	return buf.Bytes()
}

func TestHybridXRef_Type2_CompressedObjectFromStream(t *testing.T) {
	pdfData := buildHybridPDF_Type2()

	doc, err := NewDocument(context.Background(), bytes.NewReader(pdfData), int64(len(pdfData)), nil)
	if err != nil {
		t.Fatalf("NewDocument failed: %v", err)
	}

	// Object 3 should be Type 2 (from the XRefStm), not absent.
	entry, ok := doc.Index.Entries[3]
	if !ok {
		t.Fatal("object 3 not found; hybrid /XRefStm should have added it")
	}
	if entry.Type != 2 {
		t.Errorf("object 3: expected Type=2 (compressed in ObjStm), got %d", entry.Type)
	}
	if entry.ObjStmNum != 5 {
		t.Errorf("object 3: expected ObjStmNum=5, got %d", entry.ObjStmNum)
	}
}
