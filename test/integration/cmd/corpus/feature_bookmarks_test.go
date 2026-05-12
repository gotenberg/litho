package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestNormalizeBookmarkTitle pins the Unicode + whitespace canonicalisation
// applied to both sides before diffing. Without it, a non-breaking space
// in a pdfcpu export and a regular space in litho's output would surface
// as a title mismatch on every fixture that touched it.
func TestNormalizeBookmarkTitle(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		in, want string
	}{
		{"plain", "Chapter 1", "Chapter 1"},
		{"trim", "  Chapter 1  ", "Chapter 1"},
		// NFD "é" (e + U+0301) normalises to NFC "é" (U+00E9).
		{"nfc", "Café", "Café"},
		// C1 control runes fold to their PDFDocEncoding glyph
		// equivalents. pdfcpu emits raw bytes as U+0085, U+0090,
		// etc.; litho already mapped the byte to the en-dash /
		// smart quote glyph via PDFDocEncoding, so this fold
		// makes them agree without weakening the test.
		{"c1-en-dash", "Annex A \u0085 Foo", "Annex A \u2013 Foo"},
		{"c1-em-dash", "Annex A \u0084 Foo", "Annex A \u2014 Foo"},
		{"c1-rsquote", "What\u0090s in this guide?", "What\u2019s in this guide?"},
		{"c1-bullet", "Item\u0080A", "Item\u2022A"},
		// 0x9F is undefined in PDFDocEncoding; leave it alone so
		// a future producer using the slot stays visible.
		{"c1-undefined", "x\u009fy", "x\u009fy"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := normalizeBookmarkTitle(c.in); got != c.want {
				t.Errorf("normalizeBookmarkTitle(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestNormalizePdfcpuTree round-trips a pdfcpu export shape into the
// internal comparison shape: header is dropped, kids becomes Children,
// page becomes Page, and an empty bookmarks slice collapses to nil so
// it compares equal to a litho document with no outline.
func TestNormalizePdfcpuTree(t *testing.T) {
	t.Parallel()
	in := []pdfcpuBookmark{
		{Title: "Top", Page: 1, Kids: []pdfcpuBookmark{
			{Title: "Sub", Page: 2},
		}},
	}
	out := normalizePdfcpuTree(in)
	if len(out) != 1 || out[0].Title != "Top" || out[0].Page != 1 {
		t.Fatalf("root: %+v", out)
	}
	if len(out[0].Children) != 1 || out[0].Children[0].Page != 2 {
		t.Fatalf("nested: %+v", out[0].Children)
	}
	if got := normalizePdfcpuTree(nil); got != nil {
		t.Errorf("nil input: got %v, want nil", got)
	}
	if got := normalizePdfcpuTree([]pdfcpuBookmark{}); got != nil {
		t.Errorf("empty input: got %v, want nil", got)
	}
}

// TestNormalizeLithoTree mirrors [TestNormalizePdfcpuTree] for the
// []models.Bookmark side so the two normalisers stay in sync.
func TestNormalizeLithoTree(t *testing.T) {
	t.Parallel()
	in := []models.Bookmark{
		{Title: "Top", PageNumber: 1, Children: []models.Bookmark{
			{Title: "Sub", PageNumber: 2},
		}},
	}
	out := normalizeLithoTree(in)
	if len(out) != 1 || out[0].Title != "Top" || out[0].Page != 1 {
		t.Fatalf("root: %+v", out)
	}
	if len(out[0].Children) != 1 || out[0].Children[0].Page != 2 {
		t.Fatalf("nested: %+v", out[0].Children)
	}
	if got := normalizeLithoTree(nil); got != nil {
		t.Errorf("nil input: got %v, want nil", got)
	}
}

// TestDiffBookmarks pins each divergence path: equal trees return "",
// shape differences report length, leaf-field differences pin the
// exact slot. The first mismatch wins; later mismatches are silenced
// so [Verdict.Reason] stays a single readable line.
func TestDiffBookmarks(t *testing.T) {
	t.Parallel()
	equal := []normalizedBookmark{{Title: "A", Page: 1}}
	if diff := diffBookmarks("bookmarks", equal, equal); diff != "" {
		t.Errorf("equal: got %q, want empty", diff)
	}

	cases := []struct {
		name  string
		got   []normalizedBookmark
		want  []normalizedBookmark
		match string // substring expected in the reason
	}{
		{
			"length",
			[]normalizedBookmark{{Title: "A", Page: 1}},
			[]normalizedBookmark{{Title: "A", Page: 1}, {Title: "B", Page: 2}},
			"len got=1 want=2",
		},
		{
			"title",
			[]normalizedBookmark{{Title: "got", Page: 1}},
			[]normalizedBookmark{{Title: "want", Page: 1}},
			"bookmarks[0].title",
		},
		{
			"page",
			[]normalizedBookmark{{Title: "A", Page: 1}},
			[]normalizedBookmark{{Title: "A", Page: 2}},
			"bookmarks[0].page: got 1 want 2",
		},
		{
			"nested",
			[]normalizedBookmark{{Title: "A", Page: 1, Children: []normalizedBookmark{{Title: "x", Page: 2}}}},
			[]normalizedBookmark{{Title: "A", Page: 1, Children: []normalizedBookmark{{Title: "y", Page: 2}}}},
			"bookmarks[0].kids[0].title",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			diff := diffBookmarks("bookmarks", c.got, c.want)
			if !strings.Contains(diff, c.match) {
				t.Errorf("diff = %q, want substring %q", diff, c.match)
			}
		})
	}
}

// TestBookmarksFeature_LoadGoldens reads a temporary directory of
// pdfcpu-shaped JSON files and verifies the index keys by basename (the
// fixture ID convention) and discards the header without erroring on
// extra fields.
func TestBookmarksFeature_LoadGoldens(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	golden := map[string]any{
		"header": map[string]any{"source": "doc.pdf"},
		"bookmarks": []any{
			map[string]any{"title": "Intro", "page": 1, "kids": []any{
				map[string]any{"title": "Setup", "page": 2},
			}},
		},
	}
	body, err := json.Marshal(golden)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "doc.json"), body, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	f := newBookmarksFeature()
	if err := f.LoadGoldens(dir); err != nil {
		t.Fatalf("LoadGoldens: %v", err)
	}
	if !f.HasGolden("doc") {
		t.Fatal("doc golden missing")
	}
	tree := f.goldens["doc"]
	if len(tree) != 1 || tree[0].Title != "Intro" || tree[0].Children[0].Page != 2 {
		t.Fatalf("normalised tree: %+v", tree)
	}
}

// TestBookmarksFeature_LoadGoldens_EmptyDir returns [ErrNoGoldens] when
// the directory has no JSON files so the runner can show [no golden]
// rather than treating an unprovisioned corpus as a fatal error.
func TestBookmarksFeature_LoadGoldens_EmptyDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	f := newBookmarksFeature()
	if err := f.LoadGoldens(dir); err == nil {
		t.Fatal("LoadGoldens on empty dir: want error, got nil")
	}
}

// TestBookmarksFeature_Compare_Match pairs a litho-shaped output blob
// with an equivalent pdfcpu golden and pins the happy-path Verdict
// (Match=true, Score=1.0, no Reason).
func TestBookmarksFeature_Compare_Match(t *testing.T) {
	t.Parallel()
	f := newBookmarksFeature()
	f.goldens["doc"] = []normalizedBookmark{
		{Title: "Intro", Page: 1, Children: []normalizedBookmark{
			{Title: "Setup", Page: 2},
		}},
	}
	out, _ := json.Marshal(bookmarksOutput{Bookmarks: []normalizedBookmark{
		{Title: "Intro", Page: 1, Children: []normalizedBookmark{
			{Title: "Setup", Page: 2},
		}},
	}})
	v := f.Compare("doc", out)
	if !v.Match || v.Score != 1.0 || v.Reason != "" {
		t.Errorf("Verdict: %+v", v)
	}
}

// TestBookmarksFeature_Compare_Mismatch confirms a diverging tree
// surfaces as Match=false with a path-qualified reason.
func TestBookmarksFeature_Compare_Mismatch(t *testing.T) {
	t.Parallel()
	f := newBookmarksFeature()
	f.goldens["doc"] = []normalizedBookmark{{Title: "Intro", Page: 1}}
	out, _ := json.Marshal(bookmarksOutput{Bookmarks: []normalizedBookmark{
		{Title: "Wrong", Page: 1},
	}})
	v := f.Compare("doc", out)
	if v.Match {
		t.Fatalf("expected mismatch, got %+v", v)
	}
	if !strings.Contains(v.Reason, "bookmarks[0].title") {
		t.Errorf("reason missing path: %q", v.Reason)
	}
}

// TestBookmarksFeature_Compare_NoGolden returns a Skipped verdict when
// the fixture has no captured golden (e.g. provisioning failed for that
// PDF), keeping the corpus run resilient to partial provisioning.
func TestBookmarksFeature_Compare_NoGolden(t *testing.T) {
	t.Parallel()
	f := newBookmarksFeature()
	v := f.Compare("missing", []byte(`{"bookmarks":[]}`))
	if !v.Skipped {
		t.Errorf("Verdict: %+v, want Skipped", v)
	}
}
