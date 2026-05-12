package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/pdf"
	"github.com/gotenberg/litho/pkg/plugins/jbig2"
	"github.com/gotenberg/litho/pkg/plugins/jpx"
)

// mergeFeature is a canary for [pdf.Merge] using the merge-self
// scenario: each input PDF is merged with itself, then the result is
// validated against two independent oracles:
//
//   - Derived oracle (litho self-consistency). The merge output's
//     per-page text and bookmark leaves must match what the input
//     already shows under [pdf.Reader.ExtractText] and
//     [pdf.Reader.Bookmarks]. Output page count must be exactly twice
//     the input's. Catches gross merge errors: dropped pages, wrong
//     order, fabricated content.
//
//   - pdfcpu cross-check. The pdfcpu CLI's merge output (captured at
//     provisioning time as JSON via `pdfcpu bookmarks export`) is
//     filtered to bookmarks whose titles appear in the input and the
//     resulting (title, page) multiset must match the same expected
//     set. Catches asymmetric bugs in litho's merge that merge-self
//     alone might mask (a bug that applies identically to both copies
//     looks fine in the derived oracle).
//
// Output bookmark filtering is by input-title membership rather than
// page=0 because pdfcpu's default merge wraps each source under a
// per-file container titled with the filename ("a.pdf", "b.pdf") and
// those wrappers do carry page numbers. The filename is never in the
// input outline, so the title filter drops them transparently.
//
// Scope:
//   - merge-self only (one input PDF, merged with itself). Two-input
//     scenarios need a fixture-loading concept the matrix does not yet
//     have.
//   - default [pdf.BookmarkOptions]. Other bookmark handling modes are
//     not in scope.
//   - inputs with > 30 pages are skipped (merge-self doubles the page
//     count and the feature is meant to be a fast canary).
//   - inputs whose first or last page is image-only ([pdf.ErrRequiresOCR])
//     are skipped: nothing to compare in the text dimension.
type mergeFeature struct {
	mu          sync.RWMutex
	pdfcpuGolds map[string][]normalizedBookmark
	goldensDir  string
	cache       sync.Map // pdfPath -> *cachedRun
}

func newMergeFeature() *mergeFeature {
	return &mergeFeature{pdfcpuGolds: make(map[string][]normalizedBookmark)}
}

func (f *mergeFeature) Name() string         { return "merge" }
func (f *mergeFeature) GoldenSource() string { return "litho-self+pdfcpu" }

func (f *mergeFeature) LoadGoldens(dir string) error {
	f.goldensDir = dir
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
		f.pdfcpuGolds[id] = normalizePdfcpuTree(raw.Bookmarks)
		f.mu.Unlock()
		loaded++
	}
	if loaded == 0 {
		return ErrNoGoldens
	}
	return nil
}

func (f *mergeFeature) HasGolden(fixtureID string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.pdfcpuGolds[baseFixtureID(fixtureID)]
	return ok
}

// mergeMaxPages is the page-count ceiling for the input PDF. Merging
// a 100-page document with itself writes 200 pages of output and
// extracts text from all of them; the cap keeps the feature a fast
// canary rather than a stress test.
const mergeMaxPages = 30

type mergeOutput struct {
	Skipped    bool           `json:"skipped,omitempty"`
	SkipReason string         `json:"skip_reason,omitempty"`
	InputPages int            `json:"input_pages,omitempty"`
	OutPages   int            `json:"out_pages"`
	InputText  []string       `json:"input_text,omitempty"`
	OutText    []string       `json:"out_text,omitempty"`
	InputBM    []bookmarkLeaf `json:"input_bm,omitempty"`
	LithoOutBM []bookmarkLeaf `json:"litho_out_bm,omitempty"`
}

// bookmarkLeaf is the (title, page) pair we compare. Page=0 leaves
// are dropped during collection so untitled outline parents do not
// pollute the multiset.
type bookmarkLeaf struct {
	Title string `json:"title"`
	Page  int    `json:"page"`
}

func (f *mergeFeature) Run(ctx context.Context, pdfPath, _ string) ([]byte, error) {
	if v, ok := f.cache.Load(pdfPath); ok {
		cr := v.(*cachedRun)
		return cr.output, cr.err
	}
	out, err := runMerge(ctx, pdfPath)
	if err != nil {
		f.cache.Store(pdfPath, &cachedRun{err: err})
		return nil, err
	}
	blob, err := json.Marshal(out)
	f.cache.Store(pdfPath, &cachedRun{output: blob, err: err})
	return blob, err
}

func runMerge(ctx context.Context, pdfPath string) (mergeOutput, error) {
	in, err := os.Open(pdfPath)
	if err != nil {
		return mergeOutput{}, fmt.Errorf("open: %w", err)
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return mergeOutput{}, fmt.Errorf("stat: %w", err)
	}

	src, err := pdf.Open(ctx, in, info.Size(),
		pdf.WithFilterPlugin(jbig2.New()),
		pdf.WithFilterPlugin(jpx.New()),
	)
	if err != nil {
		return mergeOutput{}, fmt.Errorf("pdf.Open: %w", err)
	}
	defer src.Close()

	inputPages, err := src.PageCount(ctx)
	if err != nil {
		return mergeOutput{}, fmt.Errorf("PageCount: %w", err)
	}
	if inputPages < 1 {
		return mergeOutput{Skipped: true, SkipReason: "empty input"}, nil
	}
	if inputPages > mergeMaxPages {
		return mergeOutput{
			Skipped:    true,
			SkipReason: fmt.Sprintf("input has %d pages (cap %d)", inputPages, mergeMaxPages),
			InputPages: inputPages,
		}, nil
	}

	inputText := make([]string, 0, inputPages)
	for i := 1; i <= inputPages; i++ {
		text, skip, err := extractOrSkip(ctx, src, i)
		if err != nil {
			return mergeOutput{}, fmt.Errorf("input page %d: %w", i, err)
		}
		if skip != "" {
			return mergeOutput{
				Skipped:    true,
				SkipReason: fmt.Sprintf("input page %d: %s", i, skip),
				InputPages: inputPages,
			}, nil
		}
		inputText = append(inputText, text)
	}

	inputBM, err := src.Bookmarks(ctx)
	if err != nil {
		return mergeOutput{}, fmt.Errorf("input Bookmarks: %w", err)
	}
	inputLeaves := collectBookmarkLeaves(normalizeLithoTree(inputBM), nil)

	// Reopen for the second source. pdf.Merge consumes its inputs
	// sequentially; the same Reader can be passed twice but having
	// two independent Readers exercises the resource-table merge path
	// the way two real inputs would.
	in2, err := os.Open(pdfPath)
	if err != nil {
		return mergeOutput{}, fmt.Errorf("reopen for second source: %w", err)
	}
	defer in2.Close()
	src2, err := pdf.Open(ctx, in2, info.Size(),
		pdf.WithFilterPlugin(jbig2.New()),
		pdf.WithFilterPlugin(jpx.New()),
	)
	if err != nil {
		return mergeOutput{}, fmt.Errorf("pdf.Open (second): %w", err)
	}
	defer src2.Close()

	var buf bytes.Buffer
	if err := pdf.Merge(ctx, []*pdf.Reader{src, src2}, &buf); err != nil {
		return mergeOutput{}, fmt.Errorf("pdf.Merge: %w", err)
	}

	out, err := pdf.Open(ctx, bytes.NewReader(buf.Bytes()), int64(buf.Len()),
		pdf.WithFilterPlugin(jbig2.New()),
		pdf.WithFilterPlugin(jpx.New()),
	)
	if err != nil {
		return mergeOutput{}, fmt.Errorf("reopen merge output: %w", err)
	}
	defer out.Close()

	outPages, err := out.PageCount(ctx)
	if err != nil {
		return mergeOutput{}, fmt.Errorf("output PageCount: %w", err)
	}

	outText := make([]string, 0, outPages)
	for i := 1; i <= outPages; i++ {
		text, err := out.ExtractText(ctx, i, pdf.WithInvisibleText(), pdf.WithArtifacts())
		if err != nil && !errors.Is(err, pdf.ErrRequiresOCR) {
			return mergeOutput{}, fmt.Errorf("output page %d: %w", i, err)
		}
		outText = append(outText, text)
	}

	outBM, err := out.Bookmarks(ctx)
	if err != nil {
		return mergeOutput{}, fmt.Errorf("output Bookmarks: %w", err)
	}
	lithoOutLeaves := collectBookmarkLeaves(normalizeLithoTree(outBM), nil)

	return mergeOutput{
		InputPages: inputPages,
		OutPages:   outPages,
		InputText:  inputText,
		OutText:    outText,
		InputBM:    inputLeaves,
		LithoOutBM: lithoOutLeaves,
	}, nil
}

func (f *mergeFeature) Compare(fixtureID string, output []byte) Verdict {
	var got mergeOutput
	if err := json.Unmarshal(output, &got); err != nil {
		return Verdict{FixtureID: fixtureID, Reason: fmt.Sprintf("decode output: %v", err)}
	}
	if got.Skipped {
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: got.SkipReason}
	}

	// Page count.
	want := 2 * got.InputPages
	if got.OutPages != want {
		return Verdict{FixtureID: fixtureID, Reason: fmt.Sprintf("page count: got %d, want %d", got.OutPages, want)}
	}

	// Per-page text. Output page i (1-based) should match input page
	// (((i-1) mod N) + 1).
	for i := 0; i < got.OutPages; i++ {
		want := got.InputText[i%got.InputPages]
		if mm := comparePageText(fixtureID, i+1, want, got.OutText[i]); mm != nil {
			raw, _ := json.Marshal(mm)
			return Verdict{
				FixtureID:  fixtureID,
				Reason:     fmt.Sprintf("output page %d text differs from input page %d", i+1, (i%got.InputPages)+1),
				Mismatches: []json.RawMessage{raw},
			}
		}
	}

	// Bookmark multiset, litho side.
	expected := expectedMergeLeaves(got.InputBM, got.InputPages)
	inputTitles := titleSet(got.InputBM)
	lithoFiltered := filterByTitle(got.LithoOutBM, inputTitles)
	if reason := compareLeafMultisets("litho merge bookmarks", lithoFiltered, expected); reason != "" {
		return Verdict{FixtureID: fixtureID, Reason: reason}
	}

	// Bookmark multiset, pdfcpu side. The pdfcpu golden is loaded
	// during LoadGoldens. We collect its leaves with the same input-
	// title filter so pdfcpu's per-source wrapper containers
	// ("a.pdf", "b.pdf") get dropped.
	f.mu.RLock()
	pdfcpuTree, ok := f.pdfcpuGolds[baseFixtureID(fixtureID)]
	f.mu.RUnlock()
	if !ok {
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: "no pdfcpu merge golden"}
	}
	pdfcpuLeaves := collectBookmarkLeaves(pdfcpuTree, nil)
	pdfcpuFiltered := filterByTitle(pdfcpuLeaves, inputTitles)
	if reason := compareLeafMultisets("pdfcpu merge bookmarks", pdfcpuFiltered, expected); reason != "" {
		return Verdict{FixtureID: fixtureID, Reason: reason}
	}

	return Verdict{FixtureID: fixtureID, Match: true, Score: 1.0}
}

// collectBookmarkLeaves flattens a normalized outline tree into a
// list of (title, page) pairs. Empty titles and page=0 entries are
// skipped because they have no merge-relevant identity. The result
// is order-preserving (pre-order traversal) but multiset comparison
// later sorts before diffing.
func collectBookmarkLeaves(tree []normalizedBookmark, out []bookmarkLeaf) []bookmarkLeaf {
	for _, b := range tree {
		if b.Title != "" && b.Page > 0 {
			out = append(out, bookmarkLeaf{Title: b.Title, Page: b.Page})
		}
		out = collectBookmarkLeaves(b.Children, out)
	}
	return out
}

// expectedMergeLeaves builds the multiset the merge output should
// produce: each input leaf twice, once at its original page and once
// shifted by N to account for the second copy of the document.
func expectedMergeLeaves(input []bookmarkLeaf, n int) []bookmarkLeaf {
	out := make([]bookmarkLeaf, 0, len(input)*2)
	for _, b := range input {
		out = append(out, b)
		out = append(out, bookmarkLeaf{Title: b.Title, Page: b.Page + n})
	}
	return out
}

// titleSet returns the set of normalized titles present in a leaf
// list. Used to filter merge output leaves so producer-wrapper
// containers (pdfcpu's "a.pdf"/"b.pdf") do not pollute the comparison.
func titleSet(leaves []bookmarkLeaf) map[string]struct{} {
	out := make(map[string]struct{}, len(leaves))
	for _, b := range leaves {
		out[b.Title] = struct{}{}
	}
	return out
}

func filterByTitle(leaves []bookmarkLeaf, titles map[string]struct{}) []bookmarkLeaf {
	out := leaves[:0:0]
	for _, b := range leaves {
		if _, ok := titles[b.Title]; ok {
			out = append(out, b)
		}
	}
	return out
}

// compareLeafMultisets returns "" when got and want contain the same
// (title, page) pairs ignoring order, or a short reason describing
// the first divergence found. Used by both the litho and pdfcpu
// bookmark assertions so the failure messaging is uniform.
func compareLeafMultisets(side string, got, want []bookmarkLeaf) string {
	countGot := make(map[bookmarkLeaf]int, len(got))
	countWant := make(map[bookmarkLeaf]int, len(want))
	for _, b := range got {
		countGot[b]++
	}
	for _, b := range want {
		countWant[b]++
	}

	missing := make([]bookmarkLeaf, 0)
	for b, n := range countWant {
		if countGot[b] < n {
			missing = append(missing, b)
		}
	}
	extra := make([]bookmarkLeaf, 0)
	for b, n := range countGot {
		if countWant[b] < n {
			extra = append(extra, b)
		}
	}

	if len(missing) == 0 && len(extra) == 0 {
		return ""
	}
	sort.Slice(missing, func(i, j int) bool {
		if missing[i].Title != missing[j].Title {
			return missing[i].Title < missing[j].Title
		}
		return missing[i].Page < missing[j].Page
	})
	sort.Slice(extra, func(i, j int) bool {
		if extra[i].Title != extra[j].Title {
			return extra[i].Title < extra[j].Title
		}
		return extra[i].Page < extra[j].Page
	})

	parts := make([]string, 0, 2)
	if len(missing) > 0 {
		parts = append(parts, fmt.Sprintf("missing %d (e.g. %+v)", len(missing), missing[0]))
	}
	if len(extra) > 0 {
		parts = append(parts, fmt.Sprintf("extra %d (e.g. %+v)", len(extra), extra[0]))
	}
	return fmt.Sprintf("%s: %s", side, strings.Join(parts, "; "))
}
