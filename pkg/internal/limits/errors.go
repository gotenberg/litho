package limits

import "errors"

// Sentinel errors for resource budgets.
var (
	// ErrInvalid is returned by [Limits.Validate] when a budget is negative
	// or above its ceiling. pkg/pdf re-exports it as pdf.ErrInvalidLimits.
	ErrInvalid = errors.New("invalid resource limits")

	// ErrPageContentTooLarge is returned, wrapped in an error naming the
	// bound, when the decoded content of one page or form XObject, summed
	// across its /Contents array, passes [Limits.MaxPageContentBytes]. Every
	// reader of page content refuses such a page with it. pkg/pdf re-exports
	// it as pdf.ErrPageContentTooLarge.
	ErrPageContentTooLarge = errors.New("page content exceeds the page content limit")
)
