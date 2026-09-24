package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestCollectBookmarkLeaves: empty title or page=0 entries are
// dropped; nested children are flattened in pre-order.
func TestCollectBookmarkLeaves(t *testing.T) {
	t.Parallel()
	tree := []normalizedBookmark{
		{Title: "Intro", Page: 1, Children: []normalizedBookmark{
			{Title: "", Page: 0}, // skipped: empty title
			{Title: "Setup", Page: 2},
		}},
		{Title: "End", Page: 0}, // skipped: page=0
		{Title: "Refs", Page: 5},
	}
	got := collectBookmarkLeaves(tree, nil)
	want := []bookmarkLeaf{
		{Title: "Intro", Page: 1},
		{Title: "Setup", Page: 2},
		{Title: "Refs", Page: 5},
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%+v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestExpectedMergeLeaves doubles each input leaf, original page
// plus page+N, to model what the merge-self output should contain.
func TestExpectedMergeLeaves(t *testing.T) {
	t.Parallel()
	in := []bookmarkLeaf{{Title: "Intro", Page: 1}, {Title: "End", Page: 3}}
	got := expectedMergeLeaves(in, 5)
	want := []bookmarkLeaf{
		{Title: "Intro", Page: 1},
		{Title: "Intro", Page: 6},
		{Title: "End", Page: 3},
		{Title: "End", Page: 8},
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestFilterByTitle drops entries whose titles do not appear in the
// allow-set. Exercises the wrapper-stripping path: pdfcpu's "a.pdf"
// container does not appear in the input outline, so the filter
// removes it from the comparison.
func TestFilterByTitle(t *testing.T) {
	t.Parallel()
	leaves := []bookmarkLeaf{
		{Title: "Intro", Page: 1},
		{Title: "a.pdf", Page: 1}, // pdfcpu wrapper
		{Title: "End", Page: 3},
		{Title: "b.pdf", Page: 4}, // pdfcpu wrapper
	}
	titles := map[string]struct{}{"Intro": {}, "End": {}}
	got := filterByTitle(leaves, titles)
	want := []bookmarkLeaf{{Title: "Intro", Page: 1}, {Title: "End", Page: 3}}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%+v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestCompareLeafMultisets: equal multisets compare clean; missing
// or extra entries surface as reasons with side-name prefixes.
func TestCompareLeafMultisets(t *testing.T) {
	t.Parallel()
	a := []bookmarkLeaf{{Title: "X", Page: 1}, {Title: "Y", Page: 2}}
	b := []bookmarkLeaf{{Title: "Y", Page: 2}, {Title: "X", Page: 1}}
	if reason := compareLeafMultisets("side", a, b); reason != "" {
		t.Errorf("equal: got %q, want empty", reason)
	}

	// Missing entry.
	a = []bookmarkLeaf{{Title: "X", Page: 1}}
	b = []bookmarkLeaf{{Title: "X", Page: 1}, {Title: "Y", Page: 2}}
	r := compareLeafMultisets("side", a, b)
	if !strings.Contains(r, "missing 1") {
		t.Errorf("missing: got %q", r)
	}
	if !strings.Contains(r, "side:") {
		t.Errorf("side prefix missing: %q", r)
	}

	// Extra entry.
	a = []bookmarkLeaf{{Title: "X", Page: 1}, {Title: "Z", Page: 9}}
	b = []bookmarkLeaf{{Title: "X", Page: 1}}
	r = compareLeafMultisets("side", a, b)
	if !strings.Contains(r, "extra 1") {
		t.Errorf("extra: got %q", r)
	}

	// Duplicates differentiated.
	a = []bookmarkLeaf{{Title: "X", Page: 1}, {Title: "X", Page: 1}}
	b = []bookmarkLeaf{{Title: "X", Page: 1}}
	r = compareLeafMultisets("side", a, b)
	if !strings.Contains(r, "extra 1") {
		t.Errorf("multiset count: got %q", r)
	}
}

// TestMergeFeature_Compare_SkippedPropagates: Run's skip becomes a
// Skipped verdict in Compare without falling through to assertions.
func TestMergeFeature_Compare_SkippedPropagates(t *testing.T) {
	t.Parallel()
	f := newMergeFeature()
	out, _ := json.Marshal(mergeOutput{Skipped: true, SkipReason: "too many pages"})
	v := f.Compare("doc", out)
	if !v.Skipped {
		t.Errorf("Verdict: %+v, want Skipped", v)
	}
}

// TestMergeFeature_Compare_PageCountWrong rejects outputs whose
// total page count is not 2 × input.
func TestMergeFeature_Compare_PageCountWrong(t *testing.T) {
	t.Parallel()
	f := newMergeFeature()
	out, _ := json.Marshal(mergeOutput{InputPages: 3, OutPages: 5})
	v := f.Compare("doc", out)
	if v.Match {
		t.Fatalf("expected mismatch, got %+v", v)
	}
	if !strings.Contains(v.Reason, "page count: got 5, want 6") {
		t.Errorf("reason: %q", v.Reason)
	}
}

// TestMergeFeature_Compare_HappyPath: page count, per-page text, and
// both bookmark multisets agree with the input-derived oracle.
func TestMergeFeature_Compare_HappyPath(t *testing.T) {
	t.Parallel()
	f := newMergeFeature()
	f.pdfcpuGolds["doc"] = []normalizedBookmark{
		// pdfcpu adds a wrapper. The wrapper is filtered out by title.
		{Title: "doc.pdf", Page: 1, Children: []normalizedBookmark{
			{Title: "Intro", Page: 1},
			{Title: "End", Page: 2},
		}},
		{Title: "doc.pdf", Page: 3, Children: []normalizedBookmark{
			{Title: "Intro", Page: 3},
			{Title: "End", Page: 4},
		}},
	}
	out, _ := json.Marshal(mergeOutput{
		InputPages: 2,
		OutPages:   4,
		InputText:  []string{"A", "B"},
		OutText:    []string{"A", "B", "A", "B"},
		InputBM: []bookmarkLeaf{
			{Title: "Intro", Page: 1},
			{Title: "End", Page: 2},
		},
		LithoOutBM: []bookmarkLeaf{
			{Title: "Intro", Page: 1},
			{Title: "End", Page: 2},
			{Title: "Intro", Page: 3},
			{Title: "End", Page: 4},
		},
	})
	v := f.Compare("doc", out)
	if !v.Match {
		t.Fatalf("expected match, got %+v", v)
	}
}

// TestMergeFeature_Compare_LithoBookmarkDropped: litho's merge
// silently drops one of the bookmark copies. Surface the missing
// pair on the litho side.
func TestMergeFeature_Compare_LithoBookmarkDropped(t *testing.T) {
	t.Parallel()
	f := newMergeFeature()
	f.pdfcpuGolds["doc"] = []normalizedBookmark{
		{Title: "Intro", Page: 1}, {Title: "Intro", Page: 3},
	}
	out, _ := json.Marshal(mergeOutput{
		InputPages: 2,
		OutPages:   4,
		InputText:  []string{"A", "B"},
		OutText:    []string{"A", "B", "A", "B"},
		InputBM:    []bookmarkLeaf{{Title: "Intro", Page: 1}},
		LithoOutBM: []bookmarkLeaf{{Title: "Intro", Page: 1}}, // missing the +N copy
	})
	v := f.Compare("doc", out)
	if v.Match {
		t.Fatalf("expected mismatch, got %+v", v)
	}
	if !strings.Contains(v.Reason, "litho merge bookmarks") || !strings.Contains(v.Reason, "missing") {
		t.Errorf("reason: %q", v.Reason)
	}
}

// TestMergeFeature_Compare_NoPdfcpuGolden: missing pdfcpu golden
// surfaces as Skipped rather than mismatch, so unprovisioned corpora
// do not artificially fail.
func TestMergeFeature_Compare_NoPdfcpuGolden(t *testing.T) {
	t.Parallel()
	f := newMergeFeature()
	out, _ := json.Marshal(mergeOutput{
		InputPages: 1,
		OutPages:   2,
		InputText:  []string{"A"},
		OutText:    []string{"A", "A"},
	})
	v := f.Compare("missing", out)
	if !v.Skipped {
		t.Errorf("Verdict: %+v, want Skipped", v)
	}
}
