package mutations

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/bookmarks"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// alwaysHeading scores every line as a certain heading.
//
// [bookmarks.Options.Scorer] is only defaulted when nil, so injecting one takes
// the heading HEURISTIC out of these tests entirely. What is uncovered here is
// the reconstruct PLUMBING - which page numbers the provider is asked for and
// which numbers the resulting bookmarks carry - and the heuristic has its own
// tests in the bookmarks package. Without this the fixture would have to satisfy
// font-size clustering and a 0.5 score cut to produce anything at all, and a
// fixture that fails to would leave these tests asserting nothing.
type alwaysHeading struct{}

func (alwaysHeading) Score(bookmarks.HeadingCandidate) float64 { return 1 }

// reconstructOptions disables every filter that could silently drop the single
// candidate each fixture supplies.
func reconstructOptions() bookmarks.Options {
	return bookmarks.Options{
		MaxDepth:            3,
		MinFontSizeRatio:    1.15,
		MaxTitleLength:      200,
		ScoreThreshold:      0.5,
		Scorer:              alwaysHeading{},
		PreferStructureTree: false, // keeps bookmarks.Generate off the struct-tree path, which needs a Doc
		SkipHeaderFooter:    false,
		DetectNumbering:     false,
	}
}

// reconstructDoc returns a document carrying only a logger.
//
// reconstructFromKeptPages logs through doc.Logger when it skips an
// out-of-range kept page, so a nil document panics on exactly the branch
// TestResolveSplitBookmarks_ReconstructSkipsOutOfRange exists to exercise.
// Every case here takes a real one rather than resting on which paths happen
// not to log. Unexported fields stay zero, which is fine: nothing on these
// paths resolves an indirect reference.
func reconstructDoc() *document.Document {
	return &document.Document{Logger: document.NopLogger()}
}

func headingElement(text string) []models.TextElement {
	return []models.TextElement{
		{Text: text, FontSize: 24, X: 72, Y: 700, Width: 200},
	}
}

// TestResolveSplitBookmarks_Reconstruct covers reconstructFromKeptPages, which
// no test in the repository reached: measured at 0.0% while its caller
// resolveSplitBookmarks read 45.5%, because both DefaultSplitBookmarkConfig and
// DefaultMergeBookmarkConfig set Handling to BookmarkUpdate and nothing ever
// selected BookmarkReconstruct.
//
// The contract under test is the page-number translation: the provider is asked
// for the OLD page number, while the bookmark it produces carries the NEW
// position in the output.
func TestResolveSplitBookmarks_Reconstruct(t *testing.T) {
	pages := []*document.Page{
		{Ref: ast.PdfIndirectRef{ObjectNum: 1}},
		{Ref: ast.PdfIndirectRef{ObjectNum: 2}},
		{Ref: ast.PdfIndirectRef{ObjectNum: 3}},
	}

	var asked []int
	cfg := BookmarkConfig{
		Handling:   BookmarkReconstruct,
		Generation: reconstructOptions(),
		TextElements: func(_ context.Context, _, pageNum int) ([]models.TextElement, error) {
			asked = append(asked, pageNum)
			return headingElement("Chapter Two"), nil
		},
	}

	// Keep source page 2 only, so it becomes page 1 of the output.
	got, err := resolveSplitBookmarks(context.Background(), reconstructDoc(), pages, []int{2}, cfg)
	if err != nil {
		t.Fatalf("resolveSplitBookmarks: %v", err)
	}

	if len(asked) != 1 || asked[0] != 2 {
		t.Errorf("provider asked for pages %v, want [2]: reconstruct reads the SOURCE page", asked)
	}
	if len(got) != 1 {
		t.Fatalf("got %d bookmarks, want 1: %+v", len(got), got)
	}
	if got[0].Title != "Chapter Two" {
		t.Errorf("Title = %q, want %q", got[0].Title, "Chapter Two")
	}
	if got[0].PageNumber != 1 {
		t.Errorf("PageNumber = %d, want 1: kept page 2 becomes page 1 of the output", got[0].PageNumber)
	}
}

// TestResolveSplitBookmarks_ReconstructSkipsOutOfRange pins the guard on a
// kept-page index outside the source: it is skipped rather than indexing
// allPages out of bounds.
func TestResolveSplitBookmarks_ReconstructSkipsOutOfRange(t *testing.T) {
	pages := []*document.Page{{Ref: ast.PdfIndirectRef{ObjectNum: 1}}}

	var asked []int
	cfg := BookmarkConfig{
		Handling:   BookmarkReconstruct,
		Generation: reconstructOptions(),
		TextElements: func(_ context.Context, _, pageNum int) ([]models.TextElement, error) {
			asked = append(asked, pageNum)
			return headingElement("Only Page"), nil
		},
	}

	// Page 5 does not exist in a one-page source, and 0 is below the range.
	got, err := resolveSplitBookmarks(context.Background(), reconstructDoc(), pages, []int{1, 5, 0}, cfg)
	if err != nil {
		t.Fatalf("resolveSplitBookmarks: %v", err)
	}
	if len(asked) != 1 || asked[0] != 1 {
		t.Errorf("provider asked for pages %v, want [1]: out-of-range pages must be skipped", asked)
	}
	if len(got) != 1 {
		t.Fatalf("got %d bookmarks, want 1", len(got))
	}
}

// TestResolveSplitBookmarks_ReconstructWithoutProvider pins the guard that made
// this path easy to leave untested: with no TextElements provider the function
// returns nothing, so a test that merely selected BookmarkReconstruct would
// enter it and assert nothing at all.
func TestResolveSplitBookmarks_ReconstructWithoutProvider(t *testing.T) {
	pages := []*document.Page{{Ref: ast.PdfIndirectRef{ObjectNum: 1}}}

	got, err := resolveSplitBookmarks(context.Background(), reconstructDoc(), pages, []int{1}, BookmarkConfig{
		Handling:   BookmarkReconstruct,
		Generation: reconstructOptions(),
	})
	if err != nil {
		t.Fatalf("resolveSplitBookmarks: %v", err)
	}
	if got != nil {
		t.Errorf("got %+v, want nil when no TextElements provider is configured", got)
	}
}

// TestResolveMergeBookmarks_Reconstruct covers generateForMergeSource, the
// merge half of the same pair, also at 0.0%.
//
// Its translation runs the other way: the provider is asked for the
// SOURCE-LOCAL page number while the bookmark carries the page number offset by
// everything merged before it.
func TestResolveMergeBookmarks_Reconstruct(t *testing.T) {
	sources := []MergeSource{
		{Doc: reconstructDoc(), Pages: []*document.Page{{Ref: ast.PdfIndirectRef{ObjectNum: 1}}}},
	}

	var askedSource, askedPage []int
	cfg := BookmarkConfig{
		Handling:   BookmarkReconstruct,
		Generation: reconstructOptions(),
		TextElements: func(_ context.Context, sourceIdx, pageNum int) ([]models.TextElement, error) {
			askedSource = append(askedSource, sourceIdx)
			askedPage = append(askedPage, pageNum)
			return headingElement("Merged Heading"), nil
		},
	}

	// Ten pages precede this source in the output.
	got := resolveMergeBookmarks(context.Background(), sources, []int{10}, cfg)

	if len(askedPage) != 1 || askedPage[0] != 1 {
		t.Errorf("provider asked for pages %v, want [1]: merge reads SOURCE-LOCAL numbers", askedPage)
	}
	if len(askedSource) != 1 || askedSource[0] != 0 {
		t.Errorf("provider asked for sources %v, want [0]", askedSource)
	}
	if len(got) != 1 {
		t.Fatalf("got %d bookmarks, want 1: %+v", len(got), got)
	}
	if got[0].PageNumber != 11 {
		t.Errorf("PageNumber = %d, want 11: page 1 of a source offset by 10", got[0].PageNumber)
	}
}
