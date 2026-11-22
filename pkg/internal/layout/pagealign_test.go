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
