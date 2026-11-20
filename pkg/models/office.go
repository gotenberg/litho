package models

// TrackedChanges is how the changes a DOCX or ODT source tracks are printed
// (ECMA-376 Part 1, 17.13.5; ODF 1.3 Part 3, 5.5).
type TrackedChanges int

const (
	// TrackedChangesAccepted prints the text as it reads once every change
	// is accepted: insertions kept, deletions left out. It is the zero value
	// and the default.
	TrackedChangesAccepted TrackedChanges = iota
	// TrackedChangesRejected prints the text as it read before the changes:
	// insertions left out, deletions kept. A change of formatting keeps its
	// new formatting.
	TrackedChangesRejected
	// TrackedChangesMarked prints both, each marked as LibreOffice shows
	// tracked changes: an insertion underlined, a deletion struck through
	// and text whose formatting changed in bold, each in the colour of its
	// author, taken in turn from nine colours in the order the authors first
	// appear; moved text double underlined where it went and double struck
	// through where it was, in green; an inserted table row on a light blue
	// ground and a deleted one on a pink ground. A change bar, a black
	// hairline left of the text column, stands beside each line a change
	// touches, the lines of a text box excepted.
	TrackedChangesMarked
)

// Comments is how the comments of a DOCX or ODT source are written
// (ECMA-376 Part 1, 17.13.4; ODF 1.3 Part 3, 14.1).
type Comments int

const (
	// CommentsHidden leaves the comments out, as Word and LibreOffice
	// print a document. It is the zero value and the default.
	CommentsHidden Comments = iota
	// CommentsAnnotations writes each comment as an annotation a PDF
	// viewer lists and opens, over the text it covers; the pages print as
	// they do without it.
	CommentsAnnotations
)

// Recalculation is when the formulas of an XLSX or ODS source are computed
// again rather than shown as the value the file stores.
type Recalculation int

const (
	// RecalculateAsNeeded computes a formula again when the file gives no
	// reason to trust its stored value: every formula of an ODS source; in
	// an XLSX source, a formula without a stored value, and every formula of
	// a workbook that neither Microsoft Excel nor LibreOffice wrote and none
	// of whose formulas stores a number other than 0. It is the zero value
	// and the default.
	RecalculateAsNeeded Recalculation = iota
	// RecalculateNever shows the value the file stores for every formula,
	// and nothing for a formula without one.
	RecalculateNever
	// RecalculateAlways computes every formula again. A formula litho does
	// not compute keeps its stored value, and so does a cell that shares
	// the formula of another.
	RecalculateAlways
)
