package sheet

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// Printed returns, for each sheet of a book in book order, whether it
// prints under opts: the sheets opts.Sheets names, matched exactly, or
// the sheets at the 1-based positions opts.SheetNumbers lists, hidden
// ones included either way; without a selection, every sheet when
// opts.HiddenSheets asks, else every sheet hidden does not mark, and the
// first sheet of a book that hides them all, as Calc shows a book whose
// sheets are all hidden. names and hidden, of equal length, are the
// sheets' names and hidden flags. A name or a number the book has no
// sheet for fails with office.ErrInvalidOptions naming it.
func Printed(names []string, hidden []bool, opts office.Options) ([]bool, error) {
	if len(names) != len(hidden) {
		return nil, fmt.Errorf("%w: %d sheet names for %d hidden flags", office.ErrInvalidOptions, len(names), len(hidden))
	}
	printed := make([]bool, len(names))
	switch {
	case opts.Sheets != nil:
		for _, want := range opts.Sheets {
			found := false
			for i, name := range names {
				if name == want {
					printed[i], found = true, true
				}
			}
			if !found {
				return nil, fmt.Errorf("%w: the workbook has no sheet named %q", office.ErrInvalidOptions, want)
			}
		}
	case opts.SheetNumbers != nil:
		for _, n := range opts.SheetNumbers {
			if n < 1 || n > len(names) {
				return nil, fmt.Errorf("%w: the workbook has no sheet %d: it has %d", office.ErrInvalidOptions, n, len(names))
			}
			printed[n-1] = true
		}
	default:
		for i, h := range hidden {
			printed[i] = opts.HiddenSheets || !h
		}
		if !printedAny(printed) && len(printed) > 0 {
			printed[0] = true
		}
	}
	return printed, nil
}

// printedSheets returns which sheets of doc print under opts ([Printed]);
// a nil sheet never prints.
func printedSheets(doc *Document, opts office.Options) ([]bool, error) {
	names := make([]string, len(doc.Sheets))
	hidden := make([]bool, len(doc.Sheets))
	for i, sh := range doc.Sheets {
		if sh == nil {
			hidden[i] = true
			continue
		}
		names[i], hidden[i] = sh.Name, sh.Hidden
	}
	printed, err := Printed(names, hidden, opts)
	if err != nil {
		return nil, err
	}
	for i, sh := range doc.Sheets {
		if sh == nil {
			printed[i] = false
		}
	}
	if !printedAny(printed) {
		// A book that hides every sheet shows its first one, a nil
		// sheet skipped.
		for i, sh := range doc.Sheets {
			if sh != nil {
				printed[i] = true
				break
			}
		}
	}
	return printed, nil
}

// printedAny reports a sheet that prints.
func printedAny(printed []bool) bool {
	for _, p := range printed {
		if p {
			return true
		}
	}
	return false
}
