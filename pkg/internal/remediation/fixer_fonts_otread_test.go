package remediation

import (
	"context"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestRemediatePDFA_RepairsWidthsAsTheFontReadsItsProgram pins that the width
// repair reads an OpenType program as the font that embeds it reads it, which
// the width check (ISO 19005-2, 6.2.11.5 t1) weighs too: a Type 1 or
// CIDFontType0 font through its 'CFF ' table, a TrueType or CIDFontType2 font
// as a TrueType program. The litho fixture fontotread_2b.pdf plants programs
// whose CFF charstrings advance 600 and whose 'hmtx' states 500. A repair
// that read them by their outlines rewrote the widths of the conforming fonts
// to the other reading's values, and the output still failed the rule.
func TestRemediatePDFA_RepairsWidthsAsTheFontReadsItsProgram(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../../../test/integration/corpus/litho/pdfs/fontotread_2b.pdf")
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	doc, alloc := newTestDoc(t, data)
	if _, err := RemediatePDFA(context.Background(), doc, alloc, models.PDFA2b, Options{}); err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	after, _ := revalidatePDFA(t, doc, alloc, models.PDFA2b)
	for _, v := range after {
		if v.Rule.Clause == "6.2.11.5" {
			t.Errorf("the output still fails 6.2.11.5 t%d [%s]", v.Rule.TestNumber, v.Context)
		}
	}
}
