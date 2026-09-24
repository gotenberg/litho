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

// normalizeBookmarkTitle trims surrounding whitespace, NFC-normalises
// the title, and rewrites C1 control runes (U+0080-U+009F) to their
// PDFDocEncoding glyph equivalents.
//
// The C1 rewrite is what keeps the canary honest. PDFDocEncoding
// (ISO 32000-1 Table D.2) maps bytes 0x80-0x9F to typographic glyphs
// (en dash, em dash, smart quotes, etc.); litho applies this mapping
// at decode time, matching PDF.js's PDFStringTranslateTable. pdfcpu
// emits the raw byte as a C1 control rune (U+0085, U+0090, ...), so a
// title containing the en-dash byte 0x85 surfaces as "Annex A – Foo"
// in litho and "Annex A  Foo" in the pdfcpu golden. Both sides
// are pointing at the same source byte; the rewrite folds them back
// onto the same glyph for comparison without weakening the test for
// genuine title differences.
func normalizeBookmarkTitle(s string) string {
	s = norm.NFC.String(strings.TrimSpace(s))
	var buf strings.Builder
	buf.Grow(len(s))
	for _, r := range s {
		if g, ok := pdfDocC1Glyphs[r]; ok {
			buf.WriteRune(g)
			continue
		}
		buf.WriteRune(r)
	}
	return buf.String()
}

// pdfDocC1Glyphs mirrors the 0x80-0x9F slice of PDFDocEncoding
// (ISO 32000-1 Table D.2) promoted from byte → rune to rune → rune.
// Kept in sync by hand with litho's document.DecodePdfString table;
// the entries are immutable so duplication is cheap.
var pdfDocC1Glyphs = map[rune]rune{
	0x0080: 0x2022, // BULLET
	0x0081: 0x2020, // DAGGER
	0x0082: 0x2021, // DOUBLE DAGGER
	0x0083: 0x2026, // HORIZONTAL ELLIPSIS
	0x0084: 0x2014, // EM DASH
	0x0085: 0x2013, // EN DASH
	0x0086: 0x0192, // LATIN SMALL LETTER F WITH HOOK
	0x0087: 0x2044, // FRACTION SLASH
	0x0088: 0x2039, // SINGLE LEFT-POINTING ANGLE QUOTATION MARK
	0x0089: 0x203A, // SINGLE RIGHT-POINTING ANGLE QUOTATION MARK
	0x008A: 0x2212, // MINUS SIGN
	0x008B: 0x2030, // PER MILLE SIGN
	0x008C: 0x201E, // DOUBLE LOW-9 QUOTATION MARK
	0x008D: 0x201C, // LEFT DOUBLE QUOTATION MARK
	0x008E: 0x201D, // RIGHT DOUBLE QUOTATION MARK
	0x008F: 0x2018, // LEFT SINGLE QUOTATION MARK
	0x0090: 0x2019, // RIGHT SINGLE QUOTATION MARK
	0x0091: 0x201A, // SINGLE LOW-9 QUOTATION MARK
	0x0092: 0x2122, // TRADE MARK SIGN
	0x0093: 0xFB01, // LATIN SMALL LIGATURE FI
	0x0094: 0xFB02, // LATIN SMALL LIGATURE FL
	0x0095: 0x0141, // LATIN CAPITAL LETTER L WITH STROKE
	0x0096: 0x0152, // LATIN CAPITAL LIGATURE OE
	0x0097: 0x0160, // LATIN CAPITAL LETTER S WITH CARON
	0x0098: 0x0178, // LATIN CAPITAL LETTER Y WITH DIAERESIS
	0x0099: 0x017D, // LATIN CAPITAL LETTER Z WITH CARON
	0x009A: 0x0131, // LATIN SMALL LETTER DOTLESS I
	0x009B: 0x0142, // LATIN SMALL LETTER L WITH STROKE
	0x009C: 0x0153, // LATIN SMALL LIGATURE OE
	0x009D: 0x0161, // LATIN SMALL LETTER S WITH CARON
	0x009E: 0x017E, // LATIN SMALL LETTER Z WITH CARON
	// 0x9F: undefined in PDFDocEncoding
}

// diffBookmarks returns "" when got and want are structurally equal or
// a short path-qualified description of the first divergence otherwise.
// The pre-order traversal halts on the first miss, so [Verdict.Reason]
// carries a stable, readable reason rather than an exhaustive diff.
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
	r, err := pdf.Open(ctx, f, info.Size())
	if err != nil {
		return nil, fmt.Errorf("pdf.Open: %w", err)
	}
	defer r.Close()
	return r.Bookmarks(ctx)
}
