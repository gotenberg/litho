package pdf

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"
)

// richReaderPDF builds a document exercising the readers this file covers: a
// named-destination tree whose /Limits do not bound their subtree, an outline
// reaching pages both ways, an interactive form with a filled and an unfilled
// field, and a link annotation.
func richReaderPDF(t *testing.T) []byte {
	t.Helper()

	const content = "BT /F1 12 Tf 20 700 Td (page) Tj ET\n"
	utf16Key := "<FEFF00480069>"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Names << /Dests 7 0 R >> /Outlines 8 0 R" +
			" /AcroForm << /Fields [11 0 R 12 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R" +
			" /Resources << /Font << /F1 6 0 R >> >> /Annots [13 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R" +
			" /Resources << /Font << /F1 6 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		// Root /Limits stops at "second" but the child holds the 0xFE key, so a
		// lookup must fall back to an unpruned walk.
		"<< /Kids [10 0 R] /Limits [(first) (second)] >>",
		"<< /Type /Outlines /First 9 0 R /Last 9 0 R /Count 1 >>",
		fmt.Sprintf("<< /Title (jump) /Parent 8 0 R /Dest << /Type /Action /S /GoTo /D %s >> >>", utf16Key),
		fmt.Sprintf("<< /Limits [(first) %s] /Names [(first) [3 0 R /Fit] %s [4 0 R /Fit]] >>",
			utf16Key, utf16Key),
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (filled) /V (value) /Rect [0 0 10 10] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (blank) /Rect [0 0 10 10] >>",
		"<< /Type /Annot /Subtype /Link /Rect [10 10 50 50] /Dest [4 0 R /Fit] >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// TestConcurrentSemanticReaders pins the documented contract that a Reader is
// safe for concurrent reads, for the readers that resolve document-wide tables.
//
// The existing concurrency tests cover text and page metadata. These readers
// walk the name tree, the outline and the interactive form instead, and the
// named-destination lookup now makes a second, unpruned pass when the first
// misses, so it builds more state per call than it used to. Run under -race.
func TestConcurrentSemanticReaders(t *testing.T) {
	t.Parallel()

	data := richReaderPDF(t)
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := r.Close(); closeErr != nil {
			t.Logf("closing reader: %v", closeErr)
		}
	})

	const goroutines = 16
	errs := make(chan error, goroutines*4)
	var wg sync.WaitGroup

	for range goroutines {
		wg.Add(4)
		go func() {
			defer wg.Done()
			if _, err := r.FormFields(context.Background()); err != nil {
				errs <- fmt.Errorf("FormFields: %w", err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := r.Bookmarks(context.Background()); err != nil {
				errs <- fmt.Errorf("Bookmarks: %w", err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := r.ExtractAnnotations(context.Background(), 1); err != nil {
				errs <- fmt.Errorf("ExtractAnnotations: %w", err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := r.FormData(context.Background()); err != nil {
				errs <- fmt.Errorf("FormData: %w", err)
			}
		}()
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent read failed: %v", err)
	}
}

// TestConcurrentReadersAgreeWithSerial pins that concurrency does not change
// what the readers report: a cache populated under contention must hold the
// same answers a single-threaded run produces.
func TestConcurrentReadersAgreeWithSerial(t *testing.T) {
	t.Parallel()

	data := richReaderPDF(t)
	serial, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		if closeErr := serial.Close(); closeErr != nil {
			t.Logf("closing reader: %v", closeErr)
		}
	}()

	wantFields, err := serial.FormFields(context.Background())
	if err != nil {
		t.Fatalf("FormFields: %v", err)
	}
	wantMarks, err := serial.Bookmarks(context.Background())
	if err != nil {
		t.Fatalf("Bookmarks: %v", err)
	}

	concurrent, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		if closeErr := concurrent.Close(); closeErr != nil {
			t.Logf("closing reader: %v", closeErr)
		}
	}()

	var wg sync.WaitGroup
	counts := make(chan [2]int, 12)
	for range 12 {
		wg.Go(func() {
			f, ferr := concurrent.FormFields(context.Background())
			b, berr := concurrent.Bookmarks(context.Background())
			if ferr != nil || berr != nil {
				counts <- [2]int{-1, -1}
				return
			}
			counts <- [2]int{len(f), len(b)}
		})
	}
	wg.Wait()
	close(counts)

	for got := range counts {
		if got[0] != len(wantFields) || got[1] != len(wantMarks) {
			t.Errorf("concurrent read saw %d fields / %d bookmarks, serial saw %d / %d",
				got[0], got[1], len(wantFields), len(wantMarks))
		}
	}
}
