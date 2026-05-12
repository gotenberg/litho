package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/text/unicode/norm"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
	"github.com/gotenberg/litho/pkg/plugins/jbig2"
	"github.com/gotenberg/litho/pkg/plugins/jpx"
)

// pdfcpuBookmarkFile is the layout emitted by `pdfcpu bookmarks export`.
// Only the bookmarks tree is consulted; the header (producer, title,
// creation date) is ignored because it duplicates the metadata feature.
type pdfcpuBookmarkFile struct {
	Bookmarks []pdfcpuBookmark `json:"bookmarks"`
}

// pdfcpuBookmark mirrors a single node in pdfcpu's outline JSON. Color
// and any other annotation fields are deliberately not modeled: the
// comparison is structural (title, page, children) only.
type pdfcpuBookmark struct {
	Title string           `json:"title"`
	Page  int              `json:"page"`
	Kids  []pdfcpuBookmark `json:"kids,omitempty"`
}

// normalizedBookmark is the shape both sides (pdfcpu golden + litho
// output) are coerced to before diffing. Titles go through NFC + trim
// so producer-specific encoding quirks (NBSP vs space, NFD on Mac
// PDFs) do not register as mismatches.
type normalizedBookmark struct {
	Title    string               `json:"title"`
	Page     int                  `json:"page"`
	Children []normalizedBookmark `json:"children,omitempty"`
}

// bookmarksFeature compares each fixture's outline tree against the
// pdfcpu export captured at provisioning time. Score is binary: exact
// structural match (1) or any divergence (0). Reason carries the first
// divergent path (e.g. bookmarks[1].kids[0].title) so mismatches read
// without needing to diff full JSON blobs.
//
// Run memoises by PDFPath; corpora that expand a PDF into multiple
// (pdf, profile) fixtures only pay the outline extraction cost once.
type bookmarksFeature struct {
	mu      sync.RWMutex
	goldens map[string][]normalizedBookmark
	cache   sync.Map // pdfPath -> *cachedRun
}

func newBookmarksFeature() *bookmarksFeature {
	return &bookmarksFeature{goldens: make(map[string][]normalizedBookmark)}
}

func (f *bookmarksFeature) Name() string         { return "bookmarks" }
func (f *bookmarksFeature) GoldenSource() string { return "pdfcpu" }

func (f *bookmarksFeature) LoadGoldens(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ErrNoGoldens
	}
	loaded := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var raw pdfcpuBookmarkFile
		if err := json.Unmarshal(data, &raw); err != nil {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		f.mu.Lock()
		f.goldens[id] = normalizePdfcpuTree(raw.Bookmarks)
		f.mu.Unlock()
		loaded++
	}
	if loaded == 0 {
		return ErrNoGoldens
	}
	return nil
}

func (f *bookmarksFeature) HasGolden(fixtureID string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.goldens[baseFixtureID(fixtureID)]
	return ok
}

type bookmarksOutput struct {
	Bookmarks []normalizedBookmark `json:"bookmarks"`
}

func (f *bookmarksFeature) Run(ctx context.Context, pdfPath, _ string) ([]byte, error) {
	if v, ok := f.cache.Load(pdfPath); ok {
		cr := v.(*cachedRun)
		return cr.output, cr.err
	}
	bm, err := extractBookmarks(ctx, pdfPath)
	if err != nil {
		f.cache.Store(pdfPath, &cachedRun{err: err})
		return nil, err
	}
	out, err := json.Marshal(bookmarksOutput{Bookmarks: normalizeLithoTree(bm)})
	f.cache.Store(pdfPath, &cachedRun{output: out, err: err})
	return out, err
}

func (f *bookmarksFeature) Compare(fixtureID string, output []byte) Verdict {
	f.mu.RLock()
	golden, ok := f.goldens[baseFixtureID(fixtureID)]
	f.mu.RUnlock()
	if !ok {
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: "no golden"}
	}

	var got bookmarksOutput
	if err := json.Unmarshal(output, &got); err != nil {
		return Verdict{FixtureID: fixtureID, Reason: fmt.Sprintf("decode output: %v", err)}
	}

	if diff := diffBookmarks("bookmarks", got.Bookmarks, golden); diff != "" {
		return Verdict{FixtureID: fixtureID, Reason: diff}
	}
	return Verdict{FixtureID: fixtureID, Match: true, Score: 1.0}
}

// normalizePdfcpuTree coerces pdfcpu's export tree into the comparison
// shape. Nil-or-empty input yields nil so a PDF with no outline compares
// equal whether the producer emitted "bookmarks": [] or omitted the key.
func normalizePdfcpuTree(in []pdfcpuBookmark) []normalizedBookmark {
	if len(in) == 0 {
		return nil
	}
	out := make([]normalizedBookmark, len(in))
	for i, b := range in {
		out[i] = normalizedBookmark{
			Title:    normalizeBookmarkTitle(b.Title),
			Page:     b.Page,
			Children: normalizePdfcpuTree(b.Kids),
		}
	}
	return out
}

// normalizeLithoTree coerces []models.Bookmark into the comparison
// shape, mirroring [normalizePdfcpuTree].
func normalizeLithoTree(in []models.Bookmark) []normalizedBookmark {
	if len(in) == 0 {
		return nil
	}
	out := make([]normalizedBookmark, len(in))
	for i, b := range in {
		out[i] = normalizedBookmark{
			Title:    normalizeBookmarkTitle(b.Title),
			Page:     b.PageNumber,
			Children: normalizeLithoTree(b.Children),
		}
	}
	return out
}

// normalizeBookmarkTitle trims surrounding whitespace and NFC-normalises
// the title. PDF outline strings sit at the byte boundary between many
// producers and consumers, so canonicalising here keeps trivial
// encoding differences (NBSP vs space, decomposed accents) from
// inflating the mismatch count.
func normalizeBookmarkTitle(s string) string {
	return norm.NFC.String(strings.TrimSpace(s))
}

// diffBookmarks returns "" when got and want are structurally equal or
// a short path-qualified description of the first divergence otherwise.
// The pre-order traversal halts on the first miss; we want a stable,
// readable reason in [Verdict.Reason], not an exhaustive diff.
func diffBookmarks(path string, got, want []normalizedBookmark) string {
	if len(got) != len(want) {
		return fmt.Sprintf("%s: len got=%d want=%d", path, len(got), len(want))
	}
	for i := range got {
		p := fmt.Sprintf("%s[%d]", path, i)
		if got[i].Title != want[i].Title {
			return fmt.Sprintf("%s.title: got %q want %q", p, got[i].Title, want[i].Title)
		}
		if got[i].Page != want[i].Page {
			return fmt.Sprintf("%s.page: got %d want %d", p, got[i].Page, want[i].Page)
		}
		if d := diffBookmarks(p+".kids", got[i].Children, want[i].Children); d != "" {
			return d
		}
	}
	return ""
}

func extractBookmarks(ctx context.Context, pdfPath string) ([]models.Bookmark, error) {
	f, err := os.Open(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}
	r, err := pdf.Open(ctx, f, info.Size(),
		pdf.WithFilterPlugin(jbig2.New()),
		pdf.WithFilterPlugin(jpx.New()),
	)
	if err != nil {
		return nil, fmt.Errorf("pdf.Open: %w", err)
	}
	defer r.Close()
	return r.Bookmarks(ctx)
}
