package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/gotenberg/litho/pkg/pdf"
	"github.com/gotenberg/litho/pkg/plugins/jbig2"
	"github.com/gotenberg/litho/pkg/plugins/jpx"
)

// splitFeature is a self-consistency canary for [pdf.Split]: every
// input PDF with at least two pages is split into a 2-page output
// containing its first and last page, then the output is checked
// against what litho already saw in the input.
//
// Comparing litho's split-output extraction against litho's
// input-PDF extraction (rather than against PDF.js's golden) keeps
// the test focused on split correctness instead of folding in the
// text-extraction agreement gap. A self-consistent split passes here
// even when litho and PDF.js disagree on the underlying glyph stream.
//
// Output assertions:
//
//   - output page count == 2
//   - output page[0] text == input page[1] text (litho extraction)
//   - output page[1] text == input page[lastPage] text
//   - every output bookmark targets page 0, 1, or 2
//   - every output bookmark title appears somewhere in the input
//     outline tree
//
// The bookmark assertions are invariants rather than exact-tree
// matches, so the test does not couple to the specific orphan policy
// ([pdf.OrphanDrop] / [pdf.PromoteOrphanChildren]) the default
// Split path applies. Fixtures whose input first or last page is
// image-only ([pdf.ErrRequiresOCR]) are skipped: the split may
// have worked but the test has nothing to compare.
type splitFeature struct {
	cache sync.Map // pdfPath -> *cachedRun
}

func newSplitFeature() *splitFeature {
	return &splitFeature{}
}

func (f *splitFeature) Name() string         { return "split" }
func (f *splitFeature) GoldenSource() string { return "litho-self" }

// LoadGoldens is a no-op: the split feature derives the oracle from
// the input PDF at run time and needs no captured reference files.
// The manifest still must declare a non-empty golden path so the
// runner schedules the feature; that path is ignored.
func (f *splitFeature) LoadGoldens(_ string) error { return nil }

// HasGolden returns true for every fixture: every PDF is a candidate
// for a split round-trip. Per-PDF skips (single-page, OCR-only) are
// emitted from [Run] / [Compare].
func (f *splitFeature) HasGolden(_ string) bool { return true }

type splitOutput struct {
	Skipped    bool                 `json:"skipped,omitempty"`
	SkipReason string               `json:"skip_reason,omitempty"`
	InputPages int                  `json:"input_pages,omitempty"`
	NumPages   int                  `json:"num_pages"`
	InputFirst string               `json:"input_first,omitempty"`
	InputLast  string               `json:"input_last,omitempty"`
	OutFirst   string               `json:"out_first,omitempty"`
	OutLast    string               `json:"out_last,omitempty"`
	InputBM    []normalizedBookmark `json:"input_bookmarks,omitempty"`
	OutBM      []normalizedBookmark `json:"out_bookmarks,omitempty"`
}

func (f *splitFeature) Run(ctx context.Context, pdfPath, _ string) ([]byte, error) {
	if v, ok := f.cache.Load(pdfPath); ok {
		cr := v.(*cachedRun)
		return cr.output, cr.err
	}
	out, err := runSplit(ctx, pdfPath)
	if err != nil {
		f.cache.Store(pdfPath, &cachedRun{err: err})
		return nil, err
	}
	blob, err := json.Marshal(out)
	f.cache.Store(pdfPath, &cachedRun{output: blob, err: err})
	return blob, err
}

func runSplit(ctx context.Context, pdfPath string) (splitOutput, error) {
	in, err := os.Open(pdfPath)
	if err != nil {
		return splitOutput{}, fmt.Errorf("open: %w", err)
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return splitOutput{}, fmt.Errorf("stat: %w", err)
	}

	src, err := pdf.Open(ctx, in, info.Size(),
		pdf.WithFilterPlugin(jbig2.New()),
		pdf.WithFilterPlugin(jpx.New()),
	)
	if err != nil {
		return splitOutput{}, fmt.Errorf("pdf.Open: %w", err)
	}
	defer src.Close()

	inputPages, err := src.PageCount(ctx)
	if err != nil {
		return splitOutput{}, fmt.Errorf("PageCount: %w", err)
	}
	if inputPages < 2 {
		return splitOutput{
			Skipped:    true,
			SkipReason: fmt.Sprintf("single-page input (%d page)", inputPages),
			InputPages: inputPages,
		}, nil
	}

	firstText, skipReason, err := extractOrSkip(ctx, src, 1)
	if err != nil {
		return splitOutput{}, fmt.Errorf("input page 1: %w", err)
	}
	if skipReason != "" {
		return splitOutput{Skipped: true, SkipReason: "input page 1: " + skipReason, InputPages: inputPages}, nil
	}
	lastText, skipReason, err := extractOrSkip(ctx, src, inputPages)
	if err != nil {
		return splitOutput{}, fmt.Errorf("input page %d: %w", inputPages, err)
	}
	if skipReason != "" {
		return splitOutput{Skipped: true, SkipReason: fmt.Sprintf("input page %d: %s", inputPages, skipReason), InputPages: inputPages}, nil
	}

	inputBM, err := src.Bookmarks(ctx)
	if err != nil {
		return splitOutput{}, fmt.Errorf("input Bookmarks: %w", err)
	}

	var buf bytes.Buffer
	if err := pdf.Split(ctx, src, []int{1, inputPages}, &buf); err != nil {
		return splitOutput{}, fmt.Errorf("pdf.Split: %w", err)
	}

	out, err := pdf.Open(ctx, bytes.NewReader(buf.Bytes()), int64(buf.Len()),
		pdf.WithFilterPlugin(jbig2.New()),
		pdf.WithFilterPlugin(jpx.New()),
	)
	if err != nil {
		return splitOutput{}, fmt.Errorf("reopen split output: %w", err)
	}
	defer out.Close()

	numPages, err := out.PageCount(ctx)
	if err != nil {
		return splitOutput{}, fmt.Errorf("output PageCount: %w", err)
	}

	result := splitOutput{
		InputPages: inputPages,
		NumPages:   numPages,
		InputFirst: firstText,
		InputLast:  lastText,
		InputBM:    normalizeLithoTree(inputBM),
	}

	if numPages >= 1 {
		text, err := out.ExtractText(ctx, 1, pdf.WithInvisibleText(), pdf.WithArtifacts())
		if err != nil && !errors.Is(err, pdf.ErrRequiresOCR) {
			return splitOutput{}, fmt.Errorf("output page 1: %w", err)
		}
		result.OutFirst = text
	}
	if numPages >= 2 {
		text, err := out.ExtractText(ctx, 2, pdf.WithInvisibleText(), pdf.WithArtifacts())
		if err != nil && !errors.Is(err, pdf.ErrRequiresOCR) {
			return splitOutput{}, fmt.Errorf("output page 2: %w", err)
		}
		result.OutLast = text
	}

	outBM, err := out.Bookmarks(ctx)
	if err != nil {
		return splitOutput{}, fmt.Errorf("output Bookmarks: %w", err)
	}
	result.OutBM = normalizeLithoTree(outBM)

	return result, nil
}

// extractOrSkip pulls a single page's text via litho. When the page is
// image-only the function returns a non-empty skipReason rather than
// an error, letting the caller emit a Skipped Verdict instead of a
// mismatch: there is no digital text to round-trip in either case.
func extractOrSkip(ctx context.Context, r *pdf.Reader, page int) (text string, skipReason string, err error) {
	text, err = r.ExtractText(ctx, page, pdf.WithInvisibleText(), pdf.WithArtifacts())
	if err == nil {
		return text, "", nil
	}
	if errors.Is(err, pdf.ErrRequiresOCR) {
		return "", "image-only page", nil
	}
	return "", "", err
}

func (f *splitFeature) Compare(fixtureID string, output []byte) Verdict {
	var got splitOutput
	if err := json.Unmarshal(output, &got); err != nil {
		return Verdict{FixtureID: fixtureID, Reason: fmt.Sprintf("decode output: %v", err)}
	}
	if got.Skipped {
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: got.SkipReason}
	}

	if got.NumPages != 2 {
		return Verdict{FixtureID: fixtureID, Reason: fmt.Sprintf("page count: got %d, want 2", got.NumPages)}
	}

	if mm := comparePageText(fixtureID, 1, got.InputFirst, got.OutFirst); mm != nil {
		raw, _ := json.Marshal(mm)
		return Verdict{FixtureID: fixtureID, Reason: "page[0] text differs from input page 1", Mismatches: []json.RawMessage{raw}}
	}
	if mm := comparePageText(fixtureID, got.InputPages, got.InputLast, got.OutLast); mm != nil {
		raw, _ := json.Marshal(mm)
		return Verdict{FixtureID: fixtureID, Reason: fmt.Sprintf("page[1] text differs from input page %d", got.InputPages), Mismatches: []json.RawMessage{raw}}
	}

	if reason := checkSplitBookmarks(got.OutBM, got.InputBM); reason != "" {
		return Verdict{FixtureID: fixtureID, Reason: reason}
	}

	return Verdict{FixtureID: fixtureID, Match: true, Score: 1.0}
}

// checkSplitBookmarks enforces the two split-output invariants:
//
//   - every bookmark targets page 0, 1, or 2 (page 0 = "no destination",
//     which producers use for untitled outline parents; 1 and 2 are the
//     surviving pages)
//   - every bookmark title appears somewhere in the input outline tree
//     (no fabricated entries)
//
// Returns "" when both invariants hold; otherwise a short reason
// describing the first violation found in pre-order traversal.
func checkSplitBookmarks(out, in []normalizedBookmark) string {
	titles := collectBookmarkTitles(in, nil)
	return walkSplitBookmarks("bookmarks", out, titles)
}

func walkSplitBookmarks(path string, bookmarks []normalizedBookmark, inputTitles map[string]struct{}) string {
	for i, b := range bookmarks {
		p := fmt.Sprintf("%s[%d]", path, i)
		if b.Page != 0 && b.Page != 1 && b.Page != 2 {
			return fmt.Sprintf("%s.page = %d (only 1 or 2 are valid after split)", p, b.Page)
		}
		if b.Title != "" {
			if _, ok := inputTitles[b.Title]; !ok {
				return fmt.Sprintf("%s.title %q not present in input outline", p, b.Title)
			}
		}
		if reason := walkSplitBookmarks(p+".children", b.Children, inputTitles); reason != "" {
			return reason
		}
	}
	return ""
}

func collectBookmarkTitles(in []normalizedBookmark, set map[string]struct{}) map[string]struct{} {
	if set == nil {
		set = make(map[string]struct{})
	}
	for _, b := range in {
		if b.Title != "" {
			set[b.Title] = struct{}{}
		}
		collectBookmarkTitles(b.Children, set)
	}
	return set
}
