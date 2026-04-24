package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/pdf"
	"github.com/gotenberg/litho/pkg/plugins/jbig2"
	"github.com/gotenberg/litho/pkg/plugins/jpx"
)

// pdfjsReference is the golden layout produced by PDF.js's reference
// generator: one JSON file per fixture carrying per-page text, items,
// metadata, and outline. Text and metadata features consume this.
type pdfjsReference struct {
	ID       string           `json:"id"`
	File     string           `json:"file"`
	NumPages int              `json:"numPages"`
	Metadata pdfjsRefMetadata `json:"metadata"`
	Outline  json.RawMessage  `json:"outline"`
	Pages    []pdfjsRefPage   `json:"pages"`
}

type pdfjsRefMetadata struct {
	Info pdfjsRefInfo `json:"info"`
}

type pdfjsRefInfo struct {
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
}

type pdfjsRefPage struct {
	PageNumber int            `json:"pageNumber"`
	Text       string         `json:"text"`
	ItemCount  int            `json:"itemCount"`
	Items      []pdfjsRefItem `json:"items"`
	Error      string         `json:"error,omitempty"`
}

type pdfjsRefItem struct {
	Str      string  `json:"str"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Width    float64 `json:"width"`
	Height   float64 `json:"height"`
	FontName string  `json:"fontName"`
}

// textFeature compares per-page text extraction against the PDF.js
// reference. Primary metric is per-fixture words-match (whitespace-
// normalised); chars_match (whitespace-insensitive NFKC match) is
// reported as a secondary breakdown.
//
// Run memoises by PDFPath so corpora that expand each PDF into multiple
// (pdf, profile) fixtures (e.g. govdocs) only pay the extraction cost
// once per PDF.
type textFeature struct {
	mu      sync.RWMutex
	goldens map[string]*pdfjsReference
	cache   sync.Map // pdfPath -> *cachedRun
}

func newTextFeature() *textFeature {
	return &textFeature{goldens: make(map[string]*pdfjsReference)}
}

func (f *textFeature) Name() string         { return "text" }
func (f *textFeature) GoldenSource() string { return "pdfjs" }

func (f *textFeature) LoadGoldens(dir string) error {
	return loadPDFJSReferences(dir, f.goldens, &f.mu)
}

func (f *textFeature) HasGolden(fixtureID string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.goldens[baseFixtureID(fixtureID)]
	return ok
}

// textOutput is the blob Run emits: the per-page text litho extracted.
// Compare decodes it and diffs against the golden.
type textOutput struct {
	NumPages int              `json:"num_pages"`
	Pages    []textOutputPage `json:"pages"`
}

type textOutputPage struct {
	PageNumber int    `json:"page_number"`
	Text       string `json:"text,omitempty"`
	Error      string `json:"error,omitempty"`
}

func (f *textFeature) Run(ctx context.Context, pdfPath, fixtureID string) ([]byte, error) {
	if v, ok := f.cache.Load(pdfPath); ok {
		cr := v.(*cachedRun)
		return cr.output, cr.err
	}
	f.mu.RLock()
	golden := f.goldens[baseFixtureID(fixtureID)]
	f.mu.RUnlock()

	pages, numPages, err := extractTextPages(ctx, pdfPath, effectiveLastPage(golden))
	if err != nil {
		f.cache.Store(pdfPath, &cachedRun{err: err})
		return nil, err
	}
	out, err := json.Marshal(textOutput{NumPages: numPages, Pages: pages})
	f.cache.Store(pdfPath, &cachedRun{output: out, err: err})
	return out, err
}

func (f *textFeature) Compare(fixtureID string, output []byte) Verdict {
	f.mu.RLock()
	golden := f.goldens[baseFixtureID(fixtureID)]
	f.mu.RUnlock()

	if golden == nil {
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: "no golden"}
	}

	var got textOutput
	if err := json.Unmarshal(output, &got); err != nil {
		return Verdict{FixtureID: fixtureID, Reason: fmt.Sprintf("decode output: %v", err)}
	}

	charsMatch := true
	wordsMatch := true
	var mismatches []json.RawMessage

	gotByPage := make(map[int]textOutputPage, len(got.Pages))
	for _, p := range got.Pages {
		gotByPage[p.PageNumber] = p
	}

	for _, gp := range golden.Pages {
		if gp.Error != "" {
			continue
		}
		lp, ok := gotByPage[gp.PageNumber]
		if !ok {
			charsMatch = false
			wordsMatch = false
			mismatches = append(mismatches, textMismatchRaw(golden.File, gp.PageNumber, 0, gp.Text, "(no page in litho output)"))
			continue
		}
		if lp.Error != "" {
			if strings.TrimSpace(gp.Text) != "" {
				charsMatch = false
				wordsMatch = false
				mismatches = append(mismatches, textMismatchRaw(golden.File, gp.PageNumber, 0, gp.Text, fmt.Sprintf("(error: %s)", lp.Error)))
			}
			continue
		}
		if !comparePageTextContent(gp.Text, lp.Text) {
			charsMatch = false
		}
		if mm := comparePageText(golden.File, gp.PageNumber, gp.Text, lp.Text); mm != nil {
			wordsMatch = false
			raw, _ := json.Marshal(mm)
			mismatches = append(mismatches, raw)
		}
	}

	score := 0.0
	if wordsMatch {
		score = 1.0
	}
	metrics := map[string]int{}
	if charsMatch {
		metrics["chars_match"] = 1
	}
	if wordsMatch {
		metrics["words_match"] = 1
	}
	return Verdict{
		FixtureID:  fixtureID,
		Match:      wordsMatch,
		Score:      score,
		Metrics:    metrics,
		Mismatches: mismatches,
	}
}

// effectiveLastPage returns the page-count cap the golden requests, or
// zero when unconstrained. PDF.js's test manifest pins some fixtures to
// a prefix of pages; honouring that keeps extraction bounded.
func effectiveLastPage(golden *pdfjsReference) int {
	if golden == nil {
		return 0
	}
	return golden.NumPages
}

// loadPDFJSReferences walks dir for <id>.json files and populates
// goldens under mu. Missing or empty dir returns [ErrNoGoldens].
func loadPDFJSReferences(dir string, goldens map[string]*pdfjsReference, mu *sync.RWMutex) error {
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
		var ref pdfjsReference
		if err := json.Unmarshal(data, &ref); err != nil {
			continue
		}
		id := ref.ID
		if id == "" {
			id = strings.TrimSuffix(e.Name(), ".json")
		}
		mu.Lock()
		goldens[id] = &ref
		mu.Unlock()
		loaded++
	}
	if loaded == 0 {
		return ErrNoGoldens
	}
	return nil
}

// extractTextPages opens pdfPath and returns the per-page text output
// plus the document's page count. When lastPage > 0 only the first N
// pages are extracted (honouring PDF.js's fixture page cap).
func extractTextPages(ctx context.Context, pdfPath string, lastPage int) ([]textOutputPage, int, error) {
	f, err := os.Open(pdfPath)
	if err != nil {
		return nil, 0, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, 0, fmt.Errorf("stat: %w", err)
	}

	r, err := pdf.Open(ctx, f, info.Size(),
		pdf.WithFilterPlugin(jbig2.New()),
		pdf.WithFilterPlugin(jpx.New()),
	)
	if err != nil {
		return nil, 0, fmt.Errorf("pdf.Open: %w", err)
	}

	numPages, err := r.PageCount(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("PageCount: %w", err)
	}
	last := numPages
	if lastPage > 0 && lastPage < last {
		last = lastPage
	}

	pages := make([]textOutputPage, 0, last)
	for i := 1; i <= last; i++ {
		if err := ctx.Err(); err != nil {
			return pages, numPages, err
		}
		page := textOutputPage{PageNumber: i}
		text, err := r.ExtractText(ctx, i,
			pdf.WithInvisibleText(),
			pdf.WithArtifacts(),
		)
		if err != nil {
			page.Error = err.Error()
		} else {
			page.Text = text
		}
		pages = append(pages, page)
	}
	return pages, numPages, nil
}

// textMismatch describes a first-difference between litho and the golden
// on a specific page. Emitted as a JSON blob in Verdict.Mismatches.
type textMismatch struct {
	File     string `json:"file"`
	Page     int    `json:"page"`
	Position int    `json:"position"`
	Expected string `json:"expected"`
	Got      string `json:"got"`
}

func textMismatchRaw(file string, page, pos int, expected, got string) json.RawMessage {
	raw, _ := json.Marshal(textMismatch{
		File:     file,
		Page:     page,
		Position: pos,
		Expected: truncate(expected, 80),
		Got:      got,
	})
	return raw
}

// comparePageText emits a mismatch for the first differing rune between
// litho and the golden after whitespace collapsing.
func comparePageText(file string, pageNum int, goldenText, lithoText string) *textMismatch {
	goldenNorm := normalizeWhitespace(goldenText)
	lithoNorm := normalizeWhitespace(lithoText)
	if lithoNorm == goldenNorm {
		return nil
	}
	pos, expected, got := textDiffSummary(goldenNorm, lithoNorm, 80)
	return &textMismatch{
		File:     file,
		Page:     pageNum,
		Position: pos,
		Expected: expected,
		Got:      got,
	}
}
