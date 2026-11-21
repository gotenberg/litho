package office

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// FirstPageOnly cuts res, a text document laid out on one page under
// [Options.SinglePage], to that page: content taller than limit, the
// tallest single page (see [Options.FittedLimit]), flows on to a second
// page, which is left out, as an HTML document's single page leaves out
// what follows its first page. It returns the warning that says so, nil
// when the first page holds everything.
func FirstPageOnly(res *layout.Result, limit float64) *typeset.Warning {
	if len(res.Pages) <= 1 {
		return nil
	}
	res.Pages = res.Pages[:1]
	w := SinglePageCut(limit)
	return &w
}

// SinglePageCut returns the warning of a single page whose content passes
// limit, the tallest single page: what lies past it is left out.
func SinglePageCut(limit float64) typeset.Warning {
	return typeset.Warning{
		Code:    warn.LimitReached,
		Effect:  warn.Omitted,
		Message: fmt.Sprintf("the content past %.0f points, the tallest single page, is left out", limit),
	}
}
