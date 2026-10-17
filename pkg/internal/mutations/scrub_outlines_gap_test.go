package mutations

import (
	"context"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// TestScrubOutlineTitles_WalksPastAnItemThatDoesNotResolve pins the backward
// pass. Item 22 dangles or does not parse, so /Next from item 21 reaches
// nothing past it, but item 23 is still the chain's /Last and the rewrite
// copies it with its title. The scrub reaches it through /Prev and empties
// it, where before it stopped at 22 and left the title for the verification
// scan to refuse.
func TestScrubOutlineTitles_WalksPastAnItemThatDoesNotResolve(t *testing.T) {
	t.Parallel()
	for name, gap := range map[string]string{
		"dangling":  "",
		"unparsed":  "22 0 obj\n10P1 garbage\nendobj\n",
		"in a kid":  "",
		"not first": "",
		"a number":  "",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			outlines := "20 0 obj\n<< /Type /Outlines /First 21 0 R /Last 23 0 R /Count 3 >>\nendobj\n" +
				"21 0 obj\n<< /Title (Intro) /Parent 20 0 R /Next 22 0 R >>\nendobj\n" +
				gap +
				"23 0 obj\n<< /Title (the SECRET chapter) /Parent 20 0 R /Prev 22 0 R >>\nendobj\n"
			want := 23
			switch name {
			case "in a kid":
				// The gap sits in item 21's children.
				outlines = "20 0 obj\n<< /Type /Outlines /First 21 0 R /Last 21 0 R /Count 1 >>\nendobj\n" +
					"21 0 obj\n<< /Title (Intro) /Parent 20 0 R /First 24 0 R /Last 26 0 R >>\nendobj\n" +
					"24 0 obj\n<< /Title (One) /Parent 21 0 R /Next 25 0 R >>\nendobj\n" +
					"26 0 obj\n<< /Title (the SECRET section) /Parent 21 0 R /Prev 25 0 R >>\nendobj\n"
				want = 26
			case "a number":
				// /First is a number, no outline item at all (ISO 32000-1,
				// Table 153), so it neither holds a title nor hides one.
				outlines = "20 0 obj\n<< /Type /Outlines /First 7 /Last 23 0 R /Count 1 >>\nendobj\n" +
					"23 0 obj\n<< /Title (the SECRET chapter) /Parent 20 0 R /Prev 7 >>\nendobj\n"
			case "not first":
				// The quoting item past the gap is not the chain's last.
				outlines = "20 0 obj\n<< /Type /Outlines /First 21 0 R /Last 24 0 R /Count 4 >>\nendobj\n" +
					"21 0 obj\n<< /Title (Intro) /Parent 20 0 R /Next 22 0 R >>\nendobj\n" +
					"23 0 obj\n<< /Title (the SECRET chapter) /Parent 20 0 R /Prev 22 0 R /Next 24 0 R >>\nendobj\n" +
					"24 0 obj\n<< /Title (Outro) /Parent 20 0 R /Prev 23 0 R >>\nendobj\n"
			}
			doc := outlineFixture(t, outlines)
			alloc := writer.NewObjAllocator(50)

			scrubbed, err := ScrubOutlineTitles(context.Background(), alloc, doc, []string{"SECRET"})
			if err != nil {
				t.Fatalf("ScrubOutlineTitles: %v", err)
			}
			if !slices.Equal(scrubbed, []int{want}) {
				t.Errorf("scrubbed = %v, want [%d]", scrubbed, want)
			}
		})
	}
}
