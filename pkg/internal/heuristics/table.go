package heuristics

import (
	"math"
	"sort"
	"strings"

	"github.com/gotenberg/litho/pkg/models"
)

// lineTolerance is the maximum deviation (in points) for a line to be
// considered horizontal or vertical, and for clustering gridlines.
const lineTolerance = 1.5

// DetectTables analyzes spatial text elements and stroked line segments to
// reconstruct tables from an untagged PDF page.
//
// Strategy:
// Line-based: look for intersecting horizontal/vertical stroked lines that
// form a physical grid, then map text elements into the grid cells.
// Spatial clustering (fallback): when no drawn grid exists, group text
// by Y-coordinate into rows and infer column boundaries from consistent
// X-gaps across multiple rows.
func DetectTables(elements []models.TextElement, lines []models.Line) ([]models.Table, error) {
	visible := filterVisible(elements)
	if len(visible) < 4 {
		return nil, nil
	}

	if tables := detectFromLines(visible, lines); len(tables) > 0 {
		return tables, nil
	}

	if tables := detectFromClustering(visible); len(tables) > 0 {
		return tables, nil
	}

	return nil, nil
}

// filterVisible removes hidden, invisible, and whitespace-only elements.
func filterVisible(elements []models.TextElement) []models.TextElement {
	out := make([]models.TextElement, 0, len(elements))
	for _, el := range elements {
		if !el.Hidden && !el.Invisible && strings.TrimSpace(el.Text) != "" {
			out = append(out, el)
		}
	}
	return out
}

func detectFromLines(elements []models.TextElement, lines []models.Line) []models.Table {
	if len(lines) == 0 {
		return nil
	}

	var hLines, vLines []models.Line
	for _, l := range lines {
		dy := math.Abs(l.Y2 - l.Y1)
		dx := math.Abs(l.X2 - l.X1)
		if dy < lineTolerance && dx > lineTolerance {
			// Horizontal. Normalise so X1 < X2, snap Y.
			if l.X1 > l.X2 {
				l.X1, l.X2 = l.X2, l.X1
			}
			avg := (l.Y1 + l.Y2) / 2
			l.Y1, l.Y2 = avg, avg
			hLines = append(hLines, l)
		} else if dx < lineTolerance && dy > lineTolerance {
			// Vertical. Normalise so Y1 < Y2, snap X.
			if l.Y1 > l.Y2 {
				l.Y1, l.Y2 = l.Y2, l.Y1
			}
			avg := (l.X1 + l.X2) / 2
			l.X1, l.X2 = avg, avg
			vLines = append(vLines, l)
		}
	}

	if len(hLines) < 2 || len(vLines) < 2 {
		return nil
	}

	// Cluster gridlines by coordinate.
	hYs := clusterFloats(lineYs(hLines), lineTolerance*2)
	vXs := clusterFloats(lineXs(vLines), lineTolerance*2)

	if len(hYs) < 2 || len(vXs) < 2 {
		return nil
	}

	// Sort Y descending (PDF Y grows upward → visual top-to-bottom).
	sort.Sort(sort.Reverse(sort.Float64Slice(hYs)))
	sort.Float64s(vXs)

	numRows := len(hYs) - 1
	numCols := len(vXs) - 1
	grid := makeGrid(numRows, numCols)

	for _, el := range elements {
		r := findRowDesc(el.Y, hYs)
		c := findColAsc(el.X, vXs)
		if r >= 0 && c >= 0 {
			appendCell(grid, r, c, el.Text)
		}
	}

	bbox := [4]float64{vXs[0], hYs[len(hYs)-1], vXs[len(vXs)-1], hYs[0]}
	return []models.Table{{Rows: grid, BBox: bbox}}
}

func lineYs(lines []models.Line) []float64 {
	ys := make([]float64, len(lines))
	for i, l := range lines {
		ys[i] = l.Y1
	}
	return ys
}

func lineXs(lines []models.Line) []float64 {
	xs := make([]float64, len(lines))
	for i, l := range lines {
		xs[i] = l.X1
	}
	return xs
}

// findRowDesc finds the row index in a descending-sorted Y slice.
// Row r spans hYs[r+1] (bottom) to hYs[r] (top).
func findRowDesc(y float64, hYs []float64) int {
	for r := 0; r < len(hYs)-1; r++ {
		top := hYs[r]
		bot := hYs[r+1]
		if y <= top+lineTolerance && y >= bot-lineTolerance {
			return r
		}
	}
	return -1
}

// findColAsc finds the column index in an ascending-sorted X slice.
func findColAsc(x float64, vXs []float64) int {
	for c := 0; c < len(vXs)-1; c++ {
		if x >= vXs[c]-lineTolerance && x < vXs[c+1]+lineTolerance {
			return c
		}
	}
	return -1
}

func detectFromClustering(elements []models.TextElement) []models.Table {
	medFS := medianFontSize(elements)
	if medFS < 1 {
		medFS = 12
	}

	rows := clusterByY(elements, medFS*0.4)

	// Keep only rows with ≥2 elements (single-element rows can't define columns).
	multi := make([][]models.TextElement, 0, len(rows))
	for _, row := range rows {
		if len(row) >= 2 {
			multi = append(multi, row)
		}
	}
	if len(multi) < 2 {
		return nil
	}

	boundaries := findColumnBoundaries(multi, medFS*2.0)
	if len(boundaries) < 1 {
		return nil
	}

	numCols := len(boundaries) + 1
	grid := makeGrid(len(multi), numCols)

	var minX, minY, maxX, maxY float64
	minX, minY = math.MaxFloat64, math.MaxFloat64
	maxX, maxY = -math.MaxFloat64, -math.MaxFloat64

	for r, row := range multi {
		for _, el := range row {
			col := columnFor(el.X, boundaries)
			appendCell(grid, r, col, el.Text)

			if el.X < minX {
				minX = el.X
			}
			if el.X > maxX {
				maxX = el.X
			}
			if el.Y < minY {
				minY = el.Y
			}
			if el.Y > maxY {
				maxY = el.Y
			}
		}
	}

	return []models.Table{{
		Rows: grid,
		BBox: [4]float64{minX, minY, maxX, maxY},
	}}
}

// clusterByY groups elements into rows by Y proximity (top-to-bottom).
func clusterByY(elements []models.TextElement, tolerance float64) [][]models.TextElement {
	sorted := make([]models.TextElement, len(elements))
	copy(sorted, elements)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Y > sorted[j].Y // descending Y = visual top first
	})

	var rows [][]models.TextElement
	row := make([]models.TextElement, 0, len(sorted))
	rowY := sorted[0].Y

	for _, el := range sorted {
		if math.Abs(el.Y-rowY) > tolerance {
			if len(row) > 0 {
				rows = append(rows, row)
			}
			row = nil
			rowY = el.Y
		}
		row = append(row, el)
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}

	// Sort each row left-to-right.
	for _, r := range rows {
		sort.Slice(r, func(i, j int) bool {
			return r[i].X < r[j].X
		})
	}
	return rows
}

// findColumnBoundaries examines intra-row X gaps across all rows and returns
// the X positions that consistently separate columns.
func findColumnBoundaries(rows [][]models.TextElement, gapThreshold float64) []float64 {
	var allGaps []float64
	for _, row := range rows {
		for i := 0; i < len(row)-1; i++ {
			gap := row[i+1].X - row[i].X
			if gap > gapThreshold {
				allGaps = append(allGaps, (row[i].X+row[i+1].X)/2)
			}
		}
	}
	if len(allGaps) == 0 {
		return nil
	}

	clusters := clusterFloats(allGaps, gapThreshold*0.5)

	// Keep boundaries appearing in ≥40% of rows (minimum 2).
	minCount := max(int(float64(len(rows))*0.4), 2)

	var kept []float64
	for _, boundary := range clusters {
		count := 0
		for _, row := range rows {
			for i := 0; i < len(row)-1; i++ {
				if row[i+1].X-row[i].X > gapThreshold {
					mid := (row[i].X + row[i+1].X) / 2
					if math.Abs(mid-boundary) < gapThreshold*0.5 {
						count++
						break
					}
				}
			}
		}
		if count >= minCount {
			kept = append(kept, boundary)
		}
	}

	sort.Float64s(kept)
	return kept
}

// columnFor returns the 0-based column index for a given X position.
func columnFor(x float64, boundaries []float64) int {
	for i, b := range boundaries {
		if x < b {
			return i
		}
	}
	return len(boundaries)
}

func makeGrid(rows, cols int) [][]string {
	grid := make([][]string, rows)
	for r := range grid {
		grid[r] = make([]string, cols)
	}
	return grid
}

func appendCell(grid [][]string, r, c int, text string) {
	if r < 0 || r >= len(grid) || c < 0 || c >= len(grid[r]) {
		return
	}
	if grid[r][c] != "" {
		grid[r][c] += " "
	}
	grid[r][c] += text
}

func medianFontSize(elements []models.TextElement) float64 {
	sizes := make([]float64, len(elements))
	for i, el := range elements {
		sizes[i] = el.FontSize
	}
	sort.Float64s(sizes)
	return sizes[len(sizes)/2]
}

// clusterFloats merges a slice of values into cluster centres. Adjacent
// sorted values within tolerance are averaged together.
func clusterFloats(vals []float64, tolerance float64) []float64 {
	if len(vals) == 0 {
		return nil
	}
	sort.Float64s(vals)

	var clusters []float64
	sum := vals[0]
	count := 1

	for i := 1; i < len(vals); i++ {
		if vals[i]-vals[i-1] <= tolerance {
			sum += vals[i]
			count++
		} else {
			clusters = append(clusters, sum/float64(count))
			sum = vals[i]
			count = 1
		}
	}
	clusters = append(clusters, sum/float64(count))
	return clusters
}
