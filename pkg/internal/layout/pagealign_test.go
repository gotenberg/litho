package layout

import "testing"

// TestPageAlign moves a page's flow down by the fraction of the room its
// content leaves that its page box's AlignContent gives, and a page its
// content fills not at all.
func TestPageAlign(t *testing.T) {
	for _, tt := range []struct {
		name  string
		align float64
		want  string
	}{
		{"top", 0, "a@0,8 "},
		{"centre", 0.5, "a@0,53 "},
		{"bottom", 1, "a@0,98 "},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pb := page(100, 100)
			pb.AlignContent = tt.align
			if got := linePlaces(run(t, doc(wpara("a")), pb), 1); got != tt.want {
				t.Errorf("lines %s, want %s", got, tt.want)
			}
		})
	}
	pb := page(100, 30)
	pb.AlignContent = 1
	res := run(t, doc(wpara("a"), wpara("b"), wpara("c"), wpara("d")), pb)
	if got, want := linePlaces(res, 1), "a@0,8 b@0,18 c@0,28 "; got != want {
		t.Errorf("full page: lines %s, want %s", got, want)
	}
}

// TestPageSpread spreads the room a page's content leaves between its
// blocks: the first stays at the top, the last ends at the bottom, each
// gap grows by an equal share; a page its content runs past, and a page
// of one block, stay as they are.
func TestPageSpread(t *testing.T) {
	pb := page(100, 100)
	pb.SpreadContent = true
	pb.AlignContent = 1
	if got, want := linePlaces(run(t, doc(wpara("a"), wpara("b"), wpara("c")), pb), 1), "a@0,8 b@0,53 c@0,98 "; got != want {
		t.Errorf("lines %s, want %s", got, want)
	}
	if got, want := linePlaces(run(t, doc(wpara("a")), pb), 1), "a@0,8 "; got != want {
		t.Errorf("one block: lines %s, want %s", got, want)
	}
	pb = page(100, 30)
	pb.SpreadContent = true
	res := run(t, doc(wpara("a"), wpara("b"), wpara("c"), wpara("d"), wpara("e")), pb)
	if got, want := linePlaces(res, 1), "a@0,8 b@0,18 c@0,28 "; got != want {
		t.Errorf("full page: lines %s, want %s", got, want)
	}
	if got, want := linePlaces(res, 2), "d@0,8 e@0,28 "; got != want {
		t.Errorf("last page: lines %s, want %s", got, want)
	}
}
