package limits

import (
	"context"
	"errors"
	"fmt"
)

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

	// ErrInputTooLarge is returned, wrapped in an error naming the cap, by
	// every converter for a source past one of its size caps: its bytes,
	// its package entries, its elements, its cells, its boxes, the memory
	// it would hold. It names no format: pkg/pdf names the format and
	// re-exports it as pdf.ErrConvertInputTooLarge.
	ErrInputTooLarge = errors.New("conversion input too large")

	// ErrTooManyPages is returned, wrapped in an error naming the cap, by
	// every converter whose layout needs more pages than
	// [Limits.MaxConvertPages]. pkg/pdf re-exports it as
	// pdf.ErrConvertTooManyPages.
	ErrTooManyPages = errors.New("too many pages")

	// ErrMemoryBudget is returned, wrapped in an error naming what was being
	// held, when a charge would take a conversion's [Budget] past its limit.
	// It wraps [ErrInputTooLarge]: a source the budget cannot hold is too
	// large.
	ErrMemoryBudget = fmt.Errorf("%w: memory budget exceeded", ErrInputTooLarge)

	// ErrPictureTooLarge is returned, wrapped in an error naming the size,
	// by [Budget.ChargePicture] for a picture past [Limits.MaxImagePixels]
	// or without a positive side. A converter leaves such a picture out
	// with a warning.
	ErrPictureTooLarge = errors.New("picture too large")
)

// Stops reports whether err ends a conversion wherever it surfaces: the
// context ended, or the input passed a cap or the memory budget
// ([ErrInputTooLarge], which [ErrMemoryBudget] wraps). A converter returns
// such an error. Any other error one construct causes, a picture that does
// not decode, a part that does not parse, a header that passes the pages a
// sub-layout may take, becomes a warning and the conversion goes on
// without the construct.
func Stops(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrInputTooLarge)
}
