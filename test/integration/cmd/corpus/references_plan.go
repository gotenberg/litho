package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture"
)

// errBorrowedReference reports an options.json whose reference_of names no
// fixture of its corpus that has a reference of its own.
var errBorrowedReference = errors.New("invalid reference_of")

// referencePlan is what the options of a corpus's sources say of their
// references, for an HTML or Markdown source and an Office source alike
// (see [fixture.LoadReference]).
type referencePlan struct {
	// borrowed maps a fixture whose options.json names reference_of to the
	// fixture whose reference stands for its own: the diff cell scores it
	// against that fixture's goldens, and no reference is built for it.
	borrowed map[string]string
	// lithoOnly maps a fixture that sets litho's own options and names
	// no reference_of to those options' keys: no reference engine takes
	// them, so none is built for it and the diff cell leaves it unscored.
	lithoOnly map[string][]string
}

// ownsNoReference reports whether no reference is built for the fixture id:
// it borrows another's, or sets litho's own options.
func (p referencePlan) ownsNoReference(id string) bool {
	_, borrows := p.borrowed[id]
	_, lithoOnly := p.lithoOnly[id]
	return borrows || lithoOnly
}

// lithoOnlyNotice is what a run says of the fixtures p leaves without a
// reference, one line naming each with the options it sets; "" when there
// are none.
func (p referencePlan) lithoOnlyNotice(corpus string, fixtures []Fixture) string {
	var named []string
	for _, fx := range fixtures {
		if keys, ok := p.lithoOnly[fx.ID]; ok {
			named = append(named, fmt.Sprintf("%s (%s)", fx.ID, strings.Join(keys, ", ")))
		}
	}
	if len(named) == 0 {
		return ""
	}
	return fmt.Sprintf("%s: %d fixtures set litho's own options, which no reference engine takes, and name no reference_of, not scored: %s",
		corpus, len(named), summarize(named))
}

// planReferences reads the reference_of and the litho-only options of each
// fixture's source (see [referencePlan]). It fails when a reference_of names
// the fixture itself, a fixture the corpus does not list, a fixture that
// borrows in turn or one that has no reference for setting litho's own
// options. A source that does not load borrows nothing and sets nothing: the
// cells that convert it report why.
func planReferences(fixtures []Fixture) (referencePlan, error) {
	ids := make(map[string]bool, len(fixtures))
	for _, fx := range fixtures {
		ids[fx.ID] = true
	}
	p := referencePlan{borrowed: map[string]string{}, lithoOnly: map[string][]string{}}
	for _, fx := range fixtures {
		ref, err := fixture.LoadReference(fx.PDFPath)
		if err != nil {
			continue
		}
		switch {
		case ref.Of != "":
			if ref.Of == fx.ID || !ids[ref.Of] {
				return referencePlan{}, fmt.Errorf("%w: %s names %q, not another fixture of its corpus", errBorrowedReference, fx.ID, ref.Of)
			}
			p.borrowed[fx.ID] = ref.Of
		case ref.Unreferenced():
			p.lithoOnly[fx.ID] = ref.LithoOptions
		}
	}
	for _, fx := range fixtures {
		other, ok := p.borrowed[fx.ID]
		if !ok {
			continue
		}
		if _, chained := p.borrowed[other]; chained {
			return referencePlan{}, fmt.Errorf("%w: %s names %s, which borrows a reference too", errBorrowedReference, fx.ID, other)
		}
		if keys, ok := p.lithoOnly[other]; ok {
			return referencePlan{}, fmt.Errorf("%w: %s names %s, which has no reference: it sets %s", errBorrowedReference, fx.ID, other, strings.Join(keys, ", "))
		}
	}
	return p, nil
}

// sourceReference reads what the source at path says of its reference for
// the reference builder: borrows when it names reference_of, litho the
// litho-only keys it sets when it names none. A source that does not load
// reads as neither, and its build reports why.
func sourceReference(path string) (borrows bool, litho []string) {
	ref, err := fixture.LoadReference(path)
	if err != nil {
		return false, nil
	}
	if ref.Of != "" {
		return true, nil
	}
	return false, ref.LithoOptions
}
