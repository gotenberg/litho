package document

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// unlistedObjectsPDF builds a document whose cross-reference table and
// trailer /Size stop at object 3 while its body goes on to define objects 4
// through 3+unlisted, each one after a comment line of padding bytes, so the
// raw-byte scan that finds them reads the file in several chunks when the
// padding is large.
func unlistedObjectsPDF(unlisted, padding int) []byte {
	listed := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(listed))
	for i, obj := range listed {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	for i := range unlisted {
		if padding > 0 {
			b.WriteByte('%')
			b.Write(bytes.Repeat([]byte{'x'}, padding))
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%d 0 obj\n<< /Unlisted %d >>\nendobj\n", len(listed)+1+i, i)
	}
	xrefAt := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(listed)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(listed)+1, xrefAt)
	return b.Bytes()
}

// openUnlistedObjects opens data and checks that opening it did not run the
// raw-byte augmentation, so the first run is the one the test controls.
func openUnlistedObjects(t *testing.T, r io.ReaderAt, size int64) *Document {
	t.Helper()
	doc, err := NewDocument(context.Background(), r, size, nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if _, ok := doc.XRefEntry(4); ok {
		t.Fatal("opening the document added object 4, so the fixture cannot control the first augmentation")
	}
	return doc
}

// TestAugmentXRef_CancelledRunRunsAgain pins that a raw-byte augmentation ctx
// cancels, before its scan or in the middle of it, leaves the index as it
// stood and the Document able to run it again: the next call with a live ctx
// completes it, so [Document.HighestObjectNumber] clears the objects the table
// leaves out and they resolve.
//
// A sync.Once spent by the cancelled run left the index un-augmented for the
// Document's lifetime, so every later allocator was seeded below the unlisted
// objects and numbered its first new object over one of them.
func TestAugmentXRef_CancelledRunRunsAgain(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		unlisted int
		padding  int
		after    int32 // ctx.Err calls answered before the cancellation
	}{
		{name: "before the scan", unlisted: 1, after: 0},
		// The scan checks ctx once per 256 KiB chunk: the cancellation comes
		// after it read the chunk that holds object 4 and before the one that
		// holds object 5.
		{name: "mid-scan", unlisted: 2, padding: 300 << 10, after: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data := unlistedObjectsPDF(tc.unlisted, tc.padding)
			doc := openUnlistedObjects(t, bytes.NewReader(data), int64(len(data)))

			cancelled := &cancelAfterCtx{Context: context.Background(), after: tc.after}
			if got := doc.HighestObjectNumber(cancelled); got != 3 {
				t.Fatalf("cancelled run: HighestObjectNumber = %d, want 3, the index as it stood", got)
			}
			if _, ok := doc.XRefEntry(4); ok {
				t.Fatal("the cancelled run applied the objects its partial scan found")
			}
			if doc.XRefAugmented() {
				t.Fatal("the cancelled run reports the index augmented")
			}

			ctx := context.Background()
			want := 3 + tc.unlisted
			if got := doc.HighestObjectNumber(ctx); got != want {
				t.Fatalf("after a cancelled run: HighestObjectNumber = %d, want %d: an allocator seeded from it numbers a new object over an unlisted one", got, want)
			}
			if !doc.XRefAugmented() {
				t.Error("the second run did not augment the index")
			}
			for num := 4; num <= want; num++ {
				obj, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: num})
				if err != nil {
					t.Fatalf("resolving unlisted object %d: %v", num, err)
				}
				if dict, ok := obj.(ast.PdfDict); !ok || dict["Unlisted"] != ast.PdfInt(num-4) {
					t.Errorf("unlisted object %d resolved to %v, want << /Unlisted %d >>", num, obj, num-4)
				}
			}
		})
	}
}

// TestAugmentXRef_CancelledRunReturnsTheError pins that [Document.AugmentXRef]
// reports a run ctx cancelled, and reports nothing once a run completed, even
// under a cancelled ctx: the index is then final and nothing waits.
func TestAugmentXRef_CancelledRunReturnsTheError(t *testing.T) {
	t.Parallel()

	data := unlistedObjectsPDF(1, 0)
	doc := openUnlistedObjects(t, bytes.NewReader(data), int64(len(data)))

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := doc.AugmentXRef(cancelled); err == nil {
		t.Fatal("AugmentXRef under a cancelled ctx returned nil, want the cancellation")
	}
	if err := doc.AugmentXRef(context.Background()); err != nil {
		t.Fatalf("AugmentXRef: %v", err)
	}
	if err := doc.AugmentXRef(cancelled); err != nil {
		t.Errorf("AugmentXRef after a completed run returned %v, want nil", err)
	}
}

// scanCountingReader counts the reads that end a raw-byte scan
// ([scanIndirectObjects]): a read that reaches the end of the file and is larger
// than any other reader of the Document asks for. The parser reads through
// 32 KiB buffers and the table readers read less, while the scan reads 256 KiB
// chunks.
type scanCountingReader struct {
	r         *bytes.Reader
	completed atomic.Int32
}

// scanReadFloor is the smallest read [scanCountingReader] attributes to the
// raw-byte scan.
const scanReadFloor = 128 << 10

func (s *scanCountingReader) ReadAt(p []byte, off int64) (int, error) {
	n, err := s.r.ReadAt(p, off)
	if len(p) > scanReadFloor && off+int64(n) == s.r.Size() {
		s.completed.Add(1)
	}
	return n, err
}

// TestAugmentXRef_CancelRacingTheScan runs the augmentation from many goroutines
// at once, some under a ctx cancelled before or during the scan, beside readers
// of the index, and pins the concurrency contract: the scan runs to completion
// exactly once, a caller whose ctx stays live always reads the augmented index
// whoever started the scan, and no reader sees a partial augmentation. Run it
// under -race with -count.
func TestAugmentXRef_CancelRacingTheScan(t *testing.T) {
	t.Parallel()

	const unlisted = 4
	const listedEntries = 4 // the free entry 0 and objects 1-3
	const want = 3 + unlisted
	data := unlistedObjectsPDF(unlisted, 300<<10)

	// The scan reads 256 KiB chunks that overlap by 32 bytes, so its last read
	// starts at a multiple of 256 KiB. It must exceed scanReadFloor for the
	// reader to count it.
	const chunk = 256 << 10
	last := int64(0)
	for last+chunk+32 < int64(len(data)) {
		last += chunk
	}
	if int64(len(data))-last <= scanReadFloor {
		t.Fatalf("fixture is wrong: the scan's last read is %d bytes, want more than %d", int64(len(data))-last, scanReadFloor)
	}

	ctx := context.Background()
	const iterations = 6
	const goroutines = 16
	for range iterations {
		src := &scanCountingReader{r: bytes.NewReader(data)}
		doc := openUnlistedObjects(t, src, int64(len(data)))

		var wg sync.WaitGroup
		for g := range goroutines {
			switch g % 4 {
			case 0:
				// A live caller seeding an allocator must clear every unlisted
				// object, even when a cancelled caller started the scan.
				wg.Go(func() {
					if got := doc.HighestObjectNumber(ctx); got != want {
						t.Errorf("live caller: HighestObjectNumber = %d, want %d", got, want)
					}
				})
			case 1:
				wg.Go(func() {
					cancelled := &cancelAfterCtx{Context: ctx, after: int32(g % 5)}
					if got := doc.HighestObjectNumber(cancelled); got != 3 && got != want {
						t.Errorf("cancelled caller: HighestObjectNumber = %d, want 3 or %d", got, want)
					}
				})
			case 2:
				cancelled, cancel := context.WithCancel(ctx)
				wg.Go(cancel)
				wg.Go(func() {
					if got := doc.HighestObjectNumber(cancelled); got != 3 && got != want {
						t.Errorf("racing cancel: HighestObjectNumber = %d, want 3 or %d", got, want)
					}
				})
			default:
				wg.Go(func() {
					for range 8 {
						if n := doc.XRefEntryCount(); n != listedEntries && n != listedEntries+unlisted {
							t.Errorf("reader: %d entries, want %d or %d", n, listedEntries, listedEntries+unlisted)
						}
						seen := 0
						for range doc.XRefEntries() {
							seen++
						}
						if seen != listedEntries && seen != listedEntries+unlisted {
							t.Errorf("reader: ranged %d entries, want %d or %d", seen, listedEntries, listedEntries+unlisted)
						}
						if n := doc.OpenedXRefEntryCount(); n != listedEntries {
							t.Errorf("reader: %d opened entries, want %d", n, listedEntries)
						}
						if _, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: 3}); err != nil {
							t.Errorf("reader: resolving object 3: %v", err)
						}
					}
					// A miss under a live ctx runs or waits for the scan.
					if _, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: want}); err != nil {
						t.Errorf("reader: resolving unlisted object %d: %v", want, err)
					}
				})
			}
		}
		wg.Wait()

		if got := src.completed.Load(); got != 1 {
			t.Errorf("the scan ran to completion %d times, want once", got)
		}
		if got := doc.HighestObjectNumber(ctx); got != want {
			t.Errorf("after the race: HighestObjectNumber = %d, want %d", got, want)
		}
		if got := doc.XRefEntryCount(); got != listedEntries+unlisted {
			t.Errorf("after the race: %d entries, want %d", got, listedEntries+unlisted)
		}
	}
}

// gatedReader holds every raw-byte scan read ([scanIndirectObjects]) until
// release is closed, and closes entered at the first one, so a test can act
// while a scan is in progress. Every other read passes straight through.
type gatedReader struct {
	r       *bytes.Reader
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func newGatedReader(data []byte) *gatedReader {
	return &gatedReader{r: bytes.NewReader(data), entered: make(chan struct{}), release: make(chan struct{})}
}

func (g *gatedReader) ReadAt(p []byte, off int64) (int, error) {
	if len(p) > scanReadFloor {
		g.once.Do(func() { close(g.entered) })
		<-g.release
	}
	return g.r.ReadAt(p, off)
}

// TestAugmentXRef_WaiterRunsTheScanACancelledRunLeft pins that a caller who
// finds a run in progress, and sees that run cancelled, runs the scan itself
// under its own live ctx rather than reading the index the cancelled run left.
// With a sync.Once the waiter returned from the spent Once with the index as
// it stood, whenever it arrived.
func TestAugmentXRef_WaiterRunsTheScanACancelledRunLeft(t *testing.T) {
	t.Parallel()

	data := unlistedObjectsPDF(1, 300<<10)
	gate := newGatedReader(data)
	doc := openUnlistedObjects(t, gate, int64(len(data)))

	ctx := context.Background()
	holderCtx, cancelHolder := context.WithCancel(ctx)
	holder := make(chan int, 1)
	go func() { holder <- doc.HighestObjectNumber(holderCtx) }()
	<-gate.entered

	waiter := make(chan int, 1)
	go func() { waiter <- doc.HighestObjectNumber(ctx) }()
	cancelHolder()
	close(gate.release)

	if got := <-holder; got != 3 {
		t.Errorf("cancelled holder: HighestObjectNumber = %d, want 3, the index as it stood", got)
	}
	if got := <-waiter; got != 4 {
		t.Errorf("live waiter: HighestObjectNumber = %d, want 4", got)
	}
}

// TestAugmentXRef_CancelledWaiterDoesNotWait pins that a caller whose ctx is
// done does not wait out a scan another caller has in progress: it returns
// the cancellation while that scan is still held, and the scan then completes
// for its own caller.
func TestAugmentXRef_CancelledWaiterDoesNotWait(t *testing.T) {
	t.Parallel()

	data := unlistedObjectsPDF(1, 300<<10)
	gate := newGatedReader(data)
	doc := openUnlistedObjects(t, gate, int64(len(data)))

	ctx := context.Background()
	holder := make(chan int, 1)
	go func() { holder <- doc.HighestObjectNumber(ctx) }()
	<-gate.entered

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	waiter := make(chan error, 1)
	go func() { waiter <- doc.AugmentXRef(cancelled) }()
	select {
	case err := <-waiter:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("cancelled waiter: AugmentXRef = %v, want context.Canceled", err)
		}
	case <-time.After(30 * time.Second):
		t.Error("cancelled waiter is still waiting for another caller's scan")
	}

	close(gate.release)
	if got := <-holder; got != 4 {
		t.Errorf("holder: HighestObjectNumber = %d, want 4", got)
	}
}
