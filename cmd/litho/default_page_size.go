package main

import (
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/pdf"
)

// Page sizes --default-page-size accepts. ISO 32000-1, Annex C.2, Table C.1
// bounds a page between 3 by 3 and 14,400 by 14,400 units, the range
// [pdf.WithDefaultPageSize] enforces.
const (
	minDefaultPageSize = 3.0
	maxDefaultPageSize = 14400.0
)

// namedPageSizes are the sizes --default-page-size names, width by height in
// points. The ISO 216 sizes are their millimetre dimensions at 72 points to
// the inch, rounded to two decimals.
var namedPageSizes = map[string][2]float64{
	"letter": {612, 792},
	"legal":  {612, 1008},
	"a3":     {841.89, 1190.55},
	"a4":     {595.28, 841.89},
	"a5":     {419.53, 595.28},
}

// errPageSize reports a --default-page-size value the flag does not accept.
var errPageSize = errors.New("expected letter, legal, a3, a4, a5 or WIDTHxHEIGHT in points, each side between 3 and 14400")

// pageSizeFlag is the value of --default-page-size, the page size a write
// gives a page with no media box when no page of the document has one either
// (see [pdf.WithDefaultPageSize]).
type pageSizeFlag struct {
	set           bool
	width, height float64
	text          string
}

// String implements [flag.Value].
func (f *pageSizeFlag) String() string {
	if f == nil {
		return ""
	}
	return f.text
}

// Set implements [flag.Value]: a named size, or WIDTHxHEIGHT in points.
func (f *pageSizeFlag) Set(value string) error {
	name := strings.ToLower(strings.TrimSpace(value))
	size, named := namedPageSizes[name]
	if !named {
		w, h, found := strings.Cut(name, "x")
		if !found {
			return errPageSize
		}
		width, errW := strconv.ParseFloat(strings.TrimSpace(w), 64)
		height, errH := strconv.ParseFloat(strings.TrimSpace(h), 64)
		if errW != nil || errH != nil {
			return errPageSize
		}
		size = [2]float64{width, height}
	}
	// The negated range also refuses NaN, which ParseFloat accepts.
	for _, side := range size {
		if !(side >= minDefaultPageSize && side <= maxDefaultPageSize) {
			return errPageSize
		}
	}
	f.set, f.width, f.height, f.text = true, size[0], size[1], value
	return nil
}

// registerDefaultPageSizeFlag adds --default-page-size to fs and returns the
// bound value. Every command that writes a PDF offers it: a write gives a page
// with no media box a viewer uses a size of the document's pages, or this one,
// that holds what the page paints, whether it rewrites the file or appends an
// update that writes the page.
func registerDefaultPageSizeFlag(fs *flag.FlagSet) *pageSizeFlag {
	f := &pageSizeFlag{}
	fs.Var(f, "default-page-size", "Page size tried for a page with no media box after the document's own sizes, "+
		"when it holds what the page paints: letter, legal, a3, a4, a5, or WIDTHxHEIGHT in points")
	return f
}

// writeOptions returns the write option the flag selects, none when it was
// not passed.
func (f *pageSizeFlag) writeOptions() []pdf.WriteOption {
	if f == nil || !f.set {
		return nil
	}
	return []pdf.WriteOption{pdf.WithDefaultPageSize(f.width, f.height)}
}

// parsePageSizeFlag parses value as --default-page-size does, for the global
// flags of pipe, which parses its own.
func parsePageSizeFlag(value string) (*pageSizeFlag, error) {
	f := &pageSizeFlag{}
	if err := f.Set(value); err != nil {
		return nil, fmt.Errorf("invalid value %q for --default-page-size: %w", value, err)
	}
	return f, nil
}
