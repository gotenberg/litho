package odt

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// TestConvert_StartValuePastThirtyTwoBits pins that a list item's
// text:start-value past 2^31 numbers the list alike where int is 32 bits and
// where it is 64: ODF 1.3, Part 3, 19.868 types it as an unbounded
// nonNegativeInteger, and the count saturates at intconv.MaxCountStart, so
// the items after it add within 32 bits.
func TestConvert_StartValuePastThirtyTwoBits(t *testing.T) {
	t.Parallel()

	for _, start := range []string{"3000000000", "9223372036854775807"} {
		t.Run(start, func(t *testing.T) {
			t.Parallel()
			d := doc{styles: listStyles, body: `<text:list text:style-name="LN"><text:list-item text:start-value="` + start + `"><text:p>s</text:p></text:list-item>` +
				`<text:list-item><text:p>t</text:p></text:list-item></text:list>`}
			got := strings.Split(docText(convertDoc(t, d.pack(t), office.Options{})), "\n")
			if want := []string{"536870912.s", "536870913.t"}; strings.Join(got, "|") != strings.Join(want, "|") {
				t.Errorf("lines = %q, want %q", got, want)
			}
		})
	}
}
