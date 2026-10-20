package typography

import (
	"context"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
	"github.com/gotenberg/litho/pkg/models"
)

// zeroWidthToUnicode returns a /ToUnicode CMap whose only codespace range is
// zero bytes wide and whose only mapping is keyed by the empty string. It is
// built directly rather than parsed so the test reaches the mapper and the
// glyph loop even when the parser refuses to store such a range.
func zeroWidthToUnicode() *cmap.CMap {
	return &cmap.CMap{
		CodeSpaceRanges: []cmap.CodeSpaceRange{{Low: []byte{}, High: []byte{}}},
		Mapping:         map[string]string{"": "A"},
	}
}

// TestDecodeNextChar_ZeroWidthToUnicodeAdvances pins that the byte count
// [decodeNextChar] reports is never zero. [processText] advances the string by it,
// so a zero repeats the same code forever.
func TestDecodeNextChar_ZeroWidthToUnicodeAdvances(t *testing.T) {
	t.Parallel()

	font := &Font{
		Subtype:    "Type1",
		ToUnicode:  zeroWidthToUnicode(),
		UnitsPerEm: 1000,
	}

	_, n, _ := decodeNextChar(font, []byte("AB"))
	if n < 1 {
		t.Fatalf("decodeNextChar reported n=%d, want >= 1", n)
	}
}

// TestProcessText_ZeroWidthToUnicodeTerminates is the denial-of-service
// regression. A crafted /ToUnicode reaches this loop through the public
// Reader.ExtractText path, and the context poll inside [processText] only
// returns on cancellation, so an uncancelled run has no escape: before the
// guards landed this call never returned.
func TestProcessText_ZeroWidthToUnicodeTerminates(t *testing.T) {
	t.Parallel()

	font := &Font{
		Subtype:    "Type1",
		ToUnicode:  zeroWidthToUnicode(),
		UnitsPerEm: 1000,
	}
	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})

	done := make(chan error, 1)
	go func() {
		var elements []models.TextElement
		done <- processText(context.Background(), ast.PdfString("AB"), font, machine, &elements, nil)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("processText: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("processText did not terminate: the glyph loop consumed no bytes")
	}
}

// TestProcessText_WellFormedToUnicodeStillExtracts is the control. A guard
// that made [processText] terminate by refusing to decode anything would pass
// the test above, so pin that a normal font still produces elements.
func TestProcessText_WellFormedToUnicodeStillExtracts(t *testing.T) {
	t.Parallel()

	src := "begincmap\n" +
		"1 begincodespacerange\n<00> <FF>\nendcodespacerange\n" +
		"1 beginbfchar\n<41> <0041>\nendbfchar\n" +
		"endcmap\n"

	cm, err := cmap.Parse(context.Background(), []byte(src))
	if err != nil {
		t.Fatalf("cmap.Parse: %v", err)
	}

	font := &Font{
		Subtype:    "Type1",
		ToUnicode:  cm,
		UnitsPerEm: 1000,
	}
	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})

	var elements []models.TextElement
	if err := processText(context.Background(), ast.PdfString("A"), font, machine, &elements, nil); err != nil {
		t.Fatalf("processText: %v", err)
	}
	if len(elements) != 1 {
		t.Fatalf("elements = %d, want 1", len(elements))
	}
	if elements[0].Text != "A" {
		t.Fatalf("elements[0].Text = %q, want %q", elements[0].Text, "A")
	}
}
