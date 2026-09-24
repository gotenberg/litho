package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/pdf"
)

// mergeEmptyFeature is the two-input companion to [mergeFeature].
// Each input PDF is merged with a known one-page partner
// (pkg_testdata__empty.pdf, also part of the pdfcpu corpus) so the
// merge engine receives genuinely different sources: the inputs bring
// distinct resource tables, distinct outline trees, and distinct page
// content streams. merge-self can hide bugs that show up only when
// two real documents have to coexist (resource-name conflicts on
// different fonts, different /Catalog entries, etc.).
//
// Assertions per fixture:
//
//   - Output page count == input.N + 1.
//   - Output page[i].text == input page[i].text for i in [0, N): the
//     first N pages of the merge result must be byte-equal text to the
//     input as litho already saw it.
//   - Output page[N].text matches the empty partner's extracted text
//     (allowed to be empty).
//   - Output bookmark leaves match the input's leaves with NO page
//     offset (the second source is empty and contributes no
//     destinations; pdfcpu's per-file wrapper drops out via the
//     title filter [mergeFeature] already uses).
//   - pdfcpu's captured merge output yields the same filtered leaf
//     multiset as litho's.
//
// Skipped fixtures: the empty.pdf fixture itself (degenerate
// self-merge), inputs with more than [mergeMaxPages], inputs whose
// first or last page is image-only.
type mergeEmptyFeature struct {
	mu          sync.RWMutex
	pdfcpuGolds map[string][]normalizedBookmark
	emptyPath   string
	cache       sync.Map // pdfPath -> *cachedRun
}

func newMergeEmptyFeature() *mergeEmptyFeature {
	return &mergeEmptyFeature{pdfcpuGolds: make(map[string][]normalizedBookmark)}
}

func (f *mergeEmptyFeature) Name() string         { return "merge-empty" }
func (f *mergeEmptyFeature) GoldenSource() string { return "litho-self+pdfcpu" }

// emptyPartnerBasename is the fixture id of the partner PDF the
// feature merges every other fixture with. It must exist in the
// corpus's pdfs/ directory; without it, the feature emits
// [ErrNoGoldens] so the runner shows [no golden].
const emptyPartnerBasename = "pkg_testdata__empty"

func (f *mergeEmptyFeature) LoadGoldens(dir string) error {
	// Resolve the empty.pdf partner under the corpus's pdfs/ tree.
	// `dir` is something like /...testdata/pdfcpu/goldens/merge-empty;
	// the pdfs/ directory is one level up from goldens.
	testdataRoot := filepath.Dir(filepath.Dir(dir))
	candidate := filepath.Join(testdataRoot, "pdfs", emptyPartnerBasename+".pdf")
	if _, err := os.Stat(candidate); err != nil {
		return ErrNoGoldens
	}
	f.emptyPath = candidate

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

func (f *mergeEmptyFeature) HasGolden(fixtureID string) bool {
	base := baseFixtureID(fixtureID)
	if base == emptyPartnerBasename {
		// Merging empty.pdf with itself is degenerate; merge-self
		// already covers the same-input scenario.
		return false
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.pdfcpuGolds[base]
	return ok
}

type mergeEmptyOutput struct {
	Skipped     bool           `json:"skipped,omitempty"`
	SkipReason  string         `json:"skip_reason,omitempty"`
	InputPages  int            `json:"input_pages,omitempty"`
	OutPages    int            `json:"out_pages"`
	InputText   []string       `json:"input_text,omitempty"`
	OutText     []string       `json:"out_text,omitempty"`
	PartnerText string         `json:"partner_text,omitempty"`
	InputBM     []bookmarkLeaf `json:"input_bm,omitempty"`
	LithoOutBM  []bookmarkLeaf `json:"litho_out_bm,omitempty"`
}

func (f *mergeEmptyFeature) Run(ctx context.Context, pdfPath, _ string) ([]byte, error) {
	if v, ok := f.cache.Load(pdfPath); ok {
		cr := v.(*cachedRun)
		return cr.output, cr.err
	}
	out, err := f.runMergeEmpty(ctx, pdfPath)
	if err != nil {
		f.cache.Store(pdfPath, &cachedRun{err: err})
		return nil, err
	}
	blob, err := json.Marshal(out)
	f.cache.Store(pdfPath, &cachedRun{output: blob, err: err})
	return blob, err
}

func (f *mergeEmptyFeature) runMergeEmpty(ctx context.Context, pdfPath string) (mergeEmptyOutput, error) {
	in, err := os.Open(pdfPath)
	if err != nil {
		return mergeEmptyOutput{}, fmt.Errorf("open: %w", err)
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return mergeEmptyOutput{}, fmt.Errorf("stat: %w", err)
	}
	src, err := pdf.Open(ctx, in, info.Size())
	if err != nil {
		return mergeEmptyOutput{}, fmt.Errorf("pdf.Open: %w", err)
	}
	defer src.Close()

	inputPages, err := src.PageCount(ctx)
	if err != nil {
		return mergeEmptyOutput{}, fmt.Errorf("PageCount: %w", err)
	}
	if inputPages < 1 {
		return mergeEmptyOutput{Skipped: true, SkipReason: "empty input"}, nil
	}
	if inputPages > mergeMaxPages {
		return mergeEmptyOutput{
			Skipped:    true,
			SkipReason: fmt.Sprintf("input has %d pages (cap %d)", inputPages, mergeMaxPages),
			InputPages: inputPages,
		}, nil
	}

	inputText := make([]string, 0, inputPages)
	for i := 1; i <= inputPages; i++ {
		text, skip, err := extractOrSkip(ctx, src, i)
		if err != nil {
			return mergeEmptyOutput{}, fmt.Errorf("input page %d: %w", i, err)
		}
		if skip != "" {
			return mergeEmptyOutput{
				Skipped:    true,
				SkipReason: fmt.Sprintf("input page %d: %s", i, skip),
				InputPages: inputPages,
			}, nil
		}
		inputText = append(inputText, text)
	}

	inputBM, err := src.Bookmarks(ctx)
	if err != nil {
		return mergeEmptyOutput{}, fmt.Errorf("input Bookmarks: %w", err)
	}
	inputLeaves := collectBookmarkLeaves(normalizeLithoTree(inputBM), nil)

	// Partner: empty.pdf. Open separately and extract its page text
	// so the Compare can match output[N] against it.
	partner, err := os.Open(f.emptyPath)
	if err != nil {
		return mergeEmptyOutput{}, fmt.Errorf("open partner: %w", err)
	}
	defer partner.Close()
	pInfo, err := partner.Stat()
	if err != nil {
		return mergeEmptyOutput{}, fmt.Errorf("stat partner: %w", err)
	}
	partnerR, err := pdf.Open(ctx, partner, pInfo.Size())
	if err != nil {
		return mergeEmptyOutput{}, fmt.Errorf("pdf.Open (partner): %w", err)
	}
	defer partnerR.Close()
	partnerPages, err := partnerR.PageCount(ctx)
	if err != nil {
		return mergeEmptyOutput{}, fmt.Errorf("partner PageCount: %w", err)
	}
	if partnerPages != 1 {
		return mergeEmptyOutput{}, fmt.Errorf("partner pages: got %d, want 1", partnerPages)
	}
	partnerText, _, err := extractOrSkip(ctx, partnerR, 1)
	if err != nil {
		return mergeEmptyOutput{}, fmt.Errorf("partner page 1: %w", err)
	}

	var buf bytes.Buffer
	if err := pdf.Merge(ctx, []*pdf.Reader{src, partnerR}, &buf); err != nil {
		return mergeEmptyOutput{}, fmt.Errorf("pdf.Merge: %w", err)
	}

	out, err := pdf.Open(ctx, bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		return mergeEmptyOutput{}, fmt.Errorf("reopen merge output: %w", err)
	}
	defer out.Close()

	outPages, err := out.PageCount(ctx)
	if err != nil {
		return mergeEmptyOutput{}, fmt.Errorf("output PageCount: %w", err)
	}
	outText := make([]string, 0, outPages)
	for i := 1; i <= outPages; i++ {
		text, err := out.ExtractText(ctx, i, pdf.WithInvisibleText(), pdf.WithClipPathText(), pdf.WithArtifacts())
		if err != nil && !errors.Is(err, pdf.ErrRequiresOCR) {
			return mergeEmptyOutput{}, fmt.Errorf("output page %d: %w", i, err)
		}
		outText = append(outText, text)
	}

	outBM, err := out.Bookmarks(ctx)
	if err != nil {
		return mergeEmptyOutput{}, fmt.Errorf("output Bookmarks: %w", err)
	}
	lithoOutLeaves := collectBookmarkLeaves(normalizeLithoTree(outBM), nil)

	return mergeEmptyOutput{
		InputPages:  inputPages,
		OutPages:    outPages,
		InputText:   inputText,
		OutText:     outText,
		PartnerText: partnerText,
		InputBM:     inputLeaves,
		LithoOutBM:  lithoOutLeaves,
	}, nil
}

func (f *mergeEmptyFeature) Compare(fixtureID string, output []byte) Verdict {
	var got mergeEmptyOutput
	if err := json.Unmarshal(output, &got); err != nil {
		return Verdict{FixtureID: fixtureID, Reason: fmt.Sprintf("decode output: %v", err)}
	}
	if got.Skipped {
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: got.SkipReason}
	}

	want := got.InputPages + 1
	if got.OutPages != want {
		return Verdict{FixtureID: fixtureID, Reason: fmt.Sprintf("page count: got %d, want %d", got.OutPages, want)}
	}

	for i := 0; i < got.InputPages; i++ {
		if mm := comparePageText(fixtureID, i+1, got.InputText[i], got.OutText[i]); mm != nil {
			raw, _ := json.Marshal(mm)
			return Verdict{
				FixtureID:  fixtureID,
				Reason:     fmt.Sprintf("output page %d text differs from input page %d", i+1, i+1),
				Mismatches: []json.RawMessage{raw},
			}
		}
	}
	// Final page: empty.pdf's content.
	if mm := comparePageText(fixtureID, got.OutPages, got.PartnerText, got.OutText[got.OutPages-1]); mm != nil {
		raw, _ := json.Marshal(mm)
		return Verdict{
			FixtureID:  fixtureID,
			Reason:     fmt.Sprintf("output page %d text differs from empty partner", got.OutPages),
			Mismatches: []json.RawMessage{raw},
		}
	}

	// Bookmark leaves: same multiset as input (no page offset; empty
	// contributes no leaves).
	inputTitles := titleSet(got.InputBM)
	lithoFiltered := filterByTitle(got.LithoOutBM, inputTitles)
	if reason := compareLeafMultisets("litho merge-empty bookmarks", lithoFiltered, got.InputBM); reason != "" {
		return Verdict{FixtureID: fixtureID, Reason: reason}
	}

	f.mu.RLock()
	pdfcpuTree, ok := f.pdfcpuGolds[baseFixtureID(fixtureID)]
	f.mu.RUnlock()
	if !ok {
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: "no pdfcpu merge-empty golden"}
	}
	pdfcpuLeaves := collectBookmarkLeaves(pdfcpuTree, nil)
	pdfcpuFiltered := filterByTitle(pdfcpuLeaves, inputTitles)
	if reason := compareLeafMultisets("pdfcpu merge-empty bookmarks", pdfcpuFiltered, got.InputBM); reason != "" {
		return Verdict{FixtureID: fixtureID, Reason: reason}
	}

	return Verdict{FixtureID: fixtureID, Match: true, Score: 1.0}
}
