package models

// Table represents a table detected from spatial analysis of a PDF page.
// Concurrent reads of a value are safe; a write needs synchronization.
type Table struct {
	// Rows holds the cell text, top row first and left cell first in each
	// row.
	Rows [][]string `json:"rows"`
	// BBox is the box [minX minY maxX maxY] around the table, in default user
	// space.
	BBox [4]float64 `json:"bbox"`
}
