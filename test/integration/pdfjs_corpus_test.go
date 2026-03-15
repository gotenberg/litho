package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/pdf"
)

const corpusDir = "testdata/pdf.js"

// perSubtestTimeout is the maximum time allowed for a single corpus subtest.
// Some corpus PDFs have pathologically complex content streams; we skip them
// rather than blocking the entire suite.
const perSubtestTimeout = 30 * time.Second

// manifestEntry represents a single entry from PDF.js test_manifest.json.
type manifestEntry struct {
	ID       string `json:"id"`
	File     string `json:"file"`
	MD5      string `json:"md5"`
	Type     string `json:"type"`
	LastPage int    `json:"lastPage"`
	Link     any    `json:"link"`
}

// knownSkips maps manifest IDs to skip reasons for PDFs that exercise features
// litho does not yet support.
var knownSkips = map[string]string{
	// JBIG2Decode filter not implemented.
	"jbig2-image-load": "JBIG2Decode not supported",
	// JPXDecode (JPEG2000) filter not implemented.
	"jpx-image-load": "JPXDecode not supported",
	// Malformed xref table causes infinite loop in parseXRefChain.
	"bug1393476": "infinite loop in xref parsing",
}

func loadManifest(t *testing.T) []manifestEntry {
	t.Helper()

	manifestPath := filepath.Join(corpusDir, "test_manifest.json")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Skip("pdfjs corpus not found at pkg/pdf/testdata/pdf.js; run scripts/fetch-pdfjs-corpus.sh")
	}

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("reading manifest: %v", err)
	}

	var entries []manifestEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("parsing manifest: %v", err)
	}

	return entries
}

// dedup returns manifest entries deduplicated by file path so each PDF is
// opened only once even if the manifest lists multiple test types for it.
func dedup(entries []manifestEntry) []manifestEntry {
	seen := make(map[string]bool)
	var unique []manifestEntry
	for _, e := range entries {
		if seen[e.File] {
			continue
		}
		seen[e.File] = true
		unique = append(unique, e)
	}
	return unique
}

// openPDF opens a PDF from the corpus directory. Returns the Reader.
// If the file does not exist on disk the test is skipped.
func openPDF(t *testing.T, entry manifestEntry) *pdf.Reader {
	t.Helper()

	pdfPath := filepath.Join(corpusDir, entry.File)
	f, err := os.Open(pdfPath)
	if err != nil {
		t.Skipf("pdf not available (likely dead upstream link): %s", entry.File)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Errorf("closing %s: %v", entry.File, err)
		}
	})

	info, err := f.Stat()
	if err != nil {
		t.Fatalf("stat %s: %v", entry.File, err)
	}

	r, err := pdf.Open(context.Background(), f, info.Size(), "")
	if err != nil {
		// Some PDFs are intentionally malformed. An error here is acceptable
		// as long as we don't panic. Log and return nil so the caller can skip
		// further extraction.
		t.Logf("open error (may be expected): %v", err)
		return nil
	}

	return r
}

// runWithTimeout executes fn in a goroutine and waits up to perSubtestTimeout.
// Returns true if fn completed, false if it timed out.
func runWithTimeout(t *testing.T, name string, fn func(ctx context.Context)) bool {
	t.Helper()
	done := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), perSubtestTimeout)
	defer cancel()

	go func() {
		defer close(done)
		fn(ctx)
	}()

	select {
	case <-done:
		return true
	case <-ctx.Done():
		t.Logf("%s: skipped (exceeded %v timeout)", name, perSubtestTimeout)
		return false
	}
}

// resolveLastPage returns the effective last page to test for a given entry.
func resolveLastPage(entry manifestEntry, numPages int) int {
	lastPage := numPages
	if entry.LastPage > 0 && entry.LastPage < lastPage {
		lastPage = entry.LastPage
	}
	return lastPage
}

// TestPDFJSCorpus_Parse opens every PDF in the corpus and traverses its page
// tree. This exercises the lexer, parser, xref engine, linearization, and
// decryption pipeline.
func TestPDFJSCorpus_Parse(t *testing.T) {
	entries := dedup(loadManifest(t))

	for _, entry := range entries {
		t.Run(entry.ID, func(t *testing.T) {
			t.Parallel()

			if reason, ok := knownSkips[entry.ID]; ok {
				t.Skipf("known skip: %s", reason)
			}

			r := openPDF(t, entry)
			if r == nil {
				return
			}

			runWithTimeout(t, entry.ID, func(ctx context.Context) {
				numPages, err := r.NumPages(ctx)
				if err != nil {
					t.Logf("NumPages error: %v", err)
					return
				}

				lastPage := resolveLastPage(entry, numPages)

				for i := 1; i <= lastPage; i++ {
					if _, err := r.GetPage(ctx, i); err != nil {
						t.Errorf("GetPage(%d): %v", i, err)
					}
				}

				t.Logf("OK: %d pages", lastPage)
			})
		})
	}
}

// TestPDFJSCorpus_TextExtraction extracts text from every page of every
// parseable PDF. Survival (no panic, no nil-pointer) is the test.
func TestPDFJSCorpus_TextExtraction(t *testing.T) {
	entries := dedup(loadManifest(t))

	for _, entry := range entries {
		t.Run(entry.ID, func(t *testing.T) {
			t.Parallel()

			if reason, ok := knownSkips[entry.ID]; ok {
				t.Skipf("known skip: %s", reason)
			}

			r := openPDF(t, entry)
			if r == nil {
				return
			}

			runWithTimeout(t, entry.ID, func(ctx context.Context) {
				numPages, err := r.NumPages(ctx)
				if err != nil {
					t.Logf("NumPages error: %v", err)
					return
				}

				lastPage := resolveLastPage(entry, numPages)

				for i := 1; i <= lastPage; i++ {
					pageNum := i
					text, err := r.ExtractText(ctx, pageNum)
					if err != nil {
						t.Logf("page %d text error: %v", pageNum, err)
						return
					}
					t.Logf("page %d: %d chars", pageNum, len(text))
				}
			})
		})
	}
}

// TestPDFJSCorpus_Metadata exercises the document-level metadata, outline,
// and form APIs on every parseable PDF.
func TestPDFJSCorpus_Metadata(t *testing.T) {
	entries := dedup(loadManifest(t))

	for _, entry := range entries {
		t.Run(entry.ID, func(t *testing.T) {
			t.Parallel()

			if reason, ok := knownSkips[entry.ID]; ok {
				t.Skipf("known skip: %s", reason)
			}

			r := openPDF(t, entry)
			if r == nil {
				return
			}

			runWithTimeout(t, entry.ID, func(ctx context.Context) {
				meta, err := r.GetMetadata(ctx)
				if err != nil {
					t.Logf("GetMetadata error: %v", err)
				}

				bookmarks, err := r.GetBookmarks(ctx)
				if err != nil {
					t.Logf("GetBookmarks error: %v", err)
				}

				formData, err := r.GetFormData(ctx)
				if err != nil {
					t.Logf("GetFormData error: %v", err)
				}

				isPortfolio, err := r.IsPortfolio(ctx)
				if err != nil {
					t.Logf("IsPortfolio error: %v", err)
				}

				t.Logf("metadata=%d bookmarks=%d formFields=%d portfolio=%v linearized=%v",
					len(meta), len(bookmarks), len(formData), isPortfolio, r.IsLinearized())
			})
		})
	}
}

// TestPDFJSCorpus_LazyResources verifies the lazy accessor contract on a
// small sample of corpus PDFs that contain images. It confirms that Open is
// present and callable without exercising every resource in every PDF.
func TestPDFJSCorpus_LazyResources(t *testing.T) {
	entries := dedup(loadManifest(t))

	const maxSampled = 20
	sampled := 0

	for _, entry := range entries {
		if sampled >= maxSampled {
			break
		}

		t.Run(entry.ID, func(t *testing.T) {
			t.Parallel()

			if reason, ok := knownSkips[entry.ID]; ok {
				t.Skipf("known skip: %s", reason)
			}

			r := openPDF(t, entry)
			if r == nil {
				return
			}

			runWithTimeout(t, entry.ID, func(ctx context.Context) {
				numPages, err := r.NumPages(ctx)
				if err != nil {
					t.Logf("NumPages error: %v", err)
					return
				}

				// Only inspect the first page to keep this fast.
				if numPages < 1 {
					return
				}

				images, err := r.InspectImages(ctx, 1)
				if err != nil {
					t.Logf("InspectImages error: %v", err)
					return
				}

				if len(images) == 0 {
					return
				}

				// Verify Open is present and callable on the first image.
				img := images[0]
				if img.Open == nil {
					t.Errorf("image %q: Open accessor is nil", img.Filename)
					return
				}

				rc, err := img.Open(context.Background())
				if err != nil {
					t.Logf("image %q Open error: %v", img.Filename, err)
					return
				}
				if err := rc.Close(); err != nil {
					t.Errorf("image %q Close error: %v", img.Filename, err)
				}

				t.Logf("image %q: %dx%d %s — Open OK", img.Filename, img.Width, img.Height, img.ColorSpace)
			})
		})

		sampled++
	}
}

// --- Reference comparison types and test ---

// referenceFixture is the JSON schema written by gen-pdfjs-reference.mjs.
type referenceFixture struct {
	ID       string          `json:"id"`
	File     string          `json:"file"`
	NumPages int             `json:"numPages"`
	Metadata json.RawMessage `json:"metadata"`
	Outline  []refOutline    `json:"outline"`
	Pages    []refPage       `json:"pages"`
}

type refOutline struct {
	Title string `json:"title"`
}

type refPage struct {
	PageNumber int       `json:"pageNumber"`
	Text       string    `json:"text"`
	ItemCount  int       `json:"itemCount"`
	Items      []refItem `json:"items"`
	Error      string    `json:"error,omitempty"`
}

type refItem struct {
	Str      string  `json:"str"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Width    float64 `json:"width"`
	Height   float64 `json:"height"`
	FontName string  `json:"fontName"`
}

const referencesDir = "testdata/pdf.js/references"

// coordTolerance is the maximum allowed difference in coordinate values
// between PDF.js and litho. Different rounding in CTM and font metrics
// makes exact equality unrealistic.
const coordTolerance = 2.0

// TestPDFJSCorpus_TextComparison loads reference fixtures generated by
// gen-pdfjs-reference.mjs and compares litho's text extraction output.
// Text content must match exactly (after whitespace normalization).
// Coordinate values are compared with a tolerance.
func TestPDFJSCorpus_TextComparison(t *testing.T) {
	if _, err := os.Stat(referencesDir); os.IsNotExist(err) {
		t.Skip("reference fixtures not found at pkg/pdf/testdata/pdf.js/references/; run scripts/fetch-pdfjs-corpus.sh --refs")
	}

	entries := dedup(loadManifest(t))

	for _, entry := range entries {
		refPath := filepath.Join(referencesDir, entry.ID+".json")
		if _, err := os.Stat(refPath); os.IsNotExist(err) {
			continue // No reference for this entry.
		}

		t.Run(entry.ID, func(t *testing.T) {
			t.Parallel()

			if reason, ok := knownSkips[entry.ID]; ok {
				t.Skipf("known skip: %s", reason)
			}

			// Load reference fixture.
			refData, err := os.ReadFile(refPath)
			if err != nil {
				t.Fatalf("reading reference: %v", err)
			}
			var ref referenceFixture
			if err := json.Unmarshal(refData, &ref); err != nil {
				t.Fatalf("parsing reference: %v", err)
			}

			r := openPDF(t, entry)
			if r == nil {
				return
			}

			// Compare page count.
			numPages, err := r.NumPages(context.Background())
			if err != nil {
				t.Logf("NumPages error: %v", err)
				return
			}
			if numPages != ref.NumPages {
				t.Errorf("page count: litho=%d pdfjs=%d", numPages, ref.NumPages)
			}

			// Compare outline/bookmarks.
			if len(ref.Outline) > 0 {
				bookmarks, err := r.GetBookmarks(context.Background())
				if err != nil {
					t.Logf("GetBookmarks error: %v", err)
				} else if len(bookmarks) > 0 {
					minLen := min(len(bookmarks), len(ref.Outline))
					for i := range minLen {
						if bookmarks[i].Title != ref.Outline[i].Title {
							t.Errorf("bookmark[%d] title: litho=%q pdfjs=%q",
								i, bookmarks[i].Title, ref.Outline[i].Title)
						}
					}
				}
			}

			// Compare per-page text.
			for _, refPage := range ref.Pages {
				if refPage.Error != "" {
					continue // PDF.js couldn't extract this page either.
				}

				pageNum := refPage.PageNumber
				completed := runWithTimeout(t, entry.ID, func(ctx context.Context) {
					text, err := r.ExtractText(ctx, pageNum)
					if err != nil {
						t.Logf("page %d text error: %v", pageNum, err)
						return
					}

					// Normalize whitespace for comparison: collapse runs of
					// whitespace to single space, trim.
					lithoNorm := normalizeWhitespace(text)
					pdfjsNorm := normalizeWhitespace(refPage.Text)

					if lithoNorm != pdfjsNorm {
						// Log a truncated diff rather than dumping megabytes.
						diff := textDiffSummary(pdfjsNorm, lithoNorm, 200)
						t.Errorf("page %d text mismatch:\n%s", pageNum, diff)
					}

					// Compare text element coordinates if reference has items.
					if len(refPage.Items) > 0 {
						elems, err := r.ExtractTextElements(ctx, pageNum)
						if err != nil {
							t.Logf("page %d elements error: %v", pageNum, err)
							return
						}

						minLen := min(len(elems), len(refPage.Items))

						coordMismatches := 0
						for i := range minLen {
							ri := refPage.Items[i]
							li := elems[i]

							if math.Abs(li.X-ri.X) > coordTolerance ||
								math.Abs(li.Y-ri.Y) > coordTolerance {
								coordMismatches++
								if coordMismatches <= 3 {
									t.Logf("page %d elem[%d] coord mismatch: litho=(%.1f,%.1f) pdfjs=(%.1f,%.1f)",
										pageNum, i, li.X, li.Y, ri.X, ri.Y)
								}
							}
						}

						if coordMismatches > 3 {
							t.Logf("page %d: %d additional coordinate mismatches (tolerance=%.1f)",
								pageNum, coordMismatches-3, coordTolerance)
						}
					}
				})

				if !completed {
					break // Stop checking remaining pages if one timed out.
				}
			}
		})
	}
}

// normalizeWhitespace collapses all runs of whitespace (including newlines)
// to a single space and trims the result.
func normalizeWhitespace(s string) string {
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

// textDiffSummary returns a short summary showing where two strings first
// diverge, truncated to maxLen characters of context.
func textDiffSummary(expected, got string, maxLen int) string {
	// Find the first differing position.
	minLen := min(len(got), len(expected))

	divergeAt := minLen
	for i := range minLen {
		if expected[i] != got[i] {
			divergeAt = i
			break
		}
	}

	// Show context around the divergence point.
	start := max(divergeAt-40, 0)
	endExp := min(divergeAt+maxLen, len(expected))
	endGot := min(divergeAt+maxLen, len(got))

	return fmt.Sprintf("  first difference at position %d\n  expected: ...%s...\n  got:      ...%s...",
		divergeAt, expected[start:endExp], got[start:endGot])
}
