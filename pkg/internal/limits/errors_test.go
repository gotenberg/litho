package limits_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/models"
)

func TestStops_TellsTheEndOfAConversionFromAConstruct(t *testing.T) {
	t.Parallel()
	_, b, done := limits.WithBudget(context.Background(), models.Limits{MaxImagePixels: 100, MaxConvertMemoryBytes: 1000})
	defer done()
	tooLarge := b.CheckPicture(20, 20)
	refusal := b.Charge(2000, "pictures")
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"nil":                 {err: nil, want: false},
		"cancelled":           {err: fmt.Errorf("reading: %w", context.Canceled), want: true},
		"deadline":            {err: context.DeadlineExceeded, want: true},
		"budget refusal":      {err: fmt.Errorf("decoding: %w", refusal), want: true},
		"cap":                 {err: fmt.Errorf("%w: more than 10 parts", limits.ErrInputTooLarge), want: true},
		"sub-layout pages":    {err: fmt.Errorf("%w: more than 1", limits.ErrTooManyPages), want: false},
		"picture past pixels": {err: tooLarge, want: false},
		"decode error":        {err: errors.New("png: invalid format"), want: false},
	} {
		if got := limits.Stops(tc.err); got != tc.want {
			t.Errorf("%s: Stops(%v) = %v, want %v", name, tc.err, got, tc.want)
		}
	}
	if !errors.Is(refusal, limits.ErrMemoryBudget) || !errors.Is(refusal, limits.ErrInputTooLarge) {
		t.Errorf("refusal %v does not wrap both ErrMemoryBudget and ErrInputTooLarge", refusal)
	}
}
