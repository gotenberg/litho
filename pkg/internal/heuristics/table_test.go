package heuristics

import (
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// makeElement creates a visible TextElement at the given coordinates.
func makeElement(text string, x, y, fontSize float64) models.TextElement {
	return models.TextElement{Text: text, X: x, Y: y, FontSize: fontSize}
}

func TestDetectTables_LineBased(t *testing.T) {
	// A simple 2×2 grid formed by 3 horizontal + 3 vertical stroked lines.
	//
	//   x=50     x=200    x=350
	//    |         |         |
	//    +-------- +-------- + y=700  (top)
	//    |  A      |  B      |
	//    +-------- +-------- + y=680
	//    |  C      |  D      |
	//    +-------- +-------- + y=660  (bottom)

	lines := []models.Line{
		// Horizontal lines
		{X1: 50, Y1: 700, X2: 350, Y2: 700},
		{X1: 50, Y1: 680, X2: 350, Y2: 680},
		{X1: 50, Y1: 660, X2: 350, Y2: 660},
		// Vertical lines
		{X1: 50, Y1: 660, X2: 50, Y2: 700},
		{X1: 200, Y1: 660, X2: 200, Y2: 700},
		{X1: 350, Y1: 660, X2: 350, Y2: 700},
	}

	elements := []models.TextElement{
		makeElement("A", 80, 690, 10),
		makeElement("B", 230, 690, 10),
		makeElement("C", 80, 670, 10),
		makeElement("D", 230, 670, 10),
	}

	tables, err := DetectTables(elements, lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(tables))
	}

	tbl := tables[0]
	if len(tbl.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(tbl.Rows))
	}
	if len(tbl.Rows[0]) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(tbl.Rows[0]))
	}

	// Row 0 is the top row (Y=700→680).
	assertCell(t, tbl, 0, 0, "A")
	assertCell(t, tbl, 0, 1, "B")
	assertCell(t, tbl, 1, 0, "C")
	assertCell(t, tbl, 1, 1, "D")
}

func TestDetectTables_SpatialClustering(t *testing.T) {
	// No lines — rely on spatial gaps to infer a 3-row × 2-column table.
	//
	//   Name        Score
	//   Alice       100
	//   Bob         200

	elements := []models.TextElement{
		makeElement("Name", 50, 700, 12),
		makeElement("Score", 250, 700, 12),
		makeElement("Alice", 50, 680, 12),
		makeElement("100", 250, 680, 12),
		makeElement("Bob", 50, 660, 12),
		makeElement("200", 250, 660, 12),
	}

	tables, err := DetectTables(elements, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(tables))
	}

	tbl := tables[0]
	if len(tbl.Rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(tbl.Rows))
	}
	if len(tbl.Rows[0]) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(tbl.Rows[0]))
	}

	assertCell(t, tbl, 0, 0, "Name")
	assertCell(t, tbl, 0, 1, "Score")
	assertCell(t, tbl, 1, 0, "Alice")
	assertCell(t, tbl, 1, 1, "100")
	assertCell(t, tbl, 2, 0, "Bob")
	assertCell(t, tbl, 2, 1, "200")
}

func TestDetectTables_NoTable(t *testing.T) {
	// Single paragraph of text — no table structure.
	elements := []models.TextElement{
		makeElement("Hello", 50, 700, 12),
		makeElement("world", 90, 700, 12),
	}

	tables, err := DetectTables(elements, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tables) != 0 {
		t.Errorf("expected no tables, got %d", len(tables))
	}
}

func TestDetectTables_TooFewElements(t *testing.T) {
	elements := []models.TextElement{
		makeElement("A", 50, 700, 12),
	}

	tables, err := DetectTables(elements, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tables != nil {
		t.Errorf("expected nil, got %v", tables)
	}
}

func TestDetectTables_HiddenElementsFiltered(t *testing.T) {
	elements := []models.TextElement{
		{Text: "A", X: 50, Y: 700, FontSize: 12, Hidden: true},
		{Text: "B", X: 250, Y: 700, FontSize: 12, Hidden: true},
		{Text: "C", X: 50, Y: 680, FontSize: 12, Hidden: true},
		{Text: "D", X: 250, Y: 680, FontSize: 12, Hidden: true},
	}

	tables, err := DetectTables(elements, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tables != nil {
		t.Errorf("expected nil when all elements hidden, got %v", tables)
	}
}

func assertCell(t *testing.T, tbl models.Table, row, col int, expected string) {
	t.Helper()
	if row >= len(tbl.Rows) || col >= len(tbl.Rows[row]) {
		t.Errorf("cell [%d][%d] out of bounds", row, col)
		return
	}
	if tbl.Rows[row][col] != expected {
		t.Errorf("cell [%d][%d]: got %q, want %q", row, col, tbl.Rows[row][col], expected)
	}
}
