package bookmarks

import (
	"context"
	"math"
	"sort"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/models"
)

// headingCandidate is a single line of text that may be a heading.
type headingCandidate struct {
	text     string
	fontSize float64
	pageNum  int
	yTop     float64 // Y coordinate of the top of the line (for dedup ordering)
}

// pageTextElements holds the text elements from a single page along with
// its 1-based page number. Aliased publicly as [PageTextElements] for use
// by callers in other packages.
type pageTextElements struct {
	PageNum  int
	Elements []models.TextElement
}

// GenerateFromFontSize analyzes font sizes across pages and returns a
// hierarchical bookmark tree for headings that stand out from body text.
func GenerateFromFontSize(ctx context.Context, pages []PageTextElements, opts Options) []models.Bookmark {
	opts.applyDefaults()

	// Collect all non-trivial text elements across all pages.
	var allElements []models.TextElement
	for _, p := range pages {
		for _, el := range p.Elements {
			if strings.TrimSpace(el.Text) == "" {
				continue
			}
			if el.Hidden || el.Invisible {
				continue
			}
			allElements = append(allElements, el)
		}
	}

	bodySize := detectBodyFontSize(allElements)
	if bodySize <= 0 {
		return nil
	}

	// Find sizes that qualify as headings (strictly larger than body * ratio).
	threshold := bodySize * opts.MinFontSizeRatio
	var candidateSizes []float64
	for _, s := range collectDistinctSizes(allElements) {
		if s > threshold {
			candidateSizes = append(candidateSizes, s)
		}
	}
	if len(candidateSizes) == 0 {
		return nil
	}

	// Cluster sizes into heading levels.
	levels := clusterSizesToLevels(candidateSizes, 0.10, opts.MaxDepth)

	// Build heading candidates by grouping adjacent elements with qualifying
	// font size into lines (per page).
	var candidates []headingCandidate
	for _, p := range pages {
		if err := ctx.Err(); err != nil {
			return nil
		}
		candidates = append(candidates, extractPageHeadings(ctx, p, levels, opts.MaxTitleLength)...)
	}

	if opts.SkipHeaderFooter {
		candidates = filterHeaderFooter(candidates, pages)
	}

	if len(candidates) == 0 {
		return nil
	}

	// Build the hierarchical bookmark tree.
	return buildBookmarkTree(candidates, levels)
}

// extractPageHeadings groups same-sized adjacent text elements on a single
// page into heading candidates.
func extractPageHeadings(ctx context.Context, page pageTextElements, levels map[float64]int, maxTitleLength int) []headingCandidate {
	// Sort elements by Y descending (top to bottom in PDF coordinates), then X.
	sorted := make([]models.TextElement, len(page.Elements))
	copy(sorted, page.Elements)
	sort.SliceStable(sorted, func(i, j int) bool {
		if math.Abs(sorted[i].Y-sorted[j].Y) > 2.0 {
			return sorted[i].Y > sorted[j].Y
		}
		return sorted[i].X < sorted[j].X
	})

	var candidates []headingCandidate
	var currentLine []models.TextElement
	var currentY float64
	var currentSize float64

	flush := func() {
		if len(currentLine) == 0 {
			return
		}
		// Only emit if the line's size is a heading level.
		rounded := math.Round(math.Abs(currentSize)*2) / 2
		level, ok := levels[rounded]
		if !ok {
			currentLine = nil
			return
		}
		// Use the typography reconstructor to join adjacent glyphs into
		// words with proper space inference.
		reconstructed := typography.ReconstructText(ctx, currentLine, nil)
		text := normalizeTitle(reconstructed, maxTitleLength)
		if text != "" && len(text) >= 2 {
			candidates = append(candidates, headingCandidate{
				text:     text,
				fontSize: rounded,
				pageNum:  page.PageNum,
				yTop:     currentY,
			})
		}
		_ = level
		currentLine = nil
	}

	for _, el := range sorted {
		if strings.TrimSpace(el.Text) == "" {
			continue
		}
		if el.Hidden || el.Invisible {
			continue
		}
		size := math.Round(math.Abs(el.FontSize)*2) / 2
		if _, ok := levels[size]; !ok {
			flush()
			continue
		}
		// Start a new line if size differs or Y drops significantly.
		if len(currentLine) == 0 || size != currentSize || math.Abs(el.Y-currentY) > size*0.6 {
			flush()
			currentY = el.Y
			currentSize = size
		}
		currentLine = append(currentLine, el)
	}
	flush()

	return candidates
}

// normalizeTitle collapses whitespace, strips control characters, removes
// trailing page numbers and punctuation, and truncates to maxLength.
func normalizeTitle(s string, maxLength int) string {
	// Collapse whitespace.
	s = strings.Join(strings.Fields(s), " ")
	// Strip soft hyphens and zero-width chars.
	s = strings.NewReplacer("\u00AD", "", "\u200B", "", "\u200C", "", "\u200D", "", "\uFEFF", "").Replace(s)
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Strip trailing dots and colons common in headings.
	s = strings.TrimRight(s, ".:·•–—")
	s = strings.TrimSpace(s)

	if maxLength > 0 && len(s) > maxLength {
		s = s[:maxLength] + "…"
	}
	return s
}

// filterHeaderFooter removes candidates that appear to be running
// headers/footers (same normalized text at similar Y on > 30% of pages).
func filterHeaderFooter(candidates []headingCandidate, pages []pageTextElements) []headingCandidate {
	if len(pages) < 4 {
		return candidates
	}

	// Count occurrences of each normalized text.
	norm := func(s string) string {
		return strings.ToLower(strings.TrimSpace(s))
	}
	counts := make(map[string]int)
	for _, c := range candidates {
		counts[norm(c.text)]++
	}

	threshold := max(len(pages)*3/10, 2)

	filtered := candidates[:0]
	for _, c := range candidates {
		if counts[norm(c.text)] > threshold {
			continue
		}
		filtered = append(filtered, c)
	}
	return filtered
}

// buildBookmarkTree converts flat heading candidates into a nested bookmark
// tree using the level map.
func buildBookmarkTree(candidates []headingCandidate, levels map[float64]int) []models.Bookmark {
	// Sort by page number, then by Y descending (top of page first).
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].pageNum != candidates[j].pageNum {
			return candidates[i].pageNum < candidates[j].pageNum
		}
		return candidates[i].yTop > candidates[j].yTop
	})

	var roots []models.Bookmark
	// stack[i] points to the last bookmark at depth i+1 (so stack[0] = level 1).
	var stack []*models.Bookmark

	for _, c := range candidates {
		level := max(levels[c.fontSize], 1)

		bm := models.Bookmark{
			Title:      c.text,
			PageNumber: c.pageNum,
		}

		// Pop stack back to parent level.
		for len(stack) >= level {
			stack = stack[:len(stack)-1]
		}

		if len(stack) == 0 {
			// Root-level bookmark.
			roots = append(roots, bm)
			stack = append(stack, &roots[len(roots)-1])
		} else {
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, bm)
			stack = append(stack, &parent.Children[len(parent.Children)-1])
		}
	}

	return roots
}
