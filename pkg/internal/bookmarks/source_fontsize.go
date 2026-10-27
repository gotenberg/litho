package bookmarks

import (
	"context"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/heuristics"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/models"
)

// headingCandidate is a single line of text that may be a heading.
type headingCandidate struct {
	text           string
	fontSize       float64
	pageNum        int
	yTop           float64 // Y coordinate of the top of the line (for dedup ordering)
	isBold         bool
	hasNumbering   bool
	numberingDepth int
	score          float64 // assigned by the scorer
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

	// Detect dominant script and adjust the font-size threshold accordingly.
	var sampleText strings.Builder
	for _, el := range allElements {
		if sampleText.Len() > 4000 {
			break
		}
		sampleText.WriteString(el.Text)
	}
	script := detectScript(sampleText.String())
	ratio := opts.MinFontSizeRatio
	if scriptRatio := scriptFontSizeRatio(script); scriptRatio > 0 && scriptRatio < ratio {
		ratio = scriptRatio
	}

	// Find sizes that qualify as headings (strictly larger than body * ratio).
	threshold := bodySize * ratio
	var candidateSizes []float64
	for _, s := range collectDistinctSizes(allElements) {
		if s > threshold {
			candidateSizes = append(candidateSizes, s)
		}
	}
	// Even when no font size qualifies, language patterns may still
	// identify headings. Generation continues and the scorer decides.

	// Cluster sizes into heading levels.
	levels := clusterSizesToLevels(candidateSizes, 0.10, opts.MaxDepth)

	// Build heading candidates by grouping adjacent elements into lines
	// (per page). The qualifying-size filter is removed; the scorer decides.
	var candidates []headingCandidate
	for _, p := range pages {
		if err := ctx.Err(); err != nil {
			return nil
		}
		candidates = append(candidates, extractPageHeadings(ctx, p, opts.MaxTitleLength, opts.SkipHeaderFooter)...)
	}

	// Score each candidate. Filter out TOC entries and low-score lines.
	scored := candidates[:0]
	for _, c := range candidates {
		if looksLikeTOCEntry(c.text) {
			continue
		}

		// Parse numbering if enabled.
		var num *numberingMatch
		if opts.DetectNumbering {
			num = parseNumbering(c.text)
			if num != nil {
				c.hasNumbering = true
				c.numberingDepth = num.Depth
				if opts.StripNumberingFromTitle {
					c.text = num.Title
				}
			}
		}

		hc := HeadingCandidate{
			Text:           c.text,
			FontSize:       c.fontSize,
			BodySize:       bodySize,
			PageNum:        c.pageNum,
			CharCount:      len(c.text),
			IsBold:         c.isBold,
			HasNumbering:   c.hasNumbering,
			NumberingDepth: c.numberingDepth,
			Script:         script,
		}
		c.score = opts.Scorer.Score(hc)
		if c.score < opts.ScoreThreshold {
			continue
		}
		scored = append(scored, c)
	}
	candidates = scored

	if opts.SkipHeaderFooter {
		candidates = filterHeaderFooter(candidates, pages)
	}

	if len(candidates) == 0 {
		return nil
	}

	// Determine final levels: numbering depth overrides font-size clustering
	// when both are present.
	return buildBookmarkTreeWithOverrides(candidates, levels, opts.MaxDepth)
}

// extractPageHeadings groups adjacent text elements on a single page into
// heading candidate lines. All non-trivial lines are returned; the scorer
// decides which are headings.
func extractPageHeadings(ctx context.Context, page pageTextElements, maxTitleLength int, skipHF bool) []headingCandidate {
	// Sort elements by Y descending (top to bottom in PDF coordinates), then X.
	sorted := make([]models.TextElement, len(page.Elements))
	copy(sorted, page.Elements)
	sort.SliceStable(sorted, func(i, j int) bool {
		if math.Abs(sorted[i].Y-sorted[j].Y) > 2.0 {
			return sorted[i].Y > sorted[j].Y
		}
		return sorted[i].X < sorted[j].X
	})

	// Compute the Y range of the page from the elements (a rough proxy for
	// the CropBox top/bottom). Used for the header/footer Y-position filter.
	var minY, maxY float64
	for i, el := range sorted {
		if i == 0 || el.Y < minY {
			minY = el.Y
		}
		if i == 0 || el.Y > maxY {
			maxY = el.Y
		}
	}
	pageHeight := maxY - minY
	headerCutoff := maxY - pageHeight*0.10
	footerCutoff := minY + pageHeight*0.10

	var candidates []headingCandidate
	var currentLine []models.TextElement
	var currentY float64
	var currentSize float64

	flush := func() {
		if len(currentLine) == 0 {
			return
		}
		rounded := math.Round(math.Abs(currentSize)*2) / 2
		// Skip lines in the header/footer Y zone (running headers/footers).
		// Only when SkipHeaderFooter is enabled.
		if skipHF && pageHeight > 100 {
			if currentY >= headerCutoff || currentY <= footerCutoff {
				currentLine = nil
				return
			}
		}
		// Use the typography reconstructor to join adjacent glyphs into
		// words with proper space inference.
		reconstructed := typography.ReconstructText(ctx, currentLine, nil)
		text := normalizeTitle(reconstructed, maxTitleLength)
		if text != "" && len([]rune(text)) >= 2 {
			candidates = append(candidates, headingCandidate{
				text:     text,
				fontSize: rounded,
				pageNum:  page.PageNum,
				yTop:     currentY,
			})
		}
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
		// Start a new line when size changes or Y drops significantly.
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
// trailing page numbers and punctuation, and truncates to maxLength
// characters.
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

	// maxLength counts characters, not bytes: a byte cut could split a
	// multi-byte character and leave invalid UTF-8 in the outline title.
	if maxLength > 0 && utf8.RuneCountInString(s) > maxLength {
		s = string([]rune(s)[:maxLength]) + "…"
	}
	return s
}

// filterHeaderFooter removes candidates that appear to be running
// headers/footers: the same text, case aside, more times than 30% of the
// page count, wherever it sits (see [heuristics.RunningLines]). Auto-tagging
// reads the same repeats more strictly, by position and on a majority of
// pages, since it hides what it finds from a reader where this filter only
// keeps it out of the outline.
//
// Every occurrence counts, repeats on one page included, so each candidate is
// given a page of its own: a label a form repeats down one page ("Yes No") is
// no heading either.
func filterHeaderFooter(candidates []headingCandidate, pages []pageTextElements) []headingCandidate {
	if len(pages) < 4 {
		return candidates
	}

	lines := make([]heuristics.Line, len(candidates))
	for i, c := range candidates {
		lines[i] = heuristics.Line{Page: i, Text: c.text}
	}
	running := heuristics.RunningLines(lines, heuristics.RunningOptions{
		MinPages: max(len(pages)*3/10, 2) + 1,
		FoldCase: true,
	})

	filtered := candidates[:0]
	for i, c := range candidates {
		if running[i] {
			continue
		}
		filtered = append(filtered, c)
	}
	return filtered
}

// buildBookmarkTreeWithOverrides converts candidates into a nested bookmark
// tree, preferring the numbering depth (when present) over font-size
// clustering. Levels are capped at maxDepth.
func buildBookmarkTreeWithOverrides(candidates []headingCandidate, levels map[float64]int, maxDepth int) []models.Bookmark {
	if maxDepth < 1 {
		maxDepth = 1
	}
	// Build a final level for each candidate.
	finalLevels := make([]int, len(candidates))
	for i, c := range candidates {
		level := levels[c.fontSize]
		if level == 0 {
			level = 1
		}
		if c.hasNumbering && c.numberingDepth > 0 {
			level = c.numberingDepth
		}
		if level > maxDepth {
			level = maxDepth
		}
		if level < 1 {
			level = 1
		}
		finalLevels[i] = level
	}

	// Sort by page, then Y descending (top of page first), preserving
	// original level mapping.
	type indexed struct {
		c     headingCandidate
		level int
	}
	idx := make([]indexed, len(candidates))
	for i, c := range candidates {
		idx[i] = indexed{c: c, level: finalLevels[i]}
	}
	sort.SliceStable(idx, func(i, j int) bool {
		if idx[i].c.pageNum != idx[j].c.pageNum {
			return idx[i].c.pageNum < idx[j].c.pageNum
		}
		return idx[i].c.yTop > idx[j].c.yTop
	})

	var roots []models.Bookmark
	var stack []*models.Bookmark
	for _, e := range idx {
		bm := models.Bookmark{Title: e.c.text, PageNumber: e.c.pageNum}
		for len(stack) >= e.level {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
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
