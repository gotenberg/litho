package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestMergeEmptyFeature_HasGolden_SkipsEmptyFixture: merging empty.pdf
// with itself is degenerate; the feature must refuse to run that
// fixture so merge-self stays the only feature touching it.
func TestMergeEmptyFeature_HasGolden_SkipsEmptyFixture(t *testing.T) {
	t.Parallel()
	f := newMergeEmptyFeature()
	f.pdfcpuGolds[emptyPartnerBasename] = []normalizedBookmark{}
	f.pdfcpuGolds["pkg_testdata__test"] = []normalizedBookmark{}
	if f.HasGolden(emptyPartnerBasename) {
		t.Errorf("HasGolden(%q): want false (empty partner itself)", emptyPartnerBasename)
	}
	if !f.HasGolden("pkg_testdata__test") {
		t.Errorf("HasGolden(test): want true")
	}
}

// TestMergeEmptyFeature_Compare_SkippedPropagates: a Skipped output
// from Run becomes a Skipped verdict in Compare.
func TestMergeEmptyFeature_Compare_SkippedPropagates(t *testing.T) {
	t.Parallel()
	f := newMergeEmptyFeature()
	out, _ := json.Marshal(mergeEmptyOutput{Skipped: true, SkipReason: "input has 42 pages (cap 30)"})
	v := f.Compare("doc", out)
	if !v.Skipped {
		t.Errorf("Verdict: %+v, want Skipped", v)
	}
}

// TestMergeEmptyFeature_Compare_PageCountWrong rejects outputs whose
// total page count is not input.N + 1.
func TestMergeEmptyFeature_Compare_PageCountWrong(t *testing.T) {
	t.Parallel()
	f := newMergeEmptyFeature()
	out, _ := json.Marshal(mergeEmptyOutput{InputPages: 3, OutPages: 5})
	v := f.Compare("doc", out)
	if v.Match {
		t.Fatalf("expected mismatch, got %+v", v)
	}
	if !strings.Contains(v.Reason, "page count: got 5, want 4") {
		t.Errorf("reason: %q", v.Reason)
	}
}

// TestMergeEmptyFeature_Compare_HappyPath: page count, per-page
// text (including the empty partner at the tail), and both bookmark
// multisets agree with the input-derived oracle (no page offset for
// merge-empty, unlike merge-self).
func TestMergeEmptyFeature_Compare_HappyPath(t *testing.T) {
	t.Parallel()
	f := newMergeEmptyFeature()
	f.pdfcpuGolds["doc"] = []normalizedBookmark{
		// pdfcpu wraps each input. The wrapper titles are filenames
		// and get dropped by the title filter; only "Intro" and "End"
		// (the input's real titles) survive at their original pages.
		{Title: "doc.pdf", Page: 1, Children: []normalizedBookmark{
			{Title: "Intro", Page: 1},
			{Title: "End", Page: 2},
		}},
		{Title: "empty.pdf", Page: 3},
	}
	out, _ := json.Marshal(mergeEmptyOutput{
		InputPages:  2,
		OutPages:    3,
		InputText:   []string{"A", "B"},
		OutText:     []string{"A", "B", ""},
		PartnerText: "",
		InputBM: []bookmarkLeaf{
			{Title: "Intro", Page: 1},
			{Title: "End", Page: 2},
		},
		LithoOutBM: []bookmarkLeaf{
			{Title: "Intro", Page: 1},
			{Title: "End", Page: 2},
		},
	})
	v := f.Compare("doc", out)
	if !v.Match {
		t.Fatalf("expected match, got %+v", v)
	}
}

// TestMergeEmptyFeature_Compare_PartnerTextMismatch: the trailing
// page does not match the empty partner's text. Surface page N+1
// in the reason so debugging is fast.
func TestMergeEmptyFeature_Compare_PartnerTextMismatch(t *testing.T) {
	t.Parallel()
	f := newMergeEmptyFeature()
	f.pdfcpuGolds["doc"] = []normalizedBookmark{}
	out, _ := json.Marshal(mergeEmptyOutput{
		InputPages:  1,
		OutPages:    2,
		InputText:   []string{"A"},
		OutText:     []string{"A", "should be empty"},
		PartnerText: "",
	})
	v := f.Compare("doc", out)
	if v.Match {
		t.Fatalf("expected mismatch")
	}
	if !strings.Contains(v.Reason, "page 2 text differs from empty partner") {
		t.Errorf("reason: %q", v.Reason)
	}
}

// TestMergeEmptyFeature_Compare_BookmarkPageOffset: a bookmark that
// surfaces with a non-zero page offset (e.g. litho's merge wrongly
// shifted pages even though empty was appended last) registers as a
// mismatch on the litho side.
func TestMergeEmptyFeature_Compare_BookmarkPageOffset(t *testing.T) {
	t.Parallel()
	f := newMergeEmptyFeature()
	f.pdfcpuGolds["doc"] = []normalizedBookmark{{Title: "Intro", Page: 1}}
	out, _ := json.Marshal(mergeEmptyOutput{
		InputPages:  1,
		OutPages:    2,
		InputText:   []string{"A"},
		OutText:     []string{"A", ""},
		PartnerText: "",
		InputBM:     []bookmarkLeaf{{Title: "Intro", Page: 1}},
		LithoOutBM:  []bookmarkLeaf{{Title: "Intro", Page: 2}}, // wrong: should still be 1
	})
	v := f.Compare("doc", out)
	if v.Match {
		t.Fatalf("expected mismatch")
	}
	if !strings.Contains(v.Reason, "litho merge-empty bookmarks") {
		t.Errorf("reason: %q", v.Reason)
	}
}

// TestMergeEmptyFeature_Compare_NoPdfcpuGolden: missing pdfcpu
// golden surfaces as Skipped, so a partial provisioning run does
// not fail the canary artificially.
func TestMergeEmptyFeature_Compare_NoPdfcpuGolden(t *testing.T) {
	t.Parallel()
	f := newMergeEmptyFeature()
	out, _ := json.Marshal(mergeEmptyOutput{
		InputPages:  1,
		OutPages:    2,
		InputText:   []string{"A"},
		OutText:     []string{"A", ""},
		PartnerText: "",
	})
	v := f.Compare("missing", out)
	if !v.Skipped {
		t.Errorf("Verdict: %+v, want Skipped", v)
	}
}
