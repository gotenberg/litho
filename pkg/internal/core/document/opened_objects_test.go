package document

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// unlistedObjectDoc opens a file whose body carries object 4, which its
// cross-reference table leaves out although the trailer's /Size covers it.
func unlistedObjectDoc(t *testing.T) *Document {
	t.Helper()
	objects := []string{
		"1 0 obj\n<< /Type /Catalog /Pages 3 0 R /Probe 4 0 R >>\nendobj\n",
		"2 0 obj\n<< /Listed true >>\nendobj\n",
		"3 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n",
		"4 0 obj\n<< /Unlisted true >>\nendobj\n",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, obj := range objects {
		offsets[i+1] = buf.Len()
		buf.WriteString(obj)
	}
	xrefAt := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 4\n0000000000 65535 f\r\n%010d 00000 n\r\n%010d 00000 n\r\n%010d 00000 n\r\n", offsets[1], offsets[2], offsets[3])
	fmt.Fprintf(&buf, "trailer\n<< /Size 5 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefAt)
	data := buf.Bytes()
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if doc.Rebuilt {
		t.Fatal("the test file's cross-reference table did not parse")
	}
	return doc
}

// TestOpenedObjectsOnly pins that a lookup under [OpenedObjectsOnly] reads a
// reference to an object the cross-reference table leaves out as a reference
// to an undefined object (ISO 32000-1, 7.3.10), as veraPDF does, whether or
// not a lookup without it found and cached the object first, and that its
// miss runs no raw-byte augmentation. A lookup without it still reaches the
// object through the augmentation.
func TestOpenedObjectsOnly(t *testing.T) {
	t.Parallel()
	opened := OpenedObjectsOnly(context.Background())
	unlisted := ast.PdfIndirectRef{ObjectNum: 4}

	t.Run("fresh document", func(t *testing.T) {
		t.Parallel()
		doc := unlistedObjectDoc(t)
		if _, err := doc.Resolve(opened, unlisted); !errors.Is(err, ErrObjectNotFound) {
			t.Fatalf("Resolve(4) under OpenedObjectsOnly: err = %v, want ErrObjectNotFound", err)
		}
		if doc.XRefAugmented() {
			t.Error("a miss under OpenedObjectsOnly ran the raw-byte augmentation")
		}
		if _, err := doc.Resolve(opened, ast.PdfIndirectRef{ObjectNum: 2}); err != nil {
			t.Errorf("Resolve(2) under OpenedObjectsOnly: %v, want the listed object", err)
		}
	})

	t.Run("an earlier lookup cached the object", func(t *testing.T) {
		t.Parallel()
		doc := unlistedObjectDoc(t)
		// Control: without the context the augmentation finds object 4, and
		// the lookup caches it.
		if _, err := doc.Resolve(context.Background(), unlisted); err != nil {
			t.Fatalf("Resolve(4) without OpenedObjectsOnly: %v, want the augmentation to find it", err)
		}
		if _, ok := doc.XRefEntry(4); !ok {
			t.Fatal("the augmentation did not add object 4 to the index")
		}
		if _, err := doc.Resolve(opened, unlisted); !errors.Is(err, ErrObjectNotFound) {
			t.Errorf("Resolve(4) under OpenedObjectsOnly after a cached lookup: err = %v, want ErrObjectNotFound", err)
		}
	})
}

// TestOpenedObjectsOnly_Concurrent pins that lookups with and without
// [OpenedObjectsOnly] on one Document answer the same whatever order they run
// in, the lookups without it running the augmentation while the others read
// the index. Run it under -race.
func TestOpenedObjectsOnly_Concurrent(t *testing.T) {
	t.Parallel()
	doc := unlistedObjectDoc(t)
	opened := OpenedObjectsOnly(context.Background())
	unlisted := ast.PdfIndirectRef{ObjectNum: 4}

	const goroutines = 16
	errs := make(chan error, goroutines)
	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Go(func() {
			if i%2 == 0 {
				if _, err := doc.Resolve(context.Background(), unlisted); err != nil {
					errs <- fmt.Errorf("without OpenedObjectsOnly: %w", err)
				}
				return
			}
			if _, err := doc.Resolve(opened, unlisted); !errors.Is(err, ErrObjectNotFound) {
				errs <- fmt.Errorf("under OpenedObjectsOnly: err = %v, want ErrObjectNotFound", err)
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// unlistedObjStmPDF builds an xref-stream file whose object 3 is a member of
// object stream 8, which the cross-reference stream's /Index leaves out.
func unlistedObjStmPDF(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.5\n")
	off1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R /Probe 3 0 R >>\nendobj\n")
	off2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")

	const header, body = "3 0 ", "<< /Member true >>\n"
	var packed bytes.Buffer
	zw := zlib.NewWriter(&packed)
	if _, err := zw.Write([]byte(header + body)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&buf, "8 0 obj\n<< /Type /ObjStm /N 1 /First %d /Filter /FlateDecode /Length %d >>\nstream\n", len(header), packed.Len())
	buf.Write(packed.Bytes())
	buf.WriteString("\nendstream\nendobj\n")

	// /Index [0 8 9 1] lists objects 0 to 7 and 9, the stream itself, and
	// leaves 8 out.
	const entry = 4 // /W [1 2 1]
	listed := []int{0, 1, 2, 3, 4, 5, 6, 7, 9}
	table := make([]byte, len(listed)*entry)
	off9 := buf.Len()
	for i, num := range listed {
		row := table[i*entry : (i+1)*entry]
		switch num {
		case 1:
			row[0] = 1
			binary.BigEndian.PutUint16(row[1:], uint16(off1)) //nolint:gosec // test offsets fit in 16 bits.
		case 2:
			row[0] = 1
			binary.BigEndian.PutUint16(row[1:], uint16(off2)) //nolint:gosec // test offsets fit in 16 bits.
		case 3:
			row[0] = 2
			binary.BigEndian.PutUint16(row[1:], 8)
		case 9:
			row[0] = 1
			binary.BigEndian.PutUint16(row[1:], uint16(off9)) //nolint:gosec // test offsets fit in 16 bits.
		}
	}
	fmt.Fprintf(&buf, "9 0 obj\n<< /Type /XRef /Size 10 /Index [0 8 9 1] /Root 1 0 R /W [1 2 1] /Length %d >>\nstream\n", len(table))
	buf.Write(table)
	buf.WriteString("\nendstream\nendobj\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF\n", off9)
	return buf.Bytes()
}

// TestOpenedObjectsOnly_ObjectStreamTheTableLeavesOut pins that a member of an
// object stream the table leaves out reads as undefined under
// [OpenedObjectsOnly], the member's own entry notwithstanding: its bytes come
// from the stream. A lookup without the context reads the member through the
// augmentation, and the object and the decoded stream it caches do not stand
// for the member under the context afterwards.
func TestOpenedObjectsOnly_ObjectStreamTheTableLeavesOut(t *testing.T) {
	t.Parallel()
	data := unlistedObjStmPDF(t)
	opened := OpenedObjectsOnly(context.Background())
	member := ast.PdfIndirectRef{ObjectNum: 3}

	for _, earlier := range []bool{false, true} {
		t.Run(fmt.Sprintf("earlier lookup %v", earlier), func(t *testing.T) {
			t.Parallel()
			doc := openObjStmDoc(t, data)
			if earlier {
				// Control: the member must resolve without the context, or
				// the lookup below proves nothing about the cache.
				obj, err := doc.Resolve(context.Background(), member)
				if err != nil {
					t.Fatalf("Resolve(3) without OpenedObjectsOnly: %v", err)
				}
				if dict, ok := obj.(ast.PdfDict); !ok || dict["Member"] != ast.PdfBool(true) {
					t.Fatalf("Resolve(3) = %v, want the member", obj)
				}
			}
			if _, err := doc.Resolve(opened, member); !errors.Is(err, ErrObjectNotFound) {
				t.Errorf("Resolve(3) under OpenedObjectsOnly: err = %v, want ErrObjectNotFound", err)
			}
			doc.SetComplianceMode(true)
			doc.CheckObjectFormat(opened, 3)
			doc.mu.RLock()
			_, recorded := doc.formatViolations[3]
			doc.mu.RUnlock()
			if recorded {
				t.Error("CheckObjectFormat under OpenedObjectsOnly recorded a member of a stream the table leaves out")
			}
		})
	}
}
