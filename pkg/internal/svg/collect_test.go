package svg

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// TestCollector_TakesWhatDrawingAddsLater holds the contract a converter
// relies on: a take returns what parsing found, a later take only what the
// drawings recorded since, each warning once per code, effect and message,
// prefixed with its drawing's label.
func TestCollector_TakesWhatDrawingAddsLater(t *testing.T) {
	t.Parallel()

	const src = `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><filter id="f"/><rect width="10" height="10" filter="url(#f)"/></svg>`
	a, b := parse(t, src), parse(t, src)
	var c Collector
	c.Add(a, "in a: ")
	c.Add(b, "in b: ")
	first := c.Take()
	want := "in a: filter effects are not drawn; the filtered content is drawn unfiltered"
	if len(first) != 2 || first[0].Message != want || first[0].Code != warn.ContentUnsupported || first[0].Effect != warn.Approximated {
		t.Fatalf("first take: %+v, want the filter warning of a then of b", first)
	}
	if again := c.Take(); len(again) != 0 {
		t.Fatalf("a take with nothing new returned %+v", again)
	}

	// A drawing laid out at another size records what it meets there.
	a.warn(warn.LimitReached, warn.Omitted, "late")
	b.warn(warn.LimitReached, warn.Omitted, "late")
	late := c.Take()
	if len(late) != 2 || late[0].Message != "in a: late" || late[1].Message != "in b: late" || late[0].Effect != warn.Omitted {
		t.Fatalf("late take: %+v", late)
	}
}

// TestCollector_DeduplicatesAcrossDrawings holds the rule inline SVG
// follows: two drawings under one label report a warning once.
func TestCollector_DeduplicatesAcrossDrawings(t *testing.T) {
	t.Parallel()

	const src = `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><filter id="f"/><rect width="10" height="10" filter="url(#f)"/></svg>`
	var c Collector
	c.Add(parse(t, src), "inline: ")
	c.Add(parse(t, src), "inline: ")
	if got := c.Take(); len(got) != 1 {
		t.Fatalf("got %+v, want one warning", got)
	}
}

// TestCollect_ThroughTheContext holds that a drawing collected through a
// context reaches the collector the context carries, and that a context
// without one collects nothing.
func TestCollect_ThroughTheContext(t *testing.T) {
	t.Parallel()

	d := parse(t, `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><marker id="k"/><path d="M0 0L5 5" marker-end="url(#k)" stroke="black"/></svg>`)
	Collect(context.Background(), d, "lost: ")
	ctx, c := WithCollector(context.Background())
	Collect(ctx, d, PictureLabel("media/a.svg"))
	got := c.Take()
	if len(got) != 1 || got[0].Message != "in the SVG picture media/a.svg: markers are not drawn" {
		t.Fatalf("got %+v", got)
	}
	var none *Collector
	none.Add(d, "x")
	if ws := none.Take(); ws != nil {
		t.Fatalf("a nil collector returned %+v", ws)
	}
	if PictureLabel("") != "in an embedded SVG picture: " {
		t.Fatalf("label of a nameless picture: %q", PictureLabel(""))
	}
}
