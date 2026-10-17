package pdf

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestConcurrentGenerateBookmarks races [Reader.GenerateBookmarks] on one
// Reader and pins that every concurrent call reports the headings a serial
// call reports.
//
// It takes options, so the uniform fan-out of TestConcurrentReaderSurface does
// not reach it. The fixture carries headings in three font sizes, so each call
// extracts the text of every page and scores it rather than returning an empty
// tree early. Run under -race.
func TestConcurrentGenerateBookmarks(t *testing.T) {
	t.Parallel()

	data := headingPDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := r.Close(); closeErr != nil {
			t.Logf("closing reader: %v", closeErr)
		}
	})

	serial, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		if closeErr := serial.Close(); closeErr != nil {
			t.Logf("closing reader: %v", closeErr)
		}
	}()
	want, err := serial.GenerateBookmarks(context.Background(), BookmarkGenerationOptions{})
	if err != nil {
		t.Fatalf("GenerateBookmarks: %v", err)
	}
	// Without headings the calls would race nothing but an early return.
	if len(want) == 0 {
		t.Fatal("the fixture yields no bookmark, so the race exercises no heading detection")
	}

	const goroutines = 16
	errs := make(chan error, goroutines)
	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			got, err := r.GenerateBookmarks(context.Background(), BookmarkGenerationOptions{})
			if err != nil {
				errs <- fmt.Errorf("GenerateBookmarks: %w", err)
				return
			}
			if !slices.Equal(bookmarkTitles(got), bookmarkTitles(want)) {
				errs <- fmt.Errorf("concurrent call saw %q, serial saw %q", bookmarkTitles(got), bookmarkTitles(want))
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// bookmarkTitles flattens a bookmark tree into its titles, depth first.
func bookmarkTitles(bms []models.Bookmark) []string {
	var out []string
	for _, b := range bms {
		out = append(out, b.Title)
		out = append(out, bookmarkTitles(b.Children)...)
	}
	return out
}

// TestConcurrentVerifySignatures races [Reader.VerifySignatures] on one
// Reader holding a signature litho made, and pins that every concurrent call
// reports the verdict a serial call reports.
//
// It takes a certificate pool, so the uniform fan-out of
// TestConcurrentReaderSurface does not reach it, and that fixture carries no
// signature. A signed document makes each call hash the signed byte ranges,
// parse the CMS and build the chain. Run under -race.
func TestConcurrentVerifySignatures(t *testing.T) {
	t.Parallel()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pki := newTestPKI(t, key)
	signer, err := NewSigner(pki.leaf, key, pki.root)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	data := signWith(t, writerTestPDF(), signer)
	want := verifyOne(t, data, pki, 1)

	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := r.Close(); closeErr != nil {
			t.Logf("closing reader: %v", closeErr)
		}
	})

	roots := pki.roots()
	const goroutines = 16
	errs := make(chan error, goroutines)
	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			got, err := r.VerifySignatures(context.Background(), roots)
			if err != nil {
				errs <- fmt.Errorf("VerifySignatures: %w", err)
				return
			}
			if len(got) != len(want) {
				errs <- fmt.Errorf("concurrent call saw %d signatures, serial saw %d", len(got), len(want))
				return
			}
			for i := range got {
				if got[i].FieldName != want[i].FieldName || got[i].HashValid != want[i].HashValid ||
					got[i].CertChainValid != want[i].CertChainValid || got[i].MDPInfo != want[i].MDPInfo {
					errs <- fmt.Errorf("signature %d: concurrent call saw %+v, serial saw %+v", i, got[i], want[i])
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
