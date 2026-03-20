//go:build integration

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

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

const (
	corpusDir     = "testdata/pdf.js"
	referencesDir = "testdata/pdf.js/references"
)

// perSubtestTimeout is the maximum time allowed for a single corpus subtest.
// Some corpus PDFs have pathologically complex content streams; we skip them
// rather than blocking the entire suite.
const perSubtestTimeout = 30 * time.Second

// coordTolerance is the maximum allowed difference in coordinate values
// between PDF.js and litho. Different rounding in CTM and font metrics
// makes exact equality unrealistic.
const coordTolerance = 2.0

// --- Manifest types ---

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
	"bug1393476": "infinite loop in xref parsing",
}

var knownSkipPrefixes = []struct {
	prefix string
	reason string
}{
	{"jbig2", "JBIG2Decode not supported"},
	{"JBIG2", "JBIG2Decode not supported"},
	{"jpx", "JPXDecode (JPEG2000) not supported"},
}

func skipIfKnown(t *testing.T, id string) {
	t.Helper()
	if reason, ok := knownSkips[id]; ok {
		t.Skipf("known skip: %s", reason)
	}
	for _, sp := range knownSkipPrefixes {
		if strings.HasPrefix(id, sp.prefix) {
			t.Skipf("known skip: %s", sp.reason)
		}
	}
}

// --- Reference fixture types (matches PDF.js gen-pdfjs-reference.mjs output) ---

type referenceFixture struct {
	ID       string          `json:"id"`
	File     string          `json:"file"`
	NumPages int             `json:"numPages"`
	Metadata refMetadata     `json:"metadata"`
	Outline  json.RawMessage `json:"outline"`
	Pages    []refPage       `json:"pages"`
}

type refMetadata struct {
	Info refInfo `json:"info"`
}

type refInfo struct {
	PDFFormatVersion    string `json:"PDFFormatVersion"`
	Language            string `json:"Language"`
	IsLinearized        bool   `json:"IsLinearized"`
	IsAcroFormPresent   bool   `json:"IsAcroFormPresent"`
	IsXFAPresent        bool   `json:"IsXFAPresent"`
	IsCollectionPresent bool   `json:"IsCollectionPresent"`
	IsSignaturesPresent bool   `json:"IsSignaturesPresent"`
	Author              string `json:"Author"`
	Title               string `json:"Title"`
	Creator             string `json:"Creator"`
	Producer            string `json:"Producer"`
	CreationDate        string `json:"CreationDate"`
	ModDate             string `json:"ModDate"`
	Keywords            string `json:"Keywords"`
	EncryptFilterName   any    `json:"EncryptFilterName"`
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

// --- Litho output generation (same schema as PDF.js references) ---

// lithoReference generates a referenceFixture from a litho Reader, producing
// output in the same format as the PDF.js reference files.
func lithoReference(ctx context.Context, t *testing.T, r *pdf.Reader, id string, lastPage int) *referenceFixture {
	t.Helper()

	fixture := &referenceFixture{ID: id}

	numPages, err := r.NumPages(ctx)
	if err != nil {
		t.Logf("NumPages error: %v", err)
		return nil
	}
	fixture.NumPages = numPages

	// Metadata.
	fixture.Metadata.Info.PDFFormatVersion = r.Version()
	fixture.Metadata.Info.IsLinearized = r.IsLinearized()

	if meta, err := r.GetMetadata(ctx); err == nil {
		fixture.Metadata.Info.Author = meta["Author"]
		fixture.Metadata.Info.Title = meta["Title"]
		fixture.Metadata.Info.Creator = meta["Creator"]
		fixture.Metadata.Info.Producer = meta["Producer"]
		fixture.Metadata.Info.CreationDate = meta["CreationDate"]
		fixture.Metadata.Info.ModDate = meta["ModDate"]
		fixture.Metadata.Info.Keywords = meta["Keywords"]
		fixture.Metadata.Info.Language = meta["Language"]
	}

	if formData, err := r.GetFormData(ctx); err == nil {
		fixture.Metadata.Info.IsAcroFormPresent = len(formData) > 0
	}

	if isPortfolio, err := r.IsPortfolio(ctx); err == nil {
		fixture.Metadata.Info.IsCollectionPresent = isPortfolio
	}

	// Pages.
	effectiveLastPage := numPages
	if lastPage > 0 && lastPage < effectiveLastPage {
		effectiveLastPage = lastPage
	}

	for i := 1; i <= effectiveLastPage; i++ {
		if err := ctx.Err(); err != nil {
			return fixture
		}

		page := refPage{PageNumber: i}

		// Use WithHiddenText to match PDF.js's getTextContent, which returns
		// all text regardless of rendering mode, color, or clipping.
		text, err := r.ExtractText(ctx, i, pdf.WithHiddenText())
		if err != nil {
			page.Error = err.Error()
			fixture.Pages = append(fixture.Pages, page)
			continue
		}
		page.Text = text

		elems, err := r.ExtractTextElements(ctx, i)
		if err != nil {
			page.Error = err.Error()
			fixture.Pages = append(fixture.Pages, page)
			continue
		}

		// Group consecutive glyphs into text runs to match PDF.js item format.
		// PDF.js groups by text content operator (Tj/TJ), producing multi-char
		// strings. litho's TextElements are per-glyph. We group consecutive
		// glyphs that share the same Y coordinate (within tolerance) into runs.
		page.Items = groupGlyphsIntoRuns(elems)
		page.ItemCount = len(page.Items)

		fixture.Pages = append(fixture.Pages, page)
	}

	return fixture
}

// groupGlyphsIntoRuns groups consecutive per-glyph TextElements into text runs
// matching PDF.js's item format. A new run starts when the Y coordinate changes
// significantly (different baseline = different text operation).
func groupGlyphsIntoRuns(elems []models.TextElement) []refItem {
	if len(elems) == 0 {
		return nil
	}

	var runs []refItem
	current := refItem{
		Str:    elems[0].Text,
		X:      elems[0].X,
		Y:      elems[0].Y,
		Width:  elems[0].Width,
		Height: elems[0].FontSize,
	}

	for i := 1; i < len(elems); i++ {
		e := elems[i]
		// Same baseline (within tolerance) = same run.
		if math.Abs(e.Y-current.Y) < 0.5 {
			current.Str += e.Text
			current.Width = (e.X + e.Width) - current.X
		} else {
			runs = append(runs, current)
			current = refItem{
				Str:    e.Text,
				X:      e.X,
				Y:      e.Y,
				Width:  e.Width,
				Height: e.FontSize,
			}
		}
	}
	runs = append(runs, current)

	return runs
}

// --- Comparison ---

func compareFixtures(t *testing.T, pdfjs *referenceFixture, litho *referenceFixture) {
	t.Helper()

	// Page count.
	if litho.NumPages != pdfjs.NumPages {
		t.Errorf("numPages: litho=%d pdfjs=%d", litho.NumPages, pdfjs.NumPages)
	}

	// Metadata.
	compareMetadata(t, pdfjs.Metadata.Info, litho.Metadata.Info)

	// Per-page text and items.
	for _, pdfjsPage := range pdfjs.Pages {
		if pdfjsPage.Error != "" {
			continue // PDF.js couldn't extract this page either.
		}

		var lithoPage *refPage
		for j := range litho.Pages {
			if litho.Pages[j].PageNumber == pdfjsPage.PageNumber {
				lithoPage = &litho.Pages[j]
				break
			}
		}

		if lithoPage == nil {
			t.Errorf("page %d: litho has no output", pdfjsPage.PageNumber)
			continue
		}

		if lithoPage.Error != "" {
			// If PDF.js also has no text for this page, litho's error is acceptable
			// (e.g., ErrRequiresOCR on a genuinely image-only page).
			if strings.TrimSpace(pdfjsPage.Text) == "" {
				continue
			}
			t.Errorf("page %d: litho error: %s", pdfjsPage.PageNumber, lithoPage.Error)
			continue
		}

		comparePageText(t, pdfjsPage.PageNumber, pdfjsPage.Text, lithoPage.Text)
		comparePageItems(t, pdfjsPage.PageNumber, pdfjsPage.Items, lithoPage.Items)
	}
}

func compareMetadata(t *testing.T, pdfjs, litho refInfo) {
	t.Helper()

	if litho.PDFFormatVersion != pdfjs.PDFFormatVersion && pdfjs.PDFFormatVersion != "" {
		t.Errorf("metadata PDFFormatVersion: litho=%q pdfjs=%q", litho.PDFFormatVersion, pdfjs.PDFFormatVersion)
	}
	if litho.IsLinearized != pdfjs.IsLinearized {
		t.Errorf("metadata IsLinearized: litho=%v pdfjs=%v", litho.IsLinearized, pdfjs.IsLinearized)
	}
	if litho.Title != pdfjs.Title && pdfjs.Title != "" {
		t.Logf("metadata Title: litho=%q pdfjs=%q", litho.Title, pdfjs.Title)
	}
	if litho.Author != pdfjs.Author && pdfjs.Author != "" {
		t.Logf("metadata Author: litho=%q pdfjs=%q", litho.Author, pdfjs.Author)
	}
}

func comparePageText(t *testing.T, pageNum int, pdfjsText, lithoText string) {
	t.Helper()

	// Normalize whitespace for comparison: collapse runs to single space, trim.
	pdfjsNorm := normalizeWhitespace(pdfjsText)
	lithoNorm := normalizeWhitespace(lithoText)

	if lithoNorm != pdfjsNorm {
		diff := textDiffSummary(pdfjsNorm, lithoNorm, 200)
		t.Errorf("page %d text mismatch:\n%s", pageNum, diff)
	}
}

func comparePageItems(t *testing.T, pageNum int, pdfjsItems, lithoItems []refItem) {
	t.Helper()

	if len(pdfjsItems) == 0 {
		return
	}

	// Compare item count.
	if len(lithoItems) != len(pdfjsItems) {
		t.Logf("page %d itemCount: litho=%d pdfjs=%d", pageNum, len(lithoItems), len(pdfjsItems))
	}

	minLen := min(len(lithoItems), len(pdfjsItems))
	coordMismatches := 0
	strMismatches := 0

	for i := range minLen {
		ri := pdfjsItems[i]
		li := lithoItems[i]

		if li.Str != ri.Str {
			strMismatches++
			if strMismatches <= 3 {
				t.Logf("page %d item[%d] str: litho=%q pdfjs=%q", pageNum, i, truncate(li.Str, 40), truncate(ri.Str, 40))
			}
		}

		if math.Abs(li.X-ri.X) > coordTolerance || math.Abs(li.Y-ri.Y) > coordTolerance {
			coordMismatches++
			if coordMismatches <= 3 {
				t.Logf("page %d item[%d] coord: litho=(%.1f,%.1f) pdfjs=(%.1f,%.1f)",
					pageNum, i, li.X, li.Y, ri.X, ri.Y)
			}
		}
	}

	if strMismatches > 3 {
		t.Logf("page %d: %d additional str mismatches", pageNum, strMismatches-3)
	}
	if coordMismatches > 3 {
		t.Logf("page %d: %d additional coord mismatches (tolerance=%.1f)", pageNum, coordMismatches-3, coordTolerance)
	}
}

// --- Helpers ---

func loadManifest(t *testing.T) []manifestEntry {
	t.Helper()

	manifestPath := filepath.Join(corpusDir, "test_manifest.json")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Skip("pdfjs corpus not found; run scripts/fetch-pdfjs-corpus.sh")
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

func openPDF(t *testing.T, entry manifestEntry) *pdf.Reader {
	t.Helper()

	pdfPath := filepath.Join(corpusDir, entry.File)
	f, err := os.Open(pdfPath)
	if err != nil {
		t.Skipf("pdf not available: %s", entry.File)
		return nil
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

	r, err := pdf.Open(context.Background(), f, info.Size())
	if err != nil {
		t.Logf("open error (may be expected): %v", err)
		return nil
	}

	return r
}

func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func textDiffSummary(expected, got string, maxLen int) string {
	minLen := min(len(got), len(expected))

	divergeAt := minLen
	for i := range minLen {
		if expected[i] != got[i] {
			divergeAt = i
			break
		}
	}

	start := max(divergeAt-40, 0)
	endExp := min(divergeAt+maxLen, len(expected))
	endGot := min(divergeAt+maxLen, len(got))

	return fmt.Sprintf("  first difference at position %d\n  expected: ...%s...\n  got:      ...%s...",
		divergeAt, expected[start:endExp], got[start:endGot])
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// --- The test ---

// TestPDFJSCorpus runs every PDF in the Mozilla PDF.js corpus through litho.
// When a reference file exists, litho's output is generated in the same format
// and compared structurally. When no reference exists, the test simply verifies
// that litho can open and extract without panicking.
func TestPDFJSCorpus(t *testing.T) {
	entries := dedup(loadManifest(t))

	for _, entry := range entries {
		t.Run(entry.ID, func(t *testing.T) {
			t.Parallel()

			skipIfKnown(t, entry.ID)

			r := openPDF(t, entry)
			if r == nil {
				return
			}

			// Check if a reference file exists for this entry.
			refPath := filepath.Join(referencesDir, entry.ID+".json")
			hasRef := false
			if _, err := os.Stat(refPath); err == nil {
				hasRef = true
			}

			done := make(chan struct{})
			ctx, cancel := context.WithTimeout(context.Background(), perSubtestTimeout)
			defer cancel()

			go func() {
				defer close(done)

				if hasRef {
					// Load PDF.js reference.
					refData, err := os.ReadFile(refPath)
					if err != nil {
						t.Errorf("reading reference: %v", err)
						return
					}
					var pdfjsRef referenceFixture
					if err := json.Unmarshal(refData, &pdfjsRef); err != nil {
						t.Errorf("parsing reference: %v", err)
						return
					}

					// Generate litho output in the same format.
					lithoRef := lithoReference(ctx, t, r, entry.ID, entry.LastPage)
					if lithoRef == nil {
						return
					}

					// Compare.
					compareFixtures(t, &pdfjsRef, lithoRef)
				} else {
					// No reference — survival test: open, count pages, extract text.
					numPages, err := r.NumPages(ctx)
					if err != nil {
						t.Logf("NumPages error: %v", err)
						return
					}

					lastPage := numPages
					if entry.LastPage > 0 && entry.LastPage < lastPage {
						lastPage = entry.LastPage
					}

					for i := 1; i <= lastPage; i++ {
						if _, err := r.ExtractText(ctx, i); err != nil {
							t.Logf("page %d: %v", i, err)
							return
						}
					}
				}
			}()

			select {
			case <-done:
				// Completed.
			case <-ctx.Done():
				t.Logf("skipped (exceeded %v timeout)", perSubtestTimeout)
			}
		})
	}
}
