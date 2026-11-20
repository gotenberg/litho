package pdf

import (
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/models"
)

// DefaultLimits returns the budgets a [Reader] applies when [WithLimits] is
// not given, every field filled in.
func DefaultLimits() models.Limits {
	return limits.Default()
}

// WithLimits sets the resource budgets the document is read under. A zero
// field of l keeps its default; [Open] fails with the error [ValidateLimits]
// returns, wrapping [ErrInvalidLimits], when a field is negative or above its
// ceiling. See [models.Limits].
func WithLimits(l models.Limits) OpenOption {
	return func(o *openOpts) {
		o.limits = l
	}
}

// ValidateLimits reports the first budget of l that is negative or above its
// ceiling, as an error wrapping [ErrInvalidLimits] that names the field, its
// value and the bound it breaks. A zero budget is valid: it stands for its
// default. [Open] and [OpenFile] return this same error for the budgets
// [WithLimits] gives them, so a caller that takes budgets from configuration
// or flags can refuse a bad one before opening any document.
func ValidateLimits(l models.Limits) error {
	return limits.Validate(l)
}

// convertBudgetError logs the most memory the conversion of a name source
// under b held, as its budget accounted it, at Debug on logger, and returns
// err, the error the conversion ended with, as [Conversion.WriteTo] reports
// it: when the budget refused a charge, the refusal, which wraps
// [ErrConvertInputTooLarge], whatever error the converter saw after its
// context was cancelled. A converter that swallows a refused charge (a
// picture left out with a warning) still ends with the refusal, since the
// account it was refused on is not one the conversion may pass.
func convertBudgetError(logger *slog.Logger, name string, b *limits.Budget, err error) error {
	refusal := b.Err()
	if logger != nil {
		logger.Debug("convert: memory account", slog.Int64("peak", b.Peak()), slog.Int64("held", b.Held()), slog.Bool("refused", refusal != nil))
	}
	switch {
	case refusal == nil:
		return err
	case errors.Is(err, limits.ErrMemoryBudget):
		return err
	default:
		return fmt.Errorf("converting %s: %w", name, refusal)
	}
}

// sourceByteCost is what one byte of a conversion's source costs its
// budget: the HTML, Markdown, Office and CSV converters read the source
// whole into one []byte, and hold it, or the tree they parse it into, until
// they return.
const sourceByteCost = 1

// chargedReader reads a conversion's source, charging its budget for each
// byte read, so a source the budget cannot hold fails as it is read.
// charged counts what it charged, which the conversion gives back once
// the converter that held the source returns.
type chargedReader struct {
	r       io.Reader
	b       *limits.Budget
	charged int64
}

func (c *chargedReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if cerr := c.b.ChargeUnits(int64(n), sourceByteCost, "the source"); cerr != nil {
		return n, cerr
	}
	c.charged += int64(n) * sourceByteCost
	return n, err
}
