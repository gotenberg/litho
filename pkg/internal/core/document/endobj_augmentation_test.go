package document

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// misplacedObject is a document whose table places object 2 inside object 1's
// body, with the offsets that matter.
type misplacedObject struct {
	doc   *Document
	off1  int64 // object 1, the table's offset and the real one
	stale int64 // where the table places object 2, inside object 1's body
	off2  int64 // where object 2 really is
}

// newMisplacedObject opens a document whose table places object 2 inside
// object 1's body. Every object the bytes hold is in the table, so the
// raw-byte augmentation moves object 2 without adding an entry. Opening it
// resolves only the catalog, object 1, whose header is where the table says,
// so nothing runs the augmentation before the test does. Object 1 ends with a
// space before endobj, which ISO 19005-2, 6.1.9 forbids.
func newMisplacedObject(t *testing.T) misplacedObject {
	t.Helper()
	obj1 := "1 0 obj\n<< /Type /Catalog /Pages 3 0 R /Pad (padding) >> endobj\n"
	obj2 := "2 0 obj\n<< /A 1 >>\nendobj\n"
	obj3 := "3 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n"

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	m := misplacedObject{off1: int64(buf.Len())}
	m.stale = m.off1 + int64(strings.Index(obj1, "/Pad"))
	buf.WriteString(obj1)
	m.off2 = int64(buf.Len())
	buf.WriteString(obj2)
	off3 := buf.Len()
	buf.WriteString(obj3)
	xrefAt := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 4\n0000000000 65535 f \r\n%010d 00000 n \r\n%010d 00000 n \r\n%010d 00000 n \r\n", m.off1, m.stale, off3)
	fmt.Fprintf(&buf, "trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefAt)

	data := buf.Bytes()
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if entry, _ := doc.XRefEntry(2); entry.Offset != m.stale {
		t.Fatalf("object 2 is at %d once open, want the table's %d: the augmentation ran during open", entry.Offset, m.stale)
	}
	m.doc = doc
	return m
}

// augment runs the augmentation on m and checks that it moved object 2 and
// nothing else.
func (m misplacedObject) augment(t *testing.T) {
	t.Helper()
	count := m.doc.XRefEntryCount()
	m.doc.AugmentXRef(context.Background())
	if !m.doc.XRefAugmented() {
		t.Fatal("the augmentation did not change the index")
	}
	if got := m.doc.XRefEntryCount(); got != count {
		t.Fatalf("the augmentation changed the entry count from %d to %d, the fixture must only move an offset", count, got)
	}
	if entry, _ := m.doc.XRefEntry(2); entry.Offset != m.off2 {
		t.Fatalf("object 2 is at %d after the augmentation, want %d", entry.Offset, m.off2)
	}
}

// objectOneFindings returns the messages of the header and endobj findings
// the check reports on object 1, which come from its endobj scan alone.
func objectOneFindings(t *testing.T, m misplacedObject) []string {
	t.Helper()
	return headerMessages(t, m.doc.ValidateObjectHeaderFormat(m.off1, 1))
}

// headerMessages returns the messages of the object header findings among
// violations.
func headerMessages(t *testing.T, violations []error) []string {
	t.Helper()
	var out []string
	for _, v := range violations {
		if errors.Is(v, ErrObjectHeaderFormat) {
			out = append(out, v.Error())
		}
	}
	return out
}

// wantObjectOne is the one finding object 1 carries.
const wantObjectOne = "compliance: obj 1: endobj not preceded by EOL marker (0x20)"

// TestValidateEndobjFormat_IndependentOfAugmentation pins that the endobj
// check reports the same findings on an object whether or not the raw-byte
// augmentation has run. The scan window followed the live index, so before the
// augmentation it ended where the table misplaces object 2, inside object 1's
// body and short of object 1's endobj, and object 1 passed the check it fails
// once the augmentation had moved object 2.
func TestValidateEndobjFormat_IndependentOfAugmentation(t *testing.T) {
	t.Parallel()

	t.Run("the check itself", func(t *testing.T) {
		t.Parallel()
		m := newMisplacedObject(t)
		before := objectOneFindings(t, m)
		if !slices.Equal(before, []string{wantObjectOne}) {
			t.Fatalf("before the augmentation: object 1 reports %q, want %q", before, wantObjectOne)
		}
		objectTwo := headerMessages(t, m.doc.ValidateObjectHeaderFormat(m.off2, 2))

		m.augment(t)
		if after := objectOneFindings(t, m); !slices.Equal(after, before) {
			t.Errorf("after the augmentation: object 1 reports %q, before it %q", after, before)
		}
		if after := headerMessages(t, m.doc.ValidateObjectHeaderFormat(m.off2, 2)); !slices.Equal(after, objectTwo) {
			t.Errorf("after the augmentation: object 2 reports %q, before it %q", after, objectTwo)
		}
	})

	// Resolution records the findings of an object's first parse, so the
	// record is the check's result at whichever moment the object was first
	// resolved.
	t.Run("the record resolution keeps", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		record := func(augmentFirst bool) []string {
			m := newMisplacedObject(t)
			// Opening resolved the catalog outside compliance mode, and a
			// cached object is not parsed again.
			m.doc.ClearCaches()
			m.doc.SetComplianceMode(true)
			if augmentFirst {
				m.augment(t)
			}
			if _, err := m.doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: 1}); err != nil {
				t.Fatalf("resolving object 1: %v", err)
			}
			if !augmentFirst {
				m.augment(t)
			}
			return headerMessages(t, m.doc.ObjectFormatViolations(1))
		}
		first, later := record(false), record(true)
		if !slices.Equal(first, []string{wantObjectOne}) {
			t.Errorf("object 1 resolved before the augmentation records %q, want %q", first, wantObjectOne)
		}
		if !slices.Equal(later, first) {
			t.Errorf("object 1 resolved after the augmentation records %q, before it %q", later, first)
		}
	})
}

// TestValidateEndobjFormat_ConcurrentAugmentation races the endobj check
// against the augmentation: goroutines resolving object 2 run it, since the
// table's offset misses the object's header, while others judge object 1.
// Every judgement must be the same whichever side of the augmentation it
// lands on, and under -race the window must be read without a data race.
func TestValidateEndobjFormat_ConcurrentAugmentation(t *testing.T) {
	t.Parallel()
	m := newMisplacedObject(t)
	d := m.doc
	ctx := context.Background()
	d.SetComplianceMode(true)

	const goroutines = 8
	const iterations = 50
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Go(func() {
			for range iterations {
				if g%2 == 0 {
					_, _ = d.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: 2}) // Only the augmentation it runs matters here.
					continue
				}
				if got := headerMessages(t, d.ValidateObjectHeaderFormat(m.off1, 1)); !slices.Equal(got, []string{wantObjectOne}) {
					t.Errorf("object 1 reports %q, want %q", got, wantObjectOne)
				}
			}
		})
	}
	wg.Wait()

	if entry, _ := d.XRefEntry(2); entry.Offset != m.off2 {
		t.Errorf("object 2 is at %d once every goroutine returned, want %d: no resolution ran the augmentation", entry.Offset, m.off2)
	}
}
