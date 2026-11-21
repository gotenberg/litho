package office

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// FirstPageOnly cuts res, a text document laid out on one page under
// [Options.SinglePage], to that page: content taller than [MaxPageSide],
// the longest side a page may have (ISO 32000-1, Annex C.2), flows on to
// a second page, which is left out, as an HTML document's single page
// leaves out what follows its first page. It returns the warning that
// says so, nil when the first page holds everything.
func FirstPageOnly(res *layout.Result) *typeset.Warning {
	if len(res.Pages) <= 1 {
		return nil
	}
	res.Pages = res.Pages[:1]
	w := SinglePageCut()
	return &w
}

// SinglePageCut returns the warning of a single page whose content passes
// [MaxPageSide]: what lies past it is left out.
func SinglePageCut() typeset.Warning {
	return typeset.Warning{
		Code:    warn.LimitReached,
		Effect:  warn.Omitted,
		Message: fmt.Sprintf("the content past %d points, the tallest page a PDF may have, is left out of the single page", MaxPageSide),
	}
}
