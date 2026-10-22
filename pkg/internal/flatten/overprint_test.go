package flatten

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// overprintFixture paints cyan, then magenta under overprint mode 1 (ISO
// 32000-1, 8.6.7), which a half-transparent black square overlaps.
func overprintFixture() fixture {
	return fixture{
		resources: "/ExtGState << /GS1 6 0 R /OP 7 0 R >>",
		bare:      true,
		content:   "1 0 0 0 k 0 0 100 100 re f q /OP gs 0 1 0 0 k 0 0 100 100 re f Q q /GS1 gs 0 0 0 1 k 40 40 20 20 re f Q",
		objs:      []string{"<< /Type /ExtGState /OP true /op true /OPM 1 >>"},
	}
}

func TestFlattenReportsOverprintComposited(t *testing.T) {
	t.Parallel()
	_, report := flattenBytes(t, overprintFixture().bytes(), Options{DPI: 72})
	if len(report.Pages) != 1 {
		t.Fatalf("report %+v", report)
	}
	if pr := report.Pages[0]; pr.Overprinted != 1 {
		t.Errorf("overprinted %d, want the magenta fill: %+v", pr.Overprinted, pr)
	}
}

func TestFlattenSimulatesOverprint(t *testing.T) {
	t.Parallel()
	// The composite holds what a CMYK device prints: the magenta left the
	// cyan under it, so the half-transparent black composites over both
	// inks, C, M and K at half; without the simulation cyan would be 0.
	out, report := flattenBytes(t, overprintFixture().bytes(), Options{DPI: 72, Overprint: true})
	if len(report.Pages) != 1 || report.Pages[0].Overprinted != 1 {
		t.Fatalf("report %+v", report)
	}
	dict, samples := compositeImage(t, out)
	if cs, _ := dict["ColorSpace"].(ast.PdfName); cs != "DeviceCMYK" {
		t.Fatalf("composite colour space %v, want DeviceCMYK", dict["ColorSpace"])
	}
	w, _ := dict["Width"].(ast.PdfInt)
	h, _ := dict["Height"].(ast.PdfInt)
	if len(samples) != 4*int(w)*int(h) {
		t.Fatalf("%d samples for a %dx%d CMYK image", len(samples), w, h)
	}
	i := 4 * (5*int(w) + 5)
	if got := samples[i : i+4]; got[0] < 120 || got[1] < 120 || got[2] != 0 || got[3] < 120 {
		t.Errorf("composite pixel = %v, want C, M and K near 128", got)
	}
}
