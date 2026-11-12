package document

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestObjectCacheHoldsAtMostTheLimit pins the bound: however many objects are
// inserted, the cache holds no more than the limit, and an object it holds
// reads back as the one inserted under its number.
func TestObjectCacheHoldsAtMostTheLimit(t *testing.T) {
	var c objectCache
	const limit = 7
	for n := range 100 {
		c.insert(n, ast.PdfInt(n), limit)
		if len(c.slots) > limit {
			t.Fatalf("after %d insertions the cache holds %d objects, past %d", n+1, len(c.slots), limit)
		}
		if n%3 == 0 {
			c.lookup(n)
		}
	}
	held := 0
	for n := range 100 {
		obj, ok := c.lookup(n)
		if !ok {
			continue
		}
		held++
		if obj != ast.PdfInt(n) {
			t.Errorf("object %d reads back as %v", n, obj)
		}
	}
	if held != limit {
		t.Errorf("the cache holds %d objects, want %d", held, limit)
	}
}

// TestObjectCacheKeepsWhatIsRead pins the order: an object read between
// insertions stays while a sweep of objects read once passes through a full
// cache, as the page tree root does while a validation sweeps every object.
func TestObjectCacheKeepsWhatIsRead(t *testing.T) {
	var c objectCache
	const limit = 10
	c.insert(0, ast.PdfInt(0), limit)
	for n := 1; n < 1000; n++ {
		if _, ok := c.lookup(0); !ok {
			t.Fatalf("object 0, read before each insertion, was replaced by the time object %d came", n)
		}
		c.insert(n, ast.PdfInt(n), limit)
	}
}

// TestObjectCacheWithoutLimit pins that a limit of zero or less bounds
// nothing.
func TestObjectCacheWithoutLimit(t *testing.T) {
	var c objectCache
	for n := range 1000 {
		c.insert(n, ast.PdfInt(n), 0)
	}
	if len(c.slots) != 1000 {
		t.Errorf("the unbounded cache holds %d objects, want 1000", len(c.slots))
	}
}

// TestResolveUnderReplacementIsConcurrentSafe pins that goroutines resolving
// through a cache far smaller than the file read every object as the file
// holds it while lookups mark objects and insertions replace them. Run under
// -race it also checks the locking.
func TestResolveUnderReplacementIsConcurrentSafe(t *testing.T) {
	t.Parallel()
	const objects = 40
	bodies := make([]string, objects)
	for i := range bodies {
		bodies[i] = fmt.Sprintf("<< /N %d >>\n", i+1)
	}
	data, offsets := formatViolationPDF(t, bodies...)
	doc := formatViolationDoc(data, offsets)
	doc.maxCacheSize = 5

	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for g := range 8 {
		wg.Go(func() {
			for round := range 20 {
				for i := range objects {
					n := (i*(g+1)+round)%objects + 1
					obj, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: n})
					if err != nil {
						errs <- err
						return
					}
					dict, ok := obj.(ast.PdfDict)
					if !ok || dict["N"] != ast.PdfInt(n) {
						errs <- fmt.Errorf("object %d resolved as %v", n, obj)
						return
					}
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if got := len(doc.objectCache.slots); got > doc.maxCacheSize {
		t.Errorf("the cache holds %d objects, past %d", got, doc.maxCacheSize)
	}
}
