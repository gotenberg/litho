package html

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// TestRequiredResources_StopBeforeLayout checks that a required stylesheet
// that does not load ends the conversion before anything is laid out, and
// that the conversion gives back what it charged to the budget, as on any
// error path.
func TestRequiredResources_StopBeforeLayout(t *testing.T) {
	t.Parallel()

	ctx, mem, done := limits.WithBudget(context.Background(), limits.Limits{})
	defer done()
	laidOut := 0
	lay := func(ctx context.Context, root *layout.Node, fonts *text.Fonts, opts layout.Options) (*layout.Result, error) {
		laidOut++
		return stubLayout(ctx, root, fonts, opts)
	}
	src := `<link rel=stylesheet href=gone.css><p>x<img src=gone.png alt=a>`
	o := Options{Resources: fstest.MapFS{}, RequiredResources: true}
	_, err := convert(ctx, strings.NewReader(src), testFonts(t), o, lay)
	if !errors.Is(err, ErrResourceUnavailable) {
		t.Fatalf("got %v, want an error wrapping ErrResourceUnavailable", err)
	}
	if !strings.Contains(err.Error(), "stylesheet gone.css") || strings.Contains(err.Error(), "gone.png") {
		t.Errorf("the error %q does not name the stylesheet alone", err)
	}
	if laidOut != 0 {
		t.Errorf("the document was laid out %d times after its stylesheet failed", laidOut)
	}
	if held := mem.Held(); held != 0 {
		t.Errorf("the failed conversion holds %d bytes of its budget", held)
	}
}

// TestRequiredResources_StopInLayout checks that a picture the layout
// loads, a CSS image, ends the conversion when it does not load, that the
// error is the resource's, not the context's it cancels, and that the
// pages of a document never returned are not charged: the conversion
// holds less of its budget than the same document converted without the
// option, whose pages it holds until they are written.
func TestRequiredResources_StopInLayout(t *testing.T) {
	t.Parallel()

	src := `<div style="background-image: url(gone.png); height: 10px"></div><p>x`
	held := func(required bool) (int64, error) {
		ctx, mem, done := limits.WithBudget(context.Background(), limits.Limits{})
		defer done()
		o := Options{Resources: fstest.MapFS{}, RequiredResources: required}
		_, err := Convert(ctx, strings.NewReader(src), testFonts(t), o)
		return mem.Held(), err
	}
	failed, err := held(true)
	if !errors.Is(err, ErrResourceUnavailable) || errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want an error wrapping ErrResourceUnavailable alone", err)
	}
	written, err := held(false)
	if err != nil {
		t.Fatal(err)
	}
	if failed >= written {
		t.Errorf("the failed conversion holds %d bytes of its budget, the written one %d", failed, written)
	}
}

// TestRequiredResources_CallerCancelStaysTheCallers checks that a context
// the caller cancels ends a conversion with required resources with the
// context's error.
func TestRequiredResources_CallerCancelStaysTheCallers(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := convert(ctx, strings.NewReader(`<p>x`), testFonts(t), Options{RequiredResources: true}, stubLayout)
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrResourceUnavailable) {
		t.Fatalf("got %v, want the context's error", err)
	}
}

// TestLoadFailed_KeepsTheStopsApart checks that a refusal of the budget
// and the end of the context are no failed load: they stop a conversion
// on their own, required or not.
func TestLoadFailed_KeepsTheStopsApart(t *testing.T) {
	t.Parallel()

	for _, err := range []error{context.Canceled, context.DeadlineExceeded, limits.ErrMemoryBudget, nil} {
		if loadFailed(err) {
			t.Errorf("loadFailed(%v) = true", err)
		}
	}
	for _, err := range []error{errNoResources, errOutsideRoot, errNoNetwork, errFileURL, errBadDataURL, errTooLarge, errBudget, errTooManyRequests, &httpStatusError{code: 500}} {
		if !loadFailed(err) {
			t.Errorf("loadFailed(%v) = false", err)
		}
	}
}
