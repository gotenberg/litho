package document

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"sync/atomic"
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

// countingReaderAt counts the reads of each length at each offset.
type countingReaderAt struct {
	r     *bytes.Reader
	mu    sync.Mutex
	reads map[[2]int64]int
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	c.mu.Lock()
	c.reads[[2]int64{off, int64(len(p))}]++
	c.mu.Unlock()
	return c.r.ReadAt(p, off)
}

// TestHeaderOfEachObjectReadOnce pins that the header check of an object the
// document was opened with reads the file once for the document's life, both
// the object's own and the one its parse reads to bound it at the next
// object, however often a small cache makes resolution parse it again.
func TestHeaderOfEachObjectReadOnce(t *testing.T) {
	t.Parallel()
	const objects = 6
	bodies := make([]string, objects)
	for i := range bodies {
		bodies[i] = fmt.Sprintf("<< /N %d >>\n", i+1)
	}
	data, offsets := formatViolationPDF(t, bodies...)
	doc := formatViolationDoc(data, offsets)
	counter := &countingReaderAt{r: bytes.NewReader(data), reads: map[[2]int64]int{}}
	doc.r = counter
	doc.maxCacheSize = 1
	doc.objectStarts = declaredObjectStarts(doc.xrefEntries)
	doc.declaredStarts = doc.objectStarts
	doc.startHeaders = make([]atomic.Uint32, len(doc.objectStarts))

	ctx := context.Background()
	for range 5 {
		for n := 1; n <= objects; n++ {
			obj, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: n})
			if err != nil {
				t.Fatalf("Resolve %d: %v", n, err)
			}
			if dict, ok := obj.(ast.PdfDict); !ok || dict["N"] != ast.PdfInt(n) {
				t.Fatalf("object %d resolved as %v", n, obj)
			}
		}
	}
	for n := 1; n <= objects; n++ {
		if got := counter.reads[[2]int64{offsets[n], 64}]; got != 1 {
			t.Errorf("the header of object %d was read %d times over 5 parses, want once", n, got)
		}
	}
}

// TestRecordedObjectIsNotCheckedAgain pins that in compliance mode an object
// whose format violations are recorded is parsed again without the format
// checks, which read its header bytes: the first record stands, and the
// violations reported are the same.
func TestRecordedObjectIsNotCheckedAgain(t *testing.T) {
	t.Parallel()
	data, offsets := formatViolationPDF(t,
		"<< /Type /Catalog >>\n",
		"<< /A 1 >>",       // no EOL before endobj: one violation
		"<< /B <414> >>\n", // odd count of hex digits: one violation
	)
	doc := formatViolationDoc(data, offsets)
	counter := &countingReaderAt{r: bytes.NewReader(data), reads: map[[2]int64]int{}}
	doc.r = counter
	doc.maxCacheSize = 1
	doc.SetComplianceMode(true)

	ctx := context.Background()
	for range 5 {
		for n := 1; n <= 3; n++ {
			if _, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: n}); err != nil {
				t.Fatalf("Resolve %d: %v", n, err)
			}
		}
	}
	for n := 1; n <= 3; n++ {
		if got := counter.reads[[2]int64{offsets[n], 40}]; got != 1 {
			t.Errorf("the header format of object %d was read %d times over 5 parses, want once", n, got)
		}
	}
	if got := doc.FormatViolations(); len(got) != 2 {
		t.Errorf("recorded %d violations, want 2: %v", len(got), got)
	}
}
