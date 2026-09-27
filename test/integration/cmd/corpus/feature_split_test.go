package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestCheckSplitBookmarks_Empty: empty outline in, empty outline out
// satisfies both invariants trivially.
func TestCheckSplitBookmarks_Empty(t *testing.T) {
	t.Parallel()
	if reason := checkSplitBookmarks(nil, nil); reason != "" {
		t.Errorf("empty: got %q, want empty", reason)
	}
}

// TestCheckSplitBookmarks_ValidPages: bookmarks targeting pages 1, 2 and 3,
// the repeat of page 1, pass; titles must exist in the input tree.
func TestCheckSplitBookmarks_ValidPages(t *testing.T) {
	t.Parallel()
	in := []normalizedBookmark{
		{Title: "First", Page: 1},
		{Title: "Middle", Page: 5},
		{Title: "Last", Page: 10},
	}
	out := []normalizedBookmark{
		{Title: "First", Page: 1},
		{Title: "Last", Page: 2},
		{Title: "First", Page: 3},
	}
	if reason := checkSplitBookmarks(out, in); reason != "" {
		t.Errorf("valid: got %q, want empty", reason)
	}
}

// TestCheckSplitBookmarks_RejectStalePage flags a bookmark whose page
// is outside {0, 1, 2, 3} after split.
func TestCheckSplitBookmarks_RejectStalePage(t *testing.T) {
	t.Parallel()
	out := []normalizedBookmark{{Title: "x", Page: 4}}
	in := []normalizedBookmark{{Title: "x", Page: 5}}
	reason := checkSplitBookmarks(out, in)
	if !strings.Contains(reason, "page = 4") {
		t.Errorf("stale page: got %q, want 'page = 4' substring", reason)
	}
}

// TestCheckSplitBookmarks_RejectFabricatedTitle flags an output title
// not present in the input outline tree.
func TestCheckSplitBookmarks_RejectFabricatedTitle(t *testing.T) {
	t.Parallel()
	out := []normalizedBookmark{{Title: "Made Up", Page: 1}}
	in := []normalizedBookmark{{Title: "Real", Page: 1}}
	reason := checkSplitBookmarks(out, in)
	if !strings.Contains(reason, "not present in input outline") {
		t.Errorf("fabricated title: got %q, want 'not present in input outline' substring", reason)
	}
}

// TestCheckSplitBookmarks_NestedRespected applies both invariants to
// children. Pre-order traversal returns the first violation.
func TestCheckSplitBookmarks_NestedRespected(t *testing.T) {
	t.Parallel()
	out := []normalizedBookmark{
		{Title: "Parent", Page: 1, Children: []normalizedBookmark{
			{Title: "BadChild", Page: 99},
		}},
	}
	in := []normalizedBookmark{
		{Title: "Parent", Page: 1, Children: []normalizedBookmark{
			{Title: "BadChild", Page: 99},
		}},
	}
	reason := checkSplitBookmarks(out, in)
	if !strings.Contains(reason, "page = 99") {
		t.Errorf("nested stale page: got %q", reason)
	}
}

// TestCheckSplitBookmarks_AllowZeroPage tolerates untitled outline
// parents that emit page = 0 ("no destination").
func TestCheckSplitBookmarks_AllowZeroPage(t *testing.T) {
	t.Parallel()
	out := []normalizedBookmark{{Title: "Section", Page: 0}}
	in := []normalizedBookmark{{Title: "Section", Page: 5}}
	if reason := checkSplitBookmarks(out, in); reason != "" {
		t.Errorf("page=0: got %q, want empty", reason)
	}
}

// TestSplitFeature_Compare_SkippedPropagates: Run encodes skipped via
// the output blob; Compare must surface it as Verdict.Skipped instead
// of failing the comparison.
func TestSplitFeature_Compare_SkippedPropagates(t *testing.T) {
	t.Parallel()
	f := newSplitFeature()
	out, _ := json.Marshal(splitOutput{Skipped: true, SkipReason: "single-page input"})
	v := f.Compare("doc", out)
	if !v.Skipped {
		t.Errorf("Verdict: %+v, want Skipped", v)
	}
	if v.Reason != "single-page input" {
		t.Errorf("reason: got %q", v.Reason)
	}
}

// TestSplitFeature_Compare_PageCountWrong rejects outputs whose page
// count is anything other than 3. A split that wrote page 1 once where
// the list names it twice reads as 2 pages.
func TestSplitFeature_Compare_PageCountWrong(t *testing.T) {
	t.Parallel()
	f := newSplitFeature()
	out, _ := json.Marshal(splitOutput{InputPages: 3, NumPages: 2})
	v := f.Compare("doc", out)
	if v.Match {
		t.Fatalf("expected mismatch, got %+v", v)
	}
	if !strings.Contains(v.Reason, "page count: got 2, want 3") {
		t.Errorf("reason: got %q", v.Reason)
	}
}

// TestSplitFeature_Compare_HappyPath: text and bookmarks both match
// their input-derived oracle; the verdict is Match=true.
func TestSplitFeature_Compare_HappyPath(t *testing.T) {
	t.Parallel()
	f := newSplitFeature()
	out, _ := json.Marshal(splitOutput{
		InputPages: 3,
		NumPages:   3,
		InputFirst: "First",
		InputLast:  "Last",
		OutFirst:   "First",
		OutLast:    "Last",
		OutRepeat:  "First",
		InputBM: []normalizedBookmark{
			{Title: "Intro", Page: 1},
			{Title: "End", Page: 3},
		},
		OutBM: []normalizedBookmark{
			{Title: "Intro", Page: 1},
			{Title: "End", Page: 2},
		},
	})
	v := f.Compare("doc", out)
	if !v.Match {
		t.Fatalf("expected match, got %+v", v)
	}
	if v.Score != 1.0 {
		t.Errorf("score: %v", v.Score)
	}
}

// TestSplitFeature_Compare_TextMismatch: output page[0] text disagrees
// with the input's first-page text. Reason names page[0].
func TestSplitFeature_Compare_TextMismatch(t *testing.T) {
	t.Parallel()
	f := newSplitFeature()
	out, _ := json.Marshal(splitOutput{
		InputPages: 2,
		NumPages:   3,
		InputFirst: "Expected",
		InputLast:  "Last",
		OutFirst:   "Wrong",
		OutLast:    "Last",
		OutRepeat:  "Expected",
	})
	v := f.Compare("doc", out)
	if v.Match {
		t.Fatalf("expected mismatch")
	}
	if !strings.Contains(v.Reason, "page[0]") {
		t.Errorf("reason missing page[0]: %q", v.Reason)
	}
}

// TestSplitFeature_Compare_RepeatTextMismatch: output page[2], the second
// copy of input page 1, disagrees with it while page[0] agrees. Reason
// names page[2].
func TestSplitFeature_Compare_RepeatTextMismatch(t *testing.T) {
	t.Parallel()
	f := newSplitFeature()
	out, _ := json.Marshal(splitOutput{
		InputPages: 2,
		NumPages:   3,
		InputFirst: "First",
		InputLast:  "Last",
		OutFirst:   "First",
		OutLast:    "Last",
		OutRepeat:  "Last",
	})
	v := f.Compare("doc", out)
	if v.Match {
		t.Fatalf("expected mismatch, got %+v", v)
	}
	if !strings.Contains(v.Reason, "page[2]") {
		t.Errorf("reason missing page[2]: %q", v.Reason)
	}
}

// TestSplitPages_RepeatsThePageItNamesFirst pins the list the cell splits:
// the first page, the last and the first again, so every PDF of two
// pages or more reaches the path that writes a page named twice.
func TestSplitPages_RepeatsThePageItNamesFirst(t *testing.T) {
	t.Parallel()
	got := splitPages(7)
	if len(got) != 3 || got[0] != 1 || got[1] != 7 || got[2] != 1 {
		t.Errorf("splitPages(7) = %v, want [1 7 1]", got)
	}
}

// TestSplitFeature_Compare_BookmarkInvariantViolation: page count and
// text both agree, but a bookmark targets a non-surviving page.
func TestSplitFeature_Compare_BookmarkInvariantViolation(t *testing.T) {
	t.Parallel()
	f := newSplitFeature()
	out, _ := json.Marshal(splitOutput{
		InputPages: 2,
		NumPages:   3,
		InputFirst: "First",
		InputLast:  "Last",
		OutFirst:   "First",
		OutLast:    "Last",
		OutRepeat:  "First",
		InputBM:    []normalizedBookmark{{Title: "X", Page: 1}},
		OutBM:      []normalizedBookmark{{Title: "X", Page: 5}},
	})
	v := f.Compare("doc", out)
	if v.Match {
		t.Fatalf("expected mismatch, got %+v", v)
	}
	if !strings.Contains(v.Reason, "page = 5") {
		t.Errorf("reason: got %q", v.Reason)
	}
}
