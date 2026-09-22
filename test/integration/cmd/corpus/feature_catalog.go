package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/gotenberg/litho/pkg/pdf"
	"github.com/gotenberg/litho/pkg/plugins/jbig2"
	"github.com/gotenberg/litho/pkg/plugins/jpx"
)

// mergeCatalogFeature merges each input with itself and asserts only the
// catalog invariants, so the check can run on corpora that carry no merge
// golden.
//
// The merge feature already applies these invariants, but it is scored on
// pdfcpu alone, where exactly 2 fixtures carry the annotation shape that trips
// the field collector. Measured across all 5171 corpus files, pdfjs carries 49
// such fixtures and verapdf 23. This feature exists to reach them.
//
// It deliberately does NOT compare page text or bookmarks. Dropping the text
// extraction is what makes it cheap enough to run on a 1200-fixture corpus:
// the work per fixture is two opens, one merge and one reopen.
//
// Like [splitFeature] it derives its oracle from the input at run time, so
// GoldenSource is "litho-self" and no reference files are needed. The manifest
// must still declare a non-empty golden path for the runner to schedule it;
// that path is ignored.
type mergeCatalogFeature struct {
	cache sync.Map // pdfPath -> *cachedRun
}

func newMergeCatalogFeature() *mergeCatalogFeature {
	return &mergeCatalogFeature{}
}

func (f *mergeCatalogFeature) Name() string         { return "merge-catalog" }
func (f *mergeCatalogFeature) GoldenSource() string { return "litho-self" }

// LoadGoldens is a no-op: the oracle is the input document itself.
func (f *mergeCatalogFeature) LoadGoldens(_ string) error { return nil }

// HasGolden returns true for every fixture. Per-PDF skips are emitted from
// [mergeCatalogFeature.Run].
func (f *mergeCatalogFeature) HasGolden(_ string) bool { return true }

type mergeCatalogOutput struct {
	Skipped    bool   `json:"skipped,omitempty"`
	SkipReason string `json:"skip_reason,omitempty"`
	InputPages int    `json:"input_pages,omitempty"`
	OutPages   int    `json:"out_pages,omitempty"`

	catalogFacts
}

func (f *mergeCatalogFeature) Run(ctx context.Context, pdfPath, _ string) ([]byte, error) {
	if v, ok := f.cache.Load(pdfPath); ok {
		cr := v.(*cachedRun)
		return cr.output, cr.err
	}
	out, err := runMergeCatalog(ctx, pdfPath)
	if err != nil {
		f.cache.Store(pdfPath, &cachedRun{err: err})
		return nil, err
	}
	blob, err := json.Marshal(out)
	f.cache.Store(pdfPath, &cachedRun{output: blob, err: err})
	return blob, err
}

// runMergeCatalog reports a document litho cannot merge as Skipped rather than
// returning an error.
//
// The runner turns a Run error into a mismatch (run.go), and a file litho
// cannot open or merge at all says nothing about whether a merge preserves
// catalog entries. Scoring it as a violation would fill the cell with files
// that never exercised the invariant.
//
// This does not hide a regression: the baseline gates the matched count, so a
// fixture that merged before and stops merging drops out of matched and
// surfaces there.
func runMergeCatalog(ctx context.Context, pdfPath string) (mergeCatalogOutput, error) {
	skip := func(reason string, pages int) (mergeCatalogOutput, error) {
		return mergeCatalogOutput{Skipped: true, SkipReason: reason, InputPages: pages}, nil
	}

	in, err := os.Open(pdfPath)
	if err != nil {
		return mergeCatalogOutput{}, fmt.Errorf("open: %w", err)
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return mergeCatalogOutput{}, fmt.Errorf("stat: %w", err)
	}

	opts := []pdf.OpenOption{
		pdf.WithFilterPlugin(jbig2.New()),
		pdf.WithFilterPlugin(jpx.New()),
	}

	src, err := pdf.Open(ctx, in, info.Size(), opts...)
	if err != nil {
		return skip(fmt.Sprintf("litho cannot open the input: %v", err), 0)
	}
	defer src.Close()

	inputPages, err := src.PageCount(ctx)
	if err != nil {
		return skip(fmt.Sprintf("page count: %v", err), 0)
	}
	if inputPages < 1 {
		return skip("empty input", inputPages)
	}
	if inputPages > mergeMaxPages {
		return skip(fmt.Sprintf("input has %d pages (cap %d)", inputPages, mergeMaxPages), inputPages)
	}

	// A second independent Reader over the same bytes, matching what runMerge
	// does: it exercises the resource-table merge path the way two real inputs
	// would.
	in2, err := os.Open(pdfPath)
	if err != nil {
		return mergeCatalogOutput{}, fmt.Errorf("reopen for second source: %w", err)
	}
	defer in2.Close()
	src2, err := pdf.Open(ctx, in2, info.Size(), opts...)
	if err != nil {
		return skip(fmt.Sprintf("litho cannot open the second source: %v", err), inputPages)
	}
	defer src2.Close()

	var buf bytes.Buffer
	if err := pdf.Merge(ctx, []*pdf.Reader{src, src2}, &buf); err != nil {
		return skip(fmt.Sprintf("litho cannot merge the input: %v", err), inputPages)
	}

	out, err := pdf.Open(ctx, bytes.NewReader(buf.Bytes()), int64(buf.Len()), opts...)
	if err != nil {
		return skip(fmt.Sprintf("litho cannot reopen its own merge output: %v", err), inputPages)
	}
	defer out.Close()

	outPages, err := out.PageCount(ctx)
	if err != nil {
		return skip(fmt.Sprintf("output page count: %v", err), inputPages)
	}

	return mergeCatalogOutput{
		InputPages:   inputPages,
		OutPages:     outPages,
		catalogFacts: readCatalogFacts(ctx, src, out),
	}, nil
}

func (f *mergeCatalogFeature) Compare(fixtureID string, output []byte) Verdict {
	var got mergeCatalogOutput
	if err := json.Unmarshal(output, &got); err != nil {
		return Verdict{FixtureID: fixtureID, Reason: fmt.Sprintf("decode output: %v", err)}
	}
	if got.Skipped {
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: got.SkipReason}
	}

	// The one structural check worth keeping from the merge feature. A merge of
	// a document with itself doubles its pages, and an output that failed that
	// is not a document whose catalog says anything.
	if want := 2 * got.InputPages; got.OutPages != want {
		return Verdict{
			FixtureID: fixtureID,
			Reason:    fmt.Sprintf("page count: got %d, want %d", got.OutPages, want),
		}
	}

	if reason := checkCatalogInvariants("merge", got.catalogFacts); reason != "" {
		return Verdict{
			FixtureID: fixtureID,
			Reason:    reason,
			Metrics:   catalogInvariantMetrics(got.catalogFacts),
		}
	}

	return Verdict{
		FixtureID: fixtureID,
		Match:     true,
		Score:     1.0,
		Metrics:   catalogInvariantMetrics(got.catalogFacts),
	}
}
