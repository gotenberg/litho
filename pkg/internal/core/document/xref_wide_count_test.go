package document

import (
	"bytes"
	"testing"
)

// TestValidateXRefTableFormat_CountPastThirtyTwoBits pins that a subsection
// count whose entries the table cannot hold ends the format scan with no
// violation where int is 32 bits and where it is 64: the count is checked
// against the bytes left before it is multiplied by the 20 bytes of an entry,
// a product that wraps from 107,374,183 entries where int is 32 bits and for
// the largest 64-bit count where it is 64.
func TestValidateXRefTableFormat_CountPastThirtyTwoBits(t *testing.T) {
	t.Parallel()

	for _, count := range []string{"107374183", "3000000000", "9223372036854775807"} {
		t.Run(count, func(t *testing.T) {
			t.Parallel()
			body := "xref\n0 " + count + "\n0000000000 65535 f\r\n0 1\n0000000000 65535 f\r\ntrailer\n<< /Size 1 >>\n"
			if got := (&Document{r: bytes.NewReader([]byte(body))}).ValidateXRefTableFormat(0); len(got) != 0 {
				t.Errorf("violations = %v, want none", got)
			}
		})
	}
}
