package document

import (
	"bytes"
	"context"
	"testing"
)

// minimalPDFForCache is the smallest document NewDocument accepts. The cache
// under test is keyed by object number and never reads the file, so the content
// only has to parse.
func minimalPDFForCache() []byte {
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n" +
		"trailer\n<< /Root 1 0 R /Size 3 >>\n%%EOF\n")
}

func cacheTestDoc(t *testing.T) *Document {
	t.Helper()
	data := minimalPDFForCache()
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// TestFontProgramCacheRoundTrip covers the store-then-load contract.
//
// The font rules re-read the same embedded program for every page that declares
// the font, so this cache is what keeps a validation from inflating one stream
// thousands of times. A miss reported as a hit would hand callers a nil program
// and silently drop every font check that depends on it.
func TestFontProgramCacheRoundTrip(t *testing.T) {
	t.Parallel()

	doc := cacheTestDoc(t)

	if _, ok := doc.CachedFontProgram(7); ok {
		t.Fatal("empty cache reported a hit")
	}

	want := []byte("a font program")
	doc.SetCachedFontProgram(7, want)

	got, ok := doc.CachedFontProgram(7)
	if !ok {
		t.Fatal("stored program reported as absent")
	}
	if !bytes.Equal(got, want) {
		t.Errorf("cached program = %q, want %q", got, want)
	}
	if _, ok = doc.CachedFontProgram(8); ok {
		t.Error("unrelated object number reported a hit")
	}
}

// TestFontProgramCacheFirstWins matches the behaviour of the neighbouring
// caches: an entry already held is never replaced, so concurrent decoders of
// the same stream agree on one buffer rather than swapping it underneath each
// other. Callers share that buffer and must not modify it.
func TestFontProgramCacheFirstWins(t *testing.T) {
	t.Parallel()

	doc := cacheTestDoc(t)
	first := []byte("first")
	doc.SetCachedFontProgram(3, first)
	doc.SetCachedFontProgram(3, []byte("second"))

	got, ok := doc.CachedFontProgram(3)
	if !ok {
		t.Fatal("program went missing after a second store")
	}
	if !bytes.Equal(got, first) {
		t.Errorf("cached program = %q, want the first store %q", got, first)
	}
}

// TestFontProgramCacheRejectsOversized asserts a single program larger than the
// whole budget is not cached. Admitting it would evict the map on every store
// and leave the cache thrashing without ever serving a hit.
func TestFontProgramCacheRejectsOversized(t *testing.T) {
	t.Parallel()

	doc := cacheTestDoc(t)
	doc.SetCachedFontProgram(1, make([]byte, maxFontProgCacheBytes+1))

	if _, ok := doc.CachedFontProgram(1); ok {
		t.Error("a program larger than the budget was cached")
	}
}

// TestFontProgramCacheEvictsOnOverflow asserts the byte budget is enforced and
// that the cache keeps working afterwards. Without the bound a crafted file
// declaring many large font programs would grow it without limit.
func TestFontProgramCacheEvictsOnOverflow(t *testing.T) {
	t.Parallel()

	doc := cacheTestDoc(t)

	// Two halves fit; the third forces an evict-all and is then stored alone.
	half := make([]byte, maxFontProgCacheBytes/2)
	doc.SetCachedFontProgram(1, half)
	doc.SetCachedFontProgram(2, half)
	if _, ok := doc.CachedFontProgram(1); !ok {
		t.Fatal("first program evicted before the budget was reached")
	}

	doc.SetCachedFontProgram(3, half)
	if _, ok := doc.CachedFontProgram(1); ok {
		t.Error("overflow did not evict the earlier entries")
	}
	if _, ok := doc.CachedFontProgram(3); !ok {
		t.Error("the program that triggered eviction was not stored")
	}
}

// TestClearCachesDropsFontPrograms asserts the font-program cache takes part in
// [Document.ClearCaches]. Authenticate clears the caches because objects parsed
// before decryption are ciphertext, and a font program left behind would be the
// one piece of that stale state still served to the font rules.
func TestClearCachesDropsFontPrograms(t *testing.T) {
	t.Parallel()

	doc := cacheTestDoc(t)
	doc.SetCachedFontProgram(5, []byte("stale"))
	doc.ClearCaches()

	if _, ok := doc.CachedFontProgram(5); ok {
		t.Error("ClearCaches left the font program behind")
	}
}
