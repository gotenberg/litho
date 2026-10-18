package bookmarks

import (
	"math"
	"sort"

	"github.com/gotenberg/litho/pkg/models"
)

// detectBodyFontSize computes the most common (weighted by character count)
// font size across the given text elements. This is taken as the body text
// size; anything significantly larger is a heading candidate.
func detectBodyFontSize(elements []models.TextElement) float64 {
	if len(elements) == 0 {
		return 0
	}

	// Build weighted histogram: font size → total glyph count.
	// Round to 0.5 pt precision to merge near-identical sizes.
	hist := make(map[float64]int)
	for _, el := range elements {
		size := math.Round(math.Abs(el.FontSize)*2) / 2
		if size <= 0 {
			continue
		}
		hist[size] += max(len(el.Text), 1)
	}

	if len(hist) == 0 {
		return 0
	}

	// Find the size with the maximum weight. Map order is random, so a tie
	// breaks by size: without it, two sizes of equal weight make the outline
	// change from run to run. The smaller size wins, since body text is set
	// smaller than the headings it runs under.
	var bodySize float64
	var maxWeight int
	for size, weight := range hist {
		if weight > maxWeight || (weight == maxWeight && size < bodySize) {
			maxWeight = weight
			bodySize = size
		}
	}

	return bodySize
}

// collectDistinctSizes returns all distinct font sizes present in the
// elements, sorted descending.
func collectDistinctSizes(elements []models.TextElement) []float64 {
	seen := make(map[float64]bool)
	for _, el := range elements {
		size := math.Round(math.Abs(el.FontSize)*2) / 2
		if size > 0 {
			seen[size] = true
		}
	}
	sizes := make([]float64, 0, len(seen))
	for s := range seen {
		sizes = append(sizes, s)
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(sizes)))
	return sizes
}
