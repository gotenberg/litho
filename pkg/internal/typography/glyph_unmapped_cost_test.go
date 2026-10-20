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

// TestProcessText_ManyUnmappedCodesStayFast is the CPU regression for a large
// /ToUnicode CMap. Its 61440 two-byte entries leave the codes F000 to FFFF in
// the codespace but unmapped, and the shown string repeats them 50000 times.
// Each unmapped lookup once scanned the whole mapping for a one-byte key, so
// the string cost 3 billion map iterations; it now costs one lookup per code.
func TestProcessText_ManyUnmappedCodesStayFast(t *testing.T) {
	t.Parallel()

	src := "begincmap\n" +
		"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n" +
		"1 beginbfrange\n<0000> <EFFF> <4E00>\nendbfrange\n" +
		"endcmap\n"
	cm, err := cmap.Parse(context.Background(), []byte(src))
	if err != nil {
		t.Fatalf("cmap.Parse: %v", err)
	}
	font := &Font{
		Subtype:    "Type0",
		ToUnicode:  cm,
		UnitsPerEm: 1000,
	}

	const codes = 50000
	shown := make([]byte, 0, 2*codes)
	for i := range codes {
		shown = append(shown, 0xF0|byte(i>>8&0x0F), byte(i))
	}

	done := make(chan error, 1)
	go func() {
		machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
		var elements []models.TextElement
		done <- processText(context.Background(), ast.PdfString(shown), font, machine, &elements, nil)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("processText: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("processText took over 10s: each unmapped code scans the whole mapping")
	}
}
