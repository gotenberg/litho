package sheet

// The codes of the warnings the builder records (compose.Warning.Code).
const (
	// CodeFormat is a number format code the builder does not read.
	CodeFormat = "sheet-number-format"
	// CodeRotation is rotated text drawn approximately.
	CodeRotation = "sheet-rotation"
	// CodeTooManyCells is a page with more cells than the builder lays
	// out.
	CodeTooManyCells = "sheet-too-many-cells"
	// CodeChart is a chart not drawn.
	CodeChart = "sheet-chart"
	// CodeField is a header or footer field that shows nothing.
	CodeField = "sheet-field"
	// CodeHeader is a header or footer that could not be laid out.
	CodeHeader = "sheet-header"
	// CodeRightToLeft is a right-to-left sheet laid out left to right.
	CodeRightToLeft = "sheet-right-to-left"
	// CodeTooManyPages is a sheet with more pages than the builder lays
	// out.
	CodeTooManyPages = "sheet-too-many-pages"
	// CodeConditional is a conditional formatting rule not applied.
	CodeConditional = "sheet-conditional"
	// CodePivot is a pivot table whose output is left as the file saved
	// it, or computed in part.
	CodePivot = "sheet-pivot"
	// CodeFormula is a formula not computed, which prints its stored
	// result.
	CodeFormula = "sheet-formula"
)
