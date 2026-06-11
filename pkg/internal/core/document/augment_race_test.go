package document

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

// TestResolve_ConcurrentAugmentation_NoRace exercises the lazy
// xref-augmentation write against concurrent map reads. Object 2's
// stored offset points at object 1's header, so the first resolve of
// object 2 fails objectHeaderMatches and triggers
// augmentXrefFromRawScan, which writes Index.Entries. Other goroutines
// resolving objects 1/3/4 read the same map at the same time. Run under
// -race, this would fail with a concurrent map read/write before the
// d.mu guard was added in xrefEntry / augmentXrefFromRawScan.
func TestResolve_ConcurrentAugmentation_NoRace(t *testing.T) {
	objs := []string{
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n",
		"2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n",
		"3 0 obj\n<< /A 1 >>\nendobj\n",
		"4 0 obj\n<< /B 2 >>\nendobj\n",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int64, len(objs)+1) // 1-based
	for i, o := range objs {
		offsets[i+1] = int64(buf.Len())
		buf.WriteString(o)
	}
	data := buf.Bytes()

	// Build the index by hand so no construction-time resolve consumes the
	// augmentation sync.Once before the goroutines run. Object 2's offset
	// is deliberately wrong (points at object 1's header).
	entries := map[int]xref.XRefEntry{
		1: {Type: xref.XRefTypeNormal, Offset: offsets[1]},
		2: {Type: xref.XRefTypeNormal, Offset: offsets[1]}, // wrong on purpose
		3: {Type: xref.XRefTypeNormal, Offset: offsets[3]},
		4: {Type: xref.XRefTypeNormal, Offset: offsets[4]},
	}
	doc := &Document{
		r:    bytes.NewReader(data),
		size: int64(len(data)),
		Index: xref.Index{
			Entries: entries,
			Trailer: ast.PdfDict{"Size": ast.PdfInt(5), "Root": ast.PdfIndirectRef{ObjectNum: 1}},
		},
		objectCache: make(map[int]ast.PdfObject),
		fontCache:   make(map[int]any),
		cmapCache:   make(map[int]any),
		Logger:      slog.New(slog.DiscardHandler),
	}

	ctx := context.Background()
	const goroutines = 16
	const iterations = 40

	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for range iterations {
				// Vary order per goroutine to maximise overlap of the
				// object-2 augmentation write with reads of 1/3/4.
				order := []int{1, 2, 3, 4}
				if g%2 == 0 {
					order = []int{4, 3, 2, 1}
				}
				for _, n := range order {
					_, _ = doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: n})
				}
			}
		}(g)
	}
	wg.Wait()

	// After augmentation, object 2 must resolve to the Pages dict at its
	// real offset, proving the override took effect.
	obj2, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: 2})
	if err != nil {
		t.Fatalf("resolve object 2: %v", err)
	}
	dict, ok := obj2.(ast.PdfDict)
	if !ok {
		t.Fatalf("object 2 resolved to %T, want ast.PdfDict", obj2)
	}
	if got := dict["Type"]; got != ast.PdfName("Pages") {
		t.Errorf("object 2 /Type = %v, want Pages", got)
	}
}
