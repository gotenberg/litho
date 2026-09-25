package document

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

// misplacedObject is a document whose table places object 2 inside object 1's
// body, with the offsets that matter.
type misplacedObject struct {
	doc   *Document
	off1  int64 // object 1, the table's offset and the real one
	stale int64 // where the table places object 2, inside object 1's body
	off2  int64 // where object 2 really is
	off3  int64 // object 3, the table's offset and the real one
}

// newMisplacedObject builds the index by hand, so nothing resolves an object
// and runs the augmentation before the test does. Every object the bytes hold
// is in the table, so the augmentation moves object 2 without adding an
// entry, which leaves the entry count unchanged. Object 1 ends with a space
// before endobj, which ISO 19005-2, 6.1.9 forbids.
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
	m.off3 = int64(buf.Len())
	buf.WriteString(obj3)
	data := buf.Bytes()

	m.doc = &Document{
		r:    bytes.NewReader(data),
		size: int64(len(data)),
		xrefEntries: map[int]xref.XRefEntry{
			0: {Type: xref.XRefTypeFree, Free: true, Generation: 65535},
			1: {Type: xref.XRefTypeNormal, Offset: m.off1},
			2: {Type: xref.XRefTypeNormal, Offset: m.stale},
			3: {Type: xref.XRefTypeNormal, Offset: m.off3},
		},
		Index: xref.Index{
			Trailer: ast.PdfDict{"Size": ast.PdfInt(4), "Root": ast.PdfIndirectRef{ObjectNum: 1}},
		},
		objectCache: make(map[int]ast.PdfObject),
		fontCache:   make(map[int]any),
		cmapCache:   make(map[int]any),
		Logger:      slog.New(slog.DiscardHandler),
	}
	return m
}

// endobjViolations counts the format violations the header check reports on
// object 1, which come from its endobj scan alone.
func endobjViolations(t *testing.T, m misplacedObject) int {
	t.Helper()
	violations := m.doc.ValidateObjectHeaderFormat(m.off1, 1)
	for _, v := range violations {
		if !errors.Is(v, ErrObjectHeaderFormat) {
			t.Errorf("violation %v does not wrap ErrObjectHeaderFormat", v)
		}
	}
	return len(violations)
}

// TestNextNormalObjectOffset_AugmentationOverride pins the offset cache's
// rebuild when the raw-byte augmentation moves an entry without adding one.
// The cache was keyed on the entry count, so it kept the offset the table
// declared: object 1's endobj scan stopped inside its own body, where the table
// had put object 2, and never reached the endobj the check exists to judge.
func TestNextNormalObjectOffset_AugmentationOverride(t *testing.T) {
	m := newMisplacedObject(t)
	d := m.doc

	// The table's view builds the cache before the augmentation runs.
	if got := d.nextNormalObjectOffset(m.off1); got != m.stale {
		t.Fatalf("before the augmentation: nextNormalObjectOffset(%d) = %d, want the declared %d", m.off1, got, m.stale)
	}
	if n := endobjViolations(t, m); n != 0 {
		t.Fatalf("before the augmentation: %d violations on object 1, want 0 (its window ends before endobj)", n)
	}

	count := d.XRefEntryCount()
	d.AugmentXRef(context.Background())
	if !d.XRefAugmented() {
		t.Fatal("the augmentation did not change the index")
	}
	if got := d.XRefEntryCount(); got != count {
		t.Fatalf("the augmentation changed the entry count from %d to %d, the fixture must only move an offset", count, got)
	}
	if entry, _ := d.XRefEntry(2); entry.Offset != m.off2 {
		t.Fatalf("object 2 offset = %d after the augmentation, want %d", entry.Offset, m.off2)
	}

	for _, c := range []struct{ off, want int64 }{
		{m.off1, m.off2},
		{m.stale, m.off2},
		{m.off2, m.off3},
	} {
		if got := d.nextNormalObjectOffset(c.off); got != c.want {
			t.Errorf("after the augmentation: nextNormalObjectOffset(%d) = %d, want %d", c.off, got, c.want)
		}
	}
	if n := endobjViolations(t, m); n != 1 {
		t.Errorf("after the augmentation: %d violations on object 1, want 1 (endobj not preceded by EOL)", n)
	}
}

// TestNextNormalObjectOffset_ConcurrentAugmentation races the offset cache
// against the augmentation that invalidates it: goroutines resolving object 2
// run the augmentation, whose header mismatch triggers it, while others read
// the cache through object 1's header check. Under -race it fails if the
// generation the cache compares is read or written outside d.mu. Once every
// goroutine has returned, the cache must reflect the moved offset.
func TestNextNormalObjectOffset_ConcurrentAugmentation(t *testing.T) {
	m := newMisplacedObject(t)
	d := m.doc
	ctx := context.Background()
	d.SetComplianceMode(true)

	// Build the cache from the table's view first, so the race is against a
	// cache the augmentation has to invalidate.
	d.nextNormalObjectOffset(m.off1)

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
				if got := d.nextNormalObjectOffset(m.off1); got != m.stale && got != m.off2 {
					t.Errorf("nextNormalObjectOffset(%d) = %d, want %d or %d", m.off1, got, m.stale, m.off2)
				}
				d.ValidateObjectHeaderFormat(m.off1, 1)
			}
		})
	}
	wg.Wait()

	if got := d.nextNormalObjectOffset(m.off1); got != m.off2 {
		t.Errorf("after the augmentation: nextNormalObjectOffset(%d) = %d, want %d", m.off1, got, m.off2)
	}
	if n := endobjViolations(t, m); n != 1 {
		t.Errorf("after the augmentation: %d violations on object 1, want 1", n)
	}
}
