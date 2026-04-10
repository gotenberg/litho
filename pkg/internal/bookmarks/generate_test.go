package bookmarks

import (
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

func TestDetectBodyFontSize(t *testing.T) {
	elements := []models.TextElement{
		{Text: "Heading", FontSize: 24},
		{Text: "Body text here is quite long and repeats often", FontSize: 10},
		{Text: "More body text at the common size", FontSize: 10},
		{Text: "Another heading", FontSize: 24},
		{Text: "Final body line", FontSize: 10},
	}
	body := detectBodyFontSize(elements)
	if body != 10 {
		t.Errorf("body size: got %f, want 10", body)
	}
}

func TestClusterSizesToLevels(t *testing.T) {
	// Three distinct sizes: 24, 18, 14 → levels 1, 2, 3
	sizes := []float64{24, 18, 14}
	levels := clusterSizesToLevels(sizes, 0.10, 3)
	if levels[24] != 1 {
		t.Errorf("level for 24: got %d, want 1", levels[24])
	}
	if levels[18] != 2 {
		t.Errorf("level for 18: got %d, want 2", levels[18])
	}
	if levels[14] != 3 {
		t.Errorf("level for 14: got %d, want 3", levels[14])
	}
}

func TestClusterSizesMerging(t *testing.T) {
	// 24 and 23 are within 10% → same level
	sizes := []float64{24, 23, 18}
	levels := clusterSizesToLevels(sizes, 0.10, 3)
	if levels[24] != 1 || levels[23] != 1 {
		t.Errorf("24 and 23 should be level 1: %v", levels)
	}
	if levels[18] != 2 {
		t.Errorf("18 should be level 2: got %d", levels[18])
	}
}

func TestClusterMaxDepthCap(t *testing.T) {
	sizes := []float64{30, 25, 20, 15, 12}
	levels := clusterSizesToLevels(sizes, 0.05, 3)
	// With maxDepth=3, level 4 and 5 are capped at 3.
	if levels[30] != 1 || levels[25] != 2 || levels[20] != 3 || levels[15] != 3 || levels[12] != 3 {
		t.Errorf("unexpected levels: %v", levels)
	}
}

func TestNormalizeTitle(t *testing.T) {
	tests := []struct {
		in, out string
	}{
		{"  Chapter 1  ", "Chapter 1"},
		{"Introduction\u00AD", "Introduction"},
		{"Summary:", "Summary"},
		{"Multiple   spaces", "Multiple spaces"},
		{"", ""},
	}
	for _, tc := range tests {
		got := normalizeTitle(tc.in, 200)
		if got != tc.out {
			t.Errorf("normalizeTitle(%q): got %q, want %q", tc.in, got, tc.out)
		}
	}
}

func TestGenerateFromFontSize_SimpleHeadings(t *testing.T) {
	pages := []PageTextElements{
		{
			PageNum: 1,
			Elements: []models.TextElement{
				{Text: "Introduction", X: 50, Y: 800, FontSize: 24},
				{Text: "This is body text that appears many times", X: 50, Y: 750, FontSize: 10},
				{Text: "Another body line at the common size", X: 50, Y: 735, FontSize: 10},
				{Text: "Overview", X: 50, Y: 700, FontSize: 18},
				{Text: "Body content following the subheading here", X: 50, Y: 680, FontSize: 10},
				{Text: "More body content with enough characters", X: 50, Y: 665, FontSize: 10},
			},
		},
		{
			PageNum: 2,
			Elements: []models.TextElement{
				{Text: "Details", X: 50, Y: 800, FontSize: 18},
				{Text: "Extended body discussion of the details", X: 50, Y: 780, FontSize: 10},
				{Text: "Conclusions drawn from the analysis", X: 50, Y: 765, FontSize: 10},
			},
		},
	}

	opts := Options{}
	opts.applyDefaults()
	bookmarks := GenerateFromFontSize(t.Context(), pages, opts)

	if len(bookmarks) < 1 {
		t.Fatalf("expected at least 1 bookmark, got %d", len(bookmarks))
	}

	// Introduction (24pt) should be level 1
	if bookmarks[0].Title != "Introduction" {
		t.Errorf("first bookmark: got %q, want %q", bookmarks[0].Title, "Introduction")
	}
	if bookmarks[0].PageNumber != 1 {
		t.Errorf("first bookmark page: got %d, want 1", bookmarks[0].PageNumber)
	}

	// Overview and Details (18pt) should be children of Introduction
	if len(bookmarks[0].Children) < 2 {
		t.Errorf("Introduction should have 2 children (Overview, Details), got %d", len(bookmarks[0].Children))
	}
}

func TestGenerateFromFontSize_NoHeadings(t *testing.T) {
	// All same font size = no headings.
	pages := []PageTextElements{
		{
			PageNum: 1,
			Elements: []models.TextElement{
				{Text: "Uniform text", X: 50, Y: 800, FontSize: 10},
				{Text: "More uniform text", X: 50, Y: 780, FontSize: 10},
			},
		},
	}
	opts := Options{}
	opts.applyDefaults()
	bookmarks := GenerateFromFontSize(t.Context(), pages, opts)
	if len(bookmarks) != 0 {
		t.Errorf("expected no bookmarks, got %d", len(bookmarks))
	}
}
