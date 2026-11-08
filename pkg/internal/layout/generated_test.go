package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func TestCounterText_Styles(t *testing.T) {
	tests := []struct {
		t    css.ListStyleType
		n    int
		want string
	}{
		{css.Decimal, 3, "3"},
		{css.UpperRoman, 4, "IV"},
		{css.Disc, 2, "•"},
		{css.Square, 2, "▪"},
		{css.ListNone, 2, ""},
		{css.ListString, 2, "2"},
		{css.LowerAlpha, 0, "0"},
	}
	for _, tt := range tests {
		if got := CounterText(tt.t, tt.n); got != tt.want {
			t.Errorf("CounterText(%d, %d) = %q, want %q", tt.t, tt.n, got, tt.want)
		}
	}
}

func TestListMarkers_String(t *testing.T) {
	st := sty(display(css.ListItem), func(s *css.Style) { s.ListStyleType, s.ListStyleString = css.ListString, "> " })
	li := el(st, txt(st, "x"))
	res := run(t, doc(el(sty(padding(0, 0, 0, 40)), li)), page(200, 100))
	got, pos := lineTexts(res, 1)
	if len(got) != 1 || got[0] != "> x" || !near(pos[0][0], 20) {
		t.Errorf("lines %q at %v, want the string marker ending at 40", got, pos)
	}
}

func TestListMarkers_MarkerNode(t *testing.T) {
	st := sty(display(css.ListItem))
	ms := sty(fg(css.Color{R: 1, A: 1}))
	tests := []struct {
		name   string
		marker *Node
		want   string
	}{
		{"content", el(ms, txt(ms, "1.2"), txt(ms, " ")), "1.2 x"},
		// content: normal draws the disc as a shape, no text.
		{"normal", el(ms), "x"},
		{"none", el(ms, txt(ms, "")), "x"},
	}
	for _, tt := range tests {
		li := el(st, txt(st, "x"))
		li.ListValue = 7
		li.Marker = tt.marker
		res := run(t, doc(el(sty(padding(0, 0, 0, 40)), li)), page(200, 100))
		got, _ := lineTexts(res, 1)
		if len(got) != 1 || got[0] != tt.want {
			t.Errorf("%s: lines %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestListMarkers_MarkerStyleColour(t *testing.T) {
	st := sty(display(css.ListItem), func(s *css.Style) { s.ListStylePosition = css.Inside })
	ms := sty(fg(css.Color{R: 1, A: 1}))
	li := el(st, txt(st, "x"))
	li.Marker = el(ms, txt(ms, "M"))
	res := run(t, doc(li), page(40, 20))
	// The marker draws red in its own style, the content black.
	assertPixels(t, res, 1, "\n"+
		"RRRRRRRRRRKKKKKKKKKK____________________\n"+
		"RRRRRRRRRRKKKKKKKKKK____________________\n"+
		"RRRRRRRRRRKKKKKKKKKK____________________\n"+
		"RRRRRRRRRRKKKKKKKKKK____________________\n"+
		"RRRRRRRRRRKKKKKKKKKK____________________\n"+
		"RRRRRRRRRRKKKKKKKKKK____________________\n"+
		"RRRRRRRRRRKKKKKKKKKK____________________\n"+
		"RRRRRRRRRRKKKKKKKKKK____________________\n"+
		"RRRRRRRRRRKKKKKKKKKK____________________\n"+
		"RRRRRRRRRRKKKKKKKKKK____________________\n"+
		"________________________________________\n"+
		"________________________________________\n"+
		"________________________________________\n"+
		"________________________________________\n"+
		"________________________________________\n"+
		"________________________________________\n"+
		"________________________________________\n"+
		"________________________________________\n"+
		"________________________________________\n"+
		"________________________________________\n")
}

func TestListMarkers_InlineListItem(t *testing.T) {
	st := inlineSty(display(css.InlineListItem), func(s *css.Style) { s.ListStyleType = css.Decimal })
	a := el(st, txt(st, "a"))
	b := el(st, txt(st, "b"))
	b.ListValue = 5
	res := run(t, doc(el(sty(), a, b)), page(400, 100))
	got, _ := lineTexts(res, 1)
	if len(got) != 1 || got[0] != "1. a5. b" {
		t.Errorf("lines %q, want both inline items with inside markers on one line", got)
	}
}
